use std::collections::HashMap;
use std::sync::LazyLock;

use chrono::{DateTime, Utc};
use serde::de::DeserializeOwned;
use serde::{Deserialize, Deserializer, Serialize};
use serde_json::{Map, Value};
use url::Url;
use uuid::Uuid;

use super::bind::Body;
use super::error::{Error, Result};
use crate::client::SyncCursor;
use crate::jsonx;
use crate::sauth::Hidden;
use crate::utils::CookieScope;

pub type Column = (&'static str, &'static str);

pub trait Model:
    Serialize + DeserializeOwned + Hidden + Default + Clone + Send + Sync + 'static
{
    const TABLE: &'static str;
    const COLUMNS: &'static [Column];
    const SELECT: Option<&'static str> = None;
    const FIELDS: &'static [&'static str];

    fn id(&self) -> Uuid;

    fn record_id(&self) -> Uuid {
        self.id()
    }

    fn body(&self) -> Option<&Body> {
        None
    }

    fn column_of(json: &str) -> Option<&'static str> {
        Self::COLUMNS
            .iter()
            .find(|(_, j)| *j == json)
            .map(|(c, _)| *c)
    }

    fn json_of(column: &str) -> Option<&'static str> {
        Self::COLUMNS
            .iter()
            .find(|(c, _)| *c == column)
            .map(|(_, j)| *j)
    }

    fn from_row(row: Value) -> Result<Self> {
        let Value::Object(object) = row else {
            return Err(Error::other("row is not an object"));
        };
        let mut renamed = Map::with_capacity(object.len());
        for (key, value) in object {
            match Self::json_of(&key) {
                Some(json) => {
                    renamed.insert(json.to_string(), value);
                }
                None => {
                    renamed.insert(key, value);
                }
            }
        }
        crate::sauth::unmarshal_value(Value::Object(renamed)).map_err(Error::Other)
    }

    fn to_row(&self) -> Result<Map<String, Value>> {
        let wire = crate::sauth::map(self).map_err(Error::Other)?;
        let mut row = Map::with_capacity(wire.len());
        for (key, value) in wire {
            if let Some(column) = Self::column_of(&key) {
                row.insert(column.to_string(), value);
            }
        }
        Ok(row)
    }

    fn present_columns(raw: &Map<String, Value>) -> Vec<String> {
        Self::COLUMNS
            .iter()
            .filter(|(c, j)| raw.contains_key(*c) || raw.contains_key(*j))
            .map(|(c, _)| c.to_string())
            .collect()
    }

    fn table_name(&self) -> &'static str {
        Self::TABLE
    }

    fn strip_object_schema_wire() -> bool {
        false
    }
}

pub fn strip_object_schema(qualified: &str) -> &str {
    match qualified.find('.') {
        Some(dot) => &qualified[dot + 1..],
        None => qualified,
    }
}

pub const OBJECT_TYPE_USER: &str = "claimius.samna_user";
pub const OBJECT_TYPE_ORGANIZATION: &str = "claimius.organization";
pub const OBJECT_TYPE_LOCATION: &str = "claimius.location";
pub const OBJECT_TYPE_CLAIM: &str = "claimius.claim";
pub const OBJECT_TYPE_USER_CLAIM: &str = "claimius.user_claim";
pub const OBJECT_TYPE_CLAIM_OBJECT: &str = "claimius.claim_object";
pub const OBJECT_TYPE_USER_RELATION: &str = "claimius.user_relation";
pub const OBJECT_TYPE_SAMNA_APP: &str = "claimius.samna_app";
pub const OBJECT_TYPE_SAMNA_CLIENT: &str = "claimius.samna_client";
pub const OBJECT_TYPE_SAMNA_SECRET: &str = "claimius.samna_secret";
pub const OBJECT_TYPE_AUDIT: &str = "claimius.audit";
pub const OBJECT_TYPE_OBJECT_FIELD: &str = "claimius.object_field";
pub const OBJECT_TYPE_USER_FIELD: &str = "claimius.user_field";
pub const OBJECT_TYPE_USER_TOKEN: &str = "claimius.user_identity";
pub const OBJECT_TYPE_OBJECT_ASSET: &str = "claimius.object_asset";
pub const OBJECT_TYPE_APP_COST: &str = "claimius.app_cost";
pub const OBJECT_TYPE_CODE: &str = "claimius.code";

static CLAIMIUS_OBJECT_TYPES: LazyLock<HashMap<&'static str, &'static str>> = LazyLock::new(|| {
    let all = [
        OBJECT_TYPE_USER,
        OBJECT_TYPE_ORGANIZATION,
        OBJECT_TYPE_LOCATION,
        OBJECT_TYPE_CLAIM,
        OBJECT_TYPE_USER_CLAIM,
        OBJECT_TYPE_CLAIM_OBJECT,
        OBJECT_TYPE_USER_RELATION,
        OBJECT_TYPE_SAMNA_APP,
        OBJECT_TYPE_SAMNA_CLIENT,
        OBJECT_TYPE_SAMNA_SECRET,
        OBJECT_TYPE_AUDIT,
        OBJECT_TYPE_OBJECT_FIELD,
        OBJECT_TYPE_USER_FIELD,
        OBJECT_TYPE_USER_TOKEN,
        OBJECT_TYPE_OBJECT_ASSET,
        OBJECT_TYPE_APP_COST,
        OBJECT_TYPE_CODE,
    ];
    all.into_iter()
        .map(|t| (strip_object_schema(t), t))
        .collect()
});

#[derive(Clone, Debug, Default, PartialEq, Eq, Hash, PartialOrd, Ord)]
pub struct ObjectType(pub String);

impl ObjectType {
    pub fn new(s: &str) -> Self {
        ObjectType(s.to_string())
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }

    pub fn validate(&self, known: &[&str]) -> Result<()> {
        let Some((schema, table)) = self.0.split_once('.') else {
            return Err(Error::bad_request(format!(
                "object_type must be schema qualified: {:?}",
                self.0
            )));
        };
        if schema.is_empty() || table.is_empty() {
            return Err(Error::bad_request(format!(
                "object_type must be schema qualified: {:?}",
                self.0
            )));
        }
        if known.is_empty() || known.contains(&self.0.as_str()) {
            return Ok(());
        }
        Err(Error::bad_request(format!(
            "object_type {:?} is not one of {:?}",
            self.0, known
        )))
    }
}

impl std::fmt::Display for ObjectType {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(&self.0)
    }
}

impl From<&str> for ObjectType {
    fn from(s: &str) -> Self {
        ObjectType(s.to_string())
    }
}

impl From<String> for ObjectType {
    fn from(s: String) -> Self {
        ObjectType(s)
    }
}

impl Serialize for ObjectType {
    fn serialize<S: serde::Serializer>(
        &self,
        serializer: S,
    ) -> std::result::Result<S::Ok, S::Error> {
        serializer.serialize_str(&self.0)
    }
}

impl<'de> Deserialize<'de> for ObjectType {
    fn deserialize<D: Deserializer<'de>>(deserializer: D) -> std::result::Result<Self, D::Error> {
        let s = String::deserialize(deserializer)?;
        resolve_object_type(&s).map_err(serde::de::Error::custom)
    }
}

pub fn resolve_object_type(s: &str) -> Result<ObjectType> {
    if s.is_empty() {
        return Err(Error::bad_request("object_type is required"));
    }
    if let Some(q) = CLAIMIUS_OBJECT_TYPES.get(s) {
        return Ok(ObjectType::new(q));
    }
    if s.starts_with("claimius.") {
        if let Some(q) = CLAIMIUS_OBJECT_TYPES.get(strip_object_schema(s))
            && *q == s
        {
            return Ok(ObjectType::new(q));
        }
        return Err(Error::bad_request(format!(
            "object_type {s:?} is not a claimius type"
        )));
    }
    Ok(ObjectType::new(s))
}

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash, PartialOrd, Ord)]
pub struct ClaimAccess(pub i32);

pub const ACCESS_NONE: ClaimAccess = ClaimAccess(0);
pub const ACCESS_OWNER: ClaimAccess = ClaimAccess(1 << 0);
pub const ACCESS_WRITE: ClaimAccess = ClaimAccess(1 << 1);
pub const ACCESS_READ: ClaimAccess = ClaimAccess(1 << 2);
pub const ACCESS_EXECUTE: ClaimAccess = ClaimAccess(1 << 3);
pub const ACCESS_DENY: ClaimAccess = ClaimAccess(1 << 4);
pub const ACCESS_MEMBER: ClaimAccess = ClaimAccess(1 << 5);
pub const ACCESS_ALL: ClaimAccess =
    ClaimAccess(ACCESS_OWNER.0 | ACCESS_WRITE.0 | ACCESS_READ.0 | ACCESS_EXECUTE.0);

impl std::ops::BitOr for ClaimAccess {
    type Output = ClaimAccess;
    fn bitor(self, rhs: Self) -> Self::Output {
        ClaimAccess(self.0 | rhs.0)
    }
}

impl std::ops::BitOrAssign for ClaimAccess {
    fn bitor_assign(&mut self, rhs: Self) {
        self.0 |= rhs.0;
    }
}

impl std::ops::BitAnd for ClaimAccess {
    type Output = ClaimAccess;
    fn bitand(self, rhs: Self) -> Self::Output {
        ClaimAccess(self.0 & rhs.0)
    }
}

impl ClaimAccess {
    pub fn has(self, required: ClaimAccess) -> bool {
        (self.0 & required.0) == required.0
    }

    pub fn is_zero(self) -> bool {
        self.0 == 0
    }
}

fn map_string_to_access(s: &str) -> Result<ClaimAccess> {
    match s {
        "NONE" | "0" => Ok(ACCESS_NONE),
        "OWNER" | "1" => Ok(ACCESS_OWNER),
        "WRITE" | "2" => Ok(ACCESS_WRITE),
        "READ" | "4" => Ok(ACCESS_READ),
        "EXECUTE" | "8" => Ok(ACCESS_EXECUTE),
        "DENY" | "16" => Ok(ACCESS_DENY),
        "MEMBER" | "32" => Ok(ACCESS_MEMBER),
        other => Err(Error::bad_request(format!("invalid access value: {other}"))),
    }
}

fn map_access_to_string(a: ClaimAccess) -> Result<&'static str> {
    match a {
        ACCESS_NONE => Ok("NONE"),
        ACCESS_OWNER => Ok("OWNER"),
        ACCESS_WRITE => Ok("WRITE"),
        ACCESS_READ => Ok("READ"),
        ACCESS_EXECUTE => Ok("EXECUTE"),
        ACCESS_DENY => Ok("DENY"),
        ACCESS_MEMBER => Ok("MEMBER"),
        other => Err(Error::bad_request(format!(
            "invalid access value: {}",
            other.0
        ))),
    }
}

impl Serialize for ClaimAccess {
    fn serialize<S: serde::Serializer>(
        &self,
        serializer: S,
    ) -> std::result::Result<S::Ok, S::Error> {
        serializer.serialize_i32(self.0)
    }
}

impl<'de> Deserialize<'de> for ClaimAccess {
    fn deserialize<D: Deserializer<'de>>(deserializer: D) -> std::result::Result<Self, D::Error> {
        let value = Value::deserialize(deserializer)?;
        parse_access_value(&value).map_err(serde::de::Error::custom)
    }
}

