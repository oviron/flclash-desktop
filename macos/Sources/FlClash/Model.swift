import AppKit

extension Notification.Name {
    static let modelChanged = Notification.Name("flclash.modelChanged")
    /// The traffic counters changed. They change every second, so they have a
    /// notification of their own: the lists need not be rebuilt for them.
    static let statsChanged = Notification.Name("flclash.statsChanged")
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

    /// What measuring a server gave; nothing for a server that was not measured.
    enum Delay: Equatable {
        case waiting
        case ms(Int)
        case failed(String)
    }

    private(set) var delays: [String: Delay] = [:]

    /// The traffic of the core, while a window watches it and something runs.
    private(set) var stats: Stats?

    private var timer: Timer?
    private var statsTimer: Timer?
    private var statsPending = false
    /// The first answers came, and what the app does once at start is done.
    private var started = false

    var selectedServer: Server? { view.servers.first { $0.id == view.selected } }

    /// The helper is there and takes this app's requests.
    var helperReady: Bool { status.installed && !status.outdated }

    func start() {
        refresh()
        // The helper does not push changes yet; a slow poll keeps the icon honest.
        let timer = Timer(timeInterval: 5, repeats: true) { [weak self] _ in self?.refreshStatus() }
        timer.tolerance = 1
        RunLoop.main.add(timer, forMode: .common)
        self.timer = timer
    }

    /// A window that shows traffic asks for the counters; without one nobody
    /// reads them.
    func watchStats(_ on: Bool) {
        statsTimer?.invalidate()
        statsTimer = nil
        guard on else {
            stats = nil
            return
        }
        let timer = Timer(timeInterval: 1, repeats: true) { [weak self] _ in self?.readStats() }
        timer.tolerance = 0.2
        RunLoop.main.add(timer, forMode: .common)
        statsTimer = timer
        readStats()
    }

    private func readStats() {
        guard status.running else {
            if stats != nil {
                stats = nil
                NotificationCenter.default.post(name: .statsChanged, object: self)
            }
            return
        }
        guard !statsPending, !busy else { return }
        statsPending = true
        Engine.stats { [weak self] reply in
            guard let self else { return }
            self.statsPending = false
            guard self.statsTimer != nil else { return }
            self.stats = reply.stats
            NotificationCenter.default.post(name: .statsChanged, object: self)
        }
    }

    /// Chooses a server and turns the tunnel on through it.
    func connect(server: String) {
        busy = true
        changed()
        Engine.call(["op": "select", "server": server, "policy": ""]) { [weak self] reply in
            guard let self else { return }
            self.take(reply)
            // A tunnel that was up has moved already; one that was off starts now.
            guard reply.error == nil, !self.status.running else {
                self.busy = false
                self.changed()
                self.refreshStatus()
                return
            }
            Engine.call(["op": "on"]) { [weak self] reply in
                self?.busy = false
                self?.take(reply)
            }
        }
    }

    func setAutoConnect(_ on: Bool) {
        Engine.call(["op": "autoConnect", "text": on ? "on" : "off"]) { [weak self] reply in self?.take(reply) }
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

    func remove(servers: [String]) {
        for id in servers {
            Engine.call(["op": "remove", "server": id]) { [weak self] reply in self?.take(reply) }
        }
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

    func remove(policy: String) {
        Engine.call(["op": "policyRemove", "name": policy]) { [weak self] reply in self?.take(reply) }
    }

    /// Measures the servers one after another: a connection and a small
    /// request each. Results arrive one by one.
    func measure(servers: [String]) {
        let fresh = servers.filter { delays[$0] != .waiting }
        guard !fresh.isEmpty else { return }
        for id in fresh { delays[id] = .waiting }
        changed()
        for id in fresh {
            Engine.measure(server: id) { [weak self] reply in
                guard let self else { return }
                if let ms = reply.delay, ms > 0 {
                    self.delays[id] = .ms(ms)
                } else {
                    self.delays[id] = .failed(reply.error ?? "")
                }
                self.changed()
            }
        }
    }

    func turnOff() {
        guard !busy else { return }
        busy = true
        changed()
        Engine.call(["op": "off"]) { [weak self] reply in
            self?.busy = false
            self?.take(reply)
        }
    }

    func setMode(_ mode: String) {
        guard mode != view.mode else { return }
        change(["op": "mode", "text": mode])
    }

    func setPort(_ port: Int) {
        guard port != view.port else { return }
        change(["op": "port", "text": String(port)])
    }

    /// A change of a setting; what runs takes it at once, which takes a moment.
    private func change(_ request: [String: String]) {
        busy = status.running
        changed()
        Engine.call(request) { [weak self] reply in
            self?.busy = false
            self?.take(reply)
            self?.refreshStatus()
        }
    }

    func setProtect(_ on: Bool) {
        // A running tunnel takes the new setting at once, which takes a moment.
        busy = status.running
        changed()
        Engine.call(["op": "protect", "text": on ? "on" : "off"]) { [weak self] reply in
            self?.busy = false
            self?.take(reply)
            self?.refreshStatus()
        }
    }

    func dismissProblem() {
        problem = nil
        changed()
    }

    private var haveView = false
    private var haveStatus = false

    private func take(_ reply: Reply, quiet: Bool = false) {
        if let view = reply.view { self.view = view; haveView = true }
        if let status = reply.status { self.status = status; haveStatus = true }
        if let error = reply.error, !quiet { problem = error }
        changed()
        if !started, haveView, haveStatus {
            started = true
            // Once, at start: connect if that is what the person set.
            if view.autoConnect, helperReady, !status.running, !status.blocked, selectedServer != nil, !busy {
                toggle()
            }
        }
    }

    private func changed() {
        NotificationCenter.default.post(name: .modelChanged, object: self)
    }
}

extension TunnelStatus {
    /// One line for people: what is true right now.
    var headline: String {
        if !installed { return L("Helper is not installed") }
        if outdated { return L("Helper needs an update") }
        if blocked { return L("Internet is blocked") }
        if !running { return L("Off") }
        if !captured { return L("Not working") }
        return L("Connected")
    }
}
