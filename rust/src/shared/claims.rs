use std::collections::HashMap;
use std::sync::{LazyLock, Mutex};

use chrono::{DateTime, Utc};
use serde::Deserialize;
use serde_json::Value;
use uuid::Uuid;

use super::code::{
    ClaimOptions, ClaimOptionsSamk, code_error, seal_claim_options, show_claim_options,
};
use super::db::{Conn, Db};
use super::error::{Error, Result};
use super::models::*;
use super::query::{claim_where, object_where, params_ref};
use crate::client::Ctx;

#[derive(Clone, Debug)]
pub struct PublicBinding {
    pub object_type: String,
    pub object_id: Uuid,
    pub inherits: bool,
}

#[derive(Clone, Debug)]
pub struct PublicClaim {
    pub claim_id: Uuid,
    pub samk: Option<ClaimOptionsSamk>,
    pub starts_at: Option<DateTime<Utc>>,
    pub ends_at: Option<DateTime<Utc>>,
    pub bindings: Vec<PublicBinding>,
}

impl PublicClaim {
    pub fn accepts(&self, hash: &str, query: bool, header: bool, cookie: bool) -> bool {
        let Some(samk) = &self.samk else { return false };
        if samk.hash != hash {
            return false;
        }
        (query && samk.query) || (header && samk.header) || (cookie && samk.cookie)
    }
}

pub type PublicClaims = Vec<PublicClaim>;

pub trait PublicClaimsFind {
    fn find(&self, hash: &str) -> Option<&PublicClaim>;
}

impl PublicClaimsFind for PublicClaims {
    fn find(&self, hash: &str) -> Option<&PublicClaim> {
        self.iter()
            .find(|c| c.samk.as_ref().is_some_and(|s| s.hash == hash))
    }
}

static PUBLIC_CLAIM_CACHE: LazyLock<Mutex<HashMap<Uuid, PublicClaims>>> =
    LazyLock::new(|| Mutex::new(HashMap::new()));

pub fn delete_public_claim_cache() {
    PUBLIC_CLAIM_CACHE.lock().expect("public claims").clear();
}

#[derive(Deserialize)]
struct PublicClaimRow {
    claim_id: Uuid,
    #[serde(default)]
    options: Value,
    #[serde(default)]
    starts_at: Option<DateTime<Utc>>,
    #[serde(default)]
    ends_at: Option<DateTime<Utc>>,
    #[serde(default)]
    object_type: Option<String>,
    #[serde(default)]
    object_id: Option<Uuid>,
    #[serde(default)]
    inherits: Option<bool>,
}

#[derive(Clone, Debug, Default)]
pub struct GraphWindow {
    pub width: i32,
    pub sort: String,
    pub next_id: Option<Uuid>,
}

pub const GRAPH_SORT_NAME: &str = "name";
pub const GRAPH_SORT_USAGE: &str = "usage";

impl GraphWindow {
    fn sort(&self) -> &str {
        if self.sort.is_empty() {
            GRAPH_SORT_NAME
        } else {
            &self.sort
        }
    }
}

#[derive(Clone, Debug, Default)]
pub struct CreateVirtualClaimParams {
    pub name: String,
    pub description: Option<String>,
    pub access: ClaimAccess,
    pub owner_id: Uuid,
    pub object_id: Uuid,
    pub object_type: String,
    pub target_user_id: Uuid,
}

#[derive(Clone, Debug, Default)]
pub struct GrantFilters {
    pub ids: Vec<Uuid>,
    pub user_ids: Vec<Uuid>,
    pub claim_ids: Vec<Uuid>,
    pub owner_ids: Vec<Uuid>,
    pub no_clients: bool,
}

#[derive(Clone, Debug, Default)]
pub struct BindingFilters {
    pub object_type: String,
    pub object_ids: Vec<Uuid>,
    pub claim_ids: Vec<Uuid>,
}

#[derive(Clone, Debug, Default, serde::Serialize, Deserialize)]
pub struct Descendant {
    #[serde(default)]
    pub object_type: ObjectType,
    #[serde(default)]
    pub object_id: Uuid,
    #[serde(default)]
    pub hop: i32,
    #[serde(default)]
    pub tree_type: String,
}

pub fn nullable_json(raw: &Value) -> Option<String> {
    if raw.is_null() {
        None
    } else {
        Some(raw.to_string())
    }
}

