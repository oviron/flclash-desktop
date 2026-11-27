using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Controls.Primitives;
using Microsoft.UI.Xaml.Media;
using Microsoft.UI.Xaml.Shapes;
using Windows.ApplicationModel.DataTransfer;
using Windows.Foundation;

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
/// (The views are held, not derived from: a C# class derived from a XAML
/// one breaks the window when the system calls back into it.)
public sealed class Badge
{
    public readonly Grid View = new();
    readonly Ellipse disc = new();
    readonly FontIcon icon = new();
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
        icon.Glyph = look.Glyph;
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
    readonly Line ground = new() { StrokeThickness = 1 };
    readonly Polygon area = new();
    readonly Polyline down = new() { StrokeThickness = 1.5, StrokeLineJoin = PenLineJoin.Round };
    readonly Polyline up = new() { StrokeThickness = 1, StrokeLineJoin = PenLineJoin.Round };

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
        ground.Stroke = Ui.Res("DividerStrokeColorDefaultBrush");
        var c = Ui.Color(tint);
        down.Points.Clear(); up.Points.Clear(); area.Points.Clear();
        if (samples.Count < 2) return;
        // The scale never drops under a few kilobytes: an idle line lies flat.
        double peak = Math.Max(samples.Max(s => Math.Max(s[0], s[1])), 8 << 10);
        double left = 1, right = w - 1, bottom = h - 2, height = h - 4;
        double step = (right - left) / (HistoryLength - 1);
        double start = right - step * (samples.Count - 1);
        for (int i = 0; i < samples.Count; i++)
        {
            double x = start + step * i;
            down.Points.Add(new Point(x, bottom - height * samples[i][0] / peak));
            up.Points.Add(new Point(x, bottom - height * samples[i][1] / peak));
        }
        foreach (var p in down.Points) area.Points.Add(p);
        area.Points.Add(new Point(right, bottom));
        area.Points.Add(new Point(start, bottom));
        var fill = new LinearGradientBrush { StartPoint = new(0, 0), EndPoint = new(0, 1) };
        fill.GradientStops.Add(new GradientStop { Color = Ui.Alpha(c, 0.30), Offset = 0 });
        fill.GradientStops.Add(new GradientStop { Color = Ui.Alpha(c, 0.02), Offset = 1 });
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
    readonly StackPanel exitLine = new() { Orientation = Orientation.Horizontal, Spacing = 6 };
    readonly TextBlock testMark = Ui.Text(L.T("Test environment"), 11, Ui.Semibold);
    readonly Button button = new() { MinWidth = 120, Padding = new Thickness(18, 8, 18, 8) };
    readonly ProgressRing spinner = new() { Width = 18, Height = 18, IsActive = false };
    readonly TrafficGraph graph = new();
    readonly TextBlock downRate = Ui.Text("", 11, null, Ui.Secondary);
    readonly TextBlock upRate = Ui.Text("", 11, null, Ui.Secondary);
    readonly StackPanel traffic = new() { Orientation = Orientation.Horizontal, Spacing = 10, VerticalAlignment = VerticalAlignment.Center };
    readonly ToggleButton[] modes = new ToggleButton[3];
    static readonly string[] modeNames = ["proxy", "system", "tun"];
    readonly ComboBox policies = new() { MinWidth = 200, MaxWidth = 260 };
    readonly Button address = new();
    readonly TextBlock addressText = Ui.Text("", 13, null, Ui.Secondary);
    readonly FontIcon addressIcon = Ui.Glyph("", 11, Ui.Secondary);
    readonly Grid noticeRow = new() { ColumnSpacing = 8 };
    readonly TextBlock notice = Ui.Text("", 12, null, Ui.Secondary, wrap: true);
    readonly Button noticeButton = new();
    bool installing, copied, filling;

