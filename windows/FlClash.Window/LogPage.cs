using System.Windows;
using System.Windows.Controls;
using System.Windows.Documents;
using System.Windows.Media;
using System.Windows.Threading;

namespace FlClash;

/// What the helper and the core wrote: the place to look when something does
/// not connect.
public sealed class LogPage : IPage
{
    readonly MainWindow window;
    readonly Grid root = new();
    readonly RichTextBox text = new() { IsReadOnly = true, IsReadOnlyCaretVisible = false, FontFamily = new FontFamily("Cascadia Mono, Consolas"), FontSize = 12, Padding = new Thickness(24, 4, 18, 12) };
    readonly Ui.Empty empty = new("");
    readonly DispatcherTimer timer = new() { Interval = TimeSpan.FromSeconds(2) };
    string raw = "";
    string query = "";
    bool read;

    public LogPage(MainWindow window)
    {
        this.window = window;
        root.Children.Add(text);
        root.Children.Add(empty.View);
        timer.Tick += (_, _) => Read();
    }

    public FrameworkElement View => root;
    public string Title => L.T("Log");
    public string Subtitle => "";
    public string Query { set { if (value != query) { query = value; Render(); } } }

    /// The log is read only while the page is in front.
    public void Shown()
    {
        Read();
        timer.Start();
    }

    public void Hidden() => timer.Stop();

    public void Update(bool force) { if (force && read) Render(); }

    async void Read()
    {
        var reply = await window.Agent.Aside(new Request { Op = "log" });
        var log = reply.Log ?? "";
        if (read && log == raw) return;
        read = true;
        raw = log;
        Render();
    }

    void Render()
    {
        // A reader who scrolled up stays where they are; one at the end follows it.
        bool atEnd = text.VerticalOffset >= text.ExtentHeight - text.ViewportHeight - 20;
        var needle = query.Trim().ToLowerInvariant();
        var lines = raw.Split(new[] { '\n' }, StringSplitOptions.RemoveEmptyEntries)
            .Where(line => needle.Length == 0 || line.ToLowerInvariant().Contains(needle)).ToList();
        var p = new Paragraph();
        int count = 0;
        foreach (var line in lines)
        {
            var entry = LogLine.Parse(line.TrimEnd('\r'));
            if (count++ > 0) p.Inlines.Add(new LineBreak());
            if (entry.Time.Length > 0) p.Inlines.Add(new Run(entry.Time + "  ") { Foreground = Ui.Tertiary });
            p.Inlines.Add(new Run(entry.Text) { Foreground = entry.Tint is { } t ? Ui.Brush(t) : entry.Dim ? Ui.Secondary : Ui.Primary });
        }
        text.Document = new FlowDocument(p) { PagePadding = new Thickness(0), FontFamily = text.FontFamily, FontSize = text.FontSize };
        text.Visibility = count == 0 ? Visibility.Collapsed : Visibility.Visible;
        if (count == 0)
        {
            if (raw.Length == 0) empty.Show(L.T("The log is empty"), L.T("The core writes here once something runs."), false);
            else empty.Show(L.T("Nothing matches the search"), "", false);
        }
        else
        {
            empty.Hide();
            if (atEnd)
            {
                text.UpdateLayout();
                text.ScrollToEnd();
            }
        }
    }

    /// One line of the log, taken apart: the helper writes a date and words,
    /// the core writes time="…" level=… msg="…".
    readonly record struct LogLine(string Time, string Text, Tint? Tint, bool Dim)
    {
        public static LogLine Parse(string line)
        {
            if (line.StartsWith("time=\""))
            {
                int level = line.IndexOf("\" level=", StringComparison.Ordinal);
                int message = line.IndexOf(" msg=\"", StringComparison.Ordinal);
                if (level > 0 && message > level)
                {
                    // 2026-11-27T18:38:28.107400000+03:00 → 18:38:28
                    var stamp = line.Substring(6, level - 6);
                    var t = stamp.IndexOf('T') >= 0 ? stamp.Substring(stamp.IndexOf('T') + 1) : stamp;
                    if (t.Length > 8) t = t.Substring(0, 8);
                    var name = line.Substring(level + 8, message - level - 8);
                    var body = line.Substring(message + 6);
                    if (body.EndsWith("\"")) body = body.Substring(0, body.Length - 1);
                    body = body.Replace("\\\"", "\"");
                    Tint? tint = name switch { "warning" => FlClash.Tint.Orange, "error" or "fatal" => FlClash.Tint.Red, _ => null };
                    return new(t, body, tint, name == "debug");
                }
            }
            if (line.Length > 20 && line[4] == '/')
            {
                // 2026/11/27 18:38:15 the core died…
                var body = line.Substring(20);
                bool bad = body.Contains("died") || body.Contains("did not") || body.StartsWith("guard:") || body.StartsWith("system proxy:");
                return new(line.Substring(11, 8), body, bad ? FlClash.Tint.Orange : null, false);
            }
            return new("", line, null, true);
        }
    }
}
