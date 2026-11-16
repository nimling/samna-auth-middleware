use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use uuid::Uuid;

use super::cipher::{aes_decrypt, aes_encrypt};
use super::db::{Conn, Db};
use super::error::{Error, Result};
use super::models::*;
use super::query::{EndpointSpec, Write, stamp_access_claim};
use crate::client::{self, Ctx};

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct BodySecret {
    pub key: Option<String>,
    pub value: Option<String>,
    pub owner_id: Option<Uuid>,
    pub expires_at: Option<DateTime<Utc>>,
}

impl Db {
    pub async fn get_secrets(
        &self,
        ctx: &Ctx,
        user_id: Uuid,
        required_access: ClaimAccess,
        after: Option<Uuid>,
        limit: i32,
    ) -> Result<Vec<SamnaSecret>> {
        let page_limit: Option<i32> = if limit > 0 { Some(limit) } else { None };
        self.query_models::<SamnaSecret>(
            ctx,
            "SELECT * FROM claimius.get_secrets($1, $2, $3, $4)",
            &[&user_id, &required_access.0, &after, &page_limit],
        )
        .await
    }

    pub async fn get_secret(&self, ctx: &Ctx, secret_id: Uuid) -> Result<SamnaSecret> {
        self.query_model::<SamnaSecret>(
            ctx,
            "SELECT * FROM claimius.samna_secret WHERE id = $1 AND sa_deleted_at IS NULL LIMIT 1",
            &[&secret_id],
        )
        .await
    }

    pub(crate) async fn insert_secret(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        body: BodySecret,
    ) -> Result<SamnaSecret> {
        let Some(uc) = ac.and_then(|a| a.user_claim_id) else {
            return Err(Error::no_access());
        };
        let app = self.get_app(ctx).await?;
        let seed = hex::decode(&app.private_seed)
            .map_err(|e| Error::other(format!("decode app seed: {e}")))?;
        let value = body.value.clone().unwrap_or_default();
        let encrypted =
            aes_encrypt(&value, &seed).map_err(|e| Error::other(format!("encrypt: {e}")))?;
        let held = self.conn_or(ctx, conn).await?;
        let key = body.key.clone().unwrap_or_default();
        let owner = body.owner_id.unwrap_or_default();
        let secret_id: Uuid = held
            .query_scalar::<Uuid>("SELECT (claimius.insert_secret($1, $2, $3, $4, $5) -> 'samna_secret' ->> 'id')::uuid", &[&key, &hex::encode(encrypted), &body.expires_at, &owner, &uc])
            .await
            .map_err(|e| Error::other(format!("insert_secret: {e}")))?;
        held.query_model::<SamnaSecret>(
            "SELECT * FROM claimius.samna_secret WHERE id = $1 AND sa_deleted_at IS NULL LIMIT 1",
            &[&secret_id],
        )
        .await
    }

    pub(crate) async fn update_secret(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        secret_id: Uuid,
        body: BodySecret,
    ) -> Result<SamnaSecret> {
        if ac.is_none() {
            return Err(Error::no_access());
        }
        let held = self.conn_or(ctx, conn).await?;
        let mut secret = held.query_model::<SamnaSecret>("SELECT * FROM claimius.samna_secret WHERE id = $1 AND sa_deleted_at IS NULL LIMIT 1", &[&secret_id]).await?;
        if let Some(key) = body.key {
            secret.key = key;
        }
        if let Some(value) = body.value {
            let app = self.get_app(ctx).await?;
            let seed = hex::decode(&app.private_seed)
                .map_err(|e| Error::other(format!("decode app seed: {e}")))?;
            let encrypted =
                aes_encrypt(&value, &seed).map_err(|e| Error::other(format!("encrypt: {e}")))?;
            secret.value = hex::encode(encrypted);
        }
        if body.expires_at.is_some() {
            secret.expires_at = body.expires_at;
        }
        let row = super::query::update_generic::<SamnaSecret>(
            &held,
            &secret,
            secret_id,
            &[],
            &[],
            true,
            &[],
        )
        .await
        .map_err(|e| Error::other(format!("update secret: {e}")))?;
        let body_keys = secret.body.clone();
        let mut updated = SamnaSecret::from_row(row)?;
        updated.body = body_keys;
        Ok(updated)
    }

