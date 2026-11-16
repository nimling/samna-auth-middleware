use axum::extract::Request;
use axum::response::Response;
use http::StatusCode;

use super::respond::{get_request_id, respond_error, respond_json};
use crate::smw::CodeKey;

pub async fn get_code(req: Request) -> Response {
    let request_id = get_request_id(&req);
    match req.extensions().get::<CodeKey>() {
        Some(access) => respond_json(request_id, StatusCode::OK, access.0.code.clone(), 1),
        None => respond_error(request_id, StatusCode::NOT_FOUND, "Not found"),
    }
}
