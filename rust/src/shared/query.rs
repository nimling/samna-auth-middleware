use std::future::Future;
use std::sync::atomic::{AtomicU16, Ordering};
use std::sync::{Arc, Mutex};

use http::StatusCode;
use serde::Serialize;
use serde::de::DeserializeOwned;
use serde_json::{Map, Value};
use tokio_postgres::types::ToSql;
use uuid::Uuid;

use super::apply::apply_event;
use super::code::seal_grant_options;
use super::db::{Conn, ConnRef, Db};
use super::error::{Error, Result};
use super::models::{
    ACCESS_OWNER, AccessClaim, ClaimAccess, ClaimObject, Location, Model, OBJECT_TYPE_CLAIM,
    OBJECT_TYPE_CLAIM_OBJECT, OBJECT_TYPE_USER_CLAIM, UserClaim,
};
use crate::client::{self, Ctx, Forward, SyncCursor};

pub fn query_with_object(values: &[String]) -> Result<Vec<super::models::ObjectType>> {
    let mut seen = std::collections::HashSet::new();
    let mut out = Vec::new();
    for v in values {
        for raw in v.split(',') {
            let name = raw.trim();
            if name.is_empty() {
                continue;
            }
            let qualified = super::models::resolve_object_type(name)?;
            if !seen.insert(qualified.clone()) {
                continue;
            }
            out.push(qualified);
        }
    }
    Ok(out)
}

pub fn query_page(cursor: Option<&str>, limit: Option<&str>) -> Result<(Option<Uuid>, i32)> {
    let after = match cursor.filter(|c| !c.is_empty()) {
        Some(raw) => Some(
            Uuid::parse_str(raw)
                .map_err(|e| Error::bad_request(format!("cursor must be a uuid: {e}")))?,
        ),
        None => None,
    };
    let limit = match limit.filter(|l| !l.is_empty()) {
        Some(raw) => match raw.parse::<i32>() {
            Ok(n) if n >= 1 => n,
            _ => return Err(Error::bad_request("limit must be a positive integer")),
        },
        None => 0,
    };
    Ok((after, limit))
}

pub fn user_where(user_id: Uuid, required_access: ClaimAccess) -> String {
    format!(
        "(samna_user.user_id = '{user_id}' OR (samna_user.user_id IN (SELECT user_id FROM claimius.get_users('{user_id}', {required})) AND samna_user.user_id NOT IN (SELECT user_id FROM claimius.get_users WHERE type IN ('system', 'anonymous') AND sa_deleted_at IS NULL)))",
        required = required_access.0
    )
}

pub fn claim_where(user_id: Uuid, required_access: ClaimAccess) -> String {
    format!(
        "(claim.id IN (SELECT id FROM claimius.get_claims('{user_id}', {required})) OR claim.id IN (SELECT object_id FROM claimius.get_objects('{user_id}', 'claimius.claim', {required})))",
        required = required_access.0
    )
}

pub fn audit_where(user_id: Uuid, required_access: ClaimAccess) -> String {
    format!(
        "audit.id IN (SELECT id FROM claimius.get_audit('{user_id}', {}))",
        required_access.0
    )
}

pub type Params = Vec<Box<dyn ToSql + Sync + Send>>;

pub struct ObjectWhere {
    pub where_sql: String,
    pub order_sql: String,
    pub params: Params,
}

pub fn object_where(
    table: &str,
    user_id: Uuid,
    object_type: &str,
    required_access: ClaimAccess,
    object_id: Option<Uuid>,
    after: Option<Uuid>,
    limit: i32,
    first_param: usize,
) -> ObjectWhere {
    let n = first_param;
    if let Some(object_id) = object_id {
        return ObjectWhere {
            where_sql: format!(
                "{table}.id = ${n} AND EXISTS (SELECT 1 FROM claimius.get_access(${}, ${n}, ${}, ${}))",
                n + 1,
                n + 2,
                n + 3
            ),
            order_sql: String::new(),
            params: vec![
                Box::new(object_id),
                Box::new(user_id),
                Box::new(object_type.to_string()),
                Box::new(required_access.0),
            ],
        };
    }
    let page_limit: Option<i32> = if limit > 0 { Some(limit) } else { None };
    let mut order_sql = String::new();
    if after.is_some() || limit > 0 {
        order_sql = format!("{table}.id");
    }
    ObjectWhere {
        where_sql: format!(
            "{table}.id IN (SELECT object_id FROM claimius.get_objects(${n}, ${}, ${}, ${}, ${}))",
            n + 1,
            n + 2,
            n + 3,
            n + 4
        ),
        order_sql,
        params: vec![
            Box::new(user_id),
            Box::new(object_type.to_string()),
            Box::new(required_access.0),
            Box::new(after),
            Box::new(page_limit),
        ],
    }
}