pub fn parse_access_value(value: &Value) -> Result<ClaimAccess> {
    let data = serde_json::to_vec(value)?;
    let kind = jsonx::define_json_type(&data).map_err(Error::Other)?;
    match kind {
        jsonx::TYPE_NUMBER => value
            .as_i64()
            .map(|v| ClaimAccess(v as i32))
            .ok_or_else(|| Error::bad_request("failed to parse access")),
        jsonx::TYPE_STRING => {
            let text = value.as_str().unwrap_or("");
            let mut access = ACCESS_NONE;
            for level in text.split('|') {
                access |= map_string_to_access(level)?;
            }
            Ok(access)
        }
        jsonx::TYPE_ARRAY => {
            let levels: Vec<String> = serde_json::from_value(value.clone())
                .map_err(|_| Error::bad_request("failed to parse access"))?;
            let mut access = ACCESS_NONE;
            for level in levels {
                access |= map_string_to_access(level.trim())?;
            }
            Ok(access)
        }
        _ => Err(Error::bad_request("failed to parse access")),
    }
}

impl ClaimAccess {
    pub fn slice(&self) -> Vec<String> {
        let mut out = Vec::new();
        for i in (0..6).rev() {
            if self.0 & (1 << i) != 0 {
                if let Ok(s) = map_access_to_string(ClaimAccess(1 << i)) {
                    out.push(s.to_string());
                }
            }
        }
        out
    }

    pub fn pipe_string(&self) -> String {
        self.slice().join("|")
    }

    pub fn pipe_int(&self) -> String {
        self.slice()
            .iter()
            .filter_map(|s| map_string_to_access(s).ok())
            .map(|a| a.0.to_string())
            .collect::<Vec<_>>()
            .join("|")
    }

    pub fn pipe(&self) -> String {
        self.pipe_string()
    }
}

#[derive(Clone, Copy, Debug, Default)]
pub struct AccessFormat {
    pub slice: bool,
    pub string: bool,
}

impl AccessFormat {
    pub fn parse(values: &[String]) -> Result<AccessFormat> {
        let mut f = AccessFormat::default();
        for v in values {
            for raw in v.split(',') {
                match raw.trim() {
                    "" => {}
                    "slice" => f.slice = true,
                    "string" => f.string = true,
                    name => return Err(Error::bad_request(format!("unsupported format: {name}"))),
                }
            }
        }
        Ok(f)
    }

    pub fn output(&self, a: ClaimAccess) -> (Vec<String>, String) {
        let mut slice = Vec::new();
        let mut pipe = String::new();
        if self.slice {
            let s = a.slice();
            if !s.is_empty() {
                slice = s;
            }
        }
        if self.string {
            pipe = a.pipe_string();
        }
        (slice, pipe)
    }
}

pub type ClaimType = String;
pub const CLAIM_TYPE_SYSTEM: &str = "system";
pub const CLAIM_TYPE_USER: &str = "user";
pub const CLAIM_TYPE_SERVICE: &str = "service";
pub const CLAIM_TYPE_VIRTUAL: &str = "virtual";

pub type OrganizationType = String;
pub const ORGANIZATION_TYPE_SYSTEM: &str = "system";
pub const ORGANIZATION_TYPE_CUSTOMER: &str = "customer";
pub const ORGANIZATION_TYPE_ENTITY: &str = "entity";

pub type LocationType = String;
pub type UserType = String;
pub const USER_TYPE_USER: &str = "user";
pub const USER_TYPE_CLIENT: &str = "client";
pub const USER_TYPE_SYSTEM: &str = "system";
pub const USER_TYPE_GUEST: &str = "guest";
pub const USER_TYPE_ANONYMOUS: &str = "anonymous";

pub type UserStatus = String;
pub const USER_STATUS_ACTIVE: &str = "active";
pub const USER_STATUS_PENDING: &str = "pending";
pub const USER_STATUS_SUSPENDED: &str = "suspended";
pub const USER_STATUS_INACTIVE: &str = "inactive";

fn is_nil(id: &Uuid) -> bool {
    id.is_nil()
}

fn is_nil_opt(id: &Option<Uuid>) -> bool {
    id.is_none_or(|i| i.is_nil())
}

fn default_time() -> DateTime<Utc> {
    DateTime::<Utc>::from_timestamp(0, 0).unwrap_or_default()
}

macro_rules! fields {
    ($($name:literal),* $(,)?) => { &[$($name),*] };
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct SamnaUser {
    #[serde(skip)]
    pub body: Body,
    #[serde(default, skip_serializing_if = "is_nil")]
    pub id: Uuid,
    #[serde(default, skip_serializing_if = "is_nil")]
    pub user_id: Uuid,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub first_name: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub last_name: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub user_name: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub phone: Option<String>,
    #[serde(default)]
    pub email: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub external_id: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub user_image: Option<String>,
    #[serde(default)]
    pub profile: Value,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub status: Option<String>,
    #[serde(skip)]
    pub r#type: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub config: Option<Value>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub deleted_at: Option<DateTime<Utc>>,
    #[serde(default = "default_time")]
    pub created_at: DateTime<Utc>,
    #[serde(default = "default_time")]
    pub updated_at: DateTime<Utc>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub owner_id: Option<Uuid>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub claim_id: Option<Uuid>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub created_by: Option<Uuid>,
}

impl Default for SamnaUser {
    fn default() -> Self {
        SamnaUser {
            body: Body::default(),
            id: Uuid::nil(),
            user_id: Uuid::nil(),
            first_name: None,
            last_name: None,
            user_name: None,
            phone: None,
            email: String::new(),
            external_id: None,
            user_image: None,
            profile: Value::Null,
            status: None,
            r#type: None,
            config: None,
            deleted_at: None,
            created_at: default_time(),
            updated_at: default_time(),
            owner_id: None,
            claim_id: None,
            created_by: None,
        }
    }
}

impl Hidden for SamnaUser {
    fn put_hidden(&mut self, name: &str, value: &Value) {
        if name == "type" {
            self.r#type = value.as_str().map(str::to_string);
        }
    }
}

impl Model for SamnaUser {
    const TABLE: &'static str = "claimius.samna_user";
    const SELECT: Option<&'static str> = Some("claimius.get_users");
    const COLUMNS: &'static [Column] = &[
        ("id", "id"),
        ("user_id", "user_id"),
        ("first_name", "first_name"),
        ("last_name", "last_name"),
        ("user_name", "user_name"),
        ("phone", "phone"),
        ("email", "email"),
        ("external_id", "external_id"),
        ("user_image", "user_image"),
        ("profile", "profile"),
        ("status", "status"),
        ("type", "type"),
        ("config", "config"),
        ("sa_deleted_at", "deleted_at"),
        ("sa_created_at", "created_at"),
        ("sa_updated_at", "updated_at"),
    ];
    const FIELDS: &'static [&'static str] = fields![
        "id",
        "user_id",
        "first_name",
        "last_name",
        "user_name",
        "phone",
        "email",
        "external_id",
        "user_image",
        "profile",
        "status",
        "config",
        "deleted_at",
        "created_at",
        "updated_at",
        "owner_id",
        "claim_id",
        "created_by"
    ];

    fn id(&self) -> Uuid {
        self.id
    }

    fn record_id(&self) -> Uuid {
        self.user_id
    }

    fn body(&self) -> Option<&Body> {
        Some(&self.body)
    }

    fn to_row(&self) -> Result<Map<String, Value>> {
        let mut row = model_to_row_default(self)?;
        if let Some(t) = &self.r#type {
            row.insert("type".to_string(), Value::String(t.clone()));
        }
        Ok(row)
    }
}

fn model_to_row_default<T: Model>(model: &T) -> Result<Map<String, Value>> {
    let wire = crate::sauth::map(model).map_err(Error::Other)?;
    let mut row = Map::with_capacity(wire.len());
    for (key, value) in wire {
        if let Some(column) = T::column_of(&key) {
            row.insert(column.to_string(), value);
        }
    }
    Ok(row)
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct SamnaUserToken {
    #[serde(skip)]
    pub body: Body,
    #[serde(default)]
    pub token: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub id_claim: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub user: Option<SamnaUser>,
    #[serde(default)]
    pub claim_id: Option<Uuid>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub reason: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub owner_id: Option<Uuid>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub created_by: Option<Uuid>,
}

impl Hidden for SamnaUserToken {}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct SamnaUserExternal {
    #[serde(skip)]
    pub body: Body,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub user: Option<SamnaUser>,
    #[serde(default)]
    pub claim_id: Option<Uuid>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub reason: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub owner_id: Option<Uuid>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub created_by: Option<Uuid>,
}

impl Hidden for SamnaUserExternal {}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct SamnaUserGuest {
    #[serde(skip)]
    pub body: Body,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub user: Option<SamnaUser>,
    #[serde(default)]
    pub claim_id: Option<Uuid>,
    #[serde(default)]
    pub ends_at: Option<DateTime<Utc>>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub reason: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub owner_id: Option<Uuid>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub created_by: Option<Uuid>,
}

impl Hidden for SamnaUserGuest {}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct UserIdentity {
    #[serde(default, skip_serializing_if = "is_nil")]
    pub id: Uuid,
    #[serde(default)]
    pub user_id: Uuid,
    #[serde(default)]
    pub id_claim: String,
    #[serde(default)]
    pub external_id: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub claim_id: Option<Uuid>,
    #[serde(default, skip_serializing_if = "Value::is_null")]
    pub data: Value,
    #[serde(default)]
    pub r#type: String,
    #[serde(default)]
    pub owner_id: Uuid,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub created_by: Option<Uuid>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub deleted_at: Option<DateTime<Utc>>,
    #[serde(default = "default_time")]
    pub created_at: DateTime<Utc>,
    #[serde(default = "default_time")]
    pub updated_at: DateTime<Utc>,
}

impl Default for UserIdentity {
    fn default() -> Self {
        UserIdentity {
            id: Uuid::nil(),
            user_id: Uuid::nil(),
            id_claim: String::new(),
            external_id: String::new(),
            claim_id: None,
            data: Value::Null,
            r#type: String::new(),
            owner_id: Uuid::nil(),
            created_by: None,
            deleted_at: None,
            created_at: default_time(),
            updated_at: default_time(),
        }
    }
}

impl Hidden for UserIdentity {}

impl Model for UserIdentity {
    const TABLE: &'static str = "claimius.user_identity";
    const COLUMNS: &'static [Column] = &[
        ("id", "id"),
        ("user_id", "user_id"),
        ("id_claim", "id_claim"),
        ("external_id", "external_id"),
        ("claim_id", "claim_id"),
        ("data", "data"),
        ("type", "type"),
        ("sa_owner_id", "owner_id"),
        ("sa_created_by", "created_by"),
        ("sa_deleted_at", "deleted_at"),
        ("sa_created_at", "created_at"),
        ("sa_updated_at", "updated_at"),
    ];
    const FIELDS: &'static [&'static str] = fields![
        "id",
        "user_id",
        "id_claim",
        "external_id",
        "claim_id",
        "data",
        "type",
        "owner_id",
        "created_by",
        "deleted_at",
        "created_at",
        "updated_at"
    ];

    fn id(&self) -> Uuid {
        self.id
    }
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Organization {
    #[serde(skip)]
    pub body: Body,
    #[serde(default, skip_serializing_if = "is_nil")]
    pub id: Uuid,
    #[serde(default)]
    pub name: String,
    #[serde(default)]
    pub description: Option<String>,
    #[serde(default)]
    pub kind: Option<String>,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub r#type: OrganizationType,
    #[serde(skip)]
    pub private_seed: String,
    #[serde(default)]
    pub owner_id: Uuid,
    #[serde(default)]
    pub root_id: Uuid,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub location_id: Option<Uuid>,
    #[serde(default)]
    pub level: i32,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub external_id: Option<String>,
    #[serde(skip)]
    pub provider_id: Option<Uuid>,
    #[serde(default)]
    pub created_by: Uuid,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub deleted_at: Option<DateTime<Utc>>,
    #[serde(default = "default_time")]
    pub created_at: DateTime<Utc>,
    #[serde(default = "default_time")]
    pub updated_at: DateTime<Utc>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub parent: Option<Box<Organization>>,
}

