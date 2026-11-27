using System.Text;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Media;
using Windows.ApplicationModel.DataTransfer;
using Windows.Storage.Pickers;
using Windows.System;

namespace FlClash;

/// The routing policies: pick one, or bring your own from a rules file.
public sealed class RoutingPage : IPage
{
    readonly MainWindow window;
    Agent A => window.Agent;
    readonly Grid root = new() { AllowDrop = true, Background = new SolidColorBrush(Microsoft.UI.Colors.Transparent) };
    readonly ListView list = new() { SelectionMode = ListViewSelectionMode.Single, Padding = new Thickness(8, 0, 8, 8) };
    string key = "";

    public RoutingPage(MainWindow window)
    {
        this.window = window;
        root.Children.Add(list);
        list.KeyDown += (_, e) =>
        {
            if (e.Key == VirtualKey.Delete && list.SelectedItem is ListViewItem { Tag: Policy p }) { Delete(p); e.Handled = true; }
            else if (e.Key == VirtualKey.Enter && list.SelectedItem is ListViewItem { Tag: Policy q }) { A.SelectPolicy(q.Name); e.Handled = true; }
        };
        root.DragOver += (_, e) =>
        {
            if (e.DataView.Contains(StandardDataFormats.StorageItems) || e.DataView.Contains(StandardDataFormats.Text)) e.AcceptedOperation = DataPackageOperation.Copy;
        };
        root.Drop += async (_, e) =>
        {
            string name = "policy";
            if (e.DataView.Contains(StandardDataFormats.StorageItems))
            {
                var files = await e.DataView.GetStorageItemsAsync();
                if (files.FirstOrDefault() is { } f) name = Path.GetFileNameWithoutExtension(f.Name);
            }
            var text = await ServersPage.Dropped(e.DataView);
            if (text != null) Import(text, name);
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
        var chosen = (list.SelectedItem as ListViewItem)?.Tag is Policy c ? c.Name : null;
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

    ListViewItem Row(Policy p)
    {
        bool chosen = p.Name == A.S.View.Policy;
        var (glyph, tint) = Picture(p);
        var grid = new Grid { ColumnSpacing = 12, Height = 52 };
        grid.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        grid.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        grid.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        grid.Children.Add(Ui.Tile(glyph, tint));
        var lines = Ui.Column(2, Ui.Text(Format.PolicyTitle(p.Name), 14, chosen ? Ui.Semibold : null), Ui.Text(Format.PolicyAbout(p), 12, null, Ui.Secondary));
        lines.VerticalAlignment = VerticalAlignment.Center;
        Grid.SetColumn(lines, 1);
        grid.Children.Add(lines);
        if (chosen)
        {
            var check = Ui.Glyph("", 18, Ui.Res("AccentTextFillColorPrimaryBrush"));
            ToolTipService.SetToolTip(check, L.T("Selected"));
            Grid.SetColumn(check, 2);
            grid.Children.Add(check);
        }
        var item = new ListViewItem { HorizontalContentAlignment = HorizontalAlignment.Stretch, Content = grid, Tag = p, Padding = new Thickness(10, 2, 14, 2) };
        item.DoubleTapped += (_, _) => A.SelectPolicy(p.Name);
        var menu = new MenuFlyout();
        menu.Items.Add(MainWindow.Item(L.T("Use This Policy"), () => A.SelectPolicy(p.Name)));
        menu.Items.Add(new MenuFlyoutSeparator());
        menu.Items.Add(MainWindow.Item(L.T("Edit…"), () => Edit(p), enabled: !p.Builtin));
        menu.Items.Add(MainWindow.Item(L.T("Duplicate…"), () => Duplicate(p)));
        menu.Items.Add(new MenuFlyoutSeparator());
        menu.Items.Add(MainWindow.Item(L.T("Delete"), () => Delete(p), "Del", Removable(p)));
        item.ContextFlyout = menu;
        return item;
    }

    /// An imported policy that is not the one in use can go.
    bool Removable(Policy p) => !p.Builtin && p.Name != A.S.View.Policy;

    async void Delete(Policy p)
    {
        if (!Removable(p)) return;
        if (!await Ui.Ask(root.XamlRoot, L.F("Delete the policy “%@”?", p.Name), L.T("To get it back, you will have to import it again."), L.T("Delete"), destructive: true)) return;
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
        else await Ui.Say(root.XamlRoot, L.T("The policy could not be opened"), reply.Error ?? "");
    }

    async void Duplicate(Policy p)
    {
        var reply = await A.Do(new Request { Op = "policyRead", Name = p.Name }, quiet: true);
        if (reply.Document is not { } doc)
        {
            await Ui.Say(root.XamlRoot, L.T("The policy could not be opened"), reply.Error ?? "");
            return;
        }
        doc.Name = p.Name + "-copy";
        await PolicyEditor.Open(window, doc, null);
    }

    public async void ImportPolicy()
    {
        var picker = new FileOpenPicker();
        WinRT.Interop.InitializeWithWindow.Initialize(picker, window.Hwnd);
        foreach (var ext in new[] { ".txt", ".list", ".conf", "*" }) picker.FileTypeFilter.Add(ext);
        var file = await picker.PickSingleFileAsync();
        if (file == null) return;
        var text = await ServersPage.ReadSmall(file);
        if (text != null) Import(text, Path.GetFileNameWithoutExtension(file.Name));
    }

    /// The name a policy is kept under, in the form the engine takes.
    public static string Slug(string name) =>
        new(name.Trim().ToLowerInvariant().Select(c => c < 128 && (char.IsLetterOrDigit(c) || c == '-' || c == '_') ? c : '-').ToArray());

    async void Import(string text, string suggested)
    {
        var reply = await A.Do(new Request { Op = "policyImport", Name = Slug(suggested), Text = text }, quiet: true);
        if (!string.IsNullOrEmpty(reply.Error)) await Ui.Say(root.XamlRoot, L.T("The policy was not imported"), reply.Error);
    }
}
