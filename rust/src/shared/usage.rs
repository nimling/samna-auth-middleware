use std::collections::HashMap;
use std::sync::{Arc, Mutex};
use std::time::Duration;

use deadpool_postgres::Pool;
use serde::{Deserialize, Serialize};
use tokio::sync::Notify;
use uuid::Uuid;

use super::db::Db;
use super::error::{Error, Result};
use super::models::{Model, ObjectType};
use crate::client::Ctx;
use crate::sauth::Hidden;

const USAGE_FLUSH_INTERVAL: Duration = Duration::from_secs(10);
const USAGE_FLUSH_KEYS: usize = 1000;
const USAGE_FLUSH_TIMEOUT: Duration = Duration::from_secs(30);
const USAGE_FLUSH_ATTEMPTS: u32 = 3;
const USAGE_FLUSH_BACKOFF: Duration = Duration::from_millis(100);

#[derive(Clone, Debug, PartialEq, Eq, Hash, PartialOrd, Ord)]
struct UsageKey {
    user_id: Uuid,
    object_type: String,
    object_id: Uuid,
}

struct State {
    counts: HashMap<UsageKey, i64>,
    dropped: i64,
}

pub struct ObjectUsage {
    state: Arc<Mutex<State>>,
    limit: usize,
    full: Arc<Notify>,
    stop: tokio_util::sync::CancellationToken,
    done: Mutex<Option<tokio::task::JoinHandle<()>>>,
}

impl ObjectUsage {
    pub fn start(pool: Pool) -> ObjectUsage {
        let state = Arc::new(Mutex::new(State {
            counts: HashMap::new(),
            dropped: 0,
        }));
        let full = Arc::new(Notify::new());
        let stop = tokio_util::sync::CancellationToken::new();
        let handle = {
            let state = state.clone();
            let full = full.clone();
            let stop = stop.clone();
            tokio::spawn(async move {
                let mut ticker = tokio::time::interval(USAGE_FLUSH_INTERVAL);
                ticker.tick().await;
                loop {
                    tokio::select! {
                        _ = ticker.tick() => flush(&pool, &state, USAGE_FLUSH_KEYS).await,
                        _ = full.notified() => flush(&pool, &state, USAGE_FLUSH_KEYS).await,
                        _ = stop.cancelled() => {
                            flush(&pool, &state, USAGE_FLUSH_KEYS).await;
                            return;
                        }
                    }
                }
            })
        };
        ObjectUsage {
            state,
            limit: USAGE_FLUSH_KEYS,
            full,
            stop,
            done: Mutex::new(Some(handle)),
        }
    }

    pub fn add(&self, user_id: Uuid, object_type: &str, object_id: Uuid) {
        if user_id.is_nil() || object_id.is_nil() || object_type.is_empty() {
            return;
        }
        let key = UsageKey {
            user_id,
            object_type: object_type.to_string(),
            object_id,
        };
        let full = {
            let mut s = self.state.lock().expect("usage");
            if s.counts.contains_key(&key) || s.counts.len() < self.limit {
                *s.counts.entry(key).or_insert(0) += 1;
            } else {
                s.dropped += 1;
            }
            s.counts.len() >= self.limit
        };
        if full {
            self.full.notify_one();
        }
    }

    pub async fn close(&self) {
        self.stop.cancel();
        let handle = self.done.lock().expect("done").take();
        if let Some(handle) = handle {
            let _ = handle.await;
        }
    }
}

async fn flush(pool: &Pool, state: &Arc<Mutex<State>>, limit: usize) {
    let counted = {
        let mut s = state.lock().expect("usage");
        std::mem::take(&mut s.counts)
    };
    if counted.is_empty() {
        return;
    }
    let mut keys: Vec<UsageKey> = counted.keys().cloned().collect();
    keys.sort();
    let counts: Vec<i64> = keys.iter().map(|k| counted[k]).collect();
    let mut err: Option<Error> = None;
    for attempt in 1..=USAGE_FLUSH_ATTEMPTS {
        let result =
            tokio::time::timeout(USAGE_FLUSH_TIMEOUT, post_object_usage(pool, &keys, &counts))
                .await;
        let e = match result {
            Ok(Ok(())) => {
                err = None;
                break;
            }
            Ok(Err(e)) => e,
            Err(_) => Error::other("usage flush timed out"),
        };
        let retryable = usage_retryable(&e);
        err = Some(e);
        if !retryable || attempt == USAGE_FLUSH_ATTEMPTS {
            break;
        }
        tokio::time::sleep(USAGE_FLUSH_BACKOFF * attempt).await;
    }
    let dropped = {
        let mut s = state.lock().expect("usage");
        if err.is_some() {
            for (i, k) in keys.iter().enumerate() {
                if s.counts.contains_key(k) || s.counts.len() < limit {
                    *s.counts.entry(k.clone()).or_insert(0) += counts[i];
                } else {
                    s.dropped += counts[i];
                }
            }
        }
        let dropped = s.dropped;
        s.dropped = 0;
        dropped
    };
    if let Some(e) = err {
        tracing::warn!("usage flush: {e}");
    }
    if dropped > 0 {
        tracing::warn!("usage flush: {dropped} uses dropped over the limit of {limit} keys");
    }
}

fn usage_retryable(err: &Error) -> bool {
    matches!(err.pg_code(), Some("40P01") | Some("40001"))
}

async fn post_object_usage(pool: &Pool, keys: &[UsageKey], counts: &[i64]) -> Result<()> {
    let users: Vec<Uuid> = keys.iter().map(|k| k.user_id).collect();
    let types: Vec<String> = keys.iter().map(|k| k.object_type.clone()).collect();
    let objects: Vec<Uuid> = keys.iter().map(|k| k.object_id).collect();
    let conn = pool.get().await?;
    conn.execute(
        "SELECT claimius.touch_object_usage($1::uuid[], $2::text[], $3::uuid[], $4::bigint[])",
        &[&users, &types, &objects, &counts],
    )
    .await?;
    Ok(())
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct ObjectUsageResult {
    #[serde(default)]
    pub object_type: Option<ObjectType>,
    #[serde(default)]
    pub object_id: Uuid,
    #[serde(default)]
    pub name: Option<String>,
    #[serde(default)]
    pub used_count: Option<i64>,
}

impl Hidden for ObjectUsageResult {}

impl Model for ObjectUsageResult {
    const TABLE: &'static str = "usage";
    const COLUMNS: &'static [(&'static str, &'static str)] = &[
        ("object_type", "object_type"),
        ("object_id", "object_id"),
        ("sa_name", "name"),
        ("used_count", "used_count"),
    ];
    const FIELDS: &'static [&'static str] = &["object_type", "object_id", "name", "used_count"];

    fn id(&self) -> Uuid {
        self.object_id
    }
}

impl Db {
    pub async fn get_object_usage(
        &self,
        ctx: &Ctx,
        user_id: Uuid,
        object_ids: &[Uuid],
        object_types: &[String],
    ) -> Result<Vec<ObjectUsageResult>> {
        self.query_models::<ObjectUsageResult>(
            ctx,
            "SELECT * FROM claimius.get_object_usage($1, $2::uuid[], $3::text[])",
            &[&user_id, &object_ids, &object_types],
        )
        .await
    }
}
