use std::sync::atomic::{AtomicU16, Ordering};
use std::sync::{Arc, Mutex};
use std::time::Duration;

use http::{HeaderMap, HeaderValue, Method, StatusCode};
use serde::Serialize;
use serde::de::DeserializeOwned;
use serde_json::Value;

use super::main::new_client;
use super::models::ClientError;
use super::utils::{generate_request_id, join_url, local_ip};
use crate::utils::cfg;

pub fn request_timeout() -> Duration {
    if let Ok(n) = cfg().client_timeout.parse::<u64>()
        && n > 0
    {
        return Duration::from_secs(n);
    }
    Duration::from_secs(5)
}

#[derive(Clone, Debug, Default, serde::Deserialize)]
pub struct ResponseEnvelope {
    #[serde(default)]
    pub data: Value,
    #[serde(default)]
    pub error: Option<ResponseError>,
}

#[derive(Clone, Debug, Default, serde::Deserialize)]
pub struct ResponseError {
    #[serde(default, alias = "Message")]
    pub message: String,
}

#[derive(Clone, Debug, Default)]
pub struct Forward {
    pub ip: String,
    pub user_agent: String,
    pub user_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct ClientCredentials {
    pub id: String,
    pub secret: String,
}

pub const HEADER_SYNC_SEQ: &str = "SAuth-Seq";

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
pub struct SyncCursor {
    pub txid: i64,
    pub seq: i64,
}

impl SyncCursor {
    pub fn less(&self, o: SyncCursor) -> bool {
        self.txid < o.txid || (self.txid == o.txid && self.seq < o.seq)
    }

    pub fn is_zero(&self) -> bool {
        self.txid == 0 && self.seq == 0
    }
}

impl std::fmt::Display for SyncCursor {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}.{}", self.txid, self.seq)
    }
}

pub fn parse_sync_cursor(s: &str) -> Result<SyncCursor, String> {
    let Some((txid, seq)) = s.split_once('.') else {
        return Err(format!("sync cursor {s:?}: want <txid>.<seq>"));
    };
    let t = txid
        .parse::<i64>()
        .ok()
        .filter(|t| *t >= 0)
        .ok_or_else(|| format!("sync cursor {s:?}: bad txid"))?;
    let q = seq
        .parse::<i64>()
        .ok()
        .filter(|q| *q >= 0)
        .ok_or_else(|| format!("sync cursor {s:?}: bad seq"))?;
    Ok(SyncCursor { txid: t, seq: q })
}

pub const HEADER_PROPHET: &str = "SAuth-Prophet";
pub const HEADER_HOME: &str = "SAuth-Home";
pub const HEADER_PEER_SECRET: &str = "SAuth-Peer-Secret";

#[derive(Clone, Debug, Default)]
pub struct Target {
    pub endpoint: String,
    pub slug: String,
    pub secret: String,
    pub peer_secret: String,
}

#[derive(Debug, Default)]
pub struct RequestSeq {
    seq: Mutex<SyncCursor>,
    pub timeout: Mutex<Duration>,
}

impl RequestSeq {
    pub fn get(&self) -> SyncCursor {
        *self.seq.lock().expect("seq")
    }

    pub fn put(&self, v: SyncCursor) {
        let mut held = self.seq.lock().expect("seq");
        if held.less(v) {
            *held = v;
        }
    }

    pub fn timeout(&self) -> Duration {
        *self.timeout.lock().expect("timeout")
    }

    pub fn set_timeout(&self, d: Duration) {
        *self.timeout.lock().expect("timeout") = d;
    }
}

#[derive(Clone, Debug, Default)]
pub struct Ctx {
    pub forward: Option<Forward>,
    pub content_type: Option<String>,
    pub client_credentials: Option<ClientCredentials>,
    pub prophet: Option<Arc<Mutex<String>>>,
    pub target: Option<Target>,
    pub sync_seq: Option<Arc<Mutex<SyncCursor>>>,
    pub request_seq: Option<Arc<RequestSeq>>,
    pub status: Option<Arc<AtomicU16>>,
    pub timeout: Option<Duration>,
    pub code_hash: Option<String>,
}

impl Ctx {
    pub fn new() -> Self {
        Ctx::default()
    }

    pub fn with_forward(mut self, f: Forward) -> Self {
        self.forward = Some(f);
        self
    }

    pub fn with_content_type(mut self, content_type: &str) -> Self {
        self.content_type = Some(content_type.to_string());
        self
    }

    pub fn with_client_credentials(mut self, c: ClientCredentials) -> Self {
        self.client_credentials = Some(c);
        self
    }

    pub fn with_prophet(mut self, prophet: Arc<Mutex<String>>) -> Self {
        self.prophet = Some(prophet);
        self
    }

