use chrono::Utc;
use serde::{Deserialize, Serialize};
use serde_json::{Map, Value};
use uuid::Uuid;

use super::db::{Conn, Db};
use super::error::{Error, Result};
use super::models::*;
use super::organization::patch_nulls;
use super::query::{EndpointSpec, Write, stamp_access_claim, user_where};
use crate::client::{self, Ctx};

#[derive(Clone, Debug, Default)]
pub struct UserRelationFilters {
    pub org_ids: Vec<Uuid>,
    pub loc_ids: Vec<Uuid>,
    pub usr_ids: Vec<Uuid>,
    pub object_types: Vec<ObjectType>,
    pub object_ids: Vec<Uuid>,
    pub pinned: Option<i32>,
    pub priority: Option<i32>,
    pub parent_id: Option<Uuid>,
    pub sibling_id: Option<Uuid>,
    pub description: Option<String>,
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct OrganizationUserResult {
    #[serde(default)]
    pub samna_user: Option<SamnaUser>,
    #[serde(default)]
    pub created: bool,
    #[serde(default)]
    pub user_claim: Option<UserClaim>,
    #[serde(default)]
    pub virtual_claim: Option<Claim>,
    #[serde(default)]
    pub claim_object: Option<ClaimObject>,
    #[serde(default)]
    pub caller_user_claim: Option<UserClaim>,
    #[serde(default)]
    pub user_identity: Option<UserIdentity>,
}

impl Db {
    pub async fn get_user(&self, ctx: &Ctx, actor_id: Uuid, user_id: Uuid) -> Result<SamnaUser> {
        let sql = format!(
            "SELECT samna_user.* FROM claimius.get_users AS samna_user WHERE samna_user.user_id = $1 AND samna_user.sa_deleted_at IS NULL AND {} LIMIT 1",
            user_where(actor_id, ACCESS_READ)
        );
        self.query_model::<SamnaUser>(ctx, &sql, &[&user_id]).await
    }

    pub async fn get_users(
        &self,
        ctx: &Ctx,
        user_id: Uuid,
        required_access: ClaimAccess,
    ) -> Result<Vec<SamnaUser>> {
        let sql = format!(
            "SELECT samna_user.* FROM claimius.get_users AS samna_user WHERE samna_user.sa_deleted_at IS NULL AND {}",
            user_where(user_id, required_access)
        );
        self.query_models::<SamnaUser>(ctx, &sql, &[]).await
    }

    pub async fn get_user_claims(&self, ctx: &Ctx, user_id: Uuid) -> Result<Vec<UserClaim>> {
        let mut claims = self.query_models::<UserClaim>(ctx, "SELECT user_claim.* FROM claimius.user_claim WHERE user_claim.user_id = $1 AND user_claim.sa_deleted_at IS NULL", &[&user_id]).await?;
        let owner_ids: Vec<Uuid> = claims.iter().map(|c| c.owner_id).collect();
        let owners = self.get_organizations_of(ctx, &owner_ids).await?;
        for claim in &mut claims {
            claim.owner = owners
                .iter()
                .find(|o| o.id == claim.owner_id)
                .cloned()
                .map(Box::new);
        }
        Ok(claims)
    }

    pub async fn get_user_relations(&self, ctx: &Ctx, user_id: Uuid) -> Result<Vec<UserRelation>> {
        self.get_user_relations_filtered(ctx, user_id, &UserRelationFilters::default())
            .await
    }

