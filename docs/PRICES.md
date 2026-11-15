# Price downloads

The connector can download the daily closing prices of the securities you hold and deliver them to your Ratio price history, with no CSV to import. It runs on your machine with **your own provider accounts**. Ratio does not ship a price feed and never receives your provider keys.

The flow in short:

1. In Ratio, open **Investments**. Your browser seals the list of your open tickers to this connector.
2. The connector reads that list, downloads the closes from your providers, and keeps one CSV per ticker.
3. The new closes are encrypted to **your** public key and queued in Ratio.
4. Your open web app (or the iOS app) decrypts them, merges them into your encrypted price history, and clears the queue. Nothing is shown when all is well; a ticker that could not be served shows in the toolbar.

Delivering to Ratio needs a plan with investments (PRO or BIZ). Without Ratio (standalone, or a ticker file only) the connector still downloads and keeps the CSVs.

## Set it up in six steps

1. **Link the connector to Ratio** (Ratio API key in the settings page or `bank-connector setup`). Prices read your tickers through Ratio, so the connector must be in `ratio_managed` mode.
2. **Turn prices on** in the settings page (section **Prices**) or in the file (below). Yahoo needs no key; the other two need one.
3. **Show the fingerprint and register the key.** Press **Show fingerprint and register the key** in the Prices section, or run `bank-connector prices fingerprint`. The first time it creates a key pair on this machine, registers the public half with Ratio, and prints the fingerprint (groups of four hex digits).
4. **Approve it in Ratio.** Open **Investments**. It lists the connector keys that registered a sealing key, with the fingerprint your browser computed itself. Compare it with the one the connector printed; if they are equal, approve.
5. **Let the browser send the list.** With the key approved, Investments seals your tickers to the connector as soon as they change and whenever you open the page.
6. **Download.** The background service downloads at start and then every `prices.interval_hours`. To see it now: **Refresh prices now** (settings page or Mac menu bar icon) or `bank-connector prices sync`.

If the first run says `prices: 0 tickers`, the list has not arrived yet: see "Nothing arrives" below.

## Configuration

```yaml
prices:
  enabled: true
  interval_hours: 24         # how often the background service downloads
  dir: ""                    # default: <data dir>/prices
  tickers_file: ""           # optional extra tickers, one per line
  sealing_key_path: ""       # default: <data dir>/sealing-key.pem
  providers:                 # fallback order: the first that has the ticker wins
    - name: yahoo
    - name: twelvedata
      api_key: <key>
      daily_limit: 780       # calls per day this machine allows itself
      gap_seconds: 8         # minimum pause between calls
    - name: alphavantage
      api_key: <key>
      daily_limit: 25
      gap_seconds: 15
  overrides:                 # pin a spelling when a provider gets a ticker wrong
    - ticker: VWCE.DE
      provider: yahoo
      symbol: VWCE.DE
```

| Key | Meaning |
|---|---|
| `enabled` | Turns the downloader on. In the background service it then runs beside the bank sync; a failure in one never touches the other. |
| `interval_hours` | Hours between runs (default 24). The first run happens when the service starts. |
| `dir` | Where the CSVs and the downloader's state live (default `<data dir>/prices`). |
| `tickers_file` | A text file with one ticker per line, added to the list from Ratio. Required when the connector is **not** linked to Ratio. Useful for tickers you want even when no position holds them. |
| `sealing_key_path` | The connector's private key for the watchlist (default `<data dir>/sealing-key.pem`). It never leaves the machine. Back it up with the config: if you lose it you register and approve a new one. |
| `providers` | The providers in fallback order. A provider with a `daily_limit` is skipped for the rest of the day once the limit is reached. |
| `providers[].api_key` | Required for Twelve Data and Alpha Vantage. Stays in this file (mode 0600). |
| `providers[].daily_limit` | Calls per day this connector allows itself (0 means no limit). Set it under your plan's allowance. |
| `providers[].gap_seconds` | Minimum pause between two calls to that provider. |
| `overrides` | See "Ticker spellings". A list, not a map, because the settings keys would split on dots (`VWCE.DE`). |

