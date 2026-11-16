use serde_json::Value;
use uuid::Uuid;

use super::db::{Conn, Db};
use super::error::{Error, Result};
use super::models::{ACCESS_READ, OBJECT_TYPE_ORGANIZATION, SamnaUser};
use crate::client::{self, Ctx};
use crate::utils::{DebugMode, get_debug_mode};

pub async fn write_audit(
    conn: &Conn,
    app_id: Option<Uuid>,
    operation: &str,
    audit_type: &str,
    object_type: &str,
    object_id: Uuid,
    owner_id: Uuid,
    created_by: Option<Uuid>,
    message: &str,
) -> Result<()> {
    match app_id {
        None => {
            conn.execute(
                "SELECT FROM claimius.insert_audit($1, $2, $3, $4, $5, $6, $7)",
                &[
                    &operation,
                    &audit_type,
                    &object_type,
                    &object_id,
                    &owner_id,
                    &created_by,
                    &message,
                ],
            )
            .await?;
        }
        Some(app) => {
            conn.execute(
                "SELECT FROM claimius.insert_audit($1, $2, $3, $4, $5, $6, $7, $8)",
                &[
                    &app,
                    &operation,
                    &audit_type,
                    &object_type,
                    &object_id,
                    &owner_id,
                    &created_by,
                    &message,
                ],
            )
            .await?;
        }
    }
    Ok(())
}

fn unverified_claims(token: &str) -> Result<Value> {
    let mut parts = token.split('.');
    let (Some(_), Some(payload)) = (parts.next(), parts.next()) else {
        return Err(Error::other("parse token: token has no payload"));
    };
    use base64::Engine;
    let raw = base64::engine::general_purpose::URL_SAFE_NO_PAD
        .decode(payload)
        .map_err(|e| Error::other(format!("parse token: {e}")))?;
    serde_json::from_slice(&raw).map_err(|e| Error::other(format!("parse token: {e}")))
}

impl Db {
    pub async fn validate_token(
        &self,
        ctx: &Ctx,
        token: &str,
    ) -> Result<(Option<SamnaUser>, Uuid)> {
        if token.is_empty() {
            return Err(Error::other("token is required"));
        }
        if get_debug_mode() != DebugMode::None {
            return self.validate_token_debug(ctx, token).await;
        }
        let body = serde_json::json!({ "token": token });
        let user: SamnaUser = client::post(ctx, "user/token/validate", &body).await?;
        match user.owner_id {
            Some(owner) if !owner.is_nil() => Ok((Some(user), owner)),
            _ => Err(Error::other("token validation carried no owner")),
        }
    }

    async fn validate_token_debug(
        &self,
        ctx: &Ctx,
        token: &str,
    ) -> Result<(Option<SamnaUser>, Uuid)> {
        let claims = unverified_claims(token)?;
        let conn = self.conn(ctx).await?;
        let rows = conn
            .query_json(
                "SELECT t.user_id, t.sa_owner_id AS owner_id FROM claimius.user_identity t WHERE t.sa_deleted_at IS NULL AND t.external_id = ($1::jsonb ->> t.id_claim) ORDER BY t.sa_created_at DESC LIMIT 1",
                &[&claims],
            )
            .await
            .map_err(|e| Error::other(format!("debug validate token: {e}")))?;
        let Some(row) = rows.first() else {
            return Ok((None, Uuid::nil()));
        };
        let user_id: Uuid = serde_json::from_value(row["user_id"].clone())?;
        let owner_id: Uuid = serde_json::from_value(row["owner_id"].clone())?;
        match self.get_user(ctx, user_id, user_id).await {
            Ok(user) => Ok((Some(user), owner_id)),
            Err(e) if e.is_no_rows() => Ok((None, Uuid::nil())),
            Err(e) => Err(Error::other(format!("debug fetch samna_user: {e}"))),
        }
    }

    pub async fn resolve_external_user(
        &self,
        ctx: &Ctx,
        caller_id: Uuid,
        external_id: &str,
    ) -> Result<Option<SamnaUser>> {
        if external_id.is_empty() {
            return Err(Error::other("external_id is required"));
        }
        if self.route_to_prophet {
            let body = serde_json::json!({ "external_id": external_id });
            let user: SamnaUser = match client::post(ctx, "user/token/validate", &body).await {
                Ok(user) => user,
                Err(e) if e.is(client::ErrorKind::Unauthorized) => return Ok(None),
                Err(e) => return Err(Error::other(format!("resolve external user: {e}"))),
            };
            let Some(owner) = user.owner_id.filter(|o| !o.is_nil()) else {
                return Ok(None);
            };
            if caller_id.is_nil()
                || !self
                    .has_access(ctx, caller_id, owner, OBJECT_TYPE_ORGANIZATION, ACCESS_READ)
                    .await
            {
                return Ok(None);
            }
            return Ok(Some(user));
        }
        let conn = self.conn(ctx).await?;
        let rows = conn
            .query_json(
                "SELECT t.user_id, t.sa_owner_id AS owner_id FROM claimius.user_identity t WHERE t.type = 'external' AND t.external_id = $1 AND t.sa_deleted_at IS NULL ORDER BY t.sa_created_at DESC LIMIT 1",
                &[&external_id],
            )
            .await
            .map_err(|e| Error::other(format!("resolve external user: {e}")))?;
        let Some(row) = rows.first() else {
            return Ok(None);
        };
        let user_id: Uuid = serde_json::from_value(row["user_id"].clone())?;
        let owner_id: Uuid = serde_json::from_value(row["owner_id"].clone())?;
        if owner_id.is_nil()
            || caller_id.is_nil()
            || !self
                .has_access(
                    ctx,
                    caller_id,
                    owner_id,
                    OBJECT_TYPE_ORGANIZATION,
                    ACCESS_READ,
                )
                .await
        {
            return Ok(None);
        }
        match self.get_user(ctx, user_id, user_id).await {
            Ok(user) => Ok(Some(user)),
            Err(e) if e.is_no_rows() => Ok(None),
            Err(e) => Err(Error::other(format!("fetch samna_user: {e}"))),
        }
    }
}
