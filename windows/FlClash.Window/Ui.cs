using System.Globalization;
using System.Runtime.InteropServices;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Data;
using System.Windows.Interop;
using System.Windows.Markup;
using System.Windows.Media;
using System.Windows.Media.Animation;
using Microsoft.Win32;
using Shapes = System.Windows.Shapes;

namespace FlClash;

/// The colours of states, the same hues as on macOS, in the light and the
/// dark theme.
public enum Tint { Gray, Green, Yellow, Orange, Red, Blue, Teal, Indigo, Accent }

public static class Ui
{
    /// The theme the window draws in now; at start, the system's.
    public static bool Dark { get; set; } = SystemDark();

    /// Whether the system shows apps dark.
    public static bool SystemDark()
    {
        try
        {
            using var key = Registry.CurrentUser.OpenSubKey(@"Software\Microsoft\Windows\CurrentVersion\Themes\Personalize");
            return key?.GetValue("AppsUseLightTheme") is int light && light == 0;
        }
        catch (Exception) { return false; }
    }

    // The accent of the system in its shades, lightest first, as Windows
    // keeps them: Light3, Light2, Light1, the accent, Dark1, Dark2, Dark3.
    static Color[] AccentShades()
    {
        try
        {
            using var key = Registry.CurrentUser.OpenSubKey(@"Software\Microsoft\Windows\CurrentVersion\Explorer\Accent");
            if (key?.GetValue("AccentPalette") is byte[] p && p.Length >= 28)
                return Enumerable.Range(0, 7).Select(i => System.Windows.Media.Color.FromRgb(p[i * 4], p[i * 4 + 1], p[i * 4 + 2])).ToArray();
        }
        catch (Exception) { }
        // The default blue of Windows 11.
        return new[] { Hex(0x99EBFF), Hex(0x60CDFF), Hex(0x4CC2FF), Hex(0x0078D4), Hex(0x005FB8), Hex(0x004A83), Hex(0x003D73) };
    }

    static Color accent = Hex(0x0067C0);

    /// Puts the brushes of the theme into the application's resources; the
    /// styles take them from there.
    public static void ApplyTheme()
    {
        var shades = AccentShades();
        var r = Application.Current.Resources;
        void Set(string key, uint argb) => Put(key, System.Windows.Media.Color.FromArgb((byte)(argb >> 24), (byte)(argb >> 16), (byte)(argb >> 8), (byte)argb));
        void Put(string key, Color c)
        {
            var b = new SolidColorBrush(c);
            b.Freeze();
            r[key] = b;
        }
        if (Dark)
        {
            accent = shades[1];
            Set("Text1", 0xFFFFFFFF); Set("Text2", 0xC8FFFFFF); Set("Text3", 0x8BFFFFFF); Set("TextDisabled", 0x5DFFFFFF);
            Set("Card", 0x0DFFFFFF); Set("CardStroke", 0x1A000000); Set("Divider", 0x15FFFFFF);
            Set("Fill", 0x0FFFFFFF); Set("FillHover", 0x15FFFFFF); Set("FillPressed", 0x08FFFFFF); Set("FillDisabled", 0x0BFFFFFF);
            Set("FillSecondary", 0x15FFFFFF); Set("Stroke", 0x14FFFFFF);
            Set("Subtle", 0x0FFFFFFF); Set("SubtlePressed", 0x0AFFFFFF); Set("Selected", 0x12FFFFFF); Set("SelectedHover", 0x1AFFFFFF);
            Set("Field", 0x0FFFFFFF); Set("FieldFocus", 0xB31E1E1E);
            Set("Flyout", 0xFF2C2C2C); Set("FlyoutStroke", 0x33FFFFFF);
            Set("Dialog", 0xFF2B2B2B); Set("DialogFooter", 0xFF202020); Set("Smoke", 0x4D000000);
            Set("Solid", 0xFF202020); Set("Thumb", 0x8BFFFFFF);
            Put("Accent", accent); Put("AccentHover", Alpha(accent, 0.9)); Put("AccentPressed", Alpha(accent, 0.8));
            Put("OnAccent", Colors.Black); Put("AccentText", shades[0]);
        }
        else
        {
            accent = shades[4];
            Set("Text1", 0xE4000000); Set("Text2", 0x9E000000); Set("Text3", 0x72000000); Set("TextDisabled", 0x5C000000);
            Set("Card", 0xB3FFFFFF); Set("CardStroke", 0x12000000); Set("Divider", 0x14000000);
            Set("Fill", 0xB3FFFFFF); Set("FillHover", 0x80F9F9F9); Set("FillPressed", 0x4DF9F9F9); Set("FillDisabled", 0x4DF9F9F9);
            Set("FillSecondary", 0x80F9F9F9); Set("Stroke", 0x24000000);
            Set("Subtle", 0x09000000); Set("SubtlePressed", 0x06000000); Set("Selected", 0x0B000000); Set("SelectedHover", 0x10000000);
            Set("Field", 0xB3FFFFFF); Set("FieldFocus", 0xFFFFFFFF);
            Set("Flyout", 0xFFF9F9F9); Set("FlyoutStroke", 0x24000000);
            Set("Dialog", 0xFFFFFFFF); Set("DialogFooter", 0xFFF3F3F3); Set("Smoke", 0x4D000000);
            Set("Solid", 0xFFF3F3F3); Set("Thumb", 0x72000000);
            Put("Accent", accent); Put("AccentHover", Alpha(accent, 0.9)); Put("AccentPressed", Alpha(accent, 0.8));
            Put("OnAccent", Colors.White); Put("AccentText", shades[5]);
        }
    }

