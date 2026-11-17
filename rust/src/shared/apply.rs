use serde_json::{Map, Value};

use super::code::{Code, delete_code_cache};
use super::db::Conn;
use super::error::{Error, Result};
use super::models::*;
use super::query::{quote_table, recompute_external};
use crate::store;

pub async fn apply_event(conn: &Conn, operation: &str, event_type: &str, raw: Value) -> Result<()> {
    if event_type == "claimius.samna_app" {
        return Ok(());
    }
    let raw = strip_app_id(raw);
    if event_type == OBJECT_TYPE_CODE
        || event_type == OBJECT_TYPE_CLAIM
        || event_type == OBJECT_TYPE_USER_CLAIM
    {
        delete_code_cache();
        super::claims::delete_public_claim_cache();
    }
    match operation {
        "upsert" => {
            let changed = apply_upsert(conn, event_type, &raw).await?;
            if !changed {
                return Ok(());
            }
        }
        "delete" => apply_delete(conn, event_type, &raw).await?,
        other => return Err(Error::other(format!("unknown sync operation: {other}"))),
    }
    recompute_external(conn, event_type, &raw).await
}

fn strip_app_id(raw: Value) -> Value {
    match raw {
        Value::Object(mut m) => {
            m.remove("app_id");
            Value::Object(m)
        }
        other => other,
    }
}

async fn upsert_entity<T: Model>(conn: &Conn, raw: &Value) -> Result<bool> {
    let Value::Object(object) = raw else {
        return Err(Error::other("unmarshal: not an object"));
    };
    let entity: T =
        T::from_row(raw.clone()).map_err(|e| Error::other(format!("unmarshal: {e}")))?;
    let row = entity.to_row()?;
    let columns = T::present_columns(object);
    store::upsert(conn.client(), &quote_table(T::TABLE), &row, &columns).await
}

async fn apply_upsert(conn: &Conn, event_type: &str, raw: &Value) -> Result<bool> {
    match event_type {
        "claimius.samna_user" => upsert_entity::<SamnaUser>(conn, raw).await,
        "claimius.samna_client" => upsert_entity::<SamnaClient>(conn, raw).await,
        "claimius.samna_secret" => upsert_entity::<SamnaSecret>(conn, raw).await,
        "claimius.organization" => upsert_entity::<Organization>(conn, raw).await,
        "claimius.location" => upsert_entity::<Location>(conn, raw).await,
        "claimius.claim" => upsert_entity::<Claim>(conn, raw).await,
        "claimius.user_claim" => upsert_entity::<UserClaim>(conn, raw).await,
        "claimius.claim_object" => upsert_entity::<ClaimObject>(conn, raw).await,
        "claimius.object_field" => upsert_entity::<ObjectField>(conn, raw).await,
        "claimius.user_field" => upsert_entity::<UserField>(conn, raw).await,
        "claimius.user_relation" => upsert_entity::<UserRelation>(conn, raw).await,
        "claimius.object_asset" => upsert_entity::<ObjectAsset>(conn, raw).await,
        "claimius.app_cost" => upsert_entity::<AppCost>(conn, raw).await,
        "claimius.user_identity" => upsert_entity::<UserIdentity>(conn, raw).await,
        OBJECT_TYPE_CODE => upsert_entity::<Code>(conn, raw).await,
        "claimius.audit" => {
            let row: Audit = crate::sauth::unmarshal_value(raw.clone())
                .map_err(|e| Error::other(format!("unmarshal: {e}")))?;
            let mut values = row.to_row()?;
            values.retain(|_, v| !v.is_null());
            let columns: Vec<String> = values.keys().map(|c| format!("\"{c}\"")).collect();
            let sql = format!(
                "INSERT INTO claimius.audit ({cols}) SELECT {cols} FROM jsonb_populate_record(NULL::claimius.audit, $1::jsonb) ON CONFLICT DO NOTHING",
                cols = columns.join(", ")
            );
            let n = conn
                .client()
                .execute(&sql, &[&Value::Object(values)])
                .await?;
            Ok(n > 0)
        }
        other => Err(Error::other(format!(
            "unknown event_type for upsert: {other}"
        ))),
    }
}

fn id_of(raw: &Value) -> Result<String> {
    let id = raw
        .get("id")
        .ok_or_else(|| Error::other("unmarshal id envelope: no id"))?;
    match id {
        Value::String(s) => Ok(s.clone()),
        other => Err(Error::other(format!("unmarshal id: {other}"))),
    }
}

async fn apply_delete(conn: &Conn, event_type: &str, raw: &Value) -> Result<()> {
    let id = id_of(raw)?;
    let uuid =
        uuid::Uuid::parse_str(&id).map_err(|e| Error::other(format!("unmarshal id: {e}")))?;
    match event_type {
        "claimius.samna_user" => delete_entity(conn, SamnaUser::TABLE, uuid).await,
        "claimius.samna_client" => delete_entity(conn, SamnaClient::TABLE, uuid).await,
        "claimius.samna_secret" => delete_entity(conn, SamnaSecret::TABLE, uuid).await,
        "claimius.organization" => retire_entity(conn, Organization::TABLE, uuid).await,
        "claimius.location" => retire_entity(conn, Location::TABLE, uuid).await,
        "claimius.claim" => delete_entity(conn, Claim::TABLE, uuid).await,
        "claimius.user_claim" => delete_entity(conn, UserClaim::TABLE, uuid).await,
        "claimius.claim_object" => delete_entity(conn, ClaimObject::TABLE, uuid).await,
        "claimius.object_field" => delete_entity(conn, ObjectField::TABLE, uuid).await,
        "claimius.user_field" => delete_entity(conn, UserField::TABLE, uuid).await,
        "claimius.user_relation" => delete_entity(conn, UserRelation::TABLE, uuid).await,
        "claimius.object_asset" => delete_entity(conn, ObjectAsset::TABLE, uuid).await,
        "claimius.app_cost" => delete_entity(conn, AppCost::TABLE, uuid).await,
        "claimius.user_identity" => delete_entity(conn, UserIdentity::TABLE, uuid).await,
        OBJECT_TYPE_CODE => delete_entity(conn, Code::TABLE, uuid).await,
        other => Err(Error::other(format!(
            "delete not supported for event_type: {other}"
        ))),
    }
}

async fn delete_entity(conn: &Conn, table: &str, id: uuid::Uuid) -> Result<()> {
    let sql = format!("DELETE FROM {} WHERE id = $1", quote_table(table));
    conn.execute(&sql, &[&id])
        .await
        .map_err(|e| Error::other(format!("delete {table}: {e}")))?;
    Ok(())
}

async fn retire_entity(conn: &Conn, table: &str, id: uuid::Uuid) -> Result<()> {
    let sql = format!(
        "UPDATE {} SET sa_deleted_at = now(), sa_updated_at = now() WHERE id = $1 AND sa_deleted_at IS NULL",
        quote_table(table)
    );
    conn.execute(&sql, &[&id])
        .await
        .map_err(|e| Error::other(format!("retire {table}: {e}")))?;
    Ok(())
}

pub fn present_columns<T: Model>(raw: &Map<String, Value>) -> Vec<String> {
    T::present_columns(raw)
}