impl Default for Organization {
    fn default() -> Self {
        Organization {
            body: Body::default(),
            id: Uuid::nil(),
            name: String::new(),
            description: None,
            kind: None,
            r#type: String::new(),
            private_seed: String::new(),
            owner_id: Uuid::nil(),
            root_id: Uuid::nil(),
            location_id: None,
            level: 0,
            external_id: None,
            provider_id: None,
            created_by: Uuid::nil(),
            deleted_at: None,
            created_at: default_time(),
            updated_at: default_time(),
            parent: None,
        }
    }
}

impl Hidden for Organization {}

impl Model for Organization {
    const TABLE: &'static str = "claimius.organization";
    const SELECT: Option<&'static str> = Some("claimius.get_organizations");
    const COLUMNS: &'static [Column] = &[
        ("id", "id"),
        ("name", "name"),
        ("description", "description"),
        ("kind", "kind"),
        ("type", "type"),
        ("sa_owner_id", "owner_id"),
        ("sa_root_id", "root_id"),
        ("sa_location_id", "location_id"),
        ("sa_level", "level"),
        ("external_id", "external_id"),
        ("sa_created_by", "created_by"),
        ("sa_deleted_at", "deleted_at"),
        ("sa_created_at", "created_at"),
        ("sa_updated_at", "updated_at"),
    ];
    const FIELDS: &'static [&'static str] = fields![
        "id",
        "name",
        "description",
        "kind",
        "type",
        "owner_id",
        "root_id",
        "location_id",
        "level",
        "external_id",
        "created_by",
        "deleted_at",
        "created_at",
        "updated_at",
        "parent"
    ];

    fn id(&self) -> Uuid {
        self.id
    }

    fn body(&self) -> Option<&Body> {
        Some(&self.body)
    }
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Location {
    #[serde(skip)]
    pub body: Body,
    #[serde(default, skip_serializing_if = "is_nil")]
    pub id: Uuid,
    #[serde(default)]
    pub owner_id: Uuid,
    #[serde(default)]
    pub parent_id: Option<Uuid>,
    #[serde(default)]
    pub root_id: Uuid,
    #[serde(default)]
    pub name: Option<String>,
    #[serde(default)]
    pub description: Option<String>,
    #[serde(default)]
    pub r#type: LocationType,
    #[serde(default)]
    pub longitude: Option<f64>,
    #[serde(default)]
    pub latitude: Option<f64>,
    #[serde(default)]
    pub level: i32,
    #[serde(default)]
    pub code_type: Option<String>,
    #[serde(default)]
    pub code: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub external_id: Option<String>,
    #[serde(skip)]
    pub provider_id: Option<Uuid>,
    #[serde(default)]
    pub created_by: Uuid,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub deleted_at: Option<DateTime<Utc>>,
    #[serde(default = "default_time")]
    pub created_at: DateTime<Utc>,
    #[serde(default = "default_time")]
    pub updated_at: DateTime<Utc>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub owner: Option<Box<Organization>>,
}

impl Default for Location {
    fn default() -> Self {
        Location {
            body: Body::default(),
            id: Uuid::nil(),
            owner_id: Uuid::nil(),
            parent_id: None,
            root_id: Uuid::nil(),
            name: None,
            description: None,
            r#type: String::new(),
            longitude: None,
            latitude: None,
            level: 0,
            code_type: None,
            code: None,
            external_id: None,
            provider_id: None,
            created_by: Uuid::nil(),
            deleted_at: None,
            created_at: default_time(),
            updated_at: default_time(),
            owner: None,
        }
    }
}

impl Hidden for Location {}

impl Model for Location {
    const TABLE: &'static str = "claimius.location";
    const SELECT: Option<&'static str> = Some("claimius.get_locations");
    const COLUMNS: &'static [Column] = &[
        ("id", "id"),
        ("sa_owner_id", "owner_id"),
        ("sa_parent_id", "parent_id"),
        ("sa_root_id", "root_id"),
        ("name", "name"),
        ("description", "description"),
        ("type", "type"),
        ("longitude", "longitude"),
        ("latitude", "latitude"),
        ("sa_level", "level"),
        ("code_type", "code_type"),
        ("code", "code"),
        ("external_id", "external_id"),
        ("sa_created_by", "created_by"),
        ("sa_deleted_at", "deleted_at"),
        ("sa_created_at", "created_at"),
        ("sa_updated_at", "updated_at"),
    ];
    const FIELDS: &'static [&'static str] = fields![
        "id",
        "owner_id",
        "parent_id",
        "root_id",
        "name",
        "description",
        "type",
        "longitude",
        "latitude",
        "level",
        "code_type",
        "code",
        "external_id",
        "created_by",
        "deleted_at",
        "created_at",
        "updated_at",
        "owner"
    ];

    fn id(&self) -> Uuid {
        self.id
    }

    fn body(&self) -> Option<&Body> {
        Some(&self.body)
    }
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Claim {
    #[serde(skip)]
    pub body: Body,
    #[serde(default, skip_serializing_if = "is_nil")]
    pub id: Uuid,
    #[serde(default)]
    pub name: String,
    #[serde(default)]
    pub description: Option<String>,
    #[serde(default)]
    pub access: ClaimAccess,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub access_slice: Vec<String>,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub access_string: String,
    #[serde(default)]
    pub inherits: bool,
    #[serde(default)]
    pub level: i32,
    #[serde(default)]
    pub r#type: ClaimType,
    #[serde(default)]
    pub owner_id: Uuid,
    #[serde(default)]
    pub root_id: Uuid,
    #[serde(default)]
    pub created_by: Option<Uuid>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub deleted_at: Option<DateTime<Utc>>,
    #[serde(default = "default_time")]
    pub created_at: DateTime<Utc>,
    #[serde(default = "default_time")]
    pub updated_at: DateTime<Utc>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub claim_objects: Vec<ClaimObject>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub claim_users: Vec<UserClaim>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub owner: Option<Box<Organization>>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub creator: Option<Creator>,
}

impl Default for Claim {
    fn default() -> Self {
        Claim {
            body: Body::default(),
            id: Uuid::nil(),
            name: String::new(),
            description: None,
            access: ACCESS_NONE,
            access_slice: Vec::new(),
            access_string: String::new(),
            inherits: false,
            level: 0,
            r#type: String::new(),
            owner_id: Uuid::nil(),
            root_id: Uuid::nil(),
            created_by: None,
            deleted_at: None,
            created_at: default_time(),
            updated_at: default_time(),
            claim_objects: Vec::new(),
            claim_users: Vec::new(),
            owner: None,
            creator: None,
        }
    }
}

impl Hidden for Claim {}

impl Model for Claim {
    const TABLE: &'static str = "claimius.claim";
    const SELECT: Option<&'static str> = Some("claimius.get_claims");
    const COLUMNS: &'static [Column] = &[
        ("id", "id"),
        ("name", "name"),
        ("description", "description"),
        ("sa_access", "access"),
        ("inherits", "inherits"),
        ("level", "level"),
        ("type", "type"),
        ("sa_owner_id", "owner_id"),
        ("sa_root_id", "root_id"),
        ("sa_created_by", "created_by"),
        ("sa_deleted_at", "deleted_at"),
        ("sa_created_at", "created_at"),
        ("sa_updated_at", "updated_at"),
    ];
    const FIELDS: &'static [&'static str] = fields![
        "id",
        "name",
        "description",
        "access",
        "access_slice",
        "access_string",
        "inherits",
        "level",
        "type",
        "owner_id",
        "root_id",
        "created_by",
        "deleted_at",
        "created_at",
        "updated_at",
        "claim_objects",
        "claim_users",
        "owner",
        "creator"
    ];

    fn id(&self) -> Uuid {
        self.id
    }

    fn body(&self) -> Option<&Body> {
        Some(&self.body)
    }
}

#[derive(Clone, Debug, Default)]
pub struct CreateClaimParams {
    pub name: String,
    pub description: Option<String>,
    pub access: ClaimAccess,
    pub inherits: Option<bool>,
    pub r#type: ClaimType,
    pub level: i32,
}