    /// The system draws the frame in the theme of the window and, on Windows
    /// 11, Mica behind it; elsewhere the window is a plain colour.
    public static void Backdrop(Window w)
    {
        var hwnd = new WindowInteropHelper(w).Handle;
        if (hwnd == IntPtr.Zero) return;
        int dark = Dark ? 1 : 0;
        DwmSetWindowAttribute(hwnd, 20 /* DWMWA_USE_IMMERSIVE_DARK_MODE */, ref dark, sizeof(int));
        bool mica = false;
        if (Environment.OSVersion.Version.Build >= 22621)
        {
            int type = 2; // DWMSBT_MAINWINDOW: Mica
            mica = DwmSetWindowAttribute(hwnd, 38 /* DWMWA_SYSTEMBACKDROP_TYPE */, ref type, sizeof(int)) == 0;
        }
        if (mica && HwndSource.FromHwnd(hwnd) is { CompositionTarget: { } target })
        {
            target.BackgroundColor = Colors.Transparent;
            var margins = new Margins { Left = -1, Right = -1, Top = -1, Bottom = -1 };
            DwmExtendFrameIntoClientArea(hwnd, ref margins);
            w.Background = Brushes.Transparent;
        }
        else w.SetResourceReference(Control.BackgroundProperty, "Solid");
    }

    [StructLayout(LayoutKind.Sequential)]
    struct Margins { public int Left, Right, Top, Bottom; }

    [DllImport("dwmapi.dll")] static extern int DwmSetWindowAttribute(IntPtr hwnd, int attribute, ref int value, int size);
    [DllImport("dwmapi.dll")] static extern int DwmExtendFrameIntoClientArea(IntPtr hwnd, ref Margins margins);

    /// Reads the styles of the controls.
    public static ResourceDictionary Styles()
    {
        using var stream = typeof(Ui).Assembly.GetManifestResourceStream("Theme.xaml")!;
        return (ResourceDictionary)XamlReader.Load(stream);
    }

    public static Color Color(Tint t) => t switch
    {
        Tint.Green => Dark ? Hex(0x30D158) : Hex(0x28A745),
        Tint.Yellow => Dark ? Hex(0xFFD60A) : Hex(0xE0B000),
        Tint.Orange => Dark ? Hex(0xFF9F0A) : Hex(0xE08600),
        Tint.Red => Dark ? Hex(0xFF453A) : Hex(0xE0352B),
        Tint.Blue => Dark ? Hex(0x0A84FF) : Hex(0x007AFF),
        Tint.Teal => Dark ? Hex(0x40C8E0) : Hex(0x30B0C7),
        Tint.Indigo => Dark ? Hex(0x5E5CE6) : Hex(0x5856D6),
        Tint.Accent => accent,
        _ => Dark ? Hex(0x98989D) : Hex(0x8A8A8E),
    };