`bank-connector doctor` checks the block, lists how many tickers it sees and warns about a ticker that a provider has no spelling for.

## The providers

| Provider | Key | Free allowance (check the current terms) | Notes |
|---|---|---|---|
| `yahoo` | none | informal, can rate-limit | Unofficial endpoint, not licensed for redistribution. A convenient default, not a guarantee. |
| `twelvedata` | needed | about 800 calls/day, 8 per minute | Spells a ticker as symbol plus exchange (`ENI` + `MIL`). |
| `alphavantage` | needed | about 25 calls/day | Little room for a large portfolio: use it as a last fallback. |

The data comes from **your** accounts and lands in **your** encrypted history. Respect each provider's terms: some free tiers limit commercial use. Ratio does not ship keys or present the data as its own.

Distinct errors move the engine along: *not found* tries the next spelling, then the next provider; *quota* or a *bad key* disables that provider for the run and goes to the next.

## Ticker spellings

Ratio writes US tickers bare (`AAPL`, `BRK.B`) and the others as `BASE.EXCHANGE` (`VWCE.DE`). The list comes from **your own positions**, so a ticker is tried **exactly as you wrote it first**: `LEMA.DE` means `LEMA.DE`. Only if that fails does the connector adapt it. The candidates, in order:

1. the override you set for this ticker and provider;
2. the spelling that worked last time (learned, kept in `state.json`);
3. the ticker as written;
4. the provider's own adaptation of the market suffix.

Suffixes Ratio understands, and the provider spellings the connector tries:

| Market | Ratio suffix | Yahoo | Alpha Vantage | Twelve Data exchange |
|---|---|---|---|---|
| US | none, `.US` | bare | bare | none |
| UK | `.L`, `.LON`, `.LSE` | `.L` | `.LON` | `LSE` |
| Germany | `.DE`, `.DEX`, `.XETR` | `.DE` | `.DEX`, `.DE` | `XETR` |
| France | `.PA`, `.PAR` | `.PA` | `.PAR` | `Euronext` |
| Italy | `.MI`, `.MIL` | `.MI` | `.MIL` | `MIL` |
| Netherlands | `.AS`, `.AMS` | `.AS` | `.AMS` | `Euronext` |
| Switzerland | `.SW`, `.SWI`, `.SIX` | `.SW` | `.SWI` | `SIX` |
| Spain | `.MC`, `.MCE` | `.MC` | (not covered) | `BME` |

A suffix not in the list is kept as part of the symbol (so `BRK.B` stays `BRK.B`). A provider with no spelling for a market is skipped, not an error.

To pin a spelling, add an override: `ticker` as written in Ratio, `provider`, and `symbol`. For Twelve Data write `SYMBOL@EXCHANGE`, for example `symbol: ENI@MIL`.

`bank-connector prices check` shows, for each ticker, the spellings each provider would try, without calling any provider.

## What the connector sends to Ratio

After each run, for every workspace in the list and every ticker:

- **Prices.** The closes newer than the last one already delivered (`state.json`, `delivered`), at most 4000 points per item, with the ISIN and the currency your browser sealed (so your devices need no symbol map).
- **Problems.** A ticker that could not be served, with a code:

| Code | Meaning | What to do |
|---|---|---|
| `NOT_FOUND` | No provider knew the ticker under any spelling | Check the ticker in Ratio; add an override. |
| `QUOTA` | The providers that could serve it ran out of calls | Add a provider or wait for the next day. |
| `AUTH` | A provider key is wrong or revoked | Fix `api_key`. |
| `UNSUPPORTED` | No configured provider covers that market | Add a provider that does. |
| `NO_ISIN` | The position has no ISIN, so the price cannot be filed | Set the ISIN on the security in Ratio. |
| `NO_CURRENCY` | The position has no currency | Set it in Ratio. |
| `ERROR` | Any other failure | Look at the log. |

Everything is encrypted to your public key (the same hybrid RSA-OAEP + AES-GCM used for transactions) before it leaves the machine; Ratio stores ciphertext and learns neither the tickers nor the prices. A date counts as delivered only after the push succeeds: an unreachable Ratio is retried by the next run, and a quiet run (nothing new, no problem) makes no call. Sending the same item twice stores it once.