    pub async fn get_user_relations_filtered(
        &self,
        ctx: &Ctx,
        user_id: Uuid,
        f: &UserRelationFilters,
    ) -> Result<Vec<UserRelation>> {
        let mut sql =
            "SELECT user_relation.* FROM claimius.get_relations($1) AS user_relation WHERE TRUE"
                .to_string();
        let mut params: Vec<Box<dyn tokio_postgres::types::ToSql + Sync + Send>> =
            vec![Box::new(user_id)];
        let mut n = 2;
        let mut add =
            |sql: &mut String,
             clause: &str,
             param: Box<dyn tokio_postgres::types::ToSql + Sync + Send>| {
                sql.push_str(&clause.replace("$N", &format!("${n}")));
                params.push(param);
                n += 1;
            };
        if !f.org_ids.is_empty() {
            add(
                &mut sql,
                &format!(
                    " AND user_relation.object_type = '{OBJECT_TYPE_ORGANIZATION}' AND user_relation.object_id = ANY($N::uuid[])"
                ),
                Box::new(f.org_ids.clone()),
            );
        }
        if !f.loc_ids.is_empty() {
            add(
                &mut sql,
                &format!(
                    " AND user_relation.object_type = '{OBJECT_TYPE_LOCATION}' AND user_relation.object_id = ANY($N::uuid[])"
                ),
                Box::new(f.loc_ids.clone()),
            );
        }
        if !f.usr_ids.is_empty() {
            add(
                &mut sql,
                &format!(
                    " AND user_relation.object_type = '{OBJECT_TYPE_USER}' AND user_relation.object_id = ANY($N::uuid[])"
                ),
                Box::new(f.usr_ids.clone()),
            );
        }
        if !f.object_types.is_empty() {
            let types: Vec<String> = f
                .object_types
                .iter()
                .map(|t| t.as_str().to_string())
                .collect();
            add(
                &mut sql,
                " AND user_relation.object_type = ANY($N::text[])",
                Box::new(types),
            );
        }
        if !f.object_ids.is_empty() {
            add(
                &mut sql,
                " AND user_relation.object_id = ANY($N::uuid[])",
                Box::new(f.object_ids.clone()),
            );
        }
        if let Some(p) = f.pinned {
            add(&mut sql, " AND user_relation.pinned = $N", Box::new(p));
        }
        if let Some(p) = f.priority {
            add(&mut sql, " AND user_relation.priority = $N", Box::new(p));
        }
        if let Some(p) = f.parent_id {
            add(&mut sql, " AND user_relation.parent_id = $N", Box::new(p));
        }
        if let Some(s) = f.sibling_id {
            add(&mut sql, " AND user_relation.sibling_id = $N", Box::new(s));
        }
        if let Some(d) = &f.description
            && !d.is_empty()
        {
            add(
                &mut sql,
                " AND user_relation.description ILIKE $N",
                Box::new(format!("%{d}%")),
            );
        }
        let refs = super::query::params_ref(&params);
        self.query_models::<UserRelation>(ctx, &sql, &refs).await
    }

    pub async fn get_relation(
        &self,
        ctx: &Ctx,
        user_id: Uuid,
        relation_id: Uuid,
    ) -> Result<UserRelation> {
        let ids = vec![relation_id];
        self.query_model::<UserRelation>(ctx, "SELECT user_relation.* FROM claimius.get_relations($1, $2::uuid[]) AS user_relation LIMIT 1", &[&user_id, &ids]).await
    }

    pub(crate) async fn insert_relation(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        relation: &mut UserRelation,
    ) -> Result<UserRelation> {
        if ac.is_none() {
            return Err(Error::no_access());
        }
        let held = self.conn_or(ctx, conn).await?;
        let mut to_insert = relation.clone();
        to_insert.id = Uuid::nil();
        let row = super::query::insert_generic::<UserRelation>(&held, &to_insert).await?;
        let body = relation.body.clone();
        *relation = UserRelation::from_row(row)?;
        relation.body = body;
        Ok(relation.clone())
    }

    pub(crate) async fn update_relation(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        relation_id: Uuid,
        relation: &mut UserRelation,
    ) -> Result<UserRelation> {
        if ac.is_none() {
            return Err(Error::no_access());
        }
        let held = self.conn_or(ctx, conn).await?;
        let mut null_fields = Vec::new();
        if relation.description.is_none() {
            null_fields.push("description");
        }
        if relation.pinned.is_none() {
            null_fields.push("pinned");
        }
        if relation.priority.is_none() {
            null_fields.push("priority");
        }
        if relation.parent_id.is_none() {
            null_fields.push("parent_id");
        }
        if relation.sibling_id.is_none() {
            null_fields.push("sibling_id");
        }
        let nulls = patch_nulls(&relation.body, UserRelation::COLUMNS, &null_fields);
        let excludes = ["id", "user_id"].map(str::to_string);
        let row = super::query::update_generic::<UserRelation>(
            &held,
            relation,
            relation_id,
            &[],
            &excludes,
            true,
            &nulls,
        )
        .await?;
        let body = relation.body.clone();
        *relation = UserRelation::from_row(row)?;
        relation.body = body;
        Ok(relation.clone())
    }

    pub(crate) async fn delete_relation(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        user_id: Uuid,
        relation_id: Uuid,
    ) -> Result<UserRelation> {
        if ac.is_none() {
            return Err(Error::no_access());
        }
        let held = self.conn_or(ctx, conn).await?;
        match held.query_model::<UserRelation>("UPDATE claimius.user_relation SET sa_deleted_at = NOW() WHERE id = $1 AND user_id = $2 AND sa_deleted_at IS NULL RETURNING *", &[&relation_id, &user_id]).await {
            Ok(r) => Ok(r),
            Err(e) if e.is_no_rows() => Err(Error::no_access()),
            Err(e) => Err(e),
        }
    }

