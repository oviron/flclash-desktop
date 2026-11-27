using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;

namespace FlClash;

/// The dialog a policy is written in: three lists for most people, the rules
/// themselves for those who want the order in their own hands.
public static class PolicyEditor
{
    static TextBox Editor(string text, double height)
    {
        var box = new TextBox
        {
            Text = (text ?? "").Replace("\r\n", "\n").Replace("\n", "\r\n"),
            AcceptsReturn = true,
            AcceptsTab = false,
            TextWrapping = TextWrapping.NoWrap,
            FontFamily = new FontFamily("Cascadia Mono, Consolas"),
            FontSize = 12,
            Height = height,
            VerticalContentAlignment = VerticalAlignment.Top,
            HorizontalScrollBarVisibility = ScrollBarVisibility.Auto,
            VerticalScrollBarVisibility = ScrollBarVisibility.Auto,
        };
        SpellCheck.SetIsEnabled(box, false);
        return box;
    }

    static string Lines(TextBox box) => box.Text.Replace("\r\n", "\n").Replace('\r', '\n');

    /// original is the name of the policy edited; null makes a new one.
    public static async Task Open(MainWindow window, PolicyDocument doc, string? original)
    {
        var A = window.Agent;
        var name = Ui.Field(L.T("Name: letters, digits and dashes"), doc.Name ?? "");
        name.Visibility = original == null ? Visibility.Visible : Visibility.Collapsed;
        var direct = Editor(doc.Direct, 74);
        var proxy = Editor(doc.Proxy, 74);
        var block = Editor(doc.Block, 74);
        var rules = Editor(doc.Text, 330);
        var rest = new ComboBox { MinWidth = 180 };
        rest.Items.Add(new ComboBoxItem { Content = L.T("through the server") });
        rest.Items.Add(new ComboBoxItem { Content = L.T("directly") });
        rest.SelectedIndex = doc.Rest == "DIRECT" ? 1 : 0;
        var restRow = Ui.Row(8, Ui.Text(L.T("Everything else goes"), 14), rest);
        var hint = Ui.Text(L.T("One per line. A name stands for itself and everything under it: example.com, .ru. A network: 10.0.0.0/8. Or say the kind: domain:exact.example, keyword:word, geosite:youtube, geoip:us."), 11, null, Ui.Secondary, wrap: true);

        var lists = new StackPanel();
        foreach (var (label, box, tint, glyph) in new[] {
            (L.T("Directly"), direct, Tint.Teal, ""),
            (L.T("Through the server"), proxy, Tint.Indigo, ""),
            (L.T("Refuse"), block, Tint.Red, "") })
        {
            Ui.Add(lists, Ui.Row(6, Ui.Tile(glyph, tint, 18), Ui.Text(label, 14)), 6);
            box.Margin = new Thickness(0, 6, 0, 8);
            lists.Children.Add(box);
        }
        Ui.Add(lists, restRow, 6);
        Ui.Add(lists, hint, 6);

        var tabs = new StackPanel { Orientation = Orientation.Horizontal };
        var group = "policy-tabs" + Environment.TickCount;
        var asLists = new RadioButton { Content = L.T("Lists"), Style = Ui.Style("Tab"), GroupName = group, IsEnabled = doc.Listed };
        var asRules = new RadioButton { Content = L.T("Rules"), Style = Ui.Style("Tab"), GroupName = group };
        tabs.Children.Add(asLists);
        tabs.Children.Add(asRules);
        var problem = Ui.Text("", 12, null, Ui.Brush(Tint.Red), wrap: true);
        problem.Visibility = Visibility.Collapsed;
        void Switch()
        {
            bool l = asLists.IsChecked == true;
            lists.Visibility = l ? Visibility.Visible : Visibility.Collapsed;
            rules.Visibility = l ? Visibility.Collapsed : Visibility.Visible;
            problem.Visibility = Visibility.Collapsed;
        }
        // Rules that are not in the order of lists cannot be shown as lists
        // without changing what they mean.
        (doc.Listed ? asLists : asRules).IsChecked = true;
        asLists.Checked += (_, _) => Switch();
        asRules.Checked += (_, _) => Switch();
        Switch();

        var content = Ui.Column(12, tabs, name, lists, rules, problem);
        content.Width = 520;
        void Show(string text)
        {
            problem.Text = text;
            problem.Visibility = Visibility.Visible;
        }
        var dialog = new Dialog
        {
            Title = original == null ? L.T("New Policy") : L.F("Policy “%@”", doc.Name ?? ""),
            Content = new ScrollViewer { Content = content, MaxHeight = 560, VerticalScrollBarVisibility = ScrollBarVisibility.Auto },
            Primary = L.T("Save"),
            MaxWidth = 640,
            // The dialog stays open until the engine took the policy.
            OnPrimary = async _ =>
            {
                var slug = RoutingPage.Slug(original ?? name.Text);
                if (slug.Length == 0) { Show(L.T("Give the policy a name.")); return false; }
                Request r = asLists.IsChecked == true
                    ? new Request { Op = "policyWriteLists", Name = slug, Direct = Lines(direct), Proxy = Lines(proxy), Block = Lines(block), Rest = rest.SelectedIndex == 1 ? "DIRECT" : "PROXY" }
                    : new Request { Op = "policyWrite", Name = slug, Text = Lines(rules) };
                var reply = await A.Change(r, quiet: true);
                if (!string.IsNullOrEmpty(reply.Error)) { Show(reply.Error!); return false; }
                return true;
            },
        };
        await Dialogs.Show(dialog);
    }
}
