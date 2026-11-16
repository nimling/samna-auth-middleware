use std::future::Future;
use std::pin::Pin;
use std::sync::{Arc, Mutex, OnceLock};
use std::time::{Duration, Instant};

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use tokio_util::sync::CancellationToken;
use uuid::Uuid;

use crate::client::{self, Ctx};
use crate::shared::{Error, Result};
use crate::utils::{DebugMode, cfg, get_debug_mode};

const DEFAULT_INTERVAL: Duration = Duration::from_secs(60);

pub const MONITOR_KIND_HTTP: &str = "http";
pub const MONITOR_KIND_TCP: &str = "tcp";
pub const MONITOR_KIND_DISCIPLE: &str = "disciple";
pub const MONITOR_KIND_HEARTBEAT: &str = "heartbeat";
pub const MONITOR_NAME: &str = "heartbeat";
pub const STATUS_BASE_PATH: &str = "/api/status";
pub const HEALTH_PATH: &str = "/health";
pub const VERSION_ENV: &str = "APP_VERSION";

pub struct HealthStatus;

impl HealthStatus {
    pub const OK: &'static str = "ok";
}

pub type CheckFn = Arc<
    dyn Fn() -> Pin<Box<dyn Future<Output = std::result::Result<(), String>> + Send>> + Send + Sync,
>;

pub struct Check {
    pub name: String,
    pub run: Option<CheckFn>,
}