pub fn params_ref(params: &Params) -> Vec<&(dyn ToSql + Sync)> {
    params
        .iter()
        .map(|p| p.as_ref() as &(dyn ToSql + Sync))
        .collect()
}

#[derive(Clone, Copy, Debug)]
pub struct EndpointSpec {
    pub base: &'static str,
    pub parents: &'static [&'static str],
}

pub trait Write: Model {
    type Result: Model;

    fn endpoint() -> Option<EndpointSpec> {
        None
    }

    fn payload_endpoint() -> Option<&'static str> {
        None
    }

    fn parent_id(&self, parent: &str) -> Result<Uuid> {
        Err(Error::other(format!(
            "parent {parent:?} not derivable from model"
        )))
    }

    fn stamp(&mut self, _ac: &AccessClaim) {}

    fn stamp_root(&mut self, _root: Uuid) {}

    fn claim_object_mut(&mut self) -> Option<&mut ClaimObject> {
        None
    }

    fn location_mut(&mut self) -> Option<&mut Location> {
        None
    }

    fn organization_owner(&self) -> Option<Uuid> {
        None
    }

    fn patch_body(&self) -> Option<Map<String, Value>> {
        self.body().and_then(|b| b.validator().cloned())
    }

    fn replace_with(&mut self, _result: &Self::Result) {}

    fn insert_local(
        &mut self,
        _db: &Db,
        _ctx: &Ctx,
        _conn: Option<&Conn>,
        _ac: Option<&AccessClaim>,
    ) -> impl Future<Output = Result<Option<(Self::Result, bool)>>> + Send {
        async { Ok(None) }
    }

    fn update_local(
        &mut self,
        _db: &Db,
        _ctx: &Ctx,
        _conn: Option<&Conn>,
        _ac: Option<&AccessClaim>,
        _id: Uuid,
        _columns: &[String],
    ) -> impl Future<Output = Result<Option<Self::Result>>> + Send {
        async { Ok(None) }
    }

    fn delete_local(
        &mut self,
        _db: &Db,
        _ctx: &Ctx,
        _conn: Option<&Conn>,
        _ac: Option<&AccessClaim>,
        _id: Uuid,
    ) -> impl Future<Output = Result<Option<Self::Result>>> + Send {
        async { Ok(None) }
    }
}

pub fn endpoint_for<W: Write>(model: &W, record_id: Uuid) -> Result<String> {
    if let Some(path) = W::payload_endpoint() {
        return Ok(path.to_string());
    }
    let Some(spec) = W::endpoint() else {
        return Err(Error::other(format!(
            "no endpoint mapping for object_type {:?}",
            W::TABLE
        )));
    };
    let mut parts: Vec<String> = Vec::new();
    for p in spec.parents {
        let pid = model
            .parent_id(p)
            .map_err(|e| Error::other(format!("endpoint {:?}: {e}", W::TABLE)))?;
        parts.push(p.to_string());
        parts.push(pid.to_string());
    }
    parts.push(spec.base.to_string());
    if !record_id.is_nil() {
        parts.push(record_id.to_string());
    }
    Ok(parts.join("/"))
}

impl Db {
    fn routes<W: Write>(&self) -> bool {
        if !self.route_to_prophet {
            return false;
        }
        W::payload_endpoint().is_some() || W::endpoint().is_some()
    }

    pub fn new_insert<'a, W: Write>(&'a self, model: &'a mut W) -> InsertQuery<'a, W> {
        InsertQuery {
            db: self,
            conn: None,
            model,
            ac: None,
            seq: SyncCursor::default(),
            created: false,
        }
    }

    pub fn new_update<'a, W: Write>(&'a self, model: &'a mut W) -> UpdateQuery<'a, W> {
        UpdateQuery {
            db: self,
            conn: None,
            model,
            ac: None,
            record_id: Uuid::nil(),
            excludes: Vec::new(),
            columns: Vec::new(),
            omit_zero: false,
            set_exprs: Vec::new(),
            seq: SyncCursor::default(),
        }
    }

