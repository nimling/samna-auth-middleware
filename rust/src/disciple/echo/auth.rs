use axum::extract::{Path, Request};
use axum::response::{IntoResponse, Response};
use base64::Engine;
use base64::engine::general_purpose::URL_SAFE_NO_PAD;
use http::header::{CONTENT_SECURITY_POLICY, CONTENT_TYPE, REFERRER_POLICY, SET_COOKIE};
use http::{HeaderMap, HeaderValue, StatusCode};
use url::Url;

use super::cli::{CLI_TARGET_REFUSAL, get_cli_target, respond_cli_code};
use super::respond::{get_request_id, redirect, respond_error, set_no_store};
use crate::client::{self, auth::AuthError};
use crate::shared::db::get_initiate_app;
use crate::shared::{Ctx, SamnaApp};
use crate::smw::session::write_csrf_cookie;
use crate::utils::cookie::{CookieWrite, clear_cookie, cookie_value, write_cookie};
use crate::utils::{CookieScope, cfg};

const LOGIN_PROMPT_LOGIN: &str = "login";
const LOGIN_RETRY_WINDOW: i64 = 60;
const OAUTH_ERR_INVALID_REQUEST: &str = "invalid_request";
const OAUTH_ERR_INVALID_CLIENT: &str = "invalid_client";
const OAUTH_ERR_SERVER_ERROR: &str = "server_error";
const LOGIN_ERR_NO_CODE: &str = "The sign in came back without an authorization code.";
const LOGIN_ERR_STATE: &str = "The sign in state does not match this browser.";
const LOGIN_ERR_VERIFIER: &str = "This browser holds no verifier for the sign in.";
const LOGIN_ERR_EXCHANGE: &str = "The authorization code was refused.";
const LOGIN_ERR_PROPHET: &str = "The sign in could not be completed.";
const LOGIN_ERR_REFUSED: &str = "The sign in was refused.";
const LOGIN_ERR_RETRY_EXHAUSTED: &str =
    "It failed twice within a minute, so it stops here instead of trying again.";

const LOGIN_ERROR_SOURCE: &str = include_str!("../../../../disciple/echo/login/error.html");

fn render(template: &str, values: &[(&str, &str)]) -> String {
    let mut out = template.to_string();
    for (key, value) in values {
        out = out.replace(&format!("{{{{.{key}}}}}"), &html_escape(value));
    }
    out
}

pub fn html_escape(text: &str) -> String {
    text.replace('&', "&amp;")
        .replace('<', "&lt;")
        .replace('>', "&gt;")
        .replace('"', "&#34;")
        .replace('\'', "&#39;")
}

fn query(req: &Request) -> std::collections::BTreeMap<String, String> {
    req.uri()
        .query()
        .map(|q| {
            url::form_urlencoded::parse(q.as_bytes())
                .map(|(k, v)| (k.into_owned(), v.into_owned()))
                .collect()
        })
        .unwrap_or_default()
}

fn with_cookies(mut response: Response, cookies: HeaderMap) -> Response {
    for value in cookies.get_all(SET_COOKIE) {
        response.headers_mut().append(SET_COOKIE, value.clone());
    }
    response
}

