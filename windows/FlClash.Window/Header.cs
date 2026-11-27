using System.Windows;
using System.Windows.Controls;
using System.Windows.Controls.Primitives;
using System.Windows.Media;
using Shapes = System.Windows.Shapes;

namespace FlClash;

/// The look of a state: its colour, its symbol, and how much of the colour
/// the card takes. A state that asks for attention colours it.
public readonly record struct Look(Tint Tint, string Glyph, string Mark, double Wash)
{
    public const string Shield = "";
    public const string ShieldCheck = "";
    public const string ShieldLock = "";

    public static Look Of(Snapshot s)
    {
        var st = s.Status;
        if (s.HelperReady && st.Blocked) return new(Tint.Red, ShieldLock, "", 0.22);
        if (!s.HelperReady || (st.Running && !st.Captured)) return new(Tint.Orange, Shield, "!", 0.18);
        if (st.Running) return new(Tint.Green, ShieldCheck, "", 0.18);
        return new(Tint.Gray, Shield, "", 0);
    }
}

/// A round badge that says the state with a symbol and a colour.
public sealed class Badge
{
    public readonly Grid View = new();
    readonly Shapes.Ellipse disc = new();
    readonly TextBlock icon = new() { FontFamily = Ui.Icons, HorizontalAlignment = HorizontalAlignment.Center, VerticalAlignment = VerticalAlignment.Center };
    readonly TextBlock mark = new() { FontWeight = Ui.Semibold, HorizontalAlignment = HorizontalAlignment.Center, VerticalAlignment = VerticalAlignment.Center };

    public Badge(double side = 46)
    {
        View.Width = View.Height = side;
        disc.StrokeThickness = 1.5;
        icon.FontSize = side * 0.45;
        mark.FontSize = side * 0.24;
        mark.Margin = new Thickness(0, 0, 0, 1);
        View.Children.Add(disc);
        View.Children.Add(icon);
        View.Children.Add(mark);
    }

    public void Show(Look look)
    {
        var c = Ui.Color(look.Tint);
        disc.Fill = new SolidColorBrush(Ui.Alpha(c, 0.18));
        disc.Stroke = new SolidColorBrush(Ui.Alpha(c, 0.35));
        icon.Text = look.Glyph;
        icon.Foreground = new SolidColorBrush(c);
        mark.Text = look.Mark;
        mark.Foreground = new SolidColorBrush(c);
    }
}

/// The last minute of traffic as a small curve: what came down is the filled
/// shape, what went up is the line over it.
public sealed class TrafficGraph
{
    public readonly Canvas View = new();
    const int HistoryLength = 60;
    readonly Shapes.Line ground = new() { StrokeThickness = 1 };
    readonly Shapes.Polygon area = new();
    readonly Shapes.Polyline down = new() { StrokeThickness = 1.5, StrokeLineJoin = PenLineJoin.Round };
    readonly Shapes.Polyline up = new() { StrokeThickness = 1, StrokeLineJoin = PenLineJoin.Round };

    public TrafficGraph()
    {
        View.Width = 110;
        View.Height = 34;
        View.Children.Add(ground);
        View.Children.Add(area);
        View.Children.Add(down);
        View.Children.Add(up);
    }

    public void Draw(List<long[]> samples, Tint tint)
    {
        double w = View.Width, h = View.Height;
        ground.X1 = 1; ground.X2 = w - 1; ground.Y1 = ground.Y2 = h - 1.5;
        ground.Stroke = Ui.B("Divider");
        var c = Ui.Color(tint);
        var downs = new PointCollection();
        var ups = new PointCollection();
        var shape = new PointCollection();
        samples = samples.Where(s => s != null && s.Length >= 2).ToList();
        if (samples.Count >= 2)
        {
            // The scale never drops under a few kilobytes: an idle line lies flat.
            double peak = Math.Max(samples.Max(s => Math.Max(s[0], s[1])), 8 << 10);
            double left = 1, right = w - 1, bottom = h - 2, height = h - 4;
            double step = (right - left) / (HistoryLength - 1);
            double start = right - step * (samples.Count - 1);
            for (int i = 0; i < samples.Count; i++)
            {
                double x = start + step * i;
                downs.Add(new Point(x, bottom - height * samples[i][0] / peak));
                ups.Add(new Point(x, bottom - height * samples[i][1] / peak));
            }
            foreach (var p in downs) shape.Add(p);
            shape.Add(new Point(right, bottom));
            shape.Add(new Point(start, bottom));
        }
        down.Points = downs;
        up.Points = ups;
        area.Points = shape;
        var fill = new LinearGradientBrush { StartPoint = new Point(0, 0), EndPoint = new Point(0, 1) };
        fill.GradientStops.Add(new GradientStop(Ui.Alpha(c, 0.30), 0));
        fill.GradientStops.Add(new GradientStop(Ui.Alpha(c, 0.02), 1));
        area.Fill = fill;
        down.Stroke = new SolidColorBrush(Ui.Alpha(c, 0.9));
        up.Stroke = Ui.Secondary;
        up.Opacity = 0.7;
    }
}

