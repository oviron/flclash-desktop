using Microsoft.UI.Input;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Input;
using Microsoft.UI.Xaml.Media;
using Windows.Storage.Pickers;
using Windows.System;
using Windows.UI.Core;

namespace FlClash;

/// The few things that are set once: protection, the local port, and what
/// happens at start. Groups of rows, a name on the left and its control on
/// the right, as the system's own settings are laid out.
public sealed class SettingsPage : IPage
{
    readonly MainWindow window;
    Agent A => window.Agent;
    readonly ScrollViewer scroll = new();
    readonly StackPanel column = new() { Spacing = 14, Padding = new Thickness(24, 4, 24, 24), MaxWidth = 760, HorizontalAlignment = HorizontalAlignment.Stretch };
    // Made anew with the rows: a control is not moved from one row to another.
    ToggleSwitch protect = null!, login = null!, autoConnect = null!, lan = null!;
    NumberBox port = null!;
    TextBox auth = null!, dns = null!;
    Button hotKey = null!;
    TextBlock version = null!;
    bool filling, recording, built, saying;
    string shownLang = "";

    public SettingsPage(MainWindow window)
    {
        this.window = window;
        scroll.Content = column;
    }

    void Make()
    {
        protect = Switch();
        login = Switch();
        autoConnect = Switch();
        lan = Switch();
        port = new() { Minimum = 1024, Maximum = 65535, Width = 110, SpinButtonPlacementMode = NumberBoxSpinButtonPlacementMode.Hidden };
        auth = new() { Width = 190, PlaceholderText = L.T("user:password") };
        dns = new() { Width = 250 };
        hotKey = new() { MinWidth = 150 };
        version = Ui.Text("", 12, null, Ui.Secondary);
        protect.Toggled += (_, _) => { if (!filling) A.SetProtect(protect.IsOn); };
        autoConnect.Toggled += (_, _) => { if (!filling) A.SetAutoConnect(autoConnect.IsOn); };
        lan.Toggled += (_, _) => { if (!filling) A.SetLan(lan.IsOn); };
        login.Toggled += async (_, _) =>
        {
            if (filling) return;
            var reply = await A.SetLogin(login.IsOn);
            if (!string.IsNullOrEmpty(reply.Error)) await Ui.Say(window.Content.XamlRoot, L.T("The setting was not changed"), reply.Error);
        };
        port.ValueChanged += (_, e) =>
        {
            if (filling || double.IsNaN(e.NewValue)) return;
            A.SetPort((int)e.NewValue);
        };
        Commit(auth, () => A.SetAuth(auth.Text.Trim()));
        Commit(dns, () => A.SetDns(dns.Text.Trim()));
        hotKey.Click += (_, _) => { recording = true; hotKey.Content = L.T("Press the keys…"); };
        hotKey.PreviewKeyDown += Record;
        hotKey.LostFocus += (_, _) => { recording = false; ShowHotKey(); };
    }

    static ToggleSwitch Switch() => new() { OnContent = "", OffContent = "", MinWidth = 0, HorizontalAlignment = HorizontalAlignment.Right };

    /// A field is taken when Enter is pressed or the focus leaves it.
    static void Commit(TextBox box, Action action)
    {
        box.KeyDown += (_, e) => { if (e.Key == VirtualKey.Enter) { action(); e.Handled = true; } };
        box.LostFocus += (_, _) => action();
    }

    void Build()
    {
        column.Children.Clear();
        recording = false;
        Make();
        column.Children.Add(Group(
            Row(L.T("Let nothing go past FlClash"),
                L.T("In TUN mode nothing leaves this computer past the tunnel, IPv6 included. If the connection breaks, the internet stays off until it is back or you turn it off; in System Proxy mode that holds for the apps that follow the system proxy. The local network always works."),
                protect)));
        column.Children.Add(Group(
            Row(L.T("Local port"), L.T("HTTP and SOCKS on 127.0.0.1"), port),
            Row(L.T("Let the local network use it"), L.T("Other devices at home can point at this computer's address and the port."), lan),
            Row(L.T("Login for other devices"), L.T("Empty asks for none. This computer is never asked."), auth)));
        column.Children.Add(Group(Row(L.T("DNS in TUN mode"), L.T("Resolves names for what goes through the tunnel."), dns)));
        column.Children.Add(Group(Row(L.T("Shortcut to connect"), L.T("Works from any app."), hotKey)));
        column.Children.Add(Group(
            Row(L.T("Launch at Login"), null, login),
            Row(L.T("Connect when FlClash starts"), null, autoConnect)));
        var saveBackup = new Button { Content = L.T("Save a Backup…") };
        var restoreBackup = new Button { Content = L.T("Restore from a Backup…") };
        saveBackup.Click += (_, _) => SaveBackup();
        restoreBackup.Click += (_, _) => RestoreBackup();
        column.Children.Add(Group(Row(L.T("Backup"),
            L.T("The backup holds the keys of your servers and the addresses of your subscriptions. Keep it where others cannot read it."),
            Ui.Row(8, saveBackup, restoreBackup))));
        column.Children.Add(version);
        built = true;
    }

