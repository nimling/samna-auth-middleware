use std::net::SocketAddr;

use axum::extract::{ConnectInfo, Request};
use axum::middleware::Next;
use axum::response::{IntoResponse, Response};
use chrono::{DateTime, Utc};
use http::header::{LOCATION, REFERRER_POLICY};
use http::{HeaderMap, HeaderValue, Method, StatusCode};
use uuid::Uuid;

use super::main::{Actor, RequestCtx, is_navigation, respond_unauthenticated};
use super::models::ACCESS_TOKEN_COOKIE_KEY;
use super::rate_limit::{allow_default, rate_key, respond_rate_limited};
use crate::analytics;
use crate::disciple::sdb;
use crate::shared::claims::PublicClaimsFind;
use crate::shared::code::{self as code_mod, ANONYMOUS_USER_ID, CodeAccess, get_code_hash};
use crate::shared::{Ctx, Error};
use crate::utils::cookie::cookie_value;

pub const CODE_PARAM: &str = crate::client::CODE_PARAM;
pub const CODE_HEADER: &str = crate::client::CODE_HEADER;
pub const CODE_COOKIE: &str = code_mod::CODE_COOKIE;

const CODE_INVITE_MAX_AGE: i64 = 600;

#[derive(Clone, Debug)]
pub struct CodeKey(pub CodeAccess);

#[derive(Clone, Debug)]
pub struct AnonymousKey(pub String);

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum CodeSource {
    None,
    Query,
    Header,
    Cookie,
}

pub fn read_code(req: &mut Request) -> (String, CodeSource) {
    let uri = req.uri().clone();
    if let Some(query) = uri.query() {
        let pairs: Vec<(String, String)> = url::form_urlencoded::parse(query.as_bytes())
            .map(|(k, v)| (k.into_owned(), v.into_owned()))
            .collect();
        if pairs.iter().any(|(k, _)| k == CODE_PARAM) {
            let value = pairs
                .iter()
                .find(|(k, _)| k == CODE_PARAM)
                .map(|(_, v)| v.clone())
                .unwrap_or_default();
            let remaining: Vec<(String, String)> =
                pairs.into_iter().filter(|(k, _)| k != CODE_PARAM).collect();
            let mut parts = uri.clone().into_parts();
            let path = uri.path().to_string();
            let new_query = url::form_urlencoded::Serializer::new(String::new())
                .extend_pairs(remaining.iter())
                .finish();
            let pq = if new_query.is_empty() {
                path
            } else {
                format!("{path}?{new_query}")
            };
            if let Ok(p) = pq.parse() {
                parts.path_and_query = Some(p);
                if let Ok(new_uri) = http::Uri::from_parts(parts) {
                    *req.uri_mut() = new_uri;
                }
            }
            if !value.is_empty() {
                return (value, CodeSource::Query);
            }
        }
    }
    if let Some(value) = req
        .headers()
        .get(CODE_HEADER)
        .and_then(|h| h.to_str().ok())
        .map(str::to_string)
        && !value.is_empty()
    {
        req.headers_mut().remove(CODE_HEADER);
        return (value, CodeSource::Header);
    }
    if let Some(value) = cookie_value(req.headers(), CODE_COOKIE)
        && !value.is_empty()
    {
        return (value, CodeSource::Cookie);
    }
    (String::new(), CodeSource::None)
}

pub fn write_code_cookie(
    headers: &mut HeaderMap,
    value: &str,
    max_age: i64,
    expires: Option<DateTime<Utc>>,
) {
    let mut cookie = axum_extra::extract::cookie::Cookie::build((CODE_COOKIE, value.to_string()))
        .path("/")
        .http_only(true)
        .secure(true)
        .same_site(axum_extra::extract::cookie::SameSite::Lax);
    let mut age = max_age;
    if let Some(expires) = expires {
        age = (expires - Utc::now()).num_seconds();
        if age <= 0 {
            age = -1;
        }
    }
    cookie = cookie.max_age(time::Duration::seconds(age));
    if let Ok(v) = cookie.build().to_string().parse() {
        headers.append(http::header::SET_COOKIE, v);
    }
}

pub fn clear_code_cookie(headers: &mut HeaderMap) {
    write_code_cookie(headers, "", -1, None);
}

fn is_navigation_get(method: &Method, headers: &HeaderMap) -> bool {
    *method == Method::GET && is_navigation(headers)
}

