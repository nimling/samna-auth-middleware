use axum::extract::Request;
use axum::response::{IntoResponse, Response};
use http::header::SET_COOKIE;
use http::{HeaderMap, StatusCode};
use serde_json::Value;

use super::me::{actor, ctx, query_value};
use super::respond::{get_request_id, respond_error, respond_json};
use crate::analytics;
use crate::client::{self, Forward};
use crate::shared::{
    AnalyticsApp, AnalyticsBatch, AnalyticsCapture, AnalyticsEvent, AnalyticsHeatPoint,
    AnalyticsSession, AnalyticsSummary, ConsentRequest, ConsentState,
};
use crate::smw::rate_limit::real_ip;
use crate::utils::cookie::write_cookie;

fn forward_context(req: &Request) -> Forward {
    let addr = req
        .extensions()
        .get::<axum::extract::ConnectInfo<std::net::SocketAddr>>()
        .map(|c| c.0);
    let mut f = Forward {
        ip: real_ip(req.headers(), addr),
        user_agent: req
            .headers()
            .get(http::header::USER_AGENT)
            .and_then(|h| h.to_str().ok())
            .unwrap_or("")
            .to_string(),
        user_id: String::new(),
    };
    if let Some(id) = actor(req) {
        f.user_id = id.to_string();
    }
    f
}

fn with_cookies(mut response: Response, cookies: HeaderMap) -> Response {
    for value in cookies.get_all(SET_COOKIE) {
        response.headers_mut().append(SET_COOKIE, value.clone());
    }
    response
}

pub async fn get_consent(req: Request) -> Response {
    let request_id = get_request_id(&req);
    let (id, cookie) =
        analytics::anonymous_id(req.headers(), query_value(&req, "anonymous_id").as_deref());
    let mut cookies = HeaderMap::new();
    if let Some(c) = cookie {
        write_cookie(&mut cookies, None, &c);
    }
    let forwarded = ctx(&req).with_forward(forward_context(&req));
    let encoded: String = url::form_urlencoded::byte_serialize(id.as_bytes()).collect();
    match client::get::<ConsentState>(&forwarded, &format!("consent?anonymous_id={encoded}")).await
    {
        Ok(mut state) => {
            if state.anonymous_id.is_empty() {
                state.anonymous_id = id;
            }
            with_cookies(respond_json(request_id, StatusCode::OK, state, 1), cookies)
        }
        Err(e) => {
            tracing::error!("GetConsent: {e}");
            with_cookies(
                respond_error(
                    request_id,
                    StatusCode::SERVICE_UNAVAILABLE,
                    "Consent service unavailable",
                ),
                cookies,
            )
        }
    }
}

async fn body_json<T: serde::de::DeserializeOwned>(req: Request) -> Result<(Request, T), Request> {
    let (parts, body) = req.into_parts();
    let Ok(bytes) = axum::body::to_bytes(body, 8 << 20).await else {
        return Err(Request::from_parts(parts, axum::body::Body::empty()));
    };
    match serde_json::from_slice::<T>(&bytes) {
        Ok(value) => Ok((Request::from_parts(parts, axum::body::Body::empty()), value)),
        Err(_) => Err(Request::from_parts(parts, axum::body::Body::empty())),
    }
}

pub async fn post_consent(req: Request) -> Response {
    let request_id = get_request_id(&req);
    let (req, mut body) = match body_json::<ConsentRequest>(req).await {
        Ok(v) => v,
        Err(_) => return respond_error(request_id, StatusCode::BAD_REQUEST, "Bad request"),
    };
    let mut cookies = HeaderMap::new();
    if body.anonymous_id.is_empty() {
        let (id, cookie) = analytics::anonymous_id(req.headers(), None);
        body.anonymous_id = id;
        if let Some(c) = cookie {
            write_cookie(&mut cookies, None, &c);
        }
    }
    if let Some(c) = analytics::write_anonymous_id(req.headers(), &body.anonymous_id) {
        write_cookie(&mut cookies, None, &c);
    }
    let forwarded = ctx(&req).with_forward(forward_context(&req));
    match client::post::<ConsentRequest, ConsentState>(&forwarded, "consent", &body).await {
        Ok(state) => with_cookies(respond_json(request_id, StatusCode::OK, state, 1), cookies),
        Err(e) => {
            tracing::error!("PostConsent: {e}");
            with_cookies(
                respond_error(
                    request_id,
                    StatusCode::BAD_GATEWAY,
                    "Consent service unavailable",
                ),
                cookies,
            )
        }
    }
}

fn passthrough_query(req: &Request) -> String {
    let allowed = [
        "app",
        "type",
        "path",
        "session_id",
        "visitor",
        "anonymous_id",
        "from",
        "to",
        "limit",
        "offset",
        "viewport_width",
    ];
    let mut q = url::form_urlencoded::Serializer::new(String::new());
    let mut any = false;
    for key in allowed {
        if let Some(v) = query_value(req, key)
            && !v.is_empty()
        {
            q.append_pair(key, &v);
            any = true;
        }
    }
    if !any {
        return String::new();
    }
    format!("?{}", q.finish())
}