impl Db {
    pub async fn get_public_claims(&self, ctx: &Ctx, app_id: Uuid) -> Result<PublicClaims> {
        if let Some(cached) = PUBLIC_CLAIM_CACHE
            .lock()
            .expect("public claims")
            .get(&app_id)
        {
            return Ok(cached.clone());
        }
        let rows: Vec<PublicClaimRow> = match self
            .query_values::<PublicClaimRow>(ctx, "SELECT * FROM claimius.get_public_claims()", &[])
            .await
        {
            Ok(rows) => rows,
            Err(e) if e.is_no_rows() => Vec::new(),
            Err(e) => return Err(e),
        };
        let mut claims = PublicClaims::new();
        for row in rows {
            if claims.last().is_none_or(|c| c.claim_id != row.claim_id) {
                let options: ClaimOptions = if row.options.is_null() {
                    ClaimOptions::default()
                } else {
                    serde_json::from_value(row.options.clone())?
                };
                claims.push(PublicClaim {
                    claim_id: row.claim_id,
                    samk: options.samk,
                    starts_at: row.starts_at,
                    ends_at: row.ends_at,
                    bindings: Vec::new(),
                });
            }
            if let (Some(object_type), Some(object_id)) = (row.object_type, row.object_id)
                && let Some(current) = claims.last_mut()
            {
                current.bindings.push(PublicBinding {
                    object_type,
                    object_id,
                    inherits: row.inherits.unwrap_or(false),
                });
            }
        }
        PUBLIC_CLAIM_CACHE
            .lock()
            .expect("public claims")
            .insert(app_id, claims.clone());
        Ok(claims)
    }

    pub async fn get_claim(
        &self,
        ctx: &Ctx,
        user_id: Uuid,
        claim_id: Uuid,
        required_access: ClaimAccess,
    ) -> Result<Claim> {
        let sql = "SELECT claim.* FROM claimius.claim WHERE claim.sa_deleted_at IS NULL AND claim.id = $1 AND EXISTS (SELECT 1 FROM claimius.get_access($2, claim.id, $3, $4)) LIMIT 1";
        self.query_model::<Claim>(
            ctx,
            sql,
            &[&claim_id, &user_id, &OBJECT_TYPE_CLAIM, &required_access.0],
        )
        .await
    }

    pub async fn get_claims(
        &self,
        ctx: &Ctx,
        user_id: Uuid,
        required_access: ClaimAccess,
    ) -> Result<Vec<Claim>> {
        let sql = format!(
            "SELECT claim.* FROM claimius.claim WHERE claim.sa_deleted_at IS NULL AND {}",
            claim_where(user_id, required_access)
        );
        self.query_models::<Claim>(ctx, &sql, &[]).await
    }

    pub async fn get_claim_audit(
        &self,
        ctx: &Ctx,
        actor_id: Uuid,
        claim_id: Uuid,
        required_access: ClaimAccess,
    ) -> Result<Vec<Audit>> {
        self.query_models::<Audit>(
            ctx,
            "SELECT audit.* FROM claimius.get_audit($1, $2) AS audit WHERE audit.object_type = $3 AND audit.object_id = $4 ORDER BY audit.sa_created_at DESC",
            &[&actor_id, &required_access.0, &OBJECT_TYPE_CLAIM, &claim_id],
        )
        .await
    }

    pub async fn search(
        &self,
        ctx: &Ctx,
        user_id: Uuid,
        query: &str,
        object_types: &[String],
        required_access: ClaimAccess,
        limit: i32,
        offset: i32,
    ) -> Result<Vec<SearchResult>> {
        self.query_models::<SearchResult>(
            ctx,
            "SELECT * FROM claimius.search($1, $2, $3::text[], $4, $5, $6)",
            &[
                &user_id,
                &query,
                &object_types,
                &required_access.0,
                &limit,
                &offset,
            ],
        )
        .await
    }

    pub async fn get_audit(
        &self,
        ctx: &Ctx,
        user_id: Uuid,
        required_access: ClaimAccess,
        ids: &[Uuid],
    ) -> Result<Vec<Audit>> {
        self.query_models::<Audit>(
            ctx,
            "SELECT audit.* FROM claimius.get_audit($1, $2, p_ids => $3::uuid[]) AS audit",
            &[&user_id, &required_access.0, &ids],
        )
        .await
    }

