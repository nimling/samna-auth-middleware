use std::time::Duration;

use http::StatusCode;
use serde::{Deserialize, Serialize};
use url::Url;

use super::models::{ClientError, ErrorKind};
use super::utils::join_url;
use crate::utils::cfg;

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct ResponseToken {
    #[serde(default)]
    pub access_token: String,
    #[serde(default)]
    pub refresh_token: String,
    #[serde(default, rename = "expires_in")]
    pub access_expires_in: i64,
    #[serde(default)]
    pub refresh_expires_in: i64,
    #[serde(default)]
    pub token_type: String,
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct OpenIdConfiguration {
    #[serde(default)]
    pub issuer: String,
    #[serde(default)]
    pub token_endpoint: String,
    #[serde(default)]
    pub jwks_uri: String,
}

pub fn get_login(provider: Option<&str>) -> Result<String, String> {
    let endpoint = cfg().endpoint;
    let path = match provider {
        Some(p) if !p.is_empty() => format!("api/auth/p/{p}/login"),
        _ => "api/auth/login".to_string(),
    };
    let u = join_url(&endpoint, &[&path]).map_err(|e| format!("build url: {e}"))?;
    let mut parsed = Url::parse(&u).map_err(|e| format!("parse url: {e}"))?;
    parsed
        .query_pairs_mut()
        .append_pair("client_slug", &cfg().app_slug);
    Ok(parsed.to_string())
}

pub async fn get_logout(access_token: &str) -> Result<u16, String> {
    let c = cfg();
    let u = join_url(&c.endpoint, &["sam/logout"]).map_err(|e| format!("build url: {e}"))?;
    let client = reqwest::Client::builder()
        .timeout(Duration::from_secs(3))
        .build()
        .map_err(|e| e.to_string())?;
    let resp = client
        .get(u)
        .header("SAuth-App-Slug", &c.app_slug)
        .header("SAuth-App-Secret", &c.app_secret)
        .header("Authorization", format!("Bearer {access_token}"))
        .send()
        .await
        .map_err(|e| format!("send request: {e}"))?;
    Ok(resp.status().as_u16())
}

pub async fn get_openid_configuration(
    config_url: &str,
) -> Result<OpenIdConfiguration, ClientError> {
    let client = reqwest::Client::builder()
        .timeout(Duration::from_secs(3))
        .build()
        .map_err(|e| ClientError::other(e.to_string()))?;
    let resp = client
        .get(config_url)
        .send()
        .await
        .map_err(|e| ClientError::other(format!("send request: {e}")))?;
    if resp.status() == StatusCode::NOT_FOUND {
        return Err(ClientError::not_found());
    }
    if resp.status() != StatusCode::OK {
        return Err(ClientError::other(format!(
            "openid configuration request to {config_url} returned status {}, check that the client slug exists",
            resp.status().as_u16()
        )));
    }
    let content_type = resp
        .headers()
        .get("Content-Type")
        .and_then(|h| h.to_str().ok())
        .unwrap_or("")
        .to_string();
    if !content_type.contains("application/json") {
        let body = resp
            .bytes()
            .await
            .map(|b| String::from_utf8_lossy(&b[..b.len().min(200)]).to_string())
            .unwrap_or_default();
        return Err(ClientError::other(format!(
            "openid configuration at {config_url} returned {content_type} instead of JSON: {body}"
        )));
    }
    resp.json::<OpenIdConfiguration>().await.map_err(|e| {
        ClientError::other(format!(
            "openid configuration at {config_url} returned invalid JSON: {e}"
        ))
    })
}

#[derive(Clone, Debug, Default, Deserialize)]
pub struct TokenError {
    #[serde(skip)]
    pub status: u16,
    #[serde(default, rename = "error")]
    pub code: String,
    #[serde(default, rename = "error_description")]
    pub description: String,
}

impl std::fmt::Display for TokenError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        if !self.description.is_empty() {
            return write!(
                f,
                "auth server returned {} {}: {}",
                self.status, self.code, self.description
            );
        }
        write!(f, "auth server returned {} {}", self.status, self.code)
    }
}

