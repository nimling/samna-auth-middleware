use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use axum::Router;
use axum::routing::{delete, get, post, put};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use tokio::task::JoinSet;
use tokio_util::sync::CancellationToken;
use uuid::Uuid;

use super::echo as h;
use super::sdb;
use crate::client::{self, Ctx, SyncCursor, Target};
use crate::notification;
use crate::shared::db::{Conn, lead_sync};
use crate::shared::{
    self, AccessClaim, ClaimObject, Db, DiscipleState, Error, Model, Result, UserNameAvailable,
};
use crate::shared::{BodySamProfileItem, BodySamProfileItemKey, BodySamUserName};
use crate::smw;
use crate::uptime;
use crate::utils::{DebugMode, cfg, get_debug_mode};

const ACTIVE_INTERVAL: Duration = Duration::from_millis(100);
const MAX_BACKOFF: Duration = Duration::from_secs(30);
const FAILED_PULLS_BEFORE_RECONCILE: u32 = 10;
const LEAD_INTERVAL: Duration = Duration::from_secs(5);
const SYNC_HOLD: Duration = Duration::from_secs(25);
const SYNC_HOLD_MARGIN: Duration = Duration::from_secs(10);
const DRAIN_INTERVAL: Duration = Duration::from_secs(1);
const DRAIN_PAIRS: i32 = 1000;
const MOVE_INTERVAL: Duration = Duration::from_secs(60);
const INITIATE_INTERVAL: Duration = Duration::from_secs(60);
const CLEAN_INTERVAL: Duration = Duration::from_secs(600);
const CLEAN_GRACE: Duration = Duration::from_secs(600);

pub struct Replica {
    pub db: &'static Db,
}

pub fn disciple() -> Result<Replica> {
    Ok(Replica { db: sdb::need()? })
}