    public Header(MainWindow window)
    {
        this.window = window;
        foreach (var t in new[] { headline, detail }) t.MaxLines = 1;
        testMark.Foreground = Ui.Brush(Tint.Orange);
        testMark.Margin = new Thickness(4, 0, 0, 0);
        var titles = Ui.Column(3, headline, detail, exitLine);
        titles.VerticalAlignment = VerticalAlignment.Center;

        // No Enter shortcut: a stray key must not turn the connection on or off.
        button.Click += (_, _) => A.Toggle();
        downRate.FontFamily = upRate.FontFamily = new FontFamily("Segoe UI Variable Small");
        var rates = Ui.Column(3, downRate, upRate);
        rates.MinWidth = 84;
        rates.VerticalAlignment = VerticalAlignment.Center;
        traffic.Children.Add(graph.View);
        traffic.Children.Add(rates);

        var top = new Grid { ColumnSpacing = 14 };
        foreach (var w in new[] { GridLength.Auto, new GridLength(1, GridUnitType.Star), GridLength.Auto, GridLength.Auto, GridLength.Auto, GridLength.Auto })
            top.ColumnDefinitions.Add(new ColumnDefinition { Width = w });
        var titleRow = new Grid();
        titleRow.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        titleRow.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        titleRow.Children.Add(titles);
        Grid.SetColumn(testMark, 1);
        testMark.VerticalAlignment = VerticalAlignment.Top;
        testMark.Margin = new Thickness(12, 4, 0, 0);
        titleRow.Children.Add(testMark);
        Place(top, badge.View, 0); Place(top, titleRow, 1); Place(top, traffic, 2); Place(top, spinner, 3); Place(top, button, 4);
        spinner.VerticalAlignment = button.VerticalAlignment = VerticalAlignment.Center;

        // The modes as one segmented switch.
        var segments = new StackPanel { Orientation = Orientation.Horizontal, Spacing = 0 };
        string[] titlesOfModes = [L.T("Port Only"), L.T("System Proxy"), L.T("TUN")];
        string[] tips = [
            L.T("Nothing changes in the system: only apps pointed at the local port use it."),
            L.T("Apps that follow the system proxy setting go through the local port."),
            L.T("Everything on this computer goes through the tunnel."),
        ];
        for (int i = 0; i < 3; i++)
        {
            int index = i;
            var b = new ToggleButton { Content = titlesOfModes[i], Padding = new Thickness(14, 5, 14, 6), MinWidth = 70 };
            b.CornerRadius = i == 0 ? new CornerRadius(6, 0, 0, 6) : i == 2 ? new CornerRadius(0, 6, 6, 0) : new CornerRadius(0);
            if (i > 0) b.Margin = new Thickness(-1, 0, 0, 0);
            ToolTipService.SetToolTip(b, tips[i]);
            b.Click += (_, _) =>
            {
                if (filling) return;
                A.SetMode(modeNames[index]);
                Update();
            };
            modes[i] = b;
            segments.Children.Add(b);
        }
        segments.VerticalAlignment = VerticalAlignment.Center;

        ToolTipService.SetToolTip(policies, L.T("Routing: which connections go through the server."));
        policies.VerticalAlignment = VerticalAlignment.Center;
        policies.SelectionChanged += (_, _) =>
        {
            if (filling || policies.SelectedItem is not ComboBoxItem { Tag: string name }) return;
            if (name != A.S.View.Policy) A.SelectPolicy(name);
        };

        address.Content = Ui.Row(6, addressText, addressIcon);
        address.Background = new SolidColorBrush(Microsoft.UI.Colors.Transparent);
        address.BorderThickness = new Thickness(0);
        address.Padding = new Thickness(6, 4, 6, 4);
        address.VerticalAlignment = VerticalAlignment.Center;
        ToolTipService.SetToolTip(address, L.T("The local port for HTTP and SOCKS. Click to copy; change it in Settings."));
        address.Click += (_, _) => CopyAddress();

        var choices = new Grid { ColumnSpacing = 10 };
        choices.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        choices.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        choices.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        choices.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        Place(choices, segments, 0); Place(choices, policies, 1); Place(choices, address, 3);

        var warning = Ui.Glyph("", 14, Ui.Brush(Tint.Yellow));
        warning.VerticalAlignment = VerticalAlignment.Top;
        warning.Margin = new Thickness(0, 2, 0, 0);
        notice.IsTextSelectionEnabled = true;
        noticeButton.VerticalAlignment = VerticalAlignment.Top;
        noticeButton.Padding = new Thickness(10, 3, 10, 4);
        noticeButton.Click += (_, _) => NoticeAction();
        noticeRow.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        noticeRow.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        noticeRow.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        Place(noticeRow, warning, 0); Place(noticeRow, notice, 1); Place(noticeRow, noticeButton, 2);

        var column = Ui.Column(14, top, choices, noticeRow);
        column.Padding = new Thickness(16, 16, 16, 14);

        card.CornerRadius = wash.CornerRadius = new CornerRadius(16);
        card.BorderThickness = new Thickness(1);
        var stack = new Grid();
        stack.Children.Add(wash);
        stack.Children.Add(column);
        card.Child = stack;
        View.Margin = new Thickness(16, 4, 16, 8);
        View.Children.Add(card);
    }

