use uuid::Uuid;

use super::db::{Conn, Db};
use super::error::{Error, Result};
use super::models::*;
use super::organization::patch_nulls;
use crate::client::Ctx;

impl Db {
    pub async fn get_assets(&self, ctx: &Ctx) -> Result<Vec<Asset>> {
        self.query_models::<Asset>(
            ctx,
            "SELECT * FROM public.asset WHERE sa_deleted_at IS NULL",
            &[],
        )
        .await
    }

    pub async fn get_asset(&self, ctx: &Ctx, asset_id: Uuid) -> Result<Asset> {
        self.query_model::<Asset>(
            ctx,
            "SELECT * FROM public.asset WHERE id = $1 AND sa_deleted_at IS NULL LIMIT 1",
            &[&asset_id],
        )
        .await
    }

    pub async fn get_user_assets(&self, ctx: &Ctx, user_id: Uuid) -> Result<Vec<Asset>> {
        self.query_models::<Asset>(
            ctx,
            "SELECT asset.* FROM public.asset AS asset JOIN claimius.object_asset AS oa ON oa.asset_id = asset.id AND oa.sa_deleted_at IS NULL WHERE oa.object_type = $1 AND oa.object_id = $2 AND asset.sa_deleted_at IS NULL ORDER BY asset.\"index\" ASC, asset.sa_created_at DESC",
            &[&OBJECT_TYPE_USER, &user_id],
        )
        .await
    }

    pub async fn insert_asset(
        &self,
        ctx: &Ctx,
        ac: Option<&AccessClaim>,
        asset: &mut Asset,
    ) -> Result<Asset> {
        if ac.is_none() {
            return Err(Error::no_access());
        }
        let conn = self.conn(ctx).await?;
        let mut to_insert = asset.clone();
        to_insert.id = Uuid::nil();
        let row = super::query::insert_generic::<Asset>(&conn, &to_insert).await?;
        let body = asset.body.clone();
        *asset = Asset::from_row(row)?;
        asset.body = body;
        Ok(asset.clone())
    }

    pub async fn update_asset(
        &self,
        ctx: &Ctx,
        ac: Option<&AccessClaim>,
        asset_id: Uuid,
        asset: &mut Asset,
    ) -> Result<Asset> {
        if ac.is_none() {
            return Err(Error::no_access());
        }
        let conn = self.conn(ctx).await?;
        let null_fields: Vec<&str> = if asset.description.is_none() {
            vec!["description"]
        } else {
            Vec::new()
        };
        let nulls = patch_nulls(&asset.body, Asset::COLUMNS, &null_fields);
        let excludes = ["id", "user_id", "blob_url"].map(str::to_string);
        let row = super::query::update_generic::<Asset>(
            &conn,
            asset,
            asset_id,
            &[],
            &excludes,
            true,
            &nulls,
        )
        .await?;
        let body = asset.body.clone();
        *asset = Asset::from_row(row)?;
        asset.body = body;
        Ok(asset.clone())
    }

    pub async fn delete_asset(
        &self,
        ctx: &Ctx,
        ac: Option<&AccessClaim>,
        asset_id: Uuid,
    ) -> Result<Asset> {
        if ac.is_none() {
            return Err(Error::no_access());
        }
        self.query_model::<Asset>(ctx, "UPDATE public.asset SET sa_deleted_at = NOW() WHERE id = $1 AND sa_deleted_at IS NULL RETURNING *", &[&asset_id]).await
    }

    pub async fn get_object_assets(
        &self,
        ctx: &Ctx,
        user_id: Uuid,
        object_type: &str,
        object_id: Uuid,
        kind: &str,
    ) -> Result<Vec<ObjectAsset>> {
        let kind_arg: Option<String> = if kind.is_empty() {
            None
        } else {
            Some(kind.to_string())
        };
        self.query_models::<ObjectAsset>(
            ctx,
            "SELECT * FROM claimius.get_object_assets($1, $2, $3, $4)",
            &[&user_id, &object_type, &object_id, &kind_arg],
        )
        .await
    }

    pub async fn get_object_asset(
        &self,
        ctx: &Ctx,
        user_id: Uuid,
        link_id: Uuid,
    ) -> Result<ObjectAsset> {
        let links = self
            .query_models::<ObjectAsset>(ctx, "SELECT a.* FROM claimius.object_asset l CROSS JOIN LATERAL claimius.get_object_assets($1, l.object_type, l.object_id) a WHERE l.id = $2 AND a.id = l.id", &[&user_id, &link_id])
            .await?;
        links.into_iter().next().ok_or_else(Error::not_found)
    }

    pub(crate) async fn delete_object_asset(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        link_id: Uuid,
    ) -> Result<ObjectAsset> {
        if ac.is_none() {
            return Err(Error::no_access());
        }
        let held = self.conn_or(ctx, conn).await?;
        held.query_model::<ObjectAsset>("UPDATE claimius.object_asset SET sa_deleted_at = NOW() WHERE id = $1 AND sa_deleted_at IS NULL RETURNING *", &[&link_id]).await
    }
}

impl super::query::Write for Asset {
    type Result = Asset;

    fn replace_with(&mut self, result: &Asset) {
        let body = self.body.clone();
        *self = result.clone();
        self.body = body;
    }
}
