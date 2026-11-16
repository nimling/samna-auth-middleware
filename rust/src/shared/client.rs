use uuid::Uuid;

use super::db::Db;
use super::error::{Error, Result};
use super::models::{SamnaClient, UserClaim};
use crate::client::Ctx;

impl Db {
    pub async fn get_client(&self, ctx: &Ctx, client_id: Uuid) -> Result<SamnaClient> {
        let conn = self.read_conn(ctx).await?;
        conn.query_model::<SamnaClient>("SELECT * FROM claimius.samna_client WHERE client_id = $1 AND sa_deleted_at IS NULL AND status = 'active' AND (expires_at IS NULL OR expires_at > now()) LIMIT 1", &[&client_id])
            .await
            .map_err(|e| Error::other(format!("get client: {e}")))
    }

    pub async fn get_client_claims(&self, ctx: &Ctx, client_id: Uuid) -> Result<Vec<UserClaim>> {
        self.query_models::<UserClaim>(
            ctx,
            "SELECT * FROM claimius.user_claim WHERE user_id = $1 AND sa_deleted_at IS NULL AND (starts_at IS NULL OR starts_at <= CURRENT_TIMESTAMP) AND (ends_at IS NULL OR ends_at >= CURRENT_TIMESTAMP)",
            &[&client_id],
        )
        .await
        .map_err(|e| Error::other(format!("get client claims: {e}")))
    }
}