pub async fn get_login(app: Option<Path<String>>, req: Request) -> Response {
    let request_id = get_request_id(&req);
    let mut slug = cfg().app_slug;
    let mut initiate: Option<SamnaApp> = None;
    let mut scope: Option<CookieScope> = None;
    if let Some(Path(app_slug)) = app
        && !app_slug.is_empty()
    {
        match get_initiate_app(&Ctx::new(), &app_slug).await {
            Ok(found) => {
                slug = found.slug.clone();
                scope = Some(found.get_cookie_scope());
                initiate = Some(found);
            }
            Err(_) => return respond_error(request_id, StatusCode::NOT_FOUND, "unknown app"),
        }
    }
    let verifier = client::generate_verifier();
    let mut state = client::generate_verifier();
    let params = query(&req);
    let (landing, origins) = get_return_scope(initiate.as_ref());
    let mut next = params.get("next").cloned().unwrap_or_default();
    if next.is_empty()
        && let Some(referer) = req
            .headers()
            .get(http::header::REFERER)
            .and_then(|h| h.to_str().ok())
        && let Ok(parsed) = Url::parse(referer)
    {
        let host = req
            .headers()
            .get(http::header::HOST)
            .and_then(|h| h.to_str().ok())
            .unwrap_or("");
        let referer_host = match parsed.port() {
            Some(port) => format!("{}:{port}", parsed.host_str().unwrap_or("")),
            None => parsed.host_str().unwrap_or("").to_string(),
        };
        if referer_host.eq_ignore_ascii_case(host) && parsed.path() != req.uri().path() {
            next = format!(
                "{}{}",
                parsed.path(),
                parsed.query().map(|q| format!("?{q}")).unwrap_or_default()
            );
        }
    }
    let mut cookies = HeaderMap::new();
    if !get_return_to(&next, &landing, &origins).is_empty() {
        state.push('.');
        state.push_str(&URL_SAFE_NO_PAD.encode(next.as_bytes()));
        write_cookie(
            &mut cookies,
            scope.as_ref(),
            &CookieWrite::new(client::LOGIN_NEXT_COOKIE, &next, "/", 600),
        );
    }
    let mut challenge = client::challenge_s256(&verifier);
    let mut cli_target = String::new();
    if let Some(cli_challenge) = params.get("cli_challenge").filter(|c| !c.is_empty()) {
        match get_cli_target(
            cli_challenge,
            params.get("cli_redirect").map(String::as_str).unwrap_or(""),
            params.get("cli_state").map(String::as_str).unwrap_or(""),
        ) {
            Some(target) => {
                challenge = cli_challenge.clone();
                cli_target = target;
            }
            None => return respond_error(request_id, StatusCode::BAD_REQUEST, CLI_TARGET_REFUSAL),
        }
    }
    let mut redirect_uri = cfg().redirect_uri;
    if let Some(initiate) = &initiate {
        redirect_uri = initiate.get_callback_uri();
    }
    let mut extra: Vec<(String, String)> = Vec::new();
    for key in ["prompt", "error", "error_description"] {
        if let Some(value) = params.get(key)
            && !value.is_empty()
        {
            extra.push((key.to_string(), value.clone()));
        }
    }
    let auth_url = match client::authorize_url(&slug, &redirect_uri, &state, &challenge, &extra) {
        Ok(u) => u,
        Err(e) => {
            tracing::error!("GetLogin authorize url: {e}");
            return respond_error(
                request_id,
                StatusCode::INTERNAL_SERVER_ERROR,
                "Internal Server Error",
            );
        }
    };
    if !cli_target.is_empty() {
        clear_cookie(&mut cookies, scope.as_ref(), client::PKCE_VERIFIER_COOKIE);
        write_cookie(
            &mut cookies,
            scope.as_ref(),
            &CookieWrite::new(client::CLI_TARGET_COOKIE, &cli_target, "/", 600),
        );
    } else {
        clear_cookie(&mut cookies, scope.as_ref(), client::CLI_TARGET_COOKIE);
        write_cookie(
            &mut cookies,
            scope.as_ref(),
            &CookieWrite::new(client::PKCE_VERIFIER_COOKIE, &verifier, "/", 600),
        );
    }
    write_cookie(
        &mut cookies,
        scope.as_ref(),
        &CookieWrite::new(client::OAUTH_STATE_COOKIE, &state, "/", 600),
    );
    with_cookies(redirect(StatusCode::TEMPORARY_REDIRECT, &auth_url), cookies)
}