    pub(crate) async fn insert_user(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        input: &mut SamnaUser,
    ) -> Result<(SamnaUser, bool)> {
        if let Some(owner) = input.owner_id.filter(|o| !o.is_nil()) {
            let result = self
                .insert_organization_user(ctx, conn, ac, owner, input, false, None, None)
                .await?;
            let user = result
                .samna_user
                .ok_or_else(|| Error::other("insert_organization_user returned no samna_user"))?;
            return Ok((user, result.created));
        }
        let held = self.conn_or(ctx, conn).await?;
        let user_id: Option<Uuid> = if input.user_id.is_nil() {
            None
        } else {
            Some(input.user_id)
        };
        let payload: Value = held
            .query_scalar::<Value>(
                "SELECT (claimius.upsert_app_user($1, $2, $3, $4, $5, $6, $7, $8) -> 'samna_user')::jsonb",
                &[&user_id, &input.first_name, &input.last_name, &input.user_name, &input.email, &input.phone, &input.external_id, &input.user_image],
            )
            .await?;
        let out: SamnaUser = crate::sauth::unmarshal_value(payload).map_err(Error::Other)?;
        if let Some(claim_id) = input.claim_id.filter(|c| !c.is_nil()) {
            let held_grant: bool = held.query_scalar::<bool>("SELECT EXISTS (SELECT 1 FROM claimius.user_claim WHERE claim_id = $1 AND user_id = $2 AND sa_deleted_at IS NULL)", &[&claim_id, &out.user_id]).await?;
            if held_grant {
                return Ok((out, false));
            }
            let mut uc = UserClaim {
                user_id: out.user_id,
                ..UserClaim::default()
            };
            self.insert_claim_user(ctx, Some(&held), ac, claim_id, &mut uc)
                .await?;
        }
        Ok((out, false))
    }

    pub(crate) async fn update_user(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        user_id: Uuid,
        user: &mut SamnaUser,
    ) -> Result<SamnaUser> {
        if ac.is_none() {
            return Err(Error::no_access());
        }
        let held = self.conn_or(ctx, conn).await?;
        let mut null_fields = Vec::new();
        for (name, is_none) in [
            ("first_name", user.first_name.is_none()),
            ("last_name", user.last_name.is_none()),
            ("user_name", user.user_name.is_none()),
            ("phone", user.phone.is_none()),
            ("external_id", user.external_id.is_none()),
            ("user_image", user.user_image.is_none()),
            ("status", user.status.is_none()),
            ("config", user.config.is_none()),
        ] {
            if is_none {
                null_fields.push(name);
            }
        }
        let nulls = patch_nulls(&user.body, SamnaUser::COLUMNS, &null_fields);
        let mut row = user.to_row()?;
        for col in [
            "id",
            "user_id",
            "type",
            "sa_created_at",
            "sa_updated_at",
            "sa_deleted_at",
        ] {
            row.remove(col);
        }
        row.retain(|_, v| {
            !matches!(v, Value::Null) && !(matches!(v, Value::String(s) if s.is_empty()))
        });
        let mut sets: Vec<String> = row.keys().map(|c| format!("\"{c}\" = p.\"{c}\"")).collect();
        sets.extend(nulls);
        if sets.is_empty() {
            return Err(Error::bad_request("no fields to update"));
        }
        let sql = format!(
            "UPDATE claimius.samna_user AS t SET {} FROM jsonb_populate_record(NULL::claimius.samna_user, $1::jsonb) p WHERE t.user_id = $2 AND t.sa_deleted_at IS NULL RETURNING row_to_json(t)::jsonb",
            sets.join(", ")
        );
        let rows = held
            .client()
            .query(&sql, &[&Value::Object(row), &user_id])
            .await?;
        let Some(first) = rows.into_iter().next() else {
            return Err(Error::no_rows());
        };
        let body = user.body.clone();
        *user = SamnaUser::from_row(first.get::<_, Value>(0))?;
        user.body = body;
        Ok(user.clone())
    }

    pub async fn delete_user_through(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        user_id: Uuid,
    ) -> Result<SamnaUser> {
        let Some(ac) = ac else {
            return Err(Error::no_access());
        };
        let held = self.conn_or(ctx, conn).await?;
        let mut user = held.query_model::<SamnaUser>("SELECT * FROM claimius.samna_user WHERE user_id = $1 AND sa_deleted_at IS NULL LIMIT 1", &[&user_id]).await?;
        self.new_delete(&mut user)
            .conn(&held)
            .access(ac)
            .id(user_id)
            .scan(ctx)
            .await?;
        if user.deleted_at.is_none() {
            user.deleted_at = Some(Utc::now());
        }
        Ok(user)
    }

