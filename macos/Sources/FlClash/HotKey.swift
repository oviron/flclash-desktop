import AppKit
import Carbon.HIToolbox

/// One system-wide keyboard shortcut: it turns FlClash on or off from
/// anywhere. The system delivers it without any permission being asked.
final class HotKey {
    static let shared = HotKey()

    struct Combo: Equatable {
        var keyCode: UInt32
        var modifiers: UInt32  // Carbon modifier bits
        var title: String
    }

    private var ref: EventHotKeyRef?
    private var handler: EventHandlerRef?
    var action: (() -> Void)?

    var combo: Combo? {
        get {
            let defaults = UserDefaults.standard
            guard let title = defaults.string(forKey: "hotKeyTitle"), !title.isEmpty else { return nil }
            return Combo(keyCode: UInt32(defaults.integer(forKey: "hotKeyCode")), modifiers: UInt32(defaults.integer(forKey: "hotKeyModifiers")), title: title)
        }
        set {
            let defaults = UserDefaults.standard
            defaults.set(newValue?.title ?? "", forKey: "hotKeyTitle")
            defaults.set(Int(newValue?.keyCode ?? 0), forKey: "hotKeyCode")
            defaults.set(Int(newValue?.modifiers ?? 0), forKey: "hotKeyModifiers")
            register()
        }
    }

    /// Takes the shortcut from the system, or gives it back when there is none.
    func register() {
        if let ref { UnregisterEventHotKey(ref) }
        ref = nil
        guard let combo else { return }
        if handler == nil {
            var spec = EventTypeSpec(eventClass: OSType(kEventClassKeyboard), eventKind: UInt32(kEventHotKeyPressed))
            InstallEventHandler(GetApplicationEventTarget(), { _, _, _ in
                DispatchQueue.main.async { HotKey.shared.action?() }
                return noErr
            }, 1, &spec, nil, &handler)
        }
        let id = EventHotKeyID(signature: OSType(0x464C434C), id: 1)  // "FLCL"
        RegisterEventHotKey(combo.keyCode, combo.modifiers, id, GetApplicationEventTarget(), 0, &ref)
    }

    /// A key press as a shortcut, when it has a modifier other than Shift alone.
    static func combo(from event: NSEvent) -> Combo? {
        let flags = event.modifierFlags.intersection([.command, .option, .control, .shift])
        guard !flags.subtracting(.shift).isEmpty else { return nil }
        var carbon: UInt32 = 0
        var title = ""
        if flags.contains(.control) { carbon |= UInt32(controlKey); title += "⌃" }
        if flags.contains(.option) { carbon |= UInt32(optionKey); title += "⌥" }
        if flags.contains(.shift) { carbon |= UInt32(shiftKey); title += "⇧" }
        if flags.contains(.command) { carbon |= UInt32(cmdKey); title += "⌘" }
        let named: [UInt16: String] = [49: "Space", 36: "↩", 48: "⇥", 122: "F1", 120: "F2", 99: "F3", 118: "F4", 96: "F5", 97: "F6", 98: "F7", 100: "F8", 101: "F9", 109: "F10", 103: "F11", 111: "F12"]
        let key = named[event.keyCode] ?? (event.charactersIgnoringModifiers ?? "").uppercased()
        guard !key.isEmpty else { return nil }
        return Combo(keyCode: UInt32(event.keyCode), modifiers: carbon, title: title + key)
    }
}

/// A button that records a shortcut: click, press the keys. Escape leaves it
/// as it was, Delete clears it.
final class HotKeyRecorder: NSButton {
    private var monitor: Any?

    init() {
        super.init(frame: .zero)
        bezelStyle = .push
        target = self
        action = #selector(record)
        show()
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) { fatalError() }

    private func show() {
        title = HotKey.shared.combo?.title ?? L("Record Shortcut")
    }

    @objc private func record() {
        guard monitor == nil else { return }
        title = L("Press the keys…")
        monitor = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { [weak self] event in
            guard let self else { return event }
            if event.keyCode == 53 {  // Escape
            } else if event.keyCode == 51 || event.keyCode == 117 {  // Delete
                HotKey.shared.combo = nil
            } else if let combo = HotKey.combo(from: event) {
                HotKey.shared.combo = combo
            } else {
                NSSound.beep()
                return nil
            }
            self.stop()
            return nil
        }
    }

    private func stop() {
        if let monitor { NSEvent.removeMonitor(monitor) }
        monitor = nil
        show()
    }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        if window == nil { stop() }
    }
}
