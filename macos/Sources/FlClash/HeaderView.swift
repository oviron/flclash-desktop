import AppKit

/// A round badge that says the state with a symbol and a colour.
private final class StatusBadge: NSView {
    private let image = NSImageView()
    private var tint = NSColor.secondaryLabelColor

    override init(frame: NSRect) {
        super.init(frame: frame)
        image.symbolConfiguration = .init(pointSize: 17, weight: .medium)
        image.translatesAutoresizingMaskIntoConstraints = false
        addSubview(image)
        NSLayoutConstraint.activate([
            widthAnchor.constraint(equalToConstant: 38),
            heightAnchor.constraint(equalToConstant: 38),
            image.centerXAnchor.constraint(equalTo: centerXAnchor),
            image.centerYAnchor.constraint(equalTo: centerYAnchor),
        ])
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) { fatalError() }

    func show(symbol: String, tint: NSColor) {
        self.tint = tint
        image.image = NSImage(systemSymbolName: symbol, accessibilityDescription: nil)
        image.contentTintColor = tint
        needsDisplay = true
    }

    override func draw(_ dirtyRect: NSRect) {
        tint.withAlphaComponent(0.16).setFill()
        NSBezierPath(ovalIn: bounds).fill()
    }
}

/// What is true right now, and the one button that changes it.
final class HeaderView: NSView {
    private let badge = StatusBadge()
    private let headline = NSTextField(labelWithString: "")
    private let detail = NSTextField(labelWithString: "")
    private let button = NSButton(title: "", target: nil, action: nil)
    private let spinner = NSProgressIndicator()
    private let speed = NSTextField(labelWithString: "")
    private let modes = NSSegmentedControl(labels: [L("TUN"), L("System Proxy"), L("Port Only")], trackingMode: .selectOne, target: nil, action: nil)
    private let address = NSTextField(labelWithString: "")
    private let notice = NSTextField(wrappingLabelWithString: "")
    private let noticeButton = NSButton(title: "", target: nil, action: nil)
    private let noticeRow = NSStackView()
    private var installing = false

    private static let time: DateFormatter = {
        let formatter = DateFormatter()
        formatter.dateStyle = .none
        formatter.timeStyle = .short
        return formatter
    }()

    private static let dayAndTime: DateFormatter = {
        let formatter = DateFormatter()
        formatter.dateStyle = .medium
        formatter.timeStyle = .short
        formatter.doesRelativeDateFormatting = true
        return formatter
    }()

    override init(frame: NSRect) {
        super.init(frame: frame)

        headline.font = .systemFont(ofSize: 15, weight: .semibold)
        headline.lineBreakMode = .byTruncatingTail
        detail.font = .systemFont(ofSize: 12)
        detail.textColor = .secondaryLabelColor
        detail.lineBreakMode = .byTruncatingTail
        for label in [headline, detail] {
            label.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        }
        let titles = NSStackView(views: [headline, detail])
        titles.orientation = .vertical
        titles.alignment = .leading
        titles.spacing = 2

        spinner.style = .spinning
        spinner.controlSize = .small
        spinner.isDisplayedWhenStopped = false

        button.bezelStyle = .push
        button.controlSize = .large
        button.target = self
        button.action = #selector(toggle)
        button.setContentHuggingPriority(.required, for: .horizontal)
        // No Return shortcut: a stray Enter must not turn the tunnel on or off.

        let spacer = NSView()
        spacer.setContentHuggingPriority(.init(1), for: .horizontal)
        speed.font = .monospacedDigitSystemFont(ofSize: 12, weight: .regular)
        speed.textColor = .secondaryLabelColor
        speed.alignment = .right
        speed.setContentHuggingPriority(.required, for: .horizontal)
        speed.setContentCompressionResistancePriority(.required, for: .horizontal)
        let top = NSStackView(views: [badge, titles, spacer, speed, spinner, button])
        top.orientation = .horizontal
        top.alignment = .centerY
        top.spacing = 12

        modes.target = self
        modes.action = #selector(modeChanged)
        modes.setToolTip(L("Everything on this Mac goes through the tunnel."), forSegment: 0)
        modes.setToolTip(L("Apps that follow the system proxy setting go through the local port."), forSegment: 1)
        modes.setToolTip(L("Nothing changes in the system: only apps pointed at the local port use it."), forSegment: 2)
        address.font = .monospacedDigitSystemFont(ofSize: 11, weight: .regular)
        address.textColor = .secondaryLabelColor
        address.isSelectable = true
        address.toolTip = L("The local port for HTTP and SOCKS. It can be changed in Settings.")
        let modeRow = NSStackView(views: [modes, address])
        modeRow.orientation = .horizontal
        modeRow.alignment = .centerY
        modeRow.spacing = 12

        notice.font = .systemFont(ofSize: 12)
        notice.textColor = .secondaryLabelColor
        notice.isSelectable = true
        noticeButton.bezelStyle = .push
        noticeButton.controlSize = .small
        noticeButton.target = self
        noticeButton.action = #selector(noticeAction)
        noticeButton.setContentHuggingPriority(.required, for: .horizontal)
        let warning = NSImageView(image: NSImage(systemSymbolName: "exclamationmark.triangle.fill", accessibilityDescription: nil) ?? NSImage())
        warning.contentTintColor = .systemYellow
        noticeRow.orientation = .horizontal
        noticeRow.alignment = .firstBaseline
        noticeRow.spacing = 8
        noticeRow.setViews([warning, notice, noticeButton], in: .leading)
        notice.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)

