using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;

namespace FlClash;

/// A question or a message over the window: a title, what it is about, and
/// one or two buttons.
public sealed class Dialog
{
    public string Title = "";
    public UIElement? Content;
    /// The button that does the thing; none for a message.
    public string? Primary;
    public string Close = L.T("Cancel");
    /// Enter presses the primary button; for what cannot be undone, the other.
    public bool PrimaryIsDefault = true;
    public double MaxWidth = 548;
    /// Called by the primary button; false keeps the dialog open.
    public Func<Dialog, Task<bool>>? OnPrimary;
    public Button? PrimaryButton { get; internal set; }
}

public static class Dialogs
{
    static Grid? layer;
    static bool open;

    /// A dialog is in front: the window's keys wait.
    public static bool Open => open;

    /// The layer of the window the dialogs are drawn on.
    public static void Attach(Grid host)
    {
        layer = host;
        layer.Visibility = Visibility.Collapsed;
        layer.SetResourceReference(Panel.BackgroundProperty, "Smoke");
        KeyboardNavigation.SetTabNavigation(layer, KeyboardNavigationMode.Cycle);
        KeyboardNavigation.SetControlTabNavigation(layer, KeyboardNavigationMode.Cycle);
        KeyboardNavigation.SetDirectionalNavigation(layer, KeyboardNavigationMode.Cycle);
    }

    /// One dialog at a time: a second waits for the first to close. True when
    /// the primary button closed it.
    public static async Task<bool> Show(Dialog d)
    {
        while (open) await Task.Delay(100);
        if (layer == null) return false;
        open = true;
        try { return await Present(layer, d); }
        finally { open = false; }
    }

    static Task<bool> Present(Grid host, Dialog d)
    {
        var done = new TaskCompletionSource<bool>();
        var before = Keyboard.FocusedElement;

        var body = Ui.Column(14, Ui.Text(d.Title, 20, Ui.Semibold, null, wrap: true));
        if (d.Content != null) Ui.Add(body, d.Content, 14);
        var top = new Border { Padding = new Thickness(24, 22, 24, 24), Child = body };

        var close = new Button { Content = d.Close, IsCancel = true, HorizontalAlignment = HorizontalAlignment.Stretch };
        Button? primary = d.Primary == null ? null : new Button { Content = d.Primary, HorizontalAlignment = HorizontalAlignment.Stretch };
        d.PrimaryButton = primary;
        var buttons = primary == null ? Ui.Columns(-1, -1) : Ui.Columns(-1, -1);
        if (primary == null) Ui.Place(buttons, close, 1, 8);
        else
        {
            Ui.Place(buttons, primary, 0);
            Ui.Place(buttons, close, 1, 8);
        }
        var accent = primary != null && d.PrimaryIsDefault ? primary : close;
        accent.Style = Ui.Style("Accent");
        accent.IsDefault = true;
        var footer = new Border { Padding = new Thickness(24), Child = buttons, CornerRadius = new CornerRadius(0, 0, 8, 8), BorderThickness = new Thickness(0, 1, 0, 0) };
        footer.SetResourceReference(Border.BackgroundProperty, "DialogFooter");
        footer.SetResourceReference(Border.BorderBrushProperty, "Divider");

        var stack = new Grid();
        stack.RowDefinitions.Add(new RowDefinition { Height = new GridLength(1, GridUnitType.Star) });
        stack.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        stack.Children.Add(top);
        Grid.SetRow(footer, 1);
        stack.Children.Add(footer);
        var card = new Border
        {
            Child = stack,
            CornerRadius = new CornerRadius(8),
            BorderThickness = new Thickness(1),
            MinWidth = 320,
            MaxWidth = d.MaxWidth,
            Margin = new Thickness(24),
            HorizontalAlignment = HorizontalAlignment.Center,
            VerticalAlignment = VerticalAlignment.Center,
            SnapsToDevicePixels = true,
        };
        card.SetResourceReference(Border.BackgroundProperty, "Dialog");
        card.SetResourceReference(Border.BorderBrushProperty, "FlyoutStroke");

        void Finish(bool result)
        {
            host.Children.Remove(card);
            host.Visibility = Visibility.Collapsed;
            if (before is IInputElement e && e.Focusable) Keyboard.Focus(e);
            done.TrySetResult(result);
        }
        close.Click += (_, _) => Finish(false);
        if (primary != null)
            primary.Click += async (_, _) =>
            {
                if (d.OnPrimary != null)
                {
                    primary.IsEnabled = false;
                    bool ok;
                    try { ok = await d.OnPrimary(d); }
                    finally { primary.IsEnabled = true; }
                    if (!ok) return;
                }
                Finish(true);
            };

        host.Children.Add(card);
        host.Visibility = Visibility.Visible;
        // The first field takes the keys, or else the button Enter presses.
        card.Loaded += (_, _) =>
        {
            if (!card.MoveFocus(new TraversalRequest(FocusNavigationDirection.First)) || Keyboard.FocusedElement is not TextBox)
                accent.Focus();
        };
        return done.Task;
    }
}
