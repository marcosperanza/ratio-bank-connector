#!/usr/bin/env sh
set -eu

SOURCE_BIN="${1:-./bank-connector}"
APP_DIR="${HOME}/Library/Application Support/Ratio/BankConnector"
BIN_PATH="${APP_DIR}/bank-connector"
CONFIG_PATH="${HOME}/Library/Application Support/Ratio/bank-connector/config.yaml"
LOG_DIR="${HOME}/Library/Logs/Ratio"
PLIST_PATH="${HOME}/Library/LaunchAgents/com.ratio.bank-connector.plist"

if [ ! -f "${SOURCE_BIN}" ]; then
  echo "Binary not found: ${SOURCE_BIN}" >&2
  exit 1
fi
if [ ! -f "${CONFIG_PATH}" ]; then
  echo "Configuration not found: ${CONFIG_PATH}" >&2
  echo "Run '${SOURCE_BIN} setup-addon' before installing the background agent." >&2
  exit 1
fi

mkdir -p "${APP_DIR}" "${LOG_DIR}" "${HOME}/Library/LaunchAgents"
install -m 0755 "${SOURCE_BIN}" "${BIN_PATH}"

cat >"${PLIST_PATH}" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.ratio.bank-connector</string>
  <key>ProgramArguments</key>
  <array>
    <string>${BIN_PATH}</string>
    <string>sync</string>
    <string>--daemon</string>
    <string>--config</string>
    <string>${CONFIG_PATH}</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>
  <key>StandardOutPath</key><string>${LOG_DIR}/bank-connector.log</string>
  <key>StandardErrorPath</key><string>${LOG_DIR}/bank-connector.error.log</string>
</dict>
</plist>
EOF

launchctl bootout "gui/${UID}/com.ratio.bank-connector" 2>/dev/null || true
launchctl bootstrap "gui/${UID}" "${PLIST_PATH}"
echo "Ratio Bank Connector installed and started."