pub async fn get_callback(req: Request) -> Response {
    let request_id = get_request_id(&req);
    let params = query(&req);
    let mut redirect_uri = cfg().redirect_uri;
    let mut start_over = cfg().page_redirect;
    let mut initiate: Option<SamnaApp> = None;
    let mut scope: Option<CookieScope> = None;
    if let Some(app_slug) = params.get("app").filter(|a| !a.is_empty()) {
        match get_initiate_app(&Ctx::new(), app_slug).await {
            Ok(found) => {
                redirect_uri = found.get_callback_uri();
                start_over = found.get_primary_origin();
                scope = Some(found.get_cookie_scope());
                initiate = Some(found);
            }
            Err(_) => return respond_error(request_id, StatusCode::NOT_FOUND, "unknown app"),
        }
    }
    if start_over.is_empty() {
        start_over = "/".to_string();
    }
    let mut auth_path = req
        .uri()
        .path()
        .strip_suffix("callback")
        .unwrap_or(req.uri().path())
        .to_string();
    if let Some(initiate) = &initiate {
        auth_path.push_str(
            &url::form_urlencoded::byte_serialize(initiate.slug.as_bytes()).collect::<String>(),
        );
        auth_path.push('/');
    }
    let (landing, origins) = get_return_scope(initiate.as_ref());
    let state_param = params.get("state").cloned().unwrap_or_default();
    let mut state_next = String::new();
    if let Some(dot) = state_param.rfind('.')
        && let Ok(raw) = URL_SAFE_NO_PAD.decode(&state_param[dot + 1..])
        && let Ok(text) = String::from_utf8(raw)
        && !get_return_to(&text, &landing, &origins).is_empty()
    {
        state_next = text;
    }
    let headers = req.headers().clone();
    if let Some(refused) = params.get("error").filter(|e| !e.is_empty()) {
        let mut description = params.get("error_description").cloned().unwrap_or_default();
        if description.is_empty() {
            description = LOGIN_ERR_REFUSED.to_string();
        }
        return respond_login_error(
            &headers,
            scope.as_ref(),
            &auth_path,
            &state_next,
            refused,
            &description,
            "the authorize request was answered with an error",
        );
    }
    let code = params.get("code").cloned().unwrap_or_default();
    if code.is_empty() {
        return respond_login_error(
            &headers,
            scope.as_ref(),
            &auth_path,
            &state_next,
            OAUTH_ERR_INVALID_REQUEST,
            LOGIN_ERR_NO_CODE,
            "no code in the callback",
        );
    }
    let state_cookie = cookie_value(&headers, client::OAUTH_STATE_COOKIE).unwrap_or_default();
    if state_cookie.is_empty() || state_param.is_empty() || state_param != state_cookie {
        return respond_login_error(
            &headers,
            scope.as_ref(),
            &auth_path,
            &state_next,
            OAUTH_ERR_INVALID_REQUEST,
            LOGIN_ERR_STATE,
            "state cookie missing or not equal to the state parameter",
        );
    }
    let mut cookies = HeaderMap::new();
    if let Some(cli_target) =
        cookie_value(&headers, client::CLI_TARGET_COOKIE).filter(|c| !c.is_empty())
    {
        clear_cookie(&mut cookies, scope.as_ref(), client::CLI_TARGET_COOKIE);
        clear_cookie(&mut cookies, scope.as_ref(), client::OAUTH_STATE_COOKIE);
        clear_cookie(&mut cookies, scope.as_ref(), client::LOGIN_NEXT_COOKIE);
        return with_cookies(respond_cli_code(request_id, &cli_target, &code), cookies);
    }
    let Some(verifier) =
        cookie_value(&headers, client::PKCE_VERIFIER_COOKIE).filter(|v| !v.is_empty())
    else {
        return respond_login_error(
            &headers,
            scope.as_ref(),
            &auth_path,
            &state_next,
            OAUTH_ERR_INVALID_REQUEST,
            LOGIN_ERR_VERIFIER,
            "verifier cookie missing",
        );
    };
    let root_slug = cfg().app_slug;
    let token_url = match client::token_url_for(&root_slug) {
        Ok(u) => u,
        Err(e) => {
            tracing::error!("GetCallback token url: {e}");
            return respond_error(
                request_id,
                StatusCode::INTERNAL_SERVER_ERROR,
                "Internal Server Error",
            );
        }
    };
    let tokens = match client::post_auth_code(
        &token_url,
        &root_slug,
        &cfg().app_secret,
        &code,
        &redirect_uri,
        &verifier,
    )
    .await
    {
        Ok(t) => t,
        Err(AuthError::Token(t))
            if t.status < 500 && !t.code.is_empty() && t.code != OAUTH_ERR_INVALID_CLIENT =>
        {
            let description = if t.description.is_empty() {
                LOGIN_ERR_EXCHANGE.to_string()
            } else {
                t.description.clone()
            };
            return respond_login_error(
                &headers,
                scope.as_ref(),
                &auth_path,
                &state_next,
                &t.code,
                &description,
                &t.to_string(),
            );
        }
        Err(e) => {
            return respond_login_error(
                &headers,
                scope.as_ref(),
                &auth_path,
                &state_next,
                OAUTH_ERR_SERVER_ERROR,
                LOGIN_ERR_PROPHET,
                &e.to_string(),
            );
        }
    };
    clear_cookie(&mut cookies, scope.as_ref(), client::PKCE_VERIFIER_COOKIE);
    clear_cookie(&mut cookies, scope.as_ref(), client::OAUTH_STATE_COOKIE);
    clear_cookie(&mut cookies, scope.as_ref(), client::LOGIN_RETRY_COOKIE);
    write_cookie(
        &mut cookies,
        scope.as_ref(),
        &CookieWrite::new(
            "sauth_access_token",
            &tokens.access_token,
            "/",
            tokens.access_expires_in,
        ),
    );
    if !tokens.refresh_token.is_empty() {
        write_cookie(
            &mut cookies,
            scope.as_ref(),
            &CookieWrite::new(
                "sauth_refresh_token",
                &tokens.refresh_token,
                "/",
                tokens.refresh_expires_in,
            ),
        );
    }
    write_csrf_cookie(&mut cookies, scope.as_ref());
    let mut page_redirect = start_over;
    let target = get_return_to(&state_next, &landing, &origins);
    if !target.is_empty() {
        page_redirect = target;
    }
    if let Some(next_cookie) = cookie_value(&headers, client::LOGIN_NEXT_COOKIE) {
        let target = get_return_to(&next_cookie, &landing, &origins);
        if !target.is_empty() {
            page_redirect = target;
        }
    }
    clear_cookie(&mut cookies, scope.as_ref(), client::LOGIN_NEXT_COOKIE);
    with_cookies(redirect(StatusCode::FOUND, &page_redirect), cookies)
}