#[derive(Clone, Debug, Default)]
pub struct State {
    pub id: Uuid,
    pub cursor: SyncCursor,
    pub prophet: String,
    pub endpoint: String,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct SyncPayload {
    #[serde(default)]
    pub txid: i64,
    #[serde(default)]
    pub seq: i64,
    #[serde(default)]
    pub operation: String,
    #[serde(default)]
    pub event_type: String,
    #[serde(default)]
    pub payload: Value,
}

impl SyncPayload {
    pub fn cursor(&self) -> SyncCursor {
        SyncCursor {
            txid: self.txid,
            seq: self.seq,
        }
    }
}

#[derive(Clone, Debug, Deserialize)]
pub struct SnapshotResponse {
    #[serde(default)]
    pub records: Vec<SyncPayload>,
    #[serde(default)]
    pub txid: i64,
    #[serde(default)]
    pub seq: i64,
}

static RUN: Mutex<Option<Arc<Mutex<Option<JoinSet<()>>>>>> = Mutex::new(None);

pub async fn init(stop: CancellationToken) -> Result<()> {
    let db = sdb::need()?;
    if get_debug_mode() == DebugMode::None {
        disciple()?.get_state(&Ctx::new()).await?;
    }
    let mut set = JoinSet::new();
    notification::init(stop.clone(), db).await?;
    {
        let db: Arc<Db> = Arc::new(db_clone(db));
        let stop = stop.clone();
        set.spawn(async move {
            lead_sync(
                stop.clone(),
                db.clone(),
                shared::RETENTION_LOCK_KEY.to_string(),
                LEAD_INTERVAL,
                move |s| shared::retention::prune_loop(s, db.clone()),
            )
            .await;
        });
    }
    if get_debug_mode() != DebugMode::None {
        *RUN.lock().expect("run") = Some(Arc::new(Mutex::new(Some(set))));
        return Ok(());
    }
    {
        let stop = stop.clone();
        set.spawn(async move { bind_client(stop, LEAD_INTERVAL).await });
    }
    {
        let stop = stop.clone();
        set.spawn(async move { get_app_seed(stop, LEAD_INTERVAL).await });
    }
    {
        let stop = stop.clone();
        set.spawn(async move { put_initiate_apps(stop, INITIATE_INTERVAL).await });
    }
    {
        let db: Arc<Db> = Arc::new(db_clone(db));
        let stop = stop.clone();
        set.spawn(async move {
            lead_sync(
                stop.clone(),
                db,
                sync_lock_key(&cfg().app_slug),
                LEAD_INTERVAL,
                |s| async move {
                    if let Ok(r) = disciple() {
                        r.pull(s).await;
                    }
                },
            )
            .await;
        });
    }
    *RUN.lock().expect("run") = Some(Arc::new(Mutex::new(Some(set))));
    Ok(())
}

fn db_clone(db: &'static Db) -> Db {
    Db {
        pool: db.pool.clone(),
        replica: db.replica.clone(),
        conn_str: db.conn_str.clone(),
        usage: shared::usage::ObjectUsage::start(db.pool.clone()),
        route_to_prophet: db.route_to_prophet,
        prophet: db.prophet,
        retention: Mutex::new(db.retention.lock().expect("retention").clone()),
    }
}

pub async fn done() {
    let run = RUN.lock().expect("run").clone();
    let Some(run) = run else {
        tracing::error!("sync not started: call sam.Init");
        return;
    };
    let set = run.lock().expect("set").take();
    if let Some(mut set) = set {
        while set.join_next().await.is_some() {}
    }
}

async fn get_app_seed(stop: CancellationToken, interval: Duration) {
    loop {
        match client::get_app_seed(&Ctx::new()).await {
            Ok(_) => return,
            Err(_) if stop.is_cancelled() => return,
            Err(e) => tracing::warn!("sync: {e}"),
        }
        tokio::select! {
            _ = stop.cancelled() => return,
            _ = tokio::time::sleep(interval) => {}
        }
    }
}

async fn put_initiate_apps(stop: CancellationToken, interval: Duration) {
    loop {
        if let Err(e) = shared::db::put_initiate_apps(&Ctx::new()).await
            && !stop.is_cancelled()
        {
            tracing::warn!("sync: {e}");
        }
        tokio::select! {
            _ = stop.cancelled() => return,
            _ = tokio::time::sleep(interval) => {}
        }
    }
}

async fn bind_client(stop: CancellationToken, interval: Duration) {
    loop {
        if let Ok(r) = disciple()
            && let Ok(state) = r.get_state(&Ctx::new()).await
        {
            client::bind(&state.endpoint, &state.prophet);
        }
        tokio::select! {
            _ = stop.cancelled() => return,
            _ = tokio::time::sleep(interval) => {}
        }
    }
}

pub fn sync_lock_key(app_slug: &str) -> String {
    format!("claimius.disciple_sync:{app_slug}")
}

impl Replica {
    async fn report_stalled_cursor(&self, ctx: &Ctx, reason: &str) -> String {
        match self.get_state(ctx).await {
            Err(e) => {
                tracing::error!("sync: read cursor before reconcile: {e}");
                format!("cursor {reason}, reconciled from a fresh snapshot")
            }
            Ok(state) => {
                tracing::warn!(
                    "sync: cursor {} {reason}, reconciling from snapshot",
                    state.cursor
                );
                format!(
                    "cursor {} {reason}, reconciled from a fresh snapshot",
                    state.cursor
                )
            }
        }
    }

