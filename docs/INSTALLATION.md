# Installing Ratio Bank Connector

Ratio Bank Connector is a free local add-on for Ratio Web. It runs on a computer you control, reads transactions through your personal Enable Banking restricted application, encrypts them locally, and sends them to your Ratio workspace using your personal API key.

The connector can run continuously on a Raspberry Pi or NAS, or start automatically when you log in to Linux, macOS, or Windows. When a computer has been offline, the connector checks Ratio's remote sync state and catches up only when the configured interval has elapsed.

## Quick start (recommended)

Every platform has a guided setup that checks each step live (Ratio key, Enable Banking key, price-provider keys) and installs the background service. The configuration it writes is described in [CONFIGURATION.md](CONFIGURATION.md).

| Platform | Install | Configure |
|---|---|---|
| macOS | Open `RatioConnector-<version>.dmg`, drag **Ratio Connector** to Applications, open it | The first launch opens the settings page; press **Install and start**. A menu bar icon shows the status |
| Linux / Windows | Extract the release archive | `bank-connector setup` (terminal) or `bank-connector ui` (browser page), then `bank-connector service install` |
| Docker (NAS, Raspberry Pi) | Extract the source archive, then `docker compose --profile setup run --rm setup` and `docker compose up -d --build` | Builds the image locally; the same setup command configures it again later |

The commands behind it:

```bash
bank-connector setup              # guided setup in the terminal (also: setup-addon)
bank-connector ui                 # same settings in a browser page, on 127.0.0.1 only
bank-connector service install    # per-user service that starts at login
bank-connector service status|restart|uninstall
bank-connector doctor --network   # check everything, including the providers
```

You can combine Enable Banking, SimpleFIN and CSV folders in one connector, and add price downloads. SimpleFIN and folders are set with `bank-connector setup-simplefin` and in the configuration file ([PROVIDERS.md](PROVIDERS.md)); Enable Banking on this machine is in [ENABLEBANKING_LOCAL.md](ENABLEBANKING_LOCAL.md) and prices in [PRICES.md](PRICES.md). The full file is described in [CONFIGURATION.md](CONFIGURATION.md).

The sections below describe the manual, script-based installation, which still works.

## Supported platforms

| Platform | Architectures | Background agent |
|---|---|---|
| Linux | amd64, arm64, armv7 | systemd user service |
| macOS | Intel amd64, Apple Silicon arm64 | LaunchAgent |
| Windows | amd64, arm64 | per-user Scheduled Task |
| Docker | linux/amd64, linux/arm64, linux/arm/v7 | restart policy in Docker or Compose |

The current release archives are a technical preview. They include checksums but are not yet signed with Apple Developer ID or Windows Authenticode certificates. Verify the checksum before running a downloaded binary.

## Before installation

You need:

1. A Ratio account and workspace.
2. A connector API key created from Ratio at `/dashboard/connector`. Copy the key when it is shown and store it securely.
3. A personal Enable Banking restricted application.
4. The App ID and PEM private key belonging to that application.
5. Your own accounts linked or whitelisted in the Enable Banking Control Panel.

The Enable Banking App ID and PEM remain on the connector machine. Ratio never needs the provider private key. Do not paste the PEM or either API key into logs, screenshots, or support requests.

## Download and verify a release