    pub fn with_target(mut self, t: Target) -> Self {
        self.target = Some(t);
        self
    }

    pub fn with_sync_seq(mut self, seq: Arc<Mutex<SyncCursor>>) -> Self {
        self.sync_seq = Some(seq);
        self
    }

    pub fn with_request_seq(mut self) -> (Self, Arc<RequestSeq>, bool) {
        if let Some(r) = &self.request_seq {
            return (self.clone(), r.clone(), false);
        }
        let r = Arc::new(RequestSeq::default());
        self.request_seq = Some(r.clone());
        (self, r, true)
    }

    pub fn with_status(mut self, status: Arc<AtomicU16>) -> Self {
        self.status = Some(status);
        self
    }

    pub fn with_timeout(mut self, d: Duration) -> Self {
        self.timeout = Some(d);
        self
    }

    pub fn with_code_hash(mut self, hash: &str) -> Self {
        if !hash.is_empty() {
            self.code_hash = Some(hash.to_string());
        }
        self
    }

    pub fn code_hash(&self) -> String {
        self.code_hash.clone().unwrap_or_default()
    }

    fn content_type_from(&self) -> String {
        self.content_type
            .clone()
            .filter(|c| !c.is_empty())
            .unwrap_or_else(|| "application/json".to_string())
    }
}

pub fn decode_json<T: DeserializeOwned>(body: &[u8]) -> Result<T, ClientError> {
    let envelope: ResponseEnvelope = serde_json::from_slice(body)
        .map_err(|e| ClientError::other(format!("decode envelope: {e}")))?;
    serde_json::from_value(envelope.data)
        .map_err(|e| ClientError::other(format!("decode data: {e}")))
}

fn decode_into<T: DeserializeOwned>(body: &[u8]) -> Result<T, ClientError> {
    let envelope: ResponseEnvelope = serde_json::from_slice(body)
        .map_err(|e| ClientError::other(format!("decode envelope: {e}")))?;
    if envelope.data.is_null() {
        return serde_json::from_value(Value::Null)
            .map_err(|e| ClientError::other(format!("decode data: {e}")));
    }
    serde_json::from_value(envelope.data)
        .map_err(|e| ClientError::other(format!("decode data: {e}")))
}

fn http_client(timeout: Duration) -> reqwest::Client {
    reqwest::Client::builder()
        .timeout(timeout)
        .build()
        .expect("http client")
}

pub(super) enum Body {
    None,
    Raw(Vec<u8>),
}

fn json_body<P: Serialize>(payload: &P) -> Result<Body, ClientError> {
    serde_json::to_vec(payload)
        .map(Body::Raw)
        .map_err(|e| ClientError::other(format!("marshal request body: {e}")))
}

async fn send(
    ctx: &Ctx,
    hc: &reqwest::Client,
    method: Method,
    body: Body,
    target_url: &str,
    header: Option<&HeaderMap>,
) -> Result<reqwest::Response, ClientError> {
    let c = new_client(ctx);
    let mut req = hc.request(method, target_url);
    let payload = match body {
        Body::None => None,
        Body::Raw(raw) => Some(raw),
    };
    req = req
        .header("X-Request-ID", generate_request_id())
        .header("X-Client-IP", local_ip())
        .header("SAuth-App-Slug", &c.slug);
    if let Some(cc) = &ctx.client_credentials
        && !cc.id.is_empty()
        && !cc.secret.is_empty()
    {
        req = req.basic_auth(&cc.id, Some(&cc.secret));
    } else {
        req = req.header("SAuth-App-Secret", &c.secret);
    }
    if let Some(t) = &ctx.target
        && !t.peer_secret.is_empty()
    {
        req = req.header(HEADER_PEER_SECRET, &t.peer_secret);
    }
    if let Some(f) = &ctx.forward {
        if !f.ip.is_empty() {
            req = req.header("SAuth-Forwarded-For", &f.ip);
        }
        if !f.user_agent.is_empty() {
            req = req.header("SAuth-Forwarded-Agent", &f.user_agent);
        }
        if !f.user_id.is_empty() {
            req = req.header("SAuth-Forwarded-User", &f.user_id);
        }
    }
    if let Some(header) = header {
        for (key, value) in header {
            req = req.header(key, value);
        }
    }
    if let Some(payload) = payload {
        req = req
            .header("Content-Type", ctx.content_type_from())
            .body(payload);
    }
    let resp = req
        .send()
        .await
        .map_err(|e| ClientError::other(format!("send request: {e}")))?;
    let status = resp.status();
    if let Some(seq) = &ctx.sync_seq
        && status.is_success()
        && let Some(v) = resp
            .headers()
            .get(HEADER_SYNC_SEQ)
            .and_then(|h| h.to_str().ok())
        && let Ok(v) = parse_sync_cursor(v)
    {
        let mut held = seq.lock().expect("seq");
        if held.less(v) {
            *held = v;
        }
        if let Some(r) = &ctx.request_seq {
            r.put(v);
        }
    }
    if let Some(s) = &ctx.status {
        s.store(status.as_u16(), Ordering::Relaxed);
    }
    if let Some(p) = &ctx.prophet
        && status.is_success()
    {
        *p.lock().expect("prophet") = resp
            .headers()
            .get(HEADER_PROPHET)
            .and_then(|h| h.to_str().ok())
            .unwrap_or("")
            .to_string();
    }
    Ok(resp)
}

