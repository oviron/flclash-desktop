using System.Windows;
using System.Windows.Media;
using System.Windows.Media.Imaging;
using Microsoft.Win32;
using ZXing;
using ZXing.Common;
using ZXing.Multi.QrCode;

namespace FlClash;

/// Files the person chooses or drops, and the QR codes in pictures.
public static class Files
{
    /// A profile or a rules file is small; anything larger is not one.
    public const long Limit = 1 << 20;

    static readonly string[] pictures = { ".png", ".jpg", ".jpeg", ".bmp", ".gif", ".tif", ".tiff" };

    public static bool IsPicture(string path) => pictures.Contains(Path.GetExtension(path).ToLowerInvariant());

    /// Asks for a file to open; null when none was chosen. "*" in the types
    /// offers every file too.
    public static string? PickOpen(Window owner, params string[] types)
    {
        var named = types.Where(t => t != "*").Select(t => "*" + t).ToList();
        var filter = string.Join(";", named) + "|" + string.Join(";", named);
        if (types.Contains("*")) filter += "|" + L.T("All Files") + "|*.*";
        var dialog = new OpenFileDialog { Filter = filter, CheckFileExists = true };
        return dialog.ShowDialog(owner) == true ? dialog.FileName : null;
    }

    /// Asks where to save a file; null when nowhere was chosen.
    public static string? PickSave(Window owner, string name, string kind, string type)
    {
        var dialog = new SaveFileDialog { FileName = name, DefaultExt = type, Filter = kind + "|*" + type, AddExtension = true, OverwritePrompt = true };
        return dialog.ShowDialog(owner) == true ? dialog.FileName : null;
    }

    /// The text of a small file, or what is wrong with it.
    public static (string? Text, string? Problem) Read(string path)
    {
        try
        {
            if (new FileInfo(path).Length > Limit)
                return (null, L.F("“%@” is larger than 1 MB: a list of servers or a rules file is smaller.", Path.GetFileName(path)));
            return (File.ReadAllText(path), null);
        }
        catch (Exception e) when (e is IOException or UnauthorizedAccessException or ArgumentException or NotSupportedException)
        {
            return (null, L.T("The file could not be read") + ": " + Path.GetFileName(path));
        }
    }

    /// The path of the first file dropped, if files were dropped.
    public static string? DroppedFile(IDataObject data) =>
        data.GetDataPresent(DataFormats.FileDrop) && data.GetData(DataFormats.FileDrop) is string[] { Length: > 0 } files ? files[0] : null;

    /// The text dropped or pasted, if it is not empty.
    public static string? Text(IDataObject data)
    {
        if (!data.GetDataPresent(DataFormats.UnicodeText)) return null;
        var text = data.GetData(DataFormats.UnicodeText) as string;
        return string.IsNullOrWhiteSpace(text) ? null : text;
    }

    /// Reads the QR codes in a picture: a screenshot, a photo, a saved code.
    public static List<string> Codes(BitmapSource picture)
    {
        // A large photo is made smaller; a code needs far fewer pixels.
        double scale = Math.Min(1, 4000.0 / Math.Max(picture.PixelWidth, picture.PixelHeight));
        BitmapSource source = scale < 1 ? new TransformedBitmap(picture, new ScaleTransform(scale, scale)) : picture;
        var bgra = new FormatConvertedBitmap(source, PixelFormats.Bgra32, null, 0);
        int w = bgra.PixelWidth, h = bgra.PixelHeight;
        var pixels = new byte[w * h * 4];
        bgra.CopyPixels(pixels, w * 4, 0);
        var luminance = new RGBLuminanceSource(pixels, w, h, RGBLuminanceSource.BitmapFormat.BGRA32);
        var hints = new Dictionary<DecodeHintType, object>
        {
            [DecodeHintType.TRY_HARDER] = true,
            [DecodeHintType.POSSIBLE_FORMATS] = new List<BarcodeFormat> { BarcodeFormat.QR_CODE },
        };
        var found = new QRCodeMultiReader().decodeMultiple(new BinaryBitmap(new HybridBinarizer(luminance)), hints);
        return found?.Select(r => r.Text).Where(t => !string.IsNullOrWhiteSpace(t)).Distinct().ToList() ?? new List<string>();
    }

    /// The QR codes in a picture file.
    public static List<string> Codes(string path)
    {
        using var stream = File.OpenRead(path);
        var frame = BitmapDecoder.Create(stream, BitmapCreateOptions.IgnoreColorProfile, BitmapCacheOption.OnLoad).Frames[0];
        return Codes(frame);
    }
}
