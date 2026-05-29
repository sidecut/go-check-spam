mod auth;

use anyhow::{Result, anyhow};
use auth::{Token, ensure_valid_token, get_token, load_credentials};
use clap::Parser;
use futures::stream::StreamExt;
use serde::Deserialize;
use std::collections::HashMap;
use std::io::Write;

#[derive(Parser, Debug)]
#[command(name = "rcheckspam", about = "Checks spam emails in Gmail")]
struct Args {
    #[arg(long, default_value_t = 60, help = "timeout in seconds")]
    timeout: u64,

    #[arg(
        long,
        default_value_t = 1000,
        help = "max initial delay in milliseconds before starting to fetch messages"
    )]
    initial_delay: u64,

    #[arg(long, default_value_t = 30, help = "number of days to look back")]
    days: i64,

    #[arg(long, help = "enable debug output")]
    debug: bool,

    #[arg(
        long,
        default_value_t = 8,
        help = "number of concurrent workers fetching messages"
    )]
    concurrency: usize,

    #[arg(
        long,
        default_value_t = 8080,
        help = "port for local OAuth callback server"
    )]
    oauth_port: u16,
}

#[derive(Deserialize, Debug)]
struct MessageListResponse {
    messages: Option<Vec<GmailMessageHeader>>,
    #[serde(rename = "nextPageToken")]
    next_page_token: Option<String>,
}

#[derive(Deserialize, Debug, Clone)]
struct GmailMessageHeader {
    id: String,
}

#[derive(Deserialize, Debug)]
struct MessageDetailResponse {
    #[serde(rename = "internalDate")]
    internal_date: String,
}

#[tokio::main]
async fn main() {
    let args = Args::parse();

    let timeout_duration = std::time::Duration::from_secs(args.timeout);
    match tokio::time::timeout(timeout_duration, run(args)).await {
        Ok(Ok(())) => {}
        Ok(Err(e)) => {
            eprintln!("Error: {}", e);
            std::process::exit(1);
        }
        Err(_) => {
            eprintln!(
                "Error: process timed out after {} seconds",
                timeout_duration.as_secs()
            );
            std::process::exit(1);
        }
    }
}

async fn run(args: Args) -> Result<()> {
    let cutoff_date = (chrono::Local::now() - chrono::Duration::days(args.days))
        .format("%Y-%m-%d")
        .to_string();

    let creds = load_credentials("credentials.json")?;
    let mut token = get_token(&creds, args.oauth_port).await?;

    // Perform list and fetch in a sub-method
    let spam_counts = get_spam_counts(&args, &mut token, &creds, &cutoff_date).await?;

    println!(
        "Spam email counts for the past {} days (based on internalDate):",
        args.days
    );
    print_spam_summary(&spam_counts, &cutoff_date);

    Ok(())
}

async fn get_spam_counts(
    args: &Args,
    token: &mut Token,
    creds: &auth::CredentialsConfig,
    cutoff_date: &str,
) -> Result<HashMap<String, usize>> {
    let client = reqwest::Client::new();

    // Ensure token is valid before starting listing
    ensure_valid_token(token, creds, "token.json").await?;

    let mut message_ids = Vec::new();
    let mut page_token: Option<String> = None;
    let query = format!("after:{}", cutoff_date);
    println!("Gmail query: {}", query);

    loop {
        // Build the request URL
        let mut url =
            reqwest::Url::parse("https://gmail.googleapis.com/gmail/v1/users/me/messages")?;
        {
            let mut query_params = url.query_pairs_mut();
            query_params.append_pair("labelIds", "SPAM");
            query_params.append_pair("q", &query);
            if let Some(ref t) = page_token {
                query_params.append_pair("pageToken", t);
            }
        }

        let url_str = url.to_string();
        let access_token = token.access_token.clone();
        let debug = args.debug;
        let list_resp = retry_with_backoff(debug, || {
            let client = &client;
            let url_str = &url_str;
            let access_token = &access_token;
            async move {
                client
                    .get(url_str)
                    .header(
                        reqwest::header::AUTHORIZATION,
                        format!("Bearer {}", access_token),
                    )
                    .send()
                    .await?
                    .error_for_status()?
                    .json::<MessageListResponse>()
                    .await
            }
        })
        .await?;

        if let Some(msgs) = list_resp.messages {
            for m in msgs {
                message_ids.push(m.id);
            }
        }

        page_token = list_resp.next_page_token;
        if page_token.is_none() {
            break;
        }
    }

    if message_ids.is_empty() {
        println!("No spam messages found.");
        return Ok(HashMap::new());
    }

    let mut daily_counts = HashMap::new();
    let access_token = token.access_token.clone();

    let mut stream = futures::stream::iter(message_ids)
        .map(|msg_id| {
            let client = client.clone();
            let access_token = access_token.clone();
            let initial_delay = args.initial_delay;
            let debug = args.debug;
            async move {
                // Delay a random interval between 0 and initial_delay to avoid rate limits
                if initial_delay > 0 {
                    use rand::Rng;
                    let delay_ms = rand::thread_rng().gen_range(0..initial_delay);
                    tokio::time::sleep(std::time::Duration::from_millis(delay_ms)).await;
                }

                let url = format!(
                    "https://gmail.googleapis.com/gmail/v1/users/me/messages/{}?format=minimal",
                    msg_id
                );

                let res = retry_with_backoff(debug, || {
                    let client = &client;
                    let url = &url;
                    let access_token = &access_token;
                    async move {
                        client
                            .get(url)
                            .header(
                                reqwest::header::AUTHORIZATION,
                                format!("Bearer {}", access_token),
                            )
                            .send()
                            .await?
                            .error_for_status()?
                            .json::<MessageDetailResponse>()
                            .await
                    }
                })
                .await;

                match res {
                    Ok(detail) => Some(detail.internal_date),
                    Err(e) => {
                        if debug {
                            eprintln!("Warning: Failed to fetch message {}: {}", msg_id, e);
                        }
                        None
                    }
                }
            }
        })
        .buffer_unordered(args.concurrency);

    let mut fetched_count = 0;
    while let Some(maybe_date_ms) = stream.next().await {
        fetched_count += 1;
        print!("\r{}", fetched_count);
        let _ = std::io::stdout().flush();

        if let Some(date_ms) = maybe_date_ms {
            let date_str = internal_date_to_date(&date_ms);
            if !date_str.is_empty() {
                *daily_counts.entry(date_str).or_insert(0) += 1;
            }
        }
    }
    print!("\r"); // Erase the progress count
    let _ = std::io::stdout().flush();

    Ok(daily_counts)
}