async fn response_body(resp: reqwest::Response) -> Result<Vec<u8>, ClientError> {
    let status = resp.status();
    match status {
        s if s.is_success() => resp
            .bytes()
            .await
            .map(|b| b.to_vec())
            .map_err(|e| ClientError::other(format!("read response: {e}"))),
        StatusCode::UNAUTHORIZED => Err(ClientError::unauthorized()),
        StatusCode::FORBIDDEN => Err(ClientError::forbidden()),
        StatusCode::NOT_FOUND => Err(ClientError::not_found()),
        StatusCode::CONFLICT if resp.headers().get(HEADER_HOME).is_some() => {
            let home = resp
                .headers()
                .get(HEADER_HOME)
                .and_then(|h| h.to_str().ok())
                .unwrap_or("")
                .to_string();
            Err(ClientError::not_home(&home))
        }
        StatusCode::CONFLICT => Err(ClientError::conflict()),
        StatusCode::BAD_REQUEST => Err(refusal(resp).await),
        other => Err(ClientError::other(format!(
            "unexpected status code: {}",
            other.as_u16()
        ))),
    }
}

async fn refusal(resp: reqwest::Response) -> ClientError {
    let sentinel = ClientError::bad_request();
    let Ok(body) = resp.bytes().await else {
        return sentinel;
    };
    let Ok(envelope) = serde_json::from_slice::<ResponseEnvelope>(&body) else {
        return sentinel;
    };
    match envelope.error {
        Some(e) if !e.message.is_empty() => ClientError::new(
            sentinel.kind,
            format!("{}: {}", sentinel.message, e.message),
        ),
        _ => sentinel,
    }
}

async fn execute(
    ctx: &Ctx,
    method: Method,
    body: Body,
    path: &str,
) -> Result<Vec<u8>, ClientError> {
    let c = new_client(ctx);
    let u =
        join_url(&c.api_url, &[path]).map_err(|e| ClientError::other(format!("build url: {e}")))?;
    let hc = http_client(ctx.timeout.unwrap_or_else(request_timeout));
    let resp = send(ctx, &hc, method, body, &u, None).await?;
    response_body(resp).await
}

pub async fn get<T: DeserializeOwned>(ctx: &Ctx, path: &str) -> Result<T, ClientError> {
    let body = execute(ctx, Method::GET, Body::None, path).await?;
    decode_into(&body)
}

pub async fn get_stream(
    ctx: &Ctx,
    path: &str,
    header: &HeaderMap,
) -> Result<reqwest::Response, ClientError> {
    let c = new_client(ctx);
    let u =
        join_url(&c.api_url, &[path]).map_err(|e| ClientError::other(format!("build url: {e}")))?;
    let hc = reqwest::Client::builder().build().expect("http client");
    let resp = send(ctx, &hc, Method::GET, Body::None, &u, Some(header)).await?;
    let status = resp.status();
    if status == StatusCode::NOT_MODIFIED
        || status == StatusCode::RANGE_NOT_SATISFIABLE
        || status.is_success()
    {
        return Ok(resp);
    }
    response_body(resp).await?;
    Err(ClientError::other("unexpected status"))
}

pub async fn post<P: Serialize, T: DeserializeOwned>(
    ctx: &Ctx,
    path: &str,
    payload: &P,
) -> Result<T, ClientError> {
    let body = execute(ctx, Method::POST, json_body(payload)?, path).await?;
    decode_into(&body)
}

pub async fn post_raw<T: DeserializeOwned>(
    ctx: &Ctx,
    path: &str,
    payload: Vec<u8>,
) -> Result<T, ClientError> {
    let body = execute(ctx, Method::POST, Body::Raw(payload), path).await?;
    decode_into(&body)
}

pub async fn put<P: Serialize, T: DeserializeOwned>(
    ctx: &Ctx,
    path: &str,
    payload: &P,
) -> Result<T, ClientError> {
    let body = execute(ctx, Method::PUT, json_body(payload)?, path).await?;
    decode_into(&body)
}

