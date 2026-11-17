use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex, OnceLock};
use std::time::Duration;

use deadpool_postgres::{Manager, ManagerConfig, Object, Pool, RecyclingMethod};
use regex::Regex;
use serde::de::DeserializeOwned;
use serde_json::Value;
use tokio_postgres::types::{FromSqlOwned, ToSql};
use url::Url;
use uuid::Uuid;

use super::error::{Error, Result};
use super::models::{Model, SamnaApp};
use super::retention::Retention;
use super::usage::ObjectUsage;
use crate::client::{self, Ctx, SyncCursor};
use crate::utils::{self, DebugMode, cfg, get_debug_mode};

pub type Param<'a> = &'a (dyn ToSql + Sync);

pub const RETENTION_LOCK_KEY: &str = "claimius.retention";

static CODE_HASH: OnceLock<Regex> = OnceLock::new();

fn code_hash_pattern() -> &'static Regex {
    CODE_HASH.get_or_init(|| Regex::new(r"^[0-9a-f]{64}$").expect("code hash pattern"))
}

pub struct Conn {
    inner: Object,
    samk: Mutex<String>,
    pub in_tx: AtomicBool,
}

impl Conn {
    fn new(inner: Object) -> Conn {
        Conn {
            inner,
            samk: Mutex::new(String::new()),
            in_tx: AtomicBool::new(false),
        }
    }

    pub fn client(&self) -> &tokio_postgres::Client {
        &self.inner
    }

    pub async fn carry(&self, hash: &str) -> Result<()> {
        let hash = if code_hash_pattern().is_match(hash) {
            hash
        } else {
            ""
        };
        if *self.samk.lock().expect("samk") == hash {
            return Ok(());
        }
        self.inner
            .execute("SELECT set_config('claimius.samk', $1, false)", &[&hash])
            .await?;
        *self.samk.lock().expect("samk") = hash.to_string();
        Ok(())
    }