    pub async fn get_object_claims(
        &self,
        ctx: &Ctx,
        user_id: Uuid,
        object_id: Uuid,
        object_type: &str,
    ) -> Result<Vec<Claim>> {
        self.query_models::<Claim>(ctx, "SELECT claim.* FROM claimius.claim WHERE sa_deleted_at IS NULL AND claim.id IN (SELECT id FROM claimius.get_claims($1, $2, $3))", &[&user_id, &object_id, &object_type]).await
    }

    pub async fn get_owner_graph(
        &self,
        ctx: &Ctx,
        user_id: Uuid,
        start_id: Option<Uuid>,
        depth: i32,
        required_access: ClaimAccess,
        window: &GraphWindow,
    ) -> Result<Vec<GraphNode>> {
        self.query_models::<GraphNode>(
            ctx,
            "SELECT graph.* FROM claimius.get_owner_graph(p_user_id := $1, p_start_id := $2, p_depth := $3, p_required_access := $4, p_width := $5, p_sort := $6, p_next_id := $7) AS graph",
            &[&user_id, &start_id, &depth, &required_access.0, &window.width, &window.sort(), &window.next_id],
        )
        .await
    }

    pub async fn get_claim_graph(
        &self,
        ctx: &Ctx,
        user_id: Uuid,
        start_id: Option<Uuid>,
        depth: i32,
        required_access: ClaimAccess,
        window: &GraphWindow,
    ) -> Result<Vec<GraphNode>> {
        self.query_models::<GraphNode>(
            ctx,
            "SELECT graph.* FROM claimius.get_claim_graph(p_user_id := $1, p_start_id := $2, p_depth := $3, p_required_access := $4, p_width := $5, p_sort := $6, p_next_id := $7) AS graph",
            &[&user_id, &start_id, &depth, &required_access.0, &window.width, &window.sort(), &window.next_id],
        )
        .await
    }

    pub(crate) async fn insert_claim(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        claim: &mut Claim,
    ) -> Result<Claim> {
        let Some(ac) = ac else {
            return Err(Error::no_access());
        };
        let _clear = ClearPublic;
        claim.created_by = ac.user_claim_id;
        claim.root_id = ac.root_id;
        let held = self.conn_or(ctx, conn).await?;
        let payload: Value = held
            .query_scalar::<Value>(
                "SELECT (claimius.insert_claim($1, $2, $3, $4, $5, $6, $7, $8::text::claimius.claim_type, $9) -> 'claim')::jsonb",
                &[&claim.name, &claim.description, &claim.access.0, &claim.owner_id, &claim.root_id, &claim.created_by, &claim.inherits, &claim.r#type, &claim.level],
            )
            .await?;
        let body = claim.body.clone();
        *claim = crate::sauth::unmarshal_value(payload).map_err(Error::Other)?;
        claim.body = body;
        Ok(claim.clone())
    }

    pub(crate) async fn update_claim(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        claim_id: Uuid,
        claim: &mut Claim,
    ) -> Result<Claim> {
        let Some(ac) = ac else {
            return Err(Error::no_access());
        };
        let _clear = ClearPublic;
        let name: Option<String> = if claim.body.has_key("name") {
            Some(claim.name.clone())
        } else {
            None
        };
        let description: Option<String> = if claim.body.has_key("description") {
            claim.description.clone()
        } else {
            None
        };
        let access: Option<i32> = if claim.body.has_key("access") {
            Some(claim.access.0)
        } else {
            None
        };
        let inherits: Option<bool> = if claim.body.has_key("inherits") {
            Some(claim.inherits)
        } else {
            None
        };
        let level: Option<i32> = if claim.body.has_key("level") {
            Some(claim.level)
        } else {
            None
        };
        let claim_type: Option<String> = if claim.body.has_key("type") {
            Some(claim.r#type.clone())
        } else {
            None
        };
        let held = self.conn_or(ctx, conn).await?;
        let payload: Value = held
            .query_scalar::<Value>("SELECT (claimius.update_claim($1, $2, $3, $4, $5, $6, $7::text::claimius.claim_type, $8::uuid) -> 'claim')::jsonb", &[&claim_id, &name, &description, &access, &inherits, &level, &claim_type, &ac.user_claim_id])
            .await?;
        let body = claim.body.clone();
        *claim = crate::sauth::unmarshal_value(payload).map_err(Error::Other)?;
        claim.body = body;
        Ok(claim.clone())
    }

    pub(crate) async fn delete_claim(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        claim_id: Uuid,
    ) -> Result<Claim> {
        let Some(uc) = ac.and_then(|a| a.user_claim_id) else {
            return Err(Error::no_access());
        };
        let _clear = ClearPublic;
        let held = self.conn_or(ctx, conn).await?;
        held.query_model::<Claim>(
            "SELECT * FROM claimius.delete_claim($1, $2) AS claim",
            &[&claim_id, &uc],
        )
        .await
    }

    pub(crate) async fn insert_claim_object(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        claim_id: Uuid,
        co: &mut ClaimObject,
    ) -> Result<ClaimObject> {
        let Some(ac) = ac else {
            return Err(Error::no_access());
        };
        let _clear = ClearPublic;
        if co.owner_id.is_nil() {
            co.owner_id = ac.owner_id;
        }
        if co.root_id.is_nil() {
            co.root_id = ac.root_id;
        }
        co.created_by = ac.user_claim_id;
        let held = self.conn_or(ctx, conn).await?;
        let object_type = co.object_type.as_ref().map(|t| t.as_str().to_string());
        let access = co.access.map(|a| a.0);
        let payload: Value = held
            .query_scalar::<Value>(
                "SELECT (claimius.insert_claim_object(
				p_claim_id      := $1,
				p_sa_owner_id   := $2,
				p_sa_root_id    := $3,
				p_sa_created_by := $4,
				p_object_id     := $5,
				p_object_type   := $6,
				p_sa_access     := $7,
				p_inherits      := $8,
				p_reason        := $9,
				p_ref_id        := $10,
				p_scope         := $11
			) -> 'claim_object')::jsonb",
                &[
                    &claim_id,
                    &co.owner_id,
                    &co.root_id,
                    &co.created_by,
                    &co.object_id,
                    &object_type,
                    &access,
                    &co.inherits,
                    &co.reason,
                    &co.ref_id,
                    &co.scope,
                ],
            )
            .await?;
        let body = co.body.clone();
        *co = crate::sauth::unmarshal_value(payload).map_err(Error::Other)?;
        co.body = body;
        Ok(co.clone())
    }