/// What is true right now, the one button that changes it, and the choices
/// people change most.
public sealed class Header
{
    public readonly Grid View = new();
    readonly MainWindow window;
    Agent A => window.Agent;

    readonly Border card = new();
    readonly Border wash = new();
    readonly Badge badge = new();
    readonly TextBlock headline = Ui.Text("", 18, Ui.Semibold);
    readonly TextBlock detail = Ui.Text("", 12, null, Ui.Secondary);
    readonly StackPanel exitLine = new() { Orientation = Orientation.Horizontal };
    readonly TextBlock testMark = Ui.Text(L.T("Test environment"), 11, Ui.Semibold);
    readonly Button button = new() { MinWidth = 120, Padding = new Thickness(18, 8, 18, 8) };
    readonly Spinner spinner = new();
    readonly TrafficGraph graph = new();
    readonly TextBlock downRate = Ui.Text("", 11, null, Ui.Secondary);
    readonly TextBlock upRate = Ui.Text("", 11, null, Ui.Secondary);
    readonly StackPanel traffic = new() { Orientation = Orientation.Horizontal, VerticalAlignment = VerticalAlignment.Center };
    readonly ToggleButton[] modes = new ToggleButton[3];
    static readonly string[] modeNames = { "proxy", "system", "tun" };
    readonly ComboBox policies = new() { MinWidth = 200, MaxWidth = 260 };
    readonly Button address = new();
    readonly TextBlock addressText = Ui.Text("", 13, null, Ui.Secondary);
    readonly TextBlock addressIcon = Ui.Glyph("", 11, Ui.Secondary);
    readonly Grid noticeRow = Ui.Columns(Ui.Auto, -1, Ui.Auto);
    readonly TextBox notice = new() { IsReadOnly = true, TextWrapping = TextWrapping.Wrap, BorderThickness = new Thickness(0), Background = Brushes.Transparent, Padding = new Thickness(0), FontSize = 12 };
    readonly Button noticeButton = new();
    bool installing, copied, filling;

