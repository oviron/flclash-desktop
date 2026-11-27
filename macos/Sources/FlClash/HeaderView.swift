import AppKit

/// The look of a state: its colour and its symbol.
struct Look {
    let tint: NSColor
    let symbol: String
    /// How much of the colour the card takes: a state that asks for attention
    /// colours it, the resting one does not.
    let wash: CGFloat

    /// The look of what is true right now.
    static var current: Look {
        let model = Model.shared
        let status = model.status
        if model.helperReady, status.blocked {
            return Look(tint: .systemRed, symbol: "lock.shield.fill", wash: 0.22)
        }
        if !model.helperReady || (status.running && !status.captured) {
            return Look(tint: .systemOrange, symbol: "exclamationmark.shield.fill", wash: 0.18)
        }
        if status.running {
            return Look(tint: .systemGreen, symbol: "checkmark.shield.fill", wash: 0.18)
        }
        return Look(tint: .secondaryLabelColor, symbol: "shield", wash: 0)
    }
}

/// A round badge that says the state with a symbol and a colour.
final class StatusBadge: NSView {
    private let image = NSImageView()
    private var tint = NSColor.secondaryLabelColor

    init(side: CGFloat = 46) {
        super.init(frame: .zero)
        image.symbolConfiguration = .init(pointSize: side * 0.43, weight: .medium)
        image.translatesAutoresizingMaskIntoConstraints = false
        addSubview(image)
        NSLayoutConstraint.activate([
            widthAnchor.constraint(equalToConstant: side),
            heightAnchor.constraint(equalToConstant: side),
            image.centerXAnchor.constraint(equalTo: centerXAnchor),
            image.centerYAnchor.constraint(equalTo: centerYAnchor),
        ])
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) { fatalError() }

    func show(_ look: Look) {
        tint = look.tint
        image.image = NSImage(systemSymbolName: look.symbol, accessibilityDescription: nil)
        image.contentTintColor = look.tint
        needsDisplay = true
    }

    override func draw(_ dirtyRect: NSRect) {
        // A soft disc with a thin ring: the colour of the state, not a button.
        tint.withAlphaComponent(0.18).setFill()
        NSBezierPath(ovalIn: bounds).fill()
        let ring = NSBezierPath(ovalIn: bounds.insetBy(dx: 0.75, dy: 0.75))
        ring.lineWidth = 1.5
        tint.withAlphaComponent(0.35).setStroke()
        ring.stroke()
    }
}

/// The card everything about the state sits on. The colour of the state
/// washes over it from the leading edge and fades out.
private final class CardView: NSView {
    var look = Look(tint: .secondaryLabelColor, symbol: "", wash: 0) { didSet { needsDisplay = true } }

    override func draw(_ dirtyRect: NSRect) {
        let shape = NSBezierPath(roundedRect: bounds.insetBy(dx: 0.5, dy: 0.5), xRadius: 16, yRadius: 16)
        NSColor.quaternarySystemFill.setFill()
        shape.fill()
        if look.wash > 0 {
            NSGraphicsContext.saveGraphicsState()
            shape.addClip()
            let leading = look.tint.withAlphaComponent(look.wash)
            NSGradient(colors: [leading, look.tint.withAlphaComponent(look.wash * 0.25), look.tint.withAlphaComponent(0)],
                       atLocations: [0, 0.45, 0.9], colorSpace: .sRGB)?
                .draw(in: bounds, angle: userInterfaceLayoutDirection == .rightToLeft ? 180 : 0)
            NSGraphicsContext.restoreGraphicsState()
        }
        NSColor.separatorColor.withAlphaComponent(0.5).setStroke()
        shape.lineWidth = 1
        shape.stroke()
    }
}

/// The last minute of traffic as a small curve: what came down is the filled
/// shape, what went up is the line over it.
final class TrafficGraph: NSView {
    var tint = NSColor.systemGreen { didSet { needsDisplay = true } }
    var samples: [(down: Int, up: Int)] = [] { didSet { needsDisplay = true } }

    override var intrinsicContentSize: NSSize { NSSize(width: 110, height: 34) }

