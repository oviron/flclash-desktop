import CFlClash
import Foundation

struct Server: Decodable, Equatable {
    let id: String
    let name: String
    let group: String
    let proto: String

    enum CodingKeys: String, CodingKey {
        case id, name, group
        case proto = "protocol"
    }
}

struct Policy: Decodable, Equatable {
    let name: String
    let builtin: Bool
    let rules: Int
}

struct EngineView: Decodable, Equatable {
    var servers: [Server] = []
    var selected = ""
    var policies: [Policy] = []
    var policy = ""
    var protect = true
    /// "tun", "system" or "proxy".
    var mode = "tun"
    var port = 7890
    var autoConnect = false
    var subscriptions: [Subscription] = []
}

/// A group of servers that is read from an address. The address itself stays
/// in the engine: it is a secret.
struct Subscription: Decodable, Equatable {
    let name: String
    let host: String
    let updated: Int
    let used: Int
    let total: Int
    let expire: Int
    let problem: String
}

struct Connection: Decodable, Equatable {
    let id: String
    let host: String
    let port: Int
    let network: String
    let via: String
    let rule: String
    let upload: Int
    let download: Int
    let start: Int
}

/// The traffic of the core at one moment.
struct Stats: Decodable, Equatable {
    var up = 0
    var down = 0
    var upTotal = 0
    var downTotal = 0
    var connections: [Connection] = []
    var open = 0
}

struct TunnelStatus: Decodable, Equatable {
    var installed = true
    var outdated = false
    var running = false
    var captured = false
    var server = ""
    var policy = ""
    var since: Int = 0
    /// "full": nothing leaves past the tunnel; "ipv6": only IPv6 is closed.
    var guard_ = ""
    var guardProblem = ""
    /// The tunnel is gone and the guard keeps the Mac offline.
    var blocked = false
    /// What runs: "tun", "system" or "proxy", and the local port (0: none).
    var mode = ""
    var port = 0

    enum CodingKeys: String, CodingKey {
        case installed, outdated, running, captured, server, policy, since, guardProblem, blocked, mode, port
        case guard_ = "guard"
    }
}

struct ImportResult: Decodable {
    struct Failure: Decodable {
        let entry: Int
        let reason: String
    }
    let imported: [Server]
    let failed: [Failure]
}

struct Reply: Decodable {
    let error: String?
    let view: EngineView?
    let status: TunnelStatus?
    let imported: ImportResult?
    var delay: Int? = nil
    var stats: Stats? = nil
}

/// The Go engine. Calls can take as long as a tunnel takes to come up, so they
/// run off the main thread, one at a time and in the order they were made.
enum Engine {
    private static let queue = DispatchQueue(label: "flclash.engine", qos: .userInitiated)

    static func call(_ request: [String: String], then done: @escaping (Reply) -> Void) {
        queue.async {
            let reply = callSync(request)
            DispatchQueue.main.async { done(reply) }
        }
    }

    /// Measurements take seconds each and wait for one another, but must not
    /// hold up turning the tunnel on or off: they have a line of their own.
    private static let measuring = DispatchQueue(label: "flclash.engine.delay", qos: .utility)

    static func measure(server: String, then done: @escaping (Reply) -> Void) {
        measuring.async {
            let reply = callSync(["op": "delay", "server": server])
            DispatchQueue.main.async { done(reply) }
        }
    }

    /// Reading a subscription waits for a provider; it has a line of its own too.
    private static let fetching = DispatchQueue(label: "flclash.engine.subscriptions", qos: .utility)

    static func subscription(_ request: [String: String], then done: @escaping (Reply) -> Void) {
        fetching.async {
            let reply = callSync(request)
            DispatchQueue.main.async { done(reply) }
        }
    }

    /// The counters are read every second while a window shows them; they
    /// must not wait behind a tunnel that is coming up, nor hold it up.
    private static let counting = DispatchQueue(label: "flclash.engine.stats", qos: .utility)

    static func stats(then done: @escaping (Reply) -> Void) {
        counting.async {
            let reply = callSync(["op": "stats"])
            DispatchQueue.main.async { done(reply) }
        }
    }

    private static func callSync(_ request: [String: String]) -> Reply {
        guard let data = try? JSONSerialization.data(withJSONObject: request),
              let text = String(data: data, encoding: .utf8)
        else { return Reply(error: "bad request", view: nil, status: nil, imported: nil) }
        let raw: UnsafeMutablePointer<CChar>? = text.withCString { FlcCall(UnsafeMutablePointer(mutating: $0)) }
        guard let raw else { return Reply(error: "no reply", view: nil, status: nil, imported: nil) }
        defer { FlcFree(raw) }
        let reply = try? JSONDecoder().decode(Reply.self, from: Data(String(cString: raw).utf8))
        return reply ?? Reply(error: "unreadable reply", view: nil, status: nil, imported: nil)
    }
}