    public static Color Hex(uint rgb, byte alpha = 255) =>
        System.Windows.Media.Color.FromArgb(alpha, (byte)(rgb >> 16), (byte)(rgb >> 8), (byte)rgb);

    public static Color Alpha(Color c, double a) => System.Windows.Media.Color.FromArgb((byte)Math.Round(255 * a), c.R, c.G, c.B);

    public static SolidColorBrush Brush(Tint t, double alpha = 1) => new(Alpha(Color(t), alpha));
    public static SolidColorBrush Brush(Color c) => new(c);

    /// A brush of the theme the window draws in now. The window is drawn again
    /// when the theme changes.
    public static Brush B(string key) => Application.Current.Resources[key] as Brush ?? Brushes.Gray;

    public static Brush Primary => B("Text1");
    public static Brush Secondary => B("Text2");
    public static Brush Tertiary => B("Text3");

    // What the styles take from the controls they draw.
    public static readonly DependencyProperty CornerProperty = DependencyProperty.RegisterAttached("Corner", typeof(CornerRadius), typeof(Ui), new FrameworkPropertyMetadata(new CornerRadius(4)));
    public static CornerRadius GetCorner(DependencyObject o) => (CornerRadius)o.GetValue(CornerProperty);
    public static void SetCorner(DependencyObject o, CornerRadius v) => o.SetValue(CornerProperty, v);

    public static readonly DependencyProperty HoverProperty = DependencyProperty.RegisterAttached("Hover", typeof(Brush), typeof(Ui), new FrameworkPropertyMetadata(null));
    public static Brush? GetHover(DependencyObject o) => (Brush?)o.GetValue(HoverProperty);
    public static void SetHover(DependencyObject o, Brush? v) => o.SetValue(HoverProperty, v);

    public static readonly DependencyProperty PressedProperty = DependencyProperty.RegisterAttached("Pressed", typeof(Brush), typeof(Ui), new FrameworkPropertyMetadata(null));
    public static Brush? GetPressed(DependencyObject o) => (Brush?)o.GetValue(PressedProperty);
    public static void SetPressed(DependencyObject o, Brush? v) => o.SetValue(PressedProperty, v);

    public static readonly DependencyProperty PlaceholderProperty = DependencyProperty.RegisterAttached("Placeholder", typeof(string), typeof(Ui), new FrameworkPropertyMetadata(""));
    public static string GetPlaceholder(DependencyObject o) => (string)o.GetValue(PlaceholderProperty);
    public static void SetPlaceholder(DependencyObject o, string v) => o.SetValue(PlaceholderProperty, v);

    /// The mark of a chosen row sits left of the row's padding.
    public static readonly IValueConverter Outdent = new OutdentConverter();

    sealed class OutdentConverter : IValueConverter
    {
        public object Convert(object value, Type t, object p, CultureInfo c) => value is Thickness th ? new Thickness(-th.Left + 2, 0, 0, 0) : new Thickness();
        public object ConvertBack(object value, Type t, object p, CultureInfo c) => throw new NotSupportedException();
    }

    public static Style Style(string key) => (Style)Application.Current.Resources[key];

    public static TextBox Field(string placeholder = "", string text = "", double width = double.NaN)
    {
        var box = new TextBox { Text = text, Width = width };
        SetPlaceholder(box, placeholder);
        return box;
    }

    /// Puts text on the clipboard and says what went wrong, if anything. A
    /// secret — a link with the keys of a server — stays out of the clipboard
    /// history and off the person's other devices.
    public static string? Copy(string text, bool secret)
    {
        var data = new DataObject();
        data.SetText(text, TextDataFormat.UnicodeText);
        if (secret)
        {
            // The formats Windows reads for that: a DWORD of zero.
            data.SetData("CanIncludeInClipboardHistory", new MemoryStream(new byte[4]));
            data.SetData("CanUploadToCloudClipboard", new MemoryStream(new byte[4]));
        }
        for (int attempt = 0; ; attempt++)
        {
            try
            {
                // What was copied stays after the window is closed.
                Clipboard.SetDataObject(data, true);
                return null;
            }
            catch (COMException) when (attempt < 4) { Thread.Sleep(50); }
            catch (COMException) { return L.T("The clipboard is busy with another program. Try again."); }
            catch (Exception e) { return e.Message; }
        }
    }

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