    /// One row: what it is, a note under it when it needs one, and its control.
    static FrameworkElement Row(string title, string? note, FrameworkElement control)
    {
        var words = Ui.Column(3, Ui.Text(title, 14, null, null, wrap: true));
        if (note != null) words.Children.Add(Ui.Text(note, 12, null, Ui.Secondary, wrap: true));
        words.VerticalAlignment = VerticalAlignment.Center;
        var line = new Grid { ColumnSpacing = 16, Padding = new Thickness(16, 12, 16, 12), MinHeight = 64 };
        line.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        line.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        line.Children.Add(words);
        control.VerticalAlignment = VerticalAlignment.Center;
        Grid.SetColumn(control, 1);
        line.Children.Add(control);
        return line;
    }

    /// Rows that belong together, on one rounded panel with lines between them.
    static FrameworkElement Group(params FrameworkElement[] rows)
    {
        var stack = new StackPanel();
        for (int i = 0; i < rows.Length; i++)
        {
            if (i > 0) stack.Children.Add(new Border { Height = 1, Background = Ui.Res("DividerStrokeColorDefaultBrush"), Margin = new Thickness(16, 0, 16, 0) });
            stack.Children.Add(rows[i]);
        }
        return new Border
        {
            CornerRadius = new CornerRadius(8),
            BorderThickness = new Thickness(1),
            BorderBrush = Ui.Res("CardStrokeColorDefaultBrush"),
            Background = Ui.Res("CardBackgroundFillColorDefaultBrush"),
            Child = stack,
        };
    }

    public FrameworkElement View => scroll;
    public string Title => L.T("Settings");
    public string Subtitle => "";
    public string Query { set { } }
    public void Shown() { }
    public void Hidden() { recording = false; }

    public void Update(bool force)
    {
        if (!built || shownLang != L.Lang) { shownLang = L.Lang; Build(); }
        var s = A.S;
        filling = true;
        protect.IsOn = s.View.Protect;
        protect.IsEnabled = !s.Busy;
        login.IsOn = A.App.Login;
        autoConnect.IsOn = s.View.AutoConnect;
        // Not while the field is being typed in.
        if (port.FocusState == FocusState.Unfocused) port.Value = s.View.Port;
        port.IsEnabled = lan.IsEnabled = dns.IsEnabled = !s.Busy;
        lan.IsOn = s.View.Lan;
        if (auth.FocusState == FocusState.Unfocused) auth.Text = s.View.Auth;
        auth.IsEnabled = s.View.Lan && !s.Busy;
        if (dns.FocusState == FocusState.Unfocused) dns.Text = s.View.Dns;
        if (!recording) ShowHotKey();
        version.Text = string.IsNullOrEmpty(A.App.Version) ? "FlClash" : "FlClash " + A.App.Version;
        filling = false;
        // A value the engine refused is said once, by the window.
        if (!string.IsNullOrEmpty(s.Problem) && !saying && window.Content.XamlRoot != null) Problem(s.Problem);
    }

    async void Problem(string text)
    {
        saying = true;
        A.Dismiss();
        await Ui.Say(window.Content.XamlRoot, L.T("The setting was not changed"), text);
        saying = false;
    }

    void ShowHotKey() => hotKey.Content = A.App.HotKey.Title.Length > 0 ? A.App.HotKey.Title : L.T("Record Shortcut");

    static bool Down(VirtualKey key) => InputKeyboardSource.GetKeyStateForCurrentThread(key).HasFlag(CoreVirtualKeyStates.Down);

