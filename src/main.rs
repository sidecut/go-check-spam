mod auth;
mod errors;

use anyhow::Result;
use chrono::{DateTime, Days, Local, TimeZone, Utc};
use clap::Parser;
use errors::AppError;
use futures::stream::{self, StreamExt};
use google_gmail1::{api::Message, Gmail};
use hyper::client::HttpConnector;
use hyper_rustls::HttpsConnector;
use log::{debug, error, info, warn};
use std::collections::HashMap;
use std::time::Duration;
use tokio::sync::mpsc;
use yup_oauth2::authenticator::Authenticator;

#[derive(Parser, Debug)]
#[clap(author, version, about, long_about = None)]
struct Args {
    #[clap(short, long, value_parser, default_value_t = 60)]
    timeout: u64,

    #[clap(short, long, value_parser, default_value_t = 30)]
    days: u64,

    #[clap(short, long, action)]
    debug: bool,
}

type GmailHub = Gmail<HttpsConnector<HttpConnector>>;

async fn list_spam_messages(
    hub: &GmailHub,
    cutoff_date_str: &str,
    timeout_seconds: u64,
    debug_enabled: bool,
) -> Result<Vec<Message>, AppError> {
    let mut messages = Vec::new();
    let mut page_token: Option<String> = None;
    let query = format!("after:{}", cutoff_date_str);
    info!("Gmail query: {}", query);

    let (tx, mut rx) = mpsc::channel::<Message>(200); // Buffer size for messages
    let mut total_fetched_metadata = 0;

    // Use a timeout for the entire message listing and fetching operation
    let operation_timeout = Duration::from_secs(timeout_seconds);
    tokio::time::timeout(operation_timeout, async {
        loop {
            let mut request = hub
                .users()
                .messages_list("me")
                .q(&query)
                .add_label_ids("SPAM");
            if let Some(pt) = &page_token {
                request = request.page_token(pt);
            }

            let op = backoff::ExponentialBackoff::default();
            let response = backoff::future::retry(op, || async {
                request
                    .clone() // Clone request as it's consumed by .doit()
                    .doit()
                    .await
                    .map_err(|e| {
                        if e.is_ আচ্ছা() || e.is_redirect() || e.is_ информаশনাল() {
                            backoff::Error::transient(e)
                        } else {
                            backoff::Error::permanent(e)
                        }
                    })
            })
            .await?;
            
            let current_messages = response.1.messages.unwrap_or_default();
            if current_messages.is_empty() && page_token.is_none() {
                 // No messages at all on the first page
                break;
            }
            
            total_fetched_metadata += current_messages.len();
            if debug_enabled {
                print!("\rFetched metadata for {} messages...", total_fetched_metadata);
            }


            let mut fetch_tasks = Vec::new();

            for msg_meta in current_messages {
                if let Some(id) = msg_meta.id {
                    let hub_clone = hub.clone();
                    let tx_clone = tx.clone();
                    if debug_enabled {
                        debug!("Spawning task to fetch message ID: {}", id);
                    }
                    fetch_tasks.push(tokio::spawn(async move {
                        let op = backoff::ExponentialBackoff::default();
                        let full_msg_result = backoff::future::retry(op, || async {
                            hub_clone
                                .users()
                                .messages_get("me", &id)
                                .format("minimal") // Only need InternalDate
                                .doit()
                                .await
                                .map(|res| res.1)
                                .map_err(|e| {
                                    if debug_enabled {
                                        warn!("Retrying message {}: {:?}", id, e);
                                    }
                                    if e.is_ আচ্ছা() || e.is_redirect() || e.is_ информаশনাল() { // Simplified retry logic
                                        backoff::Error::transient(e)
                                    } else {
                                        backoff::Error::permanent(e)
                                    }
                                })
                        }).await;

                        match full_msg_result {
                            Ok(full_msg) => {
                                if tx_clone.send(full_msg).await.is_err() && debug_enabled {
                                    warn!("Receiver dropped for message ID {}", id);
                                }
                            }
                            Err(e) => {
                                if debug_enabled {
                                    error!("Error fetching message {}: {:?}", id, e);
                                }
                            }
                        }
                    }));
                }
            }
            
            // Wait for this batch of fetch tasks to complete
            for task in fetch_tasks {
                let _ = task.await; // Handle potential join errors if necessary
            }

            page_token = response.1.next_page_token;
            if page_token.is_none() {
                break;
            }
        }
        Ok::<(), AppError>(()) // Indicate success for the outer timeout block
    }).await??; // First ? for timeout error, second for AppError from the block

    if debug_enabled {
        println!(); // Newline after progress indicator
    }
    drop(tx); // Close the sender to signal completion

    while let Some(msg) = rx.recv().await {
        messages.push(msg);
    }
    
    if messages.is_empty() && total_fetched_metadata == 0 {
         return Err(AppError::NoSpamMessages);
    }

    Ok(messages)
}