    override func draw(_ dirtyRect: NSRect) {
        // The ground line is there from the first second, so the graph reads
        // as one before it has anything to show.
        let ground = NSBezierPath()
        ground.move(to: NSPoint(x: bounds.minX + 1, y: bounds.minY + 1.5))
        ground.line(to: NSPoint(x: bounds.maxX - 1, y: bounds.minY + 1.5))
        ground.lineWidth = 1
        NSColor.separatorColor.setStroke()
        ground.stroke()
        guard samples.count > 1 else { return }
        // The scale never drops under a few kilobytes: an idle line must lie
        // flat, not shake with every stray packet.
        let peak = CGFloat(max(samples.map { max($0.down, $0.up) }.max() ?? 0, 8 << 10))
        let room = bounds.insetBy(dx: 1, dy: 2)
        let step = room.width / CGFloat(Model.historyLength - 1)
        func curve(_ value: (Int) -> Int) -> NSBezierPath {
            let path = NSBezierPath()
            // The newest sample sits at the trailing edge.
            let start = room.maxX - step * CGFloat(samples.count - 1)
            for (index, sample) in samples.enumerated() {
                let point = NSPoint(x: start + step * CGFloat(index), y: room.minY + room.height * CGFloat(value(index)) / peak)
                _ = sample
                if index == 0 { path.move(to: point) } else { path.line(to: point) }
            }
            return path
        }
        let down = curve { samples[$0].down }
        let area = down.copy() as! NSBezierPath
        area.line(to: NSPoint(x: room.maxX, y: room.minY))
        area.line(to: NSPoint(x: room.maxX - step * CGFloat(samples.count - 1), y: room.minY))
        area.close()
        NSGradient(starting: tint.withAlphaComponent(0.30), ending: tint.withAlphaComponent(0.02))?.draw(in: area, angle: -90)
        down.lineWidth = 1.5
        down.lineJoinStyle = .round
        tint.withAlphaComponent(0.9).setStroke()
        down.stroke()
        let up = curve { samples[$0].up }
        up.lineWidth = 1
        up.lineJoinStyle = .round
        NSColor.secondaryLabelColor.withAlphaComponent(0.7).setStroke()
        up.stroke()
    }
}

/// What is true right now, the one button that changes it, and the choices
/// people change most.
final class HeaderView: NSView {
    private let card = CardView()
    private let badge = StatusBadge()
    private let headline = NSTextField(labelWithString: "")
    private let detail = NSTextField(labelWithString: "")
    private let exit = NSTextField(labelWithString: "")
    private let button = NSButton(title: "", target: nil, action: nil)
    private let spinner = NSProgressIndicator()
    private let graph = TrafficGraph()
    private let down = NSTextField(labelWithString: "")
    private let up = NSTextField(labelWithString: "")
    private let traffic = NSStackView()
    private let modes = NSSegmentedControl(labels: [L("Port Only"), L("System Proxy"), L("TUN")], trackingMode: .selectOne, target: nil, action: nil)
    private let policies = NSPopUpButton(frame: .zero, pullsDown: false)
    private let address = NSButton(title: "", target: nil, action: nil)
    private let testMark = NSTextField(labelWithString: L("Test environment"))
    private let notice = NSTextField(wrappingLabelWithString: "")
    private let noticeButton = NSButton(title: "", target: nil, action: nil)
    private let noticeRow = NSStackView()
    private var installing = false
    private var copied = false

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

        headline.font = .systemFont(ofSize: 17, weight: .semibold)
        headline.lineBreakMode = .byTruncatingTail
        detail.font = .systemFont(ofSize: 12)
        detail.textColor = .secondaryLabelColor
        detail.lineBreakMode = .byTruncatingTail
        // The words give way last: the graph shrinks before they are cut.
        for label in [headline, detail] {
            label.setContentCompressionResistancePriority(.init(700), for: .horizontal)
        }
        graph.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        graph.setContentHuggingPriority(.defaultHigh, for: .horizontal)
        exit.font = .systemFont(ofSize: 12)
        exit.textColor = .secondaryLabelColor
        exit.lineBreakMode = .byTruncatingTail
        exit.isSelectable = true
        exit.setContentCompressionResistancePriority(.init(700), for: .horizontal)
        let titles = NSStackView(views: [headline, detail, exit])
        titles.orientation = .vertical
        titles.alignment = .leading
        titles.spacing = 3

        spinner.style = .spinning
        spinner.controlSize = .small
        spinner.isDisplayedWhenStopped = false

        button.bezelStyle = .push
        button.controlSize = .large
        if #available(macOS 26.0, *) { button.controlSize = .extraLarge }
        button.target = self
        button.action = #selector(toggle)
        button.setContentHuggingPriority(.required, for: .horizontal)
        button.setContentCompressionResistancePriority(.required, for: .horizontal)
        // No Return shortcut: a stray Enter must not turn the tunnel on or off.

