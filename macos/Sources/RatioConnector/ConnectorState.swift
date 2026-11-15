import Foundation

/// What the daemon publishes in status.json (see internal/status in the Go code).
struct ConnectorStatus: Decodable, Equatable {
    struct Cycle: Decodable, Equatable {
        var at: Date?
        var ok: Bool
        var error: String?
    }
    struct Prices: Decodable, Equatable {
        var at: Date?
        var ok: Bool
        var error: String?
        var tickers: Int
        var failed: Int
    }
    var version: String
    var pid: Int?
    var stopped: Bool?
    var updatedAt: Date
    var bank: Cycle
    var prices: Prices

    /// The daemon rewrites the file every 30 s and marks it `stopped` on a clean exit; the
    /// recorded process must also exist, so a killed daemon does not look alive for two minutes.
    func isAlive(now: Date = Date(), processExists: (Int) -> Bool = ProcessProbe.exists) -> Bool {
        if stopped == true { return false }
        guard now.timeIntervalSince(updatedAt) < 120 else { return false }
        guard let pid else { return true }
        return processExists(pid)
    }
}

enum ProcessProbe {
    /// kill(pid, 0) sends nothing; EPERM means the process exists but belongs to someone else.
    static func exists(_ pid: Int) -> Bool {
        guard pid > 0 else { return false }
        return kill(pid_t(pid), 0) == 0 || errno == EPERM
    }
}

enum ConnectorHealth: Equatable {
    case notConfigured
    case stopped
    case running
    case problem(String)
}

enum StatusDecoder {
    /// Go writes RFC 3339 with fractional seconds, and a zero time for "never".
    static func decoder() -> JSONDecoder {
        let d = JSONDecoder()
        d.dateDecodingStrategy = .custom { decoder in
            let s = try decoder.singleValueContainer().decode(String.self)
            if s.hasPrefix("0001") { return Date(timeIntervalSince1970: 0) }
            let f = ISO8601DateFormatter()
            f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
            if let date = f.date(from: s) { return date }
            f.formatOptions = [.withInternetDateTime]
            if let date = f.date(from: s) { return date }
            throw DecodingError.dataCorrupted(.init(codingPath: decoder.codingPath, debugDescription: "bad date \(s)"))
        }
        return d
    }

    static func read(_ url: URL) -> ConnectorStatus? {
        guard let data = try? Data(contentsOf: url) else { return nil }
        return try? decoder().decode(ConnectorStatus.self, from: data)
    }
}

func health(configExists: Bool, status: ConnectorStatus?, now: Date = Date(),
            processExists: (Int) -> Bool = ProcessProbe.exists) -> ConnectorHealth {
    guard configExists else { return .notConfigured }
    guard let status, status.isAlive(now: now, processExists: processExists) else { return .stopped }
    if let e = status.bank.error, !status.bank.ok, !e.isEmpty { return .problem(e) }
    if let e = status.prices.error, !status.prices.ok, !e.isEmpty { return .problem(e) }
    return .running
}

/// Where the Go connector keeps its files on macOS (config.DefaultConfigPath / DefaultDataDir).
enum Paths {
    static var support: URL {
        FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
            .appendingPathComponent("Ratio/bank-connector", isDirectory: true)
    }
    static var config: URL { support.appendingPathComponent("config.yaml") }
    static var status: URL { support.appendingPathComponent("data/status.json") }
    static var logs: URL {
        FileManager.default.urls(for: .libraryDirectory, in: .userDomainMask)[0]
            .appendingPathComponent("Logs/Ratio", isDirectory: true)
    }
}
