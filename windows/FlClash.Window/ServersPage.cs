using System.Text;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;
using System.Windows.Media;

namespace FlClash;

/// The servers: one list, filled by importing, pasting or dropping links.
public sealed class ServersPage : IPage
{
    readonly MainWindow window;
    Agent A => window.Agent;
    readonly Grid root = new() { AllowDrop = true, Background = Brushes.Transparent };
    readonly ListBox list = new() { SelectionMode = SelectionMode.Extended, Padding = new Thickness(8, 0, 8, 8) };
    readonly StackPanel tabs = new() { Orientation = Orientation.Horizontal, HorizontalAlignment = HorizontalAlignment.Center };
    readonly Ui.Empty empty = new("");
    readonly Border dropRing = new() { CornerRadius = new CornerRadius(10), BorderThickness = new Thickness(2), Margin = new Thickness(6), IsHitTestVisible = false, Visibility = Visibility.Collapsed };
    List<string> tabGroups = new();
    string tabsLang = "";
    string query = "";
    string key = "";
    bool fillingTabs, fixing;

    public static readonly (string, string)[] Orders = { ("added", "As Added"), ("name", "By Name"), ("latency", "By Latency") };

    public ServersPage(MainWindow window)
    {
        this.window = window;
        root.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        root.RowDefinitions.Add(new RowDefinition { Height = new GridLength(1, GridUnitType.Star) });
        root.Children.Add(tabs);
        Grid.SetRow(list, 1);
        root.Children.Add(list);
        Grid.SetRow(empty.View, 1);
        root.Children.Add(empty.View);
        Grid.SetRowSpan(dropRing, 2);
        root.Children.Add(dropRing);
        tabs.Margin = new Thickness(16, 0, 16, 4);
        list.SelectionChanged += (_, e) =>
        {
            if (fixing) return;
            // Headings are not chosen.
            fixing = true;
            foreach (var added in e.AddedItems)
                if (added is ListBoxItem { Tag: string } heading) list.SelectedItems.Remove(heading);
            fixing = false;
        };
        list.KeyDown += (_, e) =>
        {
            if (e.Key == Key.Delete) { Delete(null); e.Handled = true; }
            else if (e.Key == Key.Enter && Selected().Count == 1) { Connect(Selected()[0]); e.Handled = true; }
        };
        empty.Button.Click += (_, _) => ImportFromClipboard();

        root.DragOver += (_, e) =>
        {
            if (e.Data.GetDataPresent(DataFormats.FileDrop) || e.Data.GetDataPresent(DataFormats.UnicodeText))
            {
                e.Effects = DragDropEffects.Copy;
                dropRing.BorderBrush = Ui.Brush(Tint.Accent);
                dropRing.Background = Ui.Brush(Tint.Accent, 0.12);
                dropRing.Visibility = Visibility.Visible;
            }
            else e.Effects = DragDropEffects.None;
            e.Handled = true;
        };
        root.DragLeave += (_, _) => dropRing.Visibility = Visibility.Collapsed;
        root.Drop += (_, e) =>
        {
            dropRing.Visibility = Visibility.Collapsed;
            if (Files.DroppedFile(e.Data) is { } path)
            {
                if (Files.IsPicture(path)) ImportPicture(() => Files.Codes(path));
                else ImportFile(path);
            }
            else if (Files.Text(e.Data) is { } text) ImportText(text);
        };
    }

    public FrameworkElement View => root;
    public string Title => L.T("Servers");
    public string Subtitle => A.S.View.Servers.Count == 0 ? "" : L.N("%d servers", A.S.View.Servers.Count);
    public string Query { set { if (value != query) { query = value; Update(true); } } }
    public void Shown() { }
    public void Hidden() { }

    string Tab { get => window.Prefs.ServerTab; set => window.Prefs.ServerTab = value; }

    public string Order
    {
        get => window.Prefs.ServerOrder;
        set { window.Prefs.ServerOrder = value; Update(true); }
    }

    List<Server> Shown_ = new();

