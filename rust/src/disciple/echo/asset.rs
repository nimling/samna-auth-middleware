use std::collections::HashMap;
use std::future::Future;
use std::pin::Pin;
use std::sync::{Arc, Mutex, OnceLock};

use axum::body::Body;
use axum::extract::{Path, Request};
use axum::response::{IntoResponse, Response};
use bytes::Bytes;
use chrono::{DateTime, Utc};
use futures::{StreamExt, TryStreamExt};
use http::header::{ACCEPT_RANGES, CONTENT_LENGTH, CONTENT_RANGE, CONTENT_TYPE, LAST_MODIFIED};
use http::{HeaderMap, HeaderValue, StatusCode};
use uuid::Uuid;

use super::me::{actor, ctx};
use super::respond::{get_request_id, respond_error};
use crate::client::{
    self, ASSET_HEADERS, CONDITIONAL_HEADERS, RANGE_HEADERS, get_asset_range, http::httpdate,
};
use crate::disciple::sdb;
use crate::shared::{Ctx, Error};
use crate::smw::token::{Jwks, RequestFacts, unverified_claims, validate_token};
use crate::smw::{AccessTokenKey, AuthMiddlewareConfig};
use crate::utils::cfg;

pub struct AssetResponse {
    pub status: StatusCode,
    pub header: HeaderMap,
    pub body: Option<Body>,
}

pub type BoxFuture<'a, T> = Pin<Box<dyn Future<Output = T> + Send + 'a>>;

pub trait AssetCache: Send + Sync {
    fn get<'a>(
        &'a self,
        link_id: Uuid,
        conditional: &'a HeaderMap,
    ) -> BoxFuture<'a, Result<Option<AssetResponse>, Error>>;
    fn put<'a>(
        &'a self,
        link_id: Uuid,
        header: HeaderMap,
        body: Bytes,
    ) -> BoxFuture<'a, Result<(), Error>>;
}

pub type AssetSource = Arc<
    dyn Fn(Ctx, Uuid) -> BoxFuture<'static, Result<Option<AssetResponse>, Error>> + Send + Sync,
>;

#[derive(Default, Clone)]
pub struct Options {
    pub asset_source: Option<AssetSource>,
    pub asset_cache: Option<Arc<dyn AssetCache>>,
}

static OPTIONS: Mutex<Option<Options>> = Mutex::new(None);

pub fn put_options(o: Options) {
    *OPTIONS.lock().expect("options") = Some(o);
}

fn options() -> Options {
    OPTIONS.lock().expect("options").clone().unwrap_or_default()
}

static PROPHET_KEYS: OnceLock<Mutex<HashMap<String, Arc<Jwks>>>> = OnceLock::new();

fn get_prophet_keys(jwks_url: &str) -> Arc<Jwks> {
    let keys = PROPHET_KEYS.get_or_init(|| Mutex::new(HashMap::new()));
    let mut held = keys.lock().expect("prophet keys");
    held.entry(jwks_url.to_string())
        .or_insert_with(|| Jwks::new(jwks_url))
        .clone()
}

pub async fn get_asset_data(Path(id): Path<String>, req: Request) -> Response {
    let source = options().asset_source;
    get_asset_data_from(req, &id, source).await
}

pub async fn get_object_asset_data(req: Request, link_id: Uuid) -> Response {
    let request_id = get_request_id(&req);
    let user_id = actor(&req).unwrap_or(Uuid::nil());
    let Some(db) = sdb::instance() else {
        return respond_error(
            request_id,
            StatusCode::INTERNAL_SERVER_ERROR,
            "Internal server error",
        );
    };
    if let Err(e) = db.get_object_asset(&ctx(&req), user_id, link_id).await {
        if e.is_not_found() {
            return respond_error(request_id, StatusCode::NOT_FOUND, "Not found");
        }
        tracing::error!("GetObjectAssetData: {e}");
        return respond_error(
            request_id,
            StatusCode::INTERNAL_SERVER_ERROR,
            "Internal server error",
        );
    }
    get_prophet_asset_data(req, link_id, options().asset_cache).await
}