        for label in [down, up] {
            label.font = .monospacedDigitSystemFont(ofSize: 11, weight: .regular)
            label.textColor = .secondaryLabelColor
        }
        let rates = NSStackView(views: [down, up])
        rates.orientation = .vertical
        rates.alignment = .leading
        rates.spacing = 3
        rates.widthAnchor.constraint(greaterThanOrEqualToConstant: 84).isActive = true
        traffic.orientation = .horizontal
        traffic.alignment = .centerY
        traffic.spacing = 10
        traffic.setViews([graph, rates], in: .leading)
        traffic.setContentHuggingPriority(.required, for: .horizontal)

        let spacer = NSView()
        spacer.setContentHuggingPriority(.init(1), for: .horizontal)
        // A window that talks to something other than the system helper, or
        // keeps its data elsewhere, shows numbers that may not be real. It says so.
        testMark.font = .systemFont(ofSize: 11, weight: .semibold)
        testMark.textColor = .systemOrange
        testMark.isHidden = !Model.testEnvironment
        testMark.setContentCompressionResistancePriority(.required, for: .horizontal)
        let top = NSStackView(views: [badge, titles, testMark, spacer, traffic, spinner, button])
        top.orientation = .horizontal
        top.alignment = .centerY
        top.spacing = 14
        top.setCustomSpacing(18, after: traffic)

        modes.target = self
        modes.action = #selector(modeChanged)
        modes.setToolTip(L("Nothing changes in the system: only apps pointed at the local port use it."), forSegment: 0)
        modes.setToolTip(L("Apps that follow the system proxy setting go through the local port."), forSegment: 1)
        modes.setToolTip(L("Everything on this Mac goes through the tunnel."), forSegment: 2)
        modes.setContentCompressionResistancePriority(.required, for: .horizontal)

        policies.target = self
        policies.action = #selector(policyChanged)
        policies.toolTip = L("Routing: which connections go through the server.")
        policies.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        policies.widthAnchor.constraint(lessThanOrEqualToConstant: 240).isActive = true

        address.isBordered = false
        address.image = NSImage(systemSymbolName: "doc.on.doc", accessibilityDescription: L("Copy"))
        address.imagePosition = .imageTrailing
        address.symbolConfiguration = .init(pointSize: 10, weight: .regular)
        address.contentTintColor = .secondaryLabelColor
        address.target = self
        address.action = #selector(copyAddress)
        address.toolTip = L("The local port for HTTP and SOCKS. Click to copy; change it in Settings.")
        address.setContentHuggingPriority(.required, for: .horizontal)

        let gap = NSView()
        gap.setContentHuggingPriority(.init(1), for: .horizontal)
        let choices = NSStackView(views: [modes, policies, gap, address])
        choices.orientation = .horizontal
        choices.alignment = .centerY
        choices.spacing = 10

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

        let column = NSStackView(views: [top, choices, noticeRow])
        column.orientation = .vertical
        column.alignment = .leading
        column.spacing = 14
        column.edgeInsets = NSEdgeInsets(top: 16, left: 16, bottom: 14, right: 16)
        column.translatesAutoresizingMaskIntoConstraints = false
        card.translatesAutoresizingMaskIntoConstraints = false
        card.addSubview(column)
        addSubview(card)
        NSLayoutConstraint.activate([
            card.topAnchor.constraint(equalTo: topAnchor, constant: 8),
            card.leadingAnchor.constraint(equalTo: leadingAnchor, constant: 16),
            card.trailingAnchor.constraint(equalTo: trailingAnchor, constant: -16),
            card.bottomAnchor.constraint(equalTo: bottomAnchor, constant: -8),
            column.topAnchor.constraint(equalTo: card.topAnchor),
            column.leadingAnchor.constraint(equalTo: card.leadingAnchor),
            column.trailingAnchor.constraint(equalTo: card.trailingAnchor),
            column.bottomAnchor.constraint(equalTo: card.bottomAnchor),
            top.widthAnchor.constraint(equalTo: column.widthAnchor, constant: -32),
            choices.widthAnchor.constraint(equalTo: column.widthAnchor, constant: -32),
            noticeRow.widthAnchor.constraint(lessThanOrEqualTo: column.widthAnchor, constant: -32),
        ])