    pub async fn execute(&self, sql: &str, params: &[Param<'_>]) -> Result<u64> {
        let stmt = self.inner.prepare_cached(sql).await?;
        Ok(self.inner.execute(&stmt, params).await?)
    }

    pub async fn query_json(&self, sql: &str, params: &[Param<'_>]) -> Result<Vec<Value>> {
        let wrapped = format!("SELECT row_to_json(q)::jsonb FROM ({sql}) q");
        let stmt = self.inner.prepare_cached(&wrapped).await?;
        let rows = self.inner.query(&stmt, params).await?;
        Ok(rows.into_iter().map(|r| r.get::<_, Value>(0)).collect())
    }

    pub async fn query_models<T: Model>(&self, sql: &str, params: &[Param<'_>]) -> Result<Vec<T>> {
        let rows = self.query_json(sql, params).await?;
        rows.into_iter().map(T::from_row).collect()
    }

    pub async fn query_model<T: Model>(&self, sql: &str, params: &[Param<'_>]) -> Result<T> {
        let mut rows = self.query_models::<T>(sql, params).await?;
        if rows.is_empty() {
            return Err(Error::no_rows());
        }
        Ok(rows.swap_remove(0))
    }

    pub async fn query_model_opt<T: Model>(
        &self,
        sql: &str,
        params: &[Param<'_>],
    ) -> Result<Option<T>> {
        let mut rows = self.query_models::<T>(sql, params).await?;
        if rows.is_empty() {
            return Ok(None);
        }
        Ok(Some(rows.swap_remove(0)))
    }

    pub async fn query_values<T: DeserializeOwned>(
        &self,
        sql: &str,
        params: &[Param<'_>],
    ) -> Result<Vec<T>> {
        let rows = self.query_json(sql, params).await?;
        rows.into_iter()
            .map(|r| serde_json::from_value(r).map_err(Error::Json))
            .collect()
    }

    pub async fn query_scalar<T: FromSqlOwned>(
        &self,
        sql: &str,
        params: &[Param<'_>],
    ) -> Result<T> {
        let stmt = self.inner.prepare_cached(sql).await?;
        let row = self
            .inner
            .query_opt(&stmt, params)
            .await?
            .ok_or_else(Error::no_rows)?;
        Ok(row.try_get::<_, T>(0)?)
    }

    pub async fn query_scalar_opt<T: FromSqlOwned>(
        &self,
        sql: &str,
        params: &[Param<'_>],
    ) -> Result<Option<T>> {
        let stmt = self.inner.prepare_cached(sql).await?;
        let Some(row) = self.inner.query_opt(&stmt, params).await? else {
            return Ok(None);
        };
        Ok(row.try_get::<_, Option<T>>(0)?)
    }

    pub async fn query_scalars<T: FromSqlOwned>(
        &self,
        sql: &str,
        params: &[Param<'_>],
    ) -> Result<Vec<T>> {
        let stmt = self.inner.prepare_cached(sql).await?;
        let rows = self.inner.query(&stmt, params).await?;
        rows.into_iter()
            .map(|r| r.try_get::<_, T>(0).map_err(Error::Postgres))
            .collect()
    }

    pub async fn begin(&self) -> Result<()> {
        self.inner.batch_execute("BEGIN").await?;
        self.in_tx.store(true, Ordering::Relaxed);
        Ok(())
    }

    pub async fn commit(&self) -> Result<()> {
        self.inner.batch_execute("COMMIT").await?;
        self.in_tx.store(false, Ordering::Relaxed);
        Ok(())
    }

    pub async fn rollback(&self) -> Result<()> {
        self.inner.batch_execute("ROLLBACK").await?;
        self.in_tx.store(false, Ordering::Relaxed);
        Ok(())
    }

    pub fn take(self) {
        drop(Object::take(self.inner));
    }
}

pub enum ConnRef<'a> {
    Borrowed(&'a Conn),
    Owned(Conn),
}

impl std::ops::Deref for ConnRef<'_> {
    type Target = Conn;
    fn deref(&self) -> &Conn {
        match self {
            ConnRef::Borrowed(c) => c,
            ConnRef::Owned(c) => c,
        }
    }
}

pub type LockKey = String;

pub struct Db {
    pub pool: Pool,
    pub replica: Option<Pool>,
    pub conn_str: String,
    pub usage: ObjectUsage,
    pub route_to_prophet: bool,
    pub prophet: bool,
    pub retention: Mutex<Option<Arc<dyn Fn() -> Retention + Send + Sync>>>,
}

impl Db {
    pub async fn conn(&self, ctx: &Ctx) -> Result<Conn> {
        let object = self.pool.get().await?;
        let conn = Conn::new(object);
        conn.carry(&ctx.code_hash()).await?;
        Ok(conn)
    }

    pub async fn conn_or<'a>(&self, ctx: &Ctx, conn: Option<&'a Conn>) -> Result<ConnRef<'a>> {
        match conn {
            Some(c) => Ok(ConnRef::Borrowed(c)),
            None => Ok(ConnRef::Owned(self.conn(ctx).await?)),
        }
    }

    pub async fn read_conn(&self, ctx: &Ctx) -> Result<Conn> {
        match &self.replica {
            None => self.conn(ctx).await,
            Some(replica) => {
                let object = replica.get().await?;
                let conn = Conn::new(object);
                conn.carry(&ctx.code_hash()).await?;
                Ok(conn)
            }
        }
    }

    pub async fn read_models<T: Model>(
        &self,
        ctx: &Ctx,
        sql: &str,
        params: &[Param<'_>],
    ) -> Result<Vec<T>> {
        let conn = self.read_conn(ctx).await?;
        match conn.query_models::<T>(sql, params).await {
            Err(Error::Postgres(e))
                if self.replica.is_some() && e.code().map(|c| c.code()) == Some("25006") =>
            {
                let primary = self.conn(ctx).await?;
                primary.query_models::<T>(sql, params).await
            }
            other => other,
        }
    }

    pub async fn prepared<T: Model>(
        &self,
        ctx: &Ctx,
        replica: bool,
        sql: &str,
        params: &[Param<'_>],
    ) -> Result<Vec<T>> {
        if !replica && self.route_to_prophet {
            let (_, r, fresh) = ctx.clone().with_request_seq();
            if !fresh {
                let target = r.get();
                if !target.is_zero()
                    && let Err(e) = self.wait_for_seq(ctx, target, r.timeout()).await
                {
                    tracing::warn!("routed write {target} not yet applied, reading anyway: {e}");
                }
            }
        }
        if replica {
            return self.read_models::<T>(ctx, sql, params).await;
        }
        let conn = self.conn(ctx).await?;
        conn.query_models::<T>(sql, params).await
    }

    pub async fn execute(&self, ctx: &Ctx, sql: &str, params: &[Param<'_>]) -> Result<u64> {
        let conn = self.conn(ctx).await?;
        conn.execute(sql, params).await
    }

    pub async fn query_models<T: Model>(
        &self,
        ctx: &Ctx,
        sql: &str,
        params: &[Param<'_>],
    ) -> Result<Vec<T>> {
        let conn = self.conn(ctx).await?;
        conn.query_models::<T>(sql, params).await
    }

    pub async fn query_model<T: Model>(
        &self,
        ctx: &Ctx,
        sql: &str,
        params: &[Param<'_>],
    ) -> Result<T> {
        let conn = self.conn(ctx).await?;
        conn.query_model::<T>(sql, params).await
    }

    pub async fn query_model_opt<T: Model>(
        &self,
        ctx: &Ctx,
        sql: &str,
        params: &[Param<'_>],
    ) -> Result<Option<T>> {
        let conn = self.conn(ctx).await?;
        conn.query_model_opt::<T>(sql, params).await
    }

    pub async fn query_values<T: DeserializeOwned>(
        &self,
        ctx: &Ctx,
        sql: &str,
        params: &[Param<'_>],
    ) -> Result<Vec<T>> {
        let conn = self.conn(ctx).await?;
        conn.query_values::<T>(sql, params).await
    }

    pub async fn query_scalar<T: FromSqlOwned>(
        &self,
        ctx: &Ctx,
        sql: &str,
        params: &[Param<'_>],
    ) -> Result<T> {
        let conn = self.conn(ctx).await?;
        conn.query_scalar::<T>(sql, params).await
    }

    pub async fn query_scalar_opt<T: FromSqlOwned>(
        &self,
        ctx: &Ctx,
        sql: &str,
        params: &[Param<'_>],
    ) -> Result<Option<T>> {
        let conn = self.conn(ctx).await?;
        conn.query_scalar_opt::<T>(sql, params).await
    }

    pub async fn query_scalars<T: FromSqlOwned>(
        &self,
        ctx: &Ctx,
        sql: &str,
        params: &[Param<'_>],
    ) -> Result<Vec<T>> {
        let conn = self.conn(ctx).await?;
        conn.query_scalars::<T>(sql, params).await
    }

    pub fn get_connection_string(&self) -> &str {
        &self.conn_str
    }

    pub async fn get_app(&self, ctx: &Ctx) -> Result<SamnaApp> {
        let c = cfg();
        if !c.debug_seed.is_empty() && get_debug_mode() != DebugMode::None {
            let mut app = self.get_app_env()?;
            app.private_seed = c.debug_seed;
            return Ok(app);
        }
        if self.route_to_prophet {
            let mut app = self.get_app_env()?;
            let seed = client::get_app_seed(ctx).await?;
            app.private_seed = seed.private_seed;
            app.owner_id = seed.owner_id;
            return Ok(app);
        }
        Err(Error::other(
            "no claimius app registered: a disciple reads its app from the prophet",
        ))
    }

    pub fn get_app_env(&self) -> Result<SamnaApp> {
        let c = cfg();
        if c.app_slug.is_empty() {
            return Err(Error::other("SAUTH_APP_SLUG not set"));
        }
        Ok(SamnaApp {
            slug: c.app_slug,
            app_secret: Some(c.app_secret),
            ..SamnaApp::default()
        })
    }

    pub async fn get_seq(&self, ctx: &Ctx) -> Result<SyncCursor> {
        let conn = self.conn(ctx).await?;
        get_seq_on(&conn).await
    }

    pub async fn wait_for_seq(
        &self,
        ctx: &Ctx,
        target: SyncCursor,
        timeout: Duration,
    ) -> Result<()> {
        let deadline = tokio::time::Instant::now() + timeout;
        loop {
            match self.get_seq(ctx).await {
                Ok(seq) if !seq.less(target) => return Ok(()),
                Ok(_) => {}
                Err(e) => return Err(e),
            }
            if tokio::time::Instant::now() >= deadline {
                return Err(Error::other(format!(
                    "timeout waiting for seq {target}: deadline has elapsed"
                )));
            }
            tokio::time::sleep(Duration::from_millis(50)).await;
        }
    }

    pub fn set_retention(&self, f: Arc<dyn Fn() -> Retention + Send + Sync>) {
        *self.retention.lock().expect("retention") = Some(f);
    }

    pub async fn close(&self) {
        self.usage.close().await;
        if let Some(replica) = &self.replica {
            replica.close();
        }
        self.pool.close();
    }
}

pub async fn get_seq_on(conn: &Conn) -> Result<SyncCursor> {
    let rows = conn
        .query_models::<super::models::DiscipleState>(
            "SELECT * FROM claimius.disciple_state LIMIT 1",
            &[],
        )
        .await?;
    match rows.first() {
        Some(row) => Ok(row.cursor()),
        None => Err(Error::NoDiscipleState),
    }
}

static INITIATE_APPS: Mutex<Option<Arc<Vec<SamnaApp>>>> = Mutex::new(None);

pub async fn get_initiate_apps(ctx: &Ctx) -> Result<Arc<Vec<SamnaApp>>> {
    if get_debug_mode() != DebugMode::None {
        let c = cfg();
        let mut apps = Vec::new();
        for pair in c.debug_initiate_apps.split(',') {
            let Some((slug, origin)) = pair.trim().split_once('=') else {
                continue;
            };
            let Ok(mut callback) = Url::parse(&c.redirect_uri) else {
                continue;
            };
            if slug.is_empty() || origin.is_empty() {
                continue;
            }
            callback.query_pairs_mut().append_pair("app", slug);
            apps.push(SamnaApp {
                slug: slug.to_string(),
                redirect_uris: vec![origin.to_string(), callback.to_string()],
                ..SamnaApp::default()
            });
        }
        return Ok(Arc::new(apps));
    }
    if let Some(apps) = INITIATE_APPS.lock().expect("initiate").clone() {
        return Ok(apps);
    }
    put_initiate_apps(ctx).await
}

pub async fn put_initiate_apps(ctx: &Ctx) -> Result<Arc<Vec<SamnaApp>>> {
    let apps: Vec<SamnaApp> = client::get(ctx, "app/initiate")
        .await
        .map_err(|e| Error::other(format!("get initiate apps from the prophet: {e}")))?;
    let apps = Arc::new(apps);
    *INITIATE_APPS.lock().expect("initiate") = Some(apps.clone());
    Ok(apps)
}

pub async fn get_initiate_app(ctx: &Ctx, slug: &str) -> Result<SamnaApp> {
    let apps = get_initiate_apps(ctx).await?;
    apps.iter()
        .find(|a| a.slug == slug && !a.get_callback_uri().is_empty())
        .cloned()
        .ok_or_else(Error::not_found)
}

pub async fn get_origin_initiate_app(ctx: &Ctx, origin: &str) -> Result<SamnaApp> {
    let apps = get_initiate_apps(ctx).await?;
    apps.iter()
        .find(|a| !a.get_callback_uri().is_empty() && a.get_origins().iter().any(|o| o == origin))
        .cloned()
        .ok_or_else(Error::not_found)
}

fn pg_env(key: &str) -> Result<String> {
    std::env::var(key).map_err(|_| Error::other(format!("{key} not set")))
}

pub fn tls() -> tokio_postgres_rustls::MakeRustlsConnect {
    let roots = rustls::RootCertStore {
        roots: webpki_roots::TLS_SERVER_ROOTS.to_vec(),
    };
    let config = rustls::ClientConfig::builder_with_provider(std::sync::Arc::new(
        rustls::crypto::ring::default_provider(),
    ))
    .with_safe_default_protocol_versions()
    .expect("tls protocol versions")
    .with_root_certificates(roots)
    .with_no_client_auth();
    tokio_postgres_rustls::MakeRustlsConnect::new(config)
}

fn pool_from(dsn: &str) -> Result<Pool> {
    let config: tokio_postgres::Config = dsn
        .parse()
        .map_err(|e: tokio_postgres::Error| Error::other(format!("dsn: {e}")))?;
    let manager = Manager::from_config(
        config,
        tls(),
        ManagerConfig {
            recycling_method: RecyclingMethod::Fast,
        },
    );
    Pool::builder(manager)
        .max_size(25)
        .wait_timeout(Some(Duration::from_secs(30)))
        .create_timeout(Some(Duration::from_secs(30)))
        .runtime(deadpool_postgres::Runtime::Tokio1)
        .build()
        .map_err(|e| Error::other(format!("pool: {e}")))
}

pub async fn connect() -> Result<Db> {
    let pg_host = pg_env("PGHOST")?;
    let pg_database = pg_env("PGDATABASE")?;
    let pg_port = pg_env("PGPORT")?;
    let pg_user = pg_env("PGUSER")?;
    let pg_pass = pg_env("PGPASSWORD")?;
    let ssl_mode = std::env::var("PGSSLMODE")
        .ok()
        .filter(|s| !s.is_empty())
        .unwrap_or_else(|| "verify-full".to_string());
    let dsn = format!(
        "postgres://{pg_user}:{pg_pass}@{pg_host}:{pg_port}/{pg_database}?sslmode={ssl_mode}"
    );
    let redacted = format!(
        "postgres://{pg_user}:*********@{pg_host}:{pg_port}/{pg_database}?sslmode={ssl_mode}"
    );
    tracing::info!("DSN: {redacted}");
    let pool = pool_from(&dsn)?;
    {
        let conn = pool.get().await?;
        conn.simple_query("SELECT 1")
            .await
            .map_err(|e| Error::other(format!("ping: {e}")))?;
    }
    let replica = {
        let replica_dsn = cfg().replica_dsn;
        if replica_dsn.is_empty() {
            None
        } else {
            let replica = pool_from(&replica_dsn)?;
            let conn = replica.get().await?;
            conn.simple_query("SELECT 1")
                .await
                .map_err(|e| Error::other(format!("ping replica: {e}")))?;
            tracing::info!("reads route to the replica");
            Some(replica)
        }
    };
    let usage = ObjectUsage::start(pool.clone());
    resolve_oidc().await;
    Ok(Db {
        pool,
        replica,
        conn_str: dsn,
        usage,
        route_to_prophet: false,
        prophet: false,
        retention: Mutex::new(None),
    })
}

async fn resolve_oidc() {
    if get_debug_mode() != DebugMode::None {
        return;
    }
    let c = cfg();
    if c.endpoint.is_empty() || c.app_slug.is_empty() {
        tracing::warn!("oidc resolution skipped: endpoint or app slug missing");
        utils::set_auth_disabled();
        return;
    }
    let config_url = format!(
        "{}/api/app/{}/.well-known/openid-configuration",
        c.endpoint, c.app_slug
    );
    let oidc = match client::get_openid_configuration(&config_url).await {
        Ok(o) => o,
        Err(e) => {
            if e.is(client::ErrorKind::NotFound) {
                tracing::warn!("oidc unavailable for slug {:?}; auth disabled", c.app_slug);
            } else {
                tracing::warn!("oidc resolution failed: {e}");
            }
            utils::set_auth_disabled();
            return;
        }
    };
    let token_url = match Url::parse(&oidc.token_endpoint) {
        Ok(u) => u,
        Err(e) => {
            tracing::warn!("oidc token endpoint parse: {e}");
            utils::set_auth_disabled();
            return;
        }
    };
    let jwks_url = match Url::parse(&oidc.jwks_uri) {
        Ok(u) => u,
        Err(e) => {
            tracing::warn!("oidc jwks uri parse: {e}");
            utils::set_auth_disabled();
            return;
        }
    };
    utils::set_oidc(&oidc.issuer, token_url, jwks_url);
    tracing::info!(
        "oidc issuer={} token={} jwks={}",
        oidc.issuer,
        oidc.token_endpoint,
        oidc.jwks_uri
    );
}

pub fn model_type<T: Model>() -> &'static str {
    T::TABLE
}

pub async fn lead_sync<F, Fut>(
    stop: tokio_util::sync::CancellationToken,
    db: Arc<Db>,
    key: String,
    interval: Duration,
    work: F,
) where
    F: Fn(tokio_util::sync::CancellationToken) -> Fut + Send + Sync + 'static,
    Fut: std::future::Future<Output = ()> + Send + 'static,
{
    loop {
        match take_sync_lock(&db, &key).await {
            Err(e) if !stop.is_cancelled() => tracing::error!("disciple sync: take sync lock: {e}"),
            Err(_) => {}
            Ok(None) => {}
            Ok(Some(conn)) => hold_sync_lock(stop.clone(), conn, interval, &work).await,
        }
        tokio::select! {
            _ = stop.cancelled() => return,
            _ = tokio::time::sleep(interval) => {}
        }
    }
}

async fn take_sync_lock(db: &Db, key: &str) -> Result<Option<Conn>> {
    let conn = Conn::new(db.pool.get().await?);
    let held: bool = match conn
        .query_scalar::<bool>(
            "SELECT pg_try_advisory_lock(hashtextextended($1, 0))",
            &[&key],
        )
        .await
    {
        Ok(held) => held,
        Err(e) => {
            conn.take();
            return Err(e);
        }
    };
    if !held {
        conn.take();
        return Ok(None);
    }
    Ok(Some(conn))
}

async fn hold_sync_lock<F, Fut>(
    stop: tokio_util::sync::CancellationToken,
    conn: Conn,
    interval: Duration,
    work: &F,
) where
    F: Fn(tokio_util::sync::CancellationToken) -> Fut,
    Fut: std::future::Future<Output = ()> + Send + 'static,
{
    let work_stop = stop.child_token();
    let mut handle = tokio::spawn(work(work_stop.clone()));
    let mut ticker = tokio::time::interval(interval);
    ticker.tick().await;
    loop {
        tokio::select! {
            _ = &mut handle => {
                conn.take();
                return;
            }
            _ = ticker.tick() => {
                let check = tokio::time::timeout(interval, conn.client().simple_query("SELECT 1")).await;
                if !matches!(check, Ok(Ok(_))) {
                    work_stop.cancel();
                    let _ = (&mut handle).await;
                    conn.take();
                    return;
                }
            }
        }
    }
}

pub fn uuid_param(id: &Option<Uuid>) -> Option<Uuid> {
    *id
}