    public static FontFamily Icons => (FontFamily)Application.Current.Resources["Icons"];

    /// A symbol of the system's icon font.
    public static TextBlock Glyph(string glyph, double size = 16, Brush? brush = null)
    {
        var icon = new TextBlock { Text = glyph, FontFamily = Icons, FontSize = size, VerticalAlignment = VerticalAlignment.Center };
        if (brush != null) icon.Foreground = brush;
        return icon;
    }

    public static StackPanel Row(double spacing, params UIElement[] items) => Stack(Orientation.Horizontal, spacing, items);

    public static StackPanel Column(double spacing, params UIElement[] items) => Stack(Orientation.Vertical, spacing, items);

    static StackPanel Stack(Orientation o, double spacing, UIElement[] items)
    {
        var p = new StackPanel { Orientation = o };
        foreach (var i in items) Add(p, i, spacing);
        return p;
    }

    /// Adds to a stack with the space before it that the stack keeps.
    public static void Add(StackPanel p, UIElement e, double spacing)
    {
        if (p.Children.Count > 0 && spacing > 0 && e is FrameworkElement f)
        {
            var m = f.Margin;
            f.Margin = p.Orientation == Orientation.Horizontal ? new Thickness(m.Left + spacing, m.Top, m.Right, m.Bottom) : new Thickness(m.Left, m.Top + spacing, m.Right, m.Bottom);
        }
        p.Children.Add(e);
    }

    /// A grid with columns of these widths (negative: a share of the rest).
    public static Grid Columns(params double[] widths)
    {
        var g = new Grid();
        foreach (var w in widths)
            g.ColumnDefinitions.Add(new ColumnDefinition { Width = double.IsNaN(w) ? GridLength.Auto : w < 0 ? new GridLength(-w, GridUnitType.Star) : new GridLength(w) });
        return g;
    }

    public const double Auto = double.NaN;

    /// Puts an element in a column, with the space before every column but the first.
    public static T Place<T>(Grid g, T e, int column, double spacing = 0) where T : FrameworkElement
    {
        Grid.SetColumn(e, column);
        if (column > 0 && spacing > 0) e.Margin = new Thickness(e.Margin.Left + spacing, e.Margin.Top, e.Margin.Right, e.Margin.Bottom);
        g.Children.Add(e);
        return e;
    }

    public static Shapes.Ellipse Dot(Color c, double side = 7) => new() { Width = side, Height = side, Fill = new SolidColorBrush(c), VerticalAlignment = VerticalAlignment.Center };

    public static void Tip(FrameworkElement e, string? text) => e.ToolTip = string.IsNullOrEmpty(text) ? null : text;

    /// Windows draws no flag emoji: the country is its two letters on a
    /// small plate, which reads the same at a glance in a list.
    public static FrameworkElement CountryPlate(string? code, double size = 10)
    {
        if (string.IsNullOrEmpty(code) || code!.Length != 2) return new Border();
        var b = new Border
        {
            CornerRadius = new CornerRadius(3),
            Padding = new Thickness(3, 0, 3, 1),
            BorderThickness = new Thickness(1),
            BorderBrush = B("Stroke"),
            Background = B("FillSecondary"),
            VerticalAlignment = VerticalAlignment.Center,
            HorizontalAlignment = HorizontalAlignment.Center,
            Child = new TextBlock { Text = code.ToUpperInvariant(), FontSize = size, FontWeight = FontWeights.SemiBold, Foreground = Secondary },
        };
        Tip(b, Format.Country(code));
        return b;
    }

