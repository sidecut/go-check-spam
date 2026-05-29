# go-check-spam

Small CLI to count messages in the Gmail Spam label by local date, based on `internalDate`.

## Prerequisites

- Rust toolchain with Cargo
- A Google Cloud OAuth client credential JSON (`credentials.json`) for an OAuth client ID (Desktop or Web)

## Getting credentials

1. Go to the Google Cloud Console, then APIs & Services, then Credentials.
2. Create an OAuth 2.0 Client ID. For testing you can use either a desktop or web client.
3. Download the JSON and save it as `credentials.json` in the project root.
4. Add the redirect URI you will use to the OAuth client. With the default settings in this project use `http://localhost:8080/`.
5. If you change `--oauth-port`, register the matching redirect URI in Google Cloud.

## Running

Build and run the tool from the project root:

```bash
cargo build
cargo run -- --oauth-port=8080 --concurrency=8 --days=30
```

Flags:

- `--oauth-port` (default 8080) sets the local OAuth callback port.
- `--concurrency` (default 8) sets the number of concurrent workers fetching messages.
- `--days` (default 30) controls how many days back to count.
- `--timeout` (default 60) sets the overall timeout in seconds for listing and fetching.
- `--initial-delay` (default 1000) sets the maximum jitter in milliseconds before each message fetch.
- `--debug` enables debug logging.

On first run the program opens your browser or prints the URL. It saves a token to `token.json` in the repo root after successful authorization.

## Notes

- The callback server binds to `127.0.0.1`.
- If you use a web OAuth client, make sure the redirect URI exactly matches the port you run with.
- For large mailboxes raise `--timeout` or lower `--concurrency` to reduce the chance of hitting rate limits.

## Troubleshooting

- If you see `Unable to read credentials.json`, ensure the file exists in the working directory.
- If the browser does not open, copy and paste the printed URL into your browser.
- If authorization fails because of the redirect URI, verify that `http://localhost:<port>/` is registered in Google Cloud.

## Development

- Run `cargo test` to execute unit tests.
- Run `cargo fmt` before committing changes.
- Run `cargo clippy` if you want an additional lint pass.
