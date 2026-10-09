#!/usr/bin/env sh
set -eu

BIN_PATH="${HOME}/.local/bin/bank-connector"
UNIT_PATH="${XDG_CONFIG_HOME:-${HOME}/.config}/systemd/user/ratio-bank-connector.service"

systemctl --user disable --now ratio-bank-connector.service 2>/dev/null || true
rm -f "${UNIT_PATH}" "${BIN_PATH}"
systemctl --user daemon-reload
echo "Ratio Bank Connector removed. Configuration and local data were preserved."