    pub fn new_delete<'a, W: Write>(&'a self, model: &'a mut W) -> DeleteQuery<'a, W> {
        DeleteQuery {
            db: self,
            conn: None,
            model,
            ac: None,
            record_id: Uuid::nil(),
            seq: SyncCursor::default(),
        }
    }

    pub(crate) async fn apply_routed<R: Model>(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        raw: Value,
    ) -> Result<R> {
        if raw.is_null() {
            return Ok(R::default());
        }
        let result: R = crate::sauth::unmarshal_value(raw.clone()).map_err(Error::Other)?;
        let mut replay = raw;
        if R::TABLE == OBJECT_TYPE_USER_CLAIM {
            replay = seal_grant_options(replay)?;
        }
        let held = self.conn_or(ctx, conn).await?;
        let own_tx = conn.is_none();
        if own_tx {
            held.begin().await?;
        }
        let applied: Result<()> = async {
            held.execute(
                "SELECT set_config('claimius.replay_mode', 'true', true)",
                &[],
            )
            .await?;
            apply_event(&held, "upsert", R::TABLE, replay).await?;
            held.execute(
                "SELECT set_config('claimius.replay_mode', 'false', true)",
                &[],
            )
            .await?;
            Ok(())
        }
        .await;
        if own_tx {
            match applied {
                Ok(()) => held.commit().await?,
                Err(e) => {
                    let _ = held.rollback().await;
                    return Err(e);
                }
            }
        } else {
            applied?;
        }
        Ok(result)
    }
}

fn forward_for(ctx: &Ctx, ac: Option<&AccessClaim>) -> Ctx {
    match ac {
        Some(ac) if !ac.user_id.is_nil() => ctx.clone().with_forward(Forward {
            user_id: ac.user_id.to_string(),
            ..Forward::default()
        }),
        _ => ctx.clone(),
    }
}

fn routed_error(e: client::ClientError) -> Error {
    if e.is(client::ErrorKind::Forbidden) {
        return Error::NoAccess(e.to_string());
    }
    if e.is(client::ErrorKind::NotFound) {
        return Error::NotFound(e.to_string());
    }
    Error::Client(e)
}

pub struct InsertQuery<'a, W: Write> {
    db: &'a Db,
    conn: Option<&'a Conn>,
    model: &'a mut W,
    ac: Option<AccessClaim>,
    seq: SyncCursor,
    created: bool,
}

impl<'a, W: Write> InsertQuery<'a, W> {
    pub fn conn(mut self, conn: &'a Conn) -> Self {
        self.conn = Some(conn);
        self
    }

    pub fn access(mut self, ac: &AccessClaim) -> Self {
        self.ac = Some(ac.clone());
        self
    }

    pub fn seq(&self) -> SyncCursor {
        self.seq
    }

    pub fn created(&self) -> bool {
        self.created
    }

    pub async fn scan(&mut self, ctx: &Ctx) -> Result<W::Result> {
        if self.db.routes::<W>() {
            return self.route(ctx).await;
        }
        if let Some((result, created)) = self
            .model
            .insert_local(self.db, ctx, self.conn, self.ac.as_ref())
            .await
            .map_err(Error::claimius)?
        {
            self.created = created;
            self.model.replace_with(&result);
            return Ok(result);
        }
        let held = self.db.conn_or(ctx, self.conn).await?;
        let row = insert_generic::<W>(&held, self.model).await?;
        let result: W::Result = W::Result::from_row(row)?;
        self.model.replace_with(&result);
        Ok(result)
    }

    pub async fn exec(&mut self, ctx: &Ctx) -> Result<()> {
        self.scan(ctx).await.map(|_| ())
    }

