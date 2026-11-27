using Microsoft.UI;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Media;
using Microsoft.UI.Xaml.Shapes;
using QRCoder;

namespace FlClash;

/// A server as a QR code, for another device to scan.
public static class QrDialog
{
    /// Draws text as a QR code, sharp at any size.
    public static FrameworkElement Code(string text, double side)
    {
        using var generator = new QRCodeGenerator();
        var data = generator.CreateQrCode(text, QRCodeGenerator.ECCLevel.M);
        var matrix = data.ModuleMatrix;
        int n = matrix.Count;
        double cell = side / n;
        var group = new GeometryGroup { FillRule = FillRule.Nonzero };
        for (int y = 0; y < n; y++)
            for (int x = 0; x < n; x++)
                if (matrix[y][x])
                    group.Children.Add(new RectangleGeometry { Rect = new Windows.Foundation.Rect(x * cell, y * cell, cell + 0.3, cell + 0.3) });
        return new Microsoft.UI.Xaml.Shapes.Path { Data = group, Fill = new SolidColorBrush(Colors.Black), Width = side, Height = side };
    }

    public static async Task Show(XamlRoot root, string name, string link)
    {
        // A code is read against white, whatever the theme.
        var plate = new Border
        {
            Background = new SolidColorBrush(Colors.White),
            CornerRadius = new CornerRadius(12),
            Padding = new Thickness(10),
            HorizontalAlignment = HorizontalAlignment.Center,
            Child = Code(link, 280),
        };
        var note = Ui.Text(L.T("The code holds the keys of this server. Show it only to devices you trust."), 12, null, Ui.Secondary, wrap: true);
        note.TextAlignment = TextAlignment.Center;
        note.MaxWidth = 300;
        note.HorizontalAlignment = HorizontalAlignment.Center;
        var dialog = new ContentDialog
        {
            XamlRoot = root,
            Title = name,
            Content = Ui.Column(14, plate, note),
            PrimaryButtonText = L.T("Copy Link"),
            CloseButtonText = L.T("Done"),
            DefaultButton = ContentDialogButton.Close,
        };
        dialog.PrimaryButtonClick += (_, e) =>
        {
            ServersPage.Copy(link);
            e.Cancel = true;
        };
        await Ui.Show(dialog);
    }
}
