import AppKit
import ServiceManagement

/// The few things that are set once: protection and starting at login.
final class SettingsWindowController: NSWindowController {
    private let protect = NSButton(checkboxWithTitle: L("Block everything that goes past the tunnel"), target: nil, action: nil)
    private let login = NSButton(checkboxWithTitle: L("Launch at Login"), target: nil, action: nil)
    private let port = NSTextField()
    private let autoConnect = NSButton(checkboxWithTitle: L("Connect when FlClash starts"), target: nil, action: nil)

    init() {
        let window = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: 460, height: 10),
            styleMask: [.titled, .closable], backing: .buffered, defer: false
        )
        window.title = L("Settings")
        window.isReleasedWhenClosed = false
        super.init(window: window)
        window.delegate = NSApp.delegate as? NSWindowDelegate

        protect.target = self
        protect.action = #selector(protectChanged)
        login.target = self
        login.action = #selector(loginChanged)
        autoConnect.target = self
        autoConnect.action = #selector(autoConnectChanged)

        let about = NSTextField(wrappingLabelWithString: L("While the tunnel is on, nothing leaves this Mac past it, IPv6 included. If the tunnel breaks, the internet stays off until it is back or you turn it off. The local network always works."))
        about.font = .systemFont(ofSize: 11)
        about.textColor = .secondaryLabelColor
        // The note sits under the title of its checkbox, not under the box.
        let note = NSStackView(views: [about])
        note.edgeInsets = NSEdgeInsets(top: 0, left: 20, bottom: 0, right: 0)

        let digits = NumberFormatter()
        digits.numberStyle = .none
        digits.minimum = 1024
        digits.maximum = 65535
        digits.allowsFloats = false
        port.formatter = digits
        port.alignment = .right
        port.target = self
        port.action = #selector(portChanged)
        port.widthAnchor.constraint(equalToConstant: 70).isActive = true
        let portLabel = NSTextField(labelWithString: L("Local port:"))
        let portNote = NSTextField(labelWithString: L("HTTP and SOCKS on 127.0.0.1"))
        portNote.font = .systemFont(ofSize: 11)
        portNote.textColor = .secondaryLabelColor
        let portRow = NSStackView(views: [portLabel, port, portNote])
        portRow.orientation = .horizontal
        portRow.alignment = .firstBaseline
        portRow.spacing = 8

        let column = NSStackView(views: [protect, note, portRow, login, autoConnect])
        column.orientation = .vertical
        column.alignment = .leading
        column.spacing = 6
        column.setCustomSpacing(16, after: note)
        column.setCustomSpacing(16, after: portRow)
        column.edgeInsets = NSEdgeInsets(top: 20, left: 24, bottom: 22, right: 24)
        column.translatesAutoresizingMaskIntoConstraints = false
        let content = NSView()
        content.addSubview(column)
        NSLayoutConstraint.activate([
            column.topAnchor.constraint(equalTo: content.topAnchor),
            column.leadingAnchor.constraint(equalTo: content.leadingAnchor),
            column.trailingAnchor.constraint(equalTo: content.trailingAnchor),
            column.bottomAnchor.constraint(equalTo: content.bottomAnchor),
            content.widthAnchor.constraint(equalToConstant: 460),
            about.widthAnchor.constraint(lessThanOrEqualToConstant: 390),
        ])
        window.contentView = content
        window.center()

        NotificationCenter.default.addObserver(self, selector: #selector(update), name: .modelChanged, object: nil)
        update()
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) { fatalError() }

    @objc private func update() {
        protect.state = Model.shared.view.protect ? .on : .off
        protect.isEnabled = !Model.shared.busy
        login.state = SMAppService.mainApp.status == .enabled ? .on : .off
        autoConnect.state = Model.shared.view.autoConnect ? .on : .off
        // Not while the field is being typed in.
        if port.currentEditor() == nil { port.integerValue = Model.shared.view.port }
        port.isEnabled = !Model.shared.busy
    }

    @objc private func autoConnectChanged() {
        Model.shared.setAutoConnect(autoConnect.state == .on)
    }

    @objc private func portChanged() {
        Model.shared.setPort(port.integerValue)
    }

    @objc private func protectChanged() {
        Model.shared.setProtect(protect.state == .on)
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
