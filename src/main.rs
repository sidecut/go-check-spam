use anyhow::{anyhow, bail, Context, Result};
use chrono::{Duration, Local, TimeZone, Utc};
use clap::Parser;
use futures::stream::{self, StreamExt};
use rand::Rng;
use reqwest::header::AUTHORIZATION;
use reqwest::Client;
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::fs;
use std::io::{self, BufRead, Read, Write};
use std::net::TcpListener;
use std::path::Path;
use std::sync::mpsc;
use std::thread;
use std::time::{Duration as StdDuration, Instant};

#[derive(Parser, Debug)]
#[command(name = "go-check-spam")]
#[command(about = "Count Gmail spam messages by date")]
struct Cli {
    #[arg(long, default_value_t = 60)]
    timeout: u64,

    #[arg(long = "initial-delay", default_value_t = 1000)]
    initial_delay_ms: u64,

    #[arg(long, default_value_t = 30)]
    days: i64,

    #[arg(long, default_value_t = false)]
    debug: bool,

    #[arg(long, default_value_t = 8)]
    concurrency: usize,

    #[arg(long = "oauth-port", default_value_t = 8080)]
    oauth_port: u16,
}

#[derive(Debug, Deserialize)]
struct GoogleCredentialsFile {
    installed: Option<GoogleCredentials>,
    web: Option<GoogleCredentials>,
}

#[derive(Debug, Deserialize, Clone)]
struct GoogleCredentials {
    client_id: String,
    client_secret: String,
    auth_uri: String,
    token_uri: String,
}

#[derive(Debug, Serialize, Deserialize, Clone)]
struct StoredToken {
    access_token: String,
    refresh_token: Option<String>,
    token_type: Option<String>,
    expires_at: Option<i64>,
}

#[derive(Debug, Deserialize)]
struct OAuthTokenResponse {
    access_token: String,
    token_type: Option<String>,
    expires_in: Option<i64>,
    refresh_token: Option<String>,
}

#[derive(Debug, Deserialize)]
struct ListMessagesResponse {
    messages: Option<Vec<MessageId>>,
    #[serde(rename = "nextPageToken")]
    next_page_token: Option<String>,
}

#[derive(Debug, Deserialize)]
struct MessageId {
    id: String,
}

#[derive(Debug, Deserialize)]
struct MessageMinimal {
    id: String,
    #[serde(rename = "internalDate")]
    internal_date: Option<String>,
}

#[tokio::main]
async fn main() {
    if let Err(err) = run().await {
        eprintln!("Error: {err:#}");
        std::process::exit(1);
    }
}

async fn run() -> Result<()> {
    let cli = Cli::parse();
    let cutoff_date = (Local::now() - Duration::days(cli.days))
        .format("%Y-%m-%d")
        .to_string();

    let creds_raw = fs::read_to_string("credentials.json")
        .context("Unable to read client secret file: credentials.json")?;
    let creds_file: GoogleCredentialsFile =
        serde_json::from_str(&creds_raw).context("Unable to parse credentials.json")?;
    let creds = creds_file
        .installed
        .or(creds_file.web)
        .ok_or_else(|| anyhow!("credentials.json must contain either 'installed' or 'web'"))?;

    let client = Client::builder()
        .timeout(StdDuration::from_secs(cli.timeout))
        .build()
        .context("Failed to build HTTP client")?;

    let mut token = get_token(&client, &creds, cli.oauth_port).await?;
    let spam_counts = get_spam_counts(&client, &mut token, &creds, &cli, &cutoff_date).await?;

    println!(
        "Spam email counts for the past {} days (based on internalDate):",
        cli.days
    );
    print_spam_summary(&spam_counts, &cutoff_date)?;

    Ok(())
}

async fn get_token(
    client: &Client,
    creds: &GoogleCredentials,
    oauth_port: u16,
) -> Result<StoredToken> {
    let token_path = Path::new("token.json");
    if token_path.exists() {
        let data = fs::read_to_string(token_path).context("Unable to read token.json")?;
        let token: StoredToken =
            serde_json::from_str(&data).context("Unable to parse token.json")?;
        if token_still_valid(&token) {
            return Ok(token);
        }
        if token.refresh_token.is_some() {
            let refreshed = refresh_access_token(client, creds, &token).await?;
            save_token(token_path, &refreshed)?;
            return Ok(refreshed);
        }
    }

    let fresh = get_token_from_web(client, creds, oauth_port).await?;
    save_token(token_path, &fresh)?;
    Ok(fresh)
}

