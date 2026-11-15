# Enable Banking on your own machine (`enablebanking-local`)

This guide covers the provider `enablebanking-local`: the bank consent is given **on the machine that runs the connector**, with **your own Enable Banking application**. Ratio takes no part in the consent and never sees your Enable Banking key or the bank session. For the other mode (`enablebanking`, where Ratio's page starts the consent and receives the callback) see the main [README](../README.md).

If you only want the short path, read "The whole flow in ten lines" and jump to the troubleshooting when something fails.

## The whole flow in ten lines

1. In the [Enable Banking control panel](https://enablebanking.com/): create an application, download its private key (`.pem`), note the **Application ID**, and add `https://enablebanking.com/` to its **redirect URLs**.
2. In Ratio, open **Connector** and create an API key.
3. On the connector machine: `bank-connector setup` (or the settings page: `bank-connector ui`) and fill in the Ratio address and key, the Application ID and the PEM path.
4. Connect the bank: `bank-connector link` (or **Connect a bank** in the settings page). Log in at the bank, copy the `code` from the address bar, paste it.
5. In Ratio, add a bank account with provider **`enablebanking-local`**, named after the IBAN (or the bank, if it has one account), on your connector key, with **Push to inbox** on.
6. Wait one sync cycle. The account turns *Linked* and the transactions arrive.
7. Every consent expires (the connector asks for 90 days; the bank may grant less): run `bank-connector sessions --renew` before it does. The account relinks itself.

## How it works

| | `enablebanking` (Ratio-driven) | `enablebanking-local` (this guide) |
|---|---|---|
| Who starts the consent | Ratio's page, through the connector | You, on the connector machine |
| Where the bank sends you back | A Ratio address (`/api/connector/eb-callback`) | The redirect URL in your Enable Banking application |
| Where the session is kept | Enable Banking, referenced by the account in Ratio | `eb-sessions.json` on the connector machine (mode 0600) |
| Linking the account in Ratio | Automatic after the consent | Automatic, by **matching the name** of the Ratio account with an account of a session (below) |
| Renewal | A click in Ratio | `bank-connector sessions --renew` on the machine, no click in Ratio |

Reading the transactions is the same Enable Banking call in both. Everything after that is the connector's ordinary path: each movement is encrypted locally with a random AES-256-GCM key wrapped with your RSA public key and pushed to your Ratio inbox.

The connector asks Enable Banking for a consent valid 90 days. The bank can grant less; the real end date is stored with the session and shown everywhere.

## Prerequisites

- A connector API key from Ratio (**Connector** page). The Ratio side of the connector needs a plan that includes the bank connector.
- An Enable Banking application of your own, with its private key. Enable Banking documents *restricted production* applications for personal use by private individuals; you remain responsible for respecting their terms (see the README).
- The redirect URL registered in the application **exactly as you will use it** (next section). A mismatch is the most common failure.
- The connector installed: the Mac app (`RatioConnector-<version>.dmg`), the release archive on Linux/Windows, or Docker (built from the source archive).

## Configuration

```yaml
mode: ratio_managed
ratio_managed:
  url: https://www.ratio-fin.com
  api_key: <connector API key>
enable_banking:
  app_id: <Application ID>
  private_key_path: /path/to/key.pem      # or private_key_b64 (the PEM, base64, one line)
  redirect_url: https://enablebanking.com/
  callback_cert: ""                        # optional, only for a redirect that points at this machine over https
  callback_key: ""
```

| Key | Meaning |
|---|---|
| `enable_banking.app_id` | The Application ID of your Enable Banking application. Setting it is what turns Enable Banking on. |
| `enable_banking.private_key_path` | PEM private key of the application. In Docker it is the path **inside the container**, normally `/app/config/enable-banking.pem`. |
| `enable_banking.private_key_b64` | The same key inline (base64 of the PEM, one line: `base64 -w0 key.pem`, `base64 -i key.pem` on macOS). The settings page keeps the path. |
| `enable_banking.redirect_url` | Where the bank sends you back. Must be one of the redirect URLs of the application. Empty means `https://enablebanking.com/`. |
| `enable_banking.callback_cert`, `callback_key` | Certificate and key the connector uses to serve an `https` redirect that points at this machine. Empty: a self-signed certificate. |

The file is mode 0600 and holds secrets. Both the settings page and the terminal wizard write it; `bank-connector doctor` validates it.

Environment variables (containers) work for the same settings: `RATIO_URL`, `RATIO_API_KEY`, `RATIO_ENABLE_BANKING_APP_ID`, `RATIO_ENABLE_BANKING_PRIVATE_KEY_B64`, `RATIO_ENABLE_BANKING_PRIVATE_KEY_PATH` ([DEPLOY_CONTAINER.md](DEPLOY_CONTAINER.md)). The authorized sessions are state, though: they live in the data directory (`BANK_CONNECTOR_DATA_DIR`), so a container with no volume loses them on every restart. Use a volume for `/app/data` if you use `enablebanking-local`.

## Choosing the redirect URL

You decide how the code gets back to the connector. There are three ways; pick one and register that exact URL in your Enable Banking application.

### A. `https://enablebanking.com/` (recommended, nothing to run)

Register `https://enablebanking.com/`. After the bank login you land on that page with `?code=...` in the address bar. Copy the `code` (or the whole address) and paste it into the settings page or the terminal. It needs no server and no certificate, so it works on a headless server, over SSH and in Docker.

### B. A redirect on this machine (`localhost`, `127.0.0.1`)

Set `redirect_url` to, for example, `https://localhost:8765/callback` and register it in the application. While you connect a bank, the connector listens on that address and receives the code by itself: no copy and paste. For `https` it uses `callback_cert` and `callback_key` when set (for example certificates made with `mkcert`), otherwise a self-signed certificate, and the browser warns once. Restricted production applications need an `https` redirect: this is the way to get one without a server of your own. It only works when the browser is on the same machine as the connector (not in Docker with the port unpublished, not over SSH).

### C. Any other URL

If you already have a callback of your own registered in the application, the bank sends you there; paste the address from the address bar (or just its `code`) into the terminal. If that callback consumes the code on its own, add a second redirect URL to the application for the connector.

## Connecting a bank

### From the settings page

`bank-connector ui` (on the Mac: menu bar icon → **Open settings…**). In the **Bank sources** section:

1. Tick **Enable Banking**, fill in the Application ID, the PEM path and the redirect URL, press **Test**, then **Save**.
2. Press **Connect a bank**, pick the country and the bank, press **Authorize with the bank**.
3. The bank's login opens in a new tab. Log in and authorize.
4. For redirect A or C, copy the `code` (or the whole address) from the address bar, paste it into the page and press **Finish**. For redirect B the page finishes by itself.

The page then lists each connected bank, its accounts (IBAN and the name the bank gives), and the days left. **Renew** is highlighted two weeks before the end.

### From the terminal

```bash
bank-connector link                  # list the banks of Italy, pick one, open the printed address
bank-connector link --country DE     # another country
bank-connector link ing              # only banks whose name contains "ing"
```

It prints the address to open (on any device), then asks for the code or waits for the redirect. In Docker: `docker compose run --rm cli link`. `bank-connector setup` offers the same step right after the Enable Banking keys.

The session is stored in `eb-sessions.json` in the data directory. A running connector reads it on its next cycle: **no restart** is needed after connecting or renewing a bank.

## Creating the account in Ratio

In Ratio, **Connector → bank accounts**, add an account:

- **Provider:** `enablebanking-local`.
- **Name:** how the connector finds the right bank account (below).
- The connector key, the workspace, and **Push to inbox** to receive the movements.

On its next cycle the connector matches the Ratio account with an account of a valid session, in this order:

1. The IBAN (spaces and case ignored) or the account name the bank gives, equal to the Ratio account's name (or its bank name).
2. Otherwise, the bank name equal to the Ratio account's name or bank name, **when that bank has a single account**.

The newest valid session wins. Expired sessions are ignored. If nothing matches, the account shows an error that lists what is available (for example `no authorized account matches "Casa" (available: Beta Bank IT60...)`): rename the account in Ratio after its IBAN. If there is no valid session at all, the error asks you to run `bank-connector link`.

Linked accounts are synced at the interval of `sync.interval_hours` (default 6), the first time reaching back `sync.lookback_days` (default 30). A sync that ran recently is skipped; `bank-connector sync --force` bypasses that for a diagnostic run.

## Renewing the consent

The settings page and the terminal show the days left:

```bash
bank-connector sessions              # banks connected and days left
bank-connector sessions --renew      # renew every consent that is expiring (14 days) or expired
bank-connector sessions --renew ing  # only that bank
bank-connector sessions --check      # exit status 1 when one needs renewing (for cron or monitoring)
```

Renewing repeats the consent for the same bank. The old session keeps working until the new one is in place; accounts are relinked by the connector on its next cycle, with no click in Ratio. If a consent expires first, the account shows as *Expired* (or *Error*) in Ratio: renew it on the machine and the connector picks the new session up by itself.

A running connector writes a warning to its log once a day from 14 days before the end of a consent and after it ended, and `bank-connector doctor` reports the same. On an unattended server that log is where it is said: `docker compose logs bank-connector`. For a cron check: `docker compose run --rm cli sessions --check`.

To forget a bank, use **Remove** in the settings page. This only stops the connector using the session; the bank keeps it until it expires.

## Docker and servers without a screen

```bash
docker compose run --rm setup                # builds the image on first use, guided setup
docker compose up -d --build                 # builds the image and starts the connector
docker compose run --rm setup                # the guided setup again
docker compose run --rm cli link             # connect a bank
docker compose run --rm cli sessions --renew
docker compose --profile ui up ui            # optional settings page on 127.0.0.1:8787
```

- Use redirect **A**: the container's port is not published, so redirect B cannot receive the code.
- The PEM path is the one inside the container: `/app/config/enable-banking.pem`.
- `./data` must be mounted (the compose file does it): the sessions are there.
- For the settings page on a Raspberry Pi or NAS, forward the port instead of exposing it: `ssh -L 8787:127.0.0.1:8787 user@pi`, then open the printed link on your computer.

## Files and privacy

| What | Where | Notes |
|---|---|---|
| Configuration | `config.yaml` | Mode 0600. Holds the Ratio API key. |
| Application key | `private_key_path` or `private_key_b64` | Never leaves the machine. |
| Bank sessions | `<data dir>/eb-sessions.json` | Mode 0600. Session id, bank, accounts (uid, IBAN, name), validity. |
| Status | `<data dir>/status.json` | Read by the menu bar app and the settings page. |

Ratio receives only the encrypted movements and the account status (linked, expired, error, last sync). It does not receive the Enable Banking key, the code, or the session. The data directory is `~/Library/Application Support/Ratio/bank-connector/data` on macOS, `~/.config/Ratio/bank-connector/data` on Linux, `%APPDATA%\Ratio\bank-connector\data` on Windows, `./data` in Docker, or `BANK_CONNECTOR_DATA_DIR`.

## Troubleshooting

| Symptom | Cause and fix |
|---|---|
| The bank says the redirect URL is not allowed, or Enable Banking refuses the authorization | `redirect_url` is not **exactly** one of the redirect URLs of the application (scheme, host, path, trailing slash). Fix it in the control panel or in the connector. |
| `no authorized Enable Banking session on this connector` | No valid session: run `bank-connector link`. Check `bank-connector sessions`. |
| `no authorized account matches "..." (available: ...)` | The Ratio account name matches neither an IBAN, an account name, nor a single-account bank. Rename the account in Ratio after its IBAN. |
| The account stays *Pending* or *Error* in Ratio | The connector only matches during a cycle; wait one or restart the service. Read the log: the reason is there. |
| Pasting the code fails: `state does not match` | The code belongs to another attempt. Start **Connect a bank** again and paste the code of that attempt. |
| The code expired | Codes live minutes. Start again and paste at once. |
| The browser warns about the certificate (redirect B) | The self-signed certificate. Accept it once, or give `callback_cert` and `callback_key` (for example from `mkcert`). |
| Works until the connector restarts (Docker) | `/app/data` is not a volume, so `eb-sessions.json` is lost. Mount `./data`. |
| `Enable Banking is not configured on this connector` | `enable_banking.app_id` is empty or the container has no Enable Banking variables. |
| `private key` error in `doctor` | The PEM is wrong, unreadable, or `private_key_b64` is not the base64 of the PEM on one line. |
| Consent ended early | The bank granted less than 90 days. The settings page shows the real end date; renew then. |
| HTTP 4xx from Enable Banking in a local screen | The page shows the provider's explanation. For the terminal: `RATIO_CONNECTOR_DEBUG_PROVIDER_ERRORS=1` prints the response body locally (never in logs or in Ratio). |

`bank-connector doctor --network` checks the configuration, the key, the redirect URL to register, the consents kept here and the connectivity to Enable Banking and Ratio.
