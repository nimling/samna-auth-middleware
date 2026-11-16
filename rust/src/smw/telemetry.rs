use axum::extract::Request;
use axum::middleware::Next;
use axum::response::Response;
use uuid::Uuid;

use crate::utils::cfg;

pub async fn telemetry_middleware(req: Request, next: Next) -> Response {
    let span = tracing::info_span!("http.request", app = %cfg().app_slug, method = %req.method(), path = %req.uri().path());
    let _entered = span.enter();
    drop(_entered);
    tracing::Instrument::instrument(next.run(req), span).await
}

pub fn organization(root_id: Uuid, owner_id: Uuid) {
    tracing::Span::current().record("samna.root_id", tracing::field::display(root_id));
    tracing::Span::current().record("samna.owner_id", tracing::field::display(owner_id));
}

pub fn stamp_organization(root_id: Uuid, owner_id: Uuid) {
    organization(root_id, owner_id);
}