    /// A symbol on a rounded square of its colour, the way the things one
    /// chooses between are drawn.
    public static FrameworkElement Tile(string glyph, Tint tint, double side = 30)
    {
        var c = Color(tint);
        var top = System.Windows.Media.Color.FromArgb(255, (byte)(c.R + (255 - c.R) * 0.18), (byte)(c.G + (255 - c.G) * 0.18), (byte)(c.B + (255 - c.B) * 0.18));
        var fill = new LinearGradientBrush { StartPoint = new Point(0, 0), EndPoint = new Point(0, 1) };
        fill.GradientStops.Add(new GradientStop(top, 0));
        fill.GradientStops.Add(new GradientStop(c, 1));
        return new Border
        {
            Width = side,
            Height = side,
            CornerRadius = new CornerRadius(side * 0.26),
            Background = fill,
            VerticalAlignment = VerticalAlignment.Center,
            Child = new TextBlock { Text = glyph, FontFamily = Icons, FontSize = side * 0.5, Foreground = Brushes.White, HorizontalAlignment = HorizontalAlignment.Center, VerticalAlignment = VerticalAlignment.Center },
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
            icon.HorizontalAlignment = HorizontalAlignment.Center;
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

    /// A menu of commands, opened from a button or a row.
    public static MenuItem Item(string text, Action action, string? keys = null, bool enabled = true)
    {
        var item = new MenuItem { Header = text, IsEnabled = enabled };
        if (keys != null) item.InputGestureText = keys;
        item.Click += (_, _) => action();
        return item;
    }

    /// Opens a menu under the button, as a drop-down button does.
    public static void DropDown(Button button, Func<ContextMenu> menu)
    {
        button.Click += (_, _) =>
        {
            var m = menu();
            m.PlacementTarget = button;
            m.Placement = System.Windows.Controls.Primitives.PlacementMode.Bottom;
            m.VerticalOffset = 4;
            m.IsOpen = true;
        };
    }

    public static Task Say(string title, string text) =>
        Dialogs.Show(new Dialog { Title = title, Content = text.Length == 0 ? null : new ScrollViewer { Content = Text(text, 14, null, null, wrap: true), MaxHeight = 360, VerticalScrollBarVisibility = ScrollBarVisibility.Auto }, Close = L.T("OK"), Primary = null });

    public static async Task<bool> Ask(string title, string text, string yes, bool destructive = false) =>
        await Dialogs.Show(new Dialog { Title = title, Content = text.Length == 0 ? null : Text(text, 14, null, null, wrap: true), Primary = yes, PrimaryIsDefault = !destructive });
}

/// A spinning ring: something is on its way.
public sealed class Spinner
{
    public readonly Shapes.Path View;
    readonly RotateTransform turn = new();
    bool active;

    public Spinner(double side = 18)
    {
        double r = side / 2 - 1.5;
        var figure = new PathFigure { StartPoint = new Point(side / 2, side / 2 - r), IsClosed = false };
        figure.Segments.Add(new ArcSegment(new Point(side / 2 - r, side / 2), new Size(r, r), 0, true, SweepDirection.Clockwise, true));
        var geometry = new PathGeometry();
        geometry.Figures.Add(figure);
        View = new Shapes.Path
        {
            Data = geometry,
            Width = side,
            Height = side,
            StrokeThickness = 2,
            StrokeStartLineCap = PenLineCap.Round,
            StrokeEndLineCap = PenLineCap.Round,
            RenderTransform = turn,
            RenderTransformOrigin = new Point(0.5, 0.5),
            VerticalAlignment = VerticalAlignment.Center,
            Visibility = Visibility.Collapsed,
        };
        View.SetResourceReference(Shapes.Shape.StrokeProperty, "Accent");
    }

    public bool IsActive
    {
        get => active;
        set
        {
            if (value == active) return;
            active = value;
            // The ring turns only while it shows: no work for nothing.
            turn.BeginAnimation(RotateTransform.AngleProperty, value ? new DoubleAnimation(0, 360, TimeSpan.FromSeconds(1)) { RepeatBehavior = RepeatBehavior.Forever } : null);
            View.Visibility = value ? Visibility.Visible : Visibility.Collapsed;
        }
    }

    public static Shapes.Path Running(double side)
    {
        var s = new Spinner(side) { IsActive = true };
        s.View.HorizontalAlignment = HorizontalAlignment.Left;
        return s.View;
    }
}