    pub(crate) async fn delete_user(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        user_id: Uuid,
    ) -> Result<SamnaUser> {
        if ac.is_none() {
            return Err(Error::no_access());
        }
        let held = self.conn_or(ctx, conn).await?;
        let own_tx = conn.is_none();
        if own_tx {
            held.begin().await?;
        }
        let written: Result<SamnaUser> = async {
            let user = held.query_model::<SamnaUser>("UPDATE claimius.samna_user SET sa_deleted_at = NOW() WHERE user_id = $1 AND sa_deleted_at IS NULL RETURNING *", &[&user_id]).await?;
            held.execute("UPDATE claimius.user_identity SET sa_deleted_at = NOW() WHERE user_id = $1 AND TRUE AND sa_deleted_at IS NULL", &[&user_id]).await?;
            Ok(user)
        }
        .await;
        match written {
            Ok(user) => {
                if own_tx {
                    held.commit().await?;
                }
                Ok(user)
            }
            Err(e) => {
                if own_tx {
                    let _ = held.rollback().await;
                }
                Err(e)
            }
        }
    }

    pub async fn get_user_audit(
        &self,
        ctx: &Ctx,
        user_id: Uuid,
        required_access: ClaimAccess,
    ) -> Result<Vec<Audit>> {
        self.query_models::<Audit>(ctx, "SELECT audit.* FROM claimius.get_audit($1, $2) AS audit ORDER BY audit.sa_created_at DESC", &[&user_id, &required_access.0]).await
    }

    pub async fn assert_user_single_root(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        user_id: Uuid,
        target_owner_id: Uuid,
    ) -> Result<()> {
        if user_id.is_nil() || target_owner_id.is_nil() {
            return Ok(());
        }
        let held = self.conn_or(ctx, conn).await?;
        let conflict: bool = held
            .query_scalar::<bool>(
                "SELECT EXISTS (
				SELECT 1
				FROM claimius.user_claim uc
				JOIN claimius.claim c ON c.id = uc.claim_id
				JOIN claimius.organization t ON t.id = $1
				WHERE uc.user_id = $2
				  AND TRUE
				  AND uc.sa_deleted_at IS NULL
				  AND c.sa_deleted_at IS NULL
				  AND c.sa_root_id <> t.sa_root_id
			)",
                &[&target_owner_id, &user_id],
            )
            .await?;
        if conflict {
            return Err(Error::RootConflict);
        }
        Ok(())
    }