    pub async fn pull(&self, stop: CancellationToken) {
        let ctx = Ctx::new();
        let report = |err: &Error| {
            if !stop.is_cancelled() {
                tracing::error!("sync: {err}");
            }
        };
        match self.get_state(&ctx).await {
            Err(e) => report(&Error::other(format!("get current seq: {e}"))),
            Ok(state) if state.cursor.is_zero() => {
                if let Err(e) = self.pull_snapshot(&ctx, "", "").await {
                    report(&e);
                }
            }
            Ok(_) => {}
        }
        let mut backoff = Duration::from_secs(1);
        let mut failed = 0u32;
        let long_ago = Instant::now() - Duration::from_secs(3600);
        let (mut drained, mut moved, mut cleaned) = (long_ago, long_ago, long_ago);
        loop {
            let mut wait = ACTIVE_INTERVAL;
            if moved.elapsed() >= MOVE_INTERVAL {
                moved = Instant::now();
                match self.r#move(&ctx).await {
                    Err(e) if e.is_not_home() => {
                        tracing::warn!("sync: {e}, keeping the prophet the disciple is bound to")
                    }
                    Err(e) => report(&e),
                    Ok(()) => {}
                }
            }
            match self.pull_sync(&ctx).await {
                Err(e) if e.is_not_home() => {
                    report(&e);
                    wait = backoff;
                    backoff = (backoff * 2).min(MAX_BACKOFF);
                }
                Err(Error::SnapshotRequired) => {
                    let reason = self
                        .report_stalled_cursor(&ctx, "was refused by the prophet feed")
                        .await;
                    match self.pull_snapshot(&ctx, "", &reason).await {
                        Err(e) => {
                            report(&e);
                            wait = backoff;
                            backoff = (backoff * 2).min(MAX_BACKOFF);
                        }
                        Ok(()) => {
                            backoff = Duration::from_secs(1);
                            failed = 0;
                        }
                    }
                }
                Err(e) => {
                    report(&e);
                    wait = backoff;
                    backoff = (backoff * 2).min(MAX_BACKOFF);
                    failed += 1;
                    if failed >= FAILED_PULLS_BEFORE_RECONCILE {
                        failed = 0;
                        let reason = format!(
                            "failed to apply {FAILED_PULLS_BEFORE_RECONCILE} consecutive pulls, last error {e}"
                        );
                        let reason = self.report_stalled_cursor(&ctx, &reason).await;
                        match self.pull_snapshot(&ctx, "", &reason).await {
                            Err(e) => report(&e),
                            Ok(()) => backoff = Duration::from_secs(1),
                        }
                    }
                }
                Ok(_) => {
                    backoff = Duration::from_secs(1);
                    failed = 0;
                }
            }
            if drained.elapsed() >= DRAIN_INTERVAL {
                drained = Instant::now();
                if let Err(e) = self.db.post_access_diff_drain(&ctx, DRAIN_PAIRS).await {
                    report(&Error::other(format!("drain access_diff: {e}")));
                }
            }
            if cleaned.elapsed() >= CLEAN_INTERVAL {
                cleaned = Instant::now();
                if let Err(e) = self.clean_orphans(&ctx).await {
                    report(&Error::other(format!("clean orphans: {e}")));
                }
            }
            tokio::select! {
                _ = stop.cancelled() => return,
                _ = tokio::time::sleep(wait) => {}
            }
        }
    }