async fn get_asset_data_from(req: Request, id: &str, source: Option<AssetSource>) -> Response {
    let request_id = get_request_id(&req);
    let method = req.method().to_string();
    let path = req.uri().path().to_string();
    let host = req
        .headers()
        .get(http::header::HOST)
        .and_then(|h| h.to_str().ok())
        .unwrap_or("")
        .to_string();
    let headers = req.headers().clone();
    if let Err(e) = get_prophet_token(&headers, &method, &path, &host).await {
        tracing::warn!("asset data: {e}");
        return respond_error(request_id, StatusCode::UNAUTHORIZED, "Unauthorized");
    }
    let Ok(asset_id) = Uuid::parse_str(id) else {
        return respond_error(request_id, StatusCode::BAD_REQUEST, "id must be a uuid");
    };
    let Some(source) = source else {
        return respond_error(request_id, StatusCode::NOT_FOUND, "Not found");
    };
    match source(ctx(&req), asset_id).await {
        Err(e) => {
            tracing::error!("asset source {asset_id}: {e}");
            respond_error(
                request_id,
                StatusCode::INTERNAL_SERVER_ERROR,
                "Internal server error",
            )
        }
        Ok(None) => respond_error(request_id, StatusCode::NOT_FOUND, "Not found"),
        Ok(Some(res)) => put_asset_response(res, req.headers()).await,
    }
}

async fn get_prophet_token(
    headers: &HeaderMap,
    method: &str,
    path: &str,
    host: &str,
) -> Result<(), String> {
    let c = cfg();
    let prophet = client::get_prophet();
    let Some(jwks_url) = c
        .jwks_url
        .as_ref()
        .map(|u| u.to_string())
        .filter(|u| !u.is_empty())
    else {
        return Err("no prophet bound to check the token against".to_string());
    };
    if c.auth_disabled || prophet.is_empty() {
        return Err("no prophet bound to check the token against".to_string());
    }
    let raw = headers
        .get(http::header::AUTHORIZATION)
        .and_then(|h| h.to_str().ok())
        .and_then(|a| a.strip_prefix("Bearer "))
        .unwrap_or("");
    if raw.is_empty() {
        return Err("bearer token missing".to_string());
    }
    let keys = get_prophet_keys(&jwks_url);
    let config = AuthMiddlewareConfig {
        client_slug: c.app_slug.clone(),
        expected_token_issuer: c.issuer.clone(),
        ..AuthMiddlewareConfig::default()
    };
    let facts = RequestFacts {
        method,
        path,
        host,
        scheme: "https",
        headers,
        tls_thumbprint: "",
    };
    validate_token(&facts, raw, &config, &keys, None)
        .await
        .map_err(|e| e.to_string())?;
    let claims = unverified_claims(raw).map_err(|e| e.to_string())?;
    if claims.client_id != prophet {
        return Err(format!("token not issued to the prophet {prophet}"));
    }
    if claims.exp.is_none() {
        return Err("token without expiry".to_string());
    }
    Ok(())
}

fn pick(headers: &HeaderMap, names: &[&str]) -> HeaderMap {
    let mut out = HeaderMap::new();
    for name in names {
        if let Some(v) = headers.get(*name) {
            out.insert(
                http::HeaderName::from_bytes(name.as_bytes()).expect("header name"),
                v.clone(),
            );
        }
    }
    out
}

async fn get_prophet_asset_data(
    req: Request,
    link_id: Uuid,
    cache: Option<Arc<dyn AssetCache>>,
) -> Response {
    let ctx_value = ctx(&req);
    let conditional = pick(req.headers(), &CONDITIONAL_HEADERS);
    let mut request = conditional.clone();
    for (k, v) in pick(req.headers(), &RANGE_HEADERS) {
        if let Some(k) = k {
            request.insert(k, v);
        }
    }
    let path = format!("object_asset/{link_id}/data");
    if let Some(cache) = cache {
        let cached = match cache.get(link_id, &conditional).await {
            Ok(c) => c,
            Err(e) => {
                tracing::warn!("asset cache get {link_id}: {e}");
                None
            }
        };
        if let Some(mut cached) = cached {
            if get_asset_fresh(&cached.header, Utc::now()) {
                return put_asset_response(cached, &request).await;
            }
            let mut validators = HeaderMap::new();
            if let Some(v) = cached.header.get("ETag") {
                validators.insert("If-None-Match", v.clone());
            }
            if let Some(v) = cached.header.get(LAST_MODIFIED) {
                validators.insert("If-Modified-Since", v.clone());
            }
            let resp = match client::get_stream(&ctx_value, &path, &validators).await {
                Ok(r) => r,
                Err(e) => return put_prophet_error(get_request_id(&req), &e),
            };
            if resp.status() == StatusCode::NOT_MODIFIED {
                for name in ASSET_HEADERS {
                    if let Some(v) = resp.headers().get(name)
                        && name != "Content-Length"
                        && name != "Content-Type"
                        && name != "Content-Range"
                    {
                        cached.header.insert(
                            http::HeaderName::from_bytes(name.as_bytes()).expect("header"),
                            v.clone(),
                        );
                    }
                }
                return put_asset_response(cached, &request).await;
            }
            return put_prophet_response(link_id, Some(cache), resp, &request).await;
        }
        let resp = match client::get_stream(&ctx_value, &path, &conditional).await {
            Ok(r) => r,
            Err(e) => return put_prophet_error(get_request_id(&req), &e),
        };
        return put_prophet_response(link_id, Some(cache), resp, &request).await;
    }
    let resp = match client::get_stream(&ctx_value, &path, &request).await {
        Ok(r) => r,
        Err(e) => return put_prophet_error(get_request_id(&req), &e),
    };
    put_prophet_response(link_id, None, resp, &request).await
}