    pub(crate) async fn delete_secret(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        secret_id: Uuid,
    ) -> Result<SamnaSecret> {
        if ac.is_none() {
            return Err(Error::no_access());
        }
        let held = self.conn_or(ctx, conn).await?;
        held.query_model::<SamnaSecret>("UPDATE claimius.samna_secret SET sa_deleted_at = NOW() WHERE id = $1 AND sa_deleted_at IS NULL RETURNING *", &[&secret_id]).await
    }

    pub async fn resolve_secret(
        &self,
        ctx: &Ctx,
        s: &SamnaSecret,
        user_id: Uuid,
    ) -> Result<String> {
        if let Some(expires) = s.expires_at
            && expires <= Utc::now()
        {
            return Err(Error::SecretExpired);
        }
        let value: Option<String> = self
            .query_scalar_opt::<String>(
                ctx,
                "SELECT claimius.get_secret_value($1, $2)",
                &[&user_id, &s.id],
            )
            .await
            .map_err(|e| Error::other(format!("get_secret_value: {e}")))?;
        let Some(value) = value else {
            return Err(Error::no_access());
        };
        let ciphertext = hex::decode(&value).map_err(|e| Error::other(e.to_string()))?;
        let mut attempt = 0;
        loop {
            let app = self.get_app(ctx).await?;
            let seed = hex::decode(&app.private_seed).map_err(|e| Error::other(e.to_string()))?;
            match aes_decrypt(&ciphertext, &seed) {
                Ok(plain) => return Ok(plain),
                Err(e) if !self.route_to_prophet || attempt > 0 => return Err(e),
                Err(_) => {
                    client::delete_app_seed();
                    attempt += 1;
                }
            }
        }
    }
}

impl Write for SamnaSecret {
    type Result = SamnaSecret;

    fn endpoint() -> Option<EndpointSpec> {
        Some(EndpointSpec {
            base: "secret",
            parents: &[],
        })
    }

    fn stamp(&mut self, ac: &AccessClaim) {
        stamp_access_claim(&mut self.created_by, &mut self.owner_id, ac);
    }

    fn replace_with(&mut self, result: &SamnaSecret) {
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
    ) -> impl std::future::Future<Output = Result<Option<(SamnaSecret, bool)>>> + Send {
        async move {
            let body = BodySecret {
                key: Some(self.key.clone()),
                value: Some(self.value.clone()),
                owner_id: Some(self.owner_id),
                expires_at: self.expires_at,
            };
            db.insert_secret(ctx, conn, ac, body)
                .await
                .map(|s| Some((s, false)))
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
    ) -> impl std::future::Future<Output = Result<Option<SamnaSecret>>> + Send {
        async move {
            let body = BodySecret {
                key: Some(self.key.clone()),
                value: Some(self.value.clone()),
                owner_id: Some(self.owner_id),
                expires_at: self.expires_at,
            };
            db.update_secret(ctx, conn, ac, id, body).await.map(Some)
        }
    }

    fn delete_local(
        &mut self,
        db: &Db,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        id: Uuid,
    ) -> impl std::future::Future<Output = Result<Option<SamnaSecret>>> + Send {
        async move { db.delete_secret(ctx, conn, ac, id).await.map(Some) }
    }
}

impl Write for SamnaClient {
    type Result = SamnaClient;

    fn endpoint() -> Option<EndpointSpec> {
        Some(EndpointSpec {
            base: "samna_client",
            parents: &[],
        })
    }

    fn stamp(&mut self, ac: &AccessClaim) {
        stamp_access_claim(&mut self.created_by, &mut self.owner_id, ac);
    }

    fn replace_with(&mut self, result: &SamnaClient) {
        let body = self.body.clone();
        *self = result.clone();
        self.body = body;
    }
}
