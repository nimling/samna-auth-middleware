use uuid::Uuid;

use super::db::Db;
use super::error::{Error, Result};
use super::models::*;
use super::organization::patch_nulls;
use crate::client::Ctx;

impl Db {
    pub async fn get_object_fields(&self, ctx: &Ctx) -> Result<Vec<ObjectField>> {
        self.query_models::<ObjectField>(
            ctx,
            "SELECT * FROM claimius.object_field WHERE sa_deleted_at IS NULL",
            &[],
        )
        .await
    }

    pub async fn get_object_field(&self, ctx: &Ctx, field_id: Uuid) -> Result<ObjectField> {
        self.query_model::<ObjectField>(
            ctx,
            "SELECT * FROM claimius.object_field WHERE id = $1 AND sa_deleted_at IS NULL LIMIT 1",
            &[&field_id],
        )
        .await
    }

    pub async fn get_user_fields(&self, ctx: &Ctx, field_id: Uuid) -> Result<Vec<UserField>> {
        self.query_models::<UserField>(
            ctx,
            "SELECT * FROM claimius.user_field WHERE field_id = $1 AND sa_deleted_at IS NULL",
            &[&field_id],
        )
        .await
    }

    pub async fn insert_object_field(
        &self,
        ctx: &Ctx,
        ac: Option<&AccessClaim>,
        field: &mut ObjectField,
    ) -> Result<ObjectField> {
        let Some(ac) = ac else {
            return Err(Error::no_access());
        };
        field.created_by = ac.user_claim_id;
        field.owner_id = ac.owner_id;
        let conn = self.conn(ctx).await?;
        let mut to_insert = field.clone();
        to_insert.id = Uuid::nil();
        let row = super::query::insert_generic::<ObjectField>(&conn, &to_insert).await?;
        let body = field.body.clone();
        *field = ObjectField::from_row(row)?;
        field.body = body;
        Ok(field.clone())
    }

    pub async fn update_object_field(
        &self,
        ctx: &Ctx,
        ac: Option<&AccessClaim>,
        field_id: Uuid,
        field: &mut ObjectField,
    ) -> Result<ObjectField> {
        if ac.is_none() {
            return Err(Error::no_access());
        }
        let conn = self.conn(ctx).await?;
        let mut null_fields = Vec::new();
        if field.description.is_none() {
            null_fields.push("description");
        }
        if field.json_schema.is_none() {
            null_fields.push("json_schema");
        }
        if field.value.is_none() {
            null_fields.push("value");
        }
        let nulls = patch_nulls(&field.body, ObjectField::COLUMNS, &null_fields);
        let excludes = ["id", "sa_owner_id", "sa_created_by"].map(str::to_string);
        let row = super::query::update_generic::<ObjectField>(
            &conn,
            field,
            field_id,
            &[],
            &excludes,
            true,
            &nulls,
        )
        .await?;
        let body = field.body.clone();
        *field = ObjectField::from_row(row)?;
        field.body = body;
        Ok(field.clone())
    }

    pub async fn delete_object_field(
        &self,
        ctx: &Ctx,
        ac: Option<&AccessClaim>,
        field_id: Uuid,
    ) -> Result<ObjectField> {
        if ac.is_none() {
            return Err(Error::no_access());
        }
        self.query_model::<ObjectField>(ctx, "UPDATE claimius.object_field SET sa_deleted_at = NOW() WHERE id = $1 AND sa_deleted_at IS NULL RETURNING *", &[&field_id]).await
    }

    pub async fn insert_user_field(
        &self,
        ctx: &Ctx,
        ac: Option<&AccessClaim>,
        field: &mut UserField,
    ) -> Result<UserField> {
        let Some(ac) = ac else {
            return Err(Error::no_access());
        };
        field.created_by = ac.user_claim_id;
        field.owner_id = ac.owner_id;
        let conn = self.conn(ctx).await?;
        let mut to_insert = field.clone();
        to_insert.id = Uuid::nil();
        let row = super::query::insert_generic::<UserField>(&conn, &to_insert).await?;
        let body = field.body.clone();
        *field = UserField::from_row(row)?;
        field.body = body;
        Ok(field.clone())
    }

    pub async fn update_user_field(
        &self,
        ctx: &Ctx,
        ac: Option<&AccessClaim>,
        value_id: Uuid,
        field: &mut UserField,
    ) -> Result<UserField> {
        if ac.is_none() {
            return Err(Error::no_access());
        }
        let conn = self.conn(ctx).await?;
        let null_fields: Vec<&str> = if field.value.is_none() {
            vec!["value"]
        } else {
            Vec::new()
        };
        let nulls = patch_nulls(&field.body, UserField::COLUMNS, &null_fields);
        let excludes =
            ["id", "field_id", "user_id", "sa_owner_id", "sa_created_by"].map(str::to_string);
        let row = super::query::update_generic::<UserField>(
            &conn,
            field,
            value_id,
            &[],
            &excludes,
            true,
            &nulls,
        )
        .await?;
        let body = field.body.clone();
        *field = UserField::from_row(row)?;
        field.body = body;
        Ok(field.clone())
    }

    pub async fn delete_user_field(
        &self,
        ctx: &Ctx,
        ac: Option<&AccessClaim>,
        value_id: Uuid,
    ) -> Result<UserField> {
        if ac.is_none() {
            return Err(Error::no_access());
        }
        self.query_model::<UserField>(ctx, "UPDATE claimius.user_field SET sa_deleted_at = NOW() WHERE id = $1 AND sa_deleted_at IS NULL RETURNING *", &[&value_id]).await
    }
}

impl super::query::Write for ObjectField {
    type Result = ObjectField;

    fn replace_with(&mut self, result: &ObjectField) {
        let body = self.body.clone();
        *self = result.clone();
        self.body = body;
    }
}

impl super::query::Write for UserField {
    type Result = UserField;

    fn replace_with(&mut self, result: &UserField) {
        let body = self.body.clone();
        *self = result.clone();
        self.body = body;
    }
}

impl super::query::Write for Notification {
    type Result = Notification;

    fn replace_with(&mut self, result: &Notification) {
        *self = result.clone();
    }
}