    public void Update(bool force)
    {
        var s = A.S;
        var all = s.View.Servers;
        var needle = query.Trim().ToLowerInvariant();
        var sb = new StringBuilder();
        sb.Append(s.Loaded).Append(needle).Append('|').Append(Order).Append('|').Append(Tab).Append('|').Append(s.View.Selected).Append('|').Append(Ui.Dark).Append('|').Append(L.Lang);
        foreach (var v in all) sb.Append(v.Id).Append(v.Name).Append(v.Group).Append(v.Protocol).Append(v.Country).Append(';');
        foreach (var sub in s.View.Subscriptions) sb.Append(sub.Name).Append(sub.Updated).Append(sub.Used).Append(sub.Total).Append(sub.Expire).Append(sub.Problem).Append(';');
        // The order by latency moves rows as results come; other orders do not.
        if (Order == "latency") foreach (var d in s.Delays) sb.Append(d.Key).Append(d.Value.State).Append(d.Value.Ms).Append(';');
        var next = sb.ToString();
        if (!force && next == key)
        {
            // The same rows: only the measurements and the headings change in place.
            Refresh();
            return;
        }
        key = next;
        var offset = Scroller()?.VerticalOffset ?? 0;

        var chosen = new HashSet<string>(Selected().Select(v => v.Id));
        IEnumerable<Server> found = all;
        if (needle.Length > 0) found = found.Where(v => (v.Name + " " + v.Protocol + " " + v.Group).ToLowerInvariant().Contains(needle));
        var shown = found.ToList();
        if (Order == "name") shown = shown.OrderBy(v => v.Name, new Compat.Natural()).ToList();
        else if (Order == "latency")
        {
            // Measured ones by the number; no answer after them; not measured last.
            int Value(Server v) => s.Delays.TryGetValue(v.Id, out var d) ? d.State == "ms" ? d.Ms : d.State == "failed" ? int.MaxValue - 1 : int.MaxValue : int.MaxValue;
            shown = shown.Select((v, i) => (v, i)).OrderBy(p => Value(p.v)).ThenBy(p => p.i).Select(p => p.v).ToList();
        }

        // Groups get headings once there is more than one, or one is a
        // subscription: its heading says when it was read and reads it again.
        var subscriptions = s.View.Subscriptions.Select(x => x.Name).ToList();
        var groups = new List<string>(subscriptions);
        foreach (var g in all.Select(v => v.Group))
            if (!groups.Contains(g)) groups.Insert(groups.Count - subscriptions.Count, g);
        UpdateTabs(groups);
        var current = tabGroups.Contains(Tab) ? Tab : "";
        var rows = new List<object>();
        if (current.Length > 0)
        {
            var members = shown.Where(v => v.Group == current).ToList();
            if (subscriptions.Contains(current) && (needle.Length == 0 || members.Count > 0)) rows.Add(current);
            rows.AddRange(members);
        }
        else if (groups.Count > 1 || subscriptions.Count > 0)
        {
            foreach (var g in groups)
            {
                var members = shown.Where(v => v.Group == g).ToList();
                if (members.Count == 0 && (needle.Length > 0 || !subscriptions.Contains(g))) continue;
                rows.Add(g);
                rows.AddRange(members);
            }
        }
        else rows.AddRange(shown);
        Shown_ = rows.OfType<Server>().ToList();
        Delays = new(s.Delays);
        Updating = string.Join(",", s.Updating);
        fixing = true;
        list.Items.Clear();
        foreach (var row in rows)
        {
            var item = row is Server v ? ServerRow(v) : GroupRow((string)row);
            list.Items.Add(item);
            if (row is Server v2 && chosen.Contains(v2.Id)) list.SelectedItems.Add(item);
        }
        fixing = false;

        list.Visibility = rows.Count == 0 ? Visibility.Collapsed : Visibility.Visible;
        if (offset > 0)
        {
            list.UpdateLayout();
            Scroller()?.ScrollToVerticalOffset(offset);
        }
        if (!s.Loaded) empty.Hide();
        else if (all.Count == 0)
        {
            empty.Button.Content = L.T("Import from Clipboard");
            empty.Show(L.T("No servers yet"), L.T("Copy a server link in Throne and paste it here, or drop a file with links onto this window."), true);
        }
        else if (rows.Count == 0) empty.Show(L.T("Nothing matches the search"), "", false);
        else empty.Hide();
    }

