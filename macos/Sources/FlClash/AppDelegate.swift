import AppKit

final class AppDelegate: NSObject, NSApplicationDelegate, NSWindowDelegate {
    private var statusMenu: StatusMenu?
    private var window: MainWindowController?
    private var settings: SettingsWindowController?

    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.mainMenu = makeMainMenu()
        statusMenu = StatusMenu(openWindow: { [weak self] in self?.showWindow() })
        Model.shared.start()
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
        }
        // With a window the app is an ordinary one, in the Dock and the app switcher.
        NSApp.setActivationPolicy(.regular)
        window?.showWindow(nil)
        window?.window?.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
        Model.shared.refresh()
        Model.shared.watchStats(true)
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
        fileMenu.addItem(withTitle: L("Import a Policy…"), action: #selector(MainWindowController.importPolicy(_:)), keyEquivalent: "")
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