    static void Place(Grid g, FrameworkElement e, int column)
    {
        Grid.SetColumn(e, column);
        g.Children.Add(e);
    }

    public void Update()
    {
        var s = A.S;
        var st = s.Status;
        filling = true;
        var look = Look.Of(s);
        badge.Show(look);
        card.Background = Ui.Res("CardBackgroundFillColorDefaultBrush");
        card.BorderBrush = Ui.Res("CardStrokeColorDefaultBrush");
        if (look.Wash > 0)
        {
            var c = Ui.Color(look.Tint);
            var g = new LinearGradientBrush { StartPoint = new(0, 0.5), EndPoint = new(1, 0.5) };
            g.GradientStops.Add(new GradientStop { Color = Ui.Alpha(c, look.Wash), Offset = 0 });
            g.GradientStops.Add(new GradientStop { Color = Ui.Alpha(c, look.Wash * 0.25), Offset = 0.45 });
            g.GradientStops.Add(new GradientStop { Color = Ui.Alpha(c, 0), Offset = 0.9 });
            wash.Background = g;
        }
        else wash.Background = null;
        testMark.Text = L.T("Test environment");
        testMark.Visibility = s.Test ? Visibility.Visible : Visibility.Collapsed;

        headline.Text = st.Headline;
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
                exitLine.Children.Add(Ui.Text(Format.Country(exit.Country!) + "  ·  " + exit.Ip, 12, null, Ui.Secondary));
            }
            else exitLine.Children.Add(Ui.Text(exit.Ip, 12, null, Ui.Secondary));
            exitLine.Visibility = Visibility.Visible;
        }
        else exitLine.Visibility = Visibility.Collapsed;

        // While on, the switches show what runs; while off, what would.
        var mode = st.Running && st.Mode.Length > 0 ? st.Mode : s.View.Mode;
        for (int i = 0; i < 3; i++)
        {
            modes[i].IsChecked = modeNames[i] == mode;
            modes[i].IsEnabled = s.HelperReady && !s.Busy;
        }

        var policy = st.Running && st.Policy.Length > 0 ? st.Policy : s.View.Policy;
        var have = policies.Items.OfType<ComboBoxItem>().Select(i => (string)i.Tag).ToList();
        if (!have.SequenceEqual(s.View.Policies.Select(p => p.Name)))
        {
            policies.Items.Clear();
            foreach (var p in s.View.Policies)
            {
                var item = new ComboBoxItem { Content = Format.PolicyTitle(p.Name), Tag = p.Name };
                ToolTipService.SetToolTip(item, Format.PolicyAbout(p));
                policies.Items.Add(item);
            }
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
        if (button.IsEnabled && !st.Running) button.Style = (Style)Ui.Find(Application.Current.Resources, "AccentButtonStyle")!;
        else button.ClearValue(FrameworkElement.StyleProperty);
        spinner.IsActive = s.Busy || installing;
        spinner.Visibility = spinner.IsActive ? Visibility.Visible : Visibility.Collapsed;

        ToolTipService.SetToolTip(notice, null);
        noticeButton.Visibility = Visibility.Visible;
        noticeRow.Visibility = Visibility.Visible;
        if (!st.Installed)
        {
            notice.Text = L.T("To capture the traffic of this computer, FlClash needs a system helper. Installing it asks for administrator rights once.");
            noticeButton.Content = L.T("Install Helper…");
            noticeButton.IsEnabled = !installing;
        }
        else if (st.Outdated)
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
            ToolTipService.SetToolTip(notice, st.GuardProblem);
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
        ToolTipService.SetToolTip(traffic, L.F("Since the tunnel came up: %@ down, %@ up", Format.Amount(stats.DownTotal), Format.Amount(stats.UpTotal)));
    }

    /// Puts the address of the local port on the clipboard, and says so for a moment.
    async void CopyAddress()
    {
        if (copied) return;
        if (Ui.Copy(addressText.Text, secret: false) is { } problem)
        {
            await Ui.Say(View.XamlRoot, L.T("Nothing was copied"), problem);
            return;
        }
        var shown = addressText.Text;
        copied = true;
        addressText.Text = L.T("Copied");
        addressIcon.Glyph = "";
        await Task.Delay(1200);
        copied = false;
        addressText.Text = shown;
        addressIcon.Glyph = "";
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
        if (!string.IsNullOrEmpty(reply.Error) && View.XamlRoot != null)
            await Ui.Say(View.XamlRoot, L.T("The helper was not installed"), reply.Error);
    }
}
