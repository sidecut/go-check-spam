use anyhow::{anyhow, Result};
use futures::stream::{self, StreamExt};
use serde::Deserialize;
use std::collections::HashMap;
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::Arc;
use std::time::Duration;

use crate::Config;

const BASE_URL: &str = "https://gmail.googleapis.com/gmail/v1/users/me/messages";
/// Gmail allows up to 500 message ids per list page.
const LIST_PAGE_SIZE: u32 = 500;

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
struct ListMessagesResponse {
    #[serde(default)]
    messages: Vec<MessageRef>,
    #[serde(default)]
    next_page_token: Option<String>,
}

#[derive(Debug, Deserialize)]
struct MessageRef {
    id: String,
}

#[derive(Debug, Deserialize)]
struct Message {
    #[serde(default)]
    id: String,
    /// internalDate is a string containing milliseconds since the epoch.
    #[serde(default, rename = "internalDate")]
    internal_date: Option<String>,
}

/// List spam messages received on/after the cutoff date and return a map of
/// `YYYY-MM-DD` -> count, keyed by each message's local-time internalDate.
pub async fn get_spam_counts(
    client: &reqwest::Client,
    access_token: &str,
    cfg: &Config,
) -> Result<HashMap<String, i64>> {
    let query = format!("after:{}", cfg.cutoff_date);
    println!("Gmail query: {query}");

    // Collect every message id first (the list endpoint is paginated), then
    // fetch full messages with bounded concurrency.
    let mut ids: Vec<String> = Vec::new();
    let mut page_token: Option<String> = None;

    loop {
        let mut req = client
            .get(BASE_URL)
            .bearer_auth(access_token)
            .query(&[
                ("labelIds", "SPAM"),
                ("q", query.as_str()),
                ("maxResults", &LIST_PAGE_SIZE.to_string()),
            ]);
        if let Some(token) = &page_token {
            req = req.query(&[("pageToken", token.as_str())]);
        }

        let resp: ListMessagesResponse = retry_with_backoff(cfg.debug, || {
            let req = req.try_clone().expect("request is cloneable");
            async move {
                let r = req.send().await?.error_for_status()?;
                Ok(r.json::<ListMessagesResponse>().await?)
            }
        })
        .await
        .map_err(|e| anyhow!("error fetching messages: {e}"))?;

        for m in resp.messages {
            ids.push(m.id);
        }

        match resp.next_page_token {
            Some(token) if !token.is_empty() => page_token = Some(token),
            _ => break,
        }
    }

    let total = ids.len();
    let max_workers = cfg.concurrency.max(1);
    let counts: Arc<tokio::sync::Mutex<HashMap<String, i64>>> =
        Arc::new(tokio::sync::Mutex::new(HashMap::new()));
    let progress = Arc::new(AtomicUsize::new(0));

    stream::iter(ids)
        .for_each_concurrent(max_workers, |id| {
            let client = client.clone();
            let access_token = access_token.to_string();
            let counts = Arc::clone(&counts);
            let progress = Arc::clone(&progress);
            let cfg = cfg.clone();
            async move {
                let done = progress.fetch_add(1, Ordering::Relaxed) + 1;
                print!("\r{done}/{total}");
                use std::io::Write;
                let _ = std::io::stdout().flush();

                // Random delay to spread out requests and avoid rate limits.
                if cfg.initial_delay > 0 {
                    let jitter = rand::random_range(0..cfg.initial_delay);
                    tokio::time::sleep(Duration::from_millis(jitter as u64)).await;
                }

                let url = format!("{BASE_URL}/{id}");
                let result = retry_with_backoff(cfg.debug, || {
                    let client = client.clone();
                    let access_token = access_token.clone();
                    let url = url.clone();
                    async move {
                        let r = client
                            .get(&url)
                            .bearer_auth(&access_token)
                            .query(&[("format", "minimal")])
                            .send()
                            .await?
                            .error_for_status()?;
                        Ok(r.json::<Message>().await?)
                    }
                })
                .await;

                match result {
                    Ok(msg) => {
                        if let Some(date) = msg
                            .internal_date
                            .as_deref()
                            .and_then(|s| s.parse::<i64>().ok())
                            .map(internal_date_to_date)
                        {
                            if !date.is_empty() {
                                let mut guard = counts.lock().await;
                                *guard.entry(date).or_insert(0) += 1;
                            } else if cfg.debug {
                                eprintln!(
                                    "Warning: Invalid internalDate for message ID {}",
                                    msg.id
                                );
                            }
                        }
                    }
                    Err(e) => {
                        if cfg.debug {
                            eprintln!("Failed to fetch message {id}: {e}");
                        }
                    }
                }
            }
        })
        .await;

    print!("\r"); // erase the in-progress count
    use std::io::Write;
    let _ = std::io::stdout().flush();

    let counts = Arc::try_unwrap(counts)
        .map_err(|_| anyhow!("dangling references to counts map"))?
        .into_inner();

    if counts.is_empty() {
        println!("No spam messages found.");
    }

    Ok(counts)
}

/// Convert a Gmail internalDate (milliseconds since epoch) into a local-time
/// `YYYY-MM-DD` date string. Returns an empty string for invalid timestamps.
pub fn internal_date_to_date(ms: i64) -> String {
    if ms <= 0 {
        return String::new();
    }
    match chrono::DateTime::from_timestamp_millis(ms) {
        Some(dt) => dt
            .with_timezone(&chrono::Local)
            .format("%Y-%m-%d")
            .to_string(),
        None => String::new(),
    }
}

/// Retry an async operation with exponential backoff until it succeeds or the
/// maximum number of attempts is exhausted.
async fn retry_with_backoff<T, F, Fut>(debug: bool, mut op: F) -> Result<T>
where
    F: FnMut() -> Fut,
    Fut: std::future::Future<Output = Result<T>>,
{
    let mut wait = Duration::from_millis(300);
    let max_attempts = 8;
    let mut last_err: Option<anyhow::Error> = None;

    for attempt in 0..max_attempts {
        match op().await {
            Ok(value) => return Ok(value),
            Err(e) => {
                if debug {
                    eprintln!("Request error (attempt {}): {e}", attempt + 1);
                }
                if attempt == max_attempts - 1 {
                    return Err(e);
                }
                last_err = Some(e);
                let jitter = Duration::from_millis(rand::random_range(0..200));
                tokio::time::sleep(wait + jitter).await;
                wait = (wait * 2).min(Duration::from_secs(10));
            }
        }
    }

    Err(last_err.unwrap_or_else(|| anyhow!("retry attempts exhausted")))
}

#[cfg(test)]
mod tests {
    use super::ListMessagesResponse;

    #[test]
    fn list_response_deserializes_next_page_token() {
        let json = r#"{"messages":[{"id":"abc"}],"nextPageToken":"page-2"}"#;
        let resp: ListMessagesResponse = serde_json::from_str(json).unwrap();
        assert_eq!(resp.messages.len(), 1);
        assert_eq!(resp.next_page_token.as_deref(), Some("page-2"));
    }
}
