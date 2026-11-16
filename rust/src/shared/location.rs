use uuid::Uuid;

use super::db::{Conn, Db};
use super::error::{Error, Result};
use super::models::*;
use super::organization::patch_nulls;
use super::query::{EndpointSpec, Write, params_ref, stamp_access_claim};
use crate::client::Ctx;

impl Db {
    pub async fn get_location(&self, ctx: &Ctx, location_id: Uuid) -> Result<Location> {
        self.query_model::<Location>(ctx, "SELECT location.* FROM claimius.location WHERE location.id = $1 AND location.sa_deleted_at IS NULL LIMIT 1", &[&location_id]).await
    }

    pub async fn get_locations(
        &self,
        ctx: &Ctx,
        user_id: Uuid,
        required_access: ClaimAccess,
        after: Option<Uuid>,
        limit: i32,
    ) -> Result<Vec<Location>> {
        let w = super::query::object_where(
            "location",
            user_id,
            OBJECT_TYPE_LOCATION,
            required_access,
            None,
            after,
            limit,
            1,
        );
        let order = if w.order_sql.is_empty() {
            "location.id".to_string()
        } else {
            w.order_sql.clone()
        };
        let sql = format!(
            "SELECT location.* FROM claimius.location WHERE location.sa_deleted_at IS NULL AND {} ORDER BY {order}",
            w.where_sql
        );
        let params = params_ref(&w.params);
        self.query_models::<Location>(ctx, &sql, &params).await
    }

    pub async fn get_child_locations(&self, ctx: &Ctx, start_id: Uuid) -> Result<Vec<Uuid>> {
        let conn = self.read_conn(ctx).await?;
        conn.query_scalars::<Uuid>(
            "SELECT object_id FROM claimius.get_subtree('location', (SELECT sa_root_id FROM claimius.location WHERE id = $1 AND sa_deleted_at IS NULL), $2, $1) WHERE object_type = $2 AND object_id <> $1",
            &[&start_id, &OBJECT_TYPE_LOCATION],
        )
        .await
    }

    pub async fn get_parent_locations(&self, ctx: &Ctx, start_id: Uuid) -> Result<Vec<Uuid>> {
        let conn = self.read_conn(ctx).await?;
        conn.query_scalars::<Uuid>(
            "SELECT object_id FROM claimius.get_parenthood_ancestors($1, $2) WHERE hop > 0",
            &[&OBJECT_TYPE_LOCATION, &start_id],
        )
        .await
    }

