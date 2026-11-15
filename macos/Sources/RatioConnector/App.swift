import AppKit
import SwiftUI

enum NSWorkspaceBridge {
    static func reveal(_ url: URL) { NSWorkspace.shared.open(url) }
}

/// The app has no window and no Dock icon, so opening it must do something visible:
/// a manual launch and a double-click while it runs both show the settings page.
final class AppDelegate: NSObject, NSApplicationDelegate {
    func applicationDidFinishLaunching(_ notification: Notification) {
        // A newer copy replaces older ones (the dmg volume and /Applications can both run).
        let me = NSRunningApplication.current
        if let id = me.bundleIdentifier {
            for other in NSRunningApplication.runningApplications(withBundleIdentifier: id) where other != me {
                other.terminate()
            }
        }
        if !Self.launchedAsLoginItem {
            MainActor.assumeIsolated { Controller.shared.openSettings() }
        }
    }

    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        MainActor.assumeIsolated { Controller.shared.openSettings() }
        return true
    }

    private static var launchedAsLoginItem: Bool {
        NSAppleEventManager.shared().currentAppleEvent?
            .paramDescriptor(forKeyword: AEKeyword(keyAEPropData))?.enumCodeValue == OSType(keyAELaunchedAsLogInItem)
    }
}

@main
struct RatioConnectorApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    @StateObject private var controller = Controller.shared

    var body: some Scene {
        MenuBarExtra {
            MenuContent(controller: controller)
        } label: {
            menuBarImage(controller.health)
        }
    }

    /// The brand mark as a template image; a dot marks states that need attention
    /// and a stopped connector is dimmed. Falls back to SF Symbols outside the app bundle.
    @ViewBuilder
    private func menuBarImage(_ h: ConnectorHealth) -> some View {
        if let img = Self.menuBarIcon(alert: needsAttention(h)) {
            Image(nsImage: img).opacity(h == .stopped ? 0.45 : 1)
        } else {
            Image(systemName: symbol(h))
        }
    }

    private func needsAttention(_ h: ConnectorHealth) -> Bool {
        switch h {
        case .problem, .notConfigured: return true
        case .running, .stopped: return false
        }
    }

    private static func menuBarIcon(alert: Bool) -> NSImage? {
        let name = alert ? "MenuBarIconAlert" : "MenuBarIcon"
        guard let url = Bundle.main.url(forResource: name, withExtension: "png"),
              let img = NSImage(contentsOf: url) else { return nil }
        img.size = NSSize(width: 18, height: 18)
        img.isTemplate = true
        return img
    }

    private func symbol(_ h: ConnectorHealth) -> String {
        switch h {
        case .running: return "checkmark.circle"
        case .problem: return "exclamationmark.triangle"
        case .notConfigured: return "gearshape"
        case .stopped: return "pause.circle"
        }
    }
}

struct MenuContent: View {
    @ObservedObject var controller: Controller

    var body: some View {
        Text(title).font(.headline)
        if case .problem(let detail) = controller.health { Text(detail) }
        if controller.health == .notConfigured { Text(S.t("setupFirst")) }
        if let s = controller.status, controller.health != .notConfigured {
            Text("\(S.t("lastBank")): \(when(s.bank.at))")
            Text("\(S.t("lastPrices")): \(when(s.prices.at))")
        }
        if let m = controller.message { Text(m) }
        Divider()
        Button(S.t("settings")) { controller.openSettings() }.keyboardShortcut(",")
        Button(S.t(controller.refreshingPrices ? "refreshingPrices" : "refreshPrices")) { controller.refreshPrices() }
            .disabled(controller.health == .notConfigured || controller.refreshingPrices)
        Button(S.t("restart")) { controller.restart() }.disabled(controller.health == .notConfigured)
        Button(S.t("logs")) { controller.showLogs() }
        Divider()
        Toggle(S.t("login"), isOn: Binding(get: { controller.launchAtLogin }, set: { controller.setLaunchAtLogin($0) }))
        Divider()
        Button(S.t("quit")) { NSApplication.shared.terminate(nil) }.keyboardShortcut("q")
    }

    private var title: String {
        switch controller.health {
        case .running: return S.t("running")
        case .stopped: return S.t("stopped")
        case .notConfigured: return S.t("notConfigured")
        case .problem: return S.t("problem")
        }
    }

    private func when(_ date: Date?) -> String {
        guard let date, date.timeIntervalSince1970 > 0 else { return S.t("never") }
        let f = RelativeDateTimeFormatter()
        f.unitsStyle = .short
        return f.localizedString(for: date, relativeTo: Date())
    }
}
