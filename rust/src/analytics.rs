use std::sync::{Arc, Mutex, OnceLock};
use std::time::Duration;

use chrono::{DateTime, Utc};
use http::HeaderMap;
use serde_json::Value;
use tokio::sync::mpsc;
use tokio_util::sync::CancellationToken;
use uuid::Uuid;

use crate::shared::db::lead_sync;
use crate::shared::{
    AnalyticsEventInput, Consent, ConsentCategories, ConsentRequest, ConsentState, Ctx, Db, Error,
    Result,
};
use crate::utils::cfg;
use crate::utils::cookie::{CookieWrite, cookie_value};

const JOB_BUFFER: usize = 4096;
pub const CAPTURE_BYTES_MAX: usize = 8 << 20;
pub const CAPTURE_MAX_AGE: &str = "7 days";
const FOLD_INTERVAL: Duration = Duration::from_secs(3600);
const FOLD_LEAD: Duration = Duration::from_secs(60);
const FOLD_LOCK_KEY: &str = "claimius.analytics_fold";
const DRAIN_TIMEOUT: Duration = Duration::from_secs(10);

const POLICY_VERSION: &str = "1";
const POLICY_PATH: &str = "/legal/cookie-policy";
pub const ANONYMOUS_COOKIE: &str = "sauth_anonymous_id";

struct Job {
    app_id: Uuid,
    session_key: String,
    events: Vec<AnalyticsEventInput>,
}

struct Runner {
    db: &'static Db,
    jobs: mpsc::Sender<Job>,
    closed: Mutex<bool>,
    stop: CancellationToken,
}

static RUN: OnceLock<Mutex<Option<Arc<Runner>>>> = OnceLock::new();

fn current() -> Option<Arc<Runner>> {
    RUN.get().and_then(|r| r.lock().expect("analytics").clone())
}

pub async fn init(stop: CancellationToken, db: &'static Db) -> Result<()> {
    let slot = RUN.get_or_init(|| Mutex::new(None));
    if let Some(run) = slot.lock().expect("analytics").as_ref()
        && !run.stop.is_cancelled()
    {
        return Err(Error::other("analytics already started"));
    }
    let (tx, mut rx) = mpsc::channel::<Job>(JOB_BUFFER);
    let runner = Arc::new(Runner {
        db,
        jobs: tx,
        closed: Mutex::new(false),
        stop: stop.clone(),
    });
    {
        let runner = runner.clone();
        let stop = stop.clone();
        tokio::spawn(async move {
            loop {
                tokio::select! {
                    _ = stop.cancelled() => {
                        *runner.closed.lock().expect("closed") = true;
                        let deadline = tokio::time::Instant::now() + DRAIN_TIMEOUT;
                        while let Ok(job) = rx.try_recv() {
                            if tokio::time::Instant::now() >= deadline {
                                break;
                            }
                            exec(runner.db, job).await;
                        }
                        return;
                    }
                    job = rx.recv() => match job {
                        Some(job) => exec(runner.db, job).await,
                        None => return,
                    }
                }
            }
        });
    }
    {
        let stop = stop.clone();
        let shared_db: Arc<Db> = Arc::new(Db {
            pool: db.pool.clone(),
            replica: db.replica.clone(),
            conn_str: db.conn_str.clone(),
            usage: crate::shared::usage::ObjectUsage::start(db.pool.clone()),
            route_to_prophet: db.route_to_prophet,
            prophet: db.prophet,
            retention: Mutex::new(None),
        });
        tokio::spawn(async move {
            lead_sync(
                stop,
                shared_db,
                FOLD_LOCK_KEY.to_string(),
                FOLD_LEAD,
                move |s| fold_loop(s, db),
            )
            .await;
            tracing::info!("analytics runner stopped");
        });
    }
    *slot.lock().expect("analytics") = Some(runner);
    tracing::info!("analytics runner started");
    Ok(())
}

pub fn policy_version() -> &'static str {
    POLICY_VERSION
}

pub fn policy_url() -> String {
    format!("{}{POLICY_PATH}", cfg().endpoint.trim_end_matches('/'))
}

pub fn anonymous_id(headers: &HeaderMap, query: Option<&str>) -> (String, Option<CookieWrite>) {
    let mut id = cookie_value(headers, ANONYMOUS_COOKIE).unwrap_or_default();
    if id.is_empty() {
        id = query.unwrap_or("").to_string();
    }
    if id.is_empty() {
        id = Uuid::new_v4().to_string();
    }
    let cookie = write_anonymous_id(headers, &id);
    (id, cookie)
}

pub fn write_anonymous_id(headers: &HeaderMap, value: &str) -> Option<CookieWrite> {
    if value.is_empty() {
        return None;
    }
    if cookie_value(headers, ANONYMOUS_COOKIE).as_deref() == Some(value) {
        return None;
    }
    Some(CookieWrite::new(
        ANONYMOUS_COOKIE,
        value,
        "/",
        60 * 60 * 24 * 365,
    ))
}