Download the archive for your operating system and its adjacent `.sha256` file from [GitHub Releases](https://github.com/marcosperanza/ratio-bank-connector/releases).

Choose the asset matching your machine:

| Machine | Asset |
|---|---|
| Linux PC, Intel/AMD NAS | `bank-connector-linux-amd64.tar.gz` |
| 64-bit Raspberry Pi or ARM NAS | `bank-connector-linux-arm64.tar.gz` |
| 32-bit Raspberry Pi OS | `bank-connector-linux-armv7.tar.gz` |
| Intel Mac | `bank-connector-macos-amd64.tar.gz` |
| Apple Silicon Mac | `bank-connector-macos-arm64.tar.gz` |
| Windows Intel/AMD | `bank-connector-windows-amd64.zip` |
| Windows ARM | `bank-connector-windows-arm64.zip` |

Check your architecture when needed:

```bash
uname -m
```

Common values are `x86_64` for amd64, `aarch64` or `arm64` for arm64, and `armv7l` for armv7.

On Linux, verify and extract the archive:

```bash
sha256sum -c bank-connector-linux-amd64.tar.gz.sha256
tar -xzf bank-connector-linux-amd64.tar.gz
```

On macOS:

```bash
shasum -a 256 -c bank-connector-macos-arm64.tar.gz.sha256
tar -xzf bank-connector-macos-arm64.tar.gz
```

Use the filename for your architecture in place of the examples above.

On Windows PowerShell, place the ZIP and checksum in the same directory and run:

```powershell
$Archive = "bank-connector-windows-amd64.zip"
$Expected = ((Get-Content "$Archive.sha256" -Raw) -split '\s+')[0].ToLower()
$Actual = (Get-FileHash $Archive -Algorithm SHA256).Hash.ToLower()
if ($Actual -ne $Expected) { throw "Checksum mismatch" }
Expand-Archive $Archive -DestinationPath .\ratio-bank-connector
Set-Location .\ratio-bank-connector
```

## Linux installation

The Linux installer copies the binary to `~/.local/bin` and creates a systemd user service. It does not require root privileges.

From the extracted release directory:

```bash
chmod +x bank-connector scripts/*.sh
./bank-connector version
./bank-connector setup-addon
./bank-connector doctor --network
./scripts/install-linux.sh
```

The setup wizard asks for:

- Ratio URL: accept `https://www.ratio-fin.com` unless you run your own Ratio instance;
- your personal Ratio connector API key;
- your Enable Banking App ID;
- the absolute path to your PEM private key;
- the fallback poll interval: the default is 1800 seconds (30 minutes).

The connector starts immediately and starts again at each login. Check its status and logs with:

```bash
systemctl --user status ratio-bank-connector.service
journalctl --user -u ratio-bank-connector.service -f
```

The user service runs while your user session is active. This is suitable for a workstation that should synchronize when you log in. Use Docker for an always-on Raspberry Pi or NAS installation.

## macOS installation

From the extracted release directory:

```bash
chmod +x bank-connector scripts/*.sh
./bank-connector version
./bank-connector setup-addon
./bank-connector doctor --network
./scripts/install-macos.sh
```

The installer creates a LaunchAgent for the current user. The connector starts immediately and at subsequent logins.

Until releases are notarized, macOS may block the first launch. After verifying the checksum, open **System Settings → Privacy & Security** and approve the blocked connector if macOS offers that option, then run the installer again.

Check the agent and follow its logs with:

```bash
launchctl print "gui/$UID/com.ratio.bank-connector"
tail -f "$HOME/Library/Logs/Ratio/bank-connector.log" \
  "$HOME/Library/Logs/Ratio/bank-connector.error.log"
```

## Windows installation

Extract the verified ZIP, open PowerShell in the extracted directory, and run:

```powershell
.\bank-connector.exe version
.\bank-connector.exe setup-addon
.\bank-connector.exe doctor --network
.\scripts\install-windows.ps1
```

The installer copies the executable below `%LOCALAPPDATA%\Ratio\BankConnector` and creates a Scheduled Task for the current user. It starts the connector immediately and whenever that user logs in. Administrator privileges are not required.

If Windows blocks files downloaded from the internet, verify the checksum first and then unblock only the extracted connector directory:

```powershell
Get-ChildItem -Recurse | Unblock-File
```

Check the task with:

```powershell
Get-ScheduledTask -TaskName "Ratio Bank Connector"
Get-ScheduledTaskInfo -TaskName "Ratio Bank Connector"
```

For interactive diagnostics, stop the task temporarily and run the connector in the current terminal:

```powershell
Stop-ScheduledTask -TaskName "Ratio Bank Connector"
.\bank-connector.exe doctor --network
.\bank-connector.exe sync
Start-ScheduledTask -TaskName "Ratio Bank Connector"
```

## Docker installation for NAS and Raspberry Pi

Docker is the recommended always-on installation. Create private configuration and data directories, then copy your PEM into the configuration directory:

```bash
mkdir -p config data
chmod 700 config data
cp /secure/path/enable-banking.pem config/enable-banking.pem
chmod 600 config/enable-banking.pem
```

Run the interactive setup with a writable configuration mount:

```bash
docker run --rm -it \
  -v "$PWD/config:/app/config" \
  ratio-bank-connector:local \
  setup-addon --config /app/config/config.yaml
```

When asked for the PEM path, enter `/app/config/enable-banking.pem`.

Create `compose.yaml` next to the `config` and `data` directories:

```yaml
services:
  bank-connector:
    image: ratio-bank-connector:local
    volumes:
      - ./config:/app/config:ro
      - ./data:/app/data
    restart: unless-stopped
    read_only: true
    tmpfs:
      - /tmp:size=16m
    cap_drop:
      - ALL
    security_opt:
      - no-new-privileges:true
    stop_grace_period: 30s
    logging:
      driver: json-file
      options:
        max-size: "10m"
        max-file: "3"
```

Start and inspect the connector:

```bash
docker compose up -d --build
docker compose ps
docker compose logs -f bank-connector
```

The normal container mounts configuration read-only, runs with no Linux capabilities, and uses a read-only root filesystem. Its data directory remains writable for optional local archives and future durable state.

## Link an account

After the background agent is running:

1. Open Ratio and go to `/dashboard/connector`.
2. Add a bank account.
3. Wait for the connector to publish the authorization URL. It normally polls within 30 seconds.
4. Select **Authorize** in Ratio and complete the bank consent flow.
5. Return to Ratio and confirm that the account becomes linked.
6. Wait for the first due synchronization and verify the imported transactions.

If you use a one-shot installation instead of a background agent, run `bank-connector sync` before authorization and again after returning from the bank.

## Synchronization behavior

The default transaction interval is six hours. The connector performs a lightweight Ratio check every five minutes but contacts Enable Banking only when an account is due.

Ratio stores successful sync and attempted sync timestamps. Restarting the connector, waking a laptop, or logging in repeatedly does not reset the provider interval. A computer that has been off catches up from the last successful timestamp when it next runs.

For an explicit diagnostic request, use:

```bash
bank-connector sync --force
```

Do not schedule `--force`: it intentionally bypasses the normal provider-call protection and cannot be combined with `--daemon`.

## Configuration and data locations

| Platform | Configuration |
|---|---|
| Linux | `${XDG_CONFIG_HOME:-~/.config}/Ratio/bank-connector/config.yaml` |
| macOS | `~/Library/Application Support/Ratio/bank-connector/config.yaml` |
| Windows | `%APPDATA%\Ratio\bank-connector\config.yaml` |
| Docker | `./config/config.yaml` on the host |

The setup wizard creates the configuration directory with user-only permissions where the operating system supports POSIX modes. Store the PEM in a private location readable by the same user account that runs the background agent.

Managed add-on mode defaults to `sync.archive_mode: off`, so plaintext transaction archives are not written locally. The configuration still contains sensitive provider and Ratio credentials and must be protected and backed up carefully.

## Updating

Download and verify the new release before replacing the running binary.

On Linux:

```bash
systemctl --user stop ratio-bank-connector.service
./scripts/install-linux.sh
```

On macOS:

```bash
launchctl bootout "gui/$UID/com.ratio.bank-connector" 2>/dev/null || true
./scripts/install-macos.sh
```

On Windows PowerShell:

```powershell
Stop-ScheduledTask -TaskName "Ratio Bank Connector"
.\scripts\install-windows.ps1
```

The installers replace the executable and preserve the existing configuration and data.

For Docker:

```bash
docker compose up -d --build
```

## Uninstalling

The uninstall scripts stop and remove the background agent and installed executable. They intentionally preserve configuration and local data.

Linux:

```bash
./scripts/uninstall-linux.sh
```

macOS:

```bash
./scripts/uninstall-macos.sh
```

Windows PowerShell:

```powershell
.\scripts\uninstall-windows.ps1
```

For Docker:

```bash
docker compose down
```

Delete the `config` and `data` directories separately only when you also intend to remove the API key, PEM reference, and any local archives. Revoke the Ratio API key from the Ratio connector page if the installation will no longer be used.

## Troubleshooting

Run the local checks first:

```bash
bank-connector doctor
bank-connector doctor --network
```

`doctor` validates configuration and the PEM. `doctor --network` also verifies that Ratio accepts the API key. It does not fetch bank transactions in add-on mode.

Common issues:

- **Configuration not found:** run `setup-addon` as the same operating-system user that runs the installer.
- **PEM not found:** use an absolute PEM path. In Docker, the path must be the in-container path `/app/config/enable-banking.pem`.
- **Ratio unauthorized:** create or replace the connector API key in Ratio and rerun `setup-addon`.
- **Authorization button does not appear:** confirm the agent is running and inspect its logs; it must be able to reach both Ratio and Enable Banking.
- **No immediate transaction refresh:** the six-hour gate may still be active. Use `sync --force` once only when diagnosing the installation.
- **Session expired:** reauthorize the account from Ratio. The connector will not keep calling an expired provider session.