        // The state sits on a card of its own, apart from the list under it.
        let card = NSBox()
        card.boxType = .custom
        card.titlePosition = .noTitle
        card.borderWidth = 0
        card.cornerRadius = 14
        card.fillColor = .quaternarySystemFill
        card.contentViewMargins = .zero

        let column = NSStackView(views: [top, modeRow, noticeRow])
        column.orientation = .vertical
        column.alignment = .leading
        column.spacing = 12
        column.edgeInsets = NSEdgeInsets(top: 14, left: 14, bottom: 14, right: 16)
        column.translatesAutoresizingMaskIntoConstraints = false
        card.translatesAutoresizingMaskIntoConstraints = false
        card.contentView?.addSubview(column)
        addSubview(card)
        NSLayoutConstraint.activate([
            card.topAnchor.constraint(equalTo: topAnchor, constant: 8),
            card.leadingAnchor.constraint(equalTo: leadingAnchor, constant: 16),
            card.trailingAnchor.constraint(equalTo: trailingAnchor, constant: -16),
            card.bottomAnchor.constraint(equalTo: bottomAnchor, constant: -10),
            column.topAnchor.constraint(equalTo: card.topAnchor),
            column.leadingAnchor.constraint(equalTo: card.leadingAnchor),
            column.trailingAnchor.constraint(equalTo: card.trailingAnchor),
            column.bottomAnchor.constraint(equalTo: card.bottomAnchor),
            top.widthAnchor.constraint(equalTo: column.widthAnchor, constant: -30),
            noticeRow.widthAnchor.constraint(lessThanOrEqualTo: column.widthAnchor, constant: -30),
        ])