    pub async fn assert_user_identity_single_root(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        external_id: &str,
        target_owner_id: Uuid,
    ) -> Result<()> {
        if external_id.is_empty() || target_owner_id.is_nil() {
            return Ok(());
        }
        let held = self.conn_or(ctx, conn).await?;
        let conflict: bool = held
            .query_scalar::<bool>(
                "SELECT EXISTS (
				SELECT 1
				FROM claimius.user_identity ut
				JOIN claimius.organization existing ON existing.id = ut.sa_owner_id
				JOIN claimius.organization target ON target.id = $1
				WHERE ut.external_id = $2
				  AND TRUE
				  AND ut.sa_deleted_at IS NULL
				  AND existing.sa_root_id <> target.sa_root_id
			)",
                &[&target_owner_id, &external_id],
            )
            .await?;
        if conflict {
            return Err(Error::RootConflict);
        }
        Ok(())
    }

    pub async fn post_user(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: &AccessClaim,
        input: &mut SamnaUser,
    ) -> Result<SamnaUser> {
        if ac.user_claim_id.is_none() {
            return Err(Error::no_access());
        }
        let Some(owner) = input.owner_id.filter(|o| !o.is_nil()) else {
            return Err(Error::other("owner_id required"));
        };
        if input.claim_id.is_none_or(|c| c.is_nil()) {
            return Err(Error::other("claim_id required"));
        }
        if !input.user_id.is_nil() {
            self.assert_user_single_root(ctx, conn, input.user_id, owner)
                .await?;
        }
        let held = self.conn_or(ctx, conn).await?;
        match self
            .new_insert(input)
            .conn(&held)
            .access(ac)
            .scan(ctx)
            .await
        {
            Ok(_) => Ok(input.clone()),
            Err(e) if e.is_conflict() || e.pg_code() == Some("23505") => {
                Err(Error::UserAlreadyExists)
            }
            Err(e) => Err(e),
        }
    }

    pub(crate) async fn insert_organization_user(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        owner_id: Uuid,
        user: &SamnaUser,
        upsert: bool,
        identity: Option<Map<String, Value>>,
        reason: Option<&str>,
    ) -> Result<OrganizationUserResult> {
        let Some(uc) = ac.and_then(|a| a.user_claim_id) else {
            return Err(Error::no_access());
        };
        let user_json =
            serde_json::to_string(user).map_err(|e| Error::other(format!("marshal user: {e}")))?;
        let identity_json: Option<String> = identity.map(|i| Value::Object(i).to_string());
        let held = self.conn_or(ctx, conn).await?;
        let payload: Value = match held
            .query_scalar::<Value>("SELECT claimius.insert_organization_user(p_caller_uc => $1, p_owner_id => $2, p_user => $3::text::jsonb, p_identity => $4::text::jsonb, p_reason => $5, p_upsert => $6)", &[&uc, &owner_id, &user_json, &identity_json, &reason, &upsert])
            .await
        {
            Ok(p) => p,
            Err(e) if e.pg_code() == Some("23505") => return Err(Error::UserAlreadyExists),
            Err(e) => return Err(Error::other(format!("insert_organization_user: {e}"))),
        };
        let result: OrganizationUserResult = serde_json::from_value(payload)
            .map_err(|e| Error::other(format!("unmarshal insert_organization_user: {e}")))?;
        if result.samna_user.is_none() {
            return Err(Error::other(
                "insert_organization_user returned no samna_user",
            ));
        }
        Ok(result)
    }

    pub async fn post_user_from_token(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: &AccessClaim,
        payload: SamnaUserToken,
    ) -> Result<(SamnaUser, bool)> {
        if ac.user_claim_id.is_none() {
            return Err(Error::no_access());
        }
        let (_, _, external_id, _) = get_token_identity(&payload)?;
        let owner = payload.owner_id.unwrap_or_default();
        self.put_user_identity(ctx, conn, ac, payload, owner, &external_id)
            .await
    }

    pub async fn post_user_from_external(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: &AccessClaim,
        payload: SamnaUserExternal,
    ) -> Result<(SamnaUser, bool)> {
        if ac.user_claim_id.is_none() {
            return Err(Error::no_access());
        }
        let (_, external_id) = get_external_identity(&payload)?;
        let owner = payload.owner_id.unwrap_or_default();
        self.put_user_identity(ctx, conn, ac, payload, owner, &external_id)
            .await
    }

    async fn put_user_identity<W: Write<Result = SamnaUser>>(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: &AccessClaim,
        mut payload: W,
        owner_id: Uuid,
        external_id: &str,
    ) -> Result<(SamnaUser, bool)> {
        self.assert_user_identity_single_root(ctx, conn, external_id, owner_id)
            .await?;
        let held = self.conn_or(ctx, conn).await?;
        let mut q = self.new_insert(&mut payload).conn(&held).access(ac);
        let user = q.scan(ctx).await?;
        Ok((user, q.created()))
    }

    pub async fn post_user_guest(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: &AccessClaim,
        mut payload: SamnaUserGuest,
    ) -> Result<(SamnaUser, bool)> {
        if ac.user_claim_id.is_none() {
            return Err(Error::no_access());
        }
        let held = self.conn_or(ctx, conn).await?;
        let mut q = self.new_insert(&mut payload).conn(&held).access(ac);
        let user = q.scan(ctx).await?;
        Ok((user, q.created()))
    }

    pub(crate) async fn post_user_from_token_local(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        payload: &SamnaUserToken,
    ) -> Result<OrganizationUserResult> {
        let (user, id_claim, external_id, claims_json) = get_token_identity(payload)?;
        let mut identity = Map::new();
        identity.insert("external_id".to_string(), Value::String(external_id));
        identity.insert("id_claim".to_string(), Value::String(id_claim));
        identity.insert("type".to_string(), Value::String("token".to_string()));
        identity.insert("data".to_string(), claims_json);
        self.insert_organization_user(
            ctx,
            conn,
            ac,
            payload.owner_id.unwrap_or_default(),
            &user,
            true,
            Some(identity),
            payload.reason.as_deref(),
        )
        .await
    }

    pub(crate) async fn post_user_from_external_local(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        payload: &SamnaUserExternal,
    ) -> Result<OrganizationUserResult> {
        let (user, external_id) = get_external_identity(payload)?;
        let mut identity = Map::new();
        identity.insert("external_id".to_string(), Value::String(external_id));
        identity.insert(
            "id_claim".to_string(),
            Value::String("external".to_string()),
        );
        identity.insert("type".to_string(), Value::String("external".to_string()));
        identity.insert("data".to_string(), Value::Object(Map::new()));
        self.insert_organization_user(
            ctx,
            conn,
            ac,
            payload.owner_id.unwrap_or_default(),
            &user,
            true,
            Some(identity),
            payload.reason.as_deref(),
        )
        .await
    }

    pub(crate) async fn post_user_from_guest_local(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        payload: &SamnaUserGuest,
    ) -> Result<OrganizationUserResult> {
        let Some(uc) = ac.and_then(|a| a.user_claim_id) else {
            return Err(Error::no_access());
        };
        let mut user = payload.user.clone().unwrap_or_default();
        user.claim_id = payload.claim_id;
        let user_json =
            serde_json::to_string(&user).map_err(|e| Error::other(format!("marshal user: {e}")))?;
        let held = self.conn_or(ctx, conn).await?;
        let owner = payload.owner_id.unwrap_or_default();
        let ends_at = payload.ends_at.unwrap_or_default();
        let raw: Value = held
            .query_scalar::<Value>(
                "SELECT claimius.insert_organization_guest($1, $2, $3::text::jsonb, $4, $5)",
                &[&uc, &owner, &user_json, &ends_at, &payload.reason],
            )
            .await
            .map_err(|e| Error::other(format!("insert_organization_guest: {e}")))?;
        let result: OrganizationUserResult = serde_json::from_value(raw)
            .map_err(|e| Error::other(format!("unmarshal insert_organization_guest: {e}")))?;
        if result.samna_user.is_none() {
            return Err(Error::other(
                "insert_organization_guest returned no samna_user",
            ));
        }
        Ok(result)
    }

    pub async fn get_users_many(
        &self,
        ctx: &Ctx,
        user_ids: &[Uuid],
        ids: &[Uuid],
        emails: &[String],
        external_ids: &[String],
    ) -> Result<Vec<SamnaUser>> {
        self.query_models::<SamnaUser>(
            ctx,
            "SELECT * FROM claimius.get_users($1::uuid[], $2::uuid[], $3::text[], $4::text[])",
            &[&user_ids, &ids, &emails, &external_ids],
        )
        .await
    }

    pub async fn get_user_identities(
        &self,
        ctx: &Ctx,
        user_ids: &[Uuid],
    ) -> Result<Vec<UserIdentity>> {
        self.query_models::<UserIdentity>(
            ctx,
            "SELECT * FROM claimius.get_user_identities($1::uuid[])",
            &[&user_ids],
        )
        .await
    }
}

