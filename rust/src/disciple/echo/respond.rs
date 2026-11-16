use axum::extract::Request;
use axum::response::Response;
use http::StatusCode;
use serde::Serialize;
use uuid::Uuid;

use crate::respond;

#[derive(Clone, Copy, Debug)]
pub struct RequestId(pub Uuid);

pub fn get_request_id(req: &Request) -> Uuid {
    req.extensions()
        .get::<RequestId>()
        .map(|r| r.0)
        .unwrap_or_else(Uuid::new_v4)
}

pub fn request_id(parts: &http::request::Parts) -> Uuid {
    parts
        .extensions
        .get::<RequestId>()
        .map(|r| r.0)
        .unwrap_or_else(Uuid::new_v4)
}

pub fn redirect(status: StatusCode, url: &str) -> Response {
    respond::redirect(status, url)
}

pub fn respond_error(request_id: Uuid, status: StatusCode, message: &str) -> Response {
    respond::error(request_id, status, message)
}

pub fn respond_json<T: Serialize>(
    request_id: Uuid,
    status: StatusCode,
    data: T,
    count: i64,
) -> Response {
    respond::json(request_id, status, data, count)
}

pub fn set_no_store(response: &mut Response) {
    response.headers_mut().insert(
        http::header::CACHE_CONTROL,
        http::HeaderValue::from_static("no-store"),
    );
    response.headers_mut().insert(
        http::header::PRAGMA,
        http::HeaderValue::from_static("no-cache"),
    );
}
