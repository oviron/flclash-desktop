import AppKit
import CFlClash

extension Notification.Name {
    static let modelChanged = Notification.Name("flclash.modelChanged")
    /// The traffic counters changed. They change every second, so they have a
    /// notification of their own: the lists need not be rebuilt for them.
    static let statsChanged = Notification.Name("flclash.statsChanged")
}

func L(_ key: String) -> String { NSLocalizedString(key, comment: "") }

/// What the app knows: the shared Go model (internal/model), the same one
/// the Windows app runs on. It keeps the rules: what is in progress, which
/// line a request waits on, when the counters and the exit address are read.
/// This side holds the last state it sent, so the menu and the window read it
/// and hear when it changes, and hands it what the person does.
final class Model {
    static let shared = Model()

    /// The app was pointed at a helper or a data folder of its own, as a test
    /// run is. Windows mark themselves then.
    static let testEnvironment: Bool = {
        let environment = ProcessInfo.processInfo.environment
        return environment["FLCLASH_HELPER_SOCK"] != nil || environment["FLCLASH_HOME"] != nil
    }()

    private(set) var view = EngineView()
    private(set) var status = TunnelStatus()
    /// A change of the tunnel is on its way to the helper.
    private(set) var busy = false
    /// The last thing that went wrong, for the window to show until it is dismissed.
    private(set) var problem: String?

    /// What measuring a server gave; nothing for a server that was not measured.
    enum Delay: Equatable {
        case waiting
        case ms(Int)
        case failed(String)
    }

    private(set) var delays: [String: Delay] = [:]

    /// The traffic of the core, while a window watches it and something runs.
    private(set) var stats: Stats?
    /// The speeds of the last minute, oldest first, for the graph.
    private(set) var history: [(down: Int, up: Int)] = []
    static let historyLength = 60

    /// Where the running server lets traffic out; nil until it is known, and
    /// when it could not be found out.
    private(set) var exit: Exit?

    /// The subscriptions that are being read right now.
    private(set) var updating: Set<String> = []

    /// The helper is there and takes this app's requests.
    private(set) var helperReady = false

    var selectedServer: Server? { view.servers.first { $0.id == view.selected } }

    /// What waits for a reply, by the ID of its message.
    private var pending: [Int: (Reply) -> Void] = [:]
    private var lastID = 0

    func start() {
        let language = Bundle.main.preferredLocalizations.first ?? "en"
        language.withCString { FlcStart(UnsafeMutablePointer(mutating: $0), receive) }
    }

    /// An event of the model, as it comes from the engine.
    fileprivate struct Event: Decodable {
        let event: String
        var id: Int? = nil
        var model: Snapshot? = nil
        var reply: Reply? = nil
    }

    fileprivate struct Snapshot: Decodable {
        struct Measured: Decodable {
            let state: String
            var ms: Int? = nil
            var error: String? = nil
        }

        let view: EngineView
        let status: TunnelStatus
        let busy: Bool
        var problem: String? = nil
        let delays: [String: Measured]
        var stats: Stats? = nil
        let history: [[Int]]
        var exit: Exit? = nil
        let updating: [String]
        let helperReady: Bool
    }

    /// Takes an event on the main thread: the state first, then the reply
    /// that waits on it, which the engine sends after the state it made.
    fileprivate func take(_ event: Event) {
        if let snapshot = event.model { take(snapshot) }
        if event.event == "reply", let id = event.id, let done = pending.removeValue(forKey: id) {
            done(event.reply ?? Reply(error: nil, view: nil, status: nil, imported: nil))
        }
    }

