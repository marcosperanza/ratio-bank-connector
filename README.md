# Ratio Bank Connector

Downloadable add-on for Ratio Web. It runs on the user’s computer, retrieves transactions from the user’s own open-banking application, and sends zero-knowledge encrypted envelopes to the user’s Ratio workspace. Enable Banking restricted mode is the first supported provider.

## How it works

In add-on mode, Ratio supplies the desired bank-account state and the user’s RSA public key. The add-on uses the user’s own Enable Banking App ID and private key to retrieve only accounts that user has linked in restricted mode. It encrypts each transaction locally with a random AES-256-GCM key, wraps that key with RSA-OAEP, and pushes the envelope using the user’s Ratio API key. Ratio’s server does not receive the plaintext transaction or the Enable Banking private key.

Managed mode does not persist plaintext transactions by default. The optional `sync.archive_mode` setting can create normalized CSV or CSV plus raw provider JSON for advanced local archival.

## Startup and catch-up behavior

The add-on is designed for both always-on machines and computers that run only occasionally. At startup it polls Ratio for each account’s last successful synchronization and last attempted provider call. It calls Enable Banking only when `sync.interval_hours` has elapsed; restarting the process repeatedly does not reset this gate or consume extra bank calls. While running, it checks every five minutes whether an account has become due.

With the default six-hour interval:

- a Raspberry Pi or NAS normally synchronizes up to four times per day;
- a Linux, Windows, or Mac workstation catches up after login if the previous successful sync is at least six hours old;
- a workstation started again after only one hour reports its heartbeat but skips the provider request;
- time spent powered off does not lose the cursor: the next due sync starts from Ratio’s last successful timestamp.

`bank-connector sync --force` bypasses this protection for an explicit diagnostic run. It is unavailable with `--daemon`.

## Enable Banking prerequisites

Each user configures their own restricted Enable Banking application:

1. Create an Enable Banking application.
2. Download its RSA private key and record the App ID.
3. Whitelist the personal accounts to import.
4. Create a connector API key in Ratio Web.

