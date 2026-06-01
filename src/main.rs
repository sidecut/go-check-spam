mod auth;
mod gmail;

use anyhow::{Context, Result};
use chrono::NaiveDate;
use clap::Parser;
use std::collections::HashMap;
use std::time::Duration;

/// Count messages in the Gmail Spam label by local date (based on internalDate).
#[derive(Parser, Debug, Clone)]
#[command(name = "gocheckspam")]
struct Args {
    /// Timeout in seconds for listing/fetching messages.
    #[arg(long, default_value_t = 60)]
    timeout: u64,

    /// Max initial delay in milliseconds before fetching each message.
    #[arg(long = "initial-delay", default_value_t = 1000)]
    initial_delay: u32,

    /// Number of days to look back.
    #[arg(long, default_value_t = 30)]
    days: i64,

    /// Enable debug output.
    #[arg(long, default_value_t = false)]
    debug: bool,

    /// Number of concurrent workers fetching messages.
    #[arg(long, default_value_t = 8)]
    concurrency: usize,

    /// Port for the local OAuth callback server.
    #[arg(long = "oauth-port", default_value_t = 8080)]
    oauth_port: u16,
}

/// Runtime configuration shared with the Gmail client.
#[derive(Debug, Clone)]
pub struct Config {
    pub initial_delay: u32,
    pub debug: bool,
    pub concurrency: usize,
    pub cutoff_date: String,
}

#[tokio::main]
async fn main() -> Result<()> {
    let args = Args::parse();

    let cutoff_date = (chrono::Local::now() - chrono::Duration::days(args.days))
        .format("%Y-%m-%d")
        .to_string();

    let cfg = Config {
        initial_delay: args.initial_delay,
        debug: args.debug,
        concurrency: args.concurrency,
        cutoff_date: cutoff_date.clone(),
    };

    let access_token = auth::get_access_token("credentials.json", args.oauth_port)
        .await
        .context("authentication failed")?;

    let client = reqwest::Client::new();

    let spam_counts = tokio::time::timeout(
        Duration::from_secs(args.timeout),
        gmail::get_spam_counts(&client, &access_token, &cfg),
    )
    .await
    .context("timed out listing/fetching spam messages")?
    .context("error getting spam counts")?;

    println!(
        "Spam email counts for the past {} days (based on internalDate):",
        args.days
    );
    print_spam_summary(&spam_counts, &cutoff_date);

    Ok(())
}

fn print_spam_summary(spam_counts: &HashMap<String, i64>, cutoff_date: &str) {
    let mut dates: Vec<&String> = spam_counts.keys().collect();
    dates.sort();

    let mut total = 0i64;
    let mut printed_before_date = false;

    for date in dates {
        if date.as_str() < cutoff_date {
            printed_before_date = true;
        } else {
            // Separate the "before cutoff" and "on/after cutoff" sections.
            if printed_before_date {
                println!();
                printed_before_date = false;
            }
        }

        let count = spam_counts[date];
        total += count;

        match NaiveDate::parse_from_str(date, "%Y-%m-%d") {
            Ok(parsed) => {
                let day_of_week = parsed.format("%a");
                println!("{day_of_week} {date} {count}");
            }
            Err(e) => {
                eprintln!("Error parsing date: {e}");
            }
        }
    }

    println!("Total: {total}");
}

#[cfg(test)]
mod tests {
    use crate::gmail::internal_date_to_date;

    #[test]
    fn test_internal_date_to_date() {
        // 2020-01-02 03:04:05 UTC in milliseconds.
        let ts: i64 = 1577936645000;
        let got = internal_date_to_date(ts);
        let expected = chrono::DateTime::from_timestamp_millis(ts)
            .unwrap()
            .with_timezone(&chrono::Local)
            .format("%Y-%m-%d")
            .to_string();
        assert_eq!(got, expected);

        assert_eq!(internal_date_to_date(0), "");
    }
}