    /// Puts the latest measurements and subscription states into the rows there are.
    void Refresh()
    {
        var s = A.S;
        var updating = string.Join(",", s.Updating);
        bool headings = updating != Updating;
        Updating = updating;
        foreach (var item in list.Items.OfType<ListBoxItem>())
        {
            if (item.Tag is Server v && item.Content is Grid grid)
            {
                var old = grid.Children.OfType<FrameworkElement>().FirstOrDefault(e => Grid.GetColumn(e) == 4);
                var mark = Delays.GetValueOrDefault(v.Id);
                var now = s.Delays.GetValueOrDefault(v.Id);
                if (mark?.State == now?.State && mark?.Ms == now?.Ms) continue;
                if (old != null) grid.Children.Remove(old);
                Ui.Place(grid, DelayCell(now), 4, 8);
            }
            else if (headings && item.Tag is string name)
            {
                var fresh = GroupRow(name);
                var content = fresh.Content;
                fresh.Content = null;
                item.Content = content;
            }
        }
        Delays = new(s.Delays);
    }

    Dictionary<string, Delay> Delays = new();
    string Updating = "";

    ScrollViewer? Scroller()
    {
        DependencyObject? node = list;
        while (node != null && node is not ScrollViewer)
            node = VisualTreeHelper.GetChildrenCount(node) > 0 ? VisualTreeHelper.GetChild(node, 0) : null;
        return node as ScrollViewer;
    }

    /// A tab per group once there are two.
    void UpdateTabs(List<string> groups)
    {
        var shown = groups.Count > 1 ? groups : new List<string>();
        tabs.Visibility = shown.Count == 0 ? Visibility.Collapsed : Visibility.Visible;
        fillingTabs = true;
        if (!shown.SequenceEqual(tabGroups) || tabs.Children.Count != shown.Count + 1 || tabsLang != L.Lang)
        {
            tabGroups = shown;
            tabsLang = L.Lang;
            tabs.Children.Clear();
            void Add(string title, string group)
            {
                var tab = new RadioButton { Content = title, Style = Ui.Style("Tab"), GroupName = "servers-tabs" + GetHashCode() };
                tab.Checked += (_, _) =>
                {
                    if (fillingTabs) return;
                    Tab = group;
                    Update(true);
                };
                tabs.Children.Add(tab);
            }
            Add(L.T("All Servers"), "");
            foreach (var g in shown) Add(g == "default" ? L.T("Added by Hand") : g, g);
        }
        if (shown.Count > 0) ((RadioButton)tabs.Children[shown.IndexOf(Tab) + 1]).IsChecked = true;
        fillingTabs = false;
    }

    ListBoxItem ServerRow(Server v)
    {
        var s = A.S;
        bool selected = v.Id == s.View.Selected;
        var grid = Ui.Columns(22, 30, -1, 220, 96);
        grid.Height = 30;
        if (selected)
        {
            var mark = Ui.Glyph("", 16, Ui.B("AccentText"));
            mark.ToolTip = L.T("Selected");
            grid.Children.Add(mark);
        }
        Ui.Place(grid, Ui.CountryPlate(v.Country), 1, 8);
        Ui.Place(grid, Ui.Text(v.Name, 14, selected ? Ui.Semibold : null), 2, 8);
        // The engine speaks English; this note is for the person.
        Ui.Place(grid, Ui.Text(v.Protocol.Replace("certificate not checked", L.T("certificate not checked")), 14, null, Ui.Secondary), 3, 8);
        Ui.Place(grid, DelayCell(s.Delays.GetValueOrDefault(v.Id)), 4, 8);

        var item = new ListBoxItem { Content = grid, Tag = v, Padding = new Thickness(10, 0, 10, 0), MinHeight = 32 };
        item.MouseDoubleClick += (_, _) => Connect(v);
        var menu = new ContextMenu();
        menu.Opened += (_, _) => ServerMenu(menu, v);
        menu.Items.Add(new MenuItem());
        item.ContextMenu = menu;
        return item;
    }

    /// The colour of a delay: quick, bearable, slow. The number is one round
    /// trip to a site through the server over a connection already up.
    public static Tint DelayTint(int ms) => ms < 150 ? Tint.Green : ms < 400 ? Tint.Yellow : Tint.Orange;