    async fn route(&mut self, ctx: &Ctx) -> Result<W::Result> {
        let ctx = forward_for(ctx, self.ac.as_ref());
        let path = endpoint_for(&*self.model, Uuid::nil())?;
        if let Some(ac) = &self.ac {
            self.model.stamp(ac);
        }
        stamp_root(&ctx, self.db, self.model, self.ac.as_ref()).await?;
        let held = self.db.conn_or(&ctx, self.conn).await?;
        if let Some(co) = self.model.claim_object_mut()
            && co.object_created_by.is_none()
            && let (Some(object_id), Some(object_type)) = (co.object_id, co.object_type.clone())
            && !object_type.as_str().starts_with("claimius.")
        {
            let types = self.db.get_object_types(&ctx).await?;
            if types.iter().any(|t| *t == object_type) {
                let sql = format!(
                    "SELECT sa_created_by FROM {} WHERE id = $1 AND sa_deleted_at IS NULL",
                    quote_table(object_type.as_str())
                );
                match held.query_scalar_opt::<Uuid>(&sql, &[&object_id]).await {
                    Ok(Some(created_by)) => co.object_created_by = Some(created_by),
                    Ok(None) => {}
                    Err(e) if e.is_no_rows() => {}
                    Err(e) => return Err(e),
                }
            }
        }
        let body = crate::sauth::marshal(&*self.model).map_err(Error::Other)?;
        let seq = Arc::new(Mutex::new(SyncCursor::default()));
        let status = Arc::new(AtomicU16::new(0));
        let request = ctx
            .clone()
            .with_sync_seq(seq.clone())
            .with_status(status.clone());
        let raw: Value = client::post_raw(&request, &path, body)
            .await
            .map_err(routed_error)?;
        self.seq = *seq.lock().expect("seq");
        self.created = status.load(Ordering::Relaxed) == StatusCode::CREATED.as_u16();
        let result: W::Result = self.db.apply_routed(&ctx, self.conn, raw).await?;
        self.model.replace_with(&result);
        Ok(result)
    }
}

pub fn quote_table(table: &str) -> String {
    table
        .split('.')
        .map(|p| format!("\"{p}\""))
        .collect::<Vec<_>>()
        .join(".")
}

pub async fn insert_generic<W: Model>(conn: &Conn, model: &W) -> Result<Value> {
    let mut row = model.to_row()?;
    for col in ["sa_created_at", "sa_updated_at", "sa_deleted_at"] {
        row.remove(col);
    }
    if row.get("id").is_none_or(|v| v.is_null()) {
        row.remove("id");
    }
    row.retain(|_, v| !v.is_null());
    let columns: Vec<String> = row.keys().map(|c| format!("\"{c}\"")).collect();
    let table = quote_table(W::TABLE);
    let sql = format!(
        "INSERT INTO {table} ({cols}) SELECT {cols} FROM jsonb_populate_record(NULL::{table}, $1::jsonb) RETURNING row_to_json({alias})::jsonb",
        cols = columns.join(", "),
        alias = table
    );
    let rows = conn.client().query(&sql, &[&Value::Object(row)]).await?;
    rows.into_iter()
        .next()
        .map(|r| r.get::<_, Value>(0))
        .ok_or_else(Error::no_rows)
}

pub async fn update_generic<W: Model>(
    conn: &Conn,
    model: &W,
    id: Uuid,
    columns: &[String],
    excludes: &[String],
    omit_zero: bool,
    set_exprs: &[String],
) -> Result<Value> {
    let mut row = model.to_row()?;
    for col in ["id", "sa_created_at", "sa_updated_at", "sa_deleted_at"] {
        row.remove(col);
    }
    for col in excludes {
        row.remove(col.as_str());
    }
    if !columns.is_empty() {
        row.retain(|k, _| columns.iter().any(|c| c == k));
    }
    if omit_zero {
        row.retain(|_, v| !is_zero(v));
    }
    let mut sets: Vec<String> = row.keys().map(|c| format!("\"{c}\" = p.\"{c}\"")).collect();
    sets.extend(set_exprs.iter().cloned());
    if sets.is_empty() {
        return Err(Error::bad_request("no fields to update"));
    }
    let table = quote_table(W::TABLE);
    let sql = format!(
        "UPDATE {table} AS t SET {sets} FROM jsonb_populate_record(NULL::{table}, $1::jsonb) p WHERE t.id = $2 RETURNING row_to_json(t)::jsonb",
        sets = sets.join(", ")
    );
    let rows = conn
        .client()
        .query(&sql, &[&Value::Object(row), &id])
        .await?;
    rows.into_iter()
        .next()
        .map(|r| r.get::<_, Value>(0))
        .ok_or_else(Error::no_rows)
}

pub async fn delete_generic<W: Model>(conn: &Conn, id: Uuid) -> Result<Value> {
    let table = quote_table(W::TABLE);
    let sql = format!("DELETE FROM {table} AS t WHERE t.id = $1 RETURNING row_to_json(t)::jsonb");
    let rows = conn.client().query(&sql, &[&id]).await?;
    rows.into_iter()
        .next()
        .map(|r| r.get::<_, Value>(0))
        .ok_or_else(Error::no_rows)
}