        NotificationCenter.default.addObserver(self, selector: #selector(update), name: .modelChanged, object: nil)
        NotificationCenter.default.addObserver(self, selector: #selector(updateSpeed), name: .statsChanged, object: nil)
        update()
        updateSpeed()
    }

    @objc private func updateSpeed() {
        guard let stats = Model.shared.stats, Model.shared.status.running else {
            speed.stringValue = ""
            return
        }
        speed.stringValue = "↓ " + rate(stats.down) + "   ↑ " + rate(stats.up)
        speed.toolTip = String(format: L("Since the tunnel came up: %@ down, %@ up"), amount(stats.downTotal), amount(stats.upTotal))
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) { fatalError() }

    @objc private func update() {
        let model = Model.shared
        let status = model.status

        if model.helperReady, status.blocked {
            badge.show(symbol: "lock.shield.fill", tint: .systemRed)
        } else if !model.helperReady || (status.running && !status.captured) {
            badge.show(symbol: "exclamationmark.shield.fill", tint: .systemOrange)
        } else if status.running {
            badge.show(symbol: "checkmark.shield.fill", tint: .systemGreen)
        } else {
            badge.show(symbol: "shield", tint: .secondaryLabelColor)
        }

        var title = status.headline
        if status.running, status.captured, status.since > 0 {
            let since = Date(timeIntervalSince1970: TimeInterval(status.since))
            let formatter = Calendar.current.isDateInToday(since) ? Self.time : Self.dayAndTime
            title = String(format: L("Connected since %@"), formatter.string(from: since))
        }
        headline.stringValue = title

        // While on, say what runs; while off, say what would.
        let server = status.running ? status.server : (model.selectedServer?.name ?? "")
        let policy = status.running ? status.policy : model.view.policy
        if status.blocked {
            detail.stringValue = status.server.isEmpty
                ? L("The tunnel is down")
                : String(format: L("The tunnel through %@ is down"), status.server)
        } else if server.isEmpty {
            detail.stringValue = L("Choose a server to connect")
        } else {
            var parts = [server, policyTitle(policy)]
            if status.running, status.guard_ == "full" { parts.append(L("protected")) }
            detail.stringValue = parts.joined(separator: "  ·  ")
        }

        // While on, the switch shows what runs; while off, what would.
        let mode = status.running && !status.mode.isEmpty ? status.mode : model.view.mode
        modes.selectedSegment = ["tun": 0, "system": 1, "proxy": 2][mode] ?? 0
        modes.isEnabled = model.helperReady && !model.busy
        address.stringValue = "127.0.0.1:" + String(status.running && status.port > 0 ? status.port : model.view.port)

        button.title = status.running ? L("Disconnect") : (status.blocked ? L("Connect Again") : L("Connect"))
        button.isEnabled = model.helperReady && !model.busy && (status.running || model.selectedServer != nil)
        // Turning on is the thing to do here; turning off is not.
        button.bezelColor = button.isEnabled && !status.running ? .controlAccentColor : nil
        if model.busy || installing { spinner.startAnimation(nil) } else { spinner.stopAnimation(nil) }

        notice.toolTip = nil
        if !status.installed {
            notice.stringValue = L("To capture the traffic of this Mac, FlClash needs a system helper. Installing it asks for an administrator password once.")
            noticeButton.title = L("Install Helper…")
            noticeButton.isEnabled = !installing
            noticeButton.isHidden = false
            noticeRow.isHidden = false
        } else if status.outdated {
            notice.stringValue = L("The system helper is from another version of FlClash and has to be installed again. A tunnel that is on will stop for a moment.")
            noticeButton.title = L("Update Helper…")
            noticeButton.isEnabled = !installing
            noticeButton.isHidden = false
            noticeRow.isHidden = false
        } else if status.blocked {
            notice.stringValue = L("Protection lets nothing out past the tunnel, so the internet is off until the tunnel is back. The local network works.")
            noticeButton.title = L("Unblock")
            noticeButton.isEnabled = !model.busy
            noticeButton.isHidden = false
            noticeRow.isHidden = false
        } else if let problem = model.problem {
            notice.stringValue = problem
            noticeButton.title = L("Dismiss")
            noticeButton.isEnabled = true
            noticeButton.isHidden = false
            noticeRow.isHidden = false
        } else if status.running, !status.guardProblem.isEmpty {
            notice.stringValue = L("Protection did not turn on: traffic can leave past the tunnel.")
            notice.toolTip = status.guardProblem
            noticeButton.isHidden = true
            noticeRow.isHidden = false
        } else {
            noticeRow.isHidden = true
        }
    }

    @objc private func toggle() { Model.shared.toggle() }

    @objc private func modeChanged() {
        Model.shared.setMode(["tun", "system", "proxy"][max(0, min(2, modes.selectedSegment))])
    }

    @objc private func noticeAction() {
        if Model.shared.helperReady {
            // Blocked: the button takes the guard off. Otherwise it hides a message.
            if Model.shared.status.blocked { Model.shared.turnOff() } else { Model.shared.dismissProblem() }
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

/// A number of bytes for people.
func amount(_ bytes: Int) -> String {
    ByteCountFormatter.string(fromByteCount: Int64(bytes), countStyle: .binary)
}

/// A number of bytes per second for people.
func rate(_ bytes: Int) -> String {
    String(format: L("%@/s"), amount(bytes))
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
