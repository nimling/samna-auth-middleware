use uuid::Uuid;

use super::db::Db;
use super::error::{Error, Result};
use super::models::*;
use super::query::{params_ref, query_page, query_with_object};
use crate::client::Ctx;

pub const ME_CONTEXT_KEY: &str = "user_id";

fn me_user_id(user_id: Option<Uuid>) -> Result<Uuid> {
    match user_id {
        Some(id) if !id.is_nil() => Ok(id),
        _ => Err(Error::other("me: no authenticated user in context")),
    }
}

impl Db {
    pub async fn get_me(&self, ctx: &Ctx, user_id: Option<Uuid>) -> Result<SamnaUser> {
        let user_id = me_user_id(user_id)?;
        self.get_user(ctx, user_id, user_id).await
    }

    pub async fn update_me(
        &self,
        ctx: &Ctx,
        user_id: Option<Uuid>,
        user: &mut SamnaUser,
    ) -> Result<SamnaUser> {
        let user_id = me_user_id(user_id)?;
        let ac = self
            .mutation_access(ctx, user_id, user_id, OBJECT_TYPE_USER, ACCESS_WRITE)
            .await?;
        self.new_update(user)
            .access(&ac)
            .id(user_id)
            .scan(ctx)
            .await?;
        Ok(user.clone())
    }

    pub async fn get_me_claims(
        &self,
        ctx: &Ctx,
        user_id: Option<Uuid>,
        with: &[String],
    ) -> Result<Vec<Claim>> {
        let user_id = me_user_id(user_id)?;
        let with = query_with_object(with)?;
        let mut claims = self.query_models::<Claim>(ctx, "SELECT claim.* FROM claimius.claim WHERE claim.sa_deleted_at IS NULL AND claim.id IN (SELECT id FROM claimius.get_claims($1, $2))", &[&user_id, &ACCESS_NONE.0]).await?;
        let ids: Vec<Uuid> = claims.iter().map(|c| c.id).collect();
        for w in with {
            match w.as_str() {
                OBJECT_TYPE_CLAIM_OBJECT => {
                    let objects = self.get_claim_objects_of(ctx, &ids).await?;
                    for claim in &mut claims {
                        claim.claim_objects = objects
                            .iter()
                            .filter(|o| o.claim_id == claim.id)
                            .cloned()
                            .collect();
                    }
                }
                OBJECT_TYPE_USER_CLAIM => {
                    let users = self.get_claim_users_of(ctx, &ids).await?;
                    for claim in &mut claims {
                        claim.claim_users = users
                            .iter()
                            .filter(|u| u.claim_id == claim.id)
                            .cloned()
                            .collect();
                    }
                }
                OBJECT_TYPE_ORGANIZATION => {
                    let owner_ids: Vec<Uuid> = claims.iter().map(|c| c.owner_id).collect();
                    let owners = self.get_organizations_of(ctx, &owner_ids).await?;
                    for claim in &mut claims {
                        claim.owner = owners
                            .iter()
                            .find(|o| o.id == claim.owner_id)
                            .cloned()
                            .map(Box::new);
                    }
                }
                other => {
                    return Err(Error::bad_request(format!(
                        "unsupported with object: {other}"
                    )));
                }
            }
        }
        Ok(claims)
    }

    pub async fn get_me_relations(
        &self,
        ctx: &Ctx,
        user_id: Option<Uuid>,
    ) -> Result<Vec<UserRelation>> {
        let user_id = me_user_id(user_id)?;
        self.get_user_relations(ctx, user_id).await
    }

    pub async fn get_me_organizations(
        &self,
        ctx: &Ctx,
        user_id: Option<Uuid>,
        with: &[String],
        cursor: Option<&str>,
        limit: Option<&str>,
    ) -> Result<Vec<Organization>> {
        let user_id = me_user_id(user_id)?;
        let with = query_with_object(with)?;
        let (after, limit) = query_page(cursor, limit)?;
        let mut orgs = self
            .get_organizations(ctx, user_id, ACCESS_NONE, after, limit)
            .await?;
        for w in with {
            match w.as_str() {
                OBJECT_TYPE_ORGANIZATION => {
                    let parent_ids: Vec<Uuid> = orgs.iter().map(|o| o.owner_id).collect();
                    let parents = self.get_organizations_of(ctx, &parent_ids).await?;
                    for org in &mut orgs {
                        org.parent = parents
                            .iter()
                            .find(|p| p.id == org.owner_id)
                            .cloned()
                            .map(Box::new);
                    }
                }
                other => {
                    return Err(Error::bad_request(format!(
                        "unsupported with object: {other}"
                    )));
                }
            }
        }
        Ok(orgs)
    }

    pub async fn get_me_locations(
        &self,
        ctx: &Ctx,
        user_id: Option<Uuid>,
        with: &[String],
        cursor: Option<&str>,
        limit: Option<&str>,
    ) -> Result<Vec<Location>> {
        let user_id = me_user_id(user_id)?;
        let with = query_with_object(with)?;
        let (after, limit) = query_page(cursor, limit)?;
        let mut locs = self
            .get_locations(ctx, user_id, ACCESS_READ, after, limit)
            .await?;
        for w in with {
            match w.as_str() {
                OBJECT_TYPE_ORGANIZATION => {
                    let owner_ids: Vec<Uuid> = locs.iter().map(|l| l.owner_id).collect();
                    let owners = self.get_organizations_of(ctx, &owner_ids).await?;
                    for loc in &mut locs {
                        loc.owner = owners
                            .iter()
                            .find(|o| o.id == loc.owner_id)
                            .cloned()
                            .map(Box::new);
                    }
                }
                other => {
                    return Err(Error::bad_request(format!(
                        "unsupported with object: {other}"
                    )));
                }
            }
        }
        Ok(locs)
    }

    pub async fn get_me_assets(&self, ctx: &Ctx, user_id: Option<Uuid>) -> Result<Vec<Asset>> {
        let user_id = me_user_id(user_id)?;
        self.get_user_assets(ctx, user_id).await
    }

    pub async fn get_me_audit(&self, ctx: &Ctx, user_id: Option<Uuid>) -> Result<Vec<Audit>> {
        let user_id = me_user_id(user_id)?;
        self.get_user_audit(ctx, user_id, ACCESS_READ).await
    }

    pub async fn list_table<T: Model>(
        &self,
        ctx: &Ctx,
        user_id: Uuid,
        required_access: ClaimAccess,
        after: Option<Uuid>,
        limit: i32,
    ) -> Result<Vec<T>> {
        let w = super::query::object_where(
            "t",
            user_id,
            T::TABLE,
            required_access,
            None,
            after,
            limit,
            1,
        );
        let order = if w.order_sql.is_empty() {
            "t.id".to_string()
        } else {
            w.order_sql.clone()
        };
        let sql = format!(
            "SELECT t.* FROM {} AS t WHERE t.sa_deleted_at IS NULL AND {} ORDER BY {order}",
            super::query::quote_table(T::TABLE),
            w.where_sql
        );
        let params = params_ref(&w.params);
        self.query_models::<T>(ctx, &sql, &params).await
    }
}
