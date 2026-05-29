use anyhow::{anyhow, Context, Result};
use chrono::{DateTime, Local, NaiveDate, TimeZone, Utc};
use clap::Parser;
use rand::Rng;
use reqwest::blocking::Client;
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::fs::{self, File, OpenOptions};
use std::io::{self, Read, Write};
use std::net::{TcpListener, TcpStream};
use std::path::{Path, PathBuf};
use std::sync::{mpsc, Arc, Mutex};
use std::thread;
use std::time::{Duration, Instant};
use threadpool::ThreadPool;
use url::form_urlencoded;

const GMAIL_SCOPE: &str = "https://www.googleapis.com/auth/gmail.readonly";
const DEFAULT_CUTOFF_DATE_FORMAT: &str = "%Y-%m-%d";

#[derive(Debug, Clone, Parser)]
#[command(name = "go-check-spam")]
pub struct Args {
    #[arg(long, default_value_t = 60)]
    pub timeout: u64,

    #[arg(long = "initial-delay", default_value_t = 1000)]
    pub initial_delay: u64,

    #[arg(long, default_value_t = 30)]
    pub days: i64,

    #[arg(long, default_value_t = false)]
    pub debug: bool,

    #[arg(long, default_value_t = 8)]
    pub concurrency: usize,

    #[arg(long = "oauth-port", default_value_t = 8080)]
    pub oauth_port: u16,
}

#[derive(Debug, Deserialize)]
struct ClientSecrets {
    installed: Option<OAuthClientConfig>,
    web: Option<OAuthClientConfig>,
}

