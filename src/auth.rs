use anyhow::{Result, anyhow};
use chrono::{DateTime, Duration, Utc};
use serde::{Deserialize, Serialize};
use std::fs::File;
use std::io::{self, Write};
use std::path::Path;

#[allow(dead_code)]
#[derive(Deserialize, Debug, Clone)]
pub struct CredentialsConfig {
    pub installed: InstalledConfig,
}

#[allow(dead_code)]
#[derive(Deserialize, Debug, Clone)]
pub struct InstalledConfig {
    pub client_id: String,
    pub project_id: String,
    pub auth_uri: String,
    pub token_uri: String,
    pub client_secret: String,
    pub redirect_uris: Vec<String>,
}

#[derive(Serialize, Deserialize, Clone, Debug)]
pub struct Token {
    pub access_token: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub token_type: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub refresh_token: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub expiry: Option<DateTime<Utc>>,
}

#[derive(Deserialize, Debug)]
struct RefreshResponse {
    access_token: String,
    token_type: String,
    expires_in: i64,
    refresh_token: Option<String>,
}

#[derive(Deserialize, Debug)]
struct TokenResponse {
    access_token: String,
    token_type: String,
    expires_in: i64,
    refresh_token: Option<String>,
}

const TOKEN_FILE: &str = "token.json";

/// Loads credentials from a local file.
pub fn load_credentials<P: AsRef<Path>>(path: P) -> Result<CredentialsConfig> {
    let file = File::open(path)?;
    let creds: CredentialsConfig = serde_json::from_reader(file)?;
    Ok(creds)
}

/// Retrieves a valid token. If token.json exists, loads and validates/refreshes it.
/// If not, triggers the OAuth web workflow.
pub async fn get_token(creds: &CredentialsConfig, oauth_port: u16) -> Result<Token> {
    let token = match load_token(TOKEN_FILE) {
        Ok(mut t) => {
            if let Err(e) = ensure_valid_token(&mut t, creds, TOKEN_FILE).await {
                eprintln!(
                    "Warning: Failed to refresh existing token: {}. Initiating re-auth...",
                    e
                );
                get_token_from_web(creds, oauth_port).await?
            } else {
                t
            }
        }
        Err(_) => get_token_from_web(creds, oauth_port).await?,
    };

    // Ensure we write it back to token.json
    save_token(TOKEN_FILE, &token)?;
    Ok(token)
}

fn load_token<P: AsRef<Path>>(path: P) -> Result<Token> {
    let file = File::open(path)?;
    let token: Token = serde_json::from_reader(file)?;
    Ok(token)
}

pub fn save_token<P: AsRef<Path>>(path: P, token: &Token) -> Result<()> {
    let file = File::create(path)?;
    serde_json::to_writer_pretty(file, token)?;
    Ok(())
}

/// Ensures the token is valid, refreshing it if expired or expiring soon.
pub async fn ensure_valid_token(
    token: &mut Token,
    creds: &CredentialsConfig,
    token_path: &str,
) -> Result<()> {
    let expiry = match token.expiry {
        Some(exp) => exp,
        None => return Ok(()), // If no expiry, assume always valid (e.g. static token)
    };

    // If token expires in less than 10 seconds, refresh it.
    if expiry.signed_duration_since(Utc::now()).num_seconds() > 10 {
        return Ok(());
    }

    let refresh_token = match &token.refresh_token {
        Some(rt) => rt,
        None => return Err(anyhow!("No refresh token found for automatic refresh")),
    };

    let client = reqwest::Client::new();
    let res = client
        .post(&creds.installed.token_uri)
        .form(&[
            ("client_id", &creds.installed.client_id),
            ("client_secret", &creds.installed.client_secret),
            ("refresh_token", refresh_token),
            ("grant_type", &"refresh_token".to_string()),
        ])
        .send()
        .await?
        .error_for_status()?
        .json::<RefreshResponse>()
        .await?;

    token.access_token = res.access_token;
    token.token_type = Some(res.token_type);
    token.expiry = Some(Utc::now() + Duration::seconds(res.expires_in));
    if let Some(new_rt) = res.refresh_token {
        token.refresh_token = Some(new_rt);
    }

    // Save the refreshed token
    save_token(token_path, token)?;
    Ok(())
}