fn is_zero(v: &Value) -> bool {
    match v {
        Value::Null => true,
        Value::Bool(b) => !*b,
        Value::Number(n) => n.as_f64() == Some(0.0),
        Value::String(s) => s.is_empty() || s == "00000000-0000-0000-0000-000000000000",
        Value::Array(a) => a.is_empty(),
        Value::Object(o) => o.is_empty(),
    }
}

pub struct UpdateQuery<'a, W: Write> {
    db: &'a Db,
    conn: Option<&'a Conn>,
    model: &'a mut W,
    ac: Option<AccessClaim>,
    record_id: Uuid,
    excludes: Vec<String>,
    columns: Vec<String>,
    omit_zero: bool,
    set_exprs: Vec<String>,
    seq: SyncCursor,
}

impl<'a, W: Write> UpdateQuery<'a, W> {
    pub fn conn(mut self, conn: &'a Conn) -> Self {
        self.conn = Some(conn);
        self
    }

    pub fn access(mut self, ac: &AccessClaim) -> Self {
        self.ac = Some(ac.clone());
        self
    }

    pub fn id(mut self, id: Uuid) -> Self {
        self.record_id = id;
        self
    }

    pub fn exclude_column(mut self, cols: &[&str]) -> Self {
        self.excludes.extend(cols.iter().map(|c| c.to_string()));
        self
    }

    pub fn column(mut self, cols: &[&str]) -> Self {
        self.columns.extend(cols.iter().map(|c| c.to_string()));
        self
    }

    pub fn omit_zero(mut self) -> Self {
        self.omit_zero = true;
        self
    }

    pub fn set(mut self, expr: &str) -> Self {
        self.set_exprs.push(expr.to_string());
        self
    }

    pub fn seq(&self) -> SyncCursor {
        self.seq
    }

    pub async fn scan(&mut self, ctx: &Ctx) -> Result<W::Result> {
        if self.record_id.is_nil() {
            self.record_id = self.model.record_id();
        }
        if matches!(
            W::TABLE,
            "claimius.organization"
                | "claimius.location"
                | "claimius.samna_user"
                | "claimius.claim"
        ) && self.ac.as_ref().is_none_or(|ac| {
            ac.access.0 & ACCESS_OWNER.0 == 0
                && !(W::TABLE == "claimius.samna_user" && ac.user_id == self.record_id)
        }) {
            return Err(Error::no_access());
        }
        if self.db.routes::<W>() {
            return self.route(ctx).await;
        }
        if !self.record_id.is_nil()
            && let Some(result) = self
                .model
                .update_local(
                    self.db,
                    ctx,
                    self.conn,
                    self.ac.as_ref(),
                    self.record_id,
                    &self.columns,
                )
                .await
                .map_err(Error::claimius)?
        {
            self.model.replace_with(&result);
            return Ok(result);
        }
        let held = self.db.conn_or(ctx, self.conn).await?;
        let row = update_generic::<W>(
            &held,
            self.model,
            self.record_id,
            &self.columns,
            &self.excludes,
            self.omit_zero,
            &self.set_exprs,
        )
        .await?;
        let result = W::Result::from_row(row)?;
        self.model.replace_with(&result);
        Ok(result)
    }

    pub async fn exec(&mut self, ctx: &Ctx) -> Result<()> {
        self.scan(ctx).await.map(|_| ())
    }

    async fn route(&mut self, ctx: &Ctx) -> Result<W::Result> {
        let ctx = forward_for(ctx, self.ac.as_ref());
        if self.record_id.is_nil() {
            return Err(Error::RoutedNoRecord(W::TABLE.to_string()));
        }
        if !self.set_exprs.is_empty() {
            return Err(Error::RoutedSet(W::TABLE.to_string()));
        }
        if let Some(owner) = self.model.organization_owner() {
            self.db
                .validate_organization_parent(&ctx, self.ac.as_ref(), self.record_id, owner)
                .await?;
        }
        let path = endpoint_for(&*self.model, self.record_id)?;
        let payload: Value = match self.model.patch_body() {
            Some(body) => Value::Object(body),
            None => {
                let body = crate::sauth::map(&*self.model).map_err(Error::Other)?;
                if !self.excludes.is_empty() || !self.columns.is_empty() || self.omit_zero {
                    let mut fields = body;
                    for (column, json) in W::COLUMNS {
                        let dropped = self.excludes.iter().any(|e| e == column || e == json)
                            || (!self.columns.is_empty()
                                && !self.columns.iter().any(|c| c == column || c == json))
                            || (self.omit_zero && fields.get(*json).is_some_and(is_zero));
                        if dropped {
                            fields.remove(*json);
                            fields.remove(*column);
                        }
                    }
                    Value::Object(fields)
                } else {
                    Value::Object(body)
                }
            }
        };
        let seq = Arc::new(Mutex::new(SyncCursor::default()));
        let request = ctx.clone().with_sync_seq(seq.clone());
        let raw: Value = client::patch(&request, &path, &payload)
            .await
            .map_err(routed_error)?;
        self.seq = *seq.lock().expect("seq");
        let result: W::Result = self.db.apply_routed(&ctx, self.conn, raw).await?;
        self.model.replace_with(&result);
        Ok(result)
    }
}

