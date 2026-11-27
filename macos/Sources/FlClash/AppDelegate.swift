import AppKit
import UniformTypeIdentifiers

final class AppDelegate: NSObject, NSApplicationDelegate, NSWindowDelegate {
    private var statusMenu: StatusMenu?
    private var window: MainWindowController?
    private var settings: SettingsWindowController?

    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.mainMenu = makeMainMenu()
        statusMenu = StatusMenu(openWindow: { [weak self] in self?.showWindow() },
                                openSettings: { [weak self] in self?.showSettings(nil) })
        Model.shared.start()
        HotKey.shared.action = { Model.shared.toggle() }
        HotKey.shared.register()
        // Started by hand, the app shows its window; started at login it stays in the menu bar.
        if !CommandLine.arguments.contains("--background"), !startedAtLogin {
            showWindow()
        }
    }

    /// The system marks the launch it makes for a login item.
    private var startedAtLogin: Bool {
        guard let event = NSAppleEventManager.shared().currentAppleEvent, event.eventID == kAEOpenApplication else { return false }
        return event.paramDescriptor(forKeyword: keyAEPropData)?.enumCodeValue == keyAELaunchedAsLogInItem
    }

    /// The app lives in the menu bar; the window comes and goes.
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { false }

    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        showWindow()
        return false
    }

    @objc private func saveBackup(_ sender: Any?) {
        let panel = NSSavePanel()
        panel.nameFieldStringValue = "FlClash backup.json"
        panel.message = L("The backup holds the keys of your servers and the addresses of your subscriptions. Keep it where others cannot read it.")
        guard panel.runModal() == .OK, let url = panel.url else { return }
        Model.shared.writeBackup(to: url.path) { [weak self] error in
            if let error { self?.say(L("The backup was not saved"), error) }
        }
    }

    @objc private func restoreBackup(_ sender: Any?) {
        let panel = NSOpenPanel()
        panel.allowedContentTypes = [.json]
        panel.message = L("Everything set up now — servers, subscriptions, policies, settings — is replaced by what the backup holds.")
        guard panel.runModal() == .OK, let url = panel.url else { return }
        Model.shared.readBackup(from: url.path) { [weak self] error in
            if let error { self?.say(L("The backup was not restored"), error) }
        }
    }

    private func say(_ title: String, _ text: String) {
        let alert = NSAlert()
        alert.messageText = title
        alert.informativeText = text
        alert.runModal()
    }

    @objc func showSettings(_ sender: Any?) {
        if settings == nil { settings = SettingsWindowController() }
        // Settings can be opened from the menu bar with no window: be an app for it.
        NSApp.setActivationPolicy(.regular)
        settings?.showWindow(nil)
        settings?.window?.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }

    func showWindow() {
        if window == nil {
            let controller = MainWindowController()
            controller.window?.delegate = self
            window = controller
            Model.shared.watchStats(true)
        }
        // With a window the app is an ordinary one, in the Dock and the app switcher.
        NSApp.setActivationPolicy(.regular)
        window?.showWindow(nil)
        window?.window?.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
        Model.shared.refresh()
    }

    /// The command line may have changed the lists while the window was aside.
    func windowDidBecomeKey(_ notification: Notification) {
        if (notification.object as? NSWindow) === window?.window { Model.shared.refresh() }
    }

    func windowWillClose(_ notification: Notification) {
        // Let go of the window and everything in it: closed means gone, not hidden.
        if (notification.object as? NSWindow) === settings?.window {
            settings = nil
        } else {
            window = nil
            Model.shared.watchStats(false)
        }
        // With no window left the app goes back to the menu bar.
        if window == nil, settings == nil { NSApp.setActivationPolicy(.accessory) }
    }

    private func makeMainMenu() -> NSMenu {
        let main = NSMenu()

        let appItem = NSMenuItem()
        let appMenu = NSMenu()
        appMenu.addItem(withTitle: L("About FlClash"), action: #selector(NSApplication.orderFrontStandardAboutPanel(_:)), keyEquivalent: "")
        appMenu.addItem(.separator())
        appMenu.addItem(withTitle: L("Settings…"), action: #selector(showSettings(_:)), keyEquivalent: ",").target = self
        appMenu.addItem(.separator())
        appMenu.addItem(withTitle: L("Hide FlClash"), action: #selector(NSApplication.hide(_:)), keyEquivalent: "h")
        appMenu.addItem(withTitle: L("Quit FlClash"), action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
        appItem.submenu = appMenu
        main.addItem(appItem)

        let fileItem = NSMenuItem()
        let fileMenu = NSMenu(title: L("File"))
        fileMenu.addItem(withTitle: L("Import from Clipboard"), action: #selector(MainWindowController.importFromClipboard(_:)), keyEquivalent: "V").keyEquivalentModifierMask = [.command, .shift]
        fileMenu.addItem(withTitle: L("Import from File…"), action: #selector(MainWindowController.importFromFile(_:)), keyEquivalent: "o")
        fileMenu.addItem(withTitle: L("Add a Subscription…"), action: #selector(MainWindowController.addSubscription(_:)), keyEquivalent: "")
        fileMenu.addItem(withTitle: L("Update Subscriptions"), action: #selector(MainWindowController.updateSubscriptions(_:)), keyEquivalent: "r")
        fileMenu.addItem(withTitle: L("New Policy…"), action: #selector(MainWindowController.newPolicy(_:)), keyEquivalent: "n")
        fileMenu.addItem(withTitle: L("Import a Policy…"), action: #selector(MainWindowController.importPolicy(_:)), keyEquivalent: "")
        fileMenu.addItem(.separator())
        fileMenu.addItem(withTitle: L("Save a Backup…"), action: #selector(saveBackup(_:)), keyEquivalent: "").target = self
        fileMenu.addItem(withTitle: L("Restore from a Backup…"), action: #selector(restoreBackup(_:)), keyEquivalent: "").target = self
        fileMenu.addItem(.separator())
        fileMenu.addItem(withTitle: L("Test Latency"), action: #selector(MainWindowController.testLatency(_:)), keyEquivalent: "t")
        fileMenu.addItem(.separator())
        fileMenu.addItem(withTitle: L("Close Window"), action: #selector(NSWindow.performClose(_:)), keyEquivalent: "w")
        fileItem.submenu = fileMenu
        main.addItem(fileItem)

        let editItem = NSMenuItem()
        let editMenu = NSMenu(title: L("Edit"))
        editMenu.addItem(withTitle: L("Cut"), action: #selector(NSText.cut(_:)), keyEquivalent: "x")
        editMenu.addItem(withTitle: L("Copy"), action: #selector(NSText.copy(_:)), keyEquivalent: "c")
        editMenu.addItem(withTitle: L("Paste"), action: #selector(NSText.paste(_:)), keyEquivalent: "v")
        editMenu.addItem(withTitle: L("Delete"), action: #selector(ServersViewController.delete(_:)), keyEquivalent: "\u{8}")
        editMenu.addItem(withTitle: L("Select All"), action: #selector(NSText.selectAll(_:)), keyEquivalent: "a")
        editMenu.addItem(.separator())
        editMenu.addItem(withTitle: L("Find"), action: #selector(MainWindowController.focusSearch(_:)), keyEquivalent: "f")
        editItem.submenu = editMenu
        main.addItem(editItem)

        let viewItem = NSMenuItem()
        let viewMenu = NSMenu(title: L("View"))
        viewMenu.addItem(withTitle: L("Servers"), action: #selector(MainWindowController.showServers(_:)), keyEquivalent: "1")
        viewMenu.addItem(withTitle: L("Routing"), action: #selector(MainWindowController.showRouting(_:)), keyEquivalent: "2")
        viewMenu.addItem(withTitle: L("Connections"), action: #selector(MainWindowController.showConnections(_:)), keyEquivalent: "3")
        viewMenu.addItem(withTitle: L("Log"), action: #selector(MainWindowController.showLog(_:)), keyEquivalent: "4")
        viewItem.submenu = viewMenu
        main.addItem(viewItem)

        let windowItem = NSMenuItem()
        let windowMenu = NSMenu(title: L("Window"))
        windowMenu.addItem(withTitle: L("Minimize"), action: #selector(NSWindow.performMiniaturize(_:)), keyEquivalent: "m")
        windowItem.submenu = windowMenu
        main.addItem(windowItem)
        NSApp.windowsMenu = windowMenu
        return main
    }
}
