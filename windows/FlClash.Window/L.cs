using System.Globalization;
using System.Reflection;
using System.Text;
using System.Xml.Linq;

namespace FlClash;

/// The strings of the app, read from the files the macOS app uses as they
/// are: the key is the English text, the Russian file holds the translation.
public static class L
{
    static Dictionary<string, string> strings = new();
    static Dictionary<string, Dictionary<string, string>> plurals = new();
    static string lang = "";

    /// The language shown, as the strings files name it.
    public static string Lang => lang;

    /// The culture dates and numbers are written in.
    public static CultureInfo Culture { get; private set; } = CultureInfo.CurrentUICulture;

    /// Takes the language the agent shows its menu in; the window says the same.
    public static bool Use(string language)
    {
        if (string.IsNullOrEmpty(language)) language = CultureInfo.CurrentUICulture.TwoLetterISOLanguageName;
        if (language == lang) return false;
        lang = language;
        strings = language == "ru" ? ParseStrings(Read("ru.strings")) : new();
        plurals = ParsePlurals(Read(language == "ru" ? "ru.stringsdict" : "en.stringsdict"));
        try
        {
            Culture = language == "en" || language == CultureInfo.CurrentUICulture.TwoLetterISOLanguageName
                ? CultureInfo.CurrentCulture
                : CultureInfo.GetCultureInfo(language);
        }
        catch (CultureNotFoundException) { Culture = CultureInfo.CurrentCulture; }
        return true;
    }

    public static string T(string key) => strings.TryGetValue(key, out var s) ? s : key;

    /// Formats the text for key: %@ and %d in the order given.
    public static string F(string key, params object[] args) => Format(T(key), args);

    /// A key whose words follow a count, such as "%d servers".
    public static string N(string key, int n)
    {
        if (plurals.TryGetValue(key, out var forms))
        {
            var form = lang == "ru" ? RussianForm(n) : (n == 1 ? "one" : "other");
            if (forms.TryGetValue(form, out var s) || forms.TryGetValue("other", out s)) return Format(s, new object[] { n });
        }
        return F(key, n);
    }

    static string Format(string text, object[] args)
    {
        var sb = new StringBuilder();
        int next = 0;
        for (int i = 0; i < text.Length; i++)
        {
            if (text[i] == '%' && i + 1 < text.Length)
            {
                char c = text[i + 1];
                if (c == '%') { sb.Append('%'); i++; continue; }
                if (c == '@' || c == 'd')
                {
                    sb.Append(next < args.Length ? Convert.ToString(args[next], Culture) : "");
                    next++;
                    i++;
                    continue;
                }
            }
            sb.Append(text[i]);
        }
        return sb.ToString();
    }

    static string RussianForm(int n)
    {
        int n10 = n % 10, n100 = n % 100;
        if (n10 == 1 && n100 != 11) return "one";
        if (n10 >= 2 && n10 <= 4 && (n100 < 12 || n100 > 14)) return "few";
        return "many";
    }

    static string Read(string name)
    {
        using var stream = Assembly.GetExecutingAssembly().GetManifestResourceStream(name);
        if (stream == null) return "";
        using var reader = new StreamReader(stream, Encoding.UTF8);
        return reader.ReadToEnd();
    }

    /// Lines of the form "key" = "value"; with the escapes the files use.
    static Dictionary<string, string> ParseStrings(string text)
    {
        var map = new Dictionary<string, string>();
        foreach (var raw in text.Split('\n'))
        {
            var line = raw.Trim();
            if (!line.StartsWith("\"")) continue;
            int at = 0;
            var key = Quoted(line, ref at);
            if (key == null) continue;
            while (at < line.Length && line[at] != '=') at++;
            at++;
            while (at < line.Length && line[at] != '"') at++;
            var value = Quoted(line, ref at);
            if (value != null) map[key] = value;
        }
        return map;
    }

    static string? Quoted(string s, ref int at)
    {
        if (at >= s.Length || s[at] != '"') return null;
        var sb = new StringBuilder();
        for (int i = at + 1; i < s.Length; i++)
        {
            char c = s[i];
            if (c == '\\' && i + 1 < s.Length)
            {
                char e = s[++i];
                sb.Append(e switch { 'n' => '\n', 't' => '\t', _ => e });
                continue;
            }
            if (c == '"') { at = i + 1; return sb.ToString(); }
            sb.Append(c);
        }
        return null;
    }

    /// The plural forms of a stringsdict: key → form → text.
    static Dictionary<string, Dictionary<string, string>> ParsePlurals(string text)
    {
        var map = new Dictionary<string, Dictionary<string, string>>();
        if (text.Length == 0) return map;
        // The document type names a file on Apple's site; nothing needs it.
        text = System.Text.RegularExpressions.Regex.Replace(text, "<!DOCTYPE[^>]*>", "");
        var root = XDocument.Parse(text).Root?.Element("dict");
        if (root == null) return map;
        foreach (var (key, entry) in Pairs(root))
        {
            var forms = new Dictionary<string, string>();
            foreach (var (_, value) in Pairs(entry))
            {
                if (value.Name != "dict") continue;
                foreach (var (form, t) in Pairs(value))
                    if (t.Name == "string" && !form.StartsWith("NSString")) forms[form] = t.Value;
            }
            map[key] = forms;
        }
        return map;
    }

    static IEnumerable<(string, XElement)> Pairs(XElement dict)
    {
        var items = dict.Elements().ToList();
        for (int i = 0; i + 1 < items.Count; i += 2) yield return (items[i].Value, items[i + 1]);
    }
}
