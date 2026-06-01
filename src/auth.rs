use anyhow::{anyhow, Context, Result};
use serde::{Deserialize, Serialize};
use std::process::Command;
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::TcpListener;

const GMAIL_READONLY_SCOPE: &str = "https://www.googleapis.com/auth/gmail.readonly";
const TOKEN_FILE: &str = "token.json";

/// OAuth client secret as downloaded from the Google Cloud Console
/// (the "installed" application variant).
#[derive(Debug, Deserialize)]
struct ClientSecretFile {
    installed: ClientSecret,
}

#[derive(Debug, Deserialize)]
struct ClientSecret {
    client_id: String,
    client_secret: String,
    auth_uri: String,
    token_uri: String,
}

/// On-disk token cache. The layout is intentionally compatible with the
/// token.json produced by Go's `golang.org/x/oauth2` package.
#[derive(Debug, Default, Serialize, Deserialize)]
struct StoredToken {
    #[serde(default)]
    access_token: String,
    #[serde(default)]
    token_type: String,
    #[serde(default)]
    refresh_token: String,
    /// RFC3339 expiry timestamp.
    #[serde(default)]
    expiry: Option<chrono::DateTime<chrono::Utc>>,
}

#[derive(Debug, Deserialize)]
struct TokenResponse {
    access_token: String,
    #[serde(default)]
    refresh_token: Option<String>,
    #[serde(default)]
    expires_in: Option<i64>,
    #[serde(default)]
    token_type: Option<String>,
}

/// Obtain a valid Gmail access token, performing the interactive web flow on
/// first use and refreshing a cached token when possible.
pub async fn get_access_token(credentials_path: &str, oauth_port: u16) -> Result<String> {
    let creds_raw = std::fs::read_to_string(credentials_path)
        .with_context(|| format!("Unable to read client secret file: {credentials_path}"))?;
    let secret: ClientSecret = serde_json::from_str::<ClientSecretFile>(&creds_raw)
        .context("Unable to parse client secret file to config")?
        .installed;

    let client = reqwest::Client::new();

    if let Some(tok) = load_token(TOKEN_FILE) {
        // Use the cached access token while it is still valid.
        if !tok.access_token.is_empty() && !token_expired(&tok) {
            return Ok(tok.access_token);
        }
        // Otherwise try to refresh it.
        if !tok.refresh_token.is_empty() {
            if let Ok(refreshed) = refresh_token(&client, &secret, &tok.refresh_token).await {
                save_token(TOKEN_FILE, &refreshed);
                return Ok(refreshed.access_token);
            }
        }
    }

    let tok = get_token_from_web(&client, &secret, oauth_port).await?;
    save_token(TOKEN_FILE, &tok);
    Ok(tok.access_token)
}

fn token_expired(tok: &StoredToken) -> bool {
    match tok.expiry {
        // Treat tokens expiring within the next minute as expired.
        Some(expiry) => expiry <= chrono::Utc::now() + chrono::Duration::seconds(60),
        None => false,
    }
}

fn load_token(path: &str) -> Option<StoredToken> {
    let raw = std::fs::read_to_string(path).ok()?;
    serde_json::from_str(&raw).ok()
}

fn save_token(path: &str, tok: &StoredToken) {
    println!("Saving credential file to: {path}");
    match serde_json::to_string(tok) {
        Ok(json) => {
            if let Err(e) = std::fs::write(path, json) {
                eprintln!("Unable to cache oauth token: {e}");
            }
        }
        Err(e) => eprintln!("Unable to serialize oauth token: {e}"),
    }
}

async fn refresh_token(
    client: &reqwest::Client,
    secret: &ClientSecret,
    refresh_token: &str,
) -> Result<StoredToken> {
    let params = [
        ("client_id", secret.client_id.as_str()),
        ("client_secret", secret.client_secret.as_str()),
        ("refresh_token", refresh_token),
        ("grant_type", "refresh_token"),
    ];
    let resp = client
        .post(&secret.token_uri)
        .form(&params)
        .send()
        .await?
        .error_for_status()?
        .json::<TokenResponse>()
        .await?;

    Ok(to_stored_token(resp, Some(refresh_token.to_string())))
}

