import XCTest
@testable import RatioConnector

final class StatusTests: XCTestCase {
    // Same shape internal/status writes, including Go's fractional seconds and zero time.
    let json = """
    {"version":"1.2.3","pid":7,"startedAt":"2026-11-13T07:00:00.123456789Z","updatedAt":"2026-11-13T07:30:00Z",
     "bank":{"at":"2026-11-13T07:10:00.5Z","ok":true},
     "prices":{"at":"0001-01-01T00:00:00Z","ok":false,"tickers":0,"failed":0}}
    """

    func testDecodesWhatTheGoDaemonWrites() throws {
        let s = try StatusDecoder.decoder().decode(ConnectorStatus.self, from: Data(json.utf8))
        XCTAssertEqual(s.version, "1.2.3")
        XCTAssertTrue(s.bank.ok)
        XCTAssertEqual(s.prices.at?.timeIntervalSince1970, 0)
    }

    func testHealthFollowsFreshnessAndErrors() throws {
        var s = try StatusDecoder.decoder().decode(ConnectorStatus.self, from: Data(json.utf8))
        let fresh = s.updatedAt.addingTimeInterval(30)
        XCTAssertEqual(health(configExists: false, status: s, now: fresh, processExists: { _ in true }), .notConfigured)
        XCTAssertEqual(health(configExists: true, status: nil, now: fresh, processExists: { _ in true }), .stopped)
        XCTAssertEqual(health(configExists: true, status: s, now: s.updatedAt.addingTimeInterval(600), processExists: { _ in true }), .stopped)
        s.prices.error = nil
        XCTAssertEqual(health(configExists: true, status: s, now: fresh, processExists: { _ in true }), .running)
        s.bank = .init(at: s.bank.at, ok: false, error: "SimpleFIN subscription needs payment")
        XCTAssertEqual(health(configExists: true, status: s, now: fresh, processExists: { _ in true }), .problem("SimpleFIN subscription needs payment"))
    }

    func testCleanStopAndDeadProcessAreNotRunningEvenWhenTheFileIsFresh() throws {
        var s = try StatusDecoder.decoder().decode(ConnectorStatus.self, from: Data(json.utf8))
        let fresh = s.updatedAt.addingTimeInterval(30)
        XCTAssertEqual(health(configExists: true, status: s, now: fresh, processExists: { _ in false }), .stopped)
        s.stopped = true
        XCTAssertEqual(health(configExists: true, status: s, now: fresh, processExists: { _ in true }), .stopped)
    }

    func testProbeFindsThisProcessAndNotAnImpossibleOne() {
        XCTAssertTrue(ProcessProbe.exists(Int(getpid())))
        XCTAssertFalse(ProcessProbe.exists(999_999))
    }
}
