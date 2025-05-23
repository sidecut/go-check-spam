use crate::errors::AppError;
use hyper::client::HttpConnector;
use hyper_rustls::HttpsConnector;
use yup_oauth2::authenticator::Authenticator;
use yup_oauth2::{InstalledFlowAuthenticator, InstalledFlowReturnMethod};
use std::path::Path;

const TOKEN_CACHE_FILE: &str = "token.json";

pub async fn authenticate(
    credentials_path: &str,
) -> Result<Authenticator<HttpsConnector<HttpConnector>>, AppError> {
    let secret = yup_oauth2::read_application_secret(Path::new(credentials_path))
        .await
        .map_err(|e| AppError::CredentialsError(format!("Failed to read client secret: {}", e)))?;

    let auth = InstalledFlowAuthenticator::builder(
        secret,
        InstalledFlowReturnMethod::HTTPRedirect, // Or LoopbackAddressRedirect if preferred and supported
    )
    .persist_tokens_to_disk(Path::new(TOKEN_CACHE_FILE))
    .build()
    .await
    .map_err(|e| AppError::AuthFailed(format!("Failed to build authenticator: {}", e)))?;

    // The scope for reading Gmail messages
    let scopes = &["https://www.googleapis.com/auth/gmail.readonly"];
    
    // Attempt to get a token, this will trigger the auth flow if needed
    // The Authenticator itself handles token acquisition and refresh.
    // We don't need to explicitly call a method to get a token here,
    // as it will be done when the first API call is made.
    // However, to ensure the auth flow completes before other operations,
    // we can try to force a token retrieval or check.
    // Forcing a token request to ensure auth flow completes if necessary.
    match auth.token(scopes).await {
        Ok(_) => Ok(auth),
        Err(e) => Err(AppError::AuthFailed(format!("Failed to get token: {}", e))),
    }
}