    private func take(_ s: Snapshot) {
        var delays: [String: Delay] = [:]
        for (id, d) in s.delays {
            switch d.state {
            case "waiting": delays[id] = .waiting
            case "ms": delays[id] = .ms(d.ms ?? 0)
            default: delays[id] = .failed(d.error ?? "")
            }
        }
        let problem = s.problem.flatMap { $0.isEmpty ? nil : $0 }
        let updating = Set(s.updating)
        let history = s.history.compactMap { $0.count == 2 ? (down: $0[0], up: $0[1]) : nil }
        // The counters change every second; the lists are redrawn only when
        // something else did.
        let changed = view != s.view || status != s.status || busy != s.busy || self.problem != problem
            || self.delays != delays || exit != s.exit || self.updating != updating || helperReady != s.helperReady
        let counted = stats != s.stats || !self.history.elementsEqual(history, by: { $0 == $1 })
        view = s.view
        status = s.status
        busy = s.busy
        self.problem = problem
        self.delays = delays
        stats = s.stats
        self.history = history
        exit = s.exit
        self.updating = updating
        helperReady = s.helperReady
        if changed { NotificationCenter.default.post(name: .modelChanged, object: self) }
        if counted { NotificationCenter.default.post(name: .statsChanged, object: self) }
    }

    /// Hands the model an action; with done, its reply comes back to it.
    private func send(_ action: String, _ fields: [String: Any] = [:], then done: ((Reply) -> Void)? = nil) {
        var message = fields
        message["action"] = action
        if let done {
            lastID += 1
            message["id"] = lastID
            pending[lastID] = done
        }
        guard let data = try? JSONSerialization.data(withJSONObject: message),
              let text = String(data: data, encoding: .utf8)
        else { return }
        text.withCString { FlcSend(UnsafeMutablePointer(mutating: $0)) }
    }

    /// An engine request on one of the model's lines: "change" for what alters
    /// what runs, "do" for the rest, "aside" for quick reads beside the
    /// changes, "subscription" for what waits for a provider. A quiet
    /// request's error goes to done alone.
    private func request(_ line: String, _ request: [String: String], quiet: Bool = false, then done: ((Reply) -> Void)? = nil) {
        send(line, ["request": request, "quiet": quiet], then: done)
    }

    func refresh() { send("refresh") }
    func toggle() { send("toggle") }
    func turnOff() { send("turnOff") }
    func dismissProblem() { send("dismiss") }

    /// Chooses a server and turns the tunnel on through it.
    func connect(server: String) { send("connect", ["server": server]) }

    /// Measures the servers one after another; results arrive one by one.
    func measure(servers: [String]) { send("measure", ["servers": servers]) }

    /// A window that shows traffic asks for the counters; without one nobody
    /// reads them. The window and the menu bar panel both watch.
    func watchStats(_ on: Bool) { send("watch", ["on": on]) }

    func select(server: String = "", policy: String = "") {
        request("change", ["op": "select", "server": server, "policy": policy])
    }

    /// The tunnel needs the helper: the engine installs it first, through the
    /// system's prompt. Declined, the mode stays, and the switches are drawn again.
    func setMode(_ mode: String) {
        guard mode != view.mode else { return }
        request("change", ["op": "mode", "text": mode]) { _ in
            NotificationCenter.default.post(name: .modelChanged, object: self)
        }
    }

    /// Installs the helper through the system's prompt; nil is success or a
    /// prompt the person declined.
    func installHelper(then done: @escaping (String?) -> Void) {
        send("installHelper") { done($0.error) }
    }

    /// Removes the helper through the system's prompt; nil is success or a
    /// prompt the person declined.
    func removeHelper(then done: @escaping (String?) -> Void) {
        send("removeHelper") { done($0.error) }
    }

    /// Turns off what runs, before the app quits.
    func stop() { FlcStop() }

    func setPort(_ port: Int) {
        guard port != view.port else { return }
        request("change", ["op": "port", "text": String(port)])
    }

    func setLAN(_ on: Bool) {
        guard on != view.lan else { return }
        request("change", ["op": "lan", "text": on ? "on" : "off"])
    }

    func setAuth(_ auth: String) {
        guard auth != view.auth else { return }
        request("change", ["op": "auth", "text": auth])
    }

