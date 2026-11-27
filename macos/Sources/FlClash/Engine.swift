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

struct EngineView: Decodable, Equatable {
    var servers: [Server] = []
    var selected = ""
    var policies: [String] = []
    var policy = ""
}

struct TunnelStatus: Decodable, Equatable {
    var installed = true
    var outdated = false
    var running = false
    var captured = false
    var server = ""
    var policy = ""
    var since: Int = 0
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