    public Header(MainWindow window)
    {
        this.window = window;
        testMark.Foreground = Ui.Brush(Tint.Orange);
        var titles = Ui.Column(3, headline, detail, exitLine);
        titles.VerticalAlignment = VerticalAlignment.Center;

        // No Enter shortcut: a stray key must not turn the connection on or off.
        button.Click += (_, _) => A.Toggle();
        downRate.FontFamily = upRate.FontFamily = new FontFamily("Segoe UI Variable Small, Segoe UI");
        var rates = Ui.Column(3, downRate, upRate);
        rates.MinWidth = 84;
        rates.VerticalAlignment = VerticalAlignment.Center;
        traffic.Children.Add(graph.View);
        Ui.Add(traffic, rates, 10);

        var top = Ui.Columns(Ui.Auto, -1, Ui.Auto, Ui.Auto, Ui.Auto);
        var titleRow = Ui.Columns(-1, Ui.Auto);
        titleRow.Children.Add(titles);
        testMark.VerticalAlignment = VerticalAlignment.Top;
        testMark.Margin = new Thickness(12, 4, 0, 0);
        Ui.Place(titleRow, testMark, 1);
        Ui.Place(top, badge.View, 0);
        Ui.Place(top, titleRow, 1, 14);
        Ui.Place(top, traffic, 2, 14);
        Ui.Place(top, spinner.View, 3, 14);
        Ui.Place(top, button, 4, 14);
        button.VerticalAlignment = VerticalAlignment.Center;

        // The modes as one segmented switch.
        var segments = new StackPanel { Orientation = Orientation.Horizontal };
        string[] titlesOfModes = { L.T("Port Only"), L.T("System Proxy"), L.T("TUN") };
        string[] tips = {
            L.T("Nothing changes in the system: only apps pointed at the local port use it."),
            L.T("Apps that follow the system proxy setting go through the local port."),
            L.T("Everything on this computer goes through the tunnel."),
        };
        for (int i = 0; i < 3; i++)
        {
            int index = i;
            var b = new ToggleButton { Content = titlesOfModes[i], MinWidth = 70 };
            Ui.SetCorner(b, i == 0 ? new CornerRadius(6, 0, 0, 6) : i == 2 ? new CornerRadius(0, 6, 6, 0) : new CornerRadius(0));
            if (i > 0) b.Margin = new Thickness(-1, 0, 0, 0);
            b.ToolTip = tips[i];
            b.Click += async (_, _) =>
            {
                if (filling) return;
                Update();
                // The tunnel installs the helper first; declined, the mode stays
                // and the switches are drawn again.
                await A.SetMode(modeNames[index]);
                Update();
            };
            modes[i] = b;
            segments.Children.Add(b);
        }
        segments.VerticalAlignment = VerticalAlignment.Center;

        policies.ToolTip = L.T("Routing: which connections go through the server.");
        policies.VerticalAlignment = VerticalAlignment.Center;
        policies.SelectionChanged += (_, _) =>
        {
            if (filling || policies.SelectedItem is not ComboBoxItem { Tag: string name }) return;
            if (name != A.S.View.Policy) A.SelectPolicy(name);
        };

        address.Style = Ui.Style("Subtle");
        address.Content = Ui.Row(6, addressText, addressIcon);
        address.Padding = new Thickness(6, 4, 6, 4);
        address.VerticalAlignment = VerticalAlignment.Center;
        address.ToolTip = L.T("The local port for HTTP and SOCKS. Click to copy; change it in Settings.");
        address.Click += (_, _) => CopyAddress();

        var choices = Ui.Columns(Ui.Auto, Ui.Auto, -1, Ui.Auto);
        Ui.Place(choices, segments, 0);
        Ui.Place(choices, policies, 1, 10);
        Ui.Place(choices, address, 3, 10);

        var warning = Ui.Glyph("", 14, Ui.Brush(Tint.Yellow));
        warning.VerticalAlignment = VerticalAlignment.Top;
        warning.Margin = new Thickness(0, 2, 0, 0);
        notice.SetResourceReference(Control.ForegroundProperty, "Text2");
        notice.Style = null;
        notice.Template = PlainText();
        noticeButton.VerticalAlignment = VerticalAlignment.Top;
        noticeButton.Padding = new Thickness(10, 3, 10, 4);
        noticeButton.Click += (_, _) => NoticeAction();
        Ui.Place(noticeRow, warning, 0);
        Ui.Place(noticeRow, notice, 1, 8);
        Ui.Place(noticeRow, noticeButton, 2, 8);

        var column = Ui.Column(14, top, choices, noticeRow);
        column.Margin = new Thickness(16, 16, 16, 14);

        card.CornerRadius = wash.CornerRadius = new CornerRadius(16);
        card.BorderThickness = new Thickness(1);
        var stack = new Grid();
        stack.Children.Add(wash);
        stack.Children.Add(column);
        card.Child = stack;
        View.Margin = new Thickness(16, 4, 16, 8);
        View.Children.Add(card);
    }

    /// Text that can be selected and copied, drawn as text.
    static ControlTemplate PlainText()
    {
        var t = new ControlTemplate(typeof(TextBox));
        var host = new FrameworkElementFactory(typeof(ScrollViewer), "PART_ContentHost");
        host.SetValue(ScrollViewer.FocusableProperty, false);
        host.SetValue(ScrollViewer.HorizontalScrollBarVisibilityProperty, ScrollBarVisibility.Disabled);
        host.SetValue(ScrollViewer.VerticalScrollBarVisibilityProperty, ScrollBarVisibility.Disabled);
        t.VisualTree = host;
        return t;
    }

