use super::error::{Error, Result};
use super::models::*;

fn unknown_first(unknown: Vec<String>) -> Result<()> {
    match unknown.first() {
        Some(first) => Err(Error::bad_request(format!("{first} not allowed"))),
        None => Ok(()),
    }
}

fn denied(body: &super::bind::Body, fields: &[&str]) -> Result<()> {
    for field in fields {
        if body.has_key(field) {
            return Err(Error::bad_request(format!("{field} not allowed")));
        }
    }
    Ok(())
}

const AUDIT_FIELDS: [&str; 7] = [
    "id",
    "app_id",
    "owner_id",
    "created_by",
    "created_at",
    "updated_at",
    "deleted_at",
];

fn valid_email(email: &str) -> bool {
    let Some((local, domain)) = email.rsplit_once('@') else {
        return false;
    };
    !local.is_empty()
        && !domain.is_empty()
        && !email.contains(char::is_whitespace)
        && domain.contains('.')
        || (!local.is_empty() && !domain.is_empty() && !email.contains(char::is_whitespace))
}

fn valid_status(status: &Option<String>) -> Result<()> {
    if let Some(s) = status
        && ![
            USER_STATUS_ACTIVE,
            USER_STATUS_PENDING,
            USER_STATUS_SUSPENDED,
            USER_STATUS_INACTIVE,
        ]
        .contains(&s.as_str())
    {
        return Err(Error::bad_request("status invalid"));
    }
    Ok(())
}

impl Location {
    pub fn validate_post(&self) -> Result<()> {
        unknown_first(self.body.get_unknown(Location::FIELDS))?;
        if self.name.as_deref().is_none_or(|n| n.is_empty()) {
            return Err(Error::bad_request("name required"));
        }
        if self.owner_id.is_nil() {
            return Err(Error::bad_request("owner_id required"));
        }
        Ok(())
    }

    pub fn validate_put(&self) -> Result<()> {
        unknown_first(self.body.get_unknown(Location::FIELDS))?;
        denied(
            &self.body,
            &[
                "id",
                "app_id",
                "owner_id",
                "parent_id",
                "root_id",
                "created_by",
                "created_at",
                "updated_at",
                "deleted_at",
            ],
        )?;
        if self.name.as_deref().is_none_or(|n| n.is_empty()) {
            return Err(Error::bad_request("name required"));
        }
        if self.r#type.is_empty() {
            return Err(Error::bad_request("type required"));
        }
        Ok(())
    }

    pub fn validate_patch(&self) -> Result<()> {
        if self.body.is_empty() {
            return Err(Error::bad_request("no fields to update"));
        }
        unknown_first(self.body.get_unknown(Location::FIELDS))?;
        denied(
            &self.body,
            &[
                "id",
                "app_id",
                "owner_id",
                "parent_id",
                "root_id",
                "created_by",
                "created_at",
                "updated_at",
                "deleted_at",
            ],
        )?;
        if self.body.has_key("name") && self.name.as_deref().is_none_or(|n| n.is_empty()) {
            return Err(Error::bad_request("name required"));
        }
        Ok(())
    }
}

impl Organization {
    pub fn validate_post(&self) -> Result<()> {
        denied(&self.body, &["type"])?;
        unknown_first(self.body.get_unknown(Organization::FIELDS))?;
        if self.name.is_empty() {
            return Err(Error::bad_request("name required"));
        }
        Ok(())
    }

    pub fn validate_put(&self) -> Result<()> {
        denied(&self.body, &["type"])?;
        unknown_first(self.body.get_unknown(Organization::FIELDS))?;
        denied(
            &self.body,
            &[
                "id",
                "app_id",
                "owner_id",
                "root_id",
                "created_by",
                "created_at",
                "updated_at",
                "deleted_at",
            ],
        )?;
        if self.name.is_empty() {
            return Err(Error::bad_request("name required"));
        }
        Ok(())
    }

    pub fn validate_patch(&self) -> Result<()> {
        if self.body.is_empty() {
            return Err(Error::bad_request("no fields to update"));
        }
        denied(&self.body, &["type"])?;
        unknown_first(self.body.get_unknown(Organization::FIELDS))?;
        denied(
            &self.body,
            &[
                "id",
                "app_id",
                "owner_id",
                "root_id",
                "created_by",
                "created_at",
                "updated_at",
                "deleted_at",
            ],
        )?;
        if self.body.has_key("name") && self.name.is_empty() {
            return Err(Error::bad_request("name required"));
        }
        Ok(())
    }
}

