use uuid::Uuid;

use super::db::{Conn, Db};
use super::error::{Error, Result};
use super::models::*;
use super::query::quote_table;
use crate::client::Ctx;

pub fn expand_access(a: ClaimAccess) -> ClaimAccess {
    let mut a = a;
    if a.has(ACCESS_OWNER) {
        a |= ACCESS_WRITE | ACCESS_READ | ACCESS_EXECUTE;
    }
    if a.has(ACCESS_WRITE) {
        a |= ACCESS_READ | ACCESS_EXECUTE;
    }
    if a.has(ACCESS_EXECUTE) {
        a |= ACCESS_READ;
    }
    a
}

impl Db {
    pub async fn create_claim(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: &AccessClaim,
        p: CreateClaimParams,
    ) -> Result<Claim> {
        if p.name.is_empty() {
            return Err(Error::other("claim name is required"));
        }
        let claim_type = if p.r#type.is_empty() {
            CLAIM_TYPE_VIRTUAL.to_string()
        } else {
            p.r#type
        };
        let inherits = p.inherits.unwrap_or(true);
        let held = self.conn_or(ctx, conn).await?;
        let root_id: Uuid = held.query_scalar("SELECT sa_root_id FROM claimius.organization WHERE id = $1 AND sa_deleted_at IS NULL", &[&ac.owner_id]).await?;
        let mut claim = Claim {
            name: p.name,
            description: p.description,
            access: p.access,
            inherits,
            level: p.level,
            r#type: claim_type,
            owner_id: ac.owner_id,
            root_id,
            created_by: ac.user_claim_id,
            ..Claim::default()
        };
        self.new_insert(&mut claim)
            .conn(&held)
            .access(ac)
            .scan(ctx)
            .await?;
        Ok(claim)
    }

    pub async fn assign_object(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: &AccessClaim,
        claim: &mut Claim,
        target: ClaimObject,
    ) -> Result<ClaimObject> {
        let mut co = target;
        if let Some(t) = &co.object_type {
            co.object_type = Some(resolve_object_type(t.as_str())?);
        }
        co.claim_id = claim.id;
        let held = self.conn_or(ctx, conn).await?;
        self.new_insert(&mut co)
            .conn(&held)
            .access(ac)
            .scan(ctx)
            .await?;
        Ok(co)
    }

    pub async fn assign_model_object<M: Model>(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: &AccessClaim,
        claim: &mut Claim,
        target: &M,
    ) -> Result<ClaimObject> {
        let id = target.id();
        if id.is_nil() {
            return Err(Error::other("model.ID is zero"));
        }
        let co = ClaimObject {
            object_id: Some(id),
            object_type: Some(ObjectType::new(M::TABLE)),
            inherits: true,
            ..ClaimObject::default()
        };
        self.assign_object(ctx, conn, ac, claim, co).await
    }

    pub async fn assign_user(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: &AccessClaim,
        claim: &Claim,
        user_ids: &[Uuid],
    ) -> Result<Vec<UserClaim>> {
        let mut results = Vec::with_capacity(user_ids.len());
        let held = self.conn_or(ctx, conn).await?;
        for uid in user_ids {
            if uid.is_nil() {
                continue;
            }
            let existing = held.query_model_opt::<UserClaim>("SELECT * FROM claimius.user_claim WHERE claim_id = $1 AND user_id = $2 AND sa_deleted_at IS NULL LIMIT 1", &[&claim.id, uid]).await?;
            if let Some(existing) = existing {
                results.push(existing);
                continue;
            }
            let mut uc = UserClaim {
                claim_id: claim.id,
                user_id: *uid,
                ..UserClaim::default()
            };
            self.new_insert(&mut uc)
                .conn(&held)
                .access(ac)
                .scan(ctx)
                .await?;
            results.push(uc);
        }
        Ok(results)
    }

    pub async fn re_create_for(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: &AccessClaim,
        claim: &Claim,
        access: ClaimAccess,
        opts: ReCreateForOptions,
        user_ids: &[Uuid],
    ) -> Result<Claim> {
        let name = opts.name.clone().unwrap_or_else(|| claim.name.clone());
        let inherits = claim.inherits;
        let held = self.conn_or(ctx, conn).await?;
        let mut bindings = claim.claim_objects.clone();
        if bindings.is_empty() {
            bindings = held.query_models::<ClaimObject>("SELECT * FROM claimius.claim_object WHERE claim_id = $1 AND sa_deleted_at IS NULL", &[&claim.id]).await?;
        }
        let expanded = expand_access(access);
        let mut owner = ac.clone();
        if let Some(primary) = bindings.first() {
            if let (Some(object_id), Some(object_type)) =
                (primary.object_id, primary.object_type.clone())
                && !object_type.as_str().starts_with("claimius.")
            {
                let types = self.get_object_types(ctx).await?;
                if types.iter().any(|t| *t == object_type) {
                    let sql = format!(
                        "SELECT sa_owner_id FROM {} WHERE id = $1 AND sa_deleted_at IS NULL",
                        quote_table(object_type.as_str())
                    );
                    owner.owner_id = held.query_scalar::<Uuid>(&sql, &[&object_id]).await?;
                }
            }
            let object_type = primary.object_type.as_ref().map(|t| t.as_str().to_string());
            let existing = held
                .query_model_opt::<Claim>(
                    "SELECT claim.* FROM claimius.claim WHERE claim.sa_deleted_at IS NULL AND claim.type = $1 AND claim.sa_access = $2 AND claim.inherits = $3 AND claim.name = $4 AND claim.sa_owner_id = $5 AND EXISTS (
					SELECT 1 FROM claimius.claim_object co
					WHERE co.claim_id = claim.id
					  AND co.sa_deleted_at IS NULL
					  AND co.object_id = $6
					  AND co.object_type = $7
				) LIMIT 1",
                    &[&claim.r#type, &expanded.0, &inherits, &name, &owner.owner_id, &primary.object_id, &object_type],
                )
                .await?;
            if let Some(existing) = existing {
                if !user_ids.is_empty() {
                    self.assign_user(ctx, Some(&held), &owner, &existing, user_ids)
                        .await?;
                }
                return Ok(existing);
            }
        }
        let params = CreateClaimParams {
            name,
            description: claim.description.clone(),
            access,
            inherits: Some(inherits),
            r#type: claim.r#type.clone(),
            level: claim.level,
        };
        let mut new_claim = self.create_claim(ctx, Some(&held), &owner, params).await?;
        for src in &bindings {
            let clone = ClaimObject {
                object_id: src.object_id,
                object_type: src.object_type.clone(),
                reason: src.reason.clone(),
                scope: src.scope.clone(),
                ref_id: src.ref_id,
                inherits: src.inherits,
                access: src.access,
                ..ClaimObject::default()
            };
            self.assign_object(ctx, Some(&held), &owner, &mut new_claim, clone)
                .await?;
        }
        if opts.copy_users {
            let existing_users = held.query_models::<UserClaim>("SELECT * FROM claimius.user_claim WHERE claim_id = $1 AND sa_deleted_at IS NULL", &[&claim.id]).await?;
            let copy_ids: Vec<Uuid> = existing_users.iter().map(|u| u.user_id).collect();
            if !copy_ids.is_empty() {
                self.assign_user(ctx, Some(&held), &owner, &new_claim, &copy_ids)
                    .await?;
            }
        }
        if !user_ids.is_empty() {
            self.assign_user(ctx, Some(&held), &owner, &new_claim, user_ids)
                .await?;
        }
        Ok(new_claim)
    }
}

impl super::query::Write for Claim {
    type Result = Claim;

    fn endpoint() -> Option<super::query::EndpointSpec> {
        Some(super::query::EndpointSpec {
            base: "claim",
            parents: &[],
        })
    }

    fn stamp(&mut self, ac: &AccessClaim) {
        super::query::stamp_access_claim(&mut self.created_by, &mut self.owner_id, ac);
    }

    fn stamp_root(&mut self, root: Uuid) {
        if self.root_id.is_nil() {
            self.root_id = root;
        }
    }

    fn replace_with(&mut self, result: &Claim) {
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
    ) -> impl std::future::Future<Output = Result<Option<(Claim, bool)>>> + Send {
        async move {
            db.insert_claim(ctx, conn, ac, self)
                .await
                .map(|c| Some((c, false)))
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
    ) -> impl std::future::Future<Output = Result<Option<Claim>>> + Send {
        async move { db.update_claim(ctx, conn, ac, id, self).await.map(Some) }
    }

    fn delete_local(
        &mut self,
        db: &Db,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        id: Uuid,
    ) -> impl std::future::Future<Output = Result<Option<Claim>>> + Send {
        async move { db.delete_claim(ctx, conn, ac, id).await.map(Some) }
    }
}
