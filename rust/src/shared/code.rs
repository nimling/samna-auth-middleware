use std::collections::HashMap;
use std::sync::{LazyLock, Mutex};
use std::time::{Duration, Instant};

use base64::Engine;
use chrono::{DateTime, Utc};
use rand::RngCore;
use serde::{Deserialize, Serialize};
use serde_json::{Map, Value};
use sha2::{Digest, Sha256};
use uuid::Uuid;

use super::db::Db;
use super::error::{Error, Result};
use super::models::{AccessClaim, Model, OBJECT_TYPE_CODE, OBJECT_TYPE_USER_CLAIM, UserClaim};
use crate::client::{self, Ctx};
use crate::sauth::Hidden;

pub const CODE_COOKIE: &str = "__Host-sauth_samk";

pub type CodeKind = String;
pub const CODE_KIND_INVITE: &str = "invite";

pub static ANONYMOUS_USER_ID: LazyLock<Uuid> =
    LazyLock::new(|| Uuid::new_v5(&Uuid::NAMESPACE_URL, b"claimius:anonymous"));

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct ClaimOptionsSamk {
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub value: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub hash: String,
    #[serde(default)]
    pub query: bool,
    #[serde(default)]
    pub header: bool,
    #[serde(default)]
    pub cookie: bool,
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct ClaimOptions {
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub samk: Option<ClaimOptionsSamk>,
}

impl ClaimOptions {
    pub fn seal(&mut self) -> Result<String> {
        let Some(samk) = &mut self.samk else {
            return Ok(String::new());
        };
        if samk.value.is_empty() && samk.hash.is_empty() {
            samk.value = get_code_value();
        }
        let value = samk.value.clone();
        if !value.is_empty() {
            samk.hash = get_code_hash(&value);
        }
        samk.value = String::new();
        Ok(value)
    }
}

pub fn seal_claim_options(raw: &Value) -> Result<(Value, String)> {
    if raw.is_null() {
        return Ok((Value::Null, String::new()));
    }
    let mut options: ClaimOptions =
        serde_json::from_value(raw.clone()).map_err(|e| Error::BadCode(format!("options: {e}")))?;
    let value = options.seal()?;
    let sealed = serde_json::to_value(&options)?;
    Ok((sealed, value))
}

pub fn show_claim_options(raw: &Value, value: &str) -> Value {
    if value.is_empty() || raw.is_null() {
        return raw.clone();
    }
    let Ok(mut options) = serde_json::from_value::<ClaimOptions>(raw.clone()) else {
        return raw.clone();
    };
    let Some(samk) = &mut options.samk else {
        return raw.clone();
    };
    samk.value = value.to_string();
    serde_json::to_value(&options).unwrap_or_else(|_| raw.clone())
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Code {
    #[serde(default, skip_serializing_if = "Uuid::is_nil")]
    pub id: Uuid,
    #[serde(skip)]
    pub hash: String,
    #[serde(default)]
    pub kind: CodeKind,
    #[serde(default)]
    pub claim_id: Uuid,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub object_id: Option<Uuid>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub object_type: Option<String>,
    #[serde(default)]
    pub data: Value,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub starts_at: Option<DateTime<Utc>>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub ends_at: Option<DateTime<Utc>>,
    #[serde(default)]
    pub count: i32,
    #[serde(default)]
    pub used_count: i32,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub revoked_at: Option<DateTime<Utc>>,
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
}

fn epoch() -> DateTime<Utc> {
    DateTime::<Utc>::from_timestamp(0, 0).unwrap_or_default()
}

impl Default for Code {
    fn default() -> Self {
        Code {
            id: Uuid::nil(),
            hash: String::new(),
            kind: String::new(),
            claim_id: Uuid::nil(),
            object_id: None,
            object_type: None,
            data: Value::Null,
            starts_at: None,
            ends_at: None,
            count: 0,
            used_count: 0,
            revoked_at: None,
            owner_id: Uuid::nil(),
            created_by: Uuid::nil(),
            deleted_at: None,
            created_at: epoch(),
            updated_at: epoch(),
        }
    }
}

impl Hidden for Code {
    fn hidden(&self) -> Vec<(&'static str, Value)> {
        if self.hash.is_empty() {
            Vec::new()
        } else {
            vec![("hash", Value::String(self.hash.clone()))]
        }
    }

    fn put_hidden(&mut self, name: &str, value: &Value) {
        if name == "hash" {
            self.hash = value.as_str().unwrap_or("").to_string();
        }
    }
}

impl Model for Code {
    const TABLE: &'static str = "claimius.code";
    const COLUMNS: &'static [(&'static str, &'static str)] = &[
        ("id", "id"),
        ("hash", "hash"),
        ("kind", "kind"),
        ("claim_id", "claim_id"),
        ("object_id", "object_id"),
        ("object_type", "object_type"),
        ("data", "data"),
        ("starts_at", "starts_at"),
        ("ends_at", "ends_at"),
        ("count", "count"),
        ("used_count", "used_count"),
        ("revoked_at", "revoked_at"),
        ("sa_owner_id", "owner_id"),
        ("sa_created_by", "created_by"),
        ("sa_deleted_at", "deleted_at"),
        ("sa_created_at", "created_at"),
        ("sa_updated_at", "updated_at"),
    ];
    const FIELDS: &'static [&'static str] = &[
        "id",
        "kind",
        "claim_id",
        "object_id",
        "object_type",
        "data",
        "starts_at",
        "ends_at",
        "count",
        "used_count",
        "revoked_at",
        "owner_id",
        "created_by",
        "deleted_at",
        "created_at",
        "updated_at",
    ];

    fn id(&self) -> Uuid {
        self.id
    }
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct CodeAccess {
    #[serde(flatten)]
    pub code: Code,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub app_id: Option<Uuid>,
}

impl Hidden for CodeAccess {
    fn put_hidden(&mut self, name: &str, value: &Value) {
        self.code.put_hidden(name, value);
    }
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct BodyCode {
    #[serde(default)]
    pub kind: CodeKind,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub object_id: Option<Uuid>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub object_type: Option<String>,
    #[serde(default, skip_serializing_if = "Value::is_null")]
    pub data: Value,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub starts_at: Option<DateTime<Utc>>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub ends_at: Option<DateTime<Utc>>,
    #[serde(default)]
    pub count: i32,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub path: String,
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct BodySamCode {
    #[serde(flatten)]
    pub body: BodyCode,
    pub claim_id: Uuid,
    pub user_id: Uuid,
    pub hash: String,
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct BodySamCodeHash {
    pub hash: String,
    #[serde(default, skip_serializing_if = "Uuid::is_nil")]
    pub user_id: Uuid,
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct BodySamCodeActor {
    pub user_id: Uuid,
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct CodeCreated {
    #[serde(flatten)]
    pub code: Code,
    pub value: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub url: String,
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct CodeRedeemed {
    pub code: Code,
    pub user_claim: UserClaim,
}

pub fn get_code_value() -> String {
    let mut b = [0u8; 32];
    rand::rng().fill_bytes(&mut b);
    base64::engine::general_purpose::URL_SAFE_NO_PAD.encode(b)
}

pub fn get_code_hash(value: &str) -> String {
    hex::encode(Sha256::digest(value.as_bytes()))
}

pub fn get_code_live(ca: &CodeAccess, now: DateTime<Utc>) -> Result<()> {
    if ca.code.revoked_at.is_some() || ca.code.deleted_at.is_some() {
        return Err(Error::CodeInvalid);
    }
    if let Some(starts) = ca.code.starts_at
        && now < starts
    {
        return Err(Error::CodeInvalid);
    }
    if let Some(ends) = ca.code.ends_at
        && now >= ends
    {
        return Err(Error::CodeInvalid);
    }
    if ca.code.count > 0 && ca.code.used_count >= ca.code.count {
        return Err(Error::CodeInvalid);
    }
    Ok(())
}

struct CachedCode {
    access: CodeAccess,
    at: Instant,
}

const CODE_CACHE_TTL: Duration = Duration::from_secs(30);

static CODE_CACHE: LazyLock<Mutex<HashMap<String, CachedCode>>> =
    LazyLock::new(|| Mutex::new(HashMap::new()));

pub fn delete_code_cache() {
    CODE_CACHE.lock().expect("code cache").clear();
}

pub fn unmarshal_code_access(raw: &Value) -> Result<CodeAccess> {
    crate::sauth::unmarshal_value(raw.clone()).map_err(Error::Other)
}

pub fn code_error(err: Error) -> Error {
    match err.pg_code() {
        Some("P0404") => return Error::NotFound(err.to_string()),
        Some("P0403") => return Error::NoAccess(err.to_string()),
        Some("22023") => return Error::BadCode(err.pg_message()),
        _ => {}
    }
    if let Error::Client(c) = &err {
        if c.is(client::ErrorKind::NotFound) {
            return Error::NotFound(c.to_string());
        }
        if c.is(client::ErrorKind::Forbidden) {
            return Error::NoAccess(c.to_string());
        }
        if c.is(client::ErrorKind::BadRequest) {
            return Error::BadCode(c.to_string());
        }
    }
    err
}

impl Db {
    pub async fn get_code_access(&self, ctx: &Ctx, value: &str) -> Result<CodeAccess> {
        let hash = get_code_hash(value);
        let now = Utc::now();
        if let Some(c) = CODE_CACHE.lock().expect("code cache").get(&hash)
            && c.at.elapsed() < CODE_CACHE_TTL
        {
            let access = c.access.clone();
            get_code_live(&access, now)?;
            return Ok(access);
        }
        let raw: Option<Value> = self
            .query_scalar_opt::<Value>(ctx, "SELECT claimius.get_code($1)", &[&hash])
            .await
            .map_err(|e| {
                if e.is_no_rows() {
                    Error::CodeInvalid
                } else {
                    Error::other(format!("get code: {e}"))
                }
            })?;
        let Some(raw) = raw.filter(|r| !r.is_null()) else {
            return Err(Error::CodeInvalid);
        };
        let ca =
            unmarshal_code_access(&raw).map_err(|e| Error::other(format!("read code: {e}")))?;
        CODE_CACHE.lock().expect("code cache").insert(
            hash,
            CachedCode {
                access: ca.clone(),
                at: Instant::now(),
            },
        );
        get_code_live(&ca, now)?;
        Ok(ca)
    }

    pub async fn get_codes(&self, ctx: &Ctx, claim_id: Uuid) -> Result<Vec<Code>> {
        match self
            .query_models::<Code>(ctx, "SELECT * FROM claimius.get_codes($1)", &[&claim_id])
            .await
        {
            Ok(codes) => Ok(codes),
            Err(e) if e.is_no_rows() => Ok(Vec::new()),
            Err(e) => Err(e),
        }
    }

    async fn apply_replayed(&self, ctx: &Ctx, events: Vec<(&str, Value)>) -> Result<()> {
        let conn = self.conn(ctx).await?;
        conn.begin().await?;
        let applied: Result<()> = async {
            conn.execute(
                "SELECT set_config('claimius.replay_mode', 'true', true)",
                &[],
            )
            .await?;
            for (event_type, raw) in events {
                super::apply::apply_event(&conn, "upsert", event_type, raw)
                    .await
                    .map_err(|e| Error::other(format!("apply {event_type}: {e}")))?;
            }
            conn.execute(
                "SELECT set_config('claimius.replay_mode', 'false', true)",
                &[],
            )
            .await?;
            Ok(())
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

    async fn apply_code(&self, ctx: &Ctx, raw: Value) -> Result<Code> {
        self.apply_replayed(ctx, vec![(OBJECT_TYPE_CODE, raw.clone())])
            .await?;
        crate::sauth::unmarshal_value(raw).map_err(Error::Other)
    }

    pub async fn post_code(
        &self,
        ctx: &Ctx,
        ac: &AccessClaim,
        claim_id: Uuid,
        body: BodyCode,
        base: &str,
    ) -> Result<CodeCreated> {
        if ac.user_id.is_nil() {
            return Err(Error::no_access());
        }
        if body.kind != CODE_KIND_INVITE {
            return Err(Error::BadCode("kind must be invite".to_string()));
        }
        if body.count < 0 {
            return Err(Error::BadCode("count must not be negative".to_string()));
        }
        let value = get_code_value();
        let hash = get_code_hash(&value);
        let code: Code = if self.route_to_prophet {
            let payload = BodySamCode {
                body: body.clone(),
                claim_id,
                user_id: ac.user_id,
                hash,
            };
            let raw: Value = client::post(ctx, "code", &payload)
                .await
                .map_err(|e| code_error(Error::Client(e)))?;
            self.apply_code(ctx, raw).await?
        } else {
            let data: Option<String> = if body.data.is_null() {
                None
            } else {
                Some(body.data.to_string())
            };
            let raw: Value = self
                .query_scalar::<Value>(ctx, "SELECT claimius.insert_code($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, $10) -> 'code'", &[&ac.user_id, &claim_id, &hash, &body.kind, &body.object_id, &body.object_type, &data, &body.starts_at, &body.ends_at, &body.count])
                .await
                .map_err(code_error)?;
            crate::sauth::unmarshal_value(raw).map_err(Error::Other)?
        };
        let mut created = CodeCreated {
            code,
            value: value.clone(),
            url: String::new(),
        };
        if !base.is_empty() {
            created.url = client::get_code_url(base, &body.path, &value)?;
        }
        Ok(created)
    }

    pub async fn delete_code(&self, ctx: &Ctx, ac: &AccessClaim, code_id: Uuid) -> Result<Code> {
        if ac.user_id.is_nil() {
            return Err(Error::no_access());
        }
        let _clear = ClearCodes;
        if self.route_to_prophet {
            let raw: Value = client::delete(
                ctx,
                &format!("code/{code_id}"),
                Some(&BodySamCodeActor {
                    user_id: ac.user_id,
                }),
            )
            .await
            .map_err(|e| code_error(Error::Client(e)))?;
            return self.apply_code(ctx, raw).await;
        }
        let raw: Value = self
            .query_scalar::<Value>(
                ctx,
                "SELECT claimius.delete_code($1, $2) -> 'code'",
                &[&ac.user_id, &code_id],
            )
            .await
            .map_err(code_error)?;
        crate::sauth::unmarshal_value(raw).map_err(Error::Other)
    }

    pub async fn put_code_used(&self, ctx: &Ctx, value: &str) -> Result<Code> {
        let hash = get_code_hash(value);
        let _forget = ForgetCode(hash.clone());
        if self.route_to_prophet {
            let raw: Value = match client::put(
                ctx,
                "code/used",
                &BodySamCodeHash {
                    hash,
                    user_id: Uuid::nil(),
                },
            )
            .await
            {
                Ok(raw) => raw,
                Err(e) if e.is(client::ErrorKind::NotFound) => return Err(Error::CodeInvalid),
                Err(e) => return Err(code_error(Error::Client(e))),
            };
            return self.apply_code(ctx, raw).await;
        }
        let codes = match self
            .query_models::<Code>(ctx, "SELECT * FROM claimius.update_code_used($1)", &[&hash])
            .await
        {
            Ok(codes) => codes,
            Err(e) if e.is_no_rows() => Vec::new(),
            Err(e) => return Err(e),
        };
        codes.into_iter().next().ok_or(Error::CodeInvalid)
    }

    pub async fn post_code_redeem(
        &self,
        ctx: &Ctx,
        value: &str,
        user_id: Uuid,
    ) -> Result<UserClaim> {
        let hash = get_code_hash(value);
        let _forget = ForgetCode(hash.clone());
        let raw: Value = if self.route_to_prophet {
            match client::post(ctx, "code/redeem", &BodySamCodeHash { hash, user_id }).await {
                Ok(raw) => raw,
                Err(e)
                    if e.is(client::ErrorKind::NotFound) || e.is(client::ErrorKind::Forbidden) =>
                {
                    return Err(Error::CodeInvalid);
                }
                Err(e) => return Err(code_error(Error::Client(e))),
            }
        } else {
            match self
                .query_scalar::<Value>(
                    ctx,
                    "SELECT claimius.redeem_code($1, $2)",
                    &[&hash, &user_id],
                )
                .await
            {
                Ok(raw) => raw,
                Err(e) if matches!(e.pg_code(), Some("P0404") | Some("P0403")) => {
                    return Err(Error::CodeInvalid);
                }
                Err(e) => return Err(e),
            }
        };
        let Value::Object(parts) = raw else {
            return Err(Error::other("redeem answered without code and user_claim"));
        };
        let code = parts.get("code").cloned().unwrap_or(Value::Null);
        let user_claim = parts.get("user_claim").cloned().unwrap_or(Value::Null);
        if self.route_to_prophet {
            self.apply_replayed(
                ctx,
                vec![
                    (OBJECT_TYPE_CODE, code),
                    (OBJECT_TYPE_USER_CLAIM, user_claim.clone()),
                ],
            )
            .await?;
        }
        crate::sauth::unmarshal_value(user_claim).map_err(Error::Other)
    }
}

pub fn seal_grant_options(raw: Value) -> Result<Value> {
    let Value::Object(mut fields) = raw else {
        return Ok(raw);
    };
    let Some(options) = fields.get("options").cloned() else {
        return Ok(Value::Object(fields));
    };
    if options.is_null() {
        return Ok(Value::Object(fields));
    }
    let (sealed, _) = seal_claim_options(&options)?;
    if sealed.is_null() {
        fields.remove("options");
    } else {
        fields.insert("options".to_string(), sealed);
    }
    Ok(Value::Object(fields))
}

struct ClearCodes;

impl Drop for ClearCodes {
    fn drop(&mut self) {
        delete_code_cache();
    }
}

struct ForgetCode(String);

impl Drop for ForgetCode {
    fn drop(&mut self) {
        CODE_CACHE.lock().expect("code cache").remove(&self.0);
    }
}

pub fn empty_map() -> Map<String, Value> {
    Map::new()
}