fn respond_login_error(
    headers: &HeaderMap,
    scope: Option<&CookieScope>,
    auth_path: &str,
    next: &str,
    code: &str,
    description: &str,
    cause: &str,
) -> Response {
    tracing::warn!("GetCallback {code}: {description}: {cause}");
    let mut cookies = HeaderMap::new();
    for name in [
        client::PKCE_VERIFIER_COOKIE,
        client::OAUTH_STATE_COOKIE,
        client::CLI_TARGET_COOKIE,
        client::LOGIN_NEXT_COOKIE,
        "sauth_csrf_token",
        "sauth_refresh_token",
        "sauth_access_token",
    ] {
        clear_cookie(&mut cookies, scope, name);
    }
    let retries: i64 = cookie_value(headers, client::LOGIN_RETRY_COOKIE)
        .and_then(|v| v.parse().ok())
        .unwrap_or(0);
    if retries > 0 {
        let body = render(
            LOGIN_ERROR_SOURCE,
            &[
                ("App", &cfg().app_slug),
                ("Error", code),
                (
                    "Description",
                    &format!("{description} {LOGIN_ERR_RETRY_EXHAUSTED}"),
                ),
                ("Logout", &format!("{auth_path}logout")),
            ],
        );
        let mut response = (StatusCode::BAD_REQUEST, body).into_response();
        set_no_store(&mut response);
        let h = response.headers_mut();
        h.insert(
            CONTENT_TYPE,
            HeaderValue::from_static("text/html; charset=UTF-8"),
        );
        h.insert(CONTENT_SECURITY_POLICY, HeaderValue::from_static("default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'"));
        h.insert("X-Frame-Options", HeaderValue::from_static("DENY"));
        h.insert(REFERRER_POLICY, HeaderValue::from_static("no-referrer"));
        return with_cookies(response, cookies);
    }
    write_cookie(
        &mut cookies,
        scope,
        &CookieWrite::new(
            client::LOGIN_RETRY_COOKIE,
            &(retries + 1).to_string(),
            "/",
            LOGIN_RETRY_WINDOW,
        ),
    );
    let mut q = url::form_urlencoded::Serializer::new(String::new());
    q.append_pair("prompt", LOGIN_PROMPT_LOGIN);
    q.append_pair("error", code);
    q.append_pair("error_description", description);
    if !next.is_empty() {
        q.append_pair("next", next);
    }
    let mut response = redirect(
        StatusCode::TEMPORARY_REDIRECT,
        &format!("{auth_path}login?{}", q.finish()),
    );
    set_no_store(&mut response);
    with_cookies(response, cookies)
}