pub async fn patch<P: Serialize, T: DeserializeOwned>(
    ctx: &Ctx,
    path: &str,
    payload: &P,
) -> Result<T, ClientError> {
    let body = execute(ctx, Method::PATCH, json_body(payload)?, path).await?;
    decode_into(&body)
}

pub async fn delete<P: Serialize, T: DeserializeOwned>(
    ctx: &Ctx,
    path: &str,
    payload: Option<&P>,
) -> Result<T, ClientError> {
    let body = match payload {
        Some(p) => execute(ctx, Method::DELETE, json_body(p)?, path).await?,
        None => execute(ctx, Method::DELETE, Body::None, path).await?,
    };
    decode_into(&body)
}

pub const ASSET_HEADERS: [&str; 7] = [
    "Content-Type",
    "Content-Length",
    "ETag",
    "Last-Modified",
    "Cache-Control",
    "Accept-Ranges",
    "Content-Range",
];
pub const CONDITIONAL_HEADERS: [&str; 2] = ["If-None-Match", "If-Modified-Since"];
pub const RANGE_HEADERS: [&str; 2] = ["Range", "If-Range"];

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct AssetRange {
    pub status: StatusCode,
    pub start: i64,
    pub length: i64,
    pub size: i64,
}

impl AssetRange {
    pub fn get_content_range(&self) -> String {
        if self.status == StatusCode::RANGE_NOT_SATISFIABLE {
            return format!("bytes */{}", self.size);
        }
        format!(
            "bytes {}-{}/{}",
            self.start,
            self.start + self.length - 1,
            self.size
        )
    }
}

fn header_str<'a>(headers: &'a HeaderMap, name: &str) -> &'a str {
    headers
        .get(name)
        .and_then(|h| h.to_str().ok())
        .unwrap_or("")
}

pub fn get_asset_range(
    request: &HeaderMap,
    size: i64,
    etag: &str,
    last_modified: &str,
) -> AssetRange {
    let whole = AssetRange {
        status: StatusCode::OK,
        start: 0,
        length: size,
        size,
    };
    let value = header_str(request, "Range").trim();
    if value.is_empty() || size < 0 {
        return whole;
    }
    let condition = header_str(request, "If-Range").trim();
    if !condition.is_empty() {
        if condition.starts_with('"') {
            if condition != etag {
                return whole;
            }
        } else {
            let since = httpdate::parse(condition);
            let modified = httpdate::parse(last_modified);
            match (since, modified) {
                (Some(s), Some(m)) if s == m => {}
                _ => return whole,
            }
        }
    }
    let Some(spec) = value.strip_prefix("bytes=") else {
        return whole;
    };
    if spec.contains(',') {
        return whole;
    }
    let Some((first, last)) = spec.trim().split_once('-') else {
        return whole;
    };
    let unsatisfiable = AssetRange {
        status: StatusCode::RANGE_NOT_SATISFIABLE,
        start: 0,
        length: 0,
        size,
    };
    if first.is_empty() {
        let Ok(suffix) = last.parse::<i64>() else {
            return whole;
        };
        if suffix < 0 {
            return whole;
        }
        if suffix == 0 || size == 0 {
            return unsatisfiable;
        }
        let suffix = suffix.min(size);
        return AssetRange {
            status: StatusCode::PARTIAL_CONTENT,
            start: size - suffix,
            length: suffix,
            size,
        };
    }
    let Ok(start) = first.parse::<i64>() else {
        return whole;
    };
    if start < 0 {
        return whole;
    }
    let mut end = size - 1;
    if !last.is_empty() {
        let Ok(stop) = last.parse::<i64>() else {
            return whole;
        };
        if stop < start {
            return whole;
        }
        end = end.min(stop);
    }
    if start >= size {
        return unsatisfiable;
    }
    AssetRange {
        status: StatusCode::PARTIAL_CONTENT,
        start,
        length: end - start + 1,
        size,
    }
}

pub mod httpdate {
    use chrono::{DateTime, NaiveDateTime, Utc};

    pub fn parse(text: &str) -> Option<DateTime<Utc>> {
        if text.is_empty() {
            return None;
        }
        if let Ok(t) = DateTime::parse_from_rfc2822(text) {
            return Some(t.with_timezone(&Utc));
        }
        for layout in [
            "%a, %d %b %Y %H:%M:%S GMT",
            "%A, %d-%b-%y %H:%M:%S GMT",
            "%a %b %e %H:%M:%S %Y",
        ] {
            if let Ok(t) = NaiveDateTime::parse_from_str(text, layout) {
                return Some(t.and_utc());
            }
        }
        None
    }

    pub fn format(t: DateTime<Utc>) -> String {
        t.format("%a, %d %b %Y %H:%M:%S GMT").to_string()
    }
}

pub fn header_value(text: &str) -> Option<HeaderValue> {
    HeaderValue::from_str(text).ok()
}