pub struct DeleteQuery<'a, W: Write> {
    db: &'a Db,
    conn: Option<&'a Conn>,
    model: &'a mut W,
    ac: Option<AccessClaim>,
    record_id: Uuid,
    seq: SyncCursor,
}

impl<'a, W: Write> DeleteQuery<'a, W> {
    pub fn conn(mut self, conn: &'a Conn) -> Self {
        self.conn = Some(conn);
        self
    }

    pub fn access(mut self, ac: &AccessClaim) -> Self {
        self.ac = Some(ac.clone());
        self
    }

    pub fn id(mut self, id: Uuid) -> Self {
        self.record_id = id;
        self
    }

    pub fn seq(&self) -> SyncCursor {
        self.seq
    }

    pub async fn scan(&mut self, ctx: &Ctx) -> Result<W::Result> {
        if self.record_id.is_nil() {
            self.record_id = self.model.record_id();
        }
        if matches!(
            W::TABLE,
            "claimius.organization"
                | "claimius.location"
                | "claimius.samna_user"
                | "claimius.claim"
        ) && self.ac.as_ref().is_none_or(|ac| {
            ac.access.0 & ACCESS_OWNER.0 == 0
                && !(W::TABLE == "claimius.samna_user" && ac.user_id == self.record_id)
        }) {
            return Err(Error::no_access());
        }
        if self.db.routes::<W>() {
            return self.route(ctx).await;
        }
        if !self.record_id.is_nil()
            && let Some(result) = self
                .model
                .delete_local(self.db, ctx, self.conn, self.ac.as_ref(), self.record_id)
                .await
                .map_err(Error::claimius)?
        {
            self.model.replace_with(&result);
            return Ok(result);
        }
        let held = self.db.conn_or(ctx, self.conn).await?;
        let row = delete_generic::<W>(&held, self.record_id).await?;
        let result = W::Result::from_row(row)?;
        self.model.replace_with(&result);
        Ok(result)
    }

    pub async fn exec(&mut self, ctx: &Ctx) -> Result<()> {
        self.scan(ctx).await.map(|_| ())
    }

    async fn route(&mut self, ctx: &Ctx) -> Result<W::Result> {
        let ctx = forward_for(ctx, self.ac.as_ref());
        if self.record_id.is_nil() {
            return Err(Error::RoutedNoRecord(W::TABLE.to_string()));
        }
        let path = endpoint_for(&*self.model, self.record_id)?;
        let seq = Arc::new(Mutex::new(SyncCursor::default()));
        let request = ctx.clone().with_sync_seq(seq.clone());
        let raw: Value = client::delete::<Value, Value>(&request, &path, None)
            .await
            .map_err(routed_error)?;
        self.seq = *seq.lock().expect("seq");
        let result: W::Result = self.db.apply_routed(&ctx, self.conn, raw).await?;
        self.model.replace_with(&result);
        Ok(result)
    }
}

pub fn stamp_access_claim(created_by: &mut Option<Uuid>, owner_id: &mut Uuid, ac: &AccessClaim) {
    if let Some(uc) = ac.user_claim_id
        && !uc.is_nil()
        && created_by.is_none_or(|c| c.is_nil())
    {
        *created_by = Some(uc);
    }
    if !ac.owner_id.is_nil() && owner_id.is_nil() {
        *owner_id = ac.owner_id;
    }
}