fn token_still_valid(token: &StoredToken) -> bool {
    let Some(expires_at) = token.expires_at else {
        return false;
    };
    let skew = 60;
    Utc::now().timestamp() < expires_at - skew
}

async fn refresh_access_token(
    client: &Client,
    creds: &GoogleCredentials,
    current: &StoredToken,
) -> Result<StoredToken> {
    let refresh_token = current
        .refresh_token
        .as_ref()
        .ok_or_else(|| anyhow!("No refresh token available"))?;

    let params = [
        ("client_id", creds.client_id.as_str()),
        ("client_secret", creds.client_secret.as_str()),
        ("refresh_token", refresh_token.as_str()),
        ("grant_type", "refresh_token"),
    ];

    let resp = client
        .post(&creds.token_uri)
        .form(&params)
        .send()
        .await
        .context("Token refresh request failed")?;

    if !resp.status().is_success() {
        let status = resp.status();
        let body = resp.text().await.unwrap_or_default();
        bail!("Token refresh failed: {status} {body}");
    }

    let parsed: OAuthTokenResponse = resp
        .json()
        .await
        .context("Unable to parse refresh token response")?;

    Ok(StoredToken {
        access_token: parsed.access_token,
        refresh_token: current.refresh_token.clone(),
        token_type: parsed.token_type,
        expires_at: parsed.expires_in.map(|s| Utc::now().timestamp() + s),
    })
}

async fn get_token_from_web(
    client: &Client,
    creds: &GoogleCredentials,
    oauth_port: u16,
) -> Result<StoredToken> {
    let redirect_uri = format!("http://localhost:{oauth_port}/");
    let scope = "https://www.googleapis.com/auth/gmail.readonly";
    let auth_url = format!(
        "{}?client_id={}&redirect_uri={}&response_type=code&scope={}&access_type=offline&prompt=consent",
        creds.auth_uri,
        urlencoding::encode(&creds.client_id),
        urlencoding::encode(&redirect_uri),
        urlencoding::encode(scope)
    );

    println!(
        "Go to the following link in your browser then type the authorization code:\n{}",
        auth_url
    );

    let (tx, rx) = mpsc::channel::<String>();
    spawn_callback_server(oauth_port, tx.clone())?;
    spawn_stdin_reader(tx);

    if let Err(err) = open_browser(&auth_url) {
        eprintln!("Error opening browser: {err}");
        eprintln!("Please manually open the URL in your browser.");
    }

    let auth_code = rx
        .recv_timeout(StdDuration::from_secs(60))
        .context("Timed out waiting for authorization code")?;

    let params = [
        ("code", auth_code.as_str()),
        ("client_id", creds.client_id.as_str()),
        ("client_secret", creds.client_secret.as_str()),
        ("redirect_uri", redirect_uri.as_str()),
        ("grant_type", "authorization_code"),
    ];

    let resp = client
        .post(&creds.token_uri)
        .form(&params)
        .send()
        .await
        .context("Token exchange request failed")?;

    if !resp.status().is_success() {
        let status = resp.status();
        let body = resp.text().await.unwrap_or_default();
        bail!("Token exchange failed: {status} {body}");
    }

    let parsed: OAuthTokenResponse = resp
        .json()
        .await
        .context("Unable to parse token response")?;

    Ok(StoredToken {
        access_token: parsed.access_token,
        refresh_token: parsed.refresh_token,
        token_type: parsed.token_type,
        expires_at: parsed.expires_in.map(|s| Utc::now().timestamp() + s),
    })
}

fn spawn_callback_server(oauth_port: u16, tx: mpsc::Sender<String>) -> Result<()> {
    let listener = TcpListener::bind(("127.0.0.1", oauth_port))
        .with_context(|| format!("Unable to bind local OAuth callback port {oauth_port}"))?;

    thread::spawn(move || {
        if let Ok((mut stream, _addr)) = listener.accept() {
            let mut buf = [0u8; 4096];
            if let Ok(n) = stream.read(&mut buf) {
                let request = String::from_utf8_lossy(&buf[..n]);
                if let Some(first_line) = request.lines().next() {
                    // GET /?code=... HTTP/1.1
                    if let Some(path) = first_line.split_whitespace().nth(1) {
                        let code = extract_query_param(path, "code");
                        if let Some(code) = code {
                            let _ = tx.send(code);
                        }
                    }
                }
            }

            let response = "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\n\r\nAuthorization received. You can close this window.";
            let _ = stream.write_all(response.as_bytes());
            let _ = stream.flush();
        }
    });

    Ok(())
}