fn unverified_claims(token: &str) -> Result<Map<String, Value>> {
    let mut parts = token.split('.');
    let (Some(_), Some(payload)) = (parts.next(), parts.next()) else {
        return Err(Error::other("parse token: token has no payload"));
    };
    use base64::Engine;
    let raw = base64::engine::general_purpose::URL_SAFE_NO_PAD
        .decode(payload)
        .map_err(|e| Error::other(format!("parse token: {e}")))?;
    match serde_json::from_slice::<Value>(&raw) {
        Ok(Value::Object(m)) => Ok(m),
        Ok(_) => Err(Error::other("invalid token claims")),
        Err(e) => Err(Error::other(format!("parse token: {e}"))),
    }
}

pub fn get_token_identity(payload: &SamnaUserToken) -> Result<(SamnaUser, String, String, Value)> {
    if payload.token.is_empty() {
        return Err(Error::other("token is required"));
    }
    let Some(owner) = payload.owner_id.filter(|o| !o.is_nil()) else {
        return Err(Error::other("owner_id required"));
    };
    if payload.claim_id.is_none_or(|c| c.is_nil()) {
        return Err(Error::other("claim_id required"));
    }
    let claims = unverified_claims(&payload.token)?;
    let id_claim = payload
        .id_claim
        .clone()
        .filter(|c| !c.is_empty())
        .unwrap_or_else(|| "sub".to_string());
    let Some(raw_id) = claims.get(&id_claim) else {
        return Err(Error::other("id claim missing in token"));
    };
    let Some(token_id) = raw_id.as_str().filter(|s| !s.is_empty()) else {
        return Err(Error::other("id claim not a string"));
    };
    let mut user_id = Uuid::nil();
    if let Some(u) = &payload.user
        && !u.user_id.is_nil()
    {
        user_id = u.user_id;
    } else if let Ok(parsed) = Uuid::parse_str(token_id) {
        user_id = parsed;
    }
    let external_id = token_id.to_string();
    let mut user = SamnaUser {
        user_id,
        external_id: Some(external_id.clone()),
        owner_id: Some(owner),
        claim_id: payload.claim_id,
        ..SamnaUser::default()
    };
    if let Some(u) = &payload.user {
        user.first_name = u.first_name.clone();
        user.last_name = u.last_name.clone();
        user.user_name = u.user_name.clone();
        user.phone = u.phone.clone();
        user.email = u.email.clone();
    }
    Ok((user, id_claim, external_id, Value::Object(claims)))
}

