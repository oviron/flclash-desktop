import AppKit

/// What the helper and the core wrote: the place to look when something does
/// not connect.
final class LogViewController: NSViewController {
    private let text = NSTextView()
    private let scroll = NSScrollView()
    private let empty = EmptyStateView(symbol: "text.alignleft")
    private var timer: Timer?
    private var raw = ""

    /// What the search field of the toolbar holds.
    var query = "" {
        didSet { if isViewLoaded, query != oldValue { render() } }
    }

    override func loadView() {
        text.isEditable = false
        text.isRichText = true
        text.drawsBackground = false
        text.textContainerInset = NSSize(width: 14, height: 10)
        text.isVerticallyResizable = true
        text.autoresizingMask = [.width]
        text.textContainer?.widthTracksTextView = true
        scroll.documentView = text
        scroll.hasVerticalScroller = true
        scroll.drawsBackground = false
        scroll.borderType = .noBorder
        empty.button.isHidden = true

        let root = NSView()
        for subview in [scroll, empty] as [NSView] {
            subview.translatesAutoresizingMaskIntoConstraints = false
            root.addSubview(subview)
        }
        NSLayoutConstraint.activate([
            scroll.topAnchor.constraint(equalTo: root.topAnchor),
            scroll.leadingAnchor.constraint(equalTo: root.leadingAnchor),
            scroll.trailingAnchor.constraint(equalTo: root.trailingAnchor),
            scroll.bottomAnchor.constraint(equalTo: root.bottomAnchor),
            empty.centerXAnchor.constraint(equalTo: root.centerXAnchor),
            empty.centerYAnchor.constraint(equalTo: root.centerYAnchor),
        ])
        view = root
    }

    /// The log is read only while the page is in front.
    override func viewDidAppear() {
        super.viewDidAppear()
        read()
        let timer = Timer(timeInterval: 2, repeats: true) { [weak self] _ in self?.read() }
        RunLoop.main.add(timer, forMode: .common)
        self.timer = timer
    }

    override func viewDidDisappear() {
        super.viewDidDisappear()
        timer?.invalidate()
        timer = nil
    }

    private func read() {
        Engine.stats(["op": "log"]) { [weak self] reply in
            guard let self, let log = reply.log, log != self.raw else {
                if self?.raw.isEmpty == true { self?.render() }
                return
            }
            self.raw = log
            self.render()
        }
    }

    /// The whole log as text, for copying.
    var plainText: String { raw }

    private func render() {
        // A reader who scrolled up stays where they are; one at the end follows it.
        let visible = scroll.contentView.bounds
        let atEnd = visible.maxY >= (scroll.documentView?.frame.height ?? 0) - 20
        let needle = query.trimmingCharacters(in: .whitespaces).lowercased()
        let out = NSMutableAttributedString()
        let font = NSFont.monospacedSystemFont(ofSize: 11, weight: .regular)
        for line in raw.split(separator: "\n") {
            let entry = LogLine(String(line))
            if !needle.isEmpty, !line.lowercased().contains(needle) { continue }
            out.append(NSAttributedString(string: entry.time + "  ", attributes: [.font: font, .foregroundColor: NSColor.tertiaryLabelColor]))
            out.append(NSAttributedString(string: entry.text + "\n", attributes: [.font: font, .foregroundColor: entry.color]))
        }
        text.textStorage?.setAttributedString(out)
        scroll.isHidden = out.length == 0
        if out.length == 0 {
            empty.show(title: raw.isEmpty ? L("The log is empty") : L("Nothing matches the search"),
                       text: raw.isEmpty ? L("The helper writes here once it is installed and something runs.") : "", button: false)
        } else {
            empty.isHidden = true
            if atEnd { text.scrollToEndOfDocument(nil) }
        }
    }
}

/// One line of the log, taken apart: the helper writes a date and words, the
/// core writes time="…" level=… msg="…".
private struct LogLine {
    var time = ""
    var text = ""
    var color = NSColor.labelColor

    init(_ line: String) {
        if line.hasPrefix("time=\""), let level = line.range(of: "\" level="), let message = line.range(of: " msg=\"") {
            // 2026-11-27T18:38:28.107400000+03:00 → 18:38:28
            let stamp = line[line.index(line.startIndex, offsetBy: 6)..<level.lowerBound]
            time = stamp.split(separator: "T").last.map { String($0.prefix(8)) } ?? String(stamp)
            let name = line[level.upperBound..<message.lowerBound]
            var body = String(line[message.upperBound...])
            if body.hasSuffix("\"") { body.removeLast() }
            text = body.replacingOccurrences(of: "\\\"", with: "\"")
            switch name {
            case "warning": color = .systemOrange
            case "error", "fatal": color = .systemRed
            case "debug": color = .secondaryLabelColor
            default: break
            }
        } else if line.count > 20, line[line.index(line.startIndex, offsetBy: 4)] == "/" {
            // 2026/11/27 18:38:15 the core died…
            time = String(line.dropFirst(11).prefix(8))
            text = String(line.dropFirst(20))
            if text.contains("died") || text.contains("did not") || text.hasPrefix("guard:") || text.hasPrefix("system proxy:") {
                color = .systemOrange
            }
        } else {
            text = line
            color = .secondaryLabelColor
        }
    }
}