    pub(crate) async fn delete_claim_object(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        claim_id: Uuid,
        binding_id: Uuid,
    ) -> Result<ClaimObject> {
        let Some(uc) = ac.and_then(|a| a.user_claim_id) else {
            return Err(Error::no_access());
        };
        let _clear = ClearPublic;
        let held = self.conn_or(ctx, conn).await?;
        let row_id: Uuid = match held.query_scalar_opt::<Uuid>("SELECT id FROM claimius.claim_object WHERE claim_id = $1 AND id = $2 AND sa_deleted_at IS NULL", &[&claim_id, &binding_id]).await? {
            Some(id) => id,
            None => return Err(Error::not_found()),
        };
        held.query_model::<ClaimObject>(
            "SELECT * FROM claimius.delete_claim_object($1, $2) AS claim_object",
            &[&row_id, &uc],
        )
        .await
    }

    pub(crate) async fn insert_claim_user(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        claim_id: Uuid,
        uc: &mut UserClaim,
    ) -> Result<UserClaim> {
        let Some(ac) = ac else {
            return Err(Error::no_access());
        };
        let Some(user_claim_id) = ac.user_claim_id else {
            return Err(Error::no_access());
        };
        let _clear = ClearPublic;
        uc.owner_id = ac.owner_id;
        uc.created_by = user_claim_id;
        let (options, value) = seal_claim_options(&uc.options)?;
        let held = self.conn_or(ctx, conn).await?;
        let options_text = nullable_json(&options);
        let payload: Value = held
            .query_scalar::<Value>("SELECT (claimius.insert_user_claim($1, $2, $3, $4, $5, $6, $7, $8::jsonb) -> 'user_claim')::jsonb", &[&claim_id, &uc.user_id, &uc.owner_id, &uc.created_by, &uc.reason, &uc.starts_at, &uc.ends_at, &options_text])
            .await
            .map_err(code_error)?;
        let body = uc.body.clone();
        *uc = crate::sauth::unmarshal_value(payload).map_err(Error::Other)?;
        uc.body = body;
        uc.options = show_claim_options(&uc.options, &value);
        Ok(uc.clone())
    }

