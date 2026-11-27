using System.Runtime.InteropServices;
using System.Text.Json;
using Microsoft.UI;
using Microsoft.UI.Windowing;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Input;
using Microsoft.UI.Xaml.Media;
using Microsoft.UI.Xaml.Media.Imaging;
using Windows.System;

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
    readonly NavigationView nav = new();
    readonly Grid body = new();
    readonly Border pageHost = new();
    readonly TextBlock pageTitle = Ui.Text("", 20, Ui.Semibold);
    readonly TextBlock pageSubtitle = Ui.Text("", 12, null, Ui.Secondary);
    readonly TextBlock appTitle = Ui.Text("FlClash", 12);
    readonly TextBlock testTitle = Ui.Text("", 12);
    readonly Dictionary<string, NavigationViewItem> items = new();
    readonly DropDownButton order = new();
    readonly Button measure = new();
    readonly DropDownButton addServers = new();
    readonly DropDownButton addPolicy = new();
    readonly Button pause = new();
    readonly AutoSuggestBox search = new() { Width = 220, VerticalAlignment = VerticalAlignment.Center };
    readonly MenuFlyout orderMenu = new();
    IPage current;
    string page = "servers";
    string shownLang = "";
    WindowPrefs prefs = new();
    static readonly string prefsPath = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "flclash-desktop", "window.json");

    public bool ShowsSettings => page == "settings";
    public WindowPrefs Prefs => prefs;
    public IntPtr Hwnd { get; }

    public MainWindow(Agent agent, string? startPage)
    {
        Agent = agent;
        ReadPrefs();
        Hwnd = WinRT.Interop.WindowNative.GetWindowHandle(this);
        Title = "FlClash";
        SystemBackdrop = new MicaBackdrop();
        ExtendsContentIntoTitleBar = true;

        MakePages();
        current = Servers;

        // The title bar: the app's icon and name, as the system draws others.
        var titleBar = new Grid { Height = 48, Padding = new Thickness(16, 0, 140, 0) };
        var icon = new Image { Width = 16, Height = 16, Source = new BitmapImage(new Uri(Path.Combine(AppContext.BaseDirectory, "FlClash.ico"))) };
        testTitle.Foreground = Ui.Brush(Tint.Orange);
        titleBar.Children.Add(Ui.Row(10, icon, appTitle, testTitle));
        ((StackPanel)titleBar.Children[0]).VerticalAlignment = VerticalAlignment.Center;
        root.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        root.RowDefinitions.Add(new RowDefinition { Height = new GridLength(1, GridUnitType.Star) });
        root.Children.Add(titleBar);
        Grid.SetRow(nav, 1);
        root.Children.Add(nav);

        nav.PaneDisplayMode = NavigationViewPaneDisplayMode.Left;
        nav.IsBackButtonVisible = NavigationViewBackButtonVisible.Collapsed;
        nav.IsPaneToggleButtonVisible = false;
        nav.IsSettingsVisible = true;
        nav.OpenPaneLength = 210;
        foreach (var (tag, glyph) in new[] { ("servers", ""), ("routing", ""), ("connections", ""), ("log", "") })
        {
            var item = new NavigationViewItem { Tag = tag, Icon = new FontIcon { Glyph = glyph } };
            items[tag] = item;
            nav.MenuItems.Add(item);
        }
        nav.SelectionChanged += (_, e) =>
        {
            var tag = e.IsSettingsSelected ? "settings" : (e.SelectedItem as NavigationViewItem)?.Tag as string ?? "servers";
            Show(tag);
        };
        nav.Loaded += (_, _) =>
        {
            if (nav.SettingsItem is NavigationViewItem s) s.Content = L.T("Settings");
            Mark();
        };

        // The toolbar of the page, beside its title.
        Tool(order, "", "Order");
        order.Flyout = orderMenu;
        orderMenu.Opening += (_, _) => FillOrder();
        Tool(measure, "", "Test Latency");
        measure.Click += (_, _) => { Show("servers"); Servers.MeasureAll(); };
        Tool(addServers, "", "Add servers");
        Tool(addPolicy, "", "Add a policy");
        Tool(pause, "", "Pause");
        pause.Click += (_, _) => { Connections.Paused = !Connections.Paused; UpdateToolbar(); };
        search.QueryIcon = new SymbolIcon(Symbol.Find);
        search.TextChanged += (_, _) => current.Query = search.Text;
        var tools = Ui.Row(6, order, measure, addServers, addPolicy, pause, search);
        tools.VerticalAlignment = VerticalAlignment.Center;

        var titles = Ui.Column(0, pageTitle, pageSubtitle);
        titles.VerticalAlignment = VerticalAlignment.Center;
        var toolbar = new Grid { Margin = new Thickness(20, 6, 16, 6), MinHeight = 48 };
        toolbar.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        toolbar.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        toolbar.Children.Add(titles);
        Grid.SetColumn(tools, 1);
        toolbar.Children.Add(tools);

        body.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        body.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        body.RowDefinitions.Add(new RowDefinition { Height = new GridLength(1, GridUnitType.Star) });
        body.Children.Add(toolbar);
        Grid.SetRow(Header.View, 1);
        body.Children.Add(Header.View);
        Grid.SetRow(pageHost, 2);
        body.Children.Add(pageHost);
        nav.Content = body;

        Accelerators();
        // --theme light|dark draws the window in a theme other than the system's, to look at it.
        var argv = Environment.GetCommandLineArgs();
        int at = Array.IndexOf(argv, "--theme");
        if (at >= 0 && at + 1 < argv.Length) root.RequestedTheme = argv[at + 1] == "light" ? ElementTheme.Light : ElementTheme.Dark;
        Content = root;
        SetTitleBar(titleBar);

        root.ActualThemeChanged += (_, _) => Themed();
        root.Loaded += (_, _) => Themed();
        Agent.Changed += () => Redraw(false);
        Agent.Activate += p => { if (!string.IsNullOrEmpty(p)) Show(p); Front(); };
        Agent.Lost += Close;

        Activated += (_, e) =>
        {
            // The command line may have changed the lists while the window was aside.
            if (e.WindowActivationState != WindowActivationState.Deactivated) Agent.Refresh();
        };
        Closed += (_, _) => { SavePrefs(); current.Hidden(); Agent.Close(); };
        Place();
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
    void Themed()
    {
        bool dark = root.ActualTheme == ElementTheme.Dark;
        if (dark == Ui.Dark && pageHost.Child != null) { Redraw(true); return; }
        Ui.Dark = dark;
        current.Hidden();
        body.Children.Remove(Header.View);
        MakePages();
        Grid.SetRow(Header.View, 1);
        body.Children.Add(Header.View);
        pageSubtitle.Foreground = Ui.Secondary;
        testTitle.Foreground = Ui.Brush(Tint.Orange);
        pageHost.Child = null;
        current = Servers;
        var tag = page;
        page = "";
        Show(tag);
    }

    void Tool(Button b, string glyph, string label)
    {
        b.Content = new FontIcon { Glyph = glyph, FontSize = 16 };
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
        if (nav.SettingsItem is NavigationViewItem s) s.Content = L.T("Settings");
        foreach (var b in new Button[] { order, measure, addServers, addPolicy })
        {
            var text = L.T((string)b.Tag);
            ToolTipService.SetToolTip(b, text);
            Microsoft.UI.Xaml.Automation.AutomationProperties.SetName(b, text);
        }
        search.PlaceholderText = L.T("Search");
        var addMenu = new MenuFlyout();
        addMenu.Items.Add(Item(L.T("Import from Clipboard"), () => { Show("servers"); Servers.ImportFromClipboard(); }, "Ctrl+Shift+V"));
        addMenu.Items.Add(Item(L.T("Import from File…"), () => { Show("servers"); Servers.ImportFromFile(); }, "Ctrl+O"));
        addMenu.Items.Add(new MenuFlyoutSeparator());
        addMenu.Items.Add(Item(L.T("Add a Subscription…"), () => { Show("servers"); Servers.AddSubscription(); }));
        addMenu.Items.Add(Item(L.T("Update Subscriptions"), () => Agent.UpdateAll(), "Ctrl+R"));
        addServers.Flyout = addMenu;
        var policyMenu = new MenuFlyout();
        policyMenu.Items.Add(Item(L.T("New Policy…"), () => { Show("routing"); Routing.NewPolicy(); }, "Ctrl+N"));
        policyMenu.Items.Add(Item(L.T("Import from File…"), () => { Show("routing"); Routing.ImportPolicy(); }));
        addPolicy.Flyout = policyMenu;
    }

    public static MenuFlyoutItem Item(string text, Action action, string? keys = null, bool enabled = true)
    {
        var item = new MenuFlyoutItem { Text = text, IsEnabled = enabled };
        if (keys != null) item.KeyboardAcceleratorTextOverride = keys;
        item.Click += (_, _) => action();
        return item;
    }

    void FillOrder()
    {
        orderMenu.Items.Clear();
        foreach (var (name, title) in ServersPage.Orders)
        {
            var item = new RadioMenuFlyoutItem { Text = L.T(title), GroupName = "order", IsChecked = Servers.Order == name };
            item.Click += (_, _) => Servers.Order = name;
            orderMenu.Items.Add(item);
        }
    }

    public void Select(string tag) => Show(tag);

    /// The sidebar marks the page in front, once it is there to mark.
    void Mark()
    {
        if (!nav.IsLoaded) return;
        object? want = page == "settings" ? nav.SettingsItem : items.GetValueOrDefault(page);
        if (want != null && nav.SelectedItem != want) nav.SelectedItem = want;
    }

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
        if (next == current && pageHost.Child != null) return;
        current.Hidden();
        // A pause holds the list for a look; leaving the page lets it go.
        if (tag != "connections") Connections.Paused = false;
        page = tag;
        current = next;
        Mark();
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
        testTitle.Text = test ? "— " + L.T("Test environment") : "";
        Title = test ? "FlClash — " + L.T("Test environment") : "FlClash";
        if (page != "settings") Header.Update();
        current.Update(force);
        pageTitle.Text = current.Title;
        pageSubtitle.Text = current.Subtitle;
        pageSubtitle.Visibility = current.Subtitle.Length == 0 ? Visibility.Collapsed : Visibility.Visible;
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
        ((FontIcon)pause.Content).Glyph = paused ? "" : "";
        var label = paused ? L.T("Resume") : L.T("Pause");
        ToolTipService.SetToolTip(pause, label);
        Microsoft.UI.Xaml.Automation.AutomationProperties.SetName(pause, label);
        pause.IsEnabled = Agent.S.Status.Running;
        pageSubtitle.Text = current.Subtitle;
        pageSubtitle.Visibility = current.Subtitle.Length == 0 ? Visibility.Collapsed : Visibility.Visible;
    }

    /// The keys of the macOS menus, with Ctrl for Command.
    void Accelerators()
    {
        void Key(VirtualKey key, VirtualKeyModifiers mods, Action action)
        {
            var a = new KeyboardAccelerator { Key = key, Modifiers = mods };
            a.Invoked += (_, e) => { e.Handled = true; action(); };
            root.KeyboardAccelerators.Add(a);
        }
        var ctrl = VirtualKeyModifiers.Control;
        Key(VirtualKey.Number1, ctrl, () => Select("servers"));
        Key(VirtualKey.Number2, ctrl, () => Select("routing"));
        Key(VirtualKey.Number3, ctrl, () => Select("connections"));
        Key(VirtualKey.Number4, ctrl, () => Select("log"));
        Key(VirtualKey.F, ctrl, () => { if (page is "routing" or "settings") Select("servers"); search.Focus(FocusState.Keyboard); });
        Key(VirtualKey.V, ctrl | VirtualKeyModifiers.Shift, () => { Select("servers"); Servers.ImportFromClipboard(); });
        Key(VirtualKey.O, ctrl, () => { Select("servers"); Servers.ImportFromFile(); });
        Key(VirtualKey.R, ctrl, () => Agent.UpdateAll());
        Key(VirtualKey.N, ctrl, () => { Select("routing"); Routing.NewPolicy(); });
        Key(VirtualKey.T, ctrl, () => { Select("servers"); Servers.MeasureAll(); });
        Key(VirtualKey.W, ctrl, Close);
    }

    // The window comes back where it was, at the size it was.
    void ReadPrefs()
    {
        try
        {
            if (File.Exists(prefsPath)) prefs = JsonSerializer.Deserialize(File.ReadAllText(prefsPath), Json.Default.WindowPrefs) ?? new();
        }
        catch (Exception) { prefs = new(); }
    }

    public void SavePrefs()
    {
        try
        {
            if (AppWindow.Presenter is OverlappedPresenter { State: OverlappedPresenterState.Restored })
            {
                prefs.X = AppWindow.Position.X;
                prefs.Y = AppWindow.Position.Y;
                prefs.Width = AppWindow.Size.Width;
                prefs.Height = AppWindow.Size.Height;
            }
            Directory.CreateDirectory(Path.GetDirectoryName(prefsPath)!);
            File.WriteAllText(prefsPath, JsonSerializer.Serialize(prefs, Json.Default.WindowPrefs));
        }
        catch (Exception) { }
    }

    void Place()
    {
        double scale = GetDpiForWindow(Hwnd) / 96.0;
        if (AppWindow.Presenter is OverlappedPresenter p)
        {
            p.PreferredMinimumWidth = (int)(800 * scale);
            p.PreferredMinimumHeight = (int)(520 * scale);
        }
        try { AppWindow.SetIcon(Path.Combine(AppContext.BaseDirectory, "FlClash.ico")); } catch (Exception) { }
        var area = DisplayArea.GetFromWindowId(AppWindow.Id, DisplayAreaFallback.Primary).WorkArea;
        if (prefs.Width > 0 && prefs.Height > 0 && prefs.X != int.MinValue
            && prefs.X < area.X + area.Width - 100 && prefs.Y < area.Y + area.Height - 100 && prefs.X + prefs.Width > area.X + 100 && prefs.Y >= area.Y - 20)
        {
            AppWindow.MoveAndResize(new Windows.Graphics.RectInt32(prefs.X, prefs.Y, prefs.Width, prefs.Height));
            return;
        }
        int w = (int)(980 * scale), h = (int)(660 * scale);
        AppWindow.MoveAndResize(new Windows.Graphics.RectInt32(area.X + (area.Width - w) / 2, area.Y + (area.Height - h) / 2, w, h));
    }

    /// Comes to the front: the agent let this process take the focus.
    public void Front()
    {
        if (IsIconic(Hwnd)) ShowWindow(Hwnd, 9);
        AppWindow.Show();
        SetForegroundWindow(Hwnd);
        Activate();
    }

    [DllImport("user32.dll")] static extern uint GetDpiForWindow(IntPtr hwnd);
    [DllImport("user32.dll")] static extern bool SetForegroundWindow(IntPtr hwnd);
    [DllImport("user32.dll")] static extern bool IsIconic(IntPtr hwnd);
    [DllImport("user32.dll")] static extern bool ShowWindow(IntPtr hwnd, int cmd);
}
