using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;

namespace FlClash;

/// What goes through the core right now: where to, by which rule, how much.
public sealed class ConnectionsPage : IPage
{
    readonly MainWindow window;
    Agent A => window.Agent;
    readonly Grid root = new();
    readonly Grid head = Columns();
    readonly ListBox list = new() { SelectionMode = SelectionMode.Single, Padding = new Thickness(8, 0, 8, 8) };
    readonly Ui.Empty empty = new("");
    readonly Dictionary<string, (ListBoxItem item, TextBlock[] cells)> rows = new();
    List<string> order = new();
    List<Connection>? frozen;
    string query = "";
    bool visible;
    static readonly double[] widths = { -1, 110, 170, 74, 74, 70 };
    static readonly string[] titles = { "Destination", "Through", "Rule", "↓", "↑", "Open For" };

    public ConnectionsPage(MainWindow window)
    {
        this.window = window;
        head.Margin = new Thickness(26, 0, 30, 4);
        root.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        root.RowDefinitions.Add(new RowDefinition { Height = new GridLength(1, GridUnitType.Star) });
        root.Children.Add(head);
        Grid.SetRow(list, 1);
        root.Children.Add(list);
        Grid.SetRowSpan(empty.View, 2);
        root.Children.Add(empty.View);
        list.KeyDown += (_, e) =>
        {
            if (e.Key == Key.Delete && list.SelectedItem is ListBoxItem { Tag: string id }) { Close(id); e.Handled = true; }
        };
    }

    static Grid Columns() => Ui.Columns(widths);

    public FrameworkElement View => root;
    public string Title => L.T("Connections");
    public string Subtitle => (A.S.Stats?.Open ?? 0) == 0 ? "" : L.N("%d open", A.S.Stats!.Open);
    public string Query { set { if (value != query) { query = value; Update(true); } } }
    public void Shown() { visible = true; }
    public void Hidden() { visible = false; }

    /// A paused list stays as it is, to be read and acted on; the counters in
    /// the header go on.
    public bool Paused
    {
        get => frozen != null;
        set
        {
            if (value == Paused) return;
            frozen = value ? (A.S.Status.Running ? new List<Connection>(A.S.Stats?.Connections ?? new()) : new()) : null;
            if (visible) Update(true);
        }
    }

    public void Update(bool force)
    {
        // The list changes every second; nothing to do while nobody looks.
        if (!visible) return;
        var s = A.S;
        if (force || head.Children.Count == 0)
        {
            head.Children.Clear();
            for (int i = 0; i < titles.Length; i++)
                Ui.Place(head, Ui.Text(L.T(titles[i]), 12, Ui.Semibold, Ui.Secondary), i, 8);
        }
        // Nothing to hold once the connection is off.
        if (frozen != null && !s.Status.Running) { frozen = null; window.UpdateToolbar(); }
        var all = frozen ?? (s.Status.Running ? s.Stats?.Connections ?? new() : new());
        var needle = query.Trim().ToLowerInvariant();
        var shown = needle.Length == 0 ? all : all.Where(c => (c.Host + " " + c.Via + " " + c.Rule).ToLowerInvariant().Contains(needle)).ToList();

        if (force) rows.Clear();
        var ids = shown.Select(c => c.Id).ToList();
        foreach (var c in shown)
        {
            if (!rows.TryGetValue(c.Id, out var row)) rows[c.Id] = row = Row(c.Id);
            Fill(row.cells, c);
        }
        foreach (var gone in rows.Keys.Except(ids).ToList()) rows.Remove(gone);
        if (force || !ids.SequenceEqual(order))
        {
            var chosen = (list.SelectedItem as ListBoxItem)?.Tag as string;
            order = ids;
            list.Items.Clear();
            foreach (var id in ids) list.Items.Add(rows[id].item);
            if (chosen != null && rows.TryGetValue(chosen, out var keep)) list.SelectedItem = keep.item;
        }

        bool any = shown.Count > 0;
        list.Visibility = head.Visibility = any ? Visibility.Visible : Visibility.Collapsed;
        if (!s.Status.Running) empty.Show(L.T("Nothing runs"), L.T("Connections appear here while FlClash is on."), false);
        else if (all.Count == 0) empty.Show(L.T("No open connections"), "", false);
        else if (!any) empty.Show(L.T("Nothing matches the search"), "", false);
        else empty.Hide();
    }

    (ListBoxItem, TextBlock[]) Row(string id)
    {
        var grid = Columns();
        grid.Height = 28;
        var cells = new TextBlock[widths.Length];
        for (int i = 0; i < cells.Length; i++)
            cells[i] = Ui.Place(grid, Ui.Text("", 13, null, i == 0 ? null : Ui.Secondary), i, 8);
        var item = new ListBoxItem { Content = grid, Tag = id, Padding = new Thickness(10, 0, 10, 0), MinHeight = 30 };
        var menu = new ContextMenu();
        menu.Items.Add(Ui.Item(L.T("Close Connection"), () => Close(id), "Del"));
        item.ContextMenu = menu;
        return (item, cells);
    }

    void Fill(TextBlock[] cells, Connection c)
    {
        // The port says something only when it is not the usual one.
        var usual = c.Port is 443 or 80;
        cells[0].Text = usual ? c.Host : c.Host + ":" + c.Port;
        cells[0].ToolTip = (c.Network ?? "").ToUpperInvariant() + " " + c.Host + ":" + c.Port;
        // What goes through the server, what goes past it, what is refused.
        switch (c.Via)
        {
            case "DIRECT": cells[1].Text = L.T("directly"); cells[1].Foreground = Ui.Secondary; break;
            case "REJECT" or "REJECT-DROP": cells[1].Text = L.T("refused"); cells[1].Foreground = Ui.Brush(Tint.Red); break;
            default: cells[1].Text = c.Via; cells[1].Foreground = Ui.Primary; break;
        }
        cells[2].Text = RuleTitle(c.Rule ?? "");
        cells[3].Text = Format.Amount(c.Download);
        cells[4].Text = Format.Amount(c.Upload);
        cells[5].Text = Format.Age(c.Start);
    }

    /// A rule the way a person would say it, not the way the core names it.
    static string RuleTitle(string rule)
    {
        var parts = rule.Split(new[] { ',' }, 2);
        var kind = parts[0];
        var payload = parts.Length > 1 ? parts[1] : "";
        return kind switch
        {
            "Match" => L.T("everything else"),
            "DomainSuffix" => "." + payload,
            "Domain" or "IPCIDR" or "IPCIDR6" => payload,
            "DomainKeyword" => "*" + payload + "*",
            "RuleSet" => L.F("list “%@”", payload.Replace("geosite-", "").Replace("geoip-", "")),
            _ => rule,
        };
    }

    void Close(string id)
    {
        A.CloseConnection(id);
        // A paused list does not hear of it: the row goes now.
        if (frozen != null)
        {
            frozen.RemoveAll(c => c.Id == id);
            Update(false);
        }
    }
}