    pub(crate) async fn delete_claim_user(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        claim_id: Uuid,
        grant_id: Uuid,
    ) -> Result<UserClaim> {
        let Some(user_claim_id) = ac.and_then(|a| a.user_claim_id) else {
            return Err(Error::no_access());
        };
        let _clear = ClearPublic;
        let held = self.conn_or(ctx, conn).await?;
        let row_id: Uuid = match held.query_scalar_opt::<Uuid>("SELECT id FROM claimius.user_claim WHERE claim_id = $1 AND id = $2 AND sa_deleted_at IS NULL", &[&claim_id, &grant_id]).await? {
            Some(id) => id,
            None => return Err(Error::not_found()),
        };
        held.query_model::<UserClaim>(
            "SELECT * FROM claimius.delete_user_claim($1, $2) AS user_claim",
            &[&row_id, &user_claim_id],
        )
        .await
    }

    pub(crate) async fn update_claim_user(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        grant_id: Uuid,
        uc: &mut UserClaim,
        columns: &[String],
    ) -> Result<UserClaim> {
        let Some(ac) = ac else {
            return Err(Error::no_access());
        };
        if ac.user_claim_id.is_none() {
            return Err(Error::no_access());
        }
        let _clear = ClearPublic;
        let sent = |key: &str| uc.body.has_key(key) || columns.iter().any(|c| c == key);
        let reason: Option<String> = if sent("reason") {
            uc.reason.clone()
        } else {
            None
        };
        let starts_at = if sent("starts_at") {
            uc.starts_at
        } else {
            None
        };
        let ends_at = if sent("ends_at") { uc.ends_at } else { None };
        let mut options: Option<String> = None;
        let mut value = String::new();
        if sent("options") {
            let (sealed, plain) = seal_claim_options(&uc.options)?;
            value = plain;
            options = Some(if sealed.is_null() {
                "{}".to_string()
            } else {
                sealed.to_string()
            });
        }
        let held = self.conn_or(ctx, conn).await?;
        let payload: Value = held
            .query_scalar::<Value>("SELECT (claimius.update_user_claim($1, $2, $3, $4, $5::jsonb) -> 'user_claim')::jsonb", &[&grant_id, &reason, &starts_at, &ends_at, &options])
            .await
            .map_err(code_error)?;
        let body = uc.body.clone();
        *uc = crate::sauth::unmarshal_value(payload).map_err(Error::Other)?;
        uc.body = body;
        uc.options = show_claim_options(&uc.options, &value);
        Ok(uc.clone())
    }

    pub async fn create_virtual_claim(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        p: CreateVirtualClaimParams,
    ) -> Result<AccessClaim> {
        let Some(ac) = ac else {
            return Err(Error::no_access());
        };
        if ac.user_claim_id.is_none() {
            return Err(Error::no_access());
        }
        let mut owner = ac.clone();
        owner.owner_id = p.owner_id;
        let mut claim = self
            .create_claim(
                ctx,
                conn,
                &owner,
                CreateClaimParams {
                    name: p.name,
                    description: p.description,
                    access: p.access,
                    inherits: None,
                    r#type: CLAIM_TYPE_VIRTUAL.to_string(),
                    level: 0,
                },
            )
            .await?;
        let object_type = ObjectType::new(&p.object_type);
        let binding = ClaimObject {
            object_id: Some(p.object_id),
            object_type: Some(object_type),
            inherits: true,
            owner_id: claim.owner_id,
            root_id: claim.root_id,
            ..ClaimObject::default()
        };
        self.assign_object(ctx, conn, &owner, &mut claim, binding)
            .await?;
        let granted = self
            .assign_user(ctx, conn, &owner, &claim, &[p.target_user_id])
            .await?;
        let Some(first) = granted.first() else {
            return Err(Error::no_access());
        };
        Ok(AccessClaim {
            user_claim_id: Some(first.id),
            claim_id: Some(claim.id),
            claim_name: Some(claim.name.clone()),
            access: claim.access,
            owner_id: claim.owner_id,
            root_id: claim.root_id,
            ..AccessClaim::default()
        })
    }

