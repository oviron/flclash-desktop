using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Documents;
using Microsoft.UI.Xaml.Media;

namespace FlClash;

/// What the helper and the core wrote: the place to look when something does
/// not connect.
public sealed class LogPage : IPage
{
    readonly MainWindow window;
    readonly Grid root = new();
    readonly ScrollViewer scroll = new() { Padding = new Thickness(24, 4, 18, 12) };
    readonly RichTextBlock text = new() { IsTextSelectionEnabled = true, FontFamily = new FontFamily("Cascadia Mono, Consolas"), FontSize = 12, TextWrapping = TextWrapping.Wrap };
    readonly Ui.Empty empty = new("");
    readonly DispatcherTimer timer = new() { Interval = TimeSpan.FromSeconds(2) };
    string raw = "";
    string query = "";
    bool read;

    public LogPage(MainWindow window)
    {
        this.window = window;
        scroll.Content = text;
        root.Children.Add(scroll);
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
        bool atEnd = scroll.VerticalOffset >= scroll.ScrollableHeight - 20;
        var needle = query.Trim().ToLowerInvariant();
        var p = new Paragraph();
        int count = 0;
        foreach (var line in raw.Split('\n', StringSplitOptions.RemoveEmptyEntries))
        {
            if (needle.Length > 0 && !line.ToLowerInvariant().Contains(needle)) continue;
            var entry = LogLine.Parse(line.TrimEnd('\r'));
            if (count++ > 0) p.Inlines.Add(new LineBreak());
            if (entry.Time.Length > 0) p.Inlines.Add(new Run { Text = entry.Time + "  ", Foreground = Ui.Tertiary });
            p.Inlines.Add(new Run { Text = entry.Text, Foreground = entry.Tint is { } t ? Ui.Brush(t) : entry.Dim ? Ui.Secondary : Ui.Primary });
        }
        text.Blocks.Clear();
        text.Blocks.Add(p);
        scroll.Visibility = count == 0 ? Visibility.Collapsed : Visibility.Visible;
        if (count == 0)
        {
            if (raw.Length == 0) empty.Show(L.T("The log is empty"), L.T("The helper writes here once it is installed and something runs."), false);
            else empty.Show(L.T("Nothing matches the search"), "", false);
        }
        else
        {
            empty.Hide();
            if (atEnd)
            {
                scroll.UpdateLayout();
                scroll.ChangeView(null, scroll.ScrollableHeight, null, true);
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
                    var stamp = line[6..level];
                    var t = stamp.Contains('T') ? stamp[(stamp.IndexOf('T') + 1)..] : stamp;
                    if (t.Length > 8) t = t[..8];
                    var name = line[(level + 8)..message];
                    var body = line[(message + 6)..];
                    if (body.EndsWith('"')) body = body[..^1];
                    body = body.Replace("\\\"", "\"");
                    Tint? tint = name switch { "warning" => FlClash.Tint.Orange, "error" or "fatal" => FlClash.Tint.Red, _ => null };
                    return new(t, body, tint, name == "debug");
                }
            }
            if (line.Length > 20 && line[4] == '/')
            {
                // 2026/11/27 18:38:15 the core died…
                var body = line[20..];
                bool bad = body.Contains("died") || body.Contains("did not") || body.StartsWith("guard:") || body.StartsWith("system proxy:");
                return new(line.Substring(11, 8), body, bad ? FlClash.Tint.Orange : null, false);
            }
            return new("", line, null, true);
        }
    }
}