#[derive(Debug, Clone, Deserialize)]
struct OAuthClientConfig {
    client_id: String,
    client_secret: Option<String>,
    auth_uri: String,
    token_uri: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct StoredToken {
    access_token: String,
    token_type: String,
    refresh_token: Option<String>,
    #[serde(alias = "expires_at")]
    expiry: Option<DateTime<Utc>>,
    scope: Option<String>,
}

#[derive(Debug, Deserialize)]
struct TokenResponse {
    access_token: String,
    token_type: String,
    expires_in: Option<i64>,
    refresh_token: Option<String>,
    scope: Option<String>,
}

#[derive(Debug, Deserialize)]
struct ListMessagesResponse {
    messages: Option<Vec<MessageRef>>,
    #[serde(rename = "nextPageToken")]
    next_page_token: Option<String>,
}

#[derive(Debug, Deserialize)]
struct MessageRef {
    id: String,
}

#[derive(Debug, Deserialize)]
struct GmailMessage {
    id: String,
    #[serde(rename = "internalDate")]
    internal_date: Option<String>,
}

pub fn run(args: Args) -> Result<()> {
    let client = Client::builder()
        .timeout(Duration::from_secs(args.timeout))
        .build()?;
    let cutoff_date = Local::now()
        .date_naive()
        .checked_sub_signed(chrono::Duration::days(args.days))
        .ok_or_else(|| anyhow!("failed to compute cutoff date"))?
        .format(DEFAULT_CUTOFF_DATE_FORMAT)
        .to_string();

    let credentials = read_client_config(Path::new("credentials.json"))?;
    let token = get_or_authorize_token(&client, &credentials, args.oauth_port)?;

    let spam_counts = get_spam_counts(&client, &token, &args, &cutoff_date)?;
    if spam_counts.is_empty() {
        println!("No spam messages found.");
    }

    println!(
        "Spam email counts for the past {} days (based on internalDate):",
        args.days
    );
    print_spam_summary(&spam_counts, &cutoff_date)?;

    Ok(())
}

pub fn internal_date_to_date(ms: i64) -> String {
    if ms <= 0 {
        return String::new();
    }

    match Local.timestamp_millis_opt(ms).single() {
        Some(dt) => dt.format(DEFAULT_CUTOFF_DATE_FORMAT).to_string(),
        None => String::new(),
    }
}

pub fn print_spam_summary(spam_counts: &HashMap<String, usize>, cutoff_date: &str) -> Result<()> {
    let mut dates: Vec<_> = spam_counts.keys().cloned().collect();
    dates.sort();

    let mut total = 0usize;
    let mut saw_before = false;

    for date in dates {
        if date.as_str() < cutoff_date {
            saw_before = true;
        } else if saw_before {
            println!();
            saw_before = false;
        }

        let count = spam_counts.get(&date).copied().unwrap_or_default();
        total += count;
        let date_value = NaiveDate::parse_from_str(&date, DEFAULT_CUTOFF_DATE_FORMAT)
            .with_context(|| format!("error parsing date: {}", date))?;
        let day_of_week = date_value.format("%a");
        println!("{} {} {}", day_of_week, date, count);
    }

    println!("Total: {}", total);
    Ok(())
}

fn get_spam_counts(
    client: &Client,
    token: &StoredToken,
    args: &Args,
    cutoff_date: &str,
) -> Result<HashMap<String, usize>> {
    let counts = list_spam_messages(client, token, args, cutoff_date)?;
    Ok(counts)
}

fn list_spam_messages(
    client: &Client,
    token: &StoredToken,
    args: &Args,
    cutoff_date: &str,
) -> Result<HashMap<String, usize>> {
    let counts = HashMap::new();
    let mut page_token = String::new();
    let query = format!("after:{}", cutoff_date);
    println!("Gmail query: {}", query);
    let mut total = 0usize;

    let deadline = Instant::now() + Duration::from_secs(args.timeout);
    let worker_count = args.concurrency.max(1);
    let pool = ThreadPool::new(worker_count);
    let counts = Arc::new(Mutex::new(counts));

    while Instant::now() < deadline {
        let response: ListMessagesResponse = retry_with_backoff(deadline, args.debug, || {
            let request_timeout = remaining_timeout(deadline)?;
            let url = if page_token.is_empty() {
                "https://gmail.googleapis.com/gmail/v1/users/me/messages".to_string()
            } else {
                format!(
                    "https://gmail.googleapis.com/gmail/v1/users/me/messages?pageToken={}",
                    urlencoding::encode(&page_token)
                )
            };

            client
                .get(&url)
                .timeout(request_timeout)
                .bearer_auth(&token.access_token)
                .query(&[("labelIds", "SPAM"), ("q", query.as_str())])
                .send()
                .and_then(|resp| resp.error_for_status())?
                .json::<ListMessagesResponse>()
                .context("unable to list messages")
        })?;

        let messages = response.messages.unwrap_or_default();
        for msg in messages {
            total += 1;
            print!("\r{}", total);
            io::stdout().flush().ok();

            let client = client.clone();
            let token = token.clone();
            let counts = Arc::clone(&counts);
            let debug = args.debug;
            let initial_delay = args.initial_delay;
            let deadline = deadline;

            pool.execute(move || {
                if initial_delay > 0 {
                    if let Ok(remaining) = remaining_timeout(deadline) {
                        let delay_cap = remaining.as_millis().min(initial_delay as u128) as u64;
                        if delay_cap > 0 {
                            let delay_ms = rand::thread_rng().gen_range(0..delay_cap);
                            thread::sleep(Duration::from_millis(delay_ms));
                        }
                    }
                }

                if let Ok(message) = retry_with_backoff(deadline, debug, || {
                    fetch_message(&client, &token, &msg.id, deadline)
                }) {
                    if let Some(internal_date) = message
                        .internal_date
                        .as_deref()
                        .and_then(|value| value.parse::<i64>().ok())
                    {
                        let date = internal_date_to_date(internal_date);
                        if !date.is_empty() {
                            if let Ok(mut guard) = counts.lock() {
                                *guard.entry(date).or_insert(0) += 1;
                            }
                        }
                    } else if debug {
                        eprintln!("Warning: invalid internalDate for message {}", message.id);
                    }
                } else if debug {
                    eprintln!("Failed to fetch message {}", msg.id);
                }
            });
        }

        page_token = response.next_page_token.unwrap_or_default();
        if page_token.is_empty() {
            break;
        }
    }

    print!("\r");
    io::stdout().flush().ok();
    println!("Draining remaining workers...");
    pool.join();
    println!("Done.");

    let guard = counts
        .lock()
        .map_err(|_| anyhow!("failed to lock counts"))?;
    Ok(guard.clone())
}

fn fetch_message(
    client: &Client,
    token: &StoredToken,
    message_id: &str,
    deadline: Instant,
) -> Result<GmailMessage> {
    let request_timeout = remaining_timeout(deadline)?;
    let url = format!(
        "https://gmail.googleapis.com/gmail/v1/users/me/messages/{}?format=minimal",
        urlencoding::encode(message_id)
    );
    let message = client
        .get(&url)
        .timeout(request_timeout)
        .bearer_auth(&token.access_token)
        .send()
        .and_then(|resp| resp.error_for_status())?
        .json::<GmailMessage>()
        .context("unable to fetch message")?;
    Ok(message)
}

fn retry_with_backoff<T, F>(deadline: Instant, debug: bool, mut op: F) -> Result<T>
where
    F: FnMut() -> Result<T>,
{
    let mut wait = Duration::from_millis(300);
    for attempt in 0..8 {
        if Instant::now() >= deadline {
            return Err(anyhow!("operation timed out"));
        }

        match op() {
            Ok(value) => return Ok(value),
            Err(err) => {
                if attempt == 7 {
                    return Err(err);
                }

                if debug {
                    eprintln!("Retryable error: {}", err);
                }

                let jitter = Duration::from_millis(rand::thread_rng().gen_range(0..200));
                let remaining = remaining_timeout(deadline)?;
                let sleep_for = std::cmp::min(wait + jitter, remaining);
                thread::sleep(sleep_for);
                wait = std::cmp::min(wait * 2, Duration::from_secs(10));
            }
        }
    }

    Err(anyhow!("retry attempts exhausted"))
}

fn read_client_config(path: &Path) -> Result<OAuthClientConfig> {
    let contents =
        fs::read_to_string(path).with_context(|| format!("unable to read {}", path.display()))?;
    let secrets: ClientSecrets =
        serde_json::from_str(&contents).context("unable to parse credentials.json")?;
    secrets.installed.or(secrets.web).ok_or_else(|| {
        anyhow!("credentials.json did not contain installed or web OAuth client settings")
    })
}

fn get_or_authorize_token(
    client: &Client,
    config: &OAuthClientConfig,
    oauth_port: u16,
) -> Result<StoredToken> {
    let token_path = PathBuf::from("token.json");
    let mut token = match load_token(&token_path) {
        Ok(token) => token,
        Err(_) => authorize_via_browser_and_save(client, config, oauth_port, &token_path)?,
    };

    if token.is_expired() {
        token = refresh_token(client, config, &token)
            .or_else(|_| authorize_via_browser_and_save(client, config, oauth_port, &token_path))?;
        save_token(&token_path, &token)?;
    }

    Ok(token)
}

fn authorize_via_browser_and_save(
    client: &Client,
    config: &OAuthClientConfig,
    oauth_port: u16,
    token_path: &Path,
) -> Result<StoredToken> {
    let redirect_uri = format!("http://localhost:{}/", oauth_port);
    let auth_url = build_auth_url(config, &redirect_uri);
    println!(
        "Go to the following link in your browser then type the authorization code: \n{}",
        auth_url
    );

    let (tx, rx) = mpsc::channel::<String>();
    let server_tx = tx.clone();
    let redirect_uri_for_server = redirect_uri.clone();
    thread::spawn(move || start_callback_server(oauth_port, server_tx, &redirect_uri_for_server));

    thread::spawn(move || {
        print!("Enter authorization code: ");
        io::stdout().flush().ok();
        let mut code = String::new();
        if io::stdin().read_line(&mut code).is_ok() {
            let _ = tx.send(code.trim().to_string());
        }
    });

    if let Err(err) = open::that_detached(&auth_url) {
        eprintln!("Error opening browser: {}", err);
        eprintln!("Please manually open the URL in your browser.");
    }

    let auth_code = rx
        .recv_timeout(Duration::from_secs(60))
        .context("timed out waiting for authorization code")?;
    let token = exchange_code_for_token(client, config, &auth_code, &redirect_uri)?;
    save_token(token_path, &token)?;
    Ok(token)
}

fn build_auth_url(config: &OAuthClientConfig, redirect_uri: &str) -> String {
    let mut serializer = form_urlencoded::Serializer::new(String::new());
    serializer.append_pair("response_type", "code");
    serializer.append_pair("client_id", &config.client_id);
    serializer.append_pair("redirect_uri", redirect_uri);
    serializer.append_pair("scope", GMAIL_SCOPE);
    serializer.append_pair("access_type", "offline");
    serializer.append_pair("prompt", "consent");
    format!("{}?{}", config.auth_uri, serializer.finish())
}

fn start_callback_server(port: u16, tx: mpsc::Sender<String>, redirect_uri: &str) {
    let listener = match TcpListener::bind(("127.0.0.1", port)) {
        Ok(listener) => listener,
        Err(err) => {
            eprintln!("Unable to start HTTP server on {}: {}", redirect_uri, err);
            return;
        }
    };

    for stream in listener.incoming() {
        match stream {
            Ok(mut stream) => {
                if let Some(code) = read_auth_code_from_stream(&mut stream) {
                    let _ = tx.send(code.clone());
                    let body = "Authorization received. You can close this window.";
                    let response = format!(
                        "HTTP/1.1 200 OK\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Length: {}\r\n\r\n{}",
                        body.len(),
                        body
                    );
                    let _ = stream.write_all(response.as_bytes());
                    let _ = stream.flush();
                    break;
                } else {
                    let body = "Authorization code missing.";
                    let response = format!(
                        "HTTP/1.1 400 Bad Request\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Length: {}\r\n\r\n{}",
                        body.len(),
                        body
                    );
                    let _ = stream.write_all(response.as_bytes());
                    let _ = stream.flush();
                }
            }
            Err(err) => {
                eprintln!("Callback server error: {}", err);
                break;
            }
        }
    }
}

fn read_auth_code_from_stream(stream: &mut TcpStream) -> Option<String> {
    let mut buffer = [0u8; 4096];
    let read = stream.read(&mut buffer).ok()?;
    let request = String::from_utf8_lossy(&buffer[..read]);
    let first_line = request.lines().next()?;
    let path = first_line.split_whitespace().nth(1)?;
    let query = path.split('?').nth(1)?;
    form_urlencoded::parse(query.as_bytes())
        .find_map(|(key, value)| (key == "code").then(|| value.into_owned()))
}

fn exchange_code_for_token(
    client: &Client,
    config: &OAuthClientConfig,
    auth_code: &str,
    redirect_uri: &str,
) -> Result<StoredToken> {
    let mut form = vec![
        ("grant_type", "authorization_code".to_string()),
        ("code", auth_code.to_string()),
        ("client_id", config.client_id.clone()),
        ("redirect_uri", redirect_uri.to_string()),
    ];

    if let Some(secret) = &config.client_secret {
        form.push(("client_secret", secret.clone()));
    }

    let response = client
        .post(&config.token_uri)
        .form(&form)
        .send()
        .and_then(|resp| resp.error_for_status())?
        .json::<TokenResponse>()
        .context("unable to retrieve token from web")?;

    Ok(to_stored_token(response))
}

fn refresh_token(
    client: &Client,
    config: &OAuthClientConfig,
    token: &StoredToken,
) -> Result<StoredToken> {
    let refresh_token = token
        .refresh_token
        .as_ref()
        .ok_or_else(|| anyhow!("missing refresh token"))?;
    let mut form = vec![
        ("grant_type", "refresh_token".to_string()),
        ("refresh_token", refresh_token.clone()),
        ("client_id", config.client_id.clone()),
    ];

    if let Some(secret) = &config.client_secret {
        form.push(("client_secret", secret.clone()));
    }

    let response = client
        .post(&config.token_uri)
        .form(&form)
        .send()
        .and_then(|resp| resp.error_for_status())?
        .json::<TokenResponse>()
        .context("unable to refresh access token")?;

    let mut refreshed = to_stored_token(response);
    refreshed.refresh_token = token.refresh_token.clone();
    Ok(refreshed)
}

fn to_stored_token(response: TokenResponse) -> StoredToken {
    StoredToken {
        access_token: response.access_token,
        token_type: response.token_type,
        refresh_token: response.refresh_token,
        expiry: response
            .expires_in
            .and_then(|seconds| Utc::now().checked_add_signed(chrono::Duration::seconds(seconds))),
        scope: response.scope,
    }
}

fn load_token(path: &Path) -> Result<StoredToken> {
    let file = File::open(path).with_context(|| format!("unable to open {}", path.display()))?;
    Ok(serde_json::from_reader(file).context("unable to decode token.json")?)
}

fn remaining_timeout(deadline: Instant) -> Result<Duration> {
    let remaining = deadline.saturating_duration_since(Instant::now());
    if remaining.is_zero() {
        Err(anyhow!("operation timed out"))
    } else {
        Ok(remaining)
    }
}

fn save_token(path: &Path, token: &StoredToken) -> Result<()> {
    println!("Saving credential file to: {}", path.display());
    let file = OpenOptions::new()
        .create(true)
        .write(true)
        .truncate(true)
        .open(path)
        .with_context(|| format!("unable to cache oauth token at {}", path.display()))?;
    serde_json::to_writer_pretty(file, token).context("unable to write token.json")?;
    Ok(())
}

impl StoredToken {
    fn is_expired(&self) -> bool {
        match self.expiry {
            Some(expiry) => Utc::now() >= expiry,
            None => false,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn internal_date_to_date_formats_local_date() {
        let ts = 1_577_936_445_000i64;
        let expected = Local
            .timestamp_millis_opt(ts)
            .single()
            .unwrap()
            .format(DEFAULT_CUTOFF_DATE_FORMAT)
            .to_string();
        assert_eq!(internal_date_to_date(ts), expected);
        assert_eq!(internal_date_to_date(0), "");
    }

    #[test]
    fn summary_sorts_and_totals() {
        let mut counts = HashMap::new();
        counts.insert("2024-01-03".to_string(), 2);
        counts.insert("2024-01-01".to_string(), 1);
        counts.insert("2024-01-05".to_string(), 4);
        print_spam_summary(&counts, "2024-01-02").unwrap();
    }
}
