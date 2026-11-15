import Foundation
import ServiceManagement

/// Talks to the bundled bank-connector program: it owns the settings page, the
/// background service and every setting. This app is only a window onto it.
@MainActor
final class Controller: ObservableObject {
    static let shared = Controller()

    @Published private(set) var health: ConnectorHealth = .stopped
    @Published private(set) var status: ConnectorStatus?
    @Published private(set) var launchAtLogin = SMAppService.mainApp.status == .enabled
    @Published private(set) var refreshingPrices = false
    @Published var message: String?

    private var settingsProcess: Process?
    private var timer: Timer?

    private init() {
        refresh()
        timer = Timer.scheduledTimer(withTimeInterval: 10, repeats: true) { [weak self] _ in
            Task { @MainActor in self?.refresh() }
        }
    }

    /// The Go binary inside the app bundle (or next to the executable when run from `swift run`).
    var binary: URL? {
        if let url = Bundle.main.url(forResource: "bank-connector", withExtension: nil) { return url }
        let sibling = Bundle.main.bundleURL.appendingPathComponent("bank-connector")
        return FileManager.default.isExecutableFile(atPath: sibling.path) ? sibling : nil
    }

    func refresh() {
        let configExists = FileManager.default.fileExists(atPath: Paths.config.path)
        status = StatusDecoder.read(Paths.status)
        health = RatioConnector.health(configExists: configExists, status: status)
    }

    private func run(_ arguments: [String]) -> Process? {
        guard let binary else {
            message = S.t("missingBinary")
            return nil
        }
        let p = Process()
        p.executableURL = binary
        p.arguments = arguments + ["--config", Paths.config.path]
        p.standardOutput = FileHandle.nullDevice
        p.standardError = FileHandle.nullDevice
        do {
            try p.run()
            return p
        } catch {
            message = error.localizedDescription
            return nil
        }
    }

    /// Starts the local settings page; it opens the browser itself and closes after a quiet spell.
    func openSettings() {
        settingsProcess?.terminate()
        settingsProcess = run(["ui"])
    }

    func restart() {
        guard let binary else {
            message = S.t("missingBinary")
            return
        }
        let arguments = ["service", "restart", "--config", Paths.config.path]
        Task {
            // The process runs off the main actor so the menu never freezes.
            await Task.detached {
                let p = Process()
                p.executableURL = binary
                p.arguments = arguments
                p.standardOutput = FileHandle.nullDevice
                p.standardError = FileHandle.nullDevice
                try? p.run()
                p.waitUntilExit()
            }.value
            try? await Task.sleep(nanoseconds: 2_000_000_000)
            refresh()
        }
    }

    /// Forces a price download now (`prices sync`), whatever the interval says. It runs off the main actor and the
    /// menu item stays disabled until it ends, so a second click cannot start a second download.
    func refreshPrices() {
        guard !refreshingPrices else { return }
        guard let binary else {
            message = S.t("missingBinary")
            return
        }
        refreshingPrices = true
        let arguments = ["prices", "sync", "--config", Paths.config.path]
        Task {
            await Task.detached {
                let p = Process()
                p.executableURL = binary
                p.arguments = arguments
                p.standardOutput = FileHandle.nullDevice
                p.standardError = FileHandle.nullDevice
                try? p.run()
                p.waitUntilExit()
            }.value
            refreshingPrices = false
            refresh()
        }
    }

    func showLogs() {
        try? FileManager.default.createDirectory(at: Paths.logs, withIntermediateDirectories: true)
        NSWorkspaceBridge.reveal(Paths.logs)
    }

    func setLaunchAtLogin(_ on: Bool) {
        do {
            if on { try SMAppService.mainApp.register() } else { try SMAppService.mainApp.unregister() }
        } catch {
            message = error.localizedDescription
        }
        launchAtLogin = SMAppService.mainApp.status == .enabled
    }
}