#[derive(Clone, Debug, Default)]
pub struct ReCreateForOptions {
    pub name: Option<String>,
    pub copy_users: bool,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct UserClaim {
    #[serde(skip)]
    pub body: Body,
    #[serde(default, skip_serializing_if = "is_nil")]
    pub id: Uuid,
    #[serde(default)]
    pub claim_id: Uuid,
    #[serde(default)]
    pub user_id: Uuid,
    #[serde(default)]
    pub reason: Option<String>,
    #[serde(default)]
    pub starts_at: Option<DateTime<Utc>>,
    #[serde(default)]
    pub ends_at: Option<DateTime<Utc>>,
    #[serde(default, skip_serializing_if = "Value::is_null")]
    pub options: Value,
    #[serde(default)]
    pub owner_id: Uuid,
    #[serde(default)]
    pub created_by: Uuid,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub deleted_at: Option<DateTime<Utc>>,
    #[serde(default = "default_time")]
    pub created_at: DateTime<Utc>,
    #[serde(default = "default_time")]
    pub updated_at: DateTime<Utc>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub owner: Option<Box<Organization>>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub user: Option<Box<SamnaUser>>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub access: Option<Box<AccessClaim>>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub access_slice: Vec<String>,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub access_string: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub creator: Option<Creator>,
}

impl Default for UserClaim {
    fn default() -> Self {
        UserClaim {
            body: Body::default(),
            id: Uuid::nil(),
            claim_id: Uuid::nil(),
            user_id: Uuid::nil(),
            reason: None,
            starts_at: None,
            ends_at: None,
            options: Value::Null,
            owner_id: Uuid::nil(),
            created_by: Uuid::nil(),
            deleted_at: None,
            created_at: default_time(),
            updated_at: default_time(),
            owner: None,
            user: None,
            access: None,
            access_slice: Vec::new(),
            access_string: String::new(),
            creator: None,
        }
    }
}

impl Hidden for UserClaim {}

impl Model for UserClaim {
    const TABLE: &'static str = "claimius.user_claim";
    const SELECT: Option<&'static str> = Some("claimius.get_grants");
    const COLUMNS: &'static [Column] = &[
        ("id", "id"),
        ("claim_id", "claim_id"),
        ("user_id", "user_id"),
        ("reason", "reason"),
        ("starts_at", "starts_at"),
        ("ends_at", "ends_at"),
        ("options", "options"),
        ("sa_owner_id", "owner_id"),
        ("sa_created_by", "created_by"),
        ("sa_deleted_at", "deleted_at"),
        ("sa_created_at", "created_at"),
        ("sa_updated_at", "updated_at"),
    ];
    const FIELDS: &'static [&'static str] = fields![
        "id",
        "claim_id",
        "user_id",
        "reason",
        "starts_at",
        "ends_at",
        "options",
        "owner_id",
        "created_by",
        "deleted_at",
        "created_at",
        "updated_at",
        "owner",
        "user",
        "access",
        "access_slice",
        "access_string",
        "creator"
    ];

    fn id(&self) -> Uuid {
        self.id
    }

    fn body(&self) -> Option<&Body> {
        Some(&self.body)
    }
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct ClaimObject {
    #[serde(skip)]
    pub body: Body,
    #[serde(default, skip_serializing_if = "is_nil")]
    pub id: Uuid,
    #[serde(default)]
    pub claim_id: Uuid,
    #[serde(default)]
    pub object_id: Option<Uuid>,
    #[serde(default)]
    pub object_type: Option<ObjectType>,
    #[serde(default)]
    pub reason: Option<String>,
    #[serde(default)]
    pub scope: Option<Value>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub access: Option<ClaimAccess>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub effective_access: Option<ClaimAccess>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub access_slice: Vec<String>,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub access_string: String,
    #[serde(default)]
    pub inherits: bool,
    #[serde(default)]
    pub ref_id: Option<Uuid>,
    #[serde(default)]
    pub owner_id: Uuid,
    #[serde(default)]
    pub root_id: Uuid,
    #[serde(default)]
    pub created_by: Option<Uuid>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub object_created_by: Option<Uuid>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub deleted_at: Option<DateTime<Utc>>,
    #[serde(default = "default_time")]
    pub created_at: DateTime<Utc>,
    #[serde(default = "default_time")]
    pub updated_at: DateTime<Utc>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub owner: Option<Box<Organization>>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub creator: Option<Creator>,
}

impl Default for ClaimObject {
    fn default() -> Self {
        ClaimObject {
            body: Body::default(),
            id: Uuid::nil(),
            claim_id: Uuid::nil(),
            object_id: None,
            object_type: None,
            reason: None,
            scope: None,
            access: None,
            effective_access: None,
            access_slice: Vec::new(),
            access_string: String::new(),
            inherits: false,
            ref_id: None,
            owner_id: Uuid::nil(),
            root_id: Uuid::nil(),
            created_by: None,
            object_created_by: None,
            deleted_at: None,
            created_at: default_time(),
            updated_at: default_time(),
            owner: None,
            creator: None,
        }
    }
}

impl Hidden for ClaimObject {}

impl Model for ClaimObject {
    const TABLE: &'static str = "claimius.claim_object";
    const COLUMNS: &'static [Column] = &[
        ("id", "id"),
        ("claim_id", "claim_id"),
        ("object_id", "object_id"),
        ("object_type", "object_type"),
        ("reason", "reason"),
        ("scope", "scope"),
        ("sa_access", "access"),
        ("effective_access", "effective_access"),
        ("inherits", "inherits"),
        ("ref_id", "ref_id"),
        ("sa_owner_id", "owner_id"),
        ("sa_root_id", "root_id"),
        ("sa_created_by", "created_by"),
        ("sa_deleted_at", "deleted_at"),
        ("sa_created_at", "created_at"),
        ("sa_updated_at", "updated_at"),
    ];
    const FIELDS: &'static [&'static str] = fields![
        "id",
        "claim_id",
        "object_id",
        "object_type",
        "reason",
        "scope",
        "access",
        "effective_access",
        "access_slice",
        "access_string",
        "inherits",
        "ref_id",
        "owner_id",
        "root_id",
        "created_by",
        "object_created_by",
        "deleted_at",
        "created_at",
        "updated_at",
        "owner",
        "creator"
    ];

    fn id(&self) -> Uuid {
        self.id
    }

    fn body(&self) -> Option<&Body> {
        Some(&self.body)
    }

    fn present_columns(raw: &Map<String, Value>) -> Vec<String> {
        Self::COLUMNS
            .iter()
            .filter(|(c, j)| {
                *c != "effective_access" && (raw.contains_key(*c) || raw.contains_key(*j))
            })
            .map(|(c, _)| c.to_string())
            .collect()
    }
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct AccessClaim {
    #[serde(default)]
    pub user_object_id: Uuid,
    #[serde(skip)]
    pub user_id: Uuid,
    #[serde(default)]
    pub user_claim_id: Option<Uuid>,
    #[serde(default)]
    pub claim_id: Option<Uuid>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub claim_name: Option<String>,
    #[serde(default)]
    pub access: ClaimAccess,
    #[serde(default)]
    pub scope: Option<Value>,
    #[serde(default)]
    pub owner_id: Uuid,
    #[serde(default)]
    pub root_id: Uuid,
    #[serde(default)]
    pub location_id: Option<Uuid>,
    #[serde(default)]
    pub direct_grant: bool,
    #[serde(default)]
    pub level: i32,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub objects: Vec<ClaimObject>,
}

impl Default for AccessClaim {
    fn default() -> Self {
        AccessClaim {
            user_object_id: Uuid::nil(),
            user_id: Uuid::nil(),
            user_claim_id: None,
            claim_id: None,
            claim_name: None,
            access: ACCESS_NONE,
            scope: None,
            owner_id: Uuid::nil(),
            root_id: Uuid::nil(),
            location_id: None,
            direct_grant: false,
            level: 0,
            objects: Vec::new(),
        }
    }
}

impl Hidden for AccessClaim {}

impl Model for AccessClaim {
    const TABLE: &'static str = "claimius.user_object";
    const COLUMNS: &'static [Column] = &[
        ("user_object_id", "user_object_id"),
        ("user_claim_id", "user_claim_id"),
        ("claim_id", "claim_id"),
        ("claim_name", "claim_name"),
        ("sa_access", "access"),
        ("scope", "scope"),
        ("sa_owner_id", "owner_id"),
        ("sa_root_id", "root_id"),
        ("sa_location_id", "location_id"),
        ("direct_grant", "direct_grant"),
        ("level", "level"),
    ];
    const FIELDS: &'static [&'static str] = fields![
        "user_object_id",
        "user_claim_id",
        "claim_id",
        "claim_name",
        "access",
        "scope",
        "owner_id",
        "root_id",
        "location_id",
        "direct_grant",
        "level",
        "objects"
    ];

    fn id(&self) -> Uuid {
        self.user_object_id
    }
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct AccessUser {
    #[serde(default)]
    pub target_user_id: Uuid,
    #[serde(default)]
    pub access: ClaimAccess,
    #[serde(default)]
    pub user_claim_id: Option<Uuid>,
    #[serde(default)]
    pub claim_id: Option<Uuid>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub claim_name: Option<String>,
    #[serde(default)]
    pub owner_id: Option<Uuid>,
    #[serde(default)]
    pub root_id: Option<Uuid>,
    #[serde(default)]
    pub location_id: Option<Uuid>,
    #[serde(default)]
    pub direct_grant: bool,
}

impl Hidden for AccessUser {}

impl Model for AccessUser {
    const TABLE: &'static str = "claimius.user_users";
    const COLUMNS: &'static [Column] = &[
        ("target_user_id", "target_user_id"),
        ("access", "access"),
        ("user_claim_id", "user_claim_id"),
        ("claim_id", "claim_id"),
        ("claim_name", "claim_name"),
        ("owner_id", "owner_id"),
        ("root_id", "root_id"),
        ("location_id", "location_id"),
        ("direct_grant", "direct_grant"),
    ];
    const FIELDS: &'static [&'static str] = fields![
        "target_user_id",
        "access",
        "user_claim_id",
        "claim_id",
        "claim_name",
        "owner_id",
        "root_id",
        "location_id",
        "direct_grant"
    ];

    fn id(&self) -> Uuid {
        self.target_user_id
    }
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Creator {
    #[serde(default)]
    pub id: Uuid,
    #[serde(default)]
    pub user_id: Uuid,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub first_name: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub last_name: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub user_name: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub phone: Option<String>,
    #[serde(default)]
    pub email: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub external_id: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub user_image: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub status: Option<String>,
    #[serde(default = "default_time")]
    pub created_at: DateTime<Utc>,
    #[serde(default = "default_time")]
    pub updated_at: DateTime<Utc>,
}

impl Default for Creator {
    fn default() -> Self {
        Creator {
            id: Uuid::nil(),
            user_id: Uuid::nil(),
            first_name: None,
            last_name: None,
            user_name: None,
            phone: None,
            email: String::new(),
            external_id: None,
            user_image: None,
            status: None,
            created_at: default_time(),
            updated_at: default_time(),
        }
    }
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Asset {
    #[serde(skip)]
    pub body: Body,
    #[serde(default)]
    pub id: Uuid,
    #[serde(default)]
    pub name: String,
    #[serde(default)]
    pub description: Option<String>,
    #[serde(default)]
    pub mime_type: String,
    #[serde(default)]
    pub status: String,
    #[serde(default)]
    pub index: i32,
    #[serde(skip)]
    pub blob_url: Option<String>,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub asset_url: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub deleted_at: Option<DateTime<Utc>>,
    #[serde(default = "default_time")]
    pub created_at: DateTime<Utc>,
    #[serde(default = "default_time")]
    pub updated_at: DateTime<Utc>,
}

impl Default for Asset {
    fn default() -> Self {
        Asset {
            body: Body::default(),
            id: Uuid::nil(),
            name: String::new(),
            description: None,
            mime_type: String::new(),
            status: String::new(),
            index: 0,
            blob_url: None,
            asset_url: String::new(),
            deleted_at: None,
            created_at: default_time(),
            updated_at: default_time(),
        }
    }
}

impl Hidden for Asset {}

impl Model for Asset {
    const TABLE: &'static str = "public.asset";
    const COLUMNS: &'static [Column] = &[
        ("id", "id"),
        ("name", "name"),
        ("description", "description"),
        ("mime_type", "mime_type"),
        ("status", "status"),
        ("index", "index"),
        ("sa_deleted_at", "deleted_at"),
        ("sa_created_at", "created_at"),
        ("sa_updated_at", "updated_at"),
    ];
    const FIELDS: &'static [&'static str] = fields![
        "id",
        "name",
        "description",
        "mime_type",
        "status",
        "index",
        "asset_url",
        "deleted_at",
        "created_at",
        "updated_at"
    ];

    fn id(&self) -> Uuid {
        self.id
    }

    fn body(&self) -> Option<&Body> {
        Some(&self.body)
    }
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct GraphNode {
    #[serde(default)]
    pub object_id: Uuid,
    #[serde(default)]
    pub object_type: ObjectType,
    #[serde(default)]
    pub label: String,
    #[serde(default)]
    pub parent_id: Option<Uuid>,
    #[serde(default)]
    pub data: Value,
}

