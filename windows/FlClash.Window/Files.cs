using Microsoft.UI.Xaml;
using Microsoft.Windows.Storage.Pickers;
using Windows.ApplicationModel.DataTransfer;
using Windows.Graphics.Imaging;
using Windows.Storage;
using Windows.Storage.Streams;
using ZXing;
using ZXing.Common;
using ZXing.Multi.QrCode;

namespace FlClash;

/// Files the person chooses or drops, and the QR codes in pictures.
public static class Files
{
    /// A profile or a rules file is small; anything larger is not one.
    public const long Limit = 1 << 20;

    static readonly string[] pictures = [".png", ".jpg", ".jpeg", ".bmp", ".gif", ".tif", ".tiff"];

    public static bool IsPicture(string path) => pictures.Contains(Path.GetExtension(path).ToLowerInvariant());

    /// Asks for a file to open; null when none was chosen or the dialog failed,
    /// which is said.
    public static async Task<string?> PickOpen(MainWindow window, params string[] types)
    {
        try
        {
            var picker = new FileOpenPicker(window.AppWindow.Id);
            foreach (var t in types) picker.FileTypeFilter.Add(t);
            return (await picker.PickSingleFileAsync())?.Path;
        }
        catch (Exception e)
        {
            await Ui.Say(window.Content.XamlRoot, L.T("The file dialog did not open"), e.Message);
            return null;
        }
    }

    /// Asks where to save a file; null when nowhere was chosen or the dialog failed.
    public static async Task<string?> PickSave(MainWindow window, string name, string kind, string type)
    {
        try
        {
            var picker = new FileSavePicker(window.AppWindow.Id) { SuggestedFileName = name, DefaultFileExtension = type };
            picker.FileTypeChoices.Add(kind, new List<string> { type });
            return (await picker.PickSaveFileAsync())?.Path;
        }
        catch (Exception e)
        {
            await Ui.Say(window.Content.XamlRoot, L.T("The file dialog did not open"), e.Message);
            return null;
        }
    }

    /// The text of a small file, or what is wrong with it.
    public static async Task<(string? Text, string? Problem)> Read(string path)
    {
        try
        {
            if (new FileInfo(path).Length > Limit)
                return (null, L.F("“%@” is larger than 1 MB: a list of servers or a rules file is smaller.", Path.GetFileName(path)));
            return (await File.ReadAllTextAsync(path), null);
        }
        catch (Exception e) when (e is IOException or UnauthorizedAccessException or ArgumentException or NotSupportedException)
        {
            return (null, L.T("The file could not be read") + ": " + Path.GetFileName(path));
        }
    }

    /// The path of the first file dropped, if files were dropped.
    public static async Task<string?> DroppedFile(DataPackageView data)
    {
        if (!data.Contains(StandardDataFormats.StorageItems)) return null;
        var items = await data.GetStorageItemsAsync();
        return items.FirstOrDefault() is StorageFile { Path.Length: > 0 } file ? file.Path : null;
    }

    /// The text dropped or pasted, if it is not empty.
    public static async Task<string?> Text(DataPackageView data)
    {
        if (!data.Contains(StandardDataFormats.Text)) return null;
        var text = await data.GetTextAsync();
        return string.IsNullOrWhiteSpace(text) ? null : text;
    }

    /// Reads the QR codes in a picture: a screenshot, a photo, a saved code.
    public static async Task<List<string>> Codes(IRandomAccessStream stream)
    {
        var decoder = await BitmapDecoder.CreateAsync(stream);
        uint w = decoder.PixelWidth, h = decoder.PixelHeight;
        var transform = new BitmapTransform();
        // A large photo is made smaller; a code needs far fewer pixels.
        double scale = Math.Min(1, 4000.0 / Math.Max(w, h));
        if (scale < 1)
        {
            w = (uint)(w * scale);
            h = (uint)(h * scale);
            transform.ScaledWidth = w;
            transform.ScaledHeight = h;
            transform.InterpolationMode = BitmapInterpolationMode.Fant;
        }
        var data = await decoder.GetPixelDataAsync(BitmapPixelFormat.Bgra8, BitmapAlphaMode.Ignore, transform,
            ExifOrientationMode.IgnoreExifOrientation, ColorManagementMode.DoNotColorManage);
        var source = new RGBLuminanceSource(data.DetachPixelData(), (int)w, (int)h, RGBLuminanceSource.BitmapFormat.BGRA32);
        var hints = new Dictionary<DecodeHintType, object>
        {
            [DecodeHintType.TRY_HARDER] = true,
            [DecodeHintType.POSSIBLE_FORMATS] = new List<BarcodeFormat> { BarcodeFormat.QR_CODE },
        };
        var found = new QRCodeMultiReader().decodeMultiple(new BinaryBitmap(new HybridBinarizer(source)), hints);
        return found?.Select(r => r.Text).Where(t => !string.IsNullOrWhiteSpace(t)).Distinct().ToList() ?? [];
    }

    /// The QR codes in a picture file.
    public static async Task<List<string>> Codes(string path)
    {
        var file = await StorageFile.GetFileFromPathAsync(path);
        using var stream = await file.OpenReadAsync();
        return await Codes(stream);
    }
}
