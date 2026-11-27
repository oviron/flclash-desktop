using System.Runtime.InteropServices;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;
using System.Windows.Interop;
using System.Windows.Media;
using System.Windows.Media.Imaging;

namespace FlClash;

/// A page under the header: it draws from the state and searches for what
/// the search field holds.
public interface IPage
{
    FrameworkElement View { get; }
    string Title { get; }
    string Subtitle { get; }
    /// Draws the state; force draws it even when it seems not to have changed.
    void Update(bool force);
    string Query { set; }
    void Shown();
    void Hidden();
}

/// The window: a sidebar, a toolbar that follows the page, the header with
/// the state, and the page.
public sealed class MainWindow : Window
{
    public readonly Agent Agent;
    public Header Header = null!;
    public ServersPage Servers = null!;
    public RoutingPage Routing = null!;
    public ConnectionsPage Connections = null!;
    public LogPage Log = null!;
    public SettingsPage Settings = null!;

    readonly Grid root = new();
    readonly Grid body = new();
    readonly Border pageHost = new();
    readonly TextBlock pageTitle = Ui.Text("", 20, Ui.Semibold);
    readonly TextBlock pageSubtitle = Ui.Text("", 12, null, Ui.Secondary);
    readonly Dictionary<string, RadioButton> items = new();
    readonly Button order = new();
    readonly Button measure = new();
    readonly Button addServers = new();
    readonly Button addPolicy = new();
    readonly Button pause = new();
    readonly TextBox search = Ui.Field("", "", 220);
    readonly Grid dialogs = new();
    IPage current;
    string page = "servers";
    string shownLang = "";
    bool shownDark;
    WindowPrefs prefs = new();
    static readonly string prefsPath = Path.Combine(App.DataDir, "window.json");

    public bool ShowsSettings => page == "settings";
    public WindowPrefs Prefs => prefs;
    public IntPtr Hwnd => new WindowInteropHelper(this).Handle;