impl Hidden for GraphNode {}

impl Model for GraphNode {
    const TABLE: &'static str = "graph";
    const COLUMNS: &'static [Column] = &[
        ("object_id", "object_id"),
        ("object_type", "object_type"),
        ("label", "label"),
        ("parent_id", "parent_id"),
        ("data", "data"),
    ];
    const FIELDS: &'static [&'static str] =
        fields!["object_id", "object_type", "label", "parent_id", "data"];

    fn id(&self) -> Uuid {
        self.object_id
    }
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct SamnaClient {
    #[serde(skip)]
    pub body: Body,
    #[serde(default, skip_serializing_if = "is_nil")]
    pub id: Uuid,
    #[serde(default)]
    pub name: String,
    #[serde(default)]
    pub description: Option<String>,
    #[serde(default, skip_serializing_if = "is_nil")]
    pub client_id: Uuid,
    #[serde(skip)]
    pub secret_hash: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub secret: Option<String>,
    #[serde(default)]
    pub status: String,
    #[serde(default)]
    pub expires_at: Option<DateTime<Utc>>,
    #[serde(default)]
    pub owner_id: Uuid,
    #[serde(default)]
    pub created_by: Option<Uuid>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub deleted_at: Option<DateTime<Utc>>,
    #[serde(default = "default_time")]
    pub created_at: DateTime<Utc>,
    #[serde(default = "default_time")]
    pub updated_at: DateTime<Utc>,
}

impl Default for SamnaClient {
    fn default() -> Self {
        SamnaClient {
            body: Body::default(),
            id: Uuid::nil(),
            name: String::new(),
            description: None,
            client_id: Uuid::nil(),
            secret_hash: String::new(),
            secret: None,
            status: String::new(),
            expires_at: None,
            owner_id: Uuid::nil(),
            created_by: None,
            deleted_at: None,
            created_at: default_time(),
            updated_at: default_time(),
        }
    }
}

impl Hidden for SamnaClient {
    fn hidden(&self) -> Vec<(&'static str, Value)> {
        if self.secret_hash.is_empty() {
            Vec::new()
        } else {
            vec![("secret_hash", Value::String(self.secret_hash.clone()))]
        }
    }

    fn put_hidden(&mut self, name: &str, value: &Value) {
        if name == "secret_hash" {
            self.secret_hash = value.as_str().unwrap_or("").to_string();
        }
    }
}

impl Model for SamnaClient {
    const TABLE: &'static str = "claimius.samna_client";
    const COLUMNS: &'static [Column] = &[
        ("id", "id"),
        ("name", "name"),
        ("description", "description"),
        ("client_id", "client_id"),
        ("secret_hash", "secret_hash"),
        ("status", "status"),
        ("expires_at", "expires_at"),
        ("sa_owner_id", "owner_id"),
        ("sa_created_by", "created_by"),
        ("sa_deleted_at", "deleted_at"),
        ("sa_created_at", "created_at"),
        ("sa_updated_at", "updated_at"),
    ];
    const FIELDS: &'static [&'static str] = fields![
        "id",
        "name",
        "description",
        "client_id",
        "secret",
        "status",
        "expires_at",
        "owner_id",
        "created_by",
        "deleted_at",
        "created_at",
        "updated_at"
    ];

    fn id(&self) -> Uuid {
        self.id
    }

    fn body(&self) -> Option<&Body> {
        Some(&self.body)
    }
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct SamnaSecret {
    #[serde(skip)]
    pub body: Body,
    #[serde(default, skip_serializing_if = "is_nil")]
    pub id: Uuid,
    #[serde(default)]
    pub key: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub value: String,
    #[serde(default)]
    pub expires_at: Option<DateTime<Utc>>,
    #[serde(default)]
    pub owner_id: Uuid,
    #[serde(default)]
    pub created_by: Option<Uuid>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub deleted_at: Option<DateTime<Utc>>,
    #[serde(default = "default_time")]
    pub created_at: DateTime<Utc>,
    #[serde(default = "default_time")]
    pub updated_at: DateTime<Utc>,
}

impl Default for SamnaSecret {
    fn default() -> Self {
        SamnaSecret {
            body: Body::default(),
            id: Uuid::nil(),
            key: String::new(),
            value: String::new(),
            expires_at: None,
            owner_id: Uuid::nil(),
            created_by: None,
            deleted_at: None,
            created_at: default_time(),
            updated_at: default_time(),
        }
    }
}

impl Hidden for SamnaSecret {}

impl Model for SamnaSecret {
    const TABLE: &'static str = "claimius.samna_secret";
    const COLUMNS: &'static [Column] = &[
        ("id", "id"),
        ("key", "key"),
        ("value", "value"),
        ("expires_at", "expires_at"),
        ("sa_owner_id", "owner_id"),
        ("sa_created_by", "created_by"),
        ("sa_deleted_at", "deleted_at"),
        ("sa_created_at", "created_at"),
        ("sa_updated_at", "updated_at"),
    ];
    const FIELDS: &'static [&'static str] = fields![
        "id",
        "key",
        "value",
        "expires_at",
        "owner_id",
        "created_by",
        "deleted_at",
        "created_at",
        "updated_at"
    ];

    fn id(&self) -> Uuid {
        self.id
    }

    fn body(&self) -> Option<&Body> {
        Some(&self.body)
    }
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct SamnaApp {
    #[serde(default, skip_serializing_if = "is_nil")]
    pub id: Uuid,
    #[serde(default)]
    pub slug: String,
    #[serde(default)]
    pub name: String,
    #[serde(default)]
    pub description: String,
    #[serde(default)]
    pub contact_email: Option<String>,
    #[serde(default)]
    pub app_image: Option<String>,
    #[serde(default)]
    pub redirect_uris: Vec<String>,
    #[serde(default)]
    pub sync_uri: String,
    #[serde(default)]
    pub provider_ids: Vec<Uuid>,
    #[serde(default)]
    pub status: String,
    #[serde(default)]
    pub connection: String,
    #[serde(skip)]
    pub private_key: String,
    #[serde(skip)]
    pub private_seed: String,
    #[serde(skip)]
    pub app_secret: Option<String>,
    #[serde(default)]
    pub secret_version: i32,
    #[serde(default)]
    pub claim_id: Option<Uuid>,
    #[serde(default)]
    pub style: Option<Value>,
    #[serde(default)]
    pub owner_id: Uuid,
    #[serde(default)]
    pub parent_id: Uuid,
    #[serde(default)]
    pub root_id: Uuid,
    #[serde(default)]
    pub cookie_domain: Option<String>,
    #[serde(default)]
    pub cookie_same_site: Option<String>,
    #[serde(default)]
    pub created_by: Uuid,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub deleted_at: Option<DateTime<Utc>>,
    #[serde(default = "default_time")]
    pub created_at: DateTime<Utc>,
    #[serde(default = "default_time")]
    pub updated_at: DateTime<Utc>,
}

impl Default for SamnaApp {
    fn default() -> Self {
        SamnaApp {
            id: Uuid::nil(),
            slug: String::new(),
            name: String::new(),
            description: String::new(),
            contact_email: None,
            app_image: None,
            redirect_uris: Vec::new(),
            sync_uri: String::new(),
            provider_ids: Vec::new(),
            status: String::new(),
            connection: String::new(),
            private_key: String::new(),
            private_seed: String::new(),
            app_secret: None,
            secret_version: 0,
            claim_id: None,
            style: None,
            owner_id: Uuid::nil(),
            parent_id: Uuid::nil(),
            root_id: Uuid::nil(),
            cookie_domain: None,
            cookie_same_site: None,
            created_by: Uuid::nil(),
            deleted_at: None,
            created_at: default_time(),
            updated_at: default_time(),
        }
    }
}

impl Hidden for SamnaApp {}

impl SamnaApp {
    pub fn get_primary_origin(&self) -> String {
        self.redirect_uris.first().cloned().unwrap_or_default()
    }

    pub fn get_callback_uri(&self) -> String {
        for uri in &self.redirect_uris {
            if let Ok(parsed) = Url::parse(uri)
                && parsed
                    .query_pairs()
                    .any(|(k, v)| k == "app" && v == self.slug)
            {
                return uri.clone();
            }
        }
        String::new()
    }

    pub fn get_origins(&self) -> Vec<String> {
        self.redirect_uris
            .iter()
            .filter_map(|uri| {
                let parsed = Url::parse(uri).ok()?;
                let host = parsed.host_str()?;
                let port = parsed.port().map(|p| format!(":{p}")).unwrap_or_default();
                Some(format!("{}://{host}{port}", parsed.scheme()))
            })
            .collect()
    }

    pub fn get_cookie_scope(&self) -> CookieScope {
        CookieScope {
            domain: self.cookie_domain.clone().unwrap_or_default(),
            same_site: self.cookie_same_site.clone().unwrap_or_default(),
        }
    }
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct ObjectField {
    #[serde(skip)]
    pub body: Body,
    #[serde(default, skip_serializing_if = "is_nil")]
    pub id: Uuid,
    #[serde(default)]
    pub object_type: ObjectType,
    #[serde(default)]
    pub name: String,
    #[serde(default)]
    pub description: Option<String>,
    #[serde(default)]
    pub json_schema: Option<Value>,
    #[serde(default)]
    pub required: bool,
    #[serde(default)]
    pub value: Option<String>,
    #[serde(default)]
    pub owner_id: Uuid,
    #[serde(default)]
    pub created_by: Option<Uuid>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub deleted_at: Option<DateTime<Utc>>,
    #[serde(default = "default_time")]
    pub created_at: DateTime<Utc>,
    #[serde(default = "default_time")]
    pub updated_at: DateTime<Utc>,
}

impl Default for ObjectField {
    fn default() -> Self {
        ObjectField {
            body: Body::default(),
            id: Uuid::nil(),
            object_type: ObjectType::default(),
            name: String::new(),
            description: None,
            json_schema: None,
            required: false,
            value: None,
            owner_id: Uuid::nil(),
            created_by: None,
            deleted_at: None,
            created_at: default_time(),
            updated_at: default_time(),
        }
    }
}

impl Hidden for ObjectField {}

impl Model for ObjectField {
    const TABLE: &'static str = "claimius.object_field";
    const COLUMNS: &'static [Column] = &[
        ("id", "id"),
        ("object_type", "object_type"),
        ("name", "name"),
        ("description", "description"),
        ("json_schema", "json_schema"),
        ("required", "required"),
        ("value", "value"),
        ("sa_owner_id", "owner_id"),
        ("sa_created_by", "created_by"),
        ("sa_deleted_at", "deleted_at"),
        ("sa_created_at", "created_at"),
        ("sa_updated_at", "updated_at"),
    ];
    const FIELDS: &'static [&'static str] = fields![
        "id",
        "object_type",
        "name",
        "description",
        "json_schema",
        "required",
        "value",
        "owner_id",
        "created_by",
        "deleted_at",
        "created_at",
        "updated_at"
    ];

    fn id(&self) -> Uuid {
        self.id
    }

    fn body(&self) -> Option<&Body> {
        Some(&self.body)
    }
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct UserField {
    #[serde(skip)]
    pub body: Body,
    #[serde(default, skip_serializing_if = "is_nil")]
    pub id: Uuid,
    #[serde(default)]
    pub field_id: Uuid,
    #[serde(default)]
    pub user_id: Uuid,
    #[serde(default)]
    pub value: Option<String>,
    #[serde(default)]
    pub owner_id: Uuid,
    #[serde(default)]
    pub created_by: Option<Uuid>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub deleted_at: Option<DateTime<Utc>>,
    #[serde(default = "default_time")]
    pub created_at: DateTime<Utc>,
    #[serde(default = "default_time")]
    pub updated_at: DateTime<Utc>,
}