async fn retry_with_backoff<F, Fut, T, E>(debug: bool, op: F) -> Result<T, anyhow::Error>
where
    F: Fn() -> Fut,
    Fut: std::future::Future<Output = Result<T, E>>,
    E: std::fmt::Display,
{
    let mut wait = std::time::Duration::from_millis(300);
    let max_attempts = 8;
    for i in 0..max_attempts {
        match op().await {
            Ok(val) => return Ok(val),
            Err(err) => {
                if i == max_attempts - 1 {
                    return Err(anyhow!("Retry attempts exhausted: {}", err));
                }
                if debug {
                    eprintln!(
                        "Attempt {} failed: {}. Retrying in {:?}...",
                        i + 1,
                        err,
                        wait
                    );
                }
                use rand::Rng;
                let jitter = std::time::Duration::from_millis(rand::thread_rng().gen_range(0..200));
                tokio::time::sleep(wait + jitter).await;
                wait *= 2;
                if wait > std::time::Duration::from_secs(10) {
                    wait = std::time::Duration::from_secs(10);
                }
            }
        }
    }
    Err(anyhow!("Retry attempts exhausted"))
}

fn internal_date_to_date(ms_str: &str) -> String {
    if let Ok(ms) = ms_str.parse::<i64>() {
        if ms <= 0 {
            return String::new();
        }
        use chrono::TimeZone;
        if let chrono::LocalResult::Single(local_dt) = chrono::Local.timestamp_millis_opt(ms) {
            local_dt.format("%Y-%m-%d").to_string()
        } else {
            String::new()
        }
    } else {
        String::new()
    }
}

fn print_spam_summary(spam_counts: &HashMap<String, usize>, cutoff_date: &str) {
    let mut dates: Vec<&String> = spam_counts.keys().collect();
    dates.sort();

    #[derive(PartialEq)]
    enum OutputState {
        FirstLine,
        BeforeDate,
        OnOrAfterDate,
    }

    let mut total = 0;
    let mut output_state = OutputState::FirstLine;
    for date in dates {
        if date < &cutoff_date.to_string() {
            output_state = OutputState::BeforeDate;
        } else {
            if output_state == OutputState::BeforeDate {
                println!();
            }
            output_state = OutputState::OnOrAfterDate;
        }

        let count = spam_counts[date];
        total += count;

        if let Ok(naive_date) = chrono::NaiveDate::parse_from_str(date, "%Y-%m-%d") {
            let day_of_week = naive_date.format("%a").to_string();
            println!("{} {} {}", day_of_week, date, count);
        } else {
            eprintln!("Error parsing date: {}", date);
        }
    }
    println!("Total: {}", total);
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_internal_date_to_date() {
        let ts_ms = 1577936645000i64; // 2020-01-02 03:04:05 UTC
        let got = internal_date_to_date(&ts_ms.to_string());

        use chrono::TimeZone;
        let expected = chrono::Local
            .timestamp_millis_opt(ts_ms)
            .unwrap()
            .format("%Y-%m-%d")
            .to_string();

        assert_eq!(got, expected);
        assert_eq!(internal_date_to_date("0"), "");
        assert_eq!(internal_date_to_date("-50"), "");
        assert_eq!(internal_date_to_date("not a number"), "");
    }
}
