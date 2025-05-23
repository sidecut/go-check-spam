use thiserror::Error;

#[derive(Error, Debug)]
pub enum AppError {
    #[error("I/O error: {0}")]
    Io(#[from] std::io::Error),

    #[error("JSON error: {0}")]
    Json(#[from] serde_json::Error),

    #[error("OAuth2 error: {0}")]
    OAuth2(String), // yup_oauth2::Error is complex, stringify for simplicity

    #[error("Gmail API error: {0}")]
    GmailApi(#[from] google_gmail1::Error),

    #[error("HTTP error: {0}")]
    Http(#[from] hyper::Error),

    #[error("Timeout error: {0}")]
    Timeout(#[from] tokio::time::error::Elapsed),

    #[error("Date parsing error: {0}")]
    DateParse(#[from] chrono::ParseError),

    #[error("Authentication failed: {0}")]
    AuthFailed(String),

    #[error("Failed to read credentials: {0}")]
    CredentialsError(String),

    #[error("No spam messages found")]
    NoSpamMessages,

    #[error("Error fetching messages: {0}")]
    MessageFetchError(String),

    #[error("Other error: {0}")]
    Other(String),
}

// Convert yup_oauth2::Error to AppError::OAuth2
impl From<yup_oauth2::Error> for AppError {
    fn from(err: yup_oauth2::Error) -> Self {
        AppError::OAuth2(err.to_string())
    }
}