fn respond_code_stripped(uri: &http::Uri) -> Response {
    let target = uri
        .path_and_query()
        .map(|p| p.as_str().to_string())
        .unwrap_or_else(|| "/".to_string());
    let mut response = StatusCode::SEE_OTHER.into_response();
    if let Ok(v) = HeaderValue::from_str(&target) {
        response.headers_mut().insert(LOCATION, v);
    }
    response
}

fn respond_code_refused() -> Response {
    (StatusCode::UNAUTHORIZED, "Unauthorized").into_response()
}

fn respond_unavailable(what: &str, err: &Error) -> Response {
    tracing::error!("{what}: {err}");
    (StatusCode::SERVICE_UNAVAILABLE, "Service Unavailable").into_response()
}

fn with_cookies(mut response: Response, cookies: HeaderMap) -> Response {
    for value in cookies.get_all(http::header::SET_COOKIE) {
        response
            .headers_mut()
            .append(http::header::SET_COOKIE, value.clone());
    }
    response
}

pub async fn serve_code(
    mut req: Request,
    value: String,
    source: CodeSource,
    next: Next,
) -> Response {
    let ctx = req
        .extensions()
        .get::<RequestCtx>()
        .map(|c| c.0.clone())
        .unwrap_or_default();
    let Some(db) = sdb::instance() else {
        return (StatusCode::SERVICE_UNAVAILABLE, "Service Unavailable").into_response();
    };
    let mut cookies = HeaderMap::new();
    let public = match db.get_public_claims(&ctx, Uuid::nil()).await {
        Ok(p) => p,
        Err(e) => return respond_unavailable("load public claims", &e),
    };
    let method = req.method().clone();
    let headers = req.headers().clone();
    let uri = req.uri().clone();
    let referrer: Response;
    if public.find(&get_code_hash(&value)).is_some() {
        if source == CodeSource::Query && is_navigation_get(&method, &headers) {
            referrer = respond_code_stripped(&uri);
            return no_referrer(referrer);
        }
        return no_referrer(next.run(req).await);
    }
    if source != CodeSource::Cookie {
        let addr = req
            .extensions()
            .get::<ConnectInfo<SocketAddr>>()
            .map(|c| c.0);
        if let Err(period) = allow_default(&rate_key(&headers, addr)) {
            return respond_rate_limited(period);
        }
    }
    let access = match db.get_code_access(&ctx, &value).await {
        Ok(a) => a,
        Err(Error::CodeInvalid) => {
            if source == CodeSource::Cookie {
                clear_code_cookie(&mut cookies);
                return with_cookies(no_referrer(next.run(req).await), cookies);
            }
            return no_referrer(respond_code_refused());
        }
        Err(e) => return respond_unavailable("resolve access code", &e),
    };
    let user_id = req
        .extensions()
        .get::<Actor>()
        .map(|a| a.0)
        .unwrap_or(Uuid::nil());
    if let Err(e) = db.post_code_redeem(&ctx, &value, user_id).await {
        if source == CodeSource::Cookie {
            clear_code_cookie(&mut cookies);
        }
        if matches!(e, Error::CodeInvalid) {
            return with_cookies(no_referrer(respond_code_refused()), cookies);
        }
        return with_cookies(
            respond_unavailable(&format!("redeem access code {}", access.code.id), &e),
            cookies,
        );
    }
    if source == CodeSource::Cookie {
        clear_code_cookie(&mut cookies);
    }
    req.extensions_mut().insert(CodeKey(access));
    if source == CodeSource::Query && is_navigation_get(&method, &headers) {
        return with_cookies(no_referrer(respond_code_stripped(&uri)), cookies);
    }
    with_cookies(no_referrer(next.run(req).await), cookies)
}

fn no_referrer(mut response: Response) -> Response {
    response
        .headers_mut()
        .insert(REFERRER_POLICY, HeaderValue::from_static("no-referrer"));
    response
}

