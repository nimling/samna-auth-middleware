use serde::Deserialize;
use uuid::Uuid;

use super::db::{Conn, Db};
use super::error::{Error, Result};
use super::models::*;
use super::query::{EndpointSpec, Write, params_ref, stamp_access_claim};
use crate::client::Ctx;

impl Db {
    pub async fn get_organization(&self, ctx: &Ctx, org_id: Uuid) -> Result<Organization> {
        self.query_model::<Organization>(ctx, "SELECT organization.* FROM claimius.organization WHERE organization.id = $1 AND organization.sa_deleted_at IS NULL LIMIT 1", &[&org_id]).await
    }

    pub async fn get_organizations(
        &self,
        ctx: &Ctx,
        user_id: Uuid,
        required_access: ClaimAccess,
        after: Option<Uuid>,
        limit: i32,
    ) -> Result<Vec<Organization>> {
        let w = super::query::object_where(
            "organization",
            user_id,
            OBJECT_TYPE_ORGANIZATION,
            required_access,
            None,
            after,
            limit,
            1,
        );
        let order = if w.order_sql.is_empty() {
            "organization.id".to_string()
        } else {
            w.order_sql.clone()
        };
        let sql = format!(
            "SELECT organization.* FROM claimius.organization WHERE organization.sa_deleted_at IS NULL AND {} ORDER BY {order}",
            w.where_sql
        );
        let params = params_ref(&w.params);
        self.query_models::<Organization>(ctx, &sql, &params).await
    }

    pub async fn get_child_organizations(&self, ctx: &Ctx, start_id: Uuid) -> Result<Vec<Uuid>> {
        let conn = self.read_conn(ctx).await?;
        conn.query_scalars::<Uuid>(
            "SELECT object_id FROM claimius.get_subtree('ownership', (SELECT sa_root_id FROM claimius.organization WHERE id = $1 AND sa_deleted_at IS NULL), $2, $1) WHERE object_type = $2 AND object_id <> $1",
            &[&start_id, &OBJECT_TYPE_ORGANIZATION],
        )
        .await
    }

    pub async fn get_parent_organizations(&self, ctx: &Ctx, start_id: Uuid) -> Result<Vec<Uuid>> {
        let conn = self.read_conn(ctx).await?;
        conn.query_scalars::<Uuid>(
            "SELECT object_id FROM claimius.get_owner_ancestors($1, $2) WHERE hop > 0",
            &[&OBJECT_TYPE_ORGANIZATION, &start_id],
        )
        .await
    }

    pub(crate) async fn insert_organization(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        org: &mut Organization,
    ) -> Result<Organization> {
        let Some(ac) = ac else {
            return Err(Error::no_access());
        };
        let (Some(claim_id), Some(user_claim_id)) = (ac.claim_id, ac.user_claim_id) else {
            return Err(Error::no_access());
        };
        org.created_by = user_claim_id;
        let binding_owner = if org.owner_id.is_nil() {
            ac.owner_id
        } else {
            org.owner_id
        };
        let held = self.conn_or(ctx, conn).await?;
        let own_tx = conn.is_none();
        if own_tx {
            held.begin().await?;
        }
        let written: Result<Organization> = async {
            let row = super::query::insert_generic::<Organization>(&held, org).await?;
            let inserted = Organization::from_row(row)?;
            if inserted.owner_id == inserted.id {
                return Ok(inserted);
            }
            let binding_root: Uuid = held.query_scalar("SELECT sa_root_id FROM claimius.organization WHERE id = $1 AND sa_deleted_at IS NULL", &[&binding_owner]).await?;
            let access = (ACCESS_OWNER | ACCESS_WRITE | ACCESS_READ).0;
            held.execute(
                "SELECT claimius.insert_claim_object(
				p_claim_id      := $1,
				p_sa_owner_id   := $2,
				p_sa_root_id    := $3,
				p_sa_created_by := $4,
				p_object_id     := $5,
				p_object_type   := 'claimius.organization',
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
                let body = org.body.clone();
                *org = inserted;
                org.body = body;
                Ok(org.clone())
            }
            Err(e) => {
                if own_tx {
                    let _ = held.rollback().await;
                }
                Err(e)
            }
        }
    }

    pub async fn insert_root_organization(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: &AccessClaim,
        org: &mut Organization,
    ) -> Result<Organization> {
        let Some(user_claim_id) = ac.user_claim_id else {
            return Err(Error::no_access());
        };
        org.id = Uuid::new_v4();
        org.owner_id = org.id;
        org.root_id = org.id;
        org.created_by = user_claim_id;
        org.level = 0;
        let held = self.conn_or(ctx, conn).await?;
        self.new_insert(org)
            .conn(&held)
            .access(ac)
            .scan(ctx)
            .await?;
        Ok(org.clone())
    }

    pub(crate) async fn validate_organization_parent(
        &self,
        ctx: &Ctx,
        ac: Option<&AccessClaim>,
        org_id: Uuid,
        owner_id: Uuid,
    ) -> Result<()> {
        if owner_id.is_nil() {
            return Ok(());
        }
        #[derive(Deserialize)]
        struct Parent {
            moved: bool,
            allowed: bool,
        }
        let rows = self
            .query_values::<Parent>(
                ctx,
                "SELECT o.sa_owner_id <> p.id AS moved,
			       o.sa_root_id = p.sa_root_id
			       AND NOT EXISTS (
			           SELECT 1 FROM claimius.get_owner_ancestors($1, p.id) a WHERE a.object_id = o.id
			       ) AS allowed
			FROM claimius.organization o, claimius.organization p
			WHERE o.id = $2 AND o.sa_deleted_at IS NULL
			  AND p.id = $3 AND p.sa_deleted_at IS NULL",
                &[&OBJECT_TYPE_ORGANIZATION, &org_id, &owner_id],
            )
            .await?;
        let Some(parent) = rows.into_iter().next() else {
            return Err(Error::not_found());
        };
        if !parent.moved {
            return Ok(());
        }
        if !parent.allowed {
            return Err(Error::OrganizationParent);
        }
        match ac {
            Some(ac)
                if self
                    .has_access(
                        ctx,
                        ac.user_id,
                        owner_id,
                        OBJECT_TYPE_ORGANIZATION,
                        ACCESS_WRITE,
                    )
                    .await =>
            {
                Ok(())
            }
            _ => Err(Error::no_access()),
        }
    }