impl SamnaUser {
    pub fn validate_post(&self) -> Result<()> {
        denied(&self.body, &["type"])?;
        unknown_first(self.body.get_unknown(SamnaUser::FIELDS))?;
        if self.email.is_empty() {
            return Err(Error::bad_request("email required"));
        }
        if !valid_email(&self.email) {
            return Err(Error::bad_request("email invalid"));
        }
        if self.owner_id.is_none_or(|o| o.is_nil()) {
            return Err(Error::bad_request("owner_id required"));
        }
        if self.claim_id.is_none_or(|c| c.is_nil()) {
            return Err(Error::bad_request("claim_id required"));
        }
        valid_status(&self.status)
    }

    pub fn validate_put(&self) -> Result<()> {
        denied(&self.body, &["type"])?;
        unknown_first(self.body.get_unknown(SamnaUser::FIELDS))?;
        denied(
            &self.body,
            &[
                "id",
                "app_id",
                "user_id",
                "owner_id",
                "root_id",
                "created_by",
                "created_at",
                "updated_at",
                "deleted_at",
            ],
        )?;
        if self.email.is_empty() {
            return Err(Error::bad_request("email required"));
        }
        if !valid_email(&self.email) {
            return Err(Error::bad_request("email invalid"));
        }
        valid_status(&self.status)
    }

    pub fn validate_patch(&self) -> Result<()> {
        if self.body.is_empty() {
            return Err(Error::bad_request("no fields to update"));
        }
        denied(&self.body, &["type"])?;
        unknown_first(self.body.get_unknown(SamnaUser::FIELDS))?;
        denied(
            &self.body,
            &[
                "id",
                "app_id",
                "user_id",
                "owner_id",
                "root_id",
                "created_by",
                "created_at",
                "updated_at",
                "deleted_at",
            ],
        )?;
        if self.body.has_key("email") {
            if self.email.is_empty() {
                return Err(Error::bad_request("email required"));
            }
            if !valid_email(&self.email) {
                return Err(Error::bad_request("email invalid"));
            }
        }
        valid_status(&self.status)
    }
}

impl SamnaUserToken {
    pub fn validate_post(&self) -> Result<()> {
        unknown_first(self.body.get_unknown(&[
            "token",
            "id_claim",
            "user",
            "claim_id",
            "reason",
            "owner_id",
            "created_by",
        ]))?;
        if self.token.is_empty() {
            return Err(Error::bad_request("token required"));
        }
        if self.owner_id.is_none_or(|o| o.is_nil()) {
            return Err(Error::bad_request("owner_id required"));
        }
        if self.claim_id.is_none_or(|c| c.is_nil()) {
            return Err(Error::bad_request("claim_id required"));
        }
        if self
            .user
            .as_ref()
            .and_then(|u| u.external_id.as_deref())
            .is_some_and(|e| !e.is_empty())
        {
            return Err(Error::bad_request("user.external_id not allowed"));
        }
        Ok(())
    }
}

impl SamnaUserExternal {
    pub fn validate_post(&self) -> Result<()> {
        unknown_first(self.body.get_unknown(&[
            "user",
            "claim_id",
            "reason",
            "owner_id",
            "created_by",
        ]))?;
        if self.owner_id.is_none_or(|o| o.is_nil()) {
            return Err(Error::bad_request("owner_id required"));
        }
        if self.claim_id.is_none_or(|c| c.is_nil()) {
            return Err(Error::bad_request("claim_id required"));
        }
        let Some(user) = &self.user else {
            return Err(Error::bad_request("user required"));
        };
        if user.external_id.as_deref().is_none_or(|e| e.is_empty()) {
            return Err(Error::bad_request("user.external_id required"));
        }
        Ok(())
    }
}

impl SamnaUserGuest {
    pub fn validate_post(&self) -> Result<()> {
        unknown_first(self.body.get_unknown(&[
            "user",
            "claim_id",
            "ends_at",
            "reason",
            "owner_id",
            "created_by",
        ]))?;
        if self.owner_id.is_none_or(|o| o.is_nil()) {
            return Err(Error::bad_request("owner_id required"));
        }
        if self.claim_id.is_none_or(|c| c.is_nil()) {
            return Err(Error::bad_request("claim_id required"));
        }
        if self.ends_at.is_none() {
            return Err(Error::bad_request("ends_at required"));
        }
        if self.user.is_none() {
            return Err(Error::bad_request("user required"));
        }
        Ok(())
    }
}

