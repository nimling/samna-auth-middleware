use base64::Engine;
use http::{HeaderMap, Method};
use rand::RngCore;

use super::models::{
    ACCESS_TOKEN_COOKIE_KEY, AuthError, AuthMiddlewareConfig, CSRF_TOKEN_COOKIE_KEY,
    REFRESH_TOKEN_COOKIE_KEY,
};
use super::token::{Jwks, RequestFacts, claims_from_token_unsafe, validate_token};
use crate::client::{self, ResponseToken};
use crate::dpop;
use crate::shared::Ctx;
use crate::shared::db::get_origin_initiate_app;
use crate::utils::CookieScope;
use crate::utils::cookie::{
    CookieWrite, clear_cookie, cookie_value, cookie_values, write_cookie, write_public_cookie,
};

#[derive(Clone, Debug)]
pub struct CookieScopeKey(pub CookieScope);

pub struct Authenticated {
    pub user_id: uuid::Uuid,
    pub access_token: String,
}

pub async fn authenticate_cookie(
    req: &RequestFacts<'_>,
    set_cookies: &mut HeaderMap,
    scope: Option<&CookieScope>,
    config: &AuthMiddlewareConfig,
    jwks: &Jwks,
    replay: Option<&dpop::PostgresReplayStore>,
) -> Result<Authenticated, AuthError> {
    let access_values = cookie_values(req.headers, ACCESS_TOKEN_COOKIE_KEY);
    let refresh_values = cookie_values(req.headers, REFRESH_TOKEN_COOKIE_KEY);
    if access_values.is_empty() && refresh_values.is_empty() {
        return Err(AuthError::CredentialMissing);
    }
    enforce_csrf(req.method, req.headers)?;
    let mut access_err: Option<AuthError> = None;
    for value in &access_values {
        match validate_token(req, value, config, jwks, replay).await {
            Ok(user) => {
                return Ok(Authenticated {
                    user_id: user.id,
                    access_token: value.clone(),
                });
            }
            Err(e) => access_err = Some(e),
        }
    }
    if let Some(e) = &access_err
        && *e != AuthError::TokenExpired
        && refresh_values.is_empty()
    {
        tracing::error!("Failed to validate the cookie access_token: {e}");
        clear_cookie(set_cookies, scope, ACCESS_TOKEN_COOKIE_KEY);
        clear_cookie(set_cookies, scope, REFRESH_TOKEN_COOKIE_KEY);
        clear_cookie(set_cookies, scope, CSRF_TOKEN_COOKIE_KEY);
        return Err(AuthError::CredentialMissing);
    }
    let tokens = match refresh_tokens(req.headers, set_cookies, scope, config).await {
        Ok(tokens) => tokens,
        Err(RefreshError::Unauthorized(e)) => {
            tracing::warn!("Failed to refresh tokens: {e}");
            clear_cookie(set_cookies, scope, ACCESS_TOKEN_COOKIE_KEY);
            clear_cookie(set_cookies, scope, REFRESH_TOKEN_COOKIE_KEY);
            clear_cookie(set_cookies, scope, CSRF_TOKEN_COOKIE_KEY);
            return Err(AuthError::CredentialMissing);
        }
        Err(RefreshError::Other(e)) => {
            tracing::warn!("Failed to refresh tokens: {e}");
            return Err(AuthError::Unauthorized(format!("refresh failed: {e}")));
        }
    };
    let claims = claims_from_token_unsafe(&tokens.access_token).map_err(|e| {
        tracing::error!("Failed to parse the refreshed token claims: {e}");
        AuthError::Other(format!("parse refreshed token claims: {e}"))
    })?;
    Ok(Authenticated {
        user_id: claims.id,
        access_token: tokens.access_token,
    })
}

pub fn enforce_csrf(method: &str, headers: &HeaderMap) -> Result<(), AuthError> {
    if method == Method::GET || method == Method::HEAD || method == Method::OPTIONS {
        return Ok(());
    }
    let values = cookie_values(headers, CSRF_TOKEN_COOKIE_KEY);
    if values.is_empty() {
        return Err(AuthError::Forbidden("csrf cookie missing".to_string()));
    }
    let header = headers
        .get("SAuth-CSRF-Token")
        .and_then(|h| h.to_str().ok())
        .unwrap_or("");
    if header.is_empty() {
        return Err(AuthError::Forbidden("csrf token mismatch".to_string()));
    }
    if values.iter().any(|v| v == header) {
        return Ok(());
    }
    Err(AuthError::Forbidden("csrf token mismatch".to_string()))
}