pub async fn stamp_root<W: Write>(
    ctx: &Ctx,
    db: &Db,
    model: &mut W,
    ac: Option<&AccessClaim>,
) -> Result<()> {
    if let Some(loc) = model.location_mut() {
        match loc.parent_id {
            None => {
                if loc.id.is_nil() {
                    loc.id = Uuid::new_v4();
                }
                loc.root_id = loc.id;
                loc.parent_id = Some(loc.id);
                return Ok(());
            }
            Some(parent) if parent.is_nil() => {
                if loc.id.is_nil() {
                    loc.id = Uuid::new_v4();
                }
                loc.root_id = loc.id;
                loc.parent_id = Some(loc.id);
                return Ok(());
            }
            Some(parent) => {
                if loc.root_id.is_nil() {
                    let root: Uuid = db.query_scalar(ctx, "SELECT sa_root_id FROM claimius.location WHERE id = $1 AND sa_deleted_at IS NULL", &[&parent]).await?;
                    loc.root_id = root;
                }
                return Ok(());
            }
        }
    }
    if let Some(ac) = ac
        && !ac.root_id.is_nil()
    {
        model.stamp_root(ac.root_id);
    }
    Ok(())
}

pub fn copy_into<S: Serialize, D: DeserializeOwned>(src: &S) -> Result<D> {
    let data = serde_json::to_value(src)?;
    Ok(serde_json::from_value(data)?)
}

#[derive(serde::Deserialize, Default)]
struct ClaimBindingEnvelope {
    #[serde(default)]
    id: Option<Uuid>,
    #[serde(default)]
    claim_id: Option<Uuid>,
    #[serde(default)]
    user_id: Option<Uuid>,
    #[serde(default)]
    object_type: Option<String>,
    #[serde(default)]
    object_id: Option<Uuid>,
}

pub fn claim_areas(event_type: &str, raw: &Value) -> Result<Vec<Uuid>> {
    let mut envelope: ClaimBindingEnvelope = match event_type {
        OBJECT_TYPE_CLAIM => {
            let mut e: ClaimBindingEnvelope = serde_json::from_value(raw.clone())
                .map_err(|e| Error::other(format!("unmarshal claim envelope: {e}")))?;
            e.claim_id = e.id;
            e
        }
        OBJECT_TYPE_CLAIM_OBJECT | OBJECT_TYPE_USER_CLAIM => serde_json::from_value(raw.clone())
            .map_err(|e| Error::other(format!("unmarshal claim binding envelope: {e}")))?,
        _ => return Ok(Vec::new()),
    };
    let mut areas = Vec::with_capacity(2);
    if event_type == OBJECT_TYPE_CLAIM_OBJECT
        && envelope.object_id.is_some()
        && envelope
            .object_type
            .as_deref()
            .is_some_and(|t| t != "claimius.claim")
    {
        return Ok(areas);
    }
    if let Some(claim_id) = envelope.claim_id.take() {
        areas.push(claim_id);
    }
    if event_type == OBJECT_TYPE_CLAIM_OBJECT
        && envelope.object_type.as_deref() == Some("claimius.claim")
        && let Some(object_id) = envelope.object_id
    {
        areas.push(object_id);
    }
    Ok(areas)
}

pub async fn recompute_external(conn: &Conn, event_type: &str, raw: &Value) -> Result<()> {
    let areas = claim_areas(event_type, raw)?;
    let mut assignee: Option<Uuid> = None;
    if event_type == OBJECT_TYPE_USER_CLAIM || event_type == OBJECT_TYPE_CLAIM_OBJECT {
        let envelope: ClaimBindingEnvelope = serde_json::from_value(raw.clone())
            .map_err(|e| Error::other(format!("unmarshal binding envelope: {e}")))?;
        if event_type == OBJECT_TYPE_USER_CLAIM {
            assignee = envelope.user_id;
        } else if let (Some(object_id), Some(object_type)) =
            (envelope.object_id, envelope.object_type.as_deref())
            && object_type != "claimius.claim"
        {
            conn.execute(
                "SELECT claimius._raise_object_diff($1, $2, TRUE)",
                &[&object_type, &object_id],
            )
            .await
            .map_err(|e| Error::other(format!("raise object diff for {object_id}: {e}")))?;
        }
    }
    for claim_id in areas {
        conn.execute(
            "SELECT claimius._raise_claim_diff($1, $2)",
            &[&claim_id, &assignee],
        )
        .await
        .map_err(|e| Error::other(format!("raise claim diff for {claim_id}: {e}")))?;
    }
    Ok(())
}