    public MainWindow(Agent agent, string? startPage)
    {
        Agent = agent;
        ReadPrefs();
        Title = "FlClash";
        FontFamily = new FontFamily("Segoe UI Variable Text, Segoe UI");
        FontSize = 14;
        UseLayoutRounding = true;
        SetResourceReference(ForegroundProperty, "Text1");
        SetResourceReference(BackgroundProperty, "Solid");
        MinWidth = 800;
        MinHeight = 520;
        Width = 980;
        Height = 660;
        WindowStartupLocation = WindowStartupLocation.CenterScreen;
        try
        {
            using var icon = typeof(MainWindow).Assembly.GetManifestResourceStream("FlClash.ico");
            if (icon != null) Icon = BitmapFrame.Create(icon, BitmapCreateOptions.None, BitmapCacheOption.OnLoad);
        }
        catch (Exception) { }
        shownDark = Ui.Dark;

        MakePages();
        current = Servers;

        // The sidebar: the pages, and the settings at the foot.
        var nav = new Grid { Width = 210, Margin = new Thickness(4, 6, 4, 6) };
        nav.RowDefinitions.Add(new RowDefinition { Height = new GridLength(1, GridUnitType.Star) });
        nav.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        var pages = new StackPanel();
        foreach (var (tag, glyph) in new[] { ("servers", ""), ("routing", ""), ("connections", ""), ("log", ""), ("settings", "") })
        {
            var item = new RadioButton { Style = Ui.Style("Nav"), Tag = glyph, GroupName = "nav" };
            item.Checked += (_, _) => Show(tag);
            items[tag] = item;
            if (tag == "settings") { Grid.SetRow(item, 1); nav.Children.Add(item); }
            else pages.Children.Add(item);
        }
        nav.Children.Add(pages);

        // The toolbar of the page, beside its title.
        Tool(order, "", "Order");
        Ui.DropDown(order, OrderMenu);
        Tool(measure, "", "Test Latency");
        measure.Click += (_, _) => { Show("servers"); Servers.MeasureAll(); };
        Tool(addServers, "", "Add servers");
        Ui.DropDown(addServers, AddMenu);
        Tool(addPolicy, "", "Add a policy");
        Ui.DropDown(addPolicy, PolicyMenu);
        Tool(pause, "", "Pause");
        pause.Click += (_, _) => { Connections.Paused = !Connections.Paused; UpdateToolbar(); };
        search.VerticalAlignment = VerticalAlignment.Center;
        search.TextChanged += (_, _) => current.Query = search.Text;
        var tools = Ui.Row(6, order, measure, addServers, addPolicy, pause, search);
        tools.VerticalAlignment = VerticalAlignment.Center;

        var titles = Ui.Column(0, pageTitle, pageSubtitle);
        titles.VerticalAlignment = VerticalAlignment.Center;
        var toolbar = Ui.Columns(-1, Ui.Auto);
        toolbar.Margin = new Thickness(20, 8, 16, 6);
        toolbar.MinHeight = 48;
        toolbar.Children.Add(titles);
        Ui.Place(toolbar, tools, 1, 12);

        body.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        body.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        body.RowDefinitions.Add(new RowDefinition { Height = new GridLength(1, GridUnitType.Star) });
        body.Children.Add(toolbar);
        Grid.SetRow(Header.View, 1);
        body.Children.Add(Header.View);
        Grid.SetRow(pageHost, 2);
        body.Children.Add(pageHost);

        // The page lies on a layer of its own, as the system's apps draw it.
        var content = new Border { Child = body, CornerRadius = new CornerRadius(8, 0, 0, 0), BorderThickness = new Thickness(1, 1, 0, 0) };
        content.SetResourceReference(Border.BackgroundProperty, "Card");
        content.SetResourceReference(Border.BorderBrushProperty, "CardStroke");
        root.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        root.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        root.Children.Add(nav);
        Grid.SetColumn(content, 1);
        root.Children.Add(content);
        Grid.SetColumnSpan(dialogs, 2);
        root.Children.Add(dialogs);
        Dialogs.Attach(dialogs);
        Content = root;

        Accelerators();
        SourceInitialized += (_, _) =>
        {
            Ui.Backdrop(this);
            Place();
        };
        Agent.Changed += () => Redraw(false);
        Agent.Activate += p => { if (!string.IsNullOrEmpty(p)) Show(p!); Front(); };
        Agent.Lost += Close;

        // The command line may have changed the lists while the window was aside.
        Activated += (_, _) => Agent.Refresh();
        Closing += (_, _) => SavePrefs();
        Closed += (_, _) => { current.Hidden(); Agent.Close(); };
        Select(startPage ?? "servers");
    }

    void MakePages()
    {
        Header = new Header(this);
        Servers = new ServersPage(this);
        Routing = new RoutingPage(this);
        Connections = new ConnectionsPage(this);
        Log = new LogPage(this);
        Settings = new SettingsPage(this);
    }

    /// The brushes of a theme are taken when things are made: in another
    /// theme the header and the pages are made again.
    public void Themed()
    {
        if (App.Theme == null) Ui.Dark = Ui.SystemDark();
        Ui.ApplyTheme();
        Ui.Backdrop(this);
        if (Ui.Dark == shownDark && pageHost.Child != null) { Redraw(true); return; }
        shownDark = Ui.Dark;
        current.Hidden();
        body.Children.Remove(Header.View);
        MakePages();
        Grid.SetRow(Header.View, 1);
        body.Children.Add(Header.View);
        pageSubtitle.Foreground = Ui.Secondary;
        pageHost.Child = null;
        current = Servers;
        var tag = page;
        page = "";
        Show(tag);
    }

    void Tool(Button b, string glyph, string label)
    {
        b.Style = Ui.Style("SubtleButton");
        b.Content = Ui.Glyph(glyph, 16);
        b.Padding = new Thickness(10, 7, 10, 7);
        b.Tag = label;
        b.VerticalAlignment = VerticalAlignment.Center;
    }

    /// The words of the window, again: the language may change with the first state.
    void Relabel()
    {
        items["servers"].Content = L.T("Servers");
        items["routing"].Content = L.T("Routing");
        items["connections"].Content = L.T("Connections");
        items["log"].Content = L.T("Log");
        items["settings"].Content = L.T("Settings");
        foreach (var b in new[] { order, measure, addServers, addPolicy })
        {
            var text = L.T((string)b.Tag);
            b.ToolTip = text;
            AutomationName(b, text);
        }
        Ui.SetPlaceholder(search, L.T("Search"));
    }