async fn forward_list<T: serde::de::DeserializeOwned + serde::Serialize>(
    req: Request,
    path: &str,
    name: &str,
) -> Response {
    let request_id = get_request_id(&req);
    let forwarded = ctx(&req).with_forward(forward_context(&req));
    match client::get::<Vec<T>>(&forwarded, &format!("{path}{}", passthrough_query(&req))).await {
        Ok(items) => {
            let count = items.len() as i64;
            respond_json(request_id, StatusCode::OK, items, count)
        }
        Err(e) => {
            tracing::error!("{name}: {e}");
            respond_error(
                request_id,
                StatusCode::BAD_GATEWAY,
                "Analytics service unavailable",
            )
        }
    }
}

async fn forward_one<T: serde::de::DeserializeOwned + serde::Serialize>(
    req: Request,
    path: &str,
    name: &str,
) -> Response {
    let request_id = get_request_id(&req);
    let forwarded = ctx(&req).with_forward(forward_context(&req));
    match client::get::<T>(&forwarded, &format!("{path}{}", passthrough_query(&req))).await {
        Ok(item) => respond_json(request_id, StatusCode::OK, item, 1),
        Err(e) => {
            tracing::error!("{name}: {e}");
            respond_error(
                request_id,
                StatusCode::BAD_GATEWAY,
                "Analytics service unavailable",
            )
        }
    }
}

pub async fn get_analytics_apps(req: Request) -> Response {
    let request_id = get_request_id(&req);
    let forwarded = ctx(&req).with_forward(forward_context(&req));
    match client::get::<Vec<AnalyticsApp>>(&forwarded, "analytics/app").await {
        Ok(items) => {
            let count = items.len() as i64;
            respond_json(request_id, StatusCode::OK, items, count)
        }
        Err(e) => {
            tracing::error!("GetAnalyticsApps: {e}");
            respond_error(
                request_id,
                StatusCode::BAD_GATEWAY,
                "Analytics service unavailable",
            )
        }
    }
}

pub async fn get_analytics_events(req: Request) -> Response {
    forward_list::<AnalyticsEvent>(req, "analytics/event", "GetAnalyticsEvents").await
}

pub async fn get_analytics_sessions(req: Request) -> Response {
    forward_list::<AnalyticsSession>(req, "analytics/session", "GetAnalyticsSessions").await
}

pub async fn post_analytics(req: Request) -> Response {
    let (req, mut body) = match body_json::<AnalyticsBatch>(req).await {
        Ok(v) => v,
        Err(_) => return StatusCode::NO_CONTENT.into_response(),
    };
    let mut cookies = HeaderMap::new();
    if body.anonymous_id.is_empty() {
        let (id, cookie) = analytics::anonymous_id(req.headers(), None);
        body.anonymous_id = id;
        if let Some(c) = cookie {
            write_cookie(&mut cookies, None, &c);
        }
    }
    if body.anonymous_id.is_empty() || body.session_key.is_empty() {
        return with_cookies(StatusCode::NO_CONTENT.into_response(), cookies);
    }
    let forwarded = ctx(&req).with_forward(forward_context(&req));
    if let Err(e) = client::post::<AnalyticsBatch, Value>(&forwarded, "analytics", &body).await {
        tracing::error!("PostAnalytics: {e}");
    }
    with_cookies(StatusCode::NO_CONTENT.into_response(), cookies)
}

pub async fn post_analytics_capture(req: Request) -> Response {
    let request_id = get_request_id(&req);
    if query_value(&req, "path").is_none_or(|p| p.is_empty()) {
        return StatusCode::NO_CONTENT.into_response();
    }
    let content_type = req
        .headers()
        .get(http::header::CONTENT_TYPE)
        .and_then(|h| h.to_str().ok())
        .unwrap_or("")
        .to_string();
    let forward = forward_context(&req);
    let passthrough = passthrough_query(&req);
    let ctx_value = ctx(&req);
    let Ok(body) = axum::body::to_bytes(req.into_body(), analytics::CAPTURE_BYTES_MAX + 1).await
    else {
        return StatusCode::NO_CONTENT.into_response();
    };
    if body.is_empty() {
        return StatusCode::NO_CONTENT.into_response();
    }
    if body.len() > analytics::CAPTURE_BYTES_MAX {
        return respond_error(
            request_id,
            StatusCode::PAYLOAD_TOO_LARGE,
            "snapshot is too large",
        );
    }
    let forwarded = ctx_value
        .with_forward(forward)
        .with_content_type(&content_type);
    if let Err(e) = client::http::post_raw::<Value>(
        &forwarded,
        &format!("analytics/capture{passthrough}"),
        body.to_vec(),
    )
    .await
    {
        tracing::error!("PostAnalyticsCapture: {e}");
    }
    StatusCode::NO_CONTENT.into_response()
}

pub async fn get_analytics_summary(req: Request) -> Response {
    forward_one::<AnalyticsSummary>(req, "analytics/summary", "GetAnalyticsSummary").await
}

pub async fn get_analytics_capture(req: Request) -> Response {
    forward_one::<AnalyticsCapture>(req, "analytics/capture", "GetAnalyticsCapture").await
}

pub async fn get_analytics_heat(req: Request) -> Response {
    forward_list::<AnalyticsHeatPoint>(req, "analytics/heat", "GetAnalyticsHeat").await
}
