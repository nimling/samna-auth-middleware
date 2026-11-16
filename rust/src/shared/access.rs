use std::collections::HashMap;

use serde::Deserialize;
use serde_json::Value;
use uuid::Uuid;

use super::db::Db;
use super::error::{Error, Result};
use super::models::{
    ACCESS_ALL, AccessClaim, AccessUser, ClaimAccess, ClaimObject, Model, OBJECT_TYPE_ORGANIZATION,
    OBJECT_TYPE_USER,
};
use crate::client::Ctx;

pub fn self_access_claim(user_id: Uuid, required_access: ClaimAccess) -> AccessClaim {
    AccessClaim {
        user_object_id: user_id,
        user_id,
        access: required_access,
        owner_id: user_id,
        root_id: user_id,
        direct_grant: true,
        ..AccessClaim::default()
    }
}

const MUTATION_ACCESS_QUERY: &str =
    "SELECT * FROM claimius.mutation_access($1::uuid, $2::text, $3::text, $4::int)";

#[derive(Clone, Debug)]
pub struct AccessRequest {
    pub user_id: Uuid,
    pub object_id: Uuid,
    pub object_type: String,
    pub required_access: ClaimAccess,
}

#[derive(Clone, Debug)]
pub struct AccessClaimSetEntry {
    pub request: AccessRequest,
    pub claim: Option<AccessClaim>,
}

pub type AccessClaimSet = Vec<AccessClaimSetEntry>;

pub trait AccessClaimSetLookup {
    fn get(&self, object_type: &str, object_id: Uuid, user_id: Uuid) -> Option<&AccessClaim>;
    fn has(
        &self,
        object_type: &str,
        object_id: Uuid,
        user_id: Uuid,
        required: Option<ClaimAccess>,
    ) -> bool;
}

impl AccessClaimSetLookup for AccessClaimSet {
    fn get(&self, object_type: &str, object_id: Uuid, user_id: Uuid) -> Option<&AccessClaim> {
        self.iter()
            .find(|e| {
                e.request.user_id == user_id
                    && e.request.object_type == object_type
                    && e.request.object_id == object_id
            })
            .and_then(|e| e.claim.as_ref())
    }

    fn has(
        &self,
        object_type: &str,
        object_id: Uuid,
        user_id: Uuid,
        required: Option<ClaimAccess>,
    ) -> bool {
        let Some(claim) = self.get(object_type, object_id, user_id) else {
            return false;
        };
        match required {
            None => true,
            Some(mask) => claim.access.has(mask),
        }
    }
}

fn gate_error(err: Error) -> Error {
    match err.pg_code() {
        Some("P0404") => Error::not_found(),
        Some("P0403") => Error::no_access(),
        _ if err.is_no_rows() => Error::no_access(),
        _ => Error::internal(),
    }
}

impl Db {
    pub async fn get_access(
        &self,
        ctx: &Ctx,
        user_id: Uuid,
        object_id: Uuid,
        object_type: &str,
        required_access: ClaimAccess,
    ) -> Result<AccessClaim> {
        if object_type == OBJECT_TYPE_USER && user_id == object_id {
            return Ok(self_access_claim(user_id, required_access));
        }
        let claims = self
            .prepared::<AccessClaim>(
                ctx,
                false,
                "SELECT * FROM claimius.get_access($1::uuid, $2::uuid, $3::text, $4::int)",
                &[&user_id, &object_id, &object_type, &required_access.0],
            )
            .await
            .map_err(|e| {
                if e.is_no_rows() {
                    Error::no_access()
                } else {
                    Error::internal()
                }
            })?;
        let Some(mut claim) = claims.into_iter().next() else {
            return Err(Error::no_access());
        };
        self.usage.add(user_id, object_type, object_id);
        crate::smw::telemetry::organization(claim.root_id, claim.owner_id);
        claim.user_id = user_id;
        Ok(claim)
    }

    pub async fn get_direct_access(
        &self,
        ctx: &Ctx,
        user_id: Uuid,
        object_id: Uuid,
        object_type: &str,
    ) -> Result<AccessClaim> {
        if object_type == OBJECT_TYPE_USER && user_id == object_id {
            return Ok(self_access_claim(user_id, ACCESS_ALL));
        }
        let claims = self
            .prepared::<AccessClaim>(
                ctx,
                false,
                "SELECT * FROM claimius.get_direct_access($1::uuid, $2::uuid, $3::text)",
                &[&user_id, &object_id, &object_type],
            )
            .await
            .map_err(|e| {
                if e.is_no_rows() {
                    Error::no_access()
                } else {
                    Error::internal()
                }
            })?;
        let Some(mut claim) = claims.into_iter().next() else {
            return Err(Error::no_access());
        };
        crate::smw::telemetry::organization(claim.root_id, claim.owner_id);
        claim.user_id = user_id;
        Ok(claim)
    }

