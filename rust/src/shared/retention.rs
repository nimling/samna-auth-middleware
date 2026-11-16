use std::collections::BTreeMap;
use std::sync::Arc;
use std::time::Duration;

use super::db::Db;
use super::error::Result;
use crate::client::Ctx;

pub type Retention = BTreeMap<String, i64>;

pub fn retention_env() -> Retention {
    let mut r = Retention::new();
    if let Ok(v) = std::env::var("SAUTH_RETENTION")
        && !v.is_empty()
        && let Ok(parsed) = serde_json::from_str::<Retention>(&v)
    {
        r = parsed;
    }
    r
}

impl Db {
    pub async fn prune(&self, ctx: &Ctx, r: &Retention) -> Result<i64> {
        let settings = serde_json::to_value(r)?;
        self.query_scalar::<i64>(ctx, "SELECT claimius.prune($1::jsonb)", &[&settings])
            .await
    }
}

pub async fn prune_loop(stop: tokio_util::sync::CancellationToken, db: Arc<Db>) {
    let mut ticker = tokio::time::interval(Duration::from_secs(3600));
    loop {
        let mut r = retention_env();
        let override_fn = db.retention.lock().expect("retention").clone();
        if let Some(f) = override_fn {
            for (key, v) in f() {
                r.insert(key, v);
            }
        }
        match db.prune(&Ctx::new(), &r).await {
            Err(e) if !stop.is_cancelled() => tracing::error!("retention: {e}"),
            Err(_) => return,
            Ok(n) if n > 0 => tracing::info!("retention: {n} rows deleted"),
            Ok(_) => {}
        }
        tokio::select! {
            _ = stop.cancelled() => return,
            _ = ticker.tick() => {}
        }
    }
}