fn valid_claim_type(t: &str, allow_empty: bool) -> Result<()> {
    match t {
        "" if allow_empty => Ok(()),
        "" => Err(Error::bad_request("type required")),
        CLAIM_TYPE_USER | CLAIM_TYPE_SERVICE | CLAIM_TYPE_VIRTUAL => Ok(()),
        CLAIM_TYPE_SYSTEM => Err(Error::bad_request("type system not allowed")),
        _ => Err(Error::bad_request("type invalid")),
    }
}

impl Claim {
    pub fn validate_post(&self) -> Result<()> {
        unknown_first(self.body.get_unknown(Claim::FIELDS))?;
        if self.name.is_empty() {
            return Err(Error::bad_request("name required"));
        }
        if self.owner_id.is_nil() {
            return Err(Error::bad_request("owner_id required"));
        }
        if self.access.is_zero() {
            return Err(Error::bad_request("access required"));
        }
        valid_claim_type(&self.r#type, true)
    }

    pub fn validate_put(&self) -> Result<()> {
        unknown_first(self.body.get_unknown(Claim::FIELDS))?;
        denied(
            &self.body,
            &[
                "id",
                "app_id",
                "owner_id",
                "root_id",
                "created_by",
                "created_at",
                "updated_at",
                "deleted_at",
            ],
        )?;
        if self.name.is_empty() {
            return Err(Error::bad_request("name required"));
        }
        valid_claim_type(&self.r#type, false)
    }

    pub fn validate_patch(&self) -> Result<()> {
        if self.body.is_empty() {
            return Err(Error::bad_request("no fields to update"));
        }
        unknown_first(self.body.get_unknown(Claim::FIELDS))?;
        denied(
            &self.body,
            &[
                "id",
                "app_id",
                "owner_id",
                "root_id",
                "created_by",
                "created_at",
                "updated_at",
                "deleted_at",
            ],
        )?;
        if self.body.has_key("name") && self.name.is_empty() {
            return Err(Error::bad_request("name required"));
        }
        if self.body.has_key("type") {
            match self.r#type.as_str() {
                CLAIM_TYPE_USER | CLAIM_TYPE_SERVICE | CLAIM_TYPE_VIRTUAL => {}
                CLAIM_TYPE_SYSTEM => return Err(Error::bad_request("type system not allowed")),
                _ => return Err(Error::bad_request("type invalid")),
            }
        }
        Ok(())
    }
}

impl ClaimObject {
    pub fn validate_post(&self) -> Result<()> {
        unknown_first(self.body.get_unknown(ClaimObject::FIELDS))?;
        if self.object_id.is_none_or(|o| o.is_nil()) {
            return Err(Error::bad_request("object_id required"));
        }
        if self
            .object_type
            .as_ref()
            .is_none_or(|t| t.as_str().is_empty())
        {
            return Err(Error::bad_request("object_type required"));
        }
        Ok(())
    }
}

impl UserClaim {
    pub fn validate_post(&self) -> Result<()> {
        unknown_first(self.body.get_unknown(UserClaim::FIELDS))?;
        if self.user_id.is_nil() {
            return Err(Error::bad_request("user_id required"));
        }
        Ok(())
    }
}

impl SamnaSecret {
    pub fn validate_post(&self) -> Result<()> {
        unknown_first(self.body.get_unknown(SamnaSecret::FIELDS))?;
        if self.key.is_empty() {
            return Err(Error::bad_request("key required"));
        }
        if self.value.is_empty() {
            return Err(Error::bad_request("value required"));
        }
        if self.owner_id.is_nil() {
            return Err(Error::bad_request("owner_id required"));
        }
        Ok(())
    }

    pub fn validate_put(&self) -> Result<()> {
        unknown_first(self.body.get_unknown(SamnaSecret::FIELDS))?;
        denied(&self.body, &AUDIT_FIELDS)?;
        if self.key.is_empty() {
            return Err(Error::bad_request("key required"));
        }
        if self.value.is_empty() {
            return Err(Error::bad_request("value required"));
        }
        Ok(())
    }

    pub fn validate_patch(&self) -> Result<()> {
        if self.body.is_empty() {
            return Err(Error::bad_request("no fields to update"));
        }
        unknown_first(self.body.get_unknown(SamnaSecret::FIELDS))?;
        denied(&self.body, &AUDIT_FIELDS)?;
        if self.body.has_key("key") && self.key.is_empty() {
            return Err(Error::bad_request("key required"));
        }
        if self.body.has_key("value") && self.value.is_empty() {
            return Err(Error::bad_request("value required"));
        }
        Ok(())
    }
}

impl UserRelation {
    pub fn validate_post(&self) -> Result<()> {
        unknown_first(self.body.get_unknown(UserRelation::FIELDS))?;
        if self.object_type.as_str().is_empty() {
            return Err(Error::bad_request("object_type required"));
        }
        if self.object_id.is_nil() {
            return Err(Error::bad_request("object_id required"));
        }
        Ok(())
    }

