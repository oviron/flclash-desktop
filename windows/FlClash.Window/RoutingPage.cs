using System.Text;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;
using System.Windows.Media;

namespace FlClash;

/// The routing policies: pick one, or bring your own from a rules file.
public sealed class RoutingPage : IPage
{
    readonly MainWindow window;
    Agent A => window.Agent;
    readonly Grid root = new() { AllowDrop = true, Background = Brushes.Transparent };
    readonly ListBox list = new() { SelectionMode = SelectionMode.Single, Padding = new Thickness(8, 0, 8, 8) };
    string key = "";

    public RoutingPage(MainWindow window)
    {
        this.window = window;
        root.Children.Add(list);
        list.KeyDown += (_, e) =>
        {
            if (e.Key == Key.Delete && list.SelectedItem is ListBoxItem { Tag: Policy p }) { Delete(p); e.Handled = true; }
            else if (e.Key == Key.Enter && list.SelectedItem is ListBoxItem { Tag: Policy q }) { A.SelectPolicy(q.Name); e.Handled = true; }
        };
        root.DragOver += (_, e) =>
        {
            e.Effects = e.Data.GetDataPresent(DataFormats.FileDrop) || e.Data.GetDataPresent(DataFormats.UnicodeText) ? DragDropEffects.Copy : DragDropEffects.None;
            e.Handled = true;
        };
        root.Drop += (_, e) =>
        {
            if (Files.DroppedFile(e.Data) is { } path) ImportFile(path);
            else if (Files.Text(e.Data) is { } text) Import(text, "policy");
        };
    }

    public FrameworkElement View => root;
    public string Title => L.T("Routing");
    public string Subtitle => "";
    public string Query { set { } }
    public void Shown() { }
    public void Hidden() { }

    public void Update(bool force)
    {
        var s = A.S;
        var sb = new StringBuilder().Append(s.View.Policy).Append(Ui.Dark).Append(L.Lang);
        foreach (var p in s.View.Policies) sb.Append(p.Name).Append(p.Builtin).Append(p.Rules).Append(';');
        if (!force && sb.ToString() == key) return;
        key = sb.ToString();
        var chosen = (list.SelectedItem as ListBoxItem)?.Tag is Policy c ? c.Name : null;
        list.Items.Clear();
        foreach (var p in s.View.Policies)
        {
            var item = Row(p);
            list.Items.Add(item);
            if (p.Name == chosen) list.SelectedItem = item;
        }
    }

    static (string, Tint) Picture(Policy p) => p.Name switch
    {
        "all" => ("", Tint.Blue),
        "lan-direct" => ("", Tint.Teal),
        "ru-direct" => ("", Tint.Indigo),
        _ => ("", Tint.Gray),
    };

    ListBoxItem Row(Policy p)
    {
        bool chosen = p.Name == A.S.View.Policy;
        var (glyph, tint) = Picture(p);
        var grid = Ui.Columns(Ui.Auto, -1, Ui.Auto);
        grid.Height = 52;
        grid.Children.Add(Ui.Tile(glyph, tint));
        var lines = Ui.Column(2, Ui.Text(Format.PolicyTitle(p.Name), 14, chosen ? Ui.Semibold : null), Ui.Text(Format.PolicyAbout(p), 12, null, Ui.Secondary));
        lines.VerticalAlignment = VerticalAlignment.Center;
        Ui.Place(grid, lines, 1, 12);
        if (chosen)
        {
            var check = Ui.Glyph("", 18, Ui.B("AccentText"));
            check.ToolTip = L.T("Selected");
            Ui.Place(grid, check, 2, 12);
        }
        var item = new ListBoxItem { Content = grid, Tag = p, Padding = new Thickness(10, 2, 14, 2) };
        item.MouseDoubleClick += (_, _) => A.SelectPolicy(p.Name);
        var menu = new ContextMenu();
        menu.Items.Add(Ui.Item(L.T("Use This Policy"), () => A.SelectPolicy(p.Name)));
        menu.Items.Add(new Separator());
        menu.Items.Add(Ui.Item(L.T("Edit…"), () => Edit(p), enabled: !p.Builtin));
        menu.Items.Add(Ui.Item(L.T("Duplicate…"), () => Duplicate(p)));
        menu.Items.Add(new Separator());
        menu.Items.Add(Ui.Item(L.T("Delete"), () => Delete(p), "Del", Removable(p)));
        item.ContextMenu = menu;
        return item;
    }

    /// An imported policy that is not the one in use can go.
    bool Removable(Policy p) => !p.Builtin && p.Name != A.S.View.Policy;

    async void Delete(Policy p)
    {
        if (!Removable(p)) return;
        if (!await Ui.Ask(L.F("Delete the policy “%@”?", p.Name), L.T("To get it back, you will have to import it again."), L.T("Delete"), destructive: true)) return;
        _ = A.Do(new Request { Op = "policyRemove", Name = p.Name });
    }

    /// Opens the editor on a new, empty policy.
    public void NewPolicy() => _ = PolicyEditor.Open(window, new PolicyDocument { Listed = true }, null);

    /// An imported policy opens for editing; a built-in one stays as it is,
    /// and a copy of it can be changed.
    async void Edit(Policy p)
    {
        if (p.Builtin) return;
        var reply = await A.Do(new Request { Op = "policyRead", Name = p.Name }, quiet: true);
        if (reply.Document != null) await PolicyEditor.Open(window, reply.Document, p.Name);
        else await Ui.Say(L.T("The policy could not be opened"), reply.Error ?? "");
    }

    async void Duplicate(Policy p)
    {
        var reply = await A.Do(new Request { Op = "policyRead", Name = p.Name }, quiet: true);
        if (reply.Document is not { } doc)
        {
            await Ui.Say(L.T("The policy could not be opened"), reply.Error ?? "");
            return;
        }
        doc.Name = p.Name + "-copy";
        await PolicyEditor.Open(window, doc, null);
    }

    public void ImportPolicy()
    {
        if (Files.PickOpen(window, ".txt", ".list", ".conf", "*") is { } path) ImportFile(path);
    }

    async void ImportFile(string path)
    {
        var (text, problem) = Files.Read(path);
        if (text != null) Import(text, Path.GetFileNameWithoutExtension(path));
        else await Ui.Say(L.T("The policy was not imported"), problem ?? "");
    }

    /// The name a policy is kept under, in the form the engine takes.
    public static string Slug(string name) =>
        new(name.Trim().ToLowerInvariant().Select(c => c < 128 && (char.IsLetterOrDigit(c) || c == '-' || c == '_') ? c : '-').ToArray());

    async void Import(string text, string suggested)
    {
        var reply = await A.Do(new Request { Op = "policyImport", Name = Slug(suggested), Text = text }, quiet: true);
        if (!string.IsNullOrEmpty(reply.Error)) await Ui.Say(L.T("The policy was not imported"), reply.Error!);
    }
}