async fn get_token_from_web(creds: &CredentialsConfig, port: u16) -> Result<Token> {
    let redirect_uri = format!("http://localhost:{}", port);
    let mut auth_url = reqwest::Url::parse(&creds.installed.auth_uri)?;
    {
        let mut query = auth_url.query_pairs_mut();
        query.append_pair("client_id", &creds.installed.client_id);
        query.append_pair("redirect_uri", &redirect_uri);
        query.append_pair("response_type", "code");
        query.append_pair("scope", "https://www.googleapis.com/auth/gmail.readonly");
        query.append_pair("access_type", "offline");
        query.append_pair("state", "state-token");
        query.append_pair("prompt", "consent");
    }

    let auth_url_str = auth_url.to_string();
    println!(
        "Go to the following link in your browser then type the authorization code:\n{}",
        auth_url_str
    );

    let (tx, mut rx) = tokio::sync::mpsc::channel(2);

    // 1. Start lightweight HTTP callback server on separate task
    let tx_http = tx.clone();
    tokio::spawn(async move {
        let (oneshot_tx, oneshot_rx) = tokio::sync::oneshot::channel();
        tokio::spawn(start_callback_server(port, oneshot_tx));
        if let Ok(code) = oneshot_rx.await {
            let _ = tx_http.send(code).await;
        }
    });

    // 2. Read standard input as a fallback
    let tx_stdin = tx.clone();
    tokio::task::spawn_blocking(move || {
        print!("Enter authorization code: ");
        let _ = io::stdout().flush();
        let mut code = String::new();
        if io::stdin().read_line(&mut code).is_ok() {
            let code = code.trim().to_string();
            if !code.is_empty() {
                let _ = tx_stdin.blocking_send(code);
            }
        }
    });

    // 3. Open browser automatically
    if let Err(e) = open::that(&auth_url_str) {
        eprintln!("Error opening browser: {}", e);
        println!("Please manually open the URL in your browser.");
    }

    // 4. Wait for the code (or timeout)
    let auth_code = tokio::select! {
        code = rx.recv() => {
            code.ok_or_else(|| anyhow!("Failed to receive auth code"))?
        }
        _ = tokio::time::sleep(std::time::Duration::from_secs(60)) => {
            anyhow::bail!("Timed out waiting for authorization code.")
        }
    };

    // 5. Exchange auth code for token
    let client = reqwest::Client::new();
    let res = client
        .post(&creds.installed.token_uri)
        .form(&[
            ("code", &auth_code),
            ("client_id", &creds.installed.client_id),
            ("client_secret", &creds.installed.client_secret),
            ("redirect_uri", &redirect_uri),
            ("grant_type", &"authorization_code".to_string()),
        ])
        .send()
        .await?
        .error_for_status()?
        .json::<TokenResponse>()
        .await?;

    let token = Token {
        access_token: res.access_token,
        token_type: Some(res.token_type),
        refresh_token: res.refresh_token,
        expiry: Some(Utc::now() + Duration::seconds(res.expires_in)),
    };

    Ok(token)
}

async fn start_callback_server(port: u16, code_tx: tokio::sync::oneshot::Sender<String>) {
    let addr = format!("127.0.0.1:{}", port);
    let listener = match tokio::net::TcpListener::bind(&addr).await {
        Ok(l) => l,
        Err(e) => {
            eprintln!("Unable to start HTTP server on {}: {}", addr, e);
            return;
        }
    };

    if let Ok((mut stream, _)) = listener.accept().await {
        let mut buffer = [0; 4096];
        let mut n = 0;
        // Read until we have a complete HTTP request
        while n < buffer.len() {
            match tokio::io::AsyncReadExt::read(&mut stream, &mut buffer[n..]).await {
                Ok(0) => break,
                Ok(bytes_read) => {
                    n += bytes_read;
                    if buffer[..n].windows(4).any(|w| w == b"\r\n\r\n") {
                        break;
                    }
                }
                Err(_) => break,
            }
        }

        let request = String::from_utf8_lossy(&buffer[..n]);
        let code = request.lines().next().and_then(|line| {
            let parts: Vec<&str> = line.split_whitespace().collect();
            if parts.len() >= 2 {
                let path = parts[1];
                let url = reqwest::Url::parse(&format!("http://localhost{}", path)).ok()?;
                url.query_pairs()
                    .find(|(k, _)| k == "code")
                    .map(|(_, v)| v.into_owned())
            } else {
                None
            }
        });

        if let Some(auth_code) = code {
            println!("\nReceived authorization code: {}", auth_code);
            let response = "HTTP/1.1 200 OK\r\nContent-Length: 43\r\nContent-Type: text/plain\r\n\r\nAuthorization received. You can close this window.";
            let _ = tokio::io::AsyncWriteExt::write_all(&mut stream, response.as_bytes()).await;
            let _ = code_tx.send(auth_code);
        } else {
            let response = "HTTP/1.1 400 Bad Request\r\nContent-Length: 11\r\nContent-Type: text/plain\r\n\r\nBad Request";
            let _ = tokio::io::AsyncWriteExt::write_all(&mut stream, response.as_bytes()).await;
        }
    }
}
