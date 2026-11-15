# Configuration reference

`bank-connector setup` (terminal), `bank-connector ui` (browser page) and the menu bar app all edit this one file. Location: macOS `~/Library/Application Support/Ratio/bank-connector/config.yaml`, Linux `~/.config/Ratio/bank-connector/config.yaml`, Windows `%APPDATA%\Ratio\bank-connector\config.yaml`, Docker `./config/config.yaml`. The file is mode 0600 and holds secrets; never share it.

```yaml
mode: ratio_managed
ratio_managed:
  url: https://www.ratio-fin.com
  api_key: <connector API key from Ratio>
  poll_interval_seconds: 1800
sync:
  interval_hours: 6        # how often each bank account is read
  lookback_days: 30        # how far the first sync reaches back

# Bank sources: any combination, active at the same time.
enable_banking:            # optional
  app_id: <application id>
  private_key_path: /path/to/key.pem
# Set in the file (or with `bank-connector setup-simplefin`), not on the settings page; see PROVIDERS.md.
simplefin:                 # zero or more
  - account: <Ratio account id>
    access_url_env: SIMPLEFIN_ACCESS_URL
    sfin_account: ACT-1
folders:                   # zero or more
  - account: <Ratio account id>
    path: /Users/me/Banca/casa
    format: csv            # csv | ofx | empty for both
    profile:               # CSV only
      delimiter: ";"
      date_layout: "02/01/2006"
      decimal: ","
      columns: { date: 0, description: 2, amount: 3 }

prices:
  enabled: true
  interval_hours: 24
  tickers_file: ""         # optional extra tickers, one per line
  providers:               # fallback order
    - name: yahoo
    - name: twelvedata
      api_key: <key>
      daily_limit: 780
      gap_seconds: 8
    - name: alphavantage
      api_key: <key>
      daily_limit: 25
      gap_seconds: 15
  overrides:               # pin a spelling when a provider gets a ticker wrong
    - ticker: VWCE.DE
      provider: yahoo
      symbol: VWCE.DE
```

## How a bank account picks its source

Each bank account in Ratio has a provider. The connector reads it and routes the account:

| Provider in Ratio | How it links | What the connector needs |
|---|---|---|
| `enablebanking` (default) | Browser consent through Ratio | the `enable_banking` block |
| `enablebanking-local` | Consent on this machine, then automatic by IBAN or bank name | the `enable_banking` block with `redirect_url`; run `bank-connector link` |
| `simplefin` | Automatic, by Ratio account id | a `simplefin:` entry with that account id ([PROVIDERS.md](PROVIDERS.md)) |
| `folder` | Automatic, by Ratio account id | a `folders:` entry with that account id ([PROVIDERS.md](PROVIDERS.md)) |

Enable Banking can be left out entirely if you only use SimpleFIN or folders.

### Enable Banking on this machine (`enablebanking-local`)

Ratio takes no part in the bank consent: you give it on the connector's machine, with your own Enable Banking application, and create the account in Ratio with provider `enablebanking-local`. The `enable_banking` block above (`app_id`, the key, `redirect_url`, optionally `callback_cert` and `callback_key`) is all it needs. The full guide, with every step and the troubleshooting, is [ENABLEBANKING_LOCAL.md](ENABLEBANKING_LOCAL.md).

## Prices

The `prices:` block above turns on the downloader; the guide with the providers, the ticker spellings, the sealing key approval, the delivery to Ratio and the troubleshooting is [PRICES.md](PRICES.md). In short: tickers come from your open positions in Ratio (sealed to this connector), the first time you compare the key fingerprint with the one in Ratio → Investments and approve it, and the closes arrive in your price history by themselves. To download now instead of waiting for `interval_hours`: the **Refresh prices now** button in the Prices section of the settings page, the same item in the Mac menu bar icon, or `bank-connector prices sync`.

Provider keys stay in this file and are never sent to Ratio.

## Environment

`BANK_CONNECTOR_DATA_DIR` moves all state (price CSVs, sealing key, status file, Enable Banking sessions) to another directory; the compose file sets it to `/app/data`. A container with no volume is configured from environment variables alone: [DEPLOY_CONTAINER.md](DEPLOY_CONTAINER.md).

The settings page and the wizard write only the sections they own (`mode`, `ratio_managed`, `sync`, `enable_banking`, `prices`) and keep the rest of the file (`folders`, `simplefin`, `accounts`, anything you added) as it is.