        NotificationCenter.default.addObserver(self, selector: #selector(update), name: .modelChanged, object: nil)
        NotificationCenter.default.addObserver(self, selector: #selector(updateTraffic), name: .statsChanged, object: nil)
        update()
        updateTraffic()
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) { fatalError() }

    @objc private func updateTraffic() {
        guard let stats = Model.shared.stats, Model.shared.status.running else {
            traffic.isHidden = true
            return
        }
        traffic.isHidden = false
        graph.samples = Model.shared.history
        down.stringValue = "↓ " + rate(stats.down)
        up.stringValue = "↑ " + rate(stats.up)
        traffic.toolTip = String(format: L("Since the tunnel came up: %@ down, %@ up"), amount(stats.downTotal), amount(stats.upTotal))
    }

    @objc private func update() {
        let model = Model.shared
        let status = model.status

        let look = Look.current
        badge.show(look)
        card.look = look
        graph.tint = look.tint

        headline.stringValue = status.headline

        // While on, say what runs; while off, say what would.
        let server = status.running ? status.server : (model.selectedServer?.name ?? "")
        if status.blocked {
            if status.mode == "system" {
                detail.stringValue = status.server.isEmpty ? L("The core stopped") : String(format: L("The connection through %@ stopped"), status.server)
            } else {
                detail.stringValue = status.server.isEmpty
                    ? L("The tunnel is down")
                    : String(format: L("The tunnel through %@ is down"), status.server)
            }
        } else if server.isEmpty {
            detail.stringValue = L("Choose a server to connect")
        } else {
            var parts = [server]
            if status.running, status.captured, status.since > 0 {
                let since = Date(timeIntervalSince1970: TimeInterval(status.since))
                let formatter = Calendar.current.isDateInToday(since) ? Self.time : Self.dayAndTime
                parts.append(String(format: L("since %@"), formatter.string(from: since)))
            }
            if status.running, status.guard_ == "full" { parts.append(L("protected")) }
            detail.stringValue = parts.joined(separator: "  ·  ")
        }

        // Where the server lets traffic out, once that is known.
        if status.running, let found = model.exit {
            exit.stringValue = [found.place, found.ip].compactMap { $0 }.joined(separator: "  ·  ")
            exit.isHidden = false
        } else {
            exit.isHidden = true
        }

        // While on, the switches show what runs; while off, what would.
        let mode = status.running && !status.mode.isEmpty ? status.mode : model.view.mode
        modes.selectedSegment = ["proxy": 0, "system": 1, "tun": 2][mode] ?? 0
        modes.isEnabled = model.helperReady && !model.busy

        let policy = status.running && !status.policy.isEmpty ? status.policy : model.view.policy
        policies.removeAllItems()
        for item in model.view.policies {
            policies.addItem(withTitle: policyTitle(item.name))
            policies.lastItem?.representedObject = item.name
            policies.lastItem?.toolTip = policyAbout(item)
        }
        if let index = model.view.policies.firstIndex(where: { $0.name == policy }) { policies.selectItem(at: index) }
        policies.isEnabled = !model.busy

        if !copied {
            address.title = "127.0.0.1:" + String(status.running && status.port > 0 ? status.port : model.view.port)
        }

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
            notice.stringValue = status.mode == "system"
                ? L("The system still points at FlClash, so apps that follow the system proxy are offline until it is back. They do not go out directly.")
                : L("Protection lets nothing out past the tunnel, so the internet is off until the tunnel is back. The local network works.")
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
        updateTraffic()
    }

    @objc private func toggle() { Model.shared.toggle() }

    @objc private func modeChanged() {
        Model.shared.setMode(["proxy", "system", "tun"][max(0, min(2, modes.selectedSegment))])
    }

    @objc private func policyChanged() {
        if let name = policies.selectedItem?.representedObject as? String, name != Model.shared.view.policy {
            Model.shared.select(policy: name)
        }
    }

    /// Puts the address of the local port on the clipboard, and says so for a moment.
    @objc private func copyAddress() {
        guard !copied else { return }
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(address.title, forType: .string)
        let shown = address.title
        copied = true
        address.title = L("Copied")
        address.image = NSImage(systemSymbolName: "checkmark", accessibilityDescription: nil)
        DispatchQueue.main.asyncAfter(deadline: .now() + 1.2) { [weak self] in
            guard let self else { return }
            self.copied = false
            self.address.title = shown
            self.address.image = NSImage(systemSymbolName: "doc.on.doc", accessibilityDescription: L("Copy"))
            self.update()
        }
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
    // Thousands, as speeds and plans are counted: 999 KB, then 1 MB.
    ByteCountFormatter.string(fromByteCount: Int64(bytes), countStyle: .decimal)
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
