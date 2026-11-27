import AppKit

/// The menu bar item: the state at a glance. A click opens a small panel with
/// the choices people change most; a right click opens the same as a menu.
final class StatusMenu: NSObject, NSMenuDelegate, NSPopoverDelegate {
    private let item = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)
    private let openWindow: () -> Void
    private let openSettings: () -> Void
    private let menu = NSMenu()
    private var popover: NSPopover?
    private var outside: Any?

    init(openWindow: @escaping () -> Void, openSettings: @escaping () -> Void) {
        self.openWindow = openWindow
        self.openSettings = openSettings
        super.init()
        menu.delegate = self
        item.button?.target = self
        item.button?.action = #selector(clicked(_:))
        item.button?.sendAction(on: [.leftMouseUp, .rightMouseUp])
        NotificationCenter.default.addObserver(self, selector: #selector(update), name: .modelChanged, object: nil)
        update()
    }

    @objc private func clicked(_ sender: Any?) {
        let event = NSApp.currentEvent
        if event?.type == .rightMouseUp || event?.modifierFlags.contains(.control) == true {
            closePanel()
            // The menu is attached only for this click, so that a plain click
            // stays free for the panel.
            item.menu = menu
            item.button?.performClick(nil)
            item.menu = nil
            return
        }
        if popover != nil { closePanel() } else { openPanel() }
    }

    private func openPanel() {
        guard let button = item.button else { return }
        let panel = NSPopover()
        panel.behavior = .transient
        panel.delegate = self
        panel.contentViewController = PanelViewController(
            openWindow: { [weak self] in self?.closePanel(); self?.openWindow() },
            openSettings: { [weak self] in self?.closePanel(); self?.openSettings() }
        )
        popover = panel
        Model.shared.refresh()
        Model.shared.watchStats(true)
        panel.show(relativeTo: button.bounds, of: button, preferredEdge: .minY)
        panel.contentViewController?.view.window?.makeKey()
        // A click anywhere else closes it, also in another app.
        outside = NSEvent.addGlobalMonitorForEvents(matching: [.leftMouseDown, .rightMouseDown]) { [weak self] _ in self?.closePanel() }
    }

    private func closePanel() {
        popover?.performClose(nil)
    }

    /// Closed means gone: the panel and what it read are let go of.
    func popoverDidClose(_ notification: Notification) {
        if let outside { NSEvent.removeMonitor(outside) }
        outside = nil
        if popover != nil { Model.shared.watchStats(false) }
        popover = nil
    }

    @objc private func update() {
        let status = Model.shared.status
        let symbol: String
        if Model.shared.busy {
            symbol = "arrow.triangle.2.circlepath"
        } else if Model.shared.helperReady, status.blocked {
            symbol = "lock.shield.fill"
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

        let toggle = NSMenuItem(title: status.running ? L("Disconnect") : (status.blocked ? L("Connect Again") : L("Connect")), action: #selector(toggle), keyEquivalent: "")
        toggle.target = self
        toggle.isEnabled = model.helperReady && !model.busy && (status.running || model.selectedServer != nil)
        menu.addItem(toggle)
        if model.helperReady, status.blocked {
            let unblock = NSMenuItem(title: L("Unblock"), action: #selector(unblock), keyEquivalent: "")
            unblock.target = self
            unblock.isEnabled = !model.busy
            menu.addItem(unblock)
        }
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

        let modes = NSMenuItem(title: L("Mode"), action: nil, keyEquivalent: "")
        let modeMenu = NSMenu()
        for (name, title) in [("proxy", L("Port Only")), ("system", L("System Proxy")), ("tun", L("TUN"))] {
            let entry = NSMenuItem(title: title, action: #selector(pickMode(_:)), keyEquivalent: "")
            entry.target = self
            entry.representedObject = name
            entry.state = name == model.view.mode ? .on : .off
            modeMenu.addItem(entry)
        }
        modes.submenu = modeMenu
        modes.isEnabled = model.helperReady && !model.busy
        menu.addItem(modes)
        menu.addItem(.separator())

        let open = NSMenuItem(title: L("Open FlClash"), action: #selector(open), keyEquivalent: "")
        open.target = self
        menu.addItem(open)
        let settings = NSMenuItem(title: L("Settings…"), action: #selector(settingsFromMenu), keyEquivalent: ",")
        settings.target = self
        menu.addItem(settings)
        menu.addItem(.separator())
        menu.addItem(withTitle: L("Quit FlClash"), action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
    }

    @objc private func toggle() { Model.shared.toggle() }
    @objc private func open() { openWindow() }
    @objc private func settingsFromMenu() { openSettings() }
    @objc private func unblock() { Model.shared.turnOff() }

    @objc private func pickServer(_ sender: NSMenuItem) {
        if let id = sender.representedObject as? String { Model.shared.select(server: id) }
    }

    @objc private func pickMode(_ sender: NSMenuItem) {
        if let name = sender.representedObject as? String { Model.shared.setMode(name) }
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
