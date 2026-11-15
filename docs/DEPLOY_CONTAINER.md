# Running the connector as a container

The connector can run on any host that runs a Docker image, with no disk and no
config file. Everything it needs comes from environment variables, and what it
must remember between runs (the last successful sync, which accounts are linked)
is kept by Ratio. Restarting, redeploying or moving it loses nothing.

## Variables

| Variable | Required | Meaning |
|---|---|---|
| `RATIO_API_KEY` | yes | Connector API key created in Ratio. Its presence selects environment-only configuration when no config file exists. |
| `RATIO_URL` | yes | Your Ratio address, `https://…` |
| `RATIO_ENABLE_BANKING_APP_ID` | yes | Your own Enable Banking application id |
| `RATIO_ENABLE_BANKING_PRIVATE_KEY_B64` | yes | The application's private key, PEM, base64-encoded in one line: `base64 -w0 key.pem` (`base64 -i key.pem` on macOS) |
| `RATIO_SYNC_INTERVAL_HOURS` | no | Hours between provider calls, default 6 |
| `RATIO_SYNC_LOOKBACK_DAYS` | no | Days fetched by the first sync, default 30 |
| `RATIO_POLL_INTERVAL_SECONDS` | no | Control poll while realtime is down, default 1800 |
| `RATIO_ARCHIVE_MODE` | no | `off` (default here), `csv`, `csv_and_raw`. A container has no disk worth archiving to; leave it off. |

Environment variables take precedence over a config file when both exist.

## Run

Build the image from the source archive of the release you want (GitHub Releases), then run it:

```bash
docker build -t ratio-bank-connector:local .
docker run -d --restart unless-stopped --name ratio-connector \
  -e RATIO_API_KEY=… -e RATIO_URL=https://… \
  -e RATIO_ENABLE_BANKING_APP_ID=… \
  -e RATIO_ENABLE_BANKING_PRIVATE_KEY_B64=… \
  ratio-bank-connector:local
```

Check it before leaving it: `docker run --rm … ratio-bank-connector:local doctor --network`.

## What stays on you

The container holds your bank credentials and sees your movements in clear before
encrypting them with your Ratio key. It runs in your account on your host; Ratio
never receives either. Treat the variables as secrets in your host's dashboard.