pub fn conn_ref<'a>(c: &'a ConnRef<'a>) -> &'a Conn {
    c
}

impl Write for UserClaim {
    type Result = UserClaim;

    fn endpoint() -> Option<EndpointSpec> {
        Some(EndpointSpec {
            base: "user",
            parents: &["claim"],
        })
    }

    fn parent_id(&self, parent: &str) -> Result<Uuid> {
        if parent == "claim" {
            if self.claim_id.is_nil() {
                return Err(Error::other("model.claim_id required"));
            }
            return Ok(self.claim_id);
        }
        Err(Error::other(format!(
            "parent {parent:?} not derivable from model"
        )))
    }

    fn stamp(&mut self, ac: &AccessClaim) {
        if let Some(uc) = ac.user_claim_id
            && !uc.is_nil()
            && self.created_by.is_nil()
        {
            self.created_by = uc;
        }
        if !ac.owner_id.is_nil() && self.owner_id.is_nil() {
            self.owner_id = ac.owner_id;
        }
    }

    fn replace_with(&mut self, result: &UserClaim) {
        let body = self.body.clone();
        *self = result.clone();
        self.body = body;
    }

    fn insert_local(
        &mut self,
        db: &Db,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
    ) -> impl Future<Output = Result<Option<(UserClaim, bool)>>> + Send {
        async move {
            if self.claim_id.is_nil() {
                return Err(Error::other(
                    "insert user_claim: claim_id required on model",
                ));
            }
            let claim_id = self.claim_id;
            let result = db.insert_claim_user(ctx, conn, ac, claim_id, self).await?;
            Ok(Some((result, false)))
        }
    }

    fn update_local(
        &mut self,
        db: &Db,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        id: Uuid,
        columns: &[String],
    ) -> impl Future<Output = Result<Option<UserClaim>>> + Send {
        async move {
            db.update_claim_user(ctx, conn, ac, id, self, columns)
                .await
                .map(Some)
        }
    }

    fn delete_local(
        &mut self,
        db: &Db,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        id: Uuid,
    ) -> impl Future<Output = Result<Option<UserClaim>>> + Send {
        async move {
            if self.claim_id.is_nil() {
                return Err(Error::other(
                    "delete user_claim: claim_id required on model",
                ));
            }
            db.delete_claim_user(ctx, conn, ac, self.claim_id, id)
                .await
                .map(Some)
        }
    }
}

impl Write for ClaimObject {
    type Result = ClaimObject;

    fn endpoint() -> Option<EndpointSpec> {
        Some(EndpointSpec {
            base: "object",
            parents: &["claim"],
        })
    }

    fn parent_id(&self, parent: &str) -> Result<Uuid> {
        if parent == "claim" {
            if self.claim_id.is_nil() {
                return Err(Error::other("model.claim_id required"));
            }
            return Ok(self.claim_id);
        }
        Err(Error::other(format!(
            "parent {parent:?} not derivable from model"
        )))
    }

    fn stamp(&mut self, ac: &AccessClaim) {
        stamp_access_claim(&mut self.created_by, &mut self.owner_id, ac);
    }

    fn stamp_root(&mut self, root: Uuid) {
        if self.root_id.is_nil() {
            self.root_id = root;
        }
    }

    fn claim_object_mut(&mut self) -> Option<&mut ClaimObject> {
        Some(self)
    }

    fn replace_with(&mut self, result: &ClaimObject) {
        let body = self.body.clone();
        *self = result.clone();
        self.body = body;
    }

    fn insert_local(
        &mut self,
        db: &Db,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
    ) -> impl Future<Output = Result<Option<(ClaimObject, bool)>>> + Send {
        async move {
            if self.claim_id.is_nil() {
                return Err(Error::other(
                    "insert claim_object: claim_id required on model",
                ));
            }
            let claim_id = self.claim_id;
            let result = db
                .insert_claim_object(ctx, conn, ac, claim_id, self)
                .await?;
            Ok(Some((result, false)))
        }
    }

    fn delete_local(
        &mut self,
        db: &Db,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        id: Uuid,
    ) -> impl Future<Output = Result<Option<ClaimObject>>> + Send {
        async move {
            if self.claim_id.is_nil() {
                return Err(Error::other(
                    "delete claim_object: claim_id required on model",
                ));
            }
            db.delete_claim_object(ctx, conn, ac, self.claim_id, id)
                .await
                .map(Some)
        }
    }
}
