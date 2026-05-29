# rcheckspam

A high-performance, asynchronous CLI utility in Rust to count and aggregate messages in your Gmail Spam label by local date (based on `internalDate`).

This is a modern Rust rewrite of the original Go spam checking utility, featuring async execution, bounded concurrency, self-contained OAuth callback flows, and automated backoff retries.

## Prerequisites

- **Rust & Cargo:** (2021 Edition) Installed via [rustup](https://rustup.rs/).
- **Google Cloud Credentials:** A Google Cloud OAuth client credential JSON (`credentials.json`) for a Desktop/Installed application.

## Getting credentials

1. Go to the [Google Cloud Console](https://console.cloud.google.com/) → APIs & Services → Credentials.
2. Create an OAuth 2.0 Client ID. Use **Desktop app** as the application type.
3. Download the JSON, rename it to `credentials.json`, and save it in this project root directory.
4. The application automatically starts a local callback server listening on `127.0.0.1:8080` (or whichever port is configured via `--oauth-port`). You do not need to register redirect URIs manually for Desktop apps as Google allows dynamic ports on the loopback address.

## Running

Build and run the tool using `cargo` or the provided `Makefile`:

```bash
# Run with default settings (30 days, concurrency of 8)
cargo run --release

# Run with custom parameters
cargo run -- --oauth-port 8080 --concurrency 8 --days 15 --timeout 120
```

Alternatively, use the convenience targets in the `Makefile`:

```bash
# Build release binary
make build-prod

# Run default spam checker
make run

# Run with debug output
make run-debug
```

### CLI Flags

All arguments are parsed using `clap`:

- `--days` (default `30`): Number of days to look back.
- `--concurrency` (default `8`): Number of concurrent workers fetching messages from Gmail.
- `--timeout` (default `60`): Overall timeout in seconds for the listing/fetching process.
- `--initial-delay` (default `1000`): Maximum random delay in milliseconds before fetching each message to avoid API rate limits.
- `--oauth-port` (default `8080`): The port the local HTTP server listens on for the OAuth callback redirection.
- `--debug`: Enable verbose debug logs (errors and retry attempts).

On the first run, the utility will automatically open your default web browser for Google authentication (or print the link if automatic opening fails). Upon successful authorization, a `token.json` file is cached in the root directory. Subsequent runs will use this token and refresh it automatically if it has expired.

## Security & Architecture

- **Local Address Binding:** The embedded OAuth callback HTTP server binds specifically to the loopback interface (`127.0.0.1:<port>`) rather than all interfaces for maximum local security.
- **Lightweight Networking:** The HTTP callback handler is implemented using self-contained TCP sockets under `tokio`, eliminating third-party web framework dependencies.
- **Bounded Async Stream:** Bounded concurrency is achieved via `futures::stream::StreamExt::buffer_unordered`, ensuring smooth thread execution without raw mutex locks.

## Development

Use the standard Cargo commands or `Makefile` shortcuts:

- **Format Code:** `make fmt` (runs `cargo fmt`)
- **Lint Code:** `make lint` (runs `cargo clippy -- -D warnings`)
- **Run Tests:** `make test` (runs `cargo test`)
- **Clean Artifacts:** `make clean` (runs `cargo clean`)
