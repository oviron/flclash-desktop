import AppKit

/// What is true right now, and the one button that changes it.
final class HeaderView: NSView {
    private let dot = NSImageView()
    private let headline = NSTextField(labelWithString: "")
    private let detail = NSTextField(labelWithString: "")
    private let button = NSButton(title: "", target: nil, action: nil)
    private let spinner = NSProgressIndicator()
    private let notice = NSTextField(wrappingLabelWithString: "")
    private let noticeButton = NSButton(title: "", target: nil, action: nil)
    private let noticeRow = NSStackView()
    private var installing = false

    override init(frame: NSRect) {
        super.init(frame: frame)

        headline.font = .systemFont(ofSize: 15, weight: .semibold)
        detail.font = .systemFont(ofSize: 12)
        detail.textColor = .secondaryLabelColor
        detail.lineBreakMode = .byTruncatingTail
        let titles = NSStackView(views: [headline, detail])
        titles.orientation = .vertical
        titles.alignment = .leading
        titles.spacing = 2

        spinner.style = .spinning
        spinner.controlSize = .small
        spinner.isDisplayedWhenStopped = false

        button.bezelStyle = .rounded
        button.controlSize = .large
        button.target = self
        button.action = #selector(toggle)
        button.keyEquivalent = "\r"

        let top = NSStackView(views: [dot, titles, NSView(), spinner, button])
        top.orientation = .horizontal
        top.alignment = .centerY
        top.spacing = 10
        top.setHuggingPriority(.defaultLow, for: .horizontal)

        notice.font = .systemFont(ofSize: 12)
        notice.textColor = .secondaryLabelColor
        notice.isSelectable = true
        noticeButton.bezelStyle = .rounded
        noticeButton.controlSize = .small
        noticeButton.target = self
        noticeButton.action = #selector(noticeAction)
        let warning = NSImageView(image: NSImage(systemSymbolName: "exclamationmark.triangle.fill", accessibilityDescription: nil) ?? NSImage())
        warning.contentTintColor = .systemYellow
        noticeRow.orientation = .horizontal
        noticeRow.alignment = .firstBaseline
        noticeRow.spacing = 8
        noticeRow.setViews([warning, notice, noticeButton], in: .leading)
        notice.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)

        let separator = NSBox()
        separator.boxType = .separator

        let column = NSStackView(views: [top, noticeRow])
        column.orientation = .vertical
        column.alignment = .leading
        column.spacing = 10
        column.edgeInsets = NSEdgeInsets(top: 14, left: 18, bottom: 14, right: 18)
        column.translatesAutoresizingMaskIntoConstraints = false
        separator.translatesAutoresizingMaskIntoConstraints = false
        addSubview(column)
        addSubview(separator)
        NSLayoutConstraint.activate([
            column.topAnchor.constraint(equalTo: topAnchor),
            column.leadingAnchor.constraint(equalTo: leadingAnchor),
            column.trailingAnchor.constraint(equalTo: trailingAnchor),
            top.widthAnchor.constraint(equalTo: column.widthAnchor, constant: -36),
            noticeRow.widthAnchor.constraint(lessThanOrEqualTo: column.widthAnchor, constant: -36),
            separator.topAnchor.constraint(equalTo: column.bottomAnchor),
            separator.leadingAnchor.constraint(equalTo: leadingAnchor),
            separator.trailingAnchor.constraint(equalTo: trailingAnchor),
            separator.bottomAnchor.constraint(equalTo: bottomAnchor),
            dot.widthAnchor.constraint(equalToConstant: 14),
        ])

        NotificationCenter.default.addObserver(self, selector: #selector(update), name: .modelChanged, object: nil)
        update()
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) { fatalError() }

    @objc private func update() {
        let model = Model.shared
        let status = model.status

        let color: NSColor
        if !status.installed || (status.running && !status.captured) {
            color = .systemOrange
        } else if status.running {
            color = .systemGreen
        } else {
            color = .tertiaryLabelColor
        }
        dot.image = NSImage(systemSymbolName: "circle.fill", accessibilityDescription: nil)
        dot.contentTintColor = color
        headline.stringValue = status.headline

        // While on, say what runs; while off, say what would.
        let server = status.running ? status.server : (model.selectedServer?.name ?? "")
        let policy = status.running ? status.policy : model.view.policy
        if server.isEmpty {
            detail.stringValue = L("Choose a server to connect")
        } else {
            detail.stringValue = server + "  ·  " + policyTitle(policy)
        }

        button.title = status.running ? L("Disconnect") : L("Connect")
        button.isEnabled = status.installed && !model.busy && (status.running || model.selectedServer != nil)
        if model.busy || installing { spinner.startAnimation(nil) } else { spinner.stopAnimation(nil) }

        if !status.installed {
            notice.stringValue = L("To capture the traffic of this Mac, FlClash needs a system helper. Installing it asks for an administrator password once.")
            noticeButton.title = L("Install Helper…")
            noticeButton.isEnabled = !installing
            noticeButton.isHidden = false
            noticeRow.isHidden = false
        } else if let problem = model.problem {
            notice.stringValue = problem
            noticeButton.title = L("Dismiss")
            noticeButton.isEnabled = true
            noticeButton.isHidden = false
            noticeRow.isHidden = false
        } else {
            noticeRow.isHidden = true
        }
    }

    @objc private func toggle() { Model.shared.toggle() }

    @objc private func noticeAction() {
        if Model.shared.status.installed {
            Model.shared.dismissProblem()
            return
        }
        installing = true
        update()
        HelperInstaller.install { [weak self] error in
            self?.installing = false
            if let error, let window = self?.window {
                let alert = NSAlert()
                alert.messageText = L("The helper was not installed")
                alert.informativeText = error
                alert.beginSheetModal(for: window)
            }
            Model.shared.refresh()
        }
    }
}

/// Installs the privileged helper through the system's administrator prompt.
enum HelperInstaller {
    static func install(then done: @escaping (String?) -> Void) {
        // The helper binaries ship next to the app's own executable.
        let tool = Bundle.main.bundleURL.appendingPathComponent("Contents/MacOS/flclashctl").path
        let command = "'\(tool.replacingOccurrences(of: "'", with: "'\\''"))' helper install -uid \(getuid())"
        let script = "do shell script \"\(command.replacingOccurrences(of: "\\", with: "\\\\").replacingOccurrences(of: "\"", with: "\\\""))\" with administrator privileges"
        DispatchQueue.global(qos: .userInitiated).async {
            let process = Process()
            process.executableURL = URL(fileURLWithPath: "/usr/bin/osascript")
            process.arguments = ["-e", script]
            let errors = Pipe()
            process.standardError = errors
            process.standardOutput = Pipe()
            var failure: String?
            do {
                try process.run()
                process.waitUntilExit()
                if process.terminationStatus != 0 {
                    let text = String(data: errors.fileHandleForReading.readDataToEndOfFile(), encoding: .utf8) ?? ""
                    // -128 is the person pressing Cancel: nothing to report.
                    failure = text.contains("-128") ? nil : text.trimmingCharacters(in: .whitespacesAndNewlines)
                }
            } catch {
                failure = error.localizedDescription
            }
            DispatchQueue.main.async { done(failure) }
        }
    }
}
