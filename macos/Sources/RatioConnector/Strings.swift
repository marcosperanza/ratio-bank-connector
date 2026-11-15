import Foundation

/// Italian and English; the rest of the app is symbols.
enum S {
    private static let italian = Locale.preferredLanguages.first?.hasPrefix("it") == true

    static func t(_ key: String) -> String {
        (italian ? it[key] : en[key]) ?? en[key] ?? key
    }

    private static let en: [String: String] = [
        "running": "Running", "stopped": "Not running", "notConfigured": "Not set up yet", "problem": "Needs attention",
        "settings": "Open settings…", "restart": "Restart connector", "refreshPrices": "Refresh prices now", "refreshingPrices": "Refreshing prices…", "logs": "Show logs", "login": "Open at login",
        "quit": "Quit menu (connector keeps running)", "lastBank": "Last bank sync", "lastPrices": "Last price run",
        "never": "never", "setupFirst": "Open settings to finish the setup.",
        "missingBinary": "The connector program is missing from this app.",
    ]
    private static let it: [String: String] = [
        "running": "In esecuzione", "stopped": "Non in esecuzione", "notConfigured": "Non ancora configurato", "problem": "Richiede attenzione",
        "settings": "Apri le impostazioni…", "restart": "Riavvia il connector", "refreshPrices": "Aggiorna i prezzi ora", "refreshingPrices": "Aggiorno i prezzi…", "logs": "Mostra i log", "login": "Apri all’avvio",
        "quit": "Chiudi il menu (il connector resta attivo)", "lastBank": "Ultima sync banche", "lastPrices": "Ultimo scarico prezzi",
        "never": "mai", "setupFirst": "Apri le impostazioni per completare la configurazione.",
        "missingBinary": "Il programma del connector manca da questa app.",
    ]
}
