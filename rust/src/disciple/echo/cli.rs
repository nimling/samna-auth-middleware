use std::collections::HashMap;
use std::net::IpAddr;
use std::sync::LazyLock;

use axum::extract::Request;
use axum::response::{IntoResponse, Response};
use http::header::{CONTENT_SECURITY_POLICY, CONTENT_TYPE, REFERRER_POLICY};
use http::{HeaderValue, StatusCode};
use regex::Regex;
use serde::Serialize;
use url::Url;
use uuid::Uuid;

use super::auth::html_escape;
use super::respond::{get_request_id, redirect, respond_error, set_no_store};
use crate::client::{self, auth::AuthError};
use crate::utils::{DebugMode, cfg, get_debug_mode};

pub const CLI_TOKEN_PATH: &str = "/api/auth/cli/token";
pub const CLI_REVOKE_PATH: &str = "/api/auth/cli/revoke";
const CLI_TARGET_PASTE: &str = "paste";
const CLI_GRANT_AUTH_CODE: &str = "authorization_code";
const CLI_GRANT_REFRESH: &str = "refresh_token";
const CLI_DEBUG_REFUSAL: &str = "this server runs in debug mode and authenticates every request as the debug user, so there is no cli to sign in";
pub const CLI_TARGET_REFUSAL: &str = "cli_challenge must be an S256 challenge, cli_redirect a loopback address with a port and cli_state set with it";
const CLI_LOOPBACK_SCHEME: &str = "http";
const CLI_LOOPBACK_HOSTNAME: &str = "localhost";

static CLI_CHALLENGE_PATTERN: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"^[A-Za-z0-9_-]{43}$").expect("cli challenge pattern"));
static CLI_STATE_PATTERN: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"^[A-Za-z0-9_-]{16,128}$").expect("cli state pattern"));

const CLI_CODE_SOURCE: &str = include_str!("../../../../disciple/echo/cli/code.html");

#[derive(Clone, Debug, Default, Serialize)]
pub struct ResponseCliToken {
    pub access_token: String,
    pub token_type: String,
    pub expires_in: i64,
    #[serde(skip_serializing_if = "String::is_empty")]
    pub refresh_token: String,
    #[serde(skip_serializing_if = "is_zero")]
    pub refresh_expires_in: i64,
}

fn is_zero(n: &i64) -> bool {
    *n == 0
}

#[derive(Clone, Debug, Default, Serialize)]
pub struct ResponseOAuthError {
    pub error: String,
    #[serde(skip_serializing_if = "String::is_empty")]
    pub error_description: String,
}

async fn form(req: Request) -> Result<HashMap<String, String>, ()> {
    let body = axum::body::to_bytes(req.into_body(), 1 << 20)
        .await
        .map_err(|_| ())?;
    Ok(url::form_urlencoded::parse(&body)
        .map(|(k, v)| (k.into_owned(), v.into_owned()))
        .collect())
}

pub async fn post_cli_token(req: Request) -> Response {
    if get_debug_mode() != DebugMode::None {
        return respond_oauth_error(
            StatusCode::BAD_REQUEST,
            "invalid_request",
            CLI_DEBUG_REFUSAL,
        );
    }
    let Ok(form) = form(req).await else {
        return respond_oauth_error(
            StatusCode::BAD_REQUEST,
            "invalid_request",
            "malformed token request",
        );
    };
    let slug = cfg().app_slug;
    let secret = cfg().app_secret;
    let token_url = match client::token_url_for(&slug) {
        Ok(u) => u,
        Err(e) => {
            tracing::error!("PostCliToken url: {e}");
            return respond_oauth_error(StatusCode::INTERNAL_SERVER_ERROR, "server_error", "");
        }
    };
    let get = |key: &str| form.get(key).cloned().unwrap_or_default();
    let tokens = match get("grant_type").as_str() {
        CLI_GRANT_AUTH_CODE => {
            client::post_auth_code(
                &token_url,
                &slug,
                &secret,
                &get("code"),
                &cfg().redirect_uri,
                &get("code_verifier"),
            )
            .await
        }
        CLI_GRANT_REFRESH => {
            client::post_refresh_token(&token_url, &slug, &secret, &get("refresh_token")).await
        }
        _ => return respond_oauth_error(StatusCode::BAD_REQUEST, "unsupported_grant_type", ""),
    };
    let tokens = match tokens {
        Ok(t) => t,
        Err(AuthError::Token(t)) if t.status < 500 && t.code != "invalid_client" => {
            return respond_oauth_error(StatusCode::BAD_REQUEST, &t.code, &t.description);
        }
        Err(e) => {
            tracing::error!("PostCliToken: {e}");
            return respond_oauth_error(StatusCode::BAD_GATEWAY, "server_error", "");
        }
    };
    let body = ResponseCliToken {
        access_token: tokens.access_token,
        token_type: "Bearer".to_string(),
        expires_in: tokens.access_expires_in,
        refresh_token: tokens.refresh_token,
        refresh_expires_in: tokens.refresh_expires_in,
    };
    let mut response = (StatusCode::OK, axum::Json(body)).into_response();
    set_no_store(&mut response);
    response
}