fn spawn_stdin_reader(tx: mpsc::Sender<String>) {
    thread::spawn(move || {
        print!("Enter authorization code: ");
        let _ = io::stdout().flush();
        let stdin = io::stdin();
        let mut line = String::new();
        if stdin.lock().read_line(&mut line).is_ok() {
            let trimmed = line.trim().to_string();
            if !trimmed.is_empty() {
                let _ = tx.send(trimmed);
            }
        }
    });
}

fn extract_query_param(path: &str, key: &str) -> Option<String> {
    let (_base, query) = path.split_once('?')?;
    for pair in query.split('&') {
        let mut kv = pair.splitn(2, '=');
        if let (Some(k), Some(v)) = (kv.next(), kv.next()) {
            if k == key {
                return urlencoding::decode(v).ok().map(|v| v.to_string());
            }
        }
    }
    None
}

fn save_token(path: &Path, token: &StoredToken) -> Result<()> {
    println!("Saving credential file to: {}", path.display());
    let json = serde_json::to_string_pretty(token).context("Unable to serialize token")?;
    fs::write(path, json).with_context(|| format!("Unable to write {}", path.display()))?;
    Ok(())
}

fn open_browser(url: &str) -> Result<()> {
    #[cfg(target_os = "windows")]
    {
        std::process::Command::new("cmd")
            .args(["/C", "start", url])
            .spawn()
            .context("failed to open browser")?;
        return Ok(());
    }

    #[cfg(target_os = "macos")]
    {
        std::process::Command::new("open")
            .arg(url)
            .spawn()
            .context("failed to open browser")?;
        return Ok(());
    }

    #[cfg(target_os = "linux")]
    {
        std::process::Command::new("xdg-open")
            .arg(url)
            .spawn()
            .context("failed to open browser")?;
        return Ok(());
    }

    #[allow(unreachable_code)]
    Err(anyhow!("unsupported platform"))
}

async fn get_spam_counts(
    client: &Client,
    token: &mut StoredToken,
    creds: &GoogleCredentials,
    cli: &Cli,
    cutoff_date: &str,
) -> Result<HashMap<String, i32>> {
    let mut daily_counts: HashMap<String, i32> = HashMap::new();
    let mut page_token: Option<String> = None;
    let mut total: usize = 0;
    let start = Instant::now();

    let timeout = StdDuration::from_secs(cli.timeout);
    let max_concurrency = cli.concurrency.max(1);

    println!("Gmail query: after:{cutoff_date}");

    loop {
        if start.elapsed() > timeout {
            bail!("timed out waiting for messages");
        }

        ensure_fresh_token(client, token, creds).await?;

        let mut req = client
            .get("https://gmail.googleapis.com/gmail/v1/users/me/messages")
            .query(&[("labelIds", "SPAM"), ("q", &format!("after:{cutoff_date}"))]);

        if let Some(ref token_val) = page_token {
            req = req.query(&[("pageToken", token_val)]);
        }

        let list_resp: ListMessagesResponse = retry_request(cli.debug, || async {
            let resp = req
                .try_clone()
                .ok_or_else(|| anyhow!("unable to clone request"))?
                .header(AUTHORIZATION, format!("Bearer {}", token.access_token))
                .send()
                .await
                .context("list messages request failed")?;
            parse_json_response(resp).await
        })
        .await
        .context("error fetching messages")?;

        let ids: Vec<String> = list_resp
            .messages
            .unwrap_or_default()
            .into_iter()
            .map(|m| m.id)
            .collect();

        total += ids.len();
        print!("\r{total}");
        let _ = io::stdout().flush();

        let token_value = token.access_token.clone();
        let fetches = stream::iter(ids.into_iter().map(|id| {
            let client = client.clone();
            let debug = cli.debug;
            let token_value = token_value.clone();
            let initial_delay_ms = cli.initial_delay_ms;
            async move {
                if initial_delay_ms > 0 {
                    let jitter = rand::thread_rng().gen_range(0..=initial_delay_ms);
                    tokio::time::sleep(StdDuration::from_millis(jitter)).await;
                }

                let msg_result: Result<MessageMinimal> = retry_request(debug, || {
                    let client = client.clone();
                    let token_value = token_value.clone();
                    let id = id.clone();
                    async move {
                        let url =
                            format!("https://gmail.googleapis.com/gmail/v1/users/me/messages/{id}");
                        let resp = client
                            .get(url)
                            .query(&[("format", "minimal")])
                            .header(AUTHORIZATION, format!("Bearer {}", token_value))
                            .send()
                            .await
                            .context("get message request failed")?;
                        parse_json_response(resp).await
                    }
                })
                .await;

                match msg_result {
                    Ok(msg) => {
                        if let Some(date) = msg
                            .internal_date
                            .as_deref()
                            .and_then(parse_internal_date_to_local_date)
                        {
                            Some(date)
                        } else {
                            if debug {
                                eprintln!(
                                    "Warning: Invalid internalDate ({:?}) for message ID {}",
                                    msg.internal_date, msg.id
                                );
                            }
                            None
                        }
                    }
                    Err(err) => {
                        if debug {
                            eprintln!("Error fetching message {id}: {err:#}");
                        }
                        None
                    }
                }
            }
        }))
        .buffer_unordered(max_concurrency)
        .collect::<Vec<Option<String>>>()
        .await;

        for date in fetches.into_iter().flatten() {
            *daily_counts.entry(date).or_insert(0) += 1;
        }

        page_token = list_resp.next_page_token;
        if page_token.is_none() {
            break;
        }
    }

    print!("\r");
    let _ = io::stdout().flush();

    Ok(daily_counts)
}