The add-on does not provide a shared Enable Banking application: every installation belongs to the individual who created its provider credentials and linked accounts. Enable Banking documents restricted production applications for evaluation or personal use by private individuals. Users remain responsible for respecting Enable Banking’s current terms, including the restrictions on business and commercial use. See [restricted linked accounts](https://enablebanking.com/docs/api/linked-accounts) and the [terms](https://enablebanking.com/terms/).

## Install from a release archive

For complete Linux, macOS, Windows, Docker, update, and troubleshooting instructions, see the [installation guide](docs/INSTALLATION.md). Follow the separate [Enable Banking setup guide](docs/ENABLE_BANKING_SETUP.md) to create and link your own application.

Download the archive for your platform from GitHub Releases and verify its adjacent `.sha256` file.

```bash
# Linux example
sha256sum -c bank-connector-linux-amd64.tar.gz.sha256
tar -xzf bank-connector-linux-amd64.tar.gz
./bank-connector version
./bank-connector setup-addon
./bank-connector doctor --network
./scripts/install-linux.sh
```

On macOS, run `./scripts/install-macos.sh` after setup. On Windows, open PowerShell and run:

```powershell
.\bank-connector.exe setup-addon
.\bank-connector.exe doctor --network
.\scripts\install-windows.ps1
```

The scripts install a per-user background agent:

- Linux: systemd user service;
- macOS: LaunchAgent;
- Windows: per-user Scheduled Task.

No administrator account is required. Uninstall scripts preserve configuration and local data.

## Build from source

Go 1.22 or newer is required.

```bash
go build -o bank-connector ./cmd/connector
./bank-connector setup-addon
./bank-connector sync
```

## Commands

```text
bank-connector setup-addon                Configure the local Ratio add-on
bank-connector setup-managed              Backward-compatible alias
bank-connector doctor [--network]         Validate configuration and connectivity
bank-connector sync                       Run one poll and sync cycle
bank-connector sync --force               Force one diagnostic sync regardless of its age
bank-connector sync --daemon              Run continuously
bank-connector link [--country IT]        Link an account in standalone mode
bank-connector accounts                   List Enable Banking institutions
bank-connector setup                      Configure legacy standalone mode
bank-connector version                    Show version and build metadata
```

By default the config is stored in the operating system’s user configuration directory:

- Linux: `${XDG_CONFIG_HOME:-~/.config}/Ratio/bank-connector/config.yaml`
- macOS: `~/Library/Application Support/Ratio/bank-connector/config.yaml`
- Windows: `%APPDATA%\Ratio\bank-connector\config.yaml`

An existing `./config/config.yaml` is still detected for source and Docker installations. Every command accepts `--config` to override the path.

## Configuration

The add-on setup wizard creates a configuration like this. `ratio_managed` is the internal compatibility name for this local add-on mode; it does not mean Ratio operates the connector or owns the provider credentials.

```yaml
mode: ratio_managed

enable_banking:
  app_id: "your-enable-banking-app-id"
  private_key_path: "/secure/path/private-key.pem"

sync:
  interval_hours: 6
  lookback_days: 30
  archive_mode: "off" # off | csv | csv_and_raw

ratio_managed:
  url: "https://www.ratio-fin.com"
  api_key: "your-ratio-connector-api-key"
  # Compatibility fallback while Ratio realtime is unavailable.
  poll_interval_seconds: 600
```

The private key and Ratio API key stay on the connector machine. Protect the configuration and key file with user-only permissions. Do not paste either secret into issue reports or logs.

### Realtime control and online status

The daemon connects directly to Ably using a short-lived token issued by Ratio.
Ratio never sends provider credentials through Ably: the channel only carries a
wake signal telling the add-on to fetch its canonical configuration over HTTPS.
The add-on also enters Presence so the dashboard can show whether that API key
is online.

While realtime is connected, the add-on makes one fallback Ratio request every
60 minutes. If realtime is unavailable, it automatically resumes
`poll_interval_seconds` (default 600) so the same binary remains compatible
with Ratio deployments that have not enabled Ably. Keep that value above five
minutes on a database that suspends when idle, such as Neon: every request
wakes it, and shorter intervals keep it awake around the clock. Every wake or fallback cycle fetches
the remote configuration once and reuses it for account control and due syncs.
No additional Ably secret is stored in the connector configuration.

## Docker

Docker is the recommended deployment for a Raspberry Pi or NAS. Create the configuration once with a writable mount, using a PEM file copied into `./config`:

```bash
mkdir -p config data
chmod 700 config data
cp /secure/path/enable-banking.pem config/enable-banking.pem
docker run --rm -it \
  -v "$PWD/config:/app/config" \
  ghcr.io/marcosperanza/ratio-bank-connector:latest \
  setup-addon --config /app/config/config.yaml
```

When prompted for the PEM path, enter `/app/config/enable-banking.pem`. Then start the hardened compose service:

```bash
docker compose pull
docker compose up -d
```

The configuration is mounted read-only during normal operation. The container has no Linux capabilities, uses a read-only root filesystem, restarts automatically, and exposes a local health check. The image is published for Linux amd64, arm64, and armv7.

## Development checks

```bash
go test ./...
go vet ./...
```

Provider support and setup instructions are documented in [`docs/ENABLE_BANKING_SETUP.md`](docs/ENABLE_BANKING_SETUP.md) and the [installation guide](docs/INSTALLATION.md).


## License and notices

Ratio Bank Connector source code is licensed under **AGPL-3.0-only**. See [LICENSE](LICENSE), [licensing details](LICENSING.md), [third-party notices](THIRD_PARTY_NOTICES.md), and [trademark guidance](TRADEMARKS.md). The source archive corresponding to each binary release is available from the same GitHub Release.

For Ratio's online product manual, see [ratio-fin.com/it/manual](https://www.ratio-fin.com/it/manual). Tool-specific setup and operations guides are in [`docs/`](docs/).