pub async fn post_cli_revoke(req: Request) -> Response {
    let Ok(form) = form(req).await else {
        return respond_oauth_error(
            StatusCode::BAD_REQUEST,
            "invalid_request",
            "malformed revocation request",
        );
    };
    let token = form.get("token").cloned().unwrap_or_default();
    if token.is_empty() || form.get("token_type_hint").map(String::as_str) == Some("access_token") {
        return StatusCode::OK.into_response();
    }
    let slug = cfg().app_slug;
    let revoke_url = match client::revoke_url_for(&slug) {
        Ok(u) => u,
        Err(e) => {
            tracing::error!("PostCliRevoke url: {e}");
            return respond_oauth_error(StatusCode::INTERNAL_SERVER_ERROR, "server_error", "");
        }
    };
    if let Err(e) = client::post_revoke(&revoke_url, &slug, &cfg().app_secret, &token).await {
        tracing::error!("PostCliRevoke: {e}");
        return respond_oauth_error(
            StatusCode::SERVICE_UNAVAILABLE,
            "temporarily_unavailable",
            "",
        );
    }
    StatusCode::OK.into_response()
}

pub fn get_cli_target(challenge: &str, redirect: &str, state: &str) -> Option<String> {
    if !CLI_CHALLENGE_PATTERN.is_match(challenge) {
        return None;
    }
    if redirect.is_empty() {
        return Some(CLI_TARGET_PASTE.to_string());
    }
    if !CLI_STATE_PATTERN.is_match(state) {
        return None;
    }
    let mut target = get_cli_loopback(redirect)?;
    target.query_pairs_mut().append_pair("state", state);
    Some(target.to_string())
}

pub fn get_cli_loopback(redirect: &str) -> Option<Url> {
    let target = Url::parse(redirect).ok()?;
    if target.scheme() != CLI_LOOPBACK_SCHEME
        || !target.username().is_empty()
        || target.fragment().is_some()
        || target.port().is_none()
    {
        return None;
    }
    let host = target.host_str()?.trim_matches(['[', ']']);
    if host == CLI_LOOPBACK_HOSTNAME {
        return Some(target);
    }
    let ip: IpAddr = host.parse().ok()?;
    if !(ip == IpAddr::from([127, 0, 0, 1]) || ip == IpAddr::from([0u16, 0, 0, 0, 0, 0, 0, 1])) {
        return None;
    }
    Some(target)
}

pub fn respond_cli_code(request_id: Uuid, target: &str, code: &str) -> Response {
    if target != CLI_TARGET_PASTE {
        let Some(mut loopback) = get_cli_loopback(target) else {
            return respond_error(request_id, StatusCode::BAD_REQUEST, CLI_TARGET_REFUSAL);
        };
        loopback.query_pairs_mut().append_pair("code", code);
        let mut response = redirect(StatusCode::FOUND, loopback.as_str());
        set_no_store(&mut response);
        return response;
    }
    let body = CLI_CODE_SOURCE
        .replace("{{.App}}", &html_escape(&cfg().app_slug))
        .replace("{{.Code}}", &html_escape(code));
    let mut response = (StatusCode::OK, body).into_response();
    set_no_store(&mut response);
    let h = response.headers_mut();
    h.insert(
        CONTENT_TYPE,
        HeaderValue::from_static("text/html; charset=UTF-8"),
    );
    h.insert(CONTENT_SECURITY_POLICY, HeaderValue::from_static("default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'"));
    h.insert("X-Frame-Options", HeaderValue::from_static("DENY"));
    h.insert(REFERRER_POLICY, HeaderValue::from_static("no-referrer"));
    response
}

pub fn respond_oauth_error(status: StatusCode, code: &str, description: &str) -> Response {
    let mut response = (
        status,
        axum::Json(ResponseOAuthError {
            error: code.to_string(),
            error_description: description.to_string(),
        }),
    )
        .into_response();
    set_no_store(&mut response);
    response
}

pub fn request_id_of(req: &Request) -> Uuid {
    get_request_id(req)
}