pub fn get_external_identity(payload: &SamnaUserExternal) -> Result<(SamnaUser, String)> {
    let Some(owner) = payload.owner_id.filter(|o| !o.is_nil()) else {
        return Err(Error::other("owner_id required"));
    };
    if payload.claim_id.is_none_or(|c| c.is_nil()) {
        return Err(Error::other("claim_id required"));
    }
    let Some(u) = &payload.user else {
        return Err(Error::other("user required"));
    };
    let Some(external_id) = u.external_id.clone().filter(|e| !e.is_empty()) else {
        return Err(Error::other("user.external_id required"));
    };
    let mut user = u.clone();
    user.owner_id = Some(owner);
    user.claim_id = payload.claim_id;
    Ok((user, external_id))
}

impl Write for SamnaUser {
    type Result = SamnaUser;

    fn endpoint() -> Option<EndpointSpec> {
        Some(EndpointSpec {
            base: "user",
            parents: &[],
        })
    }

    fn stamp(&mut self, ac: &AccessClaim) {
        if let Some(uc) = ac.user_claim_id
            && !uc.is_nil()
            && self.created_by.is_none_or(|c| c.is_nil())
        {
            self.created_by = Some(uc);
        }
        if !ac.owner_id.is_nil() && self.owner_id.is_none_or(|o| o.is_nil()) {
            self.owner_id = Some(ac.owner_id);
        }
    }

    fn replace_with(&mut self, result: &SamnaUser) {
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
    ) -> impl std::future::Future<Output = Result<Option<(SamnaUser, bool)>>> + Send {
        async move { db.insert_user(ctx, conn, ac, self).await.map(Some) }
    }

    fn update_local(
        &mut self,
        db: &Db,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        id: Uuid,
        _columns: &[String],
    ) -> impl std::future::Future<Output = Result<Option<SamnaUser>>> + Send {
        async move { db.update_user(ctx, conn, ac, id, self).await.map(Some) }
    }

    fn delete_local(
        &mut self,
        db: &Db,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        id: Uuid,
    ) -> impl std::future::Future<Output = Result<Option<SamnaUser>>> + Send {
        async move { db.delete_user(ctx, conn, ac, id).await.map(Some) }
    }
}

impl Model for SamnaUserToken {
    const TABLE: &'static str = "";
    const COLUMNS: &'static [(&'static str, &'static str)] = &[];
    const FIELDS: &'static [&'static str] = &[
        "token",
        "id_claim",
        "user",
        "claim_id",
        "reason",
        "owner_id",
        "created_by",
    ];

    fn id(&self) -> Uuid {
        Uuid::nil()
    }

    fn body(&self) -> Option<&super::bind::Body> {
        Some(&self.body)
    }
}

impl Write for SamnaUserToken {
    type Result = SamnaUser;

    fn payload_endpoint() -> Option<&'static str> {
        Some("user/token")
    }

    fn stamp(&mut self, ac: &AccessClaim) {
        if let Some(uc) = ac.user_claim_id
            && !uc.is_nil()
            && self.created_by.is_none_or(|c| c.is_nil())
        {
            self.created_by = Some(uc);
        }
        if !ac.owner_id.is_nil() && self.owner_id.is_none_or(|o| o.is_nil()) {
            self.owner_id = Some(ac.owner_id);
        }
    }

    fn insert_local(
        &mut self,
        db: &Db,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
    ) -> impl std::future::Future<Output = Result<Option<(SamnaUser, bool)>>> + Send {
        async move {
            let result = db.post_user_from_token_local(ctx, conn, ac, self).await?;
            Ok(Some((
                result.samna_user.unwrap_or_default(),
                result.created,
            )))
        }
    }
}

impl Model for SamnaUserExternal {
    const TABLE: &'static str = "";
    const COLUMNS: &'static [(&'static str, &'static str)] = &[];
    const FIELDS: &'static [&'static str] =
        &["user", "claim_id", "reason", "owner_id", "created_by"];

    fn id(&self) -> Uuid {
        Uuid::nil()
    }

    fn body(&self) -> Option<&super::bind::Body> {
        Some(&self.body)
    }
}

impl Write for SamnaUserExternal {
    type Result = SamnaUser;

    fn payload_endpoint() -> Option<&'static str> {
        Some("user/external")
    }

    fn stamp(&mut self, ac: &AccessClaim) {
        if let Some(uc) = ac.user_claim_id
            && !uc.is_nil()
            && self.created_by.is_none_or(|c| c.is_nil())
        {
            self.created_by = Some(uc);
        }
        if !ac.owner_id.is_nil() && self.owner_id.is_none_or(|o| o.is_nil()) {
            self.owner_id = Some(ac.owner_id);
        }
    }

    fn insert_local(
        &mut self,
        db: &Db,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
    ) -> impl std::future::Future<Output = Result<Option<(SamnaUser, bool)>>> + Send {
        async move {
            let result = db
                .post_user_from_external_local(ctx, conn, ac, self)
                .await?;
            Ok(Some((
                result.samna_user.unwrap_or_default(),
                result.created,
            )))
        }
    }
}