async fn ensure_fresh_token(
    client: &Client,
    token: &mut StoredToken,
    creds: &GoogleCredentials,
) -> Result<()> {
    if token_still_valid(token) {
        return Ok(());
    }

    if token.refresh_token.is_some() {
        let refreshed = refresh_access_token(client, creds, token).await?;
        save_token(Path::new("token.json"), &refreshed)?;
        *token = refreshed;
        return Ok(());
    }

    bail!(
        "OAuth token expired and no refresh token available; delete token.json and re-authenticate"
    )
}

async fn parse_json_response<T: for<'de> Deserialize<'de>>(resp: reqwest::Response) -> Result<T> {
    if !resp.status().is_success() {
        let status = resp.status();
        let body = resp.text().await.unwrap_or_default();
        bail!("HTTP error: {status} {body}");
    }
    let parsed = resp
        .json::<T>()
        .await
        .context("Unable to parse JSON response")?;
    Ok(parsed)
}

async fn retry_request<F, Fut, T>(debug: bool, mut op: F) -> Result<T>
where
    F: FnMut() -> Fut,
    Fut: std::future::Future<Output = Result<T>>,
{
    let mut wait = StdDuration::from_millis(300);
    let max_attempts = 8;

    for i in 0..max_attempts {
        match op().await {
            Ok(v) => return Ok(v),
            Err(err) => {
                if i == max_attempts - 1 {
                    return Err(err);
                }
                if debug {
                    eprintln!("Request failed (attempt {}): {}", i + 1, err);
                }
                let jitter_ms: u64 = rand::thread_rng().gen_range(0..200);
                tokio::time::sleep(wait + StdDuration::from_millis(jitter_ms)).await;
                wait = (wait * 2).min(StdDuration::from_secs(10));
            }
        }
    }

    bail!("retry attempts exhausted")
}

fn parse_internal_date_to_local_date(ms_str: &str) -> Option<String> {
    let ms = ms_str.parse::<i64>().ok()?;
    internal_date_to_date(ms)
}

fn internal_date_to_date(ms: i64) -> Option<String> {
    if ms <= 0 {
        return None;
    }
    let dt = Local.timestamp_millis_opt(ms).single()?;
    Some(dt.format("%Y-%m-%d").to_string())
}

fn print_spam_summary(spam_counts: &HashMap<String, i32>, cutoff_date: &str) -> Result<()> {
    let mut dates: Vec<&String> = spam_counts.keys().collect();
    dates.sort();

    let mut total = 0i32;
    let mut was_before = false;
    for date in dates {
        if date.as_str() < cutoff_date {
            was_before = true;
        } else if was_before {
            println!();
            was_before = false;
        }

        let count = *spam_counts
            .get(date)
            .ok_or_else(|| anyhow!("missing count for date"))?;
        total += count;

        let weekday = chrono::NaiveDate::parse_from_str(date, "%Y-%m-%d")
            .context("Error parsing date")?
            .format("%a")
            .to_string();
        println!("{weekday} {date} {count}");
    }

    println!("Total: {total}");
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use chrono::Local;

    #[test]
    fn test_internal_date_to_date() {
        let ts = 1_577_936_645_000i64;
        let got = internal_date_to_date(ts).expect("date expected");
        let expected = Local
            .timestamp_millis_opt(ts)
            .single()
            .expect("valid timestamp")
            .format("%Y-%m-%d")
            .to_string();
        assert_eq!(got, expected);
        assert!(internal_date_to_date(0).is_none());
    }
}
