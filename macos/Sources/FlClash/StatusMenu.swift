import AppKit
import ServiceManagement

/// The menu bar item: the state at a glance, and the choices people change most.
final class StatusMenu: NSObject, NSMenuDelegate {
    private let item = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)
    private let openWindow: () -> Void

    init(openWindow: @escaping () -> Void) {
        self.openWindow = openWindow
        super.init()
        let menu = NSMenu()
        menu.delegate = self
        item.menu = menu
        NotificationCenter.default.addObserver(self, selector: #selector(update), name: .modelChanged, object: nil)
        update()
    }

    @objc private func update() {
        let status = Model.shared.status
        let symbol: String
        if Model.shared.busy {
            symbol = "arrow.triangle.2.circlepath"
        } else if !Model.shared.helperReady || (status.running && !status.captured) {
            symbol = "exclamationmark.shield"
        } else if status.running {
            symbol = "checkmark.shield.fill"
        } else {
            symbol = "shield"
        }
        let image = NSImage(systemSymbolName: symbol, accessibilityDescription: status.headline)
        image?.isTemplate = true
        item.button?.image = image
        item.button?.toolTip = "FlClash: " + status.headline
    }

    func menuNeedsUpdate(_ menu: NSMenu) {
        let model = Model.shared
        let status = model.status
        menu.removeAllItems()

        var headline = status.headline
        if status.running, !status.server.isEmpty { headline += ": " + status.server }
        let state = NSMenuItem(title: headline, action: nil, keyEquivalent: "")
        state.isEnabled = false
        menu.addItem(state)

        let toggle = NSMenuItem(title: status.running ? L("Disconnect") : L("Connect"), action: #selector(toggle), keyEquivalent: "")
        toggle.target = self
        toggle.isEnabled = model.helperReady && !model.busy && (status.running || model.selectedServer != nil)
        menu.addItem(toggle)
        menu.addItem(.separator())

        let servers = NSMenuItem(title: L("Server"), action: nil, keyEquivalent: "")
        let serverMenu = NSMenu()
        let groups = Dictionary(grouping: model.view.servers, by: \.group)
        for group in groups.keys.sorted() {
            if groups.count > 1 {
                serverMenu.addItem(.sectionHeader(title: group))
            }
            for server in groups[group] ?? [] {
                let entry = NSMenuItem(title: server.name, action: #selector(pickServer(_:)), keyEquivalent: "")
                entry.target = self
                entry.representedObject = server.id
                entry.state = server.id == model.view.selected ? .on : .off
                serverMenu.addItem(entry)
            }
        }
        if model.view.servers.isEmpty {
            let none = NSMenuItem(title: L("No servers yet"), action: nil, keyEquivalent: "")
            none.isEnabled = false
            serverMenu.addItem(none)
        }
        servers.submenu = serverMenu
        menu.addItem(servers)

        let policies = NSMenuItem(title: L("Routing"), action: nil, keyEquivalent: "")
        let policyMenu = NSMenu()
        for policy in model.view.policies {
            let entry = NSMenuItem(title: policyTitle(policy.name), action: #selector(pickPolicy(_:)), keyEquivalent: "")
            entry.target = self
            entry.representedObject = policy.name
            entry.state = policy.name == model.view.policy ? .on : .off
            policyMenu.addItem(entry)
        }
        policies.submenu = policyMenu
        menu.addItem(policies)
        menu.addItem(.separator())

        let open = NSMenuItem(title: L("Open FlClash"), action: #selector(open), keyEquivalent: "")
        open.target = self
        menu.addItem(open)
        let login = NSMenuItem(title: L("Launch at Login"), action: #selector(toggleLogin), keyEquivalent: "")
        login.target = self
        login.state = SMAppService.mainApp.status == .enabled ? .on : .off
        menu.addItem(login)
        menu.addItem(.separator())
        menu.addItem(withTitle: L("Quit FlClash"), action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
    }

    @objc private func toggle() { Model.shared.toggle() }
    @objc private func open() { openWindow() }

    /// The system keeps the list of what starts at login; this adds or removes the app.
    @objc private func toggleLogin() {
        let service = SMAppService.mainApp
        do {
            if service.status == .enabled { try service.unregister() } else { try service.register() }
        } catch {
            let alert = NSAlert(error: error)
            NSApp.activate(ignoringOtherApps: true)
            alert.runModal()
        }
    }

    @objc private func pickServer(_ sender: NSMenuItem) {
        if let id = sender.representedObject as? String { Model.shared.select(server: id) }
    }

    @objc private func pickPolicy(_ sender: NSMenuItem) {
        if let name = sender.representedObject as? String { Model.shared.select(policy: name) }
    }
}

/// The built-in policies have names for people; imported ones keep their own.
func policyTitle(_ name: String) -> String {
    switch name {
    case "all": return L("Everything through the server")
    case "lan-direct": return L("Local addresses direct")
    case "ru-direct": return L("Russia direct")
    default: return name
    }
}
