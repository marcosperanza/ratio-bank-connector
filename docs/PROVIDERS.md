# Adding a provider

A provider is anything that can tell the connector about a user's movements: a
bank API, a folder of exports, a mailbox, a script. The connector core schedules
the work, encrypts each movement with the user's Ratio key, and delivers it to
the inbox. A provider only fetches and says what it found.

```
Ratio dashboard ── poll / status updates ──▶ core ◀── Fetch ── provider
                                              │
                          encrypt + deliver ──▶ Ratio inbox
```

## What a provider implements

| Interface | When | Methods |
|---|---|---|
| `source.Source` | always | `Name()`, `Fetch(Request) (*Result, error)` |
| `source.Linker` | the user must approve (open banking, OAuth) | `Begin`, `Complete` |
| `source.Local` | only a setting on this computer is needed | `Configured(accountID)` |

A provider is one of `Linker` or `Local`.

**The return address belongs to the provider.** `/api/connector/eb-callback` on
Ratio exists for Enable Banking only: it receives the user coming back from the
bank, stores the code and wakes the connector. Nothing in the core or in the
other providers may depend on it, and a new provider must not reuse it by
accident. `LinkRequest` carries only the base address of Ratio (`RatioURL`); a
provider that returns the user to Ratio builds its own address from it, and one
that does not need one (a folder, a script, a device-code flow) ignores it. How
the approval gets back is each `Linker`'s concern:
- through a route on Ratio for that provider (the architecture note plans
  `/api/open-banking/callback/{provider}` for this), or
- without Ratio, if the connector can complete it itself.

`Local` providers need neither. Read `internal/source/source.go` and
`internal/source/link.go`; they are short.

## The dashboard flow does not change

The user adds an account on the web, watches its state, approves it if asked, and
reviews what arrives in the inbox. Every provider drives that same flow, because
the core maps the interfaces above to the status updates the dashboard already
understands:

| Dashboard state | What happens | Provider call |
|---|---|---|
| `PENDING` | the account was just added | `Linker.Begin` → `AUTH_URL` (the dashboard shows the link), or, for `Local`, `Configured` → `SESSION_CREATED` straight away |
| `LINKING` | the user approved; Ratio holds the code | `Linker.Complete` → `SESSION_CREATED` |
| `LINKED` | movements flow | `Fetch` on the sync interval → `SYNC_COMPLETE` |
| `EXPIRED` | authorization lapsed | `Fetch` returns `source.ErrAuthExpired` → `SESSION_EXPIRED`; the user approves again |
| `ERROR` | something failed | any other error → `SYNC_ERROR` with the message |

A provider never calls Ratio, never encrypts, and never decides when to run.

## Steps

1. Copy `internal/source/providertemplate/` to `internal/source/<name>/` and
   rename the package. The template runs and passes its tests as it stands.
2. Replace the body of `Fetch`. Keep these rules:
   - `ID` is stable: the same movement has the same id every time. Use the
     provider's own identifier; derive one with `source.StableID` only if there
     is none.
   - `Amount` is signed, negative for money going out. `BookingDate` is `YYYY-MM-DD`.
   - Returning movements before `Request.From` is fine; the inbox deduplicates.
   - Put provider-specific evidence in `Metadata`; it is encrypted with the rest.
3. If the user must approve, implement `Linker` instead of `Local`.
4. Keep the `sourcetest.Run` call in the test. It checks the things the core
   depends on: valid transactions, unique and stable ids, aligned `Raw`, no error
   for a day with nothing on it.
5. Register it in `cmd/connector/main.go` with `s.RegisterSource(...)`, loading
   its settings from the configuration. Settings, credentials and paths stay on
   this computer.

## Server contract

The connector reads `provider` from each account in `GET /api/connector/poll`.
Absent or empty means `enablebanking`, so servers that do not send it keep
working. For other providers the web side needs, per account: the provider name,
and whatever non-secret fields the add-account form collects (bank name, a label).
The connector stores the reference it chose at link time in `ebAccountId`, which
is just "the provider's reference" for any provider.

## Provider configuration (folder example)

```yaml
folders:
  - account: "<the account id shown in Ratio>"
    path: "~/Banca/ING"
    format: csv          # csv | ofx | empty for both
    profile:             # CSV only; OFX needs none
      delimiter: ";"
      skip_rows: 2
      encoding: latin1
      date_layout: "02/01/2006"
      decimal: ","
      thousands: "."
      columns: { date: 0, description: 2, amount: 3 }
```

`columns` names what each zero-based column holds: `date`, `description`, and
either `amount` or `debit` and `credit`; `counterparty`, `currency` and `id` are
optional. Without an `id` column, the connector derives a stable id from the row,
counting identical rows on one day in order.

## Providers that are not Go

A provider written in another language is planned as an external program: it
prints one JSON `Transaction` per line and the core does the rest. That protocol
is not implemented yet.

## SimpleFIN (United States, Canada)

`simplefin` reads a SimpleFIN Bridge, a service that aggregates US and Canadian banks for the
user. It needs no redirect and no approval in Ratio (`Local`), and no Enable Banking
credentials.

1. The user subscribes at the bridge and creates a setup token.
2. `bank-connector simplefin-claim <setup-token>` exchanges it, once, for an access URL
   (`https://user:password@host/path`). Store it as a secret.
3. In Ratio, add an account with provider `simplefin`.
4. In the connector's config, map it. The URL is read from the named environment variable:

```yaml
simplefin:
  - account: <Ratio account id>
    access_url_env: SIMPLEFIN_ACCESS_URL
    sfin_account: ACT-1        # the account id inside the SimpleFIN connection
```

Pending movements are not delivered until they post. A `403` from the server is reported as
an expired authorization; `402` means the subscription behind the URL needs payment.

## Providers that need a licence

CBI Globe (Italian banks' PSD2 gateway) requires a registered TPP with an eIDAS certificate, and
Fabrick's PSD2 products are contracted per company. Neither is something a user can run from
their own computer, so they are not sources here. Enable Banking already reaches Italian banks
through CBI Globe; a user with a licence can write a source against the same interfaces.

### Guided setup

`bank-connector setup-simplefin` does steps 2 and 4 in one go: it exchanges the token (from
`--token`, `SIMPLEFIN_SETUP_TOKEN`, or a hidden prompt), lists your SimpleFIN accounts, and maps them to
the accounts you added in Ratio with provider `simplefin` (a connection with a single account maps by
itself). The access URL is saved in the config file, which is owner-only. Add more accounts in Ratio
later and run the command again: it reuses the saved connection and asks only for the new mapping.

It asks nothing when it has no terminal, so an installer can call it with `--token`.

### Trying SimpleFIN without a US or Canadian bank

SimpleFIN publishes a demo server with fictional accounts in dollars. Its access URL is
`https://demo:demo@beta-bridge.simplefin.org/simplefin` (accounts `Demo Checking`, `Demo Savings`).
Put it in `access_url` for a `simplefin` entry, or run the live test:

    SIMPLEFIN_LIVE_URL=https://demo:demo@beta-bridge.simplefin.org/simplefin \
      go test ./internal/source/simplefinsource -run Live -v