    pub async fn r#move(&self, ctx: &Ctx) -> Result<()> {
        let state = self
            .get_state(ctx)
            .await
            .map_err(|e| Error::other(format!("get current seq: {e}")))?;
        let configured = cfg().endpoint;
        if state.endpoint.is_empty() || state.endpoint == configured {
            return Ok(());
        }
        self.pull_snapshot(
            ctx,
            &configured,
            &format!(
                "moved from prophet {} at {} to {configured}",
                state.prophet, state.endpoint
            ),
        )
        .await
    }

    fn target(&self, state: &State, endpoint: &str) -> Target {
        let mut endpoint = endpoint.to_string();
        if endpoint.is_empty() {
            endpoint = state.endpoint.clone();
        }
        if endpoint.is_empty() {
            endpoint = cfg().endpoint;
        }
        let c = cfg();
        Target {
            endpoint,
            slug: c.app_slug,
            secret: c.app_secret,
            peer_secret: String::new(),
        }
    }

    fn query(&self, state: &State) -> String {
        let mut q = url::form_urlencoded::Serializer::new(String::new());
        q.append_pair("consumer", &state.id.to_string());
        if !state.prophet.is_empty() {
            q.append_pair("prophet", &state.prophet);
        }
        q.finish()
    }

    pub async fn pull_snapshot(&self, ctx: &Ctx, endpoint: &str, reconcile: &str) -> Result<()> {
        let state = self
            .get_state(ctx)
            .await
            .map_err(|e| Error::other(format!("get current seq: {e}")))?;
        let target = self.target(&state, endpoint);
        let prophet = Arc::new(Mutex::new(String::new()));
        let req = ctx
            .clone()
            .with_target(target.clone())
            .with_prophet(prophet.clone());
        let snapshot: SnapshotResponse =
            match client::get(&req, &format!("sync/snapshot?{}", self.query(&state))).await {
                Ok(s) => s,
                Err(e) if e.is(client::ErrorKind::NotFound) => return Err(Error::SyncUnreachable),
                Err(e) => return Err(Error::other(format!("fetch snapshot: {e}"))),
            };
        let prophet = prophet.lock().expect("prophet").clone();
        let to = State {
            id: state.id,
            cursor: SyncCursor {
                txid: snapshot.txid,
                seq: snapshot.seq,
            },
            prophet,
            endpoint: target.endpoint,
        };
        self.apply_page(ctx, state.cursor, &to, &snapshot.records, reconcile)
            .await
    }

    pub async fn pull_sync(&self, ctx: &Ctx) -> Result<usize> {
        let state = self
            .get_state(ctx)
            .await
            .map_err(|e| Error::other(format!("get current seq: {e}")))?;
        let (payloads, prophet) = self.get_sync_page(ctx, &state).await?;
        if !state.prophet.is_empty() && !prophet.is_empty() && prophet != state.prophet {
            return Err(Error::SnapshotRequired);
        }
        let bind = state.prophet.is_empty() && !prophet.is_empty();
        if payloads.is_empty() && !bind {
            return Ok(0);
        }
        let mut to = State {
            id: state.id,
            cursor: state.cursor,
            prophet,
            endpoint: self.target(&state, "").endpoint,
        };
        for payload in &payloads {
            if to.cursor.less(payload.cursor()) {
                to.cursor = payload.cursor();
            }
        }
        self.apply_page(ctx, state.cursor, &to, &payloads, "")
            .await?;
        Ok(payloads.len())
    }

    pub async fn get_sync_page(
        &self,
        ctx: &Ctx,
        state: &State,
    ) -> Result<(Vec<SyncPayload>, String)> {
        let prophet = Arc::new(Mutex::new(String::new()));
        let req = ctx
            .clone()
            .with_timeout(SYNC_HOLD + SYNC_HOLD_MARGIN)
            .with_target(self.target(state, ""))
            .with_prophet(prophet.clone());
        let mut q = self.query(state);
        q.push_str(&format!("&wait={}", SYNC_HOLD.as_secs()));
        let payloads: Vec<SyncPayload> =
            match client::get(&req, &format!("sync/{}?{q}", state.cursor)).await {
                Ok(p) => p,
                Err(e) if e.is(client::ErrorKind::NotFound) => return Err(Error::SyncUnreachable),
                Err(e) if e.is(client::ErrorKind::NotHome) => return Err(Error::Client(e)),
                Err(e) if e.is(client::ErrorKind::Conflict) => return Err(Error::SnapshotRequired),
                Err(e) => return Err(Error::other(format!("fetch sync: {e}"))),
            };
        let prophet = prophet.lock().expect("prophet").clone();
        Ok((payloads, prophet))
    }

    pub async fn apply_page(
        &self,
        ctx: &Ctx,
        from: SyncCursor,
        to: &State,
        payloads: &[SyncPayload],
        reconcile: &str,
    ) -> Result<()> {
        let conn = self.db.conn(ctx).await?;
        conn.begin().await?;
        let applied: Result<()> = async {
            conn.execute("SET LOCAL claimius.replay_mode = 'true'", &[])
                .await
                .map_err(|e| Error::other(format!("set replay_mode: {e}")))?;
            conn.execute("SET LOCAL synchronous_commit = off", &[])
                .await
                .map_err(|e| Error::other(format!("set synchronous_commit: {e}")))?;
            self.update_seq(&conn, from, to).await?;
            for payload in payloads {
                apply_payload(&conn, payload).await.map_err(|e| {
                    Error::other(format!(
                        "apply {} seq {}: {e}",
                        payload.event_type,
                        payload.cursor()
                    ))
                })?;
            }
            if reconcile.is_empty() {
                return Ok(());
            }
            sdb::write(
                &conn,
                "disciple sync reconcile",
                "update",
                "claimius.disciple_state",
                Uuid::nil(),
                Uuid::nil(),
                None,
                reconcile,
            )
            .await
        }
        .await;
        match applied {
            Ok(()) => conn.commit().await,
            Err(e) => {
                let _ = conn.rollback().await;
                Err(e)
            }
        }
    }

    pub async fn update_seq(&self, conn: &Conn, from: SyncCursor, to: &State) -> Result<()> {
        let rows = if to.prophet.is_empty() {
            conn.execute("UPDATE claimius.disciple_state SET last_applied_txid = $1, last_applied_seq = $2, sa_updated_at = now() WHERE last_applied_txid = $3 AND last_applied_seq = $4", &[&to.cursor.txid, &to.cursor.seq, &from.txid, &from.seq]).await
        } else {
            conn.execute(
                "UPDATE claimius.disciple_state SET prophet = $1, endpoint = $2, last_applied_txid = $3, last_applied_seq = $4, sa_updated_at = now() WHERE last_applied_txid = $5 AND last_applied_seq = $6",
                &[&to.prophet, &to.endpoint, &to.cursor.txid, &to.cursor.seq, &from.txid, &from.seq],
            )
            .await
        }
        .map_err(|e| Error::other(format!("update seq: {e}")))?;
        if rows == 0 {
            return Err(Error::other(format!(
                "update seq: cursor moved from {from} before the page applied"
            )));
        }
        Ok(())
    }

    pub async fn get_state(&self, ctx: &Ctx) -> Result<State> {
        let rows = self
            .db
            .query_models::<DiscipleState>(
                ctx,
                "SELECT * FROM claimius.disciple_state LIMIT 1",
                &[],
            )
            .await?;
        let Some(row) = rows.into_iter().next() else {
            return Err(Error::NoDiscipleState);
        };
        Ok(State {
            id: row.id,
            cursor: row.cursor(),
            prophet: row.prophet.clone().unwrap_or_default(),
            endpoint: row.endpoint.clone().unwrap_or_default(),
        })
    }

    pub async fn clean_orphans(&self, ctx: &Ctx) -> Result<usize> {
        let grace = format!("{} seconds", CLEAN_GRACE.as_secs());
        let mut bindings = self
            .db
            .query_models::<ClaimObject>(
                ctx,
                "SELECT * FROM claimius.get_orphan_bindings($1::text::interval)",
                &[&grace],
            )
            .await
            .map_err(|e| Error::other(format!("orphan bindings: {e}")))?;
        let mut removed = 0;
        for binding in bindings.iter_mut() {
            let (id, claim_id) = (binding.id, binding.claim_id);
            self.db
                .new_delete(binding)
                .exec(ctx)
                .await
                .map_err(|e| Error::other(format!("remove binding {id} on {claim_id}: {e}")))?;
            removed += 1;
        }
        Ok(removed)
    }

    pub async fn put_profile_item(
        &self,
        ctx: &Ctx,
        ac: Option<&AccessClaim>,
        item: &BodySamProfileItem,
    ) -> Result<Value> {
        profile_actor(ac, item.user_id)?;
        item.validate()?;
        if !self.db.route_to_prophet {
            return Err(Error::NoProphet);
        }
        client::put(ctx, "profile", item)
            .await
            .map_err(profile_error)
    }

    pub async fn delete_profile_item(
        &self,
        ctx: &Ctx,
        ac: Option<&AccessClaim>,
        key: &BodySamProfileItemKey,
    ) -> Result<Value> {
        profile_actor(ac, key.user_id)?;
        key.validate()?;
        if !self.db.route_to_prophet {
            return Err(Error::NoProphet);
        }
        client::delete(ctx, "profile", Some(key))
            .await
            .map_err(profile_error)
    }

    pub async fn get_user_name_available(
        &self,
        ctx: &Ctx,
        ac: Option<&AccessClaim>,
        user_name: &str,
    ) -> Result<UserNameAvailable> {
        let Some(ac) = ac.filter(|a| !a.user_id.is_nil()) else {
            return Err(Error::no_access());
        };
        BodySamUserName {
            user_id: ac.user_id,
            user_name: Some(user_name.to_string()),
        }
        .validate()?;
        if !self.db.route_to_prophet {
            return Err(Error::NoProphet);
        }
        let encoded: String = url::form_urlencoded::byte_serialize(user_name.as_bytes()).collect();
        client::get(ctx, &format!("user_name?user_name={encoded}"))
            .await
            .map_err(user_name_error)
    }

    pub async fn put_user_name(
        &self,
        ctx: &Ctx,
        ac: Option<&AccessClaim>,
        body: &BodySamUserName,
    ) -> Result<Option<String>> {
        profile_actor(ac, body.user_id)?;
        body.validate()?;
        if !self.db.route_to_prophet {
            return Err(Error::NoProphet);
        }
        let out: BodySamUserName = client::put(ctx, "user_name", body)
            .await
            .map_err(user_name_error)?;
        Ok(out.user_name)
    }
}

