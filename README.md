# Ratio Bank Connector

A small program that runs on your own computer (or NAS / Raspberry Pi), reads your bank transactions through a source **you** control, encrypts them locally and sends them to your Ratio workspace. Ratio only ever receives ciphertext, and your bank credentials never leave your machine.

It can read from, at the same time:

- **Enable Banking**, with your own application (this README walks through it);
- **SimpleFIN Bridge**;
- **CSV folders** with the exports of your bank.

It can also download prices for the tickers you hold. See [docs/CONFIGURATION.md](docs/CONFIGURATION.md).

## Getting started

### 1. Install it

| Where | How |
|---|---|
| **macOS** | Open `RatioConnector-<version>.dmg`, drag **Ratio Connector** to Applications and open it. A menu bar icon shows the status; **Open settings** opens the settings page. |
| **Linux / Windows** | Extract the release archive and run `bank-connector ui` (settings page in your browser). |
| **Docker** (NAS, Raspberry Pi) | Build the image from the source archive, then see [Docker](#docker). |

All three use the same **settings page**. It only listens on `127.0.0.1` and needs the one-time token in the link it opens.

### 2. Link the connector to Ratio

1. In Ratio, open **Connector** and create an API key. Copy it when it is shown.
2. In the settings page, section **Ratio**, enter the Ratio address and paste the key into **API key**, then press **Test**.
3. Press **Install and start** (section *Background service*) so the connector runs in the background and starts at login.

The key is stored only in the connector's `config.yaml`.

### 3. Connect your bank with Enable Banking

You need an Enable Banking application of your own: create one in the [Enable Banking control panel](https://enablebanking.com/), download its private key (a `.pem` file) and note its **Application ID**. Link your accounts to the application as the control panel explains.

**Once, in the control panel:** add `https://enablebanking.com/` to the application's **redirect URLs**, written exactly like this. Ratio is not involved in this step and the connector needs no server or certificate.

Then, in the settings page, section **Bank sources → Enable Banking**:

1. Tick **Enable Banking**, fill in the **Application ID** and the path to the **PEM key**. The redirect URL already says `https://enablebanking.com/`. Press **Test**.
2. Press **Connect a bank**, pick the country and the bank from the list, and press **Authorize with the bank**.
3. The bank's login opens in a new tab. Authorize the access.
4. You land on an enablebanking.com page. Copy the `code` from the address bar (or the whole address), paste it into the settings page and press **Finish**.

The page lists the connected accounts and how many days are left. That is all on the connector side.

Finally, **in Ratio** add the account (Connector → bank accounts):

- **Provider:** `enablebanking-local`;
- **Name:** the account IBAN, the holder name shown in the settings page, or just the bank name when the bank has a single account. The connector links the Ratio account to the bank account by this name;
- choose the connector key and the workspace, and tick *Push to inbox* to receive the transactions.

Within a sync cycle the account turns *Linked* and the transactions start arriving. If the name does not match, Ratio shows an error that lists the available accounts.

### 4. Renew the consent

A bank authorization lasts at most 90 days (the connector asks for 90; a bank may allow less). The settings page shows the days left for each bank and highlights **Renew** two weeks before the end. Renewing repeats steps 2-4 above for the same bank. The old authorization keeps working until the new one is in place, and the connector relinks the account in Ratio by itself: nothing to do in Ratio. If a consent expires, the account shows as *Expired* in Ratio until you renew it.

From the terminal (a server with no screen, SSH, Docker):

```bash
bank-connector link                 # connect a bank: pick it from the list, open the printed address
                                    # on any device, paste the code
bank-connector sessions             # banks connected and days left
bank-connector sessions --renew     # renew every consent that is expiring or expired
bank-connector sessions --check     # exit status 1 when one needs renewing (for cron)
```

`bank-connector setup` also offers to connect a bank right after the Enable Banking keys. The running connector writes a warning to its log once a day from 14 days before the end of a consent, and `bank-connector doctor` reports the same.

### Other sources

- **SimpleFIN**: `bank-connector setup-simplefin` claims the setup token from bridge.simplefin.org and maps its accounts to the Ratio accounts with provider `simplefin`.
- **CSV and OFX folders**: a `folders:` entry in the configuration file per Ratio account (provider `folder`).

Both are described in [docs/PROVIDERS.md](docs/PROVIDERS.md). Enable Banking on this machine: [docs/ENABLEBANKING_LOCAL.md](docs/ENABLEBANKING_LOCAL.md). Prices: [docs/PRICES.md](docs/PRICES.md). Every option: [docs/CONFIGURATION.md](docs/CONFIGURATION.md).

## How it works

In add-on mode, Ratio supplies the desired bank-account state and your RSA public key. The connector reads the transactions from your source, encrypts each one locally with a random AES-256-GCM key, wraps that key with RSA-OAEP and pushes the envelope using your Ratio API key. Ratio's server does not receive the plaintext transactions, your Enable Banking private key or the bank session, which stays in `eb-sessions.json` in the connector's data directory (mode 0600).

Managed mode does not persist plaintext transactions by default. The optional `sync.archive_mode` setting can create normalized CSV or CSV plus raw provider JSON for advanced local archival.

## Startup and catch-up behavior

The add-on is designed for both always-on machines and computers that run only occasionally. At startup it polls Ratio for each account’s last successful synchronization and last attempted provider call. It calls Enable Banking only when `sync.interval_hours` has elapsed; restarting the process repeatedly does not reset this gate or consume extra bank calls. While running, it checks every five minutes whether an account has become due.

With the default six-hour interval:

- a Raspberry Pi or NAS normally synchronizes up to four times per day;
- a Linux, Windows, or Mac workstation catches up after login if the previous successful sync is at least six hours old;
- a workstation started again after only one hour reports its heartbeat but skips the provider request;
- time spent powered off does not lose the cursor: the next due sync starts from Ratio’s last successful timestamp.

`bank-connector sync --force` bypasses this protection for an explicit diagnostic run. It is unavailable with `--daemon`.

## Enable Banking terms

Each user configures their own Enable Banking application. The connector does not provide a shared one: every installation belongs to the person who created its provider credentials and linked accounts. Enable Banking documents restricted production applications for evaluation or personal use by private individuals. You remain responsible for respecting Enable Banking's current terms, including the restrictions on business and commercial use. See [restricted linked accounts](https://enablebanking.com/docs/api/linked-accounts) and the [terms](https://enablebanking.com/terms/).

## Install from a release archive

For complete Linux, macOS, Windows, Docker, update, and troubleshooting instructions, see the [installation guide](docs/INSTALLATION.md).

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
bank-connector doctor [--network]         Validate configuration and connectivity
bank-connector sync                       Run one poll and sync cycle
bank-connector sync --force               Force one diagnostic sync regardless of its age
bank-connector sync --daemon              Run continuously
bank-connector ui                         Settings page in the browser (127.0.0.1)
bank-connector link [--country IT]        Authorize a bank with Enable Banking from the terminal
bank-connector sessions [--renew|--check] Banks connected, days left, renewal
bank-connector service install|status|restart|uninstall   Background service
bank-connector setup                      Guided setup in the terminal
bank-connector accounts                   List Enable Banking institutions
bank-connector setup-standalone           Legacy standalone mode
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
  poll_interval_seconds: 1800
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
`poll_interval_seconds` (default 1800) so the same binary remains compatible
with Ratio deployments that have not enabled Ably. Keep that value above five
minutes on a database that suspends when idle, such as Neon: every request
wakes it, and shorter intervals keep it awake around the clock. Every wake or fallback cycle fetches
the remote configuration once and reuses it for account control and due syncs.
No additional Ably secret is stored in the connector configuration.

## Docker

Docker is the recommended deployment for a Raspberry Pi or NAS, with the same settings page as the desktop install.

The image is built locally from the source archive of the release (GitHub Releases), so run these commands from the extracted directory. By hand:

```bash
mkdir -p config data && chmod 700 config data
cp /secure/path/enable-banking.pem config/enable-banking.pem
docker compose run --rm setup       # guided setup (terminal)
docker compose up -d                # the background connector
```

### Connect a bank from the terminal (no browser on the server)

This is the way to do it on a server with no screen, and it needs nothing else:

```bash
docker compose run --rm cli link             # pick the bank, open the printed address on any device, paste the code
docker compose run --rm cli sessions         # banks connected and days left
docker compose run --rm cli sessions --renew
```

The guided `docker compose run --rm setup` also offers to connect a bank after the Enable Banking keys. Use the redirect `https://enablebanking.com/` (registered once in your Enable Banking application): after the bank login you land there, copy the `code` from the address bar and paste it in the terminal. The PEM path is the one **inside the container**: `/app/config/enable-banking.pem`.

The authorizations are saved in `./data/eb-sessions.json`, which the running connector reads on its next cycle: no restart is needed after connecting or renewing a bank. After changing other settings (Ratio key, sources, schedule) run `docker compose restart bank-connector`.

An unattended server cannot show a warning, so the connector writes one to its log once a day from 14 days before a consent ends (`docker compose logs bank-connector`). `docker compose run --rm cli sessions --check` exits with status 1 when a consent needs renewing, so it can feed a cron job or a monitoring check.

### Settings page in Docker (optional)

```bash
docker compose --profile ui up ui
```

It prints `Settings page: http://127.0.0.1:8787/#t=...`: open that link in a browser **on the same machine**. For a Raspberry Pi or NAS you reach over the network, forward the port instead of exposing it: `ssh -L 8787:127.0.0.1:8787 user@pi`, then open the link on your own computer. The page closes itself after 30 minutes without use. Connecting and renewing banks works as in [Getting started](#3-connect-your-bank-with-enable-banking), with the same two rules: PEM path inside the container, and the `enablebanking.com` redirect with the pasted code (a `localhost` redirect is not available because the container's port is not published).

The running service mounts the configuration read-only, has no Linux capabilities, uses a read-only root filesystem, restarts automatically and runs on Linux amd64, arm64 and armv7. Only the `setup` and `ui` services can write the configuration.

## Development checks

```bash
go test ./...
go vet ./...
```

The architecture and provider roadmap is documented in [`docs/ARCHITECTURE_AND_DISTRIBUTION_2026-09-14.md`](docs/ARCHITECTURE_AND_DISTRIBUTION_2026-09-14.md).
