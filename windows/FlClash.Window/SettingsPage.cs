using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;

namespace FlClash;

/// The few things that are set once: protection, the local port, and what
/// happens at start. Groups of rows, a name on the left and its control on
/// the right, as the system's own settings are laid out.
public sealed class SettingsPage : IPage
{
    readonly MainWindow window;
    Agent A => window.Agent;
    readonly ScrollViewer scroll = new() { VerticalScrollBarVisibility = ScrollBarVisibility.Auto };
    readonly StackPanel column = new() { Margin = new Thickness(24, 4, 24, 24), MaxWidth = 760, HorizontalAlignment = HorizontalAlignment.Stretch };
    // Made anew with the rows: a control is not moved from one row to another.
    CheckBox protect = null!, login = null!, autoConnect = null!, lan = null!;
    TextBox port = null!, auth = null!, dns = null!;
    Button hotKey = null!, removeHelper = null!;
    TextBlock version = null!;
    bool filling, recording, built, saying, removing;
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
        // The number is checked here, not clamped: a wrong one is said, not changed.
        port = Ui.Field("", "", 110);
        auth = Ui.Field(L.T("user:password"), "", 190);
        dns = Ui.Field("", "", 250);
        hotKey = new() { MinWidth = 150 };
        removeHelper = new() { Content = L.T("Remove") };
        removeHelper.Click += async (_, _) =>
        {
            removing = true;
            Update(false);
            var reply = await A.RemoveHelper();
            removing = false;
            Update(false);
            if (!string.IsNullOrEmpty(reply.Error)) await Ui.Say(L.T("The helper was not removed"), reply.Error!);
        };
        version = Ui.Text("", 12, null, Ui.Secondary);
        protect.Click += (_, _) => { if (!filling) A.SetProtect(protect.IsChecked == true); };
        autoConnect.Click += (_, _) => { if (!filling) A.SetAutoConnect(autoConnect.IsChecked == true); };
        lan.Click += (_, _) => { if (!filling) A.SetLan(lan.IsChecked == true); };
        login.Click += async (_, _) =>
        {
            if (filling) return;
            var reply = await A.SetLogin(login.IsChecked == true);
            if (!string.IsNullOrEmpty(reply.Error)) await Ui.Say(L.T("The setting was not changed"), reply.Error!);
        };
        Commit(port, async value =>
        {
            if (!int.TryParse(value, out var v) || v < 1024 || v > 65535)
            {
                // The port in use stays, as on macOS.
                filling = true;
                port.Text = A.S.View.Port.ToString();
                filling = false;
                await Ui.Say(L.T("The setting was not changed"), L.T("The local port is a whole number from 1024 to 65535."));
                return;
            }
            A.SetPort(v);
        });
        Commit(auth, A.SetAuth);
        Commit(dns, A.SetDns);
        hotKey.Click += (_, _) => { recording = true; hotKey.Content = L.T("Press the keys…"); };
        hotKey.PreviewKeyDown += Record;
        hotKey.LostKeyboardFocus += (_, _) => { recording = false; ShowHotKey(); };
    }

    static CheckBox Switch() => new() { Style = Ui.Style("Switch"), HorizontalAlignment = HorizontalAlignment.Right };

    /// A field is taken when Enter is pressed or the focus leaves it: once for
    /// what was typed, whichever comes first. What the engine then says is put
    /// in the field again, and the next value typed is taken anew.
    void Commit(TextBox box, Action<string> action)
    {
        string? sent = null;
        void Take()
        {
            var value = box.Text.Trim();
            if (value == sent) return;
            sent = value;
            action(value);
        }
        box.KeyDown += (_, e) => { if (e.Key == Key.Enter) { Take(); e.Handled = true; } };
        box.LostKeyboardFocus += (_, _) => Take();
        box.TextChanged += (_, _) => { if (filling) sent = null; };
    }

    void Build()
    {
        column.Children.Clear();
        recording = false;
        Make();
        void Add(UIElement e) => Ui.Add(column, e, 14);
        Add(Group(
            Row(L.T("Let nothing go past FlClash"),
                L.T("In TUN mode nothing leaves this computer past the tunnel, IPv6 included. If the connection breaks, the internet stays off until it is back or you turn it off; in System Proxy mode that holds for the apps that follow the system proxy. The local network always works."),
                protect)));
        Add(Group(
            Row(L.T("Local port"), L.T("HTTP and SOCKS on 127.0.0.1"), port),
            Row(L.T("Let the local network use it"), L.T("Other devices at home can point at this computer's address and the port."), lan),
            Row(L.T("Login for other devices"), L.T("Empty asks for none. This computer is never asked."), auth)));
        Add(Group(Row(L.T("DNS in TUN mode"), L.T("Resolves names for what goes through the tunnel."), dns)));
        Add(Group(Row(L.T("Shortcut to connect"), L.T("Works from any app."), hotKey)));
        Add(Group(
            Row(L.T("Launch at Login"), null, login),
            Row(L.T("Connect when FlClash starts"), null, autoConnect)));
        var saveBackup = new Button { Content = L.T("Save a Backup…") };
        var restoreBackup = new Button { Content = L.T("Restore from a Backup…") };
        saveBackup.Click += (_, _) => SaveBackup();
        restoreBackup.Click += (_, _) => RestoreBackup();
        Add(Group(Row(L.T("Backup"),
            L.T("The backup holds the keys of your servers and the addresses of your subscriptions. Keep it where others cannot read it."),
            Ui.Row(8, saveBackup, restoreBackup))));
        Add(Group(Row(L.T("System helper"),
            L.T("Runs TUN with administrator rights and stays on the computer when FlClash is gone. Remove it from a computer you leave: turning TUN on installs it again."),
            removeHelper)));
        Add(version);
        built = true;
    }

    /// One row: what it is, a note under it when it needs one, and its control.
    static FrameworkElement Row(string title, string? note, FrameworkElement control)
    {
        var words = Ui.Column(3, Ui.Text(title, 14, null, null, wrap: true));
        if (note != null) Ui.Add(words, Ui.Text(note, 12, null, Ui.Secondary, wrap: true), 3);
        words.VerticalAlignment = VerticalAlignment.Center;
        var line = Ui.Columns(-1, Ui.Auto);
        line.MinHeight = 40;
        line.Children.Add(words);
        control.VerticalAlignment = VerticalAlignment.Center;
        Ui.Place(line, control, 1, 16);
        return new Border { Padding = new Thickness(16, 12, 16, 12), MinHeight = 64, Child = line };
    }

    /// Rows that belong together, on one rounded panel with lines between them.
    static FrameworkElement Group(params FrameworkElement[] rows)
    {
        var stack = new StackPanel();
        for (int i = 0; i < rows.Length; i++)
        {
            if (i > 0) stack.Children.Add(new Border { Height = 1, Background = Ui.B("Divider"), Margin = new Thickness(16, 0, 16, 0) });
            stack.Children.Add(rows[i]);
        }
        return new Border
        {
            CornerRadius = new CornerRadius(8),
            BorderThickness = new Thickness(1),
            BorderBrush = Ui.B("CardStroke"),
            Background = Ui.B("Card"),
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
        protect.IsChecked = s.View.Protect;
        protect.IsEnabled = !s.Busy;
        login.IsChecked = A.App.Login;
        autoConnect.IsChecked = s.View.AutoConnect;
        // Not while the field is being typed in.
        if (!port.IsKeyboardFocusWithin) port.Text = s.View.Port.ToString();
        port.IsEnabled = lan.IsEnabled = dns.IsEnabled = !s.Busy;
        lan.IsChecked = s.View.Lan;
        if (!auth.IsKeyboardFocusWithin) auth.Text = s.View.Auth;
        auth.IsEnabled = s.View.Lan && !s.Busy;
        if (!dns.IsKeyboardFocusWithin) dns.Text = s.View.Dns;
        removeHelper.IsEnabled = s.Status.Installed && !s.Status.Running && !s.Status.Blocked && !s.Busy && !removing;
        if (!recording) ShowHotKey();
        version.Text = string.IsNullOrEmpty(A.App.Version) ? "FlClash" : "FlClash " + A.App.Version;
        filling = false;
        // A value the engine refused is said once, by the window.
        if (!string.IsNullOrEmpty(s.Problem) && !saying) Problem(s.Problem!);
    }

    async void Problem(string text)
    {
        saying = true;
        A.Dismiss();
        await Ui.Say(L.T("The setting was not changed"), text);
        saying = false;
    }

    void ShowHotKey() => hotKey.Content = !string.IsNullOrEmpty(A.App.HotKey.Title) ? A.App.HotKey.Title : L.T("Record Shortcut");

    /// Escape leaves the shortcut as it was, Delete clears it, a key with
    /// Ctrl, Alt or Win (or a function key alone) becomes it.
    async void Record(object sender, KeyEventArgs e)
    {
        if (!recording) return;
        var key = (uint)KeyInterop.VirtualKeyFromKey(e.Key == Key.System ? e.SystemKey : e.Key);
        e.Handled = true;
        // Ctrl, Shift, Alt and Win on their own wait for the key they go with.
        if (key is 0x10 or 0x11 or 0x12 or 0x5B or 0x5C or >= 0xA0 and <= 0xA5) return;
        HotKey? combo = null;
        if (key == 0x1B) { }
        else if (key is 0x2E or 0x08) combo = new HotKey();
        else
        {
            uint mods = 0;
            var parts = new List<string>();
            var held = Keyboard.Modifiers;
            if (held.HasFlag(ModifierKeys.Control)) { mods |= 2; parts.Add("Ctrl"); }
            if (held.HasFlag(ModifierKeys.Alt)) { mods |= 1; parts.Add("Alt"); }
            if (held.HasFlag(ModifierKeys.Shift)) { mods |= 4; parts.Add("Shift"); }
            if (Keyboard.IsKeyDown(Key.LWin) || Keyboard.IsKeyDown(Key.RWin)) { mods |= 8; parts.Add("Win"); }
            bool function = key >= 0x70 && key <= 0x87;
            var name = KeyName(key);
            if (name == null || ((mods & ~4u) == 0 && !function)) return;
            parts.Add(name);
            combo = new HotKey { Key = key, Modifiers = mods, Title = string.Join("+", parts) };
        }
        recording = false;
        if (combo != null)
        {
            var reply = await A.SetHotKey(combo);
            if (!string.IsNullOrEmpty(reply.Error)) await Ui.Say(L.T("The setting was not changed"), reply.Error!);
        }
        ShowHotKey();
    }

    /// The name of a key as the shortcut is written, by its virtual-key code.
    static string? KeyName(uint key)
    {
        if (key >= 0x41 && key <= 0x5A) return ((char)key).ToString();
        if (key >= 0x30 && key <= 0x39) return ((char)key).ToString();
        if (key >= 0x70 && key <= 0x87) return "F" + (key - 0x70 + 1);
        return key switch
        {
            0x20 => "Space",
            0x0D => "Enter",
            0x09 => "Tab",
            0x24 => "Home",
            0x23 => "End",
            0x2D => "Insert",
            0x21 => "PageUp",
            0x22 => "PageDown",
            0x26 => "↑",
            0x28 => "↓",
            0x25 => "←",
            0x27 => "→",
            _ => null,
        };
    }

    async void SaveBackup()
    {
        if (Files.PickSave(window, "FlClash backup", "JSON", ".json") is not { } path) return;
        var reply = await A.Do(new Request { Op = "backupWrite", Name = path }, quiet: true);
        if (!string.IsNullOrEmpty(reply.Error)) await Ui.Say(L.T("The backup was not saved"), reply.Error!);
    }

    async void RestoreBackup()
    {
        if (Files.PickOpen(window, ".json") is not { } path) return;
        if (!await Ui.Ask(L.T("Restore from a Backup…"),
            L.T("Everything set up now — servers, subscriptions, policies, settings — is replaced by what the backup holds."), L.T("Restore"))) return;
        var reply = await A.Do(new Request { Op = "backupRead", Name = path }, quiet: true);
        if (!string.IsNullOrEmpty(reply.Error)) await Ui.Say(L.T("The backup was not restored"), reply.Error!);
    }
}