fn copy_asset_headers(from: &HeaderMap, into: &mut HeaderMap) {
    for name in ASSET_HEADERS {
        if let Some(v) = from.get(name) {
            into.insert(
                http::HeaderName::from_bytes(name.as_bytes()).expect("header"),
                v.clone(),
            );
        }
    }
}

async fn put_prophet_response(
    link_id: Uuid,
    cache: Option<Arc<dyn AssetCache>>,
    resp: reqwest::Response,
    request: &HeaderMap,
) -> Response {
    let status = StatusCode::from_u16(resp.status().as_u16()).unwrap_or(StatusCode::BAD_GATEWAY);
    let headers = resp.headers().clone();
    let mut out_headers = HeaderMap::new();
    copy_asset_headers(&headers, &mut out_headers);
    if status != StatusCode::OK {
        let mut response = status.into_response();
        *response.headers_mut() = out_headers;
        if status == StatusCode::NOT_MODIFIED || status == StatusCode::RANGE_NOT_SATISFIABLE {
            return response;
        }
        let stream = resp.bytes_stream().map_err(std::io::Error::other);
        *response.body_mut() = Body::from_stream(stream);
        return response;
    }
    match cache {
        Some(cache) if get_asset_storable(&headers) => {
            let bytes = match resp.bytes().await {
                Ok(b) => b,
                Err(e) => {
                    tracing::error!("asset from prophet: {e}");
                    return (StatusCode::BAD_GATEWAY, "Bad gateway").into_response();
                }
            };
            let header = headers.clone();
            let stored = bytes.clone();
            tokio::spawn(async move {
                if let Err(e) = cache.put(link_id, header, stored).await {
                    tracing::warn!("asset cache put {link_id}: {e}");
                }
            });
            put_asset_range(&headers, Body::from(bytes), request)
        }
        _ => {
            let stream = resp.bytes_stream().map_err(std::io::Error::other);
            put_asset_range(&headers, Body::from_stream(stream), request)
        }
    }
}

fn put_prophet_error(request_id: Uuid, err: &client::ClientError) -> Response {
    if err.is(client::ErrorKind::NotFound) {
        return respond_error(request_id, StatusCode::NOT_FOUND, "Not found");
    }
    if err.is(client::ErrorKind::Forbidden) {
        return respond_error(request_id, StatusCode::FORBIDDEN, "Forbidden");
    }
    tracing::error!("asset from prophet: {err}");
    respond_error(request_id, StatusCode::BAD_GATEWAY, "Bad gateway")
}

async fn put_asset_response(res: AssetResponse, request: &HeaderMap) -> Response {
    let status = if res.status.as_u16() == 0 {
        StatusCode::OK
    } else {
        res.status
    };
    let mut headers = HeaderMap::new();
    copy_asset_headers(&res.header, &mut headers);
    if status == StatusCode::OK && get_asset_not_modified(&res.header, request) {
        headers.remove(CONTENT_LENGTH);
        let mut response = StatusCode::NOT_MODIFIED.into_response();
        *response.headers_mut() = headers;
        return response;
    }
    if status == StatusCode::OK
        && let Some(body) = res.body
    {
        return put_asset_range(&res.header, body, request);
    }
    let mut response = status.into_response();
    *response.headers_mut() = headers;
    if status == StatusCode::NOT_MODIFIED {
        return response;
    }
    if let Some(body) = res.body {
        *response.body_mut() = body;
    }
    response
}

