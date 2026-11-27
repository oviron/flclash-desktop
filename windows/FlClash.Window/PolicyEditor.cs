using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Media;

namespace FlClash;

/// The dialog a policy is written in: three lists for most people, the rules
/// themselves for those who want the order in their own hands.
public static class PolicyEditor
{
    static TextBox Editor(string text, double height) => new()
    {
        Text = text.Replace("\n", "\r"),
        AcceptsReturn = true,
        TextWrapping = TextWrapping.NoWrap,
        IsSpellCheckEnabled = false,
        IsTextPredictionEnabled = false,
        FontFamily = new FontFamily("Cascadia Mono, Consolas"),
        FontSize = 12,
        Height = height,
    };

    static string Lines(TextBox box) => box.Text.Replace("\r\n", "\n").Replace('\r', '\n');

    /// original is the name of the policy edited; null makes a new one.
    public static async Task Open(MainWindow window, PolicyDocument doc, string? original)
    {
        var A = window.Agent;
        var name = new TextBox { PlaceholderText = L.T("Name: letters, digits and dashes"), Text = doc.Name, Visibility = original == null ? Visibility.Visible : Visibility.Collapsed };
        var direct = Editor(doc.Direct, 74);
        var proxy = Editor(doc.Proxy, 74);
        var block = Editor(doc.Block, 74);
        var rules = Editor(doc.Text, 330);
        var rest = new ComboBox { Items = { L.T("through the server"), L.T("directly") }, SelectedIndex = doc.Rest == "DIRECT" ? 1 : 0 };
        var restRow = Ui.Row(8, Ui.Text(L.T("Everything else goes"), 14), rest);
        var hint = Ui.Text(L.T("One per line. A name stands for itself and everything under it: example.com, .ru. A network: 10.0.0.0/8. Or say the kind: domain:exact.example, keyword:word, geosite:youtube, geoip:us."), 11, null, Ui.Secondary, wrap: true);

        var lists = new StackPanel { Spacing = 6 };
        foreach (var (label, box, tint, glyph) in new[] {
            (L.T("Directly"), direct, Tint.Teal, ""),
            (L.T("Through the server"), proxy, Tint.Indigo, ""),
            (L.T("Refuse"), block, Tint.Red, "") })
        {
            lists.Children.Add(Ui.Row(6, Ui.Tile(glyph, tint, 18), Ui.Text(label, 14)));
            box.Margin = new Thickness(0, 0, 0, 8);
            lists.Children.Add(box);
        }
        lists.Children.Add(restRow);
        lists.Children.Add(hint);

        var tabs = new SelectorBar();
        var asLists = new SelectorBarItem { Text = L.T("Lists"), IsEnabled = doc.Listed };
        var asRules = new SelectorBarItem { Text = L.T("Rules") };
        tabs.Items.Add(asLists);
        tabs.Items.Add(asRules);
        var problem = Ui.Text("", 12, null, Ui.Brush(Tint.Red), wrap: true);
        problem.Visibility = Visibility.Collapsed;
        void Switch()
        {
            bool l = tabs.SelectedItem == asLists;
            lists.Visibility = l ? Visibility.Visible : Visibility.Collapsed;
            rules.Visibility = l ? Visibility.Collapsed : Visibility.Visible;
            problem.Visibility = Visibility.Collapsed;
        }
        // Rules that are not in the order of lists cannot be shown as lists
        // without changing what they mean.
        tabs.SelectedItem = doc.Listed ? asLists : asRules;
        tabs.SelectionChanged += (_, _) => Switch();
        Switch();

        var content = Ui.Column(12, tabs, name, lists, rules, problem);
        content.Width = 520;
        var dialog = new ContentDialog
        {
            XamlRoot = window.Content.XamlRoot,
            Title = original == null ? L.T("New Policy") : L.F("Policy “%@”", doc.Name),
            Content = new ScrollViewer { Content = content, MaxHeight = 560 },
            PrimaryButtonText = L.T("Save"),
            CloseButtonText = L.T("Cancel"),
            DefaultButton = ContentDialogButton.Primary,
        };
        dialog.Resources["ContentDialogMaxWidth"] = 640.0;
        dialog.PrimaryButtonClick += async (d, e) =>
        {
            // The dialog stays open until the engine took the policy.
            var wait = e.GetDeferral();
            try
            {
                var slug = RoutingPage.Slug(original ?? name.Text);
                if (slug.Length == 0) { Show(L.T("Give the policy a name.")); e.Cancel = true; return; }
                Request r = tabs.SelectedItem == asLists
                    ? new Request { Op = "policyWriteLists", Name = slug, Direct = Lines(direct), Proxy = Lines(proxy), Block = Lines(block), Rest = rest.SelectedIndex == 1 ? "DIRECT" : "PROXY" }
                    : new Request { Op = "policyWrite", Name = slug, Text = Lines(rules) };
                d.IsPrimaryButtonEnabled = false;
                var reply = await A.Change(r, quiet: true);
                d.IsPrimaryButtonEnabled = true;
                if (!string.IsNullOrEmpty(reply.Error)) { Show(reply.Error); e.Cancel = true; }
            }
            finally { wait.Complete(); }
        };
        void Show(string text)
        {
            problem.Text = text;
            problem.Visibility = Visibility.Visible;
        }
        await Ui.Show(dialog);
    }
}