pub async fn get_logout(app: Option<Path<String>>, req: Request) -> Response {
    let request_id = get_request_id(&req);
    let mut slug = cfg().app_slug;
    let mut page_redirect = cfg().page_redirect;
    let mut scope: Option<CookieScope> = None;
    if let Some(Path(app_slug)) = app
        && !app_slug.is_empty()
    {
        match get_initiate_app(&Ctx::new(), &app_slug).await {
            Ok(initiate) => {
                slug = initiate.slug.clone();
                page_redirect = initiate.get_primary_origin();
                scope = Some(initiate.get_cookie_scope());
            }
            Err(_) => return respond_error(request_id, StatusCode::NOT_FOUND, "unknown app"),
        }
    }
    let access_token = cookie_value(req.headers(), "sauth_access_token").unwrap_or_default();
    let mut cookies = HeaderMap::new();
    for name in [
        "sauth_csrf_token",
        "sauth_refresh_token",
        "sauth_access_token",
    ] {
        clear_cookie(&mut cookies, scope.as_ref(), name);
    }
    if !access_token.is_empty() {
        match client::get_logout(&access_token).await {
            Err(e) => tracing::error!("GetLogout: {e}"),
            Ok(status) if status >= 400 => {
                tracing::error!("GetLogout: auth server returned {status}")
            }
            Ok(_) => {}
        }
    }
    match client::end_session_url(&slug, &page_redirect) {
        Ok(url) => with_cookies(redirect(StatusCode::FOUND, &url), cookies),
        Err(e) => {
            tracing::error!("GetLogout end session url: {e}");
            with_cookies(StatusCode::OK.into_response(), cookies)
        }
    }
}

pub fn get_return_scope(initiate: Option<&SamnaApp>) -> (String, Vec<String>) {
    if let Some(initiate) = initiate {
        return (initiate.get_primary_origin(), initiate.get_origins());
    }
    let landing = cfg().page_redirect;
    if let Ok(parsed) = Url::parse(&landing)
        && !parsed.scheme().is_empty()
        && parsed.host_str().is_some()
    {
        return (landing.clone(), vec![origin_of(&parsed)]);
    }
    (landing, Vec::new())
}

fn origin_of(parsed: &Url) -> String {
    match parsed.port() {
        Some(port) => format!(
            "{}://{}:{port}",
            parsed.scheme(),
            parsed.host_str().unwrap_or("")
        ),
        None => format!("{}://{}", parsed.scheme(), parsed.host_str().unwrap_or("")),
    }
}

pub fn get_return_to(raw: &str, landing: &str, origins: &[String]) -> String {
    if raw.is_empty() || raw.contains(['\\', '\r', '\n', '\t']) {
        return String::new();
    }
    if raw.starts_with('/') {
        if raw.starts_with("//") {
            return String::new();
        }
        if let Ok(base) = Url::parse(landing)
            && !base.scheme().is_empty()
            && base.host_str().is_some()
        {
            return format!("{}{raw}", origin_of(&base));
        }
        return raw.to_string();
    }
    let Ok(target) = Url::parse(raw) else {
        return String::new();
    };
    if (target.scheme() != "https" && target.scheme() != "http")
        || target.host_str().is_none()
        || !target.username().is_empty()
    {
        return String::new();
    }
    if origins.iter().any(|o| *o == origin_of(&target)) {
        return target.to_string();
    }
    String::new()
}