    pub(crate) async fn update_organization(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        org_id: Uuid,
        org: &mut Organization,
    ) -> Result<Organization> {
        if ac.is_none() {
            return Err(Error::no_access());
        }
        self.validate_organization_parent(ctx, ac, org_id, org.owner_id)
            .await?;
        let held = self.conn_or(ctx, conn).await?;
        let nulls = patch_nulls(&org.body, Organization::COLUMNS, &org_null_fields(org));
        let row = super::query::update_generic::<Organization>(
            &held,
            org,
            org_id,
            &[],
            &["id", "sa_root_id", "sa_created_by", "provider_id"].map(str::to_string),
            true,
            &nulls,
        )
        .await?;
        let body = org.body.clone();
        *org = Organization::from_row(row)?;
        org.body = body;
        Ok(org.clone())
    }

    pub(crate) async fn delete_organization(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        org_id: Uuid,
    ) -> Result<Organization> {
        if ac.is_none() {
            return Err(Error::no_access());
        }
        let held = self.conn_or(ctx, conn).await?;
        let org_type: Option<String> = held.query_scalar_opt::<String>("SELECT type::text FROM claimius.organization WHERE id = $1 AND sa_deleted_at IS NULL", &[&org_id]).await?;
        let Some(org_type) = org_type.or(Some(String::new())) else {
            return Err(Error::not_found());
        };
        let exists: bool = held.query_scalar::<bool>("SELECT EXISTS (SELECT 1 FROM claimius.organization WHERE id = $1 AND sa_deleted_at IS NULL)", &[&org_id]).await?;
        if !exists {
            return Err(Error::not_found());
        }
        if org_type == ORGANIZATION_TYPE_CUSTOMER {
            return Err(Error::OrganizationCustomer);
        }
        held.query_model::<Organization>("UPDATE claimius.organization SET sa_deleted_at = NOW() WHERE id = $1 AND sa_deleted_at IS NULL RETURNING *", &[&org_id]).await
    }

    pub async fn get_organization_audit(
        &self,
        ctx: &Ctx,
        actor_id: Uuid,
        org_id: Uuid,
        required_access: ClaimAccess,
    ) -> Result<Vec<Audit>> {
        self.query_models::<Audit>(
            ctx,
            "SELECT audit.* FROM claimius.get_audit($1, $2) AS audit WHERE audit.object_id = $3 AND audit.object_type = $4 ORDER BY audit.sa_created_at DESC",
            &[&actor_id, &required_access.0, &org_id, &OBJECT_TYPE_ORGANIZATION],
        )
        .await
    }

    pub async fn get_organizations_many(
        &self,
        ctx: &Ctx,
        ids: &[Uuid],
        owner_ids: &[Uuid],
        external_ids: &[String],
    ) -> Result<Vec<Organization>> {
        let ids = (!ids.is_empty()).then_some(ids);
        let owner_ids = (!owner_ids.is_empty()).then_some(owner_ids);
        let external_ids = (!external_ids.is_empty()).then_some(external_ids);
        self.query_models::<Organization>(
            ctx,
            "SELECT * FROM claimius.get_organizations($1::uuid[], $2::uuid[], $3::text[])",
            &[&ids, &owner_ids, &external_ids],
        )
        .await
    }
}

pub fn patch_nulls(
    body: &super::bind::Body,
    columns: &[(&str, &str)],
    null_fields: &[&str],
) -> Vec<String> {
    let mut nulls = Vec::new();
    for (column, json) in columns {
        if body.has_key(json) && null_fields.contains(json) {
            nulls.push(format!("\"{column}\" = NULL"));
        }
    }
    nulls
}

fn org_null_fields(org: &Organization) -> Vec<&'static str> {
    let mut out = Vec::new();
    if org.description.is_none() {
        out.push("description");
    }
    if org.kind.is_none() {
        out.push("kind");
    }
    if org.location_id.is_none() {
        out.push("location_id");
    }
    if org.external_id.is_none() {
        out.push("external_id");
    }
    out
}

impl Write for Organization {
    type Result = Organization;

    fn endpoint() -> Option<EndpointSpec> {
        Some(EndpointSpec {
            base: "organization",
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

    fn stamp_root(&mut self, root: Uuid) {
        if self.root_id.is_nil() {
            self.root_id = root;
        }
    }

    fn organization_owner(&self) -> Option<Uuid> {
        Some(self.owner_id)
    }

    fn replace_with(&mut self, result: &Organization) {
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
    ) -> impl std::future::Future<Output = Result<Option<(Organization, bool)>>> + Send {
        async move {
            db.insert_organization(ctx, conn, ac, self)
                .await
                .map(|o| Some((o, false)))
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
    ) -> impl std::future::Future<Output = Result<Option<Organization>>> + Send {
        async move {
            db.update_organization(ctx, conn, ac, id, self)
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
    ) -> impl std::future::Future<Output = Result<Option<Organization>>> + Send {
        async move { db.delete_organization(ctx, conn, ac, id).await.map(Some) }
    }
}