fn profile_actor(ac: Option<&AccessClaim>, user_id: Uuid) -> Result<()> {
    match ac {
        Some(ac)
            if !ac.user_id.is_nil()
                && ac.user_id == user_id
                && ac.access.has(shared::ACCESS_READ) =>
        {
            Ok(())
        }
        _ => Err(Error::no_access()),
    }
}

fn profile_error(e: client::ClientError) -> Error {
    if e.is(client::ErrorKind::NotFound) {
        return Error::NotFound(e.to_string());
    }
    if e.is(client::ErrorKind::Forbidden) {
        return Error::NoAccess(e.to_string());
    }
    if e.is(client::ErrorKind::BadRequest) {
        return Error::BadProfileItem(e.to_string());
    }
    Error::Client(e)
}

fn user_name_error(e: client::ClientError) -> Error {
    if e.is(client::ErrorKind::Conflict) {
        return Error::UserNameTaken(e.to_string());
    }
    if e.is(client::ErrorKind::NotFound) {
        return Error::NotFound(e.to_string());
    }
    if e.is(client::ErrorKind::Forbidden) {
        return Error::NoAccess(e.to_string());
    }
    if e.is(client::ErrorKind::BadRequest) {
        return Error::BadUserName(e.to_string());
    }
    Error::Client(e)
}

