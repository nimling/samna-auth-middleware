use std::sync::OnceLock;

use uuid::Uuid;

use crate::shared::db::Conn;
use crate::shared::{self, Ctx, Db, Error, Result};
use crate::utils::{DebugMode, get_debug_mode};

pub use crate::shared::access::{
    AccessClaimSet, AccessClaimSetEntry, AccessClaimSetLookup, AccessRequest,
};
pub use crate::shared::claims::{
    BindingFilters, CreateVirtualClaimParams, Descendant, GrantFilters, GraphWindow, PublicClaim,
    PublicClaims, delete_public_claim_cache,
};
pub use crate::shared::code::{
    ANONYMOUS_USER_ID, CODE_KIND_INVITE, ClaimOptions, ClaimOptionsSamk, Code, CodeAccess,
    CodeCreated, CodeKind,
};
pub use crate::shared::cost::{
    AppCostBucket, AppCostQuery, COST_GRANULARITY_DAY, COST_GRANULARITY_HOUR,
    COST_GRANULARITY_MONTH, COST_KIND_CREDIT, COST_KIND_FEE, COST_KIND_PERIOD, COST_KIND_USAGE,
    COST_SCHEMA, COST_SCHEMA_ID, COST_STATUS_ESTIMATED, COST_STATUS_INVOICED, COST_STATUS_SETTLED,
};
pub use crate::shared::models::*;
pub use crate::shared::profile::ProfileItem;
pub use crate::shared::query::{audit_where, claim_where, object_where, user_where};
pub use crate::shared::usage::ObjectUsageResult;
pub use crate::shared::user::OrganizationUserResult;

static INSTANCE: OnceLock<Db> = OnceLock::new();

pub async fn init() -> Result<&'static Db> {
    let mut d = shared::connect().await?;
    d.route_to_prophet = get_debug_mode() == DebugMode::None;
    d.get_public_claims(&Ctx::new(), Uuid::nil()).await?;
    if INSTANCE.set(d).is_err() {
        return Err(Error::other("sdb.Init called twice"));
    }
    Ok(INSTANCE.get().expect("instance"))
}

pub fn instance() -> Option<&'static Db> {
    INSTANCE.get()
}

pub fn need() -> Result<&'static Db> {
    INSTANCE.get().ok_or(Error::NotConnected)
}

pub fn get_app() -> Result<SamnaApp> {
    need()?.get_app_env()
}

pub async fn write(
    conn: &Conn,
    operation: &str,
    audit_type: &str,
    object_type: &str,
    object_id: Uuid,
    owner_id: Uuid,
    created_by: Option<Uuid>,
    message: &str,
) -> Result<()> {
    shared::write_audit(
        conn,
        None,
        operation,
        audit_type,
        object_type,
        object_id,
        owner_id,
        created_by,
        message,
    )
    .await
}

pub async fn put_app_cost(
    ctx: &Ctx,
    db: &Db,
    ac: &AccessClaim,
    input: &mut AppCostInput,
) -> Result<AppCost> {
    if ac.user_claim_id.is_none() {
        return Err(Error::no_access());
    }
    input.validate_post()?;
    db.new_insert(input).access(ac).scan(ctx).await
}

pub fn strip_object_schema(qualified: &str) -> &str {
    shared::models::strip_object_schema(qualified)
}