    /// Escape leaves the shortcut as it was, Delete clears it, a key with
    /// Ctrl, Alt or Win (or a function key alone) becomes it.
    async void Record(object sender, KeyRoutedEventArgs e)
    {
        if (!recording) return;
        var key = e.Key;
        e.Handled = true;
        if (key is VirtualKey.Control or VirtualKey.Menu or VirtualKey.Shift or VirtualKey.LeftWindows or VirtualKey.RightWindows
            or VirtualKey.LeftControl or VirtualKey.RightControl or VirtualKey.LeftMenu or VirtualKey.RightMenu or VirtualKey.LeftShift or VirtualKey.RightShift) return;
        HotKey? combo = null;
        if (key == VirtualKey.Escape) { }
        else if (key is VirtualKey.Delete or VirtualKey.Back) combo = new HotKey();
        else
        {
            uint mods = 0;
            var parts = new List<string>();
            if (Down(VirtualKey.Control)) { mods |= 2; parts.Add("Ctrl"); }
            if (Down(VirtualKey.Menu)) { mods |= 1; parts.Add("Alt"); }
            if (Down(VirtualKey.Shift)) { mods |= 4; parts.Add("Shift"); }
            if (Down(VirtualKey.LeftWindows) || Down(VirtualKey.RightWindows)) { mods |= 8; parts.Add("Win"); }
            bool function = key >= VirtualKey.F1 && key <= VirtualKey.F24;
            var name = KeyName(key);
            if (name == null || ((mods & ~4u) == 0 && !function)) return;
            parts.Add(name);
            combo = new HotKey { Key = (uint)key, Modifiers = mods, Title = string.Join("+", parts) };
        }
        recording = false;
        if (combo != null)
        {
            var reply = await A.SetHotKey(combo);
            if (!string.IsNullOrEmpty(reply.Error)) await Ui.Say(window.Content.XamlRoot, L.T("The setting was not changed"), reply.Error);
        }
        ShowHotKey();
    }

    static string? KeyName(VirtualKey key)
    {
        if (key >= VirtualKey.A && key <= VirtualKey.Z) return ((char)key).ToString();
        if (key >= VirtualKey.Number0 && key <= VirtualKey.Number9) return ((char)key).ToString();
        if (key >= VirtualKey.F1 && key <= VirtualKey.F24) return "F" + (key - VirtualKey.F1 + 1);
        return key switch
        {
            VirtualKey.Space => "Space",
            VirtualKey.Enter => "Enter",
            VirtualKey.Tab => "Tab",
            VirtualKey.Home => "Home",
            VirtualKey.End => "End",
            VirtualKey.Insert => "Insert",
            VirtualKey.PageUp => "PageUp",
            VirtualKey.PageDown => "PageDown",
            VirtualKey.Up => "↑",
            VirtualKey.Down => "↓",
            VirtualKey.Left => "←",
            VirtualKey.Right => "→",
            _ => null,
        };
    }

    async void SaveBackup()
    {
        var picker = new FileSavePicker { SuggestedFileName = "FlClash backup" };
        WinRT.Interop.InitializeWithWindow.Initialize(picker, window.Hwnd);
        picker.FileTypeChoices.Add("JSON", new List<string> { ".json" });
        var file = await picker.PickSaveFileAsync();
        if (file == null) return;
        var reply = await A.Do(new Request { Op = "backupWrite", Name = file.Path }, quiet: true);
        if (!string.IsNullOrEmpty(reply.Error)) await Ui.Say(window.Content.XamlRoot, L.T("The backup was not saved"), reply.Error);
    }

    async void RestoreBackup()
    {
        var picker = new FileOpenPicker();
        WinRT.Interop.InitializeWithWindow.Initialize(picker, window.Hwnd);
        picker.FileTypeFilter.Add(".json");
        var file = await picker.PickSingleFileAsync();
        if (file == null) return;
        if (!await Ui.Ask(window.Content.XamlRoot, L.T("Restore from a Backup…"),
            L.T("Everything set up now — servers, subscriptions, policies, settings — is replaced by what the backup holds."), L.T("Restore"))) return;
        var reply = await A.Do(new Request { Op = "backupRead", Name = file.Path }, quiet: true);
        if (!string.IsNullOrEmpty(reply.Error)) await Ui.Say(window.Content.XamlRoot, L.T("The backup was not restored"), reply.Error);
    }
}