    func setDNS(_ address: String) {
        guard address != view.dns else { return }
        request("change", ["op": "dns", "text": address])
    }

    func setProtect(_ on: Bool) { request("change", ["op": "protect", "text": on ? "on" : "off"]) }

    func setAutoConnect(_ on: Bool) { request("do", ["op": "autoConnect", "text": on ? "on" : "off"]) }

    func remove(servers: [String]) {
        for id in servers { request("do", ["op": "remove", "server": id]) }
    }

    func importServers(_ text: String, then done: @escaping (ImportResult?, String?) -> Void) {
        request("do", ["op": "import", "text": text], quiet: true) { done($0.imported, $0.error) }
    }

    /// A server as a share link; it carries the keys of the server.
    func link(server: String, then done: @escaping (String?, String?) -> Void) {
        request("do", ["op": "link", "server": server], quiet: true) { done($0.link, $0.error) }
    }

    func importPolicy(name: String, text: String, then done: @escaping (String?) -> Void) {
        request("do", ["op": "policyImport", "name": name, "text": text], quiet: true) { done($0.error) }
    }

    func remove(policy: String) { request("do", ["op": "policyRemove", "name": policy]) }

    func readPolicy(_ name: String, then done: @escaping (PolicyDocument?, String?) -> Void) {
        request("do", ["op": "policyRead", "name": name], quiet: true) { done($0.document, $0.error) }
    }

    /// Saves a policy; a tunnel that runs by it takes the new rules at once.
    func writePolicy(_ policy: [String: String], then done: @escaping (String?) -> Void) {
        request("change", policy, quiet: true) { done($0.error) }
    }

    func writeBackup(to path: String, then done: @escaping (String?) -> Void) {
        request("do", ["op": "backupWrite", "name": path], quiet: true) { done($0.error) }
    }

    func readBackup(from path: String, then done: @escaping (String?) -> Void) {
        request("do", ["op": "backupRead", "name": path], quiet: true) { done($0.error) }
    }

    func close(connection id: String) { request("aside", ["op": "close", "name": id]) }

    /// The end of the helper's log.
    func readLog(then done: @escaping (String?) -> Void) {
        request("aside", ["op": "log"]) { done($0.log) }
    }

    func addSubscription(name: String, address: String, then done: @escaping (ImportResult?, String?) -> Void) {
        request("subscription", ["op": "subAdd", "name": name, "text": address]) { done($0.imported, $0.error) }
    }

    func updateSubscription(_ name: String, then done: @escaping (String?) -> Void) {
        request("subscription", ["op": "subUpdate", "name": name]) { done($0.error) }
    }

    func editSubscription(_ name: String, address: String, agent: String, every: Int, skip: String, then done: @escaping (String?) -> Void) {
        request("subscription", ["op": "subEdit", "name": name, "text": address, "agent": agent, "every": String(every), "skip": skip]) { done($0.error) }
    }

    func updateAllSubscriptions() { send("updateAll") }

    func removeSubscription(_ name: String) { request("do", ["op": "subRemove", "name": name]) }
}

/// Hears the engine, off the main thread: the event is read here and taken
/// on the main thread, in the order it came.
private let receive: FlcEmit = { raw in
    guard let raw else { return }
    let data = Data(bytes: raw, count: strlen(raw))
    guard let event = try? JSONDecoder().decode(Model.Event.self, from: data) else { return }
    DispatchQueue.main.async { Model.shared.take(event) }
}

extension TunnelStatus {
    /// One line for people: what is true right now.
    var headline: String {
        // Only the tunnel needs the helper.
        if !Model.shared.helperReady { return installed ? L("Helper needs an update") : L("Helper is not installed") }
        if blocked { return mode == "system" ? L("The proxy is down") : L("Internet is blocked") }
        if !running { return L("Off") }
        if !captured { return L("Not working") }
        return L("Connected")
    }
}