    public void Update()
    {
        var s = A.S;
        var st = s.Status;
        filling = true;
        var look = Look.Of(s);
        badge.Show(look);
        card.Background = Ui.B("Card");
        card.BorderBrush = Ui.B("CardStroke");
        if (look.Wash > 0)
        {
            var c = Ui.Color(look.Tint);
            var g = new LinearGradientBrush { StartPoint = new Point(0, 0.5), EndPoint = new Point(1, 0.5) };
            g.GradientStops.Add(new GradientStop(Ui.Alpha(c, look.Wash), 0));
            g.GradientStops.Add(new GradientStop(Ui.Alpha(c, look.Wash * 0.25), 0.45));
            g.GradientStops.Add(new GradientStop(Ui.Alpha(c, 0), 0.9));
            wash.Background = g;
        }
        else wash.Background = null;
        testMark.Text = L.T("Test environment");
        testMark.Visibility = s.Test ? Visibility.Visible : Visibility.Collapsed;

        headline.Text = st.Headline(s.HelperReady);
        // While on, say what runs; while off, say what would.
        var server = st.Running ? st.Server : s.SelectedServer?.Name ?? "";
        if (st.Blocked)
        {
            detail.Text = st.Mode == "system"
                ? (st.Server.Length == 0 ? L.T("The core stopped") : L.F("The connection through %@ stopped", st.Server))
                : (st.Server.Length == 0 ? L.T("The tunnel is down") : L.F("The tunnel through %@ is down", st.Server));
        }
        else if (server.Length == 0) detail.Text = L.T("Choose a server to connect");
        else
        {
            var parts = new List<string> { server };
            if (st.Running && st.Captured && st.Since > 0 && Format.Moment(st.Since) is { } since) parts.Add(L.F("since %@", since));
            if (st.Running && st.Guard == "full") parts.Add(L.T("protected"));
            detail.Text = string.Join("  ·  ", parts);
        }

        // Where the server lets traffic out, once that is known.
        exitLine.Children.Clear();
        if (st.Running && s.Exit is { } exit)
        {
            if (!string.IsNullOrEmpty(exit.Country))
            {
                exitLine.Children.Add(Ui.CountryPlate(exit.Country, 9));
                Ui.Add(exitLine, Ui.Text(Format.Country(exit.Country!) + "  ·  " + exit.Ip, 12, null, Ui.Secondary), 6);
            }
            else exitLine.Children.Add(Ui.Text(exit.Ip ?? "", 12, null, Ui.Secondary));
            exitLine.Visibility = Visibility.Visible;
        }
        else exitLine.Visibility = Visibility.Collapsed;

        // While on, the switches show what runs; while off, what would.
        var mode = st.Running && st.Mode.Length > 0 ? st.Mode : s.View.Mode;
        for (int i = 0; i < 3; i++)
        {
            modes[i].IsChecked = modeNames[i] == mode;
            modes[i].IsEnabled = !s.Busy;
        }

        var policy = st.Running && st.Policy.Length > 0 ? st.Policy : s.View.Policy;
        var have = policies.Items.OfType<ComboBoxItem>().Select(i => (string)i.Tag).ToList();
        if (!have.SequenceEqual(s.View.Policies.Select(p => p.Name)))
        {
            policies.Items.Clear();
            foreach (var p in s.View.Policies)
                policies.Items.Add(new ComboBoxItem { Content = Format.PolicyTitle(p.Name), Tag = p.Name, ToolTip = Format.PolicyAbout(p) });
        }
        policies.SelectedIndex = s.View.Policies.FindIndex(p => p.Name == policy);
        policies.IsEnabled = !s.Busy;

        if (!copied)
        {
            // Open to the network, the port is reached by this computer's address there;
            // what runs says so itself, the setting is for when nothing does.
            var open = st.Running ? st.Lan : s.View.Lan;
            var host = open ? Format.LanAddress() ?? "127.0.0.1" : "127.0.0.1";
            addressText.Text = host + ":" + (st.Running && st.Port > 0 ? st.Port : s.View.Port);
        }

        button.Content = st.Running ? L.T("Disconnect") : st.Blocked ? L.T("Connect Again") : L.T("Connect");
        button.IsEnabled = s.HelperReady && !s.Busy && (st.Running || s.SelectedServer != null);
        // Turning on is the thing to do here; turning off is not.
        if (button.IsEnabled && !st.Running) button.Style = Ui.Style("Accent");
        else button.ClearValue(FrameworkElement.StyleProperty);
        spinner.IsActive = s.Busy || installing;

        notice.ToolTip = null;
        noticeButton.Visibility = Visibility.Visible;
        noticeRow.Visibility = Visibility.Visible;
        if (!s.HelperReady && !st.Installed)
        {
            notice.Text = L.T("To capture the traffic of this computer, FlClash needs a system helper. Installing it asks for administrator rights once.");
            noticeButton.Content = L.T("Install Helper…");
            noticeButton.IsEnabled = !installing;
        }
        else if (!s.HelperReady && st.Outdated)
        {
            notice.Text = L.T("The system helper is from another version of FlClash and has to be installed again. A tunnel that is on will stop for a moment.");
            noticeButton.Content = L.T("Update Helper…");
            noticeButton.IsEnabled = !installing;
        }
        else if (st.Blocked)
        {
            notice.Text = st.Mode == "system"
                ? L.T("The system still points at FlClash, so apps that follow the system proxy are offline until it is back. They do not go out directly.")
                : L.T("Protection lets nothing out past the tunnel, so the internet is off until the tunnel is back. The local network works.");
            noticeButton.Content = L.T("Unblock");
            noticeButton.IsEnabled = !s.Busy;
        }
        else if (!string.IsNullOrEmpty(s.Problem) && !window.ShowsSettings)
        {
            notice.Text = s.Problem;
            noticeButton.Content = L.T("Dismiss");
            noticeButton.IsEnabled = true;
        }
        else if (st.Running && st.GuardProblem.Length > 0)
        {
            notice.Text = L.T("Protection did not turn on: traffic can leave past the tunnel.");
            notice.ToolTip = st.GuardProblem;
            noticeButton.Visibility = Visibility.Collapsed;
        }
        else noticeRow.Visibility = Visibility.Collapsed;
        filling = false;
        UpdateTraffic(look);
    }

