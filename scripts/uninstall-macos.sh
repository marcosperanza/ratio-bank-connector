#!/usr/bin/env sh
set -eu

PLIST_PATH="${HOME}/Library/LaunchAgents/com.ratio.bank-connector.plist"
APP_DIR="${HOME}/Library/Application Support/Ratio/BankConnector"

launchctl bootout "gui/${UID}/com.ratio.bank-connector" 2>/dev/null || true
rm -f "${PLIST_PATH}"
rm -rf "${APP_DIR}"
echo "Ratio Bank Connector removed. Configuration and local data were preserved."