    static FrameworkElement DelayCell(Delay? d)
    {
        switch (d?.State)
        {
            case "ms":
                return Ui.Row(6, Ui.Dot(Ui.Color(DelayTint(d!.Ms))), Ui.Text(L.F("%d ms", d.Ms), 12, null, Ui.Secondary));
            case "failed":
                var row = Ui.Row(6, Ui.Dot(Ui.Color(Tint.Red)), Ui.Text(L.T("No answer"), 12, null, Ui.Secondary));
                Ui.Tip(row, d!.Error);
                return row;
            case "waiting":
                return Spinner.Running(14);
            default:
                return new Border();
        }
    }

    /// The heading of a group. For a subscription it says how fresh it is and
    /// what the provider told about the account, and reads it again.
    ListBoxItem GroupRow(string name)
    {
        var s = A.S;
        var grid = Ui.Columns(Ui.Auto, -1, Ui.Auto);
        grid.Height = 30;
        grid.Margin = new Thickness(0, 6, 0, 0);
        // "default" is where servers added by hand go; people need not see the word.
        grid.Children.Add(Ui.Text(name == "default" ? L.T("Added by Hand") : name, 12, Ui.Semibold, Ui.Secondary));
        var sub = s.View.Subscriptions.FirstOrDefault(x => x.Name == name);
        if (sub != null)
        {
            var about = Ui.Text(About(sub), 11, null, sub.Problem.Length == 0 ? Ui.Tertiary : Ui.Brush(Tint.Orange));
            Ui.Tip(about, sub.Problem.Length == 0 ? sub.Host : sub.Problem);
            Ui.Place(grid, about, 1, 8);
            FrameworkElement action;
            if (s.Updating.Contains(name)) action = Spinner.Running(16);
            else
            {
                var refresh = new Button { Content = Ui.Glyph("", 12), Padding = new Thickness(6), Style = Ui.Style("SubtleButton") };
                refresh.ToolTip = L.T("Update Subscription");
                refresh.Click += (_, _) => UpdateSubscription(name);
                action = refresh;
            }
            Ui.Place(grid, action, 2, 8);
        }
        var item = new ListBoxItem { Content = grid, Tag = name, Padding = new Thickness(10, 0, 10, 0), MinHeight = 30, Focusable = false };
        if (sub != null)
        {
            var menu = new ContextMenu();
            menu.Items.Add(Ui.Item(L.T("Update Subscription"), () => UpdateSubscription(name)));
            menu.Items.Add(Ui.Item(L.T("Subscription Settings…"), () => EditSubscription(name)));
            menu.Items.Add(new Separator());
            menu.Items.Add(Ui.Item(L.T("Delete Subscription…"), () => RemoveSubscription(name)));
            item.ContextMenu = menu;
        }
        return item;
    }

    static string About(Subscription sub)
    {
        if (sub.Problem.Length > 0) return L.T("The last update failed");
        var parts = new List<string>();
        if (sub.Updated > 0)
        {
            if (DateTimeOffset.UtcNow.ToUnixTimeSeconds() - sub.Updated < 60) parts.Add(L.T("updated just now"));
            else if (Format.Ago(sub.Updated) is { } ago) parts.Add(L.F("updated %@", ago));
        }
        if (sub.Total > 0) parts.Add(L.F("%@ of %@ used", Format.Amount(sub.Used), Format.Amount(sub.Total)));
        if (sub.Expire > 0 && Format.Day(sub.Expire) is { } until) parts.Add(L.F("until %@", until));
        return string.Join("  ·  ", parts);
    }

    List<Server> Selected() => list.SelectedItems.OfType<ListBoxItem>().Select(i => i.Tag).OfType<Server>().ToList();

    /// The servers an action is about: the one under a right-click when it is
    /// outside the selection, else the selection.
    List<Server> ActionRows(Server? clicked)
    {
        var selected = Selected();
        if (clicked != null && !selected.Any(v => v.Id == clicked.Id)) return new List<Server> { clicked };
        return selected;
    }