impl Default for UserField {
    fn default() -> Self {
        UserField {
            body: Body::default(),
            id: Uuid::nil(),
            field_id: Uuid::nil(),
            user_id: Uuid::nil(),
            value: None,
            owner_id: Uuid::nil(),
            created_by: None,
            deleted_at: None,
            created_at: default_time(),
            updated_at: default_time(),
        }
    }
}

impl Hidden for UserField {}

impl Model for UserField {
    const TABLE: &'static str = "claimius.user_field";
    const COLUMNS: &'static [Column] = &[
        ("id", "id"),
        ("field_id", "field_id"),
        ("user_id", "user_id"),
        ("value", "value"),
        ("sa_owner_id", "owner_id"),
        ("sa_created_by", "created_by"),
        ("sa_deleted_at", "deleted_at"),
        ("sa_created_at", "created_at"),
        ("sa_updated_at", "updated_at"),
    ];
    const FIELDS: &'static [&'static str] = fields![
        "id",
        "field_id",
        "user_id",
        "value",
        "owner_id",
        "created_by",
        "deleted_at",
        "created_at",
        "updated_at"
    ];

    fn id(&self) -> Uuid {
        self.id
    }

    fn body(&self) -> Option<&Body> {
        Some(&self.body)
    }
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct UserRelation {
    #[serde(skip)]
    pub body: Body,
    #[serde(default, skip_serializing_if = "is_nil")]
    pub id: Uuid,
    #[serde(default)]
    pub user_id: Uuid,
    #[serde(default)]
    pub description: Option<String>,
    #[serde(default)]
    pub object_type: ObjectType,
    #[serde(default)]
    pub object_id: Uuid,
    #[serde(default)]
    pub pinned: Option<i32>,
    #[serde(default)]
    pub priority: Option<i32>,
    #[serde(default)]
    pub parent_id: Option<Uuid>,
    #[serde(default)]
    pub sibling_id: Option<Uuid>,
    #[serde(default)]
    pub used_at: Option<DateTime<Utc>>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub deleted_at: Option<DateTime<Utc>>,
    #[serde(default = "default_time")]
    pub created_at: DateTime<Utc>,
    #[serde(default = "default_time")]
    pub updated_at: DateTime<Utc>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub access: Option<Box<AccessClaim>>,
}

impl Default for UserRelation {
    fn default() -> Self {
        UserRelation {
            body: Body::default(),
            id: Uuid::nil(),
            user_id: Uuid::nil(),
            description: None,
            object_type: ObjectType::default(),
            object_id: Uuid::nil(),
            pinned: None,
            priority: None,
            parent_id: None,
            sibling_id: None,
            used_at: None,
            deleted_at: None,
            created_at: default_time(),
            updated_at: default_time(),
            access: None,
        }
    }
}

impl Hidden for UserRelation {}

impl Model for UserRelation {
    const TABLE: &'static str = "claimius.user_relation";
    const COLUMNS: &'static [Column] = &[
        ("id", "id"),
        ("user_id", "user_id"),
        ("description", "description"),
        ("object_type", "object_type"),
        ("object_id", "object_id"),
        ("pinned", "pinned"),
        ("priority", "priority"),
        ("parent_id", "parent_id"),
        ("sibling_id", "sibling_id"),
        ("used_at", "used_at"),
        ("sa_deleted_at", "deleted_at"),
        ("sa_created_at", "created_at"),
        ("sa_updated_at", "updated_at"),
    ];
    const FIELDS: &'static [&'static str] = fields![
        "id",
        "user_id",
        "description",
        "object_type",
        "object_id",
        "pinned",
        "priority",
        "parent_id",
        "sibling_id",
        "used_at",
        "deleted_at",
        "created_at",
        "updated_at",
        "access"
    ];

    fn id(&self) -> Uuid {
        self.id
    }

    fn body(&self) -> Option<&Body> {
        Some(&self.body)
    }
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct ObjectAsset {
    #[serde(skip)]
    pub body: Body,
    #[serde(default, skip_serializing_if = "is_nil")]
    pub id: Uuid,
    #[serde(default)]
    pub object_type: ObjectType,
    #[serde(default)]
    pub object_id: Uuid,
    #[serde(default)]
    pub asset_id: Uuid,
    #[serde(default)]
    pub kind: String,
    #[serde(default)]
    pub name: Option<String>,
    #[serde(default)]
    pub description: Option<String>,
    #[serde(default)]
    pub mime_type: String,
    #[serde(default)]
    pub size: Option<i64>,
    #[serde(default)]
    pub index: i32,
    #[serde(default)]
    pub owner_id: Uuid,
    #[serde(default)]
    pub created_by: Option<Uuid>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub deleted_at: Option<DateTime<Utc>>,
    #[serde(default = "default_time")]
    pub created_at: DateTime<Utc>,
    #[serde(default = "default_time")]
    pub updated_at: DateTime<Utc>,
}

impl Default for ObjectAsset {
    fn default() -> Self {
        ObjectAsset {
            body: Body::default(),
            id: Uuid::nil(),
            object_type: ObjectType::default(),
            object_id: Uuid::nil(),
            asset_id: Uuid::nil(),
            kind: String::new(),
            name: None,
            description: None,
            mime_type: String::new(),
            size: None,
            index: 0,
            owner_id: Uuid::nil(),
            created_by: None,
            deleted_at: None,
            created_at: default_time(),
            updated_at: default_time(),
        }
    }
}

impl Hidden for ObjectAsset {}

impl Model for ObjectAsset {
    const TABLE: &'static str = "claimius.object_asset";
    const COLUMNS: &'static [Column] = &[
        ("id", "id"),
        ("object_type", "object_type"),
        ("object_id", "object_id"),
        ("asset_id", "asset_id"),
        ("kind", "kind"),
        ("name", "name"),
        ("description", "description"),
        ("mime_type", "mime_type"),
        ("size", "size"),
        ("index", "index"),
        ("sa_owner_id", "owner_id"),
        ("sa_created_by", "created_by"),
        ("sa_deleted_at", "deleted_at"),
        ("sa_created_at", "created_at"),
        ("sa_updated_at", "updated_at"),
    ];
    const FIELDS: &'static [&'static str] = fields![
        "id",
        "object_type",
        "object_id",
        "asset_id",
        "kind",
        "name",
        "description",
        "mime_type",
        "size",
        "index",
        "owner_id",
        "created_by",
        "deleted_at",
        "created_at",
        "updated_at"
    ];

    fn id(&self) -> Uuid {
        self.id
    }

    fn body(&self) -> Option<&Body> {
        Some(&self.body)
    }
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct AppCost {
    #[serde(skip)]
    pub body: Body,
    #[serde(default, skip_serializing_if = "is_nil")]
    pub id: Uuid,
    #[serde(default)]
    pub module: String,
    #[serde(default)]
    pub kind: String,
    #[serde(default = "default_time")]
    pub period_start: DateTime<Utc>,
    #[serde(default = "default_time")]
    pub period_end: DateTime<Utc>,
    #[serde(default)]
    pub quantity: f64,
    #[serde(default)]
    pub unit: String,
    #[serde(default)]
    pub unit_price: Option<f64>,
    #[serde(default)]
    pub amount: f64,
    #[serde(default)]
    pub currency: String,
    #[serde(default)]
    pub r#ref: String,
    #[serde(default)]
    pub supersedes_id: Option<Uuid>,
    #[serde(default)]
    pub status: String,
    #[serde(default)]
    pub schema: Value,
    #[serde(default)]
    pub detail: Value,
    #[serde(default)]
    pub owner_id: Uuid,
    #[serde(default)]
    pub created_by: Option<Uuid>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub deleted_at: Option<DateTime<Utc>>,
    #[serde(default = "default_time")]
    pub created_at: DateTime<Utc>,
    #[serde(default = "default_time")]
    pub updated_at: DateTime<Utc>,
}

impl Default for AppCost {
    fn default() -> Self {
        AppCost {
            body: Body::default(),
            id: Uuid::nil(),
            module: String::new(),
            kind: String::new(),
            period_start: default_time(),
            period_end: default_time(),
            quantity: 0.0,
            unit: String::new(),
            unit_price: None,
            amount: 0.0,
            currency: String::new(),
            r#ref: String::new(),
            supersedes_id: None,
            status: String::new(),
            schema: Value::Null,
            detail: Value::Null,
            owner_id: Uuid::nil(),
            created_by: None,
            deleted_at: None,
            created_at: default_time(),
            updated_at: default_time(),
        }
    }
}

impl Hidden for AppCost {}