    pub async fn get_claims_many(
        &self,
        ctx: &Ctx,
        ids: &[Uuid],
        owner_ids: &[Uuid],
    ) -> Result<Vec<Claim>> {
        self.query_models::<Claim>(
            ctx,
            "SELECT * FROM claimius.get_claims($1::uuid[], $2::uuid[])",
            &[&ids, &owner_ids],
        )
        .await
    }

    pub async fn get_grants(&self, ctx: &Ctx, f: &GrantFilters) -> Result<Vec<UserClaim>> {
        let with_clients = !f.no_clients;
        self.query_models::<UserClaim>(
            ctx,
            "SELECT * FROM claimius.get_grants($1::uuid[], $2::uuid[], $3::uuid[], $4::uuid[], $5)",
            &[
                &f.ids,
                &f.user_ids,
                &f.claim_ids,
                &f.owner_ids,
                &with_clients,
            ],
        )
        .await
    }

    pub async fn get_bindings(&self, ctx: &Ctx, f: &BindingFilters) -> Result<Vec<ClaimObject>> {
        let object_type: Option<String> = if f.object_type.is_empty() {
            None
        } else {
            Some(f.object_type.clone())
        };
        self.query_models::<ClaimObject>(
            ctx,
            "SELECT * FROM claimius.get_bindings($1::text, $2::uuid[], $3::uuid[])",
            &[&object_type, &f.object_ids, &f.claim_ids],
        )
        .await
    }

    pub async fn get_object_types(&self, ctx: &Ctx) -> Result<Vec<ObjectType>> {
        let types: Vec<String> = self
            .query_scalars::<String>(
                ctx,
                "SELECT object_type FROM claimius.get_object_types()",
                &[],
            )
            .await?;
        Ok(types.into_iter().map(ObjectType).collect())
    }

    pub async fn get_descendants(&self, ctx: &Ctx, object_id: Uuid) -> Result<Vec<Descendant>> {
        self.query_values::<Descendant>(
            ctx,
            "SELECT * FROM claimius.get_descendants($1::uuid)",
            &[&object_id],
        )
        .await
    }

    pub async fn get_claim_objects_of(
        &self,
        ctx: &Ctx,
        claim_ids: &[Uuid],
    ) -> Result<Vec<ClaimObject>> {
        self.query_models::<ClaimObject>(ctx, "SELECT * FROM claimius.claim_object WHERE claim_id = ANY($1::uuid[]) AND sa_deleted_at IS NULL", &[&claim_ids]).await
    }

    pub async fn get_claim_users_of(
        &self,
        ctx: &Ctx,
        claim_ids: &[Uuid],
    ) -> Result<Vec<UserClaim>> {
        self.query_models::<UserClaim>(ctx, "SELECT * FROM claimius.user_claim WHERE claim_id = ANY($1::uuid[]) AND sa_deleted_at IS NULL", &[&claim_ids]).await
    }

    pub async fn get_organizations_of(&self, ctx: &Ctx, ids: &[Uuid]) -> Result<Vec<Organization>> {
        self.query_models::<Organization>(ctx, "SELECT * FROM claimius.organization WHERE id = ANY($1::uuid[]) AND sa_deleted_at IS NULL", &[&ids]).await
    }

    pub async fn list_objects<T: Model>(
        &self,
        ctx: &Ctx,
        table: &str,
        user_id: Uuid,
        object_type: &str,
        required_access: ClaimAccess,
        after: Option<Uuid>,
        limit: i32,
    ) -> Result<Vec<T>> {
        let alias = "t";
        let w = object_where(
            alias,
            user_id,
            object_type,
            required_access,
            None,
            after,
            limit,
            1,
        );
        let mut sql = format!(
            "SELECT {alias}.* FROM {} AS {alias} WHERE {alias}.sa_deleted_at IS NULL AND {}",
            super::query::quote_table(table),
            w.where_sql
        );
        if !w.order_sql.is_empty() {
            sql.push_str(&format!(" ORDER BY {}", w.order_sql));
        } else {
            sql.push_str(&format!(" ORDER BY {alias}.id"));
        }
        let params = params_ref(&w.params);
        self.query_models::<T>(ctx, &sql, &params).await
    }
}

struct ClearPublic;

impl Drop for ClearPublic {
    fn drop(&mut self) {
        delete_public_claim_cache();
    }
}