    void UpdateTraffic(Look look)
    {
        var s = A.S;
        if (s.Stats is not { } stats || !s.Status.Running)
        {
            traffic.Visibility = Visibility.Collapsed;
            return;
        }
        traffic.Visibility = Visibility.Visible;
        graph.Draw(s.History, look.Tint);
        downRate.Text = "↓ " + Format.Rate(stats.Down);
        upRate.Text = "↑ " + Format.Rate(stats.Up);
        traffic.ToolTip = L.F("Since the tunnel came up: %@ down, %@ up", Format.Amount(stats.DownTotal), Format.Amount(stats.UpTotal));
    }

    /// Puts the address of the local port on the clipboard, and says so for a moment.
    async void CopyAddress()
    {
        if (copied) return;
        if (Ui.Copy(addressText.Text, secret: false) is { } problem)
        {
            await Ui.Say(L.T("Nothing was copied"), problem);
            return;
        }
        var shown = addressText.Text;
        copied = true;
        addressText.Text = L.T("Copied");
        addressIcon.Text = "";
        await Task.Delay(1200);
        copied = false;
        addressText.Text = shown;
        addressIcon.Text = "";
        Update();
    }

    async void NoticeAction()
    {
        var s = A.S;
        if (s.HelperReady)
        {
            // Blocked: the button takes the guard off. Otherwise it hides a message.
            if (s.Status.Blocked) A.TurnOff(); else A.Dismiss();
            return;
        }
        installing = true;
        Update();
        var reply = await A.InstallHelper();
        installing = false;
        Update();
        if (!string.IsNullOrEmpty(reply.Error)) await Ui.Say(L.T("The helper was not installed"), reply.Error!);
    }
}
