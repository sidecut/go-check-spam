# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project overview

A small Go CLI that counts messages in the Gmail Spam label and groups them by local date, using `internalDate`. All code lives in package `main` with a flat file layout.

## Common commands

Build the binary:

```bash
go build ./...
```

Run tests (the suite covers date conversion, retry logic, and API-error classification):

```bash
go test ./...
go test -run TestRetryWithBackoff ./...
```

Check and format:

```bash
go vet ./...
go fmt ./...
```

The `Makefile` wraps these; useful targets include `build`, `test`, `vet`, `fmt`, `ci`, and `clean`. `make ci` runs `clean fmt vet test build`.

## Runtime behavior and configuration

Configuration is loaded in `loadConfig` in `main.go` and uses `pflag` bound to `viper`. Each flag has an environment-variable equivalent with prefix `GOCHECKSPAM_`:

- `-oauth-port` / `GOCHECKSPAM_OAUTH_PORT` (default `8080`)
- `-concurrency` / `GOCHECKSPAM_CONCURRENCY` (default `8`)
- `-days` / `GOCHECKSPAM_DAYS` (default `30`)
- `-timeout` / `GOCHECKSPAM_TIMEOUT` (default `60`, seconds)
- `-initial-delay` / `GOCHECKSPAM_INITIAL_DELAY` (default `1000`, ms)
- `-debug` / `GOCHECKSPAM_DEBUG` (default `false`)

On startup the program expects `credentials.json` in the working directory, obtains or refreshes a Gmail OAuth token, and writes the token to `token.json`. The local OAuth callback server binds to `127.0.0.1:<oauth-port>`.

## Architecture

- `main.go` contains the full application: CLI/config parsing, retry/backoff logic, concurrent message fetching, and date aggregation/output.
- `gmailauth.go` owns the OAuth2 flow: reading `token.json`, launching the local callback server, exchanging the authorization code, saving the refreshed token, and opening the browser.
- Concurrency is controlled with an `errgroup.Group` plus a semaphore channel sized by `-concurrency`. One goroutine per message fetches the full message with `Users.Messages.Get("me", id).Format("minimal")`.
- Gmail API calls are retried with exponential backoff via `retryWithBackoff`. `isNonRetryable` short-circuits retries for `googleapi.Error` codes in the `4xx` range except `429`.
- Message dates are derived from `gmail.Message.InternalDate` (ms since epoch), converted to the local timezone and formatted as `2006-01-02`. The summary prints dates before the cutoff, then a blank line, then dates on or after the cutoff, with a total.

## Notes for working in this repo

- The project currently uses Go 1.25 (`go.mod` and CI both target it).
- `go test ./...` is the canonical test command; CI runs `go build -v ./...`, `go vet ./...`, and `go test -v ./...`.
- Keep `credentials.json` and `token.json` out of commits; they are already in `.gitignore`.
- The `.codewhale/instructions.md` file contains stale Rust-specific content and does not reflect the current Go implementation; do not rely on it.