async fn exchange_code(
    client: &reqwest::Client,
    secret: &ClientSecret,
    code: &str,
    redirect_uri: &str,
) -> Result<StoredToken> {
    let params = [
        ("client_id", secret.client_id.as_str()),
        ("client_secret", secret.client_secret.as_str()),
        ("code", code),
        ("redirect_uri", redirect_uri),
        ("grant_type", "authorization_code"),
    ];
    let resp = client
        .post(&secret.token_uri)
        .form(&params)
        .send()
        .await?
        .error_for_status()
        .context("Unable to retrieve token from web")?
        .json::<TokenResponse>()
        .await?;

    let refresh = resp.refresh_token.clone();
    Ok(to_stored_token(resp, refresh))
}

fn to_stored_token(resp: TokenResponse, fallback_refresh: Option<String>) -> StoredToken {
    let expiry = resp
        .expires_in
        .map(|secs| chrono::Utc::now() + chrono::Duration::seconds(secs));
    StoredToken {
        access_token: resp.access_token,
        token_type: resp.token_type.unwrap_or_else(|| "Bearer".to_string()),
        refresh_token: resp.refresh_token.or(fallback_refresh).unwrap_or_default(),
        expiry,
    }
}

async fn get_token_from_web(
    client: &reqwest::Client,
    secret: &ClientSecret,
    oauth_port: u16,
) -> Result<StoredToken> {
    let redirect_uri = format!("http://localhost:{oauth_port}/");
    let auth_url = format!(
        "{}?client_id={}&redirect_uri={}&response_type=code&scope={}&access_type=offline&state=state-token",
        secret.auth_uri,
        urlencoding::encode(&secret.client_id),
        urlencoding::encode(&redirect_uri),
        urlencoding::encode(GMAIL_READONLY_SCOPE),
    );

    println!(
        "Go to the following link in your browser then type the authorization code: \n{auth_url}"
    );

    if let Err(e) = open_browser(&auth_url) {
        eprintln!("Error opening browser: {e}");
        eprintln!("Please manually open the URL in your browser.");
    }

    // Wait for the authorization code, racing the local callback server
    // against an overall timeout.
    let code = tokio::time::timeout(
        std::time::Duration::from_secs(60),
        wait_for_code(oauth_port),
    )
    .await
    .map_err(|_| anyhow!("Timed out waiting for authorization code."))??;

    exchange_code(client, secret, &code, &redirect_uri).await
}

/// Run a minimal one-shot HTTP server that captures the `code` query parameter
/// from Google's OAuth redirect.
async fn wait_for_code(oauth_port: u16) -> Result<String> {
    let listener = TcpListener::bind(("127.0.0.1", oauth_port))
        .await
        .with_context(|| format!("Unable to start HTTP server on port {oauth_port}"))?;

    loop {
        let (mut stream, _) = listener.accept().await?;

        let mut buf = vec![0u8; 8192];
        let n = stream.read(&mut buf).await?;
        let request = String::from_utf8_lossy(&buf[..n]);

        let path = request
            .lines()
            .next()
            .and_then(|line| line.split_whitespace().nth(1))
            .unwrap_or("/");

        if let Some(code) = extract_code(path) {
            let body = "Authorization received. You can close this window.";
            let response = format!(
                "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
                body.len(),
                body
            );
            let _ = stream.write_all(response.as_bytes()).await;
            let _ = stream.flush().await;
            println!();
            println!("Received authorization code: {code}");
            return Ok(code);
        }

        let not_found = "HTTP/1.1 404 Not Found\r\nContent-Length: 0\r\nConnection: close\r\n\r\n";
        let _ = stream.write_all(not_found.as_bytes()).await;
        let _ = stream.flush().await;
    }
}

fn extract_code(path: &str) -> Option<String> {
    let query = path.split_once('?')?.1;
    for pair in query.split('&') {
        if let Some((key, value)) = pair.split_once('=') {
            if key == "code" {
                return Some(urlencoding::decode(value).ok()?.into_owned());
            }
        }
    }
    None
}

fn open_browser(url: &str) -> Result<()> {
    let (cmd, args): (&str, Vec<&str>) = if cfg!(target_os = "macos") {
        ("open", vec![url])
    } else if cfg!(target_os = "windows") {
        ("cmd", vec!["/c", "start", url])
    } else if cfg!(target_os = "linux") {
        ("xdg-open", vec![url])
    } else {
        return Err(anyhow!("unsupported platform"));
    };

    Command::new(cmd).args(args).spawn()?;
    Ok(())
}
