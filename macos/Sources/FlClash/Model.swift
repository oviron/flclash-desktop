import AppKit

extension Notification.Name {
    static let modelChanged = Notification.Name("flclash.modelChanged")
}

func L(_ key: String) -> String { NSLocalizedString(key, comment: "") }

/// What the app knows, kept in one place: the menu and the window both read it
/// and both hear when it changes.
final class Model {
    static let shared = Model()

    private(set) var view = EngineView()
    private(set) var status = TunnelStatus()
    /// A change of the tunnel is on its way to the helper.
    private(set) var busy = false
    /// The last thing that went wrong, for the window to show until it is dismissed.
    private(set) var problem: String?

    private var timer: Timer?

    var selectedServer: Server? { view.servers.first { $0.id == view.selected } }

    func start() {
        refresh()
        // The helper does not push changes yet; a slow poll keeps the icon honest.
        let timer = Timer(timeInterval: 5, repeats: true) { [weak self] _ in self?.refreshStatus() }
        timer.tolerance = 1
        RunLoop.main.add(timer, forMode: .common)
        self.timer = timer
    }

    func refresh() {
        Engine.call(["op": "snapshot"]) { [weak self] reply in self?.take(reply) }
        refreshStatus()
    }

    func refreshStatus() {
        guard !busy else { return }
        Engine.call(["op": "status"]) { [weak self] reply in self?.take(reply, quiet: true) }
    }

    func toggle() {
        guard !busy else { return }
        busy = true
        changed()
        Engine.call(["op": status.running ? "off" : "on"]) { [weak self] reply in
            self?.busy = false
            self?.take(reply)
        }
    }

    func select(server: String = "", policy: String = "") {
        // Moving a running tunnel takes a moment; show it as work in progress.
        busy = status.running
        changed()
        Engine.call(["op": "select", "server": server, "policy": policy]) { [weak self] reply in
            self?.busy = false
            self?.take(reply)
            self?.refreshStatus()
        }
    }

    func remove(server: String) {
        Engine.call(["op": "remove", "server": server]) { [weak self] reply in self?.take(reply) }
    }

    func importServers(_ text: String, then done: @escaping (ImportResult?, String?) -> Void) {
        Engine.call(["op": "import", "text": text]) { [weak self] reply in
            self?.take(reply, quiet: true)
            done(reply.imported, reply.error)
        }
    }

    func importPolicy(name: String, text: String, then done: @escaping (String?) -> Void) {
        Engine.call(["op": "policyImport", "name": name, "text": text]) { [weak self] reply in
            self?.take(reply, quiet: true)
            done(reply.error)
        }
    }

    func dismissProblem() {
        problem = nil
        changed()
    }

    private func take(_ reply: Reply, quiet: Bool = false) {
        if let view = reply.view { self.view = view }
        if let status = reply.status { self.status = status }
        if let error = reply.error, !quiet { problem = error }
        changed()
    }

    private func changed() {
        NotificationCenter.default.post(name: .modelChanged, object: self)
    }
}

extension TunnelStatus {
    /// One line for people: what is true right now.
    var headline: String {
        if !installed { return L("Helper is not installed") }
        if !running { return L("Off") }
        if !captured { return L("Not capturing") }
        return L("Connected")
    }
}