#[derive(Default)]
pub struct Options {
    pub interval: Duration,
    pub version: String,
    pub checks: Vec<Check>,
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct Monitor {
    #[serde(default)]
    pub id: Uuid,
    #[serde(default)]
    pub app_id: Uuid,
    #[serde(default)]
    pub name: String,
    #[serde(default)]
    pub kind: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub target: String,
    #[serde(default)]
    pub interval_seconds: i32,
    #[serde(default)]
    pub timeout_seconds: i32,
    #[serde(default)]
    pub expected_status: i32,
    #[serde(default)]
    pub enabled: bool,
    #[serde(default)]
    pub owner_id: Uuid,
    #[serde(default)]
    pub created_by: Uuid,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub deleted_at: Option<DateTime<Utc>>,
    #[serde(default = "epoch")]
    pub created_at: DateTime<Utc>,
    #[serde(default = "epoch")]
    pub updated_at: DateTime<Utc>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub last_checked_at: Option<DateTime<Utc>>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub last_ok: Option<bool>,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub app_slug: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub app_name: String,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub days: Vec<MonitorCheckDay>,
}

fn epoch() -> DateTime<Utc> {
    DateTime::<Utc>::from_timestamp(0, 0).unwrap_or_default()
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct MonitorInput {
    pub name: String,
    pub kind: String,
    pub interval_seconds: i32,
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct MonitorCheck {
    #[serde(default)]
    pub id: Uuid,
    #[serde(default)]
    pub app_id: Uuid,
    #[serde(default)]
    pub monitor_id: Uuid,
    #[serde(default = "epoch")]
    pub checked_at: DateTime<Utc>,
    #[serde(default)]
    pub ok: bool,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub status_code: Option<i32>,
    #[serde(default)]
    pub latency_ms: i32,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
    #[serde(default)]
    pub owner_id: Uuid,
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct MonitorCheckDay {
    #[serde(default)]
    pub monitor_id: Uuid,
    #[serde(default)]
    pub app_id: Uuid,
    #[serde(default = "epoch")]
    pub day: DateTime<Utc>,
    #[serde(default)]
    pub ok_count: i64,
    #[serde(default)]
    pub failed_count: i64,
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct MonitorIncident {
    #[serde(default)]
    pub id: Uuid,
    #[serde(default)]
    pub app_id: Uuid,
    #[serde(default)]
    pub monitor_id: Uuid,
    #[serde(default = "epoch")]
    pub opened_at: DateTime<Utc>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub resolved_at: Option<DateTime<Utc>>,
    #[serde(default)]
    pub title: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub description: Option<String>,
    #[serde(default)]
    pub owner_id: Uuid,
    #[serde(default)]
    pub created_by: Uuid,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub deleted_at: Option<DateTime<Utc>>,
    #[serde(default = "epoch")]
    pub created_at: DateTime<Utc>,
    #[serde(default = "epoch")]
    pub updated_at: DateTime<Utc>,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub app_slug: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub app_name: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub monitor_name: String,
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct MonitorStatus {
    #[serde(default)]
    pub app_slug: String,
    #[serde(default)]
    pub app_name: String,
    #[serde(default)]
    pub monitors: Vec<Monitor>,
    #[serde(default)]
    pub incidents: Vec<MonitorIncident>,
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct BeatCheck {
    pub name: String,
    pub ok: bool,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub error: String,
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct Beat {
    pub ok: bool,
    pub latency_ms: i32,
    pub version: String,
    pub checks: Vec<BeatCheck>,
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct Heartbeat {
    pub monitor_id: Uuid,
    #[serde(default = "epoch")]
    pub beat_at: DateTime<Utc>,
    pub reported: bool,
    #[serde(flatten)]
    pub beat: Beat,
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct Health {
    pub status: String,
    pub app: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub version: String,
}

struct Runner {
    opts: Options,
    monitor_id: Mutex<Uuid>,
    last: Mutex<Option<Heartbeat>>,
    done: Mutex<Option<tokio::task::JoinHandle<()>>>,
    stop: CancellationToken,
}

static RUN: OnceLock<Mutex<Option<Arc<Runner>>>> = OnceLock::new();

fn current() -> Option<Arc<Runner>> {
    RUN.get().and_then(|r| r.lock().expect("uptime").clone())
}

pub async fn init(stop: CancellationToken, mut opts: Options) -> Result<()> {
    let slot = RUN.get_or_init(|| Mutex::new(None));
    if let Some(run) = slot.lock().expect("uptime").as_ref()
        && !run.stop.is_cancelled()
    {
        return Err(Error::other("uptime already started"));
    }
    if opts.interval.is_zero() {
        opts.interval = DEFAULT_INTERVAL;
    }
    let runner = Arc::new(Runner {
        opts,
        monitor_id: Mutex::new(Uuid::nil()),
        last: Mutex::new(None),
        done: Mutex::new(None),
        stop: stop.clone(),
    });
    if get_debug_mode() != DebugMode::None {
        tracing::info!("uptime reporter disabled in debug mode");
        *slot.lock().expect("uptime") = Some(runner);
        return Ok(());
    }
    if let Err(e) = register(&runner).await {
        tracing::error!("uptime register: {e}");
    }
    let handle = {
        let runner = runner.clone();
        tokio::spawn(async move {
            run_loop(runner).await;
            tracing::info!("uptime reporter stopped");
        })
    };
    *runner.done.lock().expect("done") = Some(handle);
    *slot.lock().expect("uptime") = Some(runner);
    tracing::info!("uptime reporter started");
    Ok(())
}

pub async fn done() {
    let Some(run) = current() else {
        tracing::error!("uptime not started: call uptime.Init");
        return;
    };
    let handle = run.done.lock().expect("done").take();
    if let Some(handle) = handle {
        let _ = handle.await;
    }
}

pub fn get_heartbeat() -> Option<Heartbeat> {
    current().and_then(|r| r.last.lock().expect("last").clone())
}

fn monitor_target(slug: &str) -> String {
    format!("/api/app/{slug}/monitor/client")
}

fn beat_target(slug: &str, id: Uuid) -> String {
    format!("/api/app/{slug}/monitor/{id}/beat/client")
}

async fn register(runner: &Runner) -> Result<()> {
    let slug = cfg().app_slug;
    let items: Vec<Monitor> = client::get(&Ctx::new(), &monitor_target(&slug))
        .await
        .map_err(|e| Error::other(format!("list monitors: {e}")))?;
    if let Some(item) = items.iter().find(|i| i.name == MONITOR_NAME) {
        *runner.monitor_id.lock().expect("monitor") = item.id;
        return Ok(());
    }
    let seconds = (runner.opts.interval.as_secs() as i32).max(1);
    let input = MonitorInput {
        name: MONITOR_NAME.to_string(),
        kind: MONITOR_KIND_HEARTBEAT.to_string(),
        interval_seconds: seconds,
    };
    let created: Monitor = client::post(&Ctx::new(), &monitor_target(&slug), &input)
        .await
        .map_err(|e| Error::other(format!("create monitor: {e}")))?;
    if created.id.is_nil() {
        return Err(Error::other("create monitor: prophet returned no id"));
    }
    *runner.monitor_id.lock().expect("monitor") = created.id;
    Ok(())
}

async fn run_loop(runner: Arc<Runner>) {
    let mut ticker = tokio::time::interval(runner.opts.interval);
    ticker.tick().await;
    tick(&runner).await;
    loop {
        tokio::select! {
            _ = runner.stop.cancelled() => return,
            _ = ticker.tick() => tick(&runner).await,
        }
    }
}

async fn tick(runner: &Runner) {
    if runner.monitor_id.lock().expect("monitor").is_nil()
        && let Err(e) = register(runner).await
    {
        tracing::error!("uptime register: {e}");
        return;
    }
    let beat = beat(runner).await;
    let monitor_id = *runner.monitor_id.lock().expect("monitor");
    let mut heartbeat = Heartbeat {
        monitor_id,
        beat_at: Utc::now(),
        reported: false,
        beat: beat.clone(),
    };
    match client::post::<Beat, MonitorCheck>(
        &Ctx::new(),
        &beat_target(&cfg().app_slug, monitor_id),
        &beat,
    )
    .await
    {
        Err(e) => tracing::error!("uptime beat: {e}"),
        Ok(_) => heartbeat.reported = true,
    }
    *runner.last.lock().expect("last") = Some(heartbeat);
}

async fn beat(runner: &Runner) -> Beat {
    let started = Instant::now();
    let mut beat = Beat {
        ok: true,
        latency_ms: 0,
        version: runner.opts.version.clone(),
        checks: Vec::with_capacity(runner.opts.checks.len()),
    };
    for check in &runner.opts.checks {
        let mut result = BeatCheck {
            name: check.name.clone(),
            ok: true,
            error: String::new(),
        };
        if let Some(run) = &check.run {
            match tokio::time::timeout(runner.opts.interval, run()).await {
                Ok(Ok(())) => {}
                Ok(Err(e)) => {
                    result.ok = false;
                    result.error = e;
                    beat.ok = false;
                }
                Err(_) => {
                    result.ok = false;
                    result.error = "check timed out".to_string();
                    beat.ok = false;
                }
            }
        }
        beat.checks.push(result);
    }
    beat.latency_ms = started.elapsed().as_millis() as i32;
    beat
}