    pub fn validate_put(&self) -> Result<()> {
        unknown_first(self.body.get_unknown(UserRelation::FIELDS))?;
        denied(
            &self.body,
            &[
                "id",
                "app_id",
                "user_id",
                "owner_id",
                "created_by",
                "created_at",
                "updated_at",
                "deleted_at",
            ],
        )?;
        if self.object_type.as_str().is_empty() {
            return Err(Error::bad_request("object_type required"));
        }
        if self.object_id.is_nil() {
            return Err(Error::bad_request("object_id required"));
        }
        Ok(())
    }

    pub fn validate_patch(&self) -> Result<()> {
        if self.body.is_empty() {
            return Err(Error::bad_request("no fields to update"));
        }
        unknown_first(self.body.get_unknown(UserRelation::FIELDS))?;
        denied(
            &self.body,
            &[
                "id",
                "app_id",
                "user_id",
                "owner_id",
                "created_by",
                "created_at",
                "updated_at",
                "deleted_at",
            ],
        )?;
        if self.body.has_key("object_type") && self.object_type.as_str().is_empty() {
            return Err(Error::bad_request("object_type required"));
        }
        if self.body.has_key("object_id") && self.object_id.is_nil() {
            return Err(Error::bad_request("object_id required"));
        }
        Ok(())
    }
}

impl Asset {
    pub fn validate_post(&self) -> Result<()> {
        unknown_first(self.body.get_unknown(Asset::FIELDS))?;
        if self.name.is_empty() {
            return Err(Error::bad_request("name required"));
        }
        if self.mime_type.is_empty() {
            return Err(Error::bad_request("mime_type required"));
        }
        Ok(())
    }

    pub fn validate_put(&self) -> Result<()> {
        unknown_first(self.body.get_unknown(Asset::FIELDS))?;
        denied(
            &self.body,
            &[
                "id",
                "app_id",
                "user_id",
                "owner_id",
                "created_by",
                "created_at",
                "updated_at",
                "deleted_at",
            ],
        )?;
        if self.name.is_empty() {
            return Err(Error::bad_request("name required"));
        }
        if self.mime_type.is_empty() {
            return Err(Error::bad_request("mime_type required"));
        }
        Ok(())
    }

    pub fn validate_patch(&self) -> Result<()> {
        if self.body.is_empty() {
            return Err(Error::bad_request("no fields to update"));
        }
        unknown_first(self.body.get_unknown(Asset::FIELDS))?;
        denied(
            &self.body,
            &[
                "id",
                "app_id",
                "user_id",
                "owner_id",
                "created_by",
                "created_at",
                "updated_at",
                "deleted_at",
            ],
        )?;
        if self.body.has_key("name") && self.name.is_empty() {
            return Err(Error::bad_request("name required"));
        }
        if self.body.has_key("mime_type") && self.mime_type.is_empty() {
            return Err(Error::bad_request("mime_type required"));
        }
        Ok(())
    }
}

impl ObjectAsset {
    pub fn validate_post(&self) -> Result<()> {
        unknown_first(self.body.get_unknown(ObjectAsset::FIELDS))?;
        if !self.object_type.as_str().starts_with("claimius.") {
            return Err(Error::bad_request("object_type must be a claimius type"));
        }
        if self.object_id.is_nil() {
            return Err(Error::bad_request("object_id required"));
        }
        if self.asset_id.is_nil() {
            return Err(Error::bad_request("asset_id required"));
        }
        if self.kind.is_empty() {
            return Err(Error::bad_request("kind required"));
        }
        if self.mime_type.is_empty() {
            return Err(Error::bad_request("mime_type required"));
        }
        Ok(())
    }

    pub fn validate_patch(&self) -> Result<()> {
        if self.body.is_empty() {
            return Err(Error::bad_request("no fields to update"));
        }
        unknown_first(self.body.get_unknown(ObjectAsset::FIELDS))?;
        if self.body.has_key("kind") && self.kind.is_empty() {
            return Err(Error::bad_request("kind required"));
        }
        if self.body.has_key("mime_type") && self.mime_type.is_empty() {
            return Err(Error::bad_request("mime_type required"));
        }
        Ok(())
    }
}