pub async fn post_session(
    ctx: &Ctx,
    app_id: Uuid,
    anonymous_id: &str,
    session_key: &str,
    context: Value,
    user_id: Option<Uuid>,
    ip: &str,
    user_agent: &str,
) -> Result<bool> {
    let Some(run) = current() else {
        return Err(Error::other("analytics not started: call sam.Init"));
    };
    let body = if context.is_null() {
        Value::Object(serde_json::Map::new())
    } else {
        context
    };
    let result: String = run
        .db
        .query_scalar::<String>(ctx, "SELECT claimius.upsert_analytics_session($1, $2, $3, $4::jsonb, $5, $6::inet, $7)::text", &[&app_id, &anonymous_id, &session_key, &body, &user_id, &null_string(ip), &null_string(user_agent)])
        .await?;
    let wrapper: Value = serde_json::from_str(&result)?;
    Ok(wrapper
        .get("analytics_session")
        .is_some_and(|s| !s.is_null()))
}

pub fn post(app_id: Uuid, session_key: &str, events: Vec<AnalyticsEventInput>) {
    if events.is_empty() {
        return;
    }
    let Some(run) = current() else {
        tracing::error!(
            "analytics: {} events dropped: analytics not started: call sam.Init",
            events.len()
        );
        return;
    };
    if *run.closed.lock().expect("closed") {
        tracing::error!(
            "analytics: {} events dropped: the runner has ended",
            events.len()
        );
        return;
    }
    let count = events.len();
    if run
        .jobs
        .try_send(Job {
            app_id,
            session_key: session_key.to_string(),
            events,
        })
        .is_err()
    {
        tracing::error!("analytics: job queue full, dropped {count} events");
    }
}

async fn exec(db: &Db, job: Job) {
    let body = match serde_json::to_value(&job.events) {
        Ok(b) => b,
        Err(e) => {
            tracing::error!("analytics exec marshal: {e}");
            return;
        }
    };
    if let Err(e) = db
        .query_scalar::<i32>(
            &Ctx::new(),
            "SELECT claimius.insert_analytics_events($1, $2, $3::jsonb)",
            &[&job.app_id, &job.session_key, &body],
        )
        .await
    {
        tracing::error!("analytics exec: {e}");
    }
}

async fn fold_loop(stop: CancellationToken, db: &'static Db) {
    let mut ticker = tokio::time::interval(FOLD_INTERVAL);
    ticker.tick().await;
    fold(db).await;
    loop {
        tokio::select! {
            _ = stop.cancelled() => return,
            _ = ticker.tick() => fold(db).await,
        }
    }
}

async fn fold(db: &Db) {
    let now = Utc::now();
    let from = day(now - chrono::Duration::days(1));
    let to = day(now);
    match db
        .query_scalar::<i64>(
            &Ctx::new(),
            "SELECT claimius.upsert_analytics_heat($1::date, $2::date)",
            &[&from, &to],
        )
        .await
    {
        Err(e) => tracing::error!("analytics fold: {e}"),
        Ok(cells) if cells > 0 => tracing::info!("analytics fold: {cells} cells written"),
        Ok(_) => {}
    }
}

fn day(t: DateTime<Utc>) -> String {
    t.format("%Y-%m-%d").to_string()
}

fn null_string(s: &str) -> Option<String> {
    if s.is_empty() {
        None
    } else {
        Some(s.to_string())
    }
}

pub async fn get_consent(
    ctx: &Ctx,
    app_id: Uuid,
    user_id: Option<Uuid>,
    anonymous_id: &str,
) -> Result<ConsentState> {
    let mut state = ConsentState {
        anonymous_id: anonymous_id.to_string(),
        policy_version: POLICY_VERSION.to_string(),
        policy_url: policy_url(),
        categories: ConsentCategories {
            necessary: true,
            ..ConsentCategories::default()
        },
        ..ConsentState::default()
    };
    let Some(run) = current() else {
        return Err(Error::other("analytics not started: call sam.Init"));
    };
    let rows = run
        .db
        .query_models::<Consent>(
            ctx,
            "SELECT * FROM claimius.get_consent($1, $2, $3)",
            &[&app_id, &user_id, &null_string(anonymous_id)],
        )
        .await?;
    let Some(row) = rows.into_iter().next() else {
        return Ok(state);
    };
    if row.policy_version != state.policy_version {
        return Ok(state);
    }
    if !row.categories.is_null() {
        state.categories = serde_json::from_value(row.categories.clone())?;
    }
    state.categories.necessary = true;
    state.decided = true;
    state.granted_at = Some(row.granted_at);
    Ok(state)
}

pub async fn post_consent(
    ctx: &Ctx,
    app_id: Uuid,
    input: ConsentRequest,
    user_id: Option<Uuid>,
    ip: &str,
    user_agent: &str,
) -> Result<ConsentState> {
    let mut input = input;
    input.categories.necessary = true;
    let categories = serde_json::to_value(&input.categories)?;
    let Some(run) = current() else {
        return Err(Error::other("analytics not started: call sam.Init"));
    };
    let result: String = run
        .db
        .query_scalar::<String>(
            ctx,
            "SELECT claimius.insert_consent($1, $2, $3::jsonb, $4, $5, $6::inet, $7)::text",
            &[
                &app_id,
                &input.anonymous_id,
                &categories,
                &input.policy_version,
                &user_id,
                &null_string(ip),
                &null_string(user_agent),
            ],
        )
        .await?;
    let wrapper: Value = serde_json::from_str(&result)?;
    let consent: Consent =
        serde_json::from_value(wrapper.get("consent").cloned().unwrap_or(Value::Null))?;
    Ok(ConsentState {
        anonymous_id: consent.anonymous_id,
        policy_version: consent.policy_version,
        policy_url: policy_url(),
        decided: true,
        categories: input.categories,
        granted_at: Some(consent.granted_at),
        captured: Vec::new(),
    })
}
