use axum::extract::{Path, Request};
use axum::response::Response;
use http::StatusCode;
use uuid::Uuid;

use super::me::{actor, ctx};
use super::respond::{get_request_id, respond_error, respond_json};
use crate::disciple::sdb;
use crate::notification;
use crate::shared::Notification;

pub async fn get_notifications(req: Request) -> Response {
    let request_id = get_request_id(&req);
    let Some(user_id) = actor(&req) else {
        return respond_error(request_id, StatusCode::UNAUTHORIZED, "Unauthorized");
    };
    if req
        .headers()
        .get(http::header::ACCEPT)
        .and_then(|h| h.to_str().ok())
        .is_some_and(|a| a.contains("text/event-stream"))
    {
        return notification::stream(user_id);
    }
    let Some(db) = sdb::instance() else {
        return respond_error(
            request_id,
            StatusCode::INTERNAL_SERVER_ERROR,
            "Internal server error",
        );
    };
    match db.query_models::<Notification>(&ctx(&req), "SELECT * FROM claimius.notification WHERE user_id = $1 AND sa_deleted_at IS NULL ORDER BY sa_created_at DESC", &[&user_id]).await {
        Ok(items) => {
            let count = items.len() as i64;
            respond_json(request_id, StatusCode::OK, items, count)
        }
        Err(e) => {
            tracing::error!("GetNotifications: {e}");
            respond_error(request_id, StatusCode::INTERNAL_SERVER_ERROR, "Internal server error")
        }
    }
}

pub async fn put_notification_read(Path(id): Path<String>, req: Request) -> Response {
    set_notification_time(req, &id, "read_at").await
}

pub async fn put_notification_closed(Path(id): Path<String>, req: Request) -> Response {
    set_notification_time(req, &id, "closed_at").await
}

pub async fn delete_notification(Path(id): Path<String>, req: Request) -> Response {
    let request_id = get_request_id(&req);
    let Some(user_id) = actor(&req) else {
        return respond_error(request_id, StatusCode::UNAUTHORIZED, "Unauthorized");
    };
    let Ok(id) = Uuid::parse_str(&id) else {
        return respond_error(request_id, StatusCode::BAD_REQUEST, "id must be a uuid");
    };
    let Some(db) = sdb::instance() else {
        return respond_error(
            request_id,
            StatusCode::INTERNAL_SERVER_ERROR,
            "Internal server error",
        );
    };
    match db.execute(&ctx(&req), "UPDATE claimius.notification SET sa_deleted_at = NOW() WHERE id = $1 AND user_id = $2 AND sa_deleted_at IS NULL", &[&id, &user_id]).await {
        Ok(0) => respond_error(request_id, StatusCode::NOT_FOUND, "Not found"),
        Ok(_) => respond_json(request_id, StatusCode::OK, serde_json::json!({ "id": id.to_string() }), 1),
        Err(e) => {
            tracing::error!("DeleteNotification: {e}");
            respond_error(request_id, StatusCode::INTERNAL_SERVER_ERROR, "Internal server error")
        }
    }
}

async fn set_notification_time(req: Request, id: &str, column: &str) -> Response {
    let request_id = get_request_id(&req);
    let Some(user_id) = actor(&req) else {
        return respond_error(request_id, StatusCode::UNAUTHORIZED, "Unauthorized");
    };
    let Ok(id) = Uuid::parse_str(id) else {
        return respond_error(request_id, StatusCode::BAD_REQUEST, "id must be a uuid");
    };
    let Some(db) = sdb::instance() else {
        return respond_error(
            request_id,
            StatusCode::INTERNAL_SERVER_ERROR,
            "Internal server error",
        );
    };
    let sql = format!(
        "UPDATE claimius.notification SET \"{column}\" = NOW() WHERE id = $1 AND user_id = $2 AND sa_deleted_at IS NULL RETURNING *"
    );
    match db
        .query_model_opt::<Notification>(&ctx(&req), &sql, &[&id, &user_id])
        .await
    {
        Ok(Some(updated)) => respond_json(request_id, StatusCode::OK, updated, 1),
        Ok(None) => respond_error(request_id, StatusCode::NOT_FOUND, "Not found"),
        Err(e) => {
            tracing::error!("setNotificationTime {column}: {e}");
            respond_error(
                request_id,
                StatusCode::INTERNAL_SERVER_ERROR,
                "Internal server error",
            )
        }
    }
}