    pub(crate) async fn insert_location(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        loc: &mut Location,
    ) -> Result<Location> {
        let Some(ac) = ac else {
            return Err(Error::no_access());
        };
        let (Some(claim_id), Some(user_claim_id)) = (ac.claim_id, ac.user_claim_id) else {
            return Err(Error::no_access());
        };
        loc.created_by = user_claim_id;
        let binding_owner = if loc.owner_id.is_nil() {
            ac.owner_id
        } else {
            loc.owner_id
        };
        let held = self.conn_or(ctx, conn).await?;
        let own_tx = conn.is_none();
        if own_tx {
            held.begin().await?;
        }
        let written: Result<Location> = async {
            let mut to_insert = loc.clone();
            to_insert.id = Uuid::nil();
            to_insert.root_id = Uuid::nil();
            let row = super::query::insert_generic::<Location>(&held, &to_insert).await?;
            let inserted = Location::from_row(row)?;
            let binding_root: Uuid = held.query_scalar("SELECT sa_root_id FROM claimius.organization WHERE id = $1 AND sa_deleted_at IS NULL", &[&binding_owner]).await?;
            let access = (ACCESS_OWNER | ACCESS_WRITE | ACCESS_READ).0;
            held.execute(
                "SELECT claimius.insert_claim_object(
				p_claim_id      := $1,
				p_sa_owner_id   := $2,
				p_sa_root_id    := $3,
				p_sa_created_by := $4,
				p_object_id     := $5,
				p_object_type   := 'claimius.location',
				p_sa_access     := $6,
				p_inherits      := TRUE
			)",
                &[&claim_id, &binding_owner, &binding_root, &user_claim_id, &inserted.id, &access],
            )
            .await?;
            Ok(inserted)
        }
        .await;
        match written {
            Ok(inserted) => {
                if own_tx {
                    held.commit().await?;
                }
                let body = loc.body.clone();
                *loc = inserted;
                loc.body = body;
                Ok(loc.clone())
            }
            Err(e) => {
                if own_tx {
                    let _ = held.rollback().await;
                }
                Err(e)
            }
        }
    }

    pub(crate) async fn update_location(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        location_id: Uuid,
        loc: &mut Location,
    ) -> Result<Location> {
        if ac.is_none() {
            return Err(Error::no_access());
        }
        let held = self.conn_or(ctx, conn).await?;
        let nulls = patch_nulls(&loc.body, Location::COLUMNS, &loc_null_fields(loc));
        let excludes = [
            "id",
            "sa_owner_id",
            "sa_root_id",
            "sa_parent_id",
            "sa_created_by",
            "provider_id",
        ]
        .map(str::to_string);
        let row = super::query::update_generic::<Location>(
            &held,
            loc,
            location_id,
            &[],
            &excludes,
            true,
            &nulls,
        )
        .await?;
        let body = loc.body.clone();
        *loc = Location::from_row(row)?;
        loc.body = body;
        Ok(loc.clone())
    }

    pub(crate) async fn delete_location(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        location_id: Uuid,
    ) -> Result<Location> {
        if ac.is_none() {
            return Err(Error::no_access());
        }
        let held = self.conn_or(ctx, conn).await?;
        held.query_model::<Location>("UPDATE claimius.location SET sa_deleted_at = NOW() WHERE id = $1 AND sa_deleted_at IS NULL RETURNING *", &[&location_id]).await
    }

    pub async fn get_location_audit(
        &self,
        ctx: &Ctx,
        actor_id: Uuid,
        location_id: Uuid,
        required_access: ClaimAccess,
    ) -> Result<Vec<Audit>> {
        self.query_models::<Audit>(
            ctx,
            "SELECT audit.* FROM claimius.get_audit($1, $2) AS audit WHERE audit.object_id = $3 AND audit.object_type = $4 ORDER BY audit.sa_created_at DESC",
            &[&actor_id, &required_access.0, &location_id, &OBJECT_TYPE_LOCATION],
        )
        .await
    }

    pub async fn get_locations_many(
        &self,
        ctx: &Ctx,
        ids: &[Uuid],
        parent_ids: &[Uuid],
        external_ids: &[String],
    ) -> Result<Vec<Location>> {
        let ids = (!ids.is_empty()).then_some(ids);
        let parent_ids = (!parent_ids.is_empty()).then_some(parent_ids);
        let external_ids = (!external_ids.is_empty()).then_some(external_ids);
        self.query_models::<Location>(
            ctx,
            "SELECT * FROM claimius.get_locations($1::uuid[], $2::uuid[], $3::text[])",
            &[&ids, &parent_ids, &external_ids],
        )
        .await
    }
}

fn loc_null_fields(loc: &Location) -> Vec<&'static str> {
    let mut out = Vec::new();
    if loc.name.is_none() {
        out.push("name");
    }
    if loc.description.is_none() {
        out.push("description");
    }
    if loc.longitude.is_none() {
        out.push("longitude");
    }
    if loc.latitude.is_none() {
        out.push("latitude");
    }
    if loc.code_type.is_none() {
        out.push("code_type");
    }
    if loc.code.is_none() {
        out.push("code");
    }
    if loc.external_id.is_none() {
        out.push("external_id");
    }
    out
}

impl Write for Location {
    type Result = Location;

    fn endpoint() -> Option<EndpointSpec> {
        Some(EndpointSpec {
            base: "location",
            parents: &[],
        })
    }

    fn stamp(&mut self, ac: &AccessClaim) {
        let mut created_by = if self.created_by.is_nil() {
            None
        } else {
            Some(self.created_by)
        };
        stamp_access_claim(&mut created_by, &mut self.owner_id, ac);
        if let Some(c) = created_by {
            self.created_by = c;
        }
    }

    fn location_mut(&mut self) -> Option<&mut Location> {
        Some(self)
    }

    fn replace_with(&mut self, result: &Location) {
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
    ) -> impl std::future::Future<Output = Result<Option<(Location, bool)>>> + Send {
        async move {
            db.insert_location(ctx, conn, ac, self)
                .await
                .map(|l| Some((l, false)))
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
    ) -> impl std::future::Future<Output = Result<Option<Location>>> + Send {
        async move { db.update_location(ctx, conn, ac, id, self).await.map(Some) }
    }

    fn delete_local(
        &mut self,
        db: &Db,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        id: Uuid,
    ) -> impl std::future::Future<Output = Result<Option<Location>>> + Send {
        async move { db.delete_location(ctx, conn, ac, id).await.map(Some) }
    }
}
