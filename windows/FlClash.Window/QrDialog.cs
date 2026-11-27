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
        FrameworkElement picture;
        try
        {
            // A code is read against white, whatever the theme.
            picture = new Border
            {
                Background = new SolidColorBrush(Colors.White),
                CornerRadius = new CornerRadius(12),
                Padding = new Thickness(10),
                HorizontalAlignment = HorizontalAlignment.Center,
                Child = Code(link, 280),
            };
        }
        catch (QRCoder.Exceptions.DataTooLongException)
        {
            // The link is still there to be copied and passed on another way.
            picture = Ui.Text(L.T("The link is too long for a QR code. Copy it and pass it on another way."), 14, null, null, wrap: true);
            picture.MaxWidth = 300;
        }
        var note = Ui.Text(L.T("The code holds the keys of this server. Show it only to devices you trust."), 12, null, Ui.Secondary, wrap: true);
        note.TextAlignment = TextAlignment.Center;
        note.MaxWidth = 300;
        note.HorizontalAlignment = HorizontalAlignment.Center;
        var problem = Ui.Text("", 12, null, Ui.Brush(Tint.Red), wrap: true);
        problem.MaxWidth = 300;
        problem.HorizontalAlignment = HorizontalAlignment.Center;
        problem.Visibility = Visibility.Collapsed;
        var dialog = new ContentDialog
        {
            XamlRoot = root,
            Title = name,
            Content = Ui.Column(14, picture, note, problem),
            PrimaryButtonText = L.T("Copy Link"),
            CloseButtonText = L.T("Done"),
            DefaultButton = ContentDialogButton.Close,
        };
        dialog.PrimaryButtonClick += (_, e) =>
        {
            // The dialog stays: the link is copied beside the code, not instead of it.
            e.Cancel = true;
            var error = Ui.Copy(link, secret: true);
            problem.Text = error == null ? "" : L.T("Nothing was copied") + ": " + error;
            problem.Visibility = error == null ? Visibility.Collapsed : Visibility.Visible;
        };
        await Ui.Show(dialog);
    }
}
