# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project overview

A small Go CLI that counts messages in the Gmail Spam label and groups them by local date, using `internalDate`. Code is organized into a slim `cmd/gocheckspam` entry point and focused internal packages.

## Common commands

Build the binary:

```bash
make build
# or: go build -o gocheckspam ./cmd/gocheckspam
```

Run tests:

```bash
go test ./...
# run a single test
go test -run TestCountSpamByDate ./internal/spam
```

Check and format:

```bash
go vet ./...
go fmt ./...
```

The `Makefile` wraps these; useful targets include `build`, `test`, `vet`, `fmt`, `ci`, and `clean`. `make ci` runs `clean fmt vet test build`.

## Runtime behavior and configuration

Configuration is loaded by `internal/config.Load` using `pflag` bound to `viper`. Each flag has an environment-variable equivalent with prefix `GOCHECKSPAM_`:

- `-oauth-port` / `GOCHECKSPAM_OAUTH_PORT` (default `8080`)
- `-concurrency` / `GOCHECKSPAM_CONCURRENCY` (default `8`)
- `-days` / `GOCHECKSPAM_DAYS` (default `30`)
- `-timeout` / `GOCHECKSPAM_TIMEOUT` (default `60`, seconds)
- `-initial-delay` / `GOCHECKSPAM_INITIAL_DELAY` (default `1000`, ms)
- `-debug` / `GOCHECKSPAM_DEBUG` (default `false`)

On startup the program expects `credentials.json` in the working directory, obtains or refreshes a Gmail OAuth token, and writes the token to `token.json`. The local OAuth callback server binds to `127.0.0.1:<oauth-port>`.

## Architecture

- `cmd/gocheckspam/main.go` is the CLI entry point: load config, build the OAuth client, construct the Gmail adapter, run the spam service, and print results.
- `internal/config` parses CLI flags and environment variables into a `Config` struct.
- `internal/auth` owns the OAuth2 flow: reading `token.json`, launching the local callback server, exchanging the authorization code, saving the refreshed token, and opening the browser.
- `internal/gmail` defines a `Client` interface with local `Message`/`ListResponse` types and a real adapter that wraps `*gmail.Service`. `internal/gmail/fake` provides an in-memory fake implementation for tests.
- `internal/spam` contains the core orchestration: listing spam messages with pagination, concurrently fetching full messages (bounded by a semaphore), converting `internalDate` to local dates, and aggregating daily counts.
- `internal/retry` implements exponential backoff retry logic for Gmail API calls. `retry.IsNonRetryable` short-circuits retries for `googleapi.Error` codes in the `4xx` range except `429`.
- `internal/reporter` formats and prints the daily spam summary.

## Notes for working in this repo

- The project currently uses Go 1.25 (`go.mod` and CI both target it).
- `go test ./...` is the canonical test command; CI runs `go build -v ./...`, `go vet ./...`, and `go test -v ./...`.
- Keep `credentials.json` and `token.json` out of commits; they are already in `.gitignore`.
- The `.codewhale/instructions.md` file contains stale Rust-specific content and does not reflect the current Go implementation; do not rely on it.
