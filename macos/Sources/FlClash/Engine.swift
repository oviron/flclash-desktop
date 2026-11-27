import Foundation

struct Server: Decodable, Equatable {
    let id: String
    let name: String
    let group: String
    let proto: String
    /// Where the server lets traffic out, a two-letter code, once it was asked.
    var country: String? = nil

    enum CodingKeys: String, CodingKey {
        case id, name, group, country
        case proto = "protocol"
    }

    /// The flag of that country, made of the two regional letters.
    var flag: String? {
        guard let code = country, code.count == 2 else { return nil }
        return String(String.UnicodeScalarView(code.uppercased().unicodeScalars.compactMap { UnicodeScalar(127397 + $0.value) }))
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
    /// "proxy", "system" or "tun".
    var mode = "proxy"
    var port = 7890
    var autoConnect = false
    var subscriptions: [Subscription] = []
    /// The local port is open to the local network; `auth` is the
    /// user:password asked of other devices, empty for none.
    var lan = false
    var auth = ""
    /// The resolver used under the tunnel.
    var dns = ""
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
    var agent = ""
    var every = 0
    var skip = ""
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

/// Where the running server lets traffic out: the address sites see.
struct Exit: Decodable, Equatable {
    let ip: String
    var country: String? = nil

    /// The country for people: its flag and its name in their language.
    var place: String? {
        guard let code = country, code.count == 2 else { return nil }
        let flag = String(String.UnicodeScalarView(code.uppercased().unicodeScalars.compactMap { UnicodeScalar(127397 + $0.value) }))
        let name = Locale.current.localizedString(forRegionCode: code) ?? code
        return flag + " " + name
    }
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
    /// The running port is open to the local network too.
    var lan = false

    enum CodingKeys: String, CodingKey {
        case installed, outdated, running, captured, server, policy, since, guardProblem, blocked, mode, port, lan
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
    var document: PolicyDocument? = nil
    var log: String? = nil
    var exit: Exit? = nil
    var link: String? = nil
}
