use std::sync::Arc;
use std::time::Duration;

use axum::extract::{Request, State};
use axum::middleware::Next;
use axum::response::Response;

use super::main::RequestCtx;
use crate::disciple::sdb;

pub const SEQ_TIMEOUT: Duration = Duration::from_secs(10);

pub struct WaitForSeq {
    pub timeout: Duration,
}

impl WaitForSeq {
    pub fn new(timeout: Duration) -> Arc<WaitForSeq> {
        Arc::new(WaitForSeq {
            timeout: if timeout.is_zero() {
                SEQ_TIMEOUT
            } else {
                timeout
            },
        })
    }
}

pub async fn wait_for_seq(
    State(wait): State<Arc<WaitForSeq>>,
    mut req: Request,
    next: Next,
) -> Response {
    let Some(db) = sdb::instance() else {
        return next.run(req).await;
    };
    if !db.route_to_prophet {
        return next.run(req).await;
    }
    let ctx = req
        .extensions()
        .get::<RequestCtx>()
        .map(|c| c.0.clone())
        .unwrap_or_default();
    let (ctx, seq, fresh) = ctx.with_request_seq();
    if !fresh {
        return next.run(req).await;
    }
    seq.set_timeout(wait.timeout);
    req.extensions_mut().insert(RequestCtx(ctx.clone()));
    let response = next.run(req).await;
    let target = seq.get();
    if !target.is_zero()
        && let Err(e) = db.wait_for_seq(&ctx, target, wait.timeout).await
    {
        tracing::warn!("routed write {target} not yet applied, answering anyway: {e}");
    }
    response
}