The web toolbar shows an indicator only when there is a problem; the iOS app has an alert button on the Investments toolbar.

## Commands

```bash
bank-connector prices fingerprint   # create/show the sealing key fingerprint, register it with Ratio
bank-connector prices check         # which spelling each provider would try for each ticker
bank-connector prices sync          # download and deliver once, now
bank-connector prices sync --daemon # download on prices.interval_hours until stopped
bank-connector doctor [--network]   # validate the prices block (and the providers with --network)
```

In the background service you do not run `prices sync --daemon`: `sync --daemon` already runs the downloader when `prices.enabled` is true. In Docker, prefix with `docker compose run --rm cli`.

## Refreshing on demand

- **Settings page:** the **Refresh prices now** button in the Prices section runs one download and delivery and says how many tickers it tried and how many came back without data. One at a time: a second click while it runs is told so.
- **Mac menu bar icon:** **Refresh prices now** runs `prices sync` in the background; the item is disabled while it runs.
- **Terminal:** `bank-connector prices sync`.

A forced run does not update "Last price run" in the menu (only the background service writes it); the prices still reach Ratio.

## Files

All in the data directory unless you set `prices.dir`:

| File | What |
|---|---|
| `sealing-key.pem` | The connector's private sealing key (mode 0600). |
| `prices/<ticker>.csv` | One file per ticker: `Date,Open,High,Low,Close,Volume`, merged by date, written atomically. |
| `prices/state.json` | Provider usage today, spellings learned, the dates delivered per workspace and ticker. |
| `prices/watchlist.json` | The last good list from Ratio. An unreachable Ratio, or an empty answer, never empties it. |
| `status.json` | Last runs, read by the menu bar app and the settings page. |

## Security model

- The server only ever holds ciphertext: the list is sealed to a key it does not have; the prices are sealed to a key only your devices have.
- The connector's sealing key is approved **per browser**, by comparing the fingerprint. A server (or a stolen API key) that swaps in its own key gets nothing: the browser seals only to a fingerprint you approved, and a new key means a new fingerprint to approve.
- A replayed older list is ignored (sequence numbers); a moved or edited envelope fails authentication.
- Provider keys never leave `config.yaml`.
- If this machine is compromised, the sealing key and the provider keys are exposed: revoke the API key in Ratio and rotate the provider keys.

Limits: approval is per browser (a second browser asks again); the list is refreshed when you open Investments, so a position sold while Ratio stays closed keeps being downloaded until your next visit.

## Troubleshooting

| Symptom | Cause and fix |
|---|---|
| `prices: 0 tickers, 0 without data` | The list from Ratio has not arrived. Check, in order: the connector is linked to Ratio (`ratio_managed`); the key is registered (`prices fingerprint`); the key is **approved** in Investments on this browser (the fingerprints match); you opened Investments with positions after approving. `<prices dir>/watchlist.json` exists once the list arrived. |
| Investments does not list the connector key | The key did not register: run `prices fingerprint` and read its message (Ratio URL, API key, plan). |
| The fingerprint in Ratio differs from the connector's | Do **not** approve. Either the key was replaced (run `prices fingerprint` again and approve the new one) or someone is substituting it. |
| Prices are downloaded but nothing arrives in Ratio | Delivery needs a plan with investments, the Ratio API key, and an open web app or the iOS app (they decrypt and apply the queue). The CSVs in `prices/` prove the download worked. |
| A ticker shows a problem in the toolbar | See the codes above. |
| Yahoo returns nothing for everything | Rate-limited or blocked from this network; add a provider with a key, or wait. |
| `provider twelvedata needs api_key` | The provider is listed without a key: add it or remove the provider. |
| `prices.tickers_file is required` | The connector is not in `ratio_managed` mode: set a tickers file or link the connector to Ratio. |
| The Mac menu shows an old "Last price run" | Only the background service writes it. Check that the service runs the new version (`bank-connector service restart`). |
| Nothing at all after a new installation | Run `bank-connector prices check` (does it see tickers?), then `prices sync` and read its output. |
