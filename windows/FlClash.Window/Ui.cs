using Microsoft.UI;
using Microsoft.UI.Text;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Media;
using Microsoft.UI.Xaml.Shapes;
using Windows.UI;
using Windows.UI.Text;

namespace FlClash;

/// The colours of states, the same hues as on macOS, in the light and the
/// dark theme.
public enum Tint { Gray, Green, Yellow, Orange, Red, Blue, Teal, Indigo, Accent }

public static class Ui
{
    /// The theme the window draws in now; at start, the system's.
    public static bool Dark { get; set; } = new Windows.UI.ViewManagement.UISettings().GetColorValue(Windows.UI.ViewManagement.UIColorType.Background).R < 128;

    public static Color Color(Tint t) => t switch
    {
        Tint.Green => Dark ? Hex(0x30D158) : Hex(0x28A745),
        Tint.Yellow => Dark ? Hex(0xFFD60A) : Hex(0xE0B000),
        Tint.Orange => Dark ? Hex(0xFF9F0A) : Hex(0xE08600),
        Tint.Red => Dark ? Hex(0xFF453A) : Hex(0xE0352B),
        Tint.Blue => Dark ? Hex(0x0A84FF) : Hex(0x007AFF),
        Tint.Teal => Dark ? Hex(0x40C8E0) : Hex(0x30B0C7),
        Tint.Indigo => Dark ? Hex(0x5E5CE6) : Hex(0x5856D6),
        Tint.Accent => Find(Application.Current.Resources, "SystemAccentColor") is Color c ? c : Hex(0x0067C0),
        _ => Dark ? Hex(0x98989D) : Hex(0x8A8A8E),
    };

    public static Color Hex(uint rgb, byte alpha = 255) =>
        Windows.UI.Color.FromArgb(alpha, (byte)(rgb >> 16), (byte)(rgb >> 8), (byte)rgb);

    public static Color Alpha(Color c, double a) => Windows.UI.Color.FromArgb((byte)Math.Round(255 * a), c.R, c.G, c.B);

    public static SolidColorBrush Brush(Tint t, double alpha = 1) => new(Alpha(Color(t), alpha));
    public static SolidColorBrush Brush(Color c) => new(c);

    /// A brush of the system's theme, for the theme the window draws in now.
    /// The window is drawn again when the theme changes. A brush of a theme
    /// dictionary takes its colour from the application's theme, not from the
    /// dictionary it is in, so the brush is made anew from the colour of the
    /// dictionary of the theme asked for.
    public static Brush Res(string key)
    {
        if (key == "AccentTextFillColorPrimaryBrush")
            return new SolidColorBrush(new Windows.UI.ViewManagement.UISettings().GetColorValue(
                Dark ? Windows.UI.ViewManagement.UIColorType.AccentLight2 : Windows.UI.ViewManagement.UIColorType.AccentDark2));
        if (key.EndsWith("Brush") && Find(Application.Current.Resources, key[..^5]) is Color c) return new SolidColorBrush(c);
        return Find(Application.Current.Resources, key) as Brush ?? new SolidColorBrush(Microsoft.UI.Colors.Gray);
    }

    public static object? Find(ResourceDictionary d, string key)
    {
        if (d.ThemeDictionaries.Count > 0)
            foreach (var name in Dark ? new[] { "Dark", "Default" } : new[] { "Light", "Default" })
                if (d.ThemeDictionaries.TryGetValue(name, out var t) && t is ResourceDictionary theme && theme.TryGetValue(key, out var v)) return v;
        if (d.TryGetValue(key, out var direct)) return direct;
        foreach (var m in d.MergedDictionaries)
            if (Find(m, key) is { } found) return found;
        return null;
    }
    public static Brush Primary => Res("TextFillColorPrimaryBrush");
    public static Brush Secondary => Res("TextFillColorSecondaryBrush");
    public static Brush Tertiary => Res("TextFillColorTertiaryBrush");

    public static TextBlock Text(string text, double size = 14, FontWeight? weight = null, Brush? brush = null, bool wrap = false, bool trim = true)
    {
        var t = new TextBlock { Text = text, FontSize = size, VerticalAlignment = VerticalAlignment.Center };
        if (weight is { } w) t.FontWeight = w;
        if (brush != null) t.Foreground = brush;
        if (wrap) t.TextWrapping = TextWrapping.Wrap;
        else if (trim) t.TextTrimming = TextTrimming.CharacterEllipsis;
        return t;
    }

    public static FontWeight Semibold => FontWeights.SemiBold;

    public static FontIcon Glyph(string glyph, double size = 16, Brush? brush = null)
    {
        var icon = new FontIcon { Glyph = glyph, FontSize = size };
        if (brush != null) icon.Foreground = brush;
        return icon;
    }

    public static StackPanel Row(double spacing, params UIElement[] items)
    {
        var p = new StackPanel { Orientation = Orientation.Horizontal, Spacing = spacing };
        foreach (var i in items) p.Children.Add(i);
        return p;
    }

    public static StackPanel Column(double spacing, params UIElement[] items)
    {
        var p = new StackPanel { Orientation = Orientation.Vertical, Spacing = spacing };
        foreach (var i in items) p.Children.Add(i);
        return p;
    }

