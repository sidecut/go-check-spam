# go-check-spam

Small CLI to count messages in the Gmail Spam label by local date (based on `internalDate`).

## Prerequisites

- Go 1.25+
- A Google Cloud OAuth client credential JSON (`credentials.json`) for an OAuth Client ID (Desktop or Web).

## Getting credentials

1. Go to the Google Cloud Console → APIs & Services → Credentials.
2. Create an OAuth 2.0 Client ID. For testing you can use "Desktop app" or "Web application".
3. Download the JSON and save it as `credentials.json` in the project root.
4. Add the redirect URI you will use to the OAuth client (if using a web client). For the default settings in this project use:

   - `http://localhost:8080/` or
   - `http://127.0.0.1:8080/`

   If you change `-oauth-port` pass the matching port and register the corresponding redirect URI.

## Running

Build and run the tool from the project root:

```bash
go build ./...
./go-check-spam -oauth-port=8080 -concurrency=8 -days=30
```

Flags:

- `-oauth-port` (default 8080) – port the local OAuth callback server listens on.
- `-concurrency` (default 8) – number of concurrent workers fetching messages.
- `-days` (default 30) – how many days back to count.
- `-timeout` (default 60) – overall timeout (seconds) for listing/fetching.
- `-initial-delay` (default 1000) – maximum jitter in milliseconds before each message fetch.
- `-debug` – enable debug logging.

On first run the program will open your browser (or print the URL). It saves a token to `token.json` in the repo root after successful authorization.

## Notes & recommendations

- The program currently binds to `:<port>` (all interfaces) by default. For tighter security you can modify `gmailauth.go` to bind to `127.0.0.1:<port>`.
- If you use the Google Cloud "Web application" client, make sure the redirect URI exactly matches the `http://localhost:<port>/` you use when running.
- For large mailboxes raise `-timeout` and/or lower `-concurrency` to avoid API rate limits.

## Troubleshooting

- "Unable to read client secret file": ensure `credentials.json` exists in the working directory.
- If the browser doesn't open, copy/paste the printed URL into your browser.
- If you see permission errors on `:80`, use a non-privileged port (e.g. 8080) and update OAuth redirect.

## Development

- Run `go test ./...` to execute unit tests.
- Run `go mod tidy` after changing imports.

If you want, I can:

- Make the server bind to `127.0.0.1` instead of all interfaces.
- Add a small README section showing how to register the redirect URI step-by-step with screenshots (text only).