    static void AutomationName(UIElement e, string text) => System.Windows.Automation.AutomationProperties.SetName(e, text);

    ContextMenu AddMenu()
    {
        var m = new ContextMenu();
        m.Items.Add(Ui.Item(L.T("Import from Clipboard"), () => { Show("servers"); Servers.ImportFromClipboard(); }, "Ctrl+Shift+V"));
        m.Items.Add(Ui.Item(L.T("Import from File…"), () => { Show("servers"); Servers.ImportFromFile(); }, "Ctrl+O"));
        m.Items.Add(new Separator());
        m.Items.Add(Ui.Item(L.T("Add a Subscription…"), () => { Show("servers"); Servers.AddSubscription(); }));
        m.Items.Add(Ui.Item(L.T("Update Subscriptions"), () => Agent.UpdateAll(), "Ctrl+R"));
        return m;
    }

    ContextMenu PolicyMenu()
    {
        var m = new ContextMenu();
        m.Items.Add(Ui.Item(L.T("New Policy…"), () => { Show("routing"); Routing.NewPolicy(); }, "Ctrl+N"));
        m.Items.Add(Ui.Item(L.T("Import from File…"), () => { Show("routing"); Routing.ImportPolicy(); }));
        return m;
    }

    ContextMenu OrderMenu()
    {
        var m = new ContextMenu();
        foreach (var (name, title) in ServersPage.Orders)
        {
            var item = new MenuItem { Header = L.T(title), IsChecked = Servers.Order == name };
            item.Click += (_, _) => Servers.Order = name;
            m.Items.Add(item);
        }
        return m;
    }

    public void Select(string tag) => Show(tag);

    /// Brings a page to the front.
    public void Show(string tag)
    {
        IPage next = tag switch
        {
            "routing" => Routing,
            "connections" => Connections,
            "log" => Log,
            "settings" => Settings,
            _ => Servers,
        };
        if (tag is not ("routing" or "connections" or "log" or "settings")) tag = "servers";
        if (items.TryGetValue(tag, out var item) && item.IsChecked != true) item.IsChecked = true;
        if (next == current && pageHost.Child != null) return;
        current.Hidden();
        // A pause holds the list for a look; leaving the page lets it go.
        if (tag != "connections") Connections.Paused = false;
        page = tag;
        current = next;
        pageHost.Child = next.View;
        // The field keeps its text; the page that comes to the front takes it.
        next.Query = search.Text;
        next.Shown();
        Header.View.Visibility = tag == "settings" ? Visibility.Collapsed : Visibility.Visible;
        Redraw(true);
    }

    void Redraw(bool force)
    {
        if (L.Lang != shownLang)
        {
            shownLang = L.Lang;
            force = true;
            Relabel();
        }
        var test = Agent.S.Test;
        Title = test ? "FlClash — " + L.T("Test environment") : "FlClash";
        if (page != "settings") Header.Update();
        current.Update(force);
        pageTitle.Text = current.Title;
        UpdateToolbar();
    }

    public void UpdateToolbar()
    {
        Visibility On(bool on) => on ? Visibility.Visible : Visibility.Collapsed;
        order.Visibility = measure.Visibility = addServers.Visibility = On(page == "servers");
        addPolicy.Visibility = On(page == "routing");
        pause.Visibility = On(page == "connections");
        search.Visibility = On(page is "servers" or "connections" or "log");
        var paused = Connections.Paused;
        ((TextBlock)pause.Content).Text = paused ? "" : "";
        var label = paused ? L.T("Resume") : L.T("Pause");
        pause.ToolTip = label;
        AutomationName(pause, label);
        pause.IsEnabled = Agent.S.Status.Running;
        pageSubtitle.Text = current.Subtitle;
        pageSubtitle.Visibility = current.Subtitle.Length == 0 ? Visibility.Collapsed : Visibility.Visible;
    }