    public static Ellipse Dot(Color c, double side = 7) => new() { Width = side, Height = side, Fill = new SolidColorBrush(c), VerticalAlignment = VerticalAlignment.Center };

    /// Windows draws no flag emoji: the country is its two letters on a
    /// small plate, which reads the same at a glance in a list.
    public static FrameworkElement CountryPlate(string? code, double size = 10)
    {
        if (string.IsNullOrEmpty(code) || code.Length != 2) return new Border();
        var b = new Border
        {
            CornerRadius = new CornerRadius(3),
            Padding = new Thickness(3, 0, 3, 1),
            BorderThickness = new Thickness(1),
            BorderBrush = Res("ControlStrokeColorDefaultBrush"),
            Background = Res("ControlFillColorSecondaryBrush"),
            VerticalAlignment = VerticalAlignment.Center,
            HorizontalAlignment = HorizontalAlignment.Center,
            Child = new TextBlock { Text = code.ToUpperInvariant(), FontSize = size, FontWeight = FontWeights.SemiBold, Foreground = Secondary, CharacterSpacing = 40 },
        };
        ToolTipService.SetToolTip(b, Format.Country(code));
        return b;
    }

    /// A symbol on a rounded square of its colour, the way the things one
    /// chooses between are drawn.
    public static FrameworkElement Tile(string glyph, Tint tint, double side = 30)
    {
        var c = Color(tint);
        var top = Windows.UI.Color.FromArgb(255, (byte)(c.R + (255 - c.R) * 0.18), (byte)(c.G + (255 - c.G) * 0.18), (byte)(c.B + (255 - c.B) * 0.18));
        var fill = new LinearGradientBrush { StartPoint = new(0, 0), EndPoint = new(0, 1) };
        fill.GradientStops.Add(new GradientStop { Color = top, Offset = 0 });
        fill.GradientStops.Add(new GradientStop { Color = c, Offset = 1 });
        return new Border
        {
            Width = side,
            Height = side,
            CornerRadius = new CornerRadius(side * 0.26),
            Background = fill,
            VerticalAlignment = VerticalAlignment.Center,
            Child = new FontIcon { Glyph = glyph, FontSize = side * 0.5, Foreground = new SolidColorBrush(Colors.White) },
        };
    }

    /// What a page shows in place of an empty list.
    public sealed class Empty
    {
        public readonly StackPanel View;
        readonly TextBlock title, text;
        public readonly Button Button = new();

        public Empty(string glyph)
        {
            title = Text("", 16, Semibold, Secondary);
            title.HorizontalAlignment = HorizontalAlignment.Center;
            text = Text("", 12, null, Secondary, wrap: true);
            text.TextAlignment = TextAlignment.Center;
            text.MaxWidth = 340;
            var icon = Glyph(glyph, 34, Tertiary);
            icon.Margin = new Thickness(0, 0, 0, 4);
            Button.HorizontalAlignment = HorizontalAlignment.Center;
            Button.Margin = new Thickness(0, 6, 0, 0);
            View = Column(8, icon, title, text, Button);
            View.HorizontalAlignment = HorizontalAlignment.Center;
            View.VerticalAlignment = VerticalAlignment.Center;
            View.Margin = new Thickness(30);
        }

        public void Show(string title, string text, bool button)
        {
            this.title.Text = title;
            this.text.Text = text;
            this.text.Visibility = text.Length == 0 ? Visibility.Collapsed : Visibility.Visible;
            Button.Visibility = button ? Visibility.Visible : Visibility.Collapsed;
            View.Visibility = Visibility.Visible;
        }

        public void Hide() => View.Visibility = Visibility.Collapsed;
    }

    public static async Task Say(XamlRoot root, string title, string text)
    {
        var d = new ContentDialog
        {
            XamlRoot = root,
            Title = title,
            Content = new ScrollViewer { Content = Text(text, 14, null, null, wrap: true), MaxHeight = 360 },
            CloseButtonText = L.T("OK"),
            DefaultButton = ContentDialogButton.Close,
        };
        if (text.Length == 0) d.Content = null;
        await Show(d);
    }

    public static async Task<bool> Ask(XamlRoot root, string title, string text, string yes, bool destructive = false)
    {
        var d = new ContentDialog
        {
            XamlRoot = root,
            Title = title,
            Content = text.Length == 0 ? null : Text(text, 14, null, null, wrap: true),
            PrimaryButtonText = yes,
            CloseButtonText = L.T("Cancel"),
            DefaultButton = destructive ? ContentDialogButton.Close : ContentDialogButton.Primary,
        };
        return await Show(d) == ContentDialogResult.Primary;
    }

    static bool open;

    /// One dialog at a time: a second waits for the first to close.
    public static async Task<ContentDialogResult> Show(ContentDialog d)
    {
        while (open) await Task.Delay(100);
        open = true;
        try
        {
            d.RequestedTheme = Dark ? ElementTheme.Dark : ElementTheme.Light;
            return await d.ShowAsync();
        }
        finally { open = false; }
    }
}