pub fn generate_csrf_token() -> String {
    let mut b = [0u8; 32];
    rand::rng().fill_bytes(&mut b);
    base64::engine::general_purpose::URL_SAFE.encode(b)
}

pub fn write_csrf_cookie(set_cookies: &mut HeaderMap, scope: Option<&CookieScope>) {
    let token = generate_csrf_token();
    write_public_cookie(
        set_cookies,
        scope,
        &CookieWrite::new(CSRF_TOKEN_COOKIE_KEY, &token, "/", 86400),
    );
}

pub enum RefreshError {
    Unauthorized(String),
    Other(String),
}

impl std::fmt::Display for RefreshError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            RefreshError::Unauthorized(s) | RefreshError::Other(s) => f.write_str(s),
        }
    }
}

async fn refresh_tokens(
    headers: &HeaderMap,
    set_cookies: &mut HeaderMap,
    scope: Option<&CookieScope>,
    config: &AuthMiddlewareConfig,
) -> Result<ResponseToken, RefreshError> {
    let values = cookie_values(headers, REFRESH_TOKEN_COOKIE_KEY);
    if values.is_empty() {
        return Err(RefreshError::Other(
            "refresh token from cookie is empty: Unauthorized".to_string(),
        ));
    }
    let token_url = config
        .token_url
        .as_ref()
        .map(|u| u.to_string())
        .unwrap_or_default();
    let mut last: Option<client::auth::AuthError> = None;
    let mut tokens: Option<ResponseToken> = None;
    for value in &values {
        match client::post_refresh_token(
            &token_url,
            &config.client_slug,
            &config.client_secret,
            value,
        )
        .await
        {
            Ok(t) => {
                tokens = Some(t);
                break;
            }
            Err(e) => last = Some(e),
        }
    }
    let Some(tokens) = tokens else {
        let e = last.expect("a refresh error");
        return Err(if e.is_unauthorized() {
            RefreshError::Unauthorized(format!("refresh tokens: {e}"))
        } else {
            RefreshError::Other(format!("refresh tokens: {e}"))
        });
    };
    write_cookie(
        set_cookies,
        scope,
        &CookieWrite::new(
            ACCESS_TOKEN_COOKIE_KEY,
            &tokens.access_token,
            "/",
            tokens.access_expires_in,
        ),
    );
    if !tokens.refresh_token.is_empty() {
        write_cookie(
            set_cookies,
            scope,
            &CookieWrite::new(
                REFRESH_TOKEN_COOKIE_KEY,
                &tokens.refresh_token,
                "/",
                tokens.refresh_expires_in,
            ),
        );
    }
    match cookie_value(headers, CSRF_TOKEN_COOKIE_KEY) {
        Some(held) if !held.is_empty() => write_public_cookie(
            set_cookies,
            scope,
            &CookieWrite::new(CSRF_TOKEN_COOKIE_KEY, &held, "/", 86400),
        ),
        _ => write_csrf_cookie(set_cookies, scope),
    }
    Ok(tokens)
}

pub async fn initiate_middleware(
    mut req: axum::extract::Request,
    next: axum::middleware::Next,
) -> axum::response::Response {
    if let Some(origin) = req
        .headers()
        .get(http::header::ORIGIN)
        .and_then(|h| h.to_str().ok())
        .map(str::to_string)
        && !origin.is_empty()
        && let Ok(initiate) = get_origin_initiate_app(&Ctx::new(), &origin).await
    {
        req.extensions_mut()
            .insert(CookieScopeKey(initiate.get_cookie_scope()));
    }
    next.run(req).await
}

pub async fn get_origin_allowed(origins: &[String], origin: &str) -> bool {
    if origins.iter().any(|o| o == origin) {
        return true;
    }
    get_origin_initiate_app(&Ctx::new(), origin).await.is_ok()
}