    /// The keys of the macOS menus, with Ctrl for Command.
    void Accelerators()
    {
        // KeyDown, not PreviewKeyDown: a control that takes the keys itself,
        // such as the hot key recorder, gets them first.
        KeyDown += (_, e) =>
        {
            if (Dialogs.Open) return;
            var mods = Keyboard.Modifiers;
            var key = e.Key == Key.System ? e.SystemKey : e.Key;
            Action? action = null;
            if (mods == ModifierKeys.Control)
                action = key switch
                {
                    Key.D1 => () => Select("servers"),
                    Key.D2 => () => Select("routing"),
                    Key.D3 => () => Select("connections"),
                    Key.D4 => () => Select("log"),
                    Key.F => () => { if (page is "routing" or "settings") Select("servers"); search.Focus(); search.SelectAll(); },
                    Key.O => () => { Select("servers"); Servers.ImportFromFile(); },
                    Key.R => () => Agent.UpdateAll(),
                    Key.N => () => { Select("routing"); Routing.NewPolicy(); },
                    Key.T => () => { Select("servers"); Servers.MeasureAll(); },
                    Key.W => Close,
                    _ => null,
                };
            else if (mods == (ModifierKeys.Control | ModifierKeys.Shift) && key == Key.V)
                action = () => { Select("servers"); Servers.ImportFromClipboard(); };
            if (action == null) return;
            e.Handled = true;
            action();
        };
    }

    // The window comes back where it was, at the size it was, in the pixels
    // of the screen.
    void ReadPrefs()
    {
        try
        {
            if (File.Exists(prefsPath)) prefs = Json.Parse<WindowPrefs>(File.ReadAllText(prefsPath)) ?? new();
        }
        catch (Exception) { prefs = new(); }
        prefs.ServerTab ??= "";
        prefs.ServerOrder ??= "added";
    }

    public void SavePrefs()
    {
        try
        {
            if (WindowState == WindowState.Normal && GetWindowRect(Hwnd, out var r))
            {
                prefs.X = r.Left;
                prefs.Y = r.Top;
                prefs.Width = r.Right - r.Left;
                prefs.Height = r.Bottom - r.Top;
            }
            Directory.CreateDirectory(Path.GetDirectoryName(prefsPath)!);
            File.WriteAllText(prefsPath, Json.Write(prefs));
        }
        catch (Exception) { }
    }

    void Place()
    {
        if (prefs.Width <= 0 || prefs.Height <= 0 || prefs.X == int.MinValue) return;
        // The work area of the screen it was on, in pixels.
        var saved = new Rect { Left = prefs.X, Top = prefs.Y, Right = prefs.X + prefs.Width, Bottom = prefs.Y + prefs.Height };
        var monitor = MonitorFromRect(ref saved, 0 /* MONITOR_DEFAULTTONULL */);
        if (monitor == IntPtr.Zero) return;
        var info = new MonitorInfo { Size = Marshal.SizeOf<MonitorInfo>() };
        if (!GetMonitorInfo(monitor, ref info)) return;
        var w = info.Work;
        if (prefs.X < w.Right - 100 && prefs.Y < w.Bottom - 100 && prefs.X + prefs.Width > w.Left + 100 && prefs.Y >= w.Top - 20)
        {
            WindowStartupLocation = WindowStartupLocation.Manual;
            SetWindowPos(Hwnd, IntPtr.Zero, prefs.X, prefs.Y, prefs.Width, prefs.Height, 0x0014 /* SWP_NOZORDER | SWP_NOACTIVATE */);
        }
    }

    /// Comes to the front: the agent let this process take the focus.
    public void Front()
    {
        if (WindowState == WindowState.Minimized) WindowState = WindowState.Normal;
        Show();
        Activate();
        SetForegroundWindow(Hwnd);
    }

    [StructLayout(LayoutKind.Sequential)] struct Rect { public int Left, Top, Right, Bottom; }
    [StructLayout(LayoutKind.Sequential)] struct MonitorInfo { public int Size; public Rect Monitor; public Rect Work; public uint Flags; }

    [DllImport("user32.dll")] static extern bool SetForegroundWindow(IntPtr hwnd);
    [DllImport("user32.dll")] static extern bool GetWindowRect(IntPtr hwnd, out Rect rect);
    [DllImport("user32.dll")] static extern bool SetWindowPos(IntPtr hwnd, IntPtr after, int x, int y, int w, int h, uint flags);
    [DllImport("user32.dll")] static extern IntPtr MonitorFromRect(ref Rect rect, uint flags);
    [DllImport("user32.dll")] static extern bool GetMonitorInfo(IntPtr monitor, ref MonitorInfo info);
}