    void ServerMenu(ContextMenu menu, Server clicked)
    {
        var rows = ActionRows(clicked);
        menu.Items.Clear();
        menu.Items.Add(Ui.Item(L.T("Use This Server"), () => A.SelectServer(rows[0].Id), enabled: rows.Count == 1));
        menu.Items.Add(Ui.Item(L.T("Test Latency"), () => A.Measure(rows.Select(v => v.Id)), enabled: rows.Count > 0));
        menu.Items.Add(new Separator());
        menu.Items.Add(Ui.Item(L.T("Copy Link"), () => WithLink(rows, (_, link) => CopyLink(link)), enabled: rows.Count == 1));
        menu.Items.Add(Ui.Item(L.T("Show QR Code…"), () => WithLink(rows, (v, link) => _ = QrDialog.Show(v.Name, link)), enabled: rows.Count == 1));
        menu.Items.Add(new Separator());
        menu.Items.Add(Ui.Item(L.T("Delete"), () => Delete(clicked), "Del", rows.Count > 0));
    }

    async void CopyLink(string link)
    {
        if (Ui.Copy(link, secret: true) is { } problem) await Ui.Say(L.T("Nothing was copied"), problem);
    }

    /// The link of the one server an action is about.
    async void WithLink(List<Server> rows, Action<Server, string> then)
    {
        if (rows.Count != 1) return;
        var reply = await A.Do(new Request { Op = "link", Server = rows[0].Id }, quiet: true);
        if (!string.IsNullOrEmpty(reply.Link)) then(rows[0], reply.Link!);
        else await Ui.Say(L.T("This server cannot be written as a link"), reply.Error ?? "");
    }

    /// A double click is the whole wish: this server, now.
    void Connect(Server v)
    {
        if (!A.S.HelperReady || A.S.Busy) return;
        A.ConnectTo(v.Id);
    }

    /// Measures every server the list shows, in its order.
    public void MeasureAll() => A.Measure(Shown_.Select(v => v.Id));

    async void Delete(Server? clicked)
    {
        var rows = ActionRows(clicked);
        if (rows.Count == 0) return;
        var title = rows.Count == 1 ? L.F("Delete the server “%@”?", rows[0].Name) : L.T("Delete the selected servers?");
        var text = rows.Count == 1 ? L.T("To get it back, you will have to import it again.") : string.Join(", ", rows.Select(v => v.Name));
        if (!await Ui.Ask(title, text, L.T("Delete"), destructive: true)) return;
        foreach (var v in rows) _ = A.Do(new Request { Op = "remove", Server = v.Id });
    }

    public async void ImportFromClipboard()
    {
        string? text = null;
        try
        {
            var content = Clipboard.GetDataObject();
            text = content == null ? null : Files.Text(content);
            // A copied picture of a QR code is a server too: a screenshot, a photo.
            if (text == null && Clipboard.ContainsImage() && Clipboard.GetImage() is { } picture)
            {
                ImportPicture(() => Files.Codes(picture));
                return;
            }
        }
        catch (Exception e)
        {
            await Ui.Say(L.T("The clipboard could not be read"), e.Message);
            return;
        }
        if (text == null)
        {
            await Ui.Say(L.T("The clipboard has no text"), L.T("Copy a server link first: in Throne, right-click a profile and copy it as a link."));
            return;
        }
        ImportText(text);
    }

    public void ImportFromFile()
    {
        if (Files.PickOpen(window, ".txt", ".json", ".yaml", ".yml", ".conf", "*") is { } path) ImportFile(path);
    }

    async void ImportFile(string path)
    {
        var (text, problem) = Files.Read(path);
        if (text != null) ImportText(text);
        else await Ui.Say(L.T("Nothing was imported"), problem ?? "");
    }

    /// Reads the QR codes of a picture and imports what they say.
    async void ImportPicture(Func<List<string>> read)
    {
        List<string> codes;
        try { codes = read(); }
        catch (Exception e)
        {
            await Ui.Say(L.T("The picture could not be read"), e.Message);
            return;
        }
        if (codes.Count == 0) await Ui.Say(L.T("No QR code was found in the picture"), "");
        else ImportText(string.Join("\n", codes));
    }