impl Model for SamnaUserGuest {
    const TABLE: &'static str = "";
    const COLUMNS: &'static [(&'static str, &'static str)] = &[];
    const FIELDS: &'static [&'static str] = &[
        "user",
        "claim_id",
        "ends_at",
        "reason",
        "owner_id",
        "created_by",
    ];

    fn id(&self) -> Uuid {
        Uuid::nil()
    }

    fn body(&self) -> Option<&super::bind::Body> {
        Some(&self.body)
    }
}

impl Write for SamnaUserGuest {
    type Result = SamnaUser;

    fn payload_endpoint() -> Option<&'static str> {
        Some("user/guest")
    }

    fn stamp(&mut self, ac: &AccessClaim) {
        if let Some(uc) = ac.user_claim_id
            && !uc.is_nil()
            && self.created_by.is_none_or(|c| c.is_nil())
        {
            self.created_by = Some(uc);
        }
        if !ac.owner_id.is_nil() && self.owner_id.is_none_or(|o| o.is_nil()) {
            self.owner_id = Some(ac.owner_id);
        }
    }

    fn insert_local(
        &mut self,
        db: &Db,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
    ) -> impl std::future::Future<Output = Result<Option<(SamnaUser, bool)>>> + Send {
        async move {
            let result = db.post_user_from_guest_local(ctx, conn, ac, self).await?;
            Ok(Some((
                result.samna_user.unwrap_or_default(),
                result.created,
            )))
        }
    }
}

impl Write for UserRelation {
    type Result = UserRelation;

    fn endpoint() -> Option<EndpointSpec> {
        Some(EndpointSpec {
            base: "relation",
            parents: &[],
        })
    }

    fn parent_id(&self, parent: &str) -> Result<Uuid> {
        if parent == "user" {
            if self.user_id.is_nil() {
                return Err(Error::other("model.user_id required"));
            }
            return Ok(self.user_id);
        }
        Err(Error::other(format!(
            "parent {parent:?} not derivable from model"
        )))
    }

    fn replace_with(&mut self, result: &UserRelation) {
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
    ) -> impl std::future::Future<Output = Result<Option<(UserRelation, bool)>>> + Send {
        async move {
            db.insert_relation(ctx, conn, ac, self)
                .await
                .map(|r| Some((r, false)))
        }
    }

    fn update_local(
        &mut self,
        db: &Db,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        id: Uuid,
        _columns: &[String],
    ) -> impl std::future::Future<Output = Result<Option<UserRelation>>> + Send {
        async move { db.update_relation(ctx, conn, ac, id, self).await.map(Some) }
    }

    fn delete_local(
        &mut self,
        db: &Db,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        id: Uuid,
    ) -> impl std::future::Future<Output = Result<Option<UserRelation>>> + Send {
        async move {
            if self.user_id.is_nil() {
                return Err(Error::other(
                    "delete user_relation: user_id required on model",
                ));
            }
            db.delete_relation(ctx, conn, ac, self.user_id, id)
                .await
                .map(Some)
        }
    }
}

impl Write for UserIdentity {
    type Result = UserIdentity;

    fn insert_local(
        &mut self,
        _db: &Db,
        _ctx: &Ctx,
        _conn: Option<&Conn>,
        _ac: Option<&AccessClaim>,
    ) -> impl std::future::Future<Output = Result<Option<(UserIdentity, bool)>>> + Send {
        async {
            Err(Error::other(
                "user_identity creation reserved; use PostUserFromToken",
            ))
        }
    }
}

impl Write for ObjectAsset {
    type Result = ObjectAsset;

    fn endpoint() -> Option<EndpointSpec> {
        Some(EndpointSpec {
            base: "object_asset",
            parents: &[],
        })
    }

    fn stamp(&mut self, ac: &AccessClaim) {
        stamp_access_claim(&mut self.created_by, &mut self.owner_id, ac);
    }

    fn replace_with(&mut self, result: &ObjectAsset) {
        let body = self.body.clone();
        *self = result.clone();
        self.body = body;
    }

    fn delete_local(
        &mut self,
        db: &Db,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        id: Uuid,
    ) -> impl std::future::Future<Output = Result<Option<ObjectAsset>>> + Send {
        async move { db.delete_object_asset(ctx, conn, ac, id).await.map(Some) }
    }
}

pub fn client_unused() -> Option<client::ErrorKind> {
    None
}