impl Model for AppCost {
    const TABLE: &'static str = "claimius.app_cost";
    const COLUMNS: &'static [Column] = &[
        ("id", "id"),
        ("module", "module"),
        ("kind", "kind"),
        ("period_start", "period_start"),
        ("period_end", "period_end"),
        ("quantity", "quantity"),
        ("unit", "unit"),
        ("unit_price", "unit_price"),
        ("amount", "amount"),
        ("currency", "currency"),
        ("ref", "ref"),
        ("supersedes_id", "supersedes_id"),
        ("status", "status"),
        ("schema", "schema"),
        ("detail", "detail"),
        ("sa_owner_id", "owner_id"),
        ("sa_created_by", "created_by"),
        ("sa_deleted_at", "deleted_at"),
        ("sa_created_at", "created_at"),
        ("sa_updated_at", "updated_at"),
    ];
    const FIELDS: &'static [&'static str] = fields![
        "id",
        "module",
        "kind",
        "period_start",
        "period_end",
        "quantity",
        "unit",
        "unit_price",
        "amount",
        "currency",
        "ref",
        "supersedes_id",
        "status",
        "schema",
        "detail",
        "owner_id",
        "created_by",
        "deleted_at",
        "created_at",
        "updated_at"
    ];

    fn id(&self) -> Uuid {
        self.id
    }

    fn body(&self) -> Option<&Body> {
        Some(&self.body)
    }
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct AppCostItem {
    #[serde(default)]
    pub description: String,
    #[serde(default)]
    pub quantity: f64,
    #[serde(default)]
    pub unit: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub unit_price: Option<f64>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub amount: Option<f64>,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub r#ref: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub at: Option<DateTime<Utc>>,
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct AppCostInput {
    #[serde(skip)]
    pub body: Body,
    #[serde(default)]
    pub module: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub kind: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub period_start: Option<DateTime<Utc>>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub period_end: Option<DateTime<Utc>>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub at: Option<DateTime<Utc>>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub quantity: Option<f64>,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub unit: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub unit_price: Option<f64>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub amount: Option<f64>,
    #[serde(default)]
    pub currency: String,
    #[serde(default)]
    pub r#ref: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub status: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub supersedes_id: Option<Uuid>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub items: Vec<AppCostItem>,
    #[serde(default, skip_serializing_if = "Value::is_null")]
    pub schema: Value,
    #[serde(default, skip_serializing_if = "Value::is_null")]
    pub detail: Value,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub created_by: Option<Uuid>,
}

impl Hidden for AppCostInput {}

impl AppCostInput {
    pub const FIELDS: &'static [&'static str] = fields![
        "module",
        "kind",
        "period_start",
        "period_end",
        "at",
        "quantity",
        "unit",
        "unit_price",
        "amount",
        "currency",
        "ref",
        "status",
        "supersedes_id",
        "items",
        "schema",
        "detail",
        "created_by"
    ];
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Audit {
    #[serde(default, skip_serializing_if = "is_nil")]
    pub id: Uuid,
    #[serde(default)]
    pub operation: String,
    #[serde(default)]
    pub message: String,
    #[serde(default)]
    pub r#type: String,
    #[serde(default)]
    pub object_type: ObjectType,
    #[serde(default)]
    pub object_id: Uuid,
    #[serde(default)]
    pub owner_id: Uuid,
    #[serde(default)]
    pub created_by: Option<Uuid>,
    #[serde(default = "default_time")]
    pub created_at: DateTime<Utc>,
    #[serde(default = "default_time")]
    pub updated_at: DateTime<Utc>,
}

impl Default for Audit {
    fn default() -> Self {
        Audit {
            id: Uuid::nil(),
            operation: String::new(),
            message: String::new(),
            r#type: String::new(),
            object_type: ObjectType::default(),
            object_id: Uuid::nil(),
            owner_id: Uuid::nil(),
            created_by: None,
            created_at: default_time(),
            updated_at: default_time(),
        }
    }
}

impl Hidden for Audit {}

impl Model for Audit {
    const TABLE: &'static str = "claimius.audit";
    const COLUMNS: &'static [Column] = &[
        ("id", "id"),
        ("operation", "operation"),
        ("message", "message"),
        ("type", "type"),
        ("object_type", "object_type"),
        ("object_id", "object_id"),
        ("sa_owner_id", "owner_id"),
        ("sa_created_by", "created_by"),
        ("sa_created_at", "created_at"),
        ("sa_updated_at", "updated_at"),
    ];
    const FIELDS: &'static [&'static str] = fields![
        "id",
        "operation",
        "message",
        "type",
        "object_type",
        "object_id",
        "owner_id",
        "created_by",
        "created_at",
        "updated_at"
    ];

    fn id(&self) -> Uuid {
        self.id
    }
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct UserObject {
    #[serde(default)]
    pub user_id: Uuid,
    #[serde(default)]
    pub object_id: Uuid,
    #[serde(default)]
    pub object_type: ObjectType,
    #[serde(default)]
    pub access: ClaimAccess,
    #[serde(default)]
    pub scope: Option<Value>,
    #[serde(default)]
    pub grants: Value,
    #[serde(default)]
    pub direct_grant: bool,
    #[serde(default = "default_time")]
    pub created_at: DateTime<Utc>,
    #[serde(default = "default_time")]
    pub updated_at: DateTime<Utc>,
}

impl Default for UserObject {
    fn default() -> Self {
        UserObject {
            user_id: Uuid::nil(),
            object_id: Uuid::nil(),
            object_type: ObjectType::default(),
            access: ACCESS_NONE,
            scope: None,
            grants: Value::Array(Vec::new()),
            direct_grant: false,
            created_at: default_time(),
            updated_at: default_time(),
        }
    }
}

impl Hidden for UserObject {}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct UserUsers {
    #[serde(default)]
    pub id: Uuid,
    #[serde(default)]
    pub viewer_id: Uuid,
    #[serde(default)]
    pub target_user_id: Uuid,
    #[serde(default = "default_time")]
    pub first_shared_at: DateTime<Utc>,
    #[serde(default = "default_time")]
    pub last_shared_at: DateTime<Utc>,
    #[serde(default)]
    pub sharing_object_count: i32,
}

impl Default for UserUsers {
    fn default() -> Self {
        UserUsers {
            id: Uuid::nil(),
            viewer_id: Uuid::nil(),
            target_user_id: Uuid::nil(),
            first_shared_at: default_time(),
            last_shared_at: default_time(),
            sharing_object_count: 0,
        }
    }
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Notification {
    #[serde(default, skip_serializing_if = "is_nil")]
    pub id: Uuid,
    #[serde(default)]
    pub user_id: Uuid,
    #[serde(default)]
    pub title: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub description: Option<String>,
    #[serde(default)]
    pub style: Value,
    #[serde(default)]
    pub r#type: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub link: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub object_id: Option<Uuid>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub object_type: Option<ObjectType>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub created_by: Option<Uuid>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub owner_id: Option<Uuid>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub read_at: Option<DateTime<Utc>>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub closed_at: Option<DateTime<Utc>>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub repeats_at: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub deleted_at: Option<DateTime<Utc>>,
    #[serde(default = "default_time")]
    pub created_at: DateTime<Utc>,
    #[serde(default = "default_time")]
    pub updated_at: DateTime<Utc>,
}

impl Default for Notification {
    fn default() -> Self {
        Notification {
            id: Uuid::nil(),
            user_id: Uuid::nil(),
            title: String::new(),
            description: None,
            style: Value::Object(Map::new()),
            r#type: String::new(),
            link: None,
            object_id: None,
            object_type: None,
            created_by: None,
            owner_id: None,
            read_at: None,
            closed_at: None,
            repeats_at: None,
            deleted_at: None,
            created_at: default_time(),
            updated_at: default_time(),
        }
    }
}

impl Hidden for Notification {}

impl Model for Notification {
    const TABLE: &'static str = "claimius.notification";
    const COLUMNS: &'static [Column] = &[
        ("id", "id"),
        ("user_id", "user_id"),
        ("title", "title"),
        ("description", "description"),
        ("style", "style"),
        ("type", "type"),
        ("link", "link"),
        ("object_id", "object_id"),
        ("object_type", "object_type"),
        ("sa_created_by", "created_by"),
        ("sa_owner_id", "owner_id"),
        ("read_at", "read_at"),
        ("closed_at", "closed_at"),
        ("repeats_at", "repeats_at"),
        ("sa_deleted_at", "deleted_at"),
        ("sa_created_at", "created_at"),
        ("sa_updated_at", "updated_at"),
    ];
    const FIELDS: &'static [&'static str] = fields![
        "id",
        "user_id",
        "title",
        "description",
        "style",
        "type",
        "link",
        "object_id",
        "object_type",
        "created_by",
        "owner_id",
        "read_at",
        "closed_at",
        "repeats_at",
        "deleted_at",
        "created_at",
        "updated_at"
    ];

    fn id(&self) -> Uuid {
        self.id
    }
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Consent {
    #[serde(default)]
    pub id: Uuid,
    #[serde(default)]
    pub app_id: Uuid,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub user_id: Option<Uuid>,
    #[serde(default)]
    pub anonymous_id: String,
    #[serde(default)]
    pub categories: Value,
    #[serde(default)]
    pub policy_version: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub ip: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub user_agent: Option<String>,
    #[serde(default = "default_time")]
    pub granted_at: DateTime<Utc>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub revoked_at: Option<DateTime<Utc>>,
    #[serde(default = "default_time")]
    pub created_at: DateTime<Utc>,
    #[serde(default = "default_time")]
    pub updated_at: DateTime<Utc>,
}

impl Default for Consent {
    fn default() -> Self {
        Consent {
            id: Uuid::nil(),
            app_id: Uuid::nil(),
            user_id: None,
            anonymous_id: String::new(),
            categories: Value::Object(Map::new()),
            policy_version: String::new(),
            ip: None,
            user_agent: None,
            granted_at: default_time(),
            revoked_at: None,
            created_at: default_time(),
            updated_at: default_time(),
        }
    }
}

impl Hidden for Consent {}

impl Model for Consent {
    const TABLE: &'static str = "claimius.consent";
    const COLUMNS: &'static [Column] = &[
        ("id", "id"),
        ("app_id", "app_id"),
        ("user_id", "user_id"),
        ("anonymous_id", "anonymous_id"),
        ("categories", "categories"),
        ("policy_version", "policy_version"),
        ("ip", "ip"),
        ("user_agent", "user_agent"),
        ("granted_at", "granted_at"),
        ("revoked_at", "revoked_at"),
        ("sa_created_at", "created_at"),
        ("sa_updated_at", "updated_at"),
    ];
    const FIELDS: &'static [&'static str] = fields![
        "id",
        "app_id",
        "user_id",
        "anonymous_id",
        "categories",
        "policy_version",
        "ip",
        "user_agent",
        "granted_at",
        "revoked_at",
        "created_at",
        "updated_at"
    ];

    fn id(&self) -> Uuid {
        self.id
    }
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct AnalyticsSession {
    #[serde(default)]
    pub id: Uuid,
    #[serde(default)]
    pub app_id: Uuid,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub user_id: Option<Uuid>,
    #[serde(default)]
    pub anonymous_id: String,
    #[serde(default)]
    pub session_key: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub entry_path: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub referrer: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub user_agent: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub ip: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub language: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub timezone: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub platform: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub screen_width: Option<i32>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub screen_height: Option<i32>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub device_pixel_ratio: Option<f32>,
    #[serde(default)]
    pub context: Value,
    #[serde(default = "default_time")]
    pub started_at: DateTime<Utc>,
    #[serde(default = "default_time")]
    pub last_seen_at: DateTime<Utc>,
}