    async void ImportText(string text)
    {
        var reply = await A.Do(new Request { Op = "import", Text = text }, quiet: true);
        int imported = reply.Imported?.Imported?.Count ?? 0;
        var failed = reply.Imported?.Failed ?? new();
        // A clean import needs no dialog: the new rows are the answer.
        if (failed.Count == 0 && string.IsNullOrEmpty(reply.Error)) return;
        var lines = failed.Select(f => L.F("Entry %d: %@", f.Entry, f.Reason)).ToList();
        if (failed.Count == 0 && reply.Error != null) lines = new List<string> { reply.Error };
        var title = imported > 0 ? L.F("Imported %d, not imported %d", imported, failed.Count) : L.T("Nothing was imported");
        await Ui.Say(title, string.Join("\n\n", lines));
    }

    async void UpdateSubscription(string name)
    {
        if (A.S.Updating.Contains(name)) return;
        var reply = await A.Subscription(new Request { Op = "subUpdate", Name = name });
        if (!string.IsNullOrEmpty(reply.Error)) await Ui.Say(L.F("The subscription “%@” was not updated", name), reply.Error!);
    }

    // The fields take the width of the dialog, as its text does.
    static TextBox Field(string placeholder, string text = "")
    {
        var box = Ui.Field(placeholder, text);
        box.MinWidth = 380;
        return box;
    }

    /// Asks for the address of a subscription and adds it.
    public async void AddSubscription()
    {
        var address = Field("https://");
        var name = Field(L.T("Name (optional)"));
        // A copied address is most likely the one to add.
        try
        {
            if (Clipboard.ContainsText())
            {
                var copied = Clipboard.GetText().Trim();
                if ((copied.StartsWith("http://") || copied.StartsWith("https://")) && copied.IndexOf('\n') < 0) address.Text = copied;
            }
        }
        catch (Exception) { }
        var dialog = new Dialog
        {
            Title = L.T("Add a Subscription"),
            Content = Ui.Column(10, Ui.Text(L.T("The address your provider gave you. FlClash reads the list of servers from it and keeps it up to date."), 13, null, Ui.Secondary, wrap: true), address, name),
            Primary = L.T("Add"),
        };
        if (!await Dialogs.Show(dialog)) return;
        var reply = await A.Subscription(new Request { Op = "subAdd", Name = name.Text, Text = address.Text });
        var failed = reply.Imported?.Failed ?? new();
        if (string.IsNullOrEmpty(reply.Error) && failed.Count == 0) return;
        var lines = failed.Take(8).Select(f => L.F("Entry %d: %@", f.Entry, f.Reason)).ToList();
        if (!string.IsNullOrEmpty(reply.Error)) lines.Insert(0, reply.Error!);
        var title = string.IsNullOrEmpty(reply.Error) ? L.F("Imported %d, not imported %d", reply.Imported?.Imported?.Count ?? 0, failed.Count) : L.T("The subscription was not added");
        await Ui.Say(title, string.Join("\n\n", lines));
    }

    async void EditSubscription(string name)
    {
        var sub = A.S.View.Subscriptions.FirstOrDefault(x => x.Name == name);
        if (sub == null) return;
        var address = Field(L.T("New address (leave empty to keep)"));
        var agent = Field(L.T("Client name told to the provider"), sub.Agent);
        var every = Field(L.T("Hours between updates (empty: as the provider says)"), sub.Every > 0 ? sub.Every.ToString() : "");
        var skip = Field(L.T("Leave out servers with these words, comma-separated"), sub.Skip);
        var dialog = new Dialog
        {
            Title = L.F("Subscription “%@”", name),
            Content = Ui.Column(10, Ui.Text(sub.Host, 13, null, Ui.Secondary), address, agent, every, skip),
            Primary = L.T("Save"),
        };
        if (!await Dialogs.Show(dialog)) return;
        int.TryParse(every.Text.Trim(), out var hours);
        var reply = await A.Subscription(new Request { Op = "subEdit", Name = name, Text = address.Text, Agent = agent.Text, Every = hours.ToString(), Skip = skip.Text });
        if (!string.IsNullOrEmpty(reply.Error)) await Ui.Say(L.F("The subscription “%@” was not updated", name), reply.Error!);
    }

    async void RemoveSubscription(string name)
    {
        if (!await Ui.Ask(L.F("Delete the subscription “%@”?", name), L.T("Its servers will be deleted with it."), L.T("Delete"), destructive: true)) return;
        _ = A.Do(new Request { Op = "subRemove", Name = name });
    }
}