impl std::error::Error for TokenError {}

#[derive(Debug)]
pub enum AuthError {
    Token(TokenError),
    Other(String),
}

impl AuthError {
    pub fn is_unauthorized(&self) -> bool {
        matches!(self, AuthError::Token(t) if t.status == StatusCode::UNAUTHORIZED.as_u16())
    }

    pub fn kind(&self) -> ErrorKind {
        if self.is_unauthorized() {
            ErrorKind::Unauthorized
        } else {
            ErrorKind::Other
        }
    }
}

impl std::fmt::Display for AuthError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            AuthError::Token(t) => t.fmt(f),
            AuthError::Other(s) => f.write_str(s),
        }
    }
}

impl std::error::Error for AuthError {}

async fn post_auth(
    target: &str,
    slug: &str,
    secret: &str,
    form: &[(&str, &str)],
) -> Result<Vec<u8>, AuthError> {
    let client = reqwest::Client::builder()
        .timeout(Duration::from_secs(3))
        .build()
        .map_err(|e| AuthError::Other(e.to_string()))?;
    let mut req = client
        .post(target)
        .header("Accept", "application/json")
        .form(form);
    if !slug.is_empty() {
        req = req
            .header("SAuth-App-Slug", slug)
            .header("SAuth-App-Secret", secret);
    }
    let resp = req
        .send()
        .await
        .map_err(|e| AuthError::Other(format!("send request: {e}")))?;
    let status = resp.status();
    let raw = resp
        .bytes()
        .await
        .map_err(|e| AuthError::Other(format!("read response: {e}")))?;
    if status != StatusCode::OK {
        let mut token_err: TokenError = serde_json::from_slice(&raw).unwrap_or_default();
        token_err.status = status.as_u16();
        if token_err.code.is_empty() {
            token_err.code = status.canonical_reason().unwrap_or("").to_string();
        }
        return Err(AuthError::Token(token_err));
    }
    Ok(raw.to_vec())
}

async fn post_token(
    token_url: &str,
    slug: &str,
    secret: &str,
    form: &[(&str, &str)],
) -> Result<ResponseToken, AuthError> {
    let raw = post_auth(token_url, slug, secret, form).await?;
    serde_json::from_slice(&raw).map_err(|e| AuthError::Other(format!("decode response: {e}")))
}

pub async fn post_auth_code(
    token_url: &str,
    slug: &str,
    secret: &str,
    code: &str,
    redirect_uri: &str,
    code_verifier: &str,
) -> Result<ResponseToken, AuthError> {
    post_token(
        token_url,
        slug,
        secret,
        &[
            ("grant_type", "authorization_code"),
            ("code", code),
            ("redirect_uri", redirect_uri),
            ("code_verifier", code_verifier),
        ],
    )
    .await
}

pub async fn post_refresh_token(
    token_url: &str,
    slug: &str,
    secret: &str,
    refresh_token: &str,
) -> Result<ResponseToken, AuthError> {
    post_token(
        token_url,
        slug,
        secret,
        &[
            ("grant_type", "refresh_token"),
            ("refresh_token", refresh_token),
        ],
    )
    .await
}

pub async fn post_client_credentials(
    token_url: &str,
    client_id: &str,
    client_secret: &str,
) -> Result<ResponseToken, AuthError> {
    post_token(
        token_url,
        "",
        "",
        &[
            ("grant_type", "client_credentials"),
            ("client_id", client_id),
            ("client_secret", client_secret),
        ],
    )
    .await
}

pub async fn post_revoke(
    revoke_url: &str,
    slug: &str,
    secret: &str,
    refresh_token: &str,
) -> Result<(), AuthError> {
    post_auth(
        revoke_url,
        slug,
        secret,
        &[
            ("token", refresh_token),
            ("token_type_hint", "refresh_token"),
        ],
    )
    .await
    .map(|_| ())
}
