import AppKit
import ServiceManagement

/// The few things that are set once: protection, the local port, and what
/// happens at start. It is laid out the way System Settings is: groups of
/// rows, a name on the left and its control on the right.
final class SettingsWindowController: NSWindowController {
    private let protect = NSSwitch()
    private let login = NSSwitch()
    private let autoConnect = NSSwitch()
    private let port = NSTextField()
    private let lan = NSSwitch()
    private let auth = NSTextField()
    private let dns = NSTextField()
    private let hotKey = HotKeyRecorder()

    init() {
        let window = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: 500, height: 10),
            styleMask: [.titled, .closable], backing: .buffered, defer: false
        )
        window.title = L("Settings")
        window.isReleasedWhenClosed = false
        super.init(window: window)
        window.delegate = NSApp.delegate as? NSWindowDelegate

        for (toggle, action) in [(protect, #selector(protectChanged)), (login, #selector(loginChanged)), (autoConnect, #selector(autoConnectChanged))] {
            toggle.target = self
            toggle.action = action
        }

        let digits = NumberFormatter()
        digits.numberStyle = .none
        digits.minimum = 1024
        digits.maximum = 65535
        digits.allowsFloats = false
        port.formatter = digits
        port.alignment = .right
        port.target = self
        port.action = #selector(portChanged)
        port.widthAnchor.constraint(equalToConstant: 76).isActive = true
        lan.target = self
        lan.action = #selector(lanChanged)
        auth.placeholderString = L("user:password")
        auth.target = self
        auth.action = #selector(authChanged)
        auth.widthAnchor.constraint(equalToConstant: 170).isActive = true
        dns.target = self
        dns.action = #selector(dnsChanged)
        dns.widthAnchor.constraint(equalToConstant: 230).isActive = true

        let column = NSStackView(views: [
            group([
                row(L("Let nothing go past FlClash"),
                    L("In TUN mode nothing leaves this Mac past the tunnel, IPv6 included. If the connection breaks, the internet stays off until it is back or you turn it off; in System Proxy mode that holds for the apps that follow the system proxy. The local network always works."),
                    protect),
            ]),
            group([
                row(L("Local port"), L("HTTP and SOCKS on 127.0.0.1"), port),
                row(L("Let the local network use it"), L("Other devices at home can point at this Mac's address and the port."), lan),
                row(L("Login for other devices"), L("Empty asks for none. This Mac is never asked."), auth),
            ]),
            group([
                row(L("DNS in TUN mode"), L("Resolves names for what goes through the tunnel."), dns),
            ]),
            group([
                row(L("Shortcut to connect"), L("Works from any app."), hotKey),
            ]),
            group([
                row(L("Launch at Login"), nil, login),
                row(L("Connect when FlClash starts"), nil, autoConnect),
            ]),
        ])
        column.orientation = .vertical
        column.alignment = .leading
        column.spacing = 14
        column.edgeInsets = NSEdgeInsets(top: 20, left: 20, bottom: 20, right: 20)
        column.translatesAutoresizingMaskIntoConstraints = false
        let content = NSView()
        content.addSubview(column)
        NSLayoutConstraint.activate([
            column.topAnchor.constraint(equalTo: content.topAnchor),
            column.leadingAnchor.constraint(equalTo: content.leadingAnchor),
            column.trailingAnchor.constraint(equalTo: content.trailingAnchor),
            column.bottomAnchor.constraint(equalTo: content.bottomAnchor),
            content.widthAnchor.constraint(equalToConstant: 500),
        ])
        for box in column.arrangedSubviews {
            box.widthAnchor.constraint(equalTo: column.widthAnchor, constant: -40).isActive = true
        }
        window.contentView = content
        content.layoutSubtreeIfNeeded()
        window.setContentSize(content.fittingSize)
        window.center()
        // Nothing is being typed until the person clicks the field.
        window.initialFirstResponder = nil
        DispatchQueue.main.async { [weak window] in window?.makeFirstResponder(nil) }

        NotificationCenter.default.addObserver(self, selector: #selector(update), name: .modelChanged, object: nil)
        update()
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) { fatalError() }

    /// One row: what it is, a note under it when it needs one, and its control.
    private func row(_ title: String, _ note: String?, _ control: NSView) -> NSView {
        let name = NSTextField(labelWithString: title)
        let words = NSStackView(views: [name])
        words.orientation = .vertical
        words.alignment = .leading
        words.spacing = 3
        if let note {
            let about = NSTextField(wrappingLabelWithString: note)
            about.font = .systemFont(ofSize: 11)
            about.textColor = .secondaryLabelColor
            // The width the note wraps at is known before layout, so the
            // window is sized for all its lines from the start.
            about.preferredMaxLayoutWidth = 330
            about.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
            about.setContentCompressionResistancePriority(.required, for: .vertical)
            words.addArrangedSubview(about)
        }
        control.setContentHuggingPriority(.required, for: .horizontal)
        control.setContentCompressionResistancePriority(.required, for: .horizontal)
        // The words take the room; the control keeps to the trailing edge.
        let gap = NSView()
        gap.setContentHuggingPriority(.init(1), for: .horizontal)
        let line = NSStackView(views: [words, gap, control])
        line.orientation = .horizontal
        line.alignment = .centerY
        line.spacing = 12
        line.translatesAutoresizingMaskIntoConstraints = false
        // Margins by constraints: a stack view lets its own insets be squeezed.
        let padded = NSView()
        padded.addSubview(line)
        NSLayoutConstraint.activate([
            line.topAnchor.constraint(equalTo: padded.topAnchor, constant: 11),
            line.bottomAnchor.constraint(equalTo: padded.bottomAnchor, constant: -11),
            line.leadingAnchor.constraint(equalTo: padded.leadingAnchor, constant: 14),
            line.trailingAnchor.constraint(equalTo: padded.trailingAnchor, constant: -14),
            padded.heightAnchor.constraint(greaterThanOrEqualToConstant: 44),
        ])
        return padded
    }

    /// Rows that belong together, on one rounded panel with lines between them.
    private func group(_ rows: [NSView]) -> NSView {
        let box = NSBox()
        box.boxType = .custom
        box.titlePosition = .noTitle
        box.borderWidth = 0
        box.cornerRadius = 10
        box.fillColor = .quaternarySystemFill
        box.contentViewMargins = .zero
        let column = NSStackView()
        column.orientation = .vertical
        column.alignment = .centerX
        column.spacing = 0
        for (index, row) in rows.enumerated() {
            if index > 0 {
                let line = NSBox()
                line.boxType = .separator
                column.addArrangedSubview(line)
                // The line stops short of the edges, as the words do.
                line.widthAnchor.constraint(equalTo: column.widthAnchor, constant: -28).isActive = true
            }
            column.addArrangedSubview(row)
            row.widthAnchor.constraint(equalTo: column.widthAnchor).isActive = true
        }
        column.translatesAutoresizingMaskIntoConstraints = false
        box.contentView?.addSubview(column)
        NSLayoutConstraint.activate([
            column.topAnchor.constraint(equalTo: box.topAnchor),
            column.leadingAnchor.constraint(equalTo: box.leadingAnchor),
            column.trailingAnchor.constraint(equalTo: box.trailingAnchor),
            column.bottomAnchor.constraint(equalTo: box.bottomAnchor),
        ])
        return box
    }

    @objc private func update() {
        protect.state = Model.shared.view.protect ? .on : .off
        protect.isEnabled = !Model.shared.busy
        login.state = SMAppService.mainApp.status == .enabled ? .on : .off
        autoConnect.state = Model.shared.view.autoConnect ? .on : .off
        // Not while the field is being typed in.
        if port.currentEditor() == nil { port.integerValue = Model.shared.view.port }
        port.isEnabled = !Model.shared.busy
        lan.state = Model.shared.view.lan ? .on : .off
        lan.isEnabled = !Model.shared.busy
        if auth.currentEditor() == nil { auth.stringValue = Model.shared.view.auth }
        auth.isEnabled = Model.shared.view.lan && !Model.shared.busy
        if dns.currentEditor() == nil { dns.stringValue = Model.shared.view.dns }
        dns.isEnabled = !Model.shared.busy
        // A value the engine refused is said once, by the window.
        if let problem = Model.shared.problem, let window, window.attachedSheet == nil, window.isKeyWindow {
            Model.shared.dismissProblem()
            let alert = NSAlert()
            alert.messageText = L("The setting was not changed")
            alert.informativeText = problem
            alert.beginSheetModal(for: window)
        }
    }

    @objc private func lanChanged() { Model.shared.setLAN(lan.state == .on) }
    @objc private func authChanged() { Model.shared.setAuth(auth.stringValue.trimmingCharacters(in: .whitespaces)) }
    @objc private func dnsChanged() { Model.shared.setDNS(dns.stringValue.trimmingCharacters(in: .whitespaces)) }

    @objc private func protectChanged() {
        Model.shared.setProtect(protect.state == .on)
    }

    @objc private func autoConnectChanged() {
        Model.shared.setAutoConnect(autoConnect.state == .on)
    }

    @objc private func portChanged() {
        Model.shared.setPort(port.integerValue)
    }

    /// The system keeps the list of what starts at login; this adds or removes the app.
    @objc private func loginChanged() {
        let service = SMAppService.mainApp
        do {
            if login.state == .on { try service.register() } else { try service.unregister() }
        } catch {
            if let window { NSAlert(error: error).beginSheetModal(for: window) }
        }
        update()
    }
}