    pub async fn has_access(
        &self,
        ctx: &Ctx,
        user_id: Uuid,
        object_id: Uuid,
        object_type: &str,
        required_access: ClaimAccess,
    ) -> bool {
        self.get_access(ctx, user_id, object_id, object_type, required_access)
            .await
            .is_ok()
    }

    pub async fn mutation_access(
        &self,
        ctx: &Ctx,
        user_id: Uuid,
        object_id: Uuid,
        object_type: &str,
        required_access: ClaimAccess,
    ) -> Result<AccessClaim> {
        if object_type == OBJECT_TYPE_USER && user_id == object_id {
            return Ok(self_access_claim(user_id, required_access));
        }
        let object = object_id.to_string();
        let claims = self
            .prepared::<AccessClaim>(
                ctx,
                false,
                MUTATION_ACCESS_QUERY,
                &[&user_id, &object, &object_type, &required_access.0],
            )
            .await
            .map_err(gate_error)?;
        let Some(mut claim) = claims.into_iter().next() else {
            return Err(Error::no_access());
        };
        self.usage.add(user_id, object_type, object_id);
        crate::smw::telemetry::organization(claim.root_id, claim.owner_id);
        claim.user_id = user_id;
        Ok(claim)
    }

    pub async fn mutation_access_many(
        &self,
        ctx: &Ctx,
        requests: &[AccessRequest],
    ) -> Result<AccessClaimSet> {
        if requests.is_empty() {
            return Ok(AccessClaimSet::new());
        }
        let user_ids: Vec<Uuid> = requests.iter().map(|r| r.user_id).collect();
        let object_ids: Vec<Uuid> = requests.iter().map(|r| r.object_id).collect();
        let object_types: Vec<String> = requests.iter().map(|r| r.object_type.clone()).collect();
        let required: Vec<i32> = requests.iter().map(|r| r.required_access.0).collect();
        #[derive(Deserialize)]
        struct BatchRow {
            idx: i64,
            #[serde(flatten)]
            rest: Value,
        }
        let sql = "WITH inputs AS (
				SELECT user_id, object_id, object_type, required_access,
				       row_number() OVER () AS idx
				FROM unnest($1::uuid[], $2::uuid[], $3::text[], $4::int[]) AS u(user_id, object_id, object_type, required_access)
			)
			SELECT
				inputs.idx,
				g.user_object_id,
				g.user_claim_id,
				g.claim_id,
				g.claim_name,
				g.sa_access,
				g.scope,
				g.sa_owner_id,
				g.sa_root_id,
				g.sa_location_id,
				g.direct_grant,
				g.level
			FROM inputs
			LEFT JOIN LATERAL claimius.resolve_access(inputs.user_id, inputs.object_id::text, inputs.object_type, inputs.required_access) g ON TRUE
			ORDER BY inputs.idx";
        let mut set: AccessClaimSet = requests
            .iter()
            .map(|r| AccessClaimSetEntry {
                request: r.clone(),
                claim: None,
            })
            .collect();
        let conn = self.conn(ctx).await?;
        let rows = match conn
            .query_values::<BatchRow>(sql, &[&user_ids, &object_ids, &object_types, &required])
            .await
        {
            Ok(rows) => rows,
            Err(e) if e.is_no_rows() => return Ok(set),
            Err(_) => return Err(Error::internal()),
        };
        for row in rows {
            let idx = row.idx - 1;
            if idx < 0 || idx as usize >= requests.len() {
                continue;
            }
            let idx = idx as usize;
            let Ok(mut ac) = AccessClaim::from_row(row.rest) else {
                continue;
            };
            if ac.user_object_id.is_nil() {
                continue;
            }
            ac.user_id = requests[idx].user_id;
            set[idx].claim = Some(ac);
            self.usage.add(
                requests[idx].user_id,
                &requests[idx].object_type,
                requests[idx].object_id,
            );
        }
        Ok(set)
    }

    pub async fn scope_access(
        &self,
        ctx: &Ctx,
        user_id: Uuid,
        scope_path: &str,
        required_access: ClaimAccess,
    ) -> Result<Vec<AccessClaim>> {
        #[derive(Deserialize)]
        struct ScopeAccessRow {
            user_claim_id: Uuid,
            claim_id: Uuid,
            claim_name: String,
            sa_access: ClaimAccess,
            sa_owner_id: Uuid,
            sa_root_id: Uuid,
            object_row_id: Uuid,
            #[serde(default)]
            object_scope: Option<Value>,
            #[serde(default)]
            object_access: Option<ClaimAccess>,
            #[serde(default)]
            object_inherits: bool,
            #[serde(default)]
            level: i32,
        }
        let conn = self.read_conn(ctx).await?;
        let rows = match conn
            .query_values::<ScopeAccessRow>(
                "SELECT * FROM claimius.get_scope_access($1::uuid, $2::jsonpath, $3::int)",
                &[&user_id, &scope_path, &required_access.0],
            )
            .await
        {
            Ok(rows) => rows,
            Err(e) if e.is_no_rows() => return Ok(Vec::new()),
            Err(_) => return Err(Error::internal()),
        };
        let mut order: Vec<Uuid> = Vec::new();
        let mut by_claim: HashMap<Uuid, AccessClaim> = HashMap::new();
        for r in rows {
            let ac = by_claim.entry(r.claim_id).or_insert_with(|| {
                order.push(r.claim_id);
                AccessClaim {
                    user_claim_id: Some(r.user_claim_id),
                    claim_id: Some(r.claim_id),
                    claim_name: Some(r.claim_name.clone()),
                    access: r.sa_access,
                    owner_id: r.sa_owner_id,
                    root_id: r.sa_root_id,
                    level: r.level,
                    ..AccessClaim::default()
                }
            });
            if !ac.objects.is_empty() {
                ac.access |= r.sa_access;
            }
            ac.objects.push(ClaimObject {
                id: r.object_row_id,
                claim_id: r.claim_id,
                scope: r.object_scope,
                access: r.object_access,
                inherits: r.object_inherits,
                owner_id: r.sa_owner_id,
                root_id: r.sa_root_id,
                ..ClaimObject::default()
            });
        }
        Ok(order
            .into_iter()
            .filter_map(|id| by_claim.remove(&id))
            .collect())
    }

    pub async fn user_access(
        &self,
        ctx: &Ctx,
        actor_id: Uuid,
        target: &str,
        required_access: ClaimAccess,
    ) -> Result<AccessUser> {
        let users = self
            .prepared::<AccessUser>(
                ctx,
                false,
                "SELECT * FROM claimius.get_user_access($1::uuid, $2::text, $3::int)",
                &[&actor_id, &target, &required_access.0],
            )
            .await
            .map_err(|e| {
                if e.is_no_rows() {
                    Error::no_access()
                } else {
                    Error::internal()
                }
            })?;
        let Some(user) = users.into_iter().next() else {
            let gate = self
                .prepared::<AccessClaim>(
                    ctx,
                    false,
                    MUTATION_ACCESS_QUERY,
                    &[&actor_id, &target, &OBJECT_TYPE_USER, &required_access.0],
                )
                .await;
            if let Err(e) = gate
                && e.pg_code() == Some("P0404")
            {
                return Err(Error::not_found());
            }
            return Err(Error::no_access());
        };
        self.usage
            .add(actor_id, OBJECT_TYPE_USER, user.target_user_id);
        Ok(user)
    }

    pub async fn root_access(
        &self,
        ctx: &Ctx,
        user_id: Uuid,
        required_access: ClaimAccess,
    ) -> Result<AccessClaim> {
        let sql = "SELECT claim.*
			FROM (
				SELECT DISTINCT c.sa_root_id
				FROM claimius.user_claim uc
				JOIN claimius.claim c ON c.id = uc.claim_id
				JOIN claimius.organization o ON o.id = c.sa_root_id AND o.sa_deleted_at IS NULL
				WHERE uc.user_id = $1::uuid
				  AND uc.sa_deleted_at IS NULL
				  AND c.sa_deleted_at IS NULL
			) root
			CROSS JOIN LATERAL claimius.get_access($1::uuid, root.sa_root_id, $2::text, $3::int) AS claim
			ORDER BY root.sa_root_id
			LIMIT 1";
        let claims = self
            .prepared::<AccessClaim>(
                ctx,
                false,
                sql,
                &[&user_id, &OBJECT_TYPE_ORGANIZATION, &required_access.0],
            )
            .await
            .map_err(|e| {
                if e.is_no_rows() {
                    Error::no_access()
                } else {
                    Error::internal()
                }
            })?;
        let Some(mut claim) = claims.into_iter().next() else {
            return Err(Error::no_access());
        };
        crate::smw::telemetry::organization(claim.root_id, claim.owner_id);
        claim.user_id = user_id;
        Ok(claim)
    }

    pub async fn post_access_diff_drain(&self, ctx: &Ctx, limit_pairs: i32) -> Result<i32> {
        #[derive(Deserialize)]
        struct Drained {
            settled: i32,
            #[serde(default)]
            failures: Vec<String>,
        }
        let conn = self.conn(ctx).await?;
        let rows = conn
            .query_values::<Drained>(
                "SELECT * FROM claimius.drain_access_diff($1)",
                &[&limit_pairs],
            )
            .await?;
        let Some(drained) = rows.into_iter().next() else {
            return Ok(0);
        };
        if !drained.failures.is_empty() {
            return Err(Error::other(format!(
                "drain: {}",
                drained.failures.join("; ")
            )));
        }
        Ok(drained.settled)
    }
}