fn serve_invite_login(
    uri: &http::Uri,
    method: &Method,
    headers: &HeaderMap,
    value: &str,
) -> Response {
    let login = super::main::get_login_path();
    if login.is_empty() || !is_navigation_get(method, headers) {
        return respond_code_refused();
    }
    let mut cookies = HeaderMap::new();
    write_code_cookie(&mut cookies, value, CODE_INVITE_MAX_AGE, None);
    let request_uri = uri
        .path_and_query()
        .map(|p| p.as_str().to_string())
        .unwrap_or_else(|| "/".to_string());
    let target = format!(
        "{login}?next={}",
        url::form_urlencoded::byte_serialize(request_uri.as_bytes()).collect::<String>()
    );
    let mut response = StatusCode::SEE_OTHER.into_response();
    if let Ok(v) = HeaderValue::from_str(&target) {
        response.headers_mut().insert(LOCATION, v);
    }
    with_cookies(response, cookies)
}

pub async fn anonymous_middleware(mut req: Request, next: Next) -> Response {
    if let Some(actor) = req.extensions().get::<Actor>()
        && !actor.0.is_nil()
    {
        return next.run(req).await;
    }
    let (value, source) = read_code(&mut req);
    serve_anonymous(req, value, source, next).await
}

pub async fn serve_anonymous(
    mut req: Request,
    value: String,
    source: CodeSource,
    next: Next,
) -> Response {
    let addr = req
        .extensions()
        .get::<ConnectInfo<SocketAddr>>()
        .map(|c| c.0);
    let headers = req.headers().clone();
    if let Err(period) = allow_default(&rate_key(&headers, addr)) {
        return respond_rate_limited(period);
    }
    let Some(db) = sdb::instance() else {
        return respond_unauthenticated(&headers);
    };
    let mut ctx = req
        .extensions()
        .get::<RequestCtx>()
        .map(|c| c.0.clone())
        .unwrap_or_default();
    let mut cookies = HeaderMap::new();
    let public = match db.get_public_claims(&ctx, Uuid::nil()).await {
        Ok(p) => p,
        Err(e) => return respond_unavailable("load public claims", &e),
    };
    let method = req.method().clone();
    let uri = req.uri().clone();
    let mut referrer = false;
    if !value.is_empty() {
        referrer = true;
        let hash = get_code_hash(&value);
        match public.find(&hash) {
            Some(claim)
                if claim.accepts(
                    &hash,
                    source == CodeSource::Query,
                    source == CodeSource::Header,
                    source == CodeSource::Cookie,
                ) =>
            {
                ctx = ctx.with_code_hash(&hash);
                req.extensions_mut().insert(RequestCtx(ctx.clone()));
                if source == CodeSource::Query
                    && claim.samk.as_ref().is_some_and(|s| s.cookie)
                    && is_navigation_get(&method, &headers)
                {
                    write_code_cookie(&mut cookies, &value, 0, claim.ends_at);
                    return with_cookies(no_referrer(respond_code_stripped(&uri)), cookies);
                }
            }
            Some(_) => {
                if source != CodeSource::Cookie {
                    return no_referrer(respond_code_refused());
                }
                clear_code_cookie(&mut cookies);
            }
            None => match db.get_code_access(&ctx, &value).await {
                Ok(_) => return no_referrer(serve_invite_login(&uri, &method, &headers, &value)),
                Err(Error::CodeInvalid) => {
                    if source != CodeSource::Cookie {
                        return no_referrer(respond_code_refused());
                    }
                    clear_code_cookie(&mut cookies);
                }
                Err(e) => return respond_unavailable("resolve access code", &e),
            },
        }
    }
    if public.is_empty() {
        return with_cookies(respond_unauthenticated(&headers), cookies);
    }
    req.extensions_mut().insert(Actor(*ANONYMOUS_USER_ID));
    let (anonymous, anonymous_cookie) = analytics::anonymous_id(&headers, None);
    if let Some(c) = anonymous_cookie {
        crate::utils::cookie::write_cookie(&mut cookies, None, &c);
    }
    req.extensions_mut().insert(AnonymousKey(anonymous));
    let mut response = next.run(req).await;
    if response.status() == StatusCode::FORBIDDEN {
        *response.status_mut() = StatusCode::UNAUTHORIZED;
    }
    if referrer {
        response = no_referrer(response);
    }
    with_cookies(response, cookies)
}

pub fn access_token_cookie(headers: &HeaderMap) -> Option<String> {
    cookie_value(headers, ACCESS_TOKEN_COOKIE_KEY)
}

pub fn ctx_of(req: &Request) -> Ctx {
    req.extensions()
        .get::<RequestCtx>()
        .map(|c| c.0.clone())
        .unwrap_or_default()
}