pub async fn apply_payload(conn: &Conn, payload: &SyncPayload) -> Result<()> {
    shared::apply_event(
        conn,
        &payload.operation,
        &payload.event_type,
        payload.payload.clone(),
    )
    .await
}

pub fn register_endpoints(router: Router) -> Router {
    router
        .route("/login", get(h::get_login))
        .route("/callback", get(h::get_callback))
        .route("/logout", get(h::get_logout))
        .route("/{app}/login", get(h::get_login))
        .route("/{app}/logout", get(h::get_logout))
        .route("/me", get(h::get_me).put(h::put_me).patch(h::patch_me))
        .route("/me/claim", get(h::get_me_claims))
        .route("/me/relation", get(h::get_me_relations))
        .route("/me/organization", get(h::get_me_organizations))
        .route("/me/location", get(h::get_me_locations))
        .route("/me/asset", get(h::get_me_assets))
        .route("/me/audit", get(h::get_me_audit))
        .route("/asset/{id}/data", get(h::get_asset_data))
        .route("/code", get(h::get_code))
        .route("/notification", get(h::get_notifications))
        .route("/notification/{id}/read", put(h::put_notification_read))
        .route("/notification/{id}/closed", put(h::put_notification_closed))
        .route("/notification/{id}", delete(h::delete_notification))
        .merge(register_capture(Router::new()))
}

pub fn register_status(router: Router) -> Router {
    router.route(uptime::HEALTH_PATH, get(h::get_health))
}

pub fn register_capture(router: Router) -> Router {
    router
        .route(
            "/analytics/consent",
            get(h::get_consent).post(h::post_consent),
        )
        .route("/analytics", post(h::post_analytics))
        .route("/analytics/capture", post(h::post_analytics_capture))
}

pub fn register_capture_read(router: Router) -> Router {
    router
        .route("/analytics/app", get(h::get_analytics_apps))
        .route("/analytics/event", get(h::get_analytics_events))
        .route("/analytics/session", get(h::get_analytics_sessions))
        .route("/analytics/capture", get(h::get_analytics_capture))
        .route("/analytics/heat", get(h::get_analytics_heat))
        .route("/analytics/summary", get(h::get_analytics_summary))
}

pub fn register_cli(router: Router) -> Router {
    router
        .route(
            "/cli/token",
            post(h::post_cli_token).layer(axum::middleware::from_fn(smw::rate_limit)),
        )
        .route(
            "/cli/revoke",
            post(h::post_cli_revoke).layer(axum::middleware::from_fn(smw::rate_limit)),
        )
}

pub fn model_table<T: Model>() -> &'static str {
    T::TABLE
}