impl Default for AnalyticsSession {
    fn default() -> Self {
        AnalyticsSession {
            id: Uuid::nil(),
            app_id: Uuid::nil(),
            user_id: None,
            anonymous_id: String::new(),
            session_key: String::new(),
            entry_path: None,
            referrer: None,
            user_agent: None,
            ip: None,
            language: None,
            timezone: None,
            platform: None,
            screen_width: None,
            screen_height: None,
            device_pixel_ratio: None,
            context: Value::Object(Map::new()),
            started_at: default_time(),
            last_seen_at: default_time(),
        }
    }
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct AnalyticsEvent {
    #[serde(default)]
    pub id: Uuid,
    #[serde(default)]
    pub app_id: Uuid,
    #[serde(default)]
    pub session_id: Uuid,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub user_id: Option<Uuid>,
    #[serde(default)]
    pub r#type: String,
    #[serde(default)]
    pub path: String,
    #[serde(default)]
    pub payload: Value,
    #[serde(default = "default_time")]
    pub occurred_at: DateTime<Utc>,
    #[serde(default = "default_time")]
    pub received_at: DateTime<Utc>,
}

impl Default for AnalyticsEvent {
    fn default() -> Self {
        AnalyticsEvent {
            id: Uuid::nil(),
            app_id: Uuid::nil(),
            session_id: Uuid::nil(),
            user_id: None,
            r#type: String::new(),
            path: String::new(),
            payload: Value::Object(Map::new()),
            occurred_at: default_time(),
            received_at: default_time(),
        }
    }
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct ConsentCategories {
    #[serde(default)]
    pub necessary: bool,
    #[serde(default)]
    pub preferences: bool,
    #[serde(default)]
    pub analytics: bool,
    #[serde(default)]
    pub marketing: bool,
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct ConsentState {
    #[serde(default)]
    pub anonymous_id: String,
    #[serde(default)]
    pub policy_version: String,
    #[serde(default)]
    pub policy_url: String,
    #[serde(default)]
    pub decided: bool,
    #[serde(default)]
    pub categories: ConsentCategories,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub granted_at: Option<DateTime<Utc>>,
    #[serde(default)]
    pub captured: Vec<String>,
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct ConsentRequest {
    #[serde(default)]
    pub anonymous_id: String,
    #[serde(default)]
    pub policy_version: String,
    #[serde(default)]
    pub categories: ConsentCategories,
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct AnalyticsEventInput {
    #[serde(default)]
    pub r#type: String,
    #[serde(default)]
    pub path: String,
    #[serde(default)]
    pub payload: Value,
    #[serde(default)]
    pub occurred_at: i64,
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct AnalyticsBatch {
    #[serde(default)]
    pub anonymous_id: String,
    #[serde(default)]
    pub session_key: String,
    #[serde(default)]
    pub context: Value,
    #[serde(default)]
    pub events: Vec<AnalyticsEventInput>,
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct AnalyticsApp {
    #[serde(default)]
    pub id: Uuid,
    #[serde(default)]
    pub slug: String,
    #[serde(default)]
    pub name: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub app_image: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub origin: Option<String>,
    #[serde(default)]
    pub sessions: i64,
    #[serde(default)]
    pub events: i64,
    #[serde(default)]
    pub visitors: i64,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub last_seen: Option<DateTime<Utc>>,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct AnalyticsCapture {
    #[serde(default)]
    pub id: Uuid,
    #[serde(default)]
    pub app_id: Uuid,
    #[serde(default)]
    pub path: String,
    #[serde(default)]
    pub content_type: String,
    #[serde(default)]
    pub body: Value,
    #[serde(default = "default_time")]
    pub captured_at: DateTime<Utc>,
}

impl Default for AnalyticsCapture {
    fn default() -> Self {
        AnalyticsCapture {
            id: Uuid::nil(),
            app_id: Uuid::nil(),
            path: String::new(),
            content_type: String::new(),
            body: Value::Null,
            captured_at: default_time(),
        }
    }
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct AnalyticsActor {
    #[serde(default)]
    pub id: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub user_id: Option<String>,
    #[serde(default)]
    pub anonymous_id: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub ip: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub user_agent: Option<String>,
    #[serde(default)]
    pub sessions: i64,
    #[serde(default)]
    pub events: i64,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub last_seen: Option<DateTime<Utc>>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub path: Option<String>,
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct AnalyticsPath {
    #[serde(default)]
    pub path: String,
    #[serde(default)]
    pub events: i64,
    #[serde(default)]
    pub sessions: i64,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub viewport_width: Option<i32>,
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct AnalyticsSummary {
    #[serde(default)]
    pub actors: Vec<AnalyticsActor>,
    #[serde(default)]
    pub paths: Vec<AnalyticsPath>,
    #[serde(default)]
    pub types: Vec<String>,
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct AnalyticsHeatPoint {
    #[serde(default)]
    pub x: i32,
    #[serde(default)]
    pub y: i32,
    #[serde(default)]
    pub weight: i64,
    #[serde(default)]
    pub sessions: i64,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct SyncEvent {
    #[serde(default)]
    pub seq: i64,
    #[serde(default)]
    pub operation: String,
    #[serde(default)]
    pub event_type: String,
    #[serde(default)]
    pub payload: Value,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub app_id: Option<Uuid>,
    #[serde(default = "default_time")]
    pub written_at: DateTime<Utc>,
}

impl Default for SyncEvent {
    fn default() -> Self {
        SyncEvent {
            seq: 0,
            operation: String::new(),
            event_type: String::new(),
            payload: Value::Null,
            app_id: None,
            written_at: default_time(),
        }
    }
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct SyncConsumer {
    #[serde(default)]
    pub consumer_id: Uuid,
    #[serde(default)]
    pub peer: bool,
    #[serde(default)]
    pub txid: i64,
    #[serde(default)]
    pub seq: i64,
    #[serde(default = "default_time")]
    pub last_contact_at: DateTime<Utc>,
    #[serde(default)]
    pub in_contact: bool,
    #[serde(default)]
    pub head_txid: i64,
    #[serde(default)]
    pub head_seq: i64,
    #[serde(default)]
    pub trimmed_txid: i64,
    #[serde(default)]
    pub trimmed_seq: i64,
    #[serde(default)]
    pub behind: i64,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub next_txid: Option<i64>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub next_seq: Option<i64>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub next_operation: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub next_event_type: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub next_payload_id: Option<String>,
}

impl Default for SyncConsumer {
    fn default() -> Self {
        SyncConsumer {
            consumer_id: Uuid::nil(),
            peer: false,
            txid: 0,
            seq: 0,
            last_contact_at: default_time(),
            in_contact: false,
            head_txid: 0,
            head_seq: 0,
            trimmed_txid: 0,
            trimmed_seq: 0,
            behind: 0,
            next_txid: None,
            next_seq: None,
            next_operation: None,
            next_event_type: None,
            next_payload_id: None,
        }
    }
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct DiscipleState {
    #[serde(default)]
    pub id: Uuid,
    #[serde(default)]
    pub last_applied_txid: i64,
    #[serde(default)]
    pub last_applied_seq: i64,
    #[serde(default)]
    pub prophet: Option<String>,
    #[serde(default)]
    pub endpoint: Option<String>,
    #[serde(default = "default_time")]
    pub updated_at: DateTime<Utc>,
}

impl Default for DiscipleState {
    fn default() -> Self {
        DiscipleState {
            id: Uuid::nil(),
            last_applied_txid: 0,
            last_applied_seq: 0,
            prophet: None,
            endpoint: None,
            updated_at: default_time(),
        }
    }
}

impl Hidden for DiscipleState {}

impl Model for DiscipleState {
    const TABLE: &'static str = "claimius.disciple_state";
    const COLUMNS: &'static [Column] = &[
        ("id", "id"),
        ("last_applied_txid", "last_applied_txid"),
        ("last_applied_seq", "last_applied_seq"),
        ("prophet", "prophet"),
        ("endpoint", "endpoint"),
        ("sa_updated_at", "updated_at"),
    ];
    const FIELDS: &'static [&'static str] = fields![
        "id",
        "last_applied_txid",
        "last_applied_seq",
        "prophet",
        "endpoint",
        "updated_at"
    ];

    fn id(&self) -> Uuid {
        self.id
    }
}

impl DiscipleState {
    pub fn cursor(&self) -> SyncCursor {
        SyncCursor {
            txid: self.last_applied_txid,
            seq: self.last_applied_seq,
        }
    }
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct ProphetPeer {
    #[serde(default)]
    pub id: Uuid,
    #[serde(default)]
    pub prophet: String,
    #[serde(default)]
    pub endpoint: String,
    #[serde(default)]
    pub app_slug: String,
    #[serde(skip)]
    pub app_secret: String,
    #[serde(skip)]
    pub peer_secret: String,
    #[serde(default)]
    pub status: String,
    #[serde(default)]
    pub last_applied_txid: i64,
    #[serde(default)]
    pub last_applied_seq: i64,
    #[serde(default)]
    pub created_by: Uuid,
    #[serde(default = "default_time")]
    pub created_at: DateTime<Utc>,
    #[serde(default = "default_time")]
    pub updated_at: DateTime<Utc>,
}

impl Default for ProphetPeer {
    fn default() -> Self {
        ProphetPeer {
            id: Uuid::nil(),
            prophet: String::new(),
            endpoint: String::new(),
            app_slug: String::new(),
            app_secret: String::new(),
            peer_secret: String::new(),
            status: String::new(),
            last_applied_txid: 0,
            last_applied_seq: 0,
            created_by: Uuid::nil(),
            created_at: default_time(),
            updated_at: default_time(),
        }
    }
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct SearchResult {
    #[serde(default)]
    pub object_id: Uuid,
    #[serde(default)]
    pub object_type: ObjectType,
    #[serde(default)]
    pub user_claim_id: Option<Uuid>,
    #[serde(default)]
    pub name: Option<String>,
    #[serde(default)]
    pub description: Option<String>,
    #[serde(default)]
    pub link: Option<String>,
    #[serde(default)]
    pub access: ClaimAccess,
    #[serde(default)]
    pub scope: Option<Value>,
    #[serde(default)]
    pub owner_id: Option<Uuid>,
    #[serde(default)]
    pub location_id: Option<Uuid>,
    #[serde(default)]
    pub direct_grant: bool,
    #[serde(default)]
    pub rank: f32,
}

impl Hidden for SearchResult {}

impl Model for SearchResult {
    const TABLE: &'static str = "search";
    const COLUMNS: &'static [Column] = &[
        ("object_id", "object_id"),
        ("object_type", "object_type"),
        ("user_claim_id", "user_claim_id"),
        ("name", "name"),
        ("description", "description"),
        ("link", "link"),
        ("sa_access", "access"),
        ("scope", "scope"),
        ("owner_id", "owner_id"),
        ("location_id", "location_id"),
        ("is_direct_grant", "direct_grant"),
        ("rank", "rank"),
    ];
    const FIELDS: &'static [&'static str] = fields![
        "object_id",
        "object_type",
        "user_claim_id",
        "name",
        "description",
        "link",
        "access",
        "scope",
        "owner_id",
        "location_id",
        "direct_grant",
        "rank"
    ];

    fn id(&self) -> Uuid {
        self.object_id
    }
}

impl Claim {
    pub fn format_access(&mut self, f: AccessFormat) {
        let (slice, pipe) = f.output(self.access);
        self.access_slice = slice;
        self.access_string = pipe;
        for co in &mut self.claim_objects {
            co.format_access(f);
        }
        let access = self.access;
        for uc in &mut self.claim_users {
            uc.format_access(f, access);
        }
    }
}

impl ClaimObject {
    pub fn format_access(&mut self, f: AccessFormat) {
        let access = self.access.unwrap_or(ACCESS_NONE);
        let (slice, pipe) = f.output(access);
        self.access_slice = slice;
        self.access_string = pipe;
    }
}

impl UserClaim {
    pub fn format_access(&mut self, f: AccessFormat, access: ClaimAccess) {
        let (slice, pipe) = f.output(access);
        self.access_slice = slice;
        self.access_string = pipe;
    }
}

pub fn nil_opt(id: &Option<Uuid>) -> bool {
    is_nil_opt(id)
}
