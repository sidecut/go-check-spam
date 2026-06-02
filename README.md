# go-check-spam

Small CLI to count messages in the Gmail Spam label by local date (based on `internalDate`).

> Originally written in Go, this project is now implemented in Rust.

## Prerequisites

- Rust 1.96+ (install via [rustup](https://rustup.rs/)).
- A Google Cloud OAuth client credential JSON (`credentials.json`) for an OAuth Client ID (Desktop / "installed" app).

## Getting credentials

1. Go to the Google Cloud Console → APIs & Services → Credentials.
2. Create an OAuth 2.0 Client ID. For testing you can use "Desktop app".
3. Download the JSON and save it as `credentials.json` in the project root.
4. Add the redirect URI you will use to the OAuth client. For the default settings in this project use:
   - `http://localhost:8080/`

   If you change `--oauth-port` pass the matching port and register the corresponding redirect URI.

## Running

Build and run the tool from the project root:

```bash
cargo build --release
./target/release/rcheckspam --oauth-port 8080 --concurrency 8 --days 30
```

Or run directly with Cargo:

```bash
cargo run -- --days 30
```

Flags:

- `--oauth-port` (default 8080) – port the local OAuth callback server listens on.
- `--concurrency` (default 8) – number of concurrent workers fetching messages.
- `--days` (default 30) – how many days back to count.
- `--timeout` (default 60) – overall timeout (seconds) for listing/fetching.
- `--initial-delay` (default 1000) – maximum jitter in milliseconds before each message fetch.
- `--debug` – enable debug logging.

On first run the program will open your browser (or print the URL). It saves a token to `token.json` in the repo root after successful authorization. The token is refreshed automatically on subsequent runs.

## Notes & recommendations

- The local OAuth callback server binds to `127.0.0.1:<port>` (loopback only).
- For large mailboxes raise `--timeout` and/or lower `--concurrency` to avoid API rate limits.

## Troubleshooting

- "Unable to read client secret file": ensure `credentials.json` exists in the working directory.
- If the browser doesn't open, copy/paste the printed URL into your browser.
- If you see permission errors, use a non-privileged port (e.g. 8080) and update the OAuth redirect URI to match.

## Development

- Run `cargo test` to execute unit tests.
- Run `cargo fmt` to format and `cargo clippy` to lint.
- A `Makefile` wraps the common tasks (`make build`, `make test`, `make ci`, ...).