fn put_asset_range(header: &HeaderMap, body: Body, request: &HeaderMap) -> Response {
    let size: i64 = header
        .get(CONTENT_LENGTH)
        .and_then(|h| h.to_str().ok())
        .and_then(|s| s.parse().ok())
        .unwrap_or(-1);
    let mut out = HeaderMap::new();
    copy_asset_headers(header, &mut out);
    if size >= 0 {
        out.insert(ACCEPT_RANGES, HeaderValue::from_static("bytes"));
    }
    let etag = header
        .get("ETag")
        .and_then(|h| h.to_str().ok())
        .unwrap_or("");
    let last_modified = header
        .get(LAST_MODIFIED)
        .and_then(|h| h.to_str().ok())
        .unwrap_or("");
    let r = get_asset_range(request, size, etag, last_modified);
    match r.status {
        StatusCode::RANGE_NOT_SATISFIABLE => {
            if let Ok(v) = HeaderValue::from_str(&r.get_content_range()) {
                out.insert(CONTENT_RANGE, v);
            }
            out.insert(CONTENT_LENGTH, HeaderValue::from_static("0"));
            let mut response = r.status.into_response();
            *response.headers_mut() = out;
            response
        }
        StatusCode::PARTIAL_CONTENT => {
            if let Ok(v) = HeaderValue::from_str(&r.get_content_range()) {
                out.insert(CONTENT_RANGE, v);
            }
            if let Ok(v) = HeaderValue::from_str(&r.length.to_string()) {
                out.insert(CONTENT_LENGTH, v);
            }
            let start = r.start as usize;
            let length = r.length as usize;
            let sliced = body.into_data_stream().scan(0usize, move |seen, chunk| {
                let chunk = match chunk {
                    Ok(c) => c,
                    Err(e) => return futures::future::ready(Some(Err(std::io::Error::other(e)))),
                };
                let begin = *seen;
                *seen += chunk.len();
                let end = *seen;
                let from = start.saturating_sub(begin).min(chunk.len());
                let to = (start + length).saturating_sub(begin).min(chunk.len());
                if end <= start || begin >= start + length || from >= to {
                    return futures::future::ready(Some(Ok(Bytes::new())));
                }
                futures::future::ready(Some(Ok(chunk.slice(from..to))))
            });
            let mut response = r.status.into_response();
            *response.headers_mut() = out;
            *response.body_mut() = Body::from_stream(sliced);
            response
        }
        _ => {
            let mut response = StatusCode::OK.into_response();
            *response.headers_mut() = out;
            *response.body_mut() = body;
            response
        }
    }
}

fn get_asset_not_modified(header: &HeaderMap, conditional: &HeaderMap) -> bool {
    if let Some(matched) = conditional
        .get("If-None-Match")
        .and_then(|h| h.to_str().ok())
        && !matched.is_empty()
    {
        let etag = header
            .get("ETag")
            .and_then(|h| h.to_str().ok())
            .unwrap_or("");
        if etag.is_empty() {
            return false;
        }
        return matched
            .split(',')
            .map(str::trim)
            .any(|c| c == "*" || c.trim_start_matches("W/") == etag.trim_start_matches("W/"));
    }
    let Some(since) = conditional
        .get("If-Modified-Since")
        .and_then(|h| h.to_str().ok())
        .and_then(httpdate::parse)
    else {
        return false;
    };
    let Some(modified) = header
        .get(LAST_MODIFIED)
        .and_then(|h| h.to_str().ok())
        .and_then(httpdate::parse)
    else {
        return false;
    };
    modified <= since
}

fn get_asset_storable(header: &HeaderMap) -> bool {
    let directives = get_cache_directives(header);
    !directives.contains_key("no-store") && !directives.contains_key("private")
}

fn get_asset_fresh(header: &HeaderMap, now: DateTime<Utc>) -> bool {
    let directives = get_cache_directives(header);
    for name in ["no-store", "no-cache", "private"] {
        if directives.contains_key(name) {
            return false;
        }
    }
    let Some(lifetime) = directives
        .get("s-maxage")
        .or_else(|| directives.get("max-age"))
    else {
        return false;
    };
    let Ok(seconds) = lifetime.parse::<i64>() else {
        return false;
    };
    let Some(date) = header
        .get("Date")
        .and_then(|h| h.to_str().ok())
        .and_then(httpdate::parse)
    else {
        return false;
    };
    let mut age = now - date;
    if let Some(v) = header
        .get("Age")
        .and_then(|h| h.to_str().ok())
        .and_then(|s| s.parse::<i64>().ok())
    {
        age += chrono::Duration::seconds(v);
    }
    age < chrono::Duration::seconds(seconds)
}

fn get_cache_directives(header: &HeaderMap) -> HashMap<String, String> {
    let mut directives = HashMap::new();
    let value = header
        .get(http::header::CACHE_CONTROL)
        .and_then(|h| h.to_str().ok())
        .unwrap_or("");
    for part in value.split(',') {
        let part = part.trim();
        let (name, value) = part.split_once('=').unwrap_or((part, ""));
        if name.is_empty() {
            continue;
        }
        directives.insert(name.to_lowercase(), value.trim_matches('"').to_string());
    }
    directives
}

pub fn content_type_of(headers: &HeaderMap) -> String {
    headers
        .get(CONTENT_TYPE)
        .and_then(|h| h.to_str().ok())
        .unwrap_or("")
        .to_string()
}

pub fn access_token_of(req: &Request) -> Option<String> {
    req.extensions()
        .get::<AccessTokenKey>()
        .map(|t| t.0.clone())
}
