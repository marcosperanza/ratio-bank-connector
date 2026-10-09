#!/usr/bin/env sh
set -eu

SOURCE_BIN="${1:-./bank-connector}"
BIN_DIR="${HOME}/.local/bin"
CONFIG_PATH="${XDG_CONFIG_HOME:-${HOME}/.config}/Ratio/bank-connector/config.yaml"
UNIT_DIR="${XDG_CONFIG_HOME:-${HOME}/.config}/systemd/user"
UNIT_PATH="${UNIT_DIR}/ratio-bank-connector.service"

if [ ! -f "${SOURCE_BIN}" ]; then
  echo "Binary not found: ${SOURCE_BIN}" >&2
  exit 1
fi
if [ ! -f "${CONFIG_PATH}" ]; then
  echo "Configuration not found: ${CONFIG_PATH}" >&2
  echo "Run '${SOURCE_BIN} setup-addon' before installing the background agent." >&2
  exit 1
fi

mkdir -p "${BIN_DIR}" "${UNIT_DIR}"
install -m 0755 "${SOURCE_BIN}" "${BIN_DIR}/bank-connector"

cat >"${UNIT_PATH}" <<EOF
[Unit]
Description=Ratio Bank Connector
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=${BIN_DIR}/bank-connector sync --daemon --config ${CONFIG_PATH}
Restart=on-failure
RestartSec=30
NoNewPrivileges=true
PrivateTmp=true

[Install]
WantedBy=default.target
EOF

systemctl --user daemon-reload
systemctl --user enable --now ratio-bank-connector.service
echo "Ratio Bank Connector installed and started."
echo "Status: systemctl --user status ratio-bank-connector.service"
