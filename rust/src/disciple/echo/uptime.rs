use axum::extract::Request;
use axum::response::Response;
use http::StatusCode;

use super::respond::{get_request_id, respond_error, respond_json};
use crate::client;
use crate::uptime::{self, Health, HealthStatus, MonitorStatus};
use crate::utils::cfg;

fn status_target() -> String {
    format!("/api/app/{}/status", cfg().app_slug)
}

pub async fn get_health(req: Request) -> Response {
    let request_id = get_request_id(&req);
    let health = Health {
        status: HealthStatus::OK.to_string(),
        app: cfg().app_slug,
        version: std::env::var(uptime::VERSION_ENV).unwrap_or_default(),
    };
    respond_json(request_id, StatusCode::OK, health, 1)
}

pub async fn get_uptime(req: Request) -> Response {
    let request_id = get_request_id(&req);
    match uptime::get_heartbeat() {
        Some(heartbeat) => respond_json(request_id, StatusCode::OK, heartbeat, 1),
        None => respond_error(
            request_id,
            StatusCode::SERVICE_UNAVAILABLE,
            "No heartbeat reported yet",
        ),
    }
}

pub async fn get_uptime_status(req: Request) -> Response {
    let request_id = get_request_id(&req);
    let ctx = super::me::ctx(&req);
    match client::get::<MonitorStatus>(&ctx, &status_target()).await {
        Ok(status) => respond_json(request_id, StatusCode::OK, status, 1),
        Err(e) => {
            tracing::error!("GetUptimeStatus: {e}");
            respond_error(
                request_id,
                StatusCode::BAD_GATEWAY,
                "Status service unavailable",
            )
        }
    }
}