async fn get_spam_counts(
    hub: &GmailHub,
    cutoff_date_str: &str,
    timeout_seconds: u64,
    debug_enabled: bool,
) -> Result<HashMap<String, i32>, AppError> {
    let mut daily_counts: HashMap<String, i32> = HashMap::new();

    let messages = list_spam_messages(hub, cutoff_date_str, timeout_seconds, debug_enabled).await?;

    if messages.is_empty() {
        info!("No spam messages found after filtering.");
        return Ok(daily_counts);
    }

    for m in messages {
        if let Some(internal_date_ms_str) = m.internal_date {
            if let Ok(internal_date_ms) = internal_date_ms_str.parse::<i64>() {
                if internal_date_ms <= 0 {
                    if debug_enabled {
                        warn!(
                            "Warning: Invalid internalDate ({}) for message ID {:?}",
                            internal_date_ms, m.id
                        );
                    }
                    continue;
                }
                // Gmail internalDate is epoch milliseconds in UTC.
                let email_time_utc = Utc.timestamp_millis_opt(internal_date_ms).single();
                if let Some(utc_dt) = email_time_utc {
                    // Convert to local timezone for date formatting
                    let email_time_local: DateTime<Local> = DateTime::from(utc_dt);
                    let email_date_str = email_time_local.format("%Y-%m-%d").to_string();
                    *daily_counts.entry(email_date_str).or_insert(0) += 1;
                } else if debug_enabled {
                     warn!("Warning: Could not parse internalDate ({}) for message ID {:?}", internal_date_ms, m.id);
                }
            } else if debug_enabled {
                 warn!("Warning: Could not parse internalDate string ({:?}) for message ID {:?}", internal_date_ms_str, m.id);
            }
        }
    }
    Ok(daily_counts)
}

#[derive(PartialEq, Debug)]
enum OutputState {
    FirstLine,
    BeforeDate,
    OnOrAfterDate,
}

fn print_spam_summary(spam_counts: &HashMap<String, i32>, cutoff_date_str: &str) -> Result<(), AppError> {
    if spam_counts.is_empty() {
        println!("No spam messages to summarize.");
        return Ok(());
    }

    let mut dates: Vec<String> = spam_counts.keys().cloned().collect();
    dates.sort();

    let mut total = 0;
    let mut output_state = OutputState::FirstLine;

    for date_str in dates {
        let current_state = if date_str < cutoff_date_str {
            OutputState::BeforeDate
        } else {
            OutputState::OnOrAfterDate
        };

        if output_state == OutputState::BeforeDate && current_state == OutputState::OnOrAfterDate {
            println!(); // Print a blank line to separate sections
        }
        output_state = current_state;

        let count = spam_counts[&date_str];
        total += count;

        // Parse date string to get day of the week
        // Assuming date_str is "YYYY-MM-DD"
        let date_value = Local
            .datetime_from_str(&format!("{} 00:00:00", date_str), "%Y-%m-%d %H:%M:%S")
            .map_err(|e| AppError::DateParse(e))?; // Or use NaiveDate::parse_from_str

        let day_of_week = date_value.format("%a"); // Mon, Tue, etc.
        println!("{} {} {}", day_of_week, date_str, count);
    }
    println!("Total: {}", total);
    Ok(())
}

#[tokio::main]
async fn main() -> Result<()> {
    let args = Args::parse();

    if args.debug {
        std::env::set_var("RUST_LOG", "debug");
    } else {
        std::env::set_var("RUST_LOG", "info");
    }
    env_logger::init();

    let cutoff_date = Utc::now() - Days::new(args.days);
    let cutoff_date_str = cutoff_date.format("%Y-%m-%d").to_string();

    info!(
        "Attempting to authenticate and fetch spam for the past {} days.",
        args.days
    );
    info!("Credentials expected at: credentials.json");
    info!("Token cache will be at: token.json");

    let authenticator: Authenticator<HttpsConnector<HttpConnector>> =
        auth::authenticate("credentials.json").await?;

    let hub = Gmail::new(
        hyper::Client::builder().build(hyper_rustls::HttpsConnectorBuilder::new()
            .with_native_roots()
            .https_or_http()
            .enable_http1()
            .build()),
        authenticator,
    );

    match get_spam_counts(&hub, &cutoff_date_str, args.timeout, args.debug).await {
        Ok(spam_counts) => {
            if spam_counts.is_empty() && !args.debug {
                 println!("No spam messages found for the past {} days (based on internalDate).", args.days);
            } else {
                println!(
                    "Spam email counts for the past {} days (based on internalDate, local timezone):",
                    args.days
                );
                print_spam_summary(&spam_counts, &cutoff_date_str)?;
            }
        }
        Err(AppError::NoSpamMessages) => {
             println!("No spam messages found for the past {} days (based on internalDate).", args.days);
        }
        Err(e) => {
            error!("Error getting spam counts: {}", e);
            return Err(e.into());
        }
    }

    Ok(())
}