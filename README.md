# go-check-spam

Rust CLI to count messages in the Gmail Spam label by local date (based on Gmail `internalDate`).

## Prerequisites

- Rust toolchain (stable), including `cargo`
- Google Cloud OAuth client credentials in `credentials.json`

## Credentials setup

1. In Google Cloud Console, go to APIs & Services -> Credentials.
2. Create an OAuth 2.0 Client ID (Desktop or Web works).
3. Download the JSON and place it at `credentials.json` in the project root.
4. If using a Web OAuth client, register redirect URI matching your callback port:
   - `http://localhost:8080/`
   - or `http://127.0.0.1:8080/`

If you use `--oauth-port <PORT>`, register the matching redirect URI.

## Run

```bash
cargo run -- --oauth-port 8080 --concurrency 8 --days 30
```

First run opens a browser (or prints a URL). After successful auth, token data is cached in `token.json`.

## CLI flags

- `--oauth-port` (default `8080`) local OAuth callback port
- `--concurrency` (default `8`) concurrent Gmail message fetches
- `--days` (default `30`) lookback window
- `--timeout` (default `60`) overall request timeout in seconds
- `--initial-delay` (default `1000`) max jitter in ms before each message fetch
- `--debug` enable verbose retry/request logs

## Development

```bash
cargo fmt
cargo test
cargo clippy -- -D warnings
cargo build --release
```

## Notes

- OAuth callback listener binds to `127.0.0.1:<port>`.
- Existing Go files are left in the repository for reference; Rust source is in `src/main.rs`.
