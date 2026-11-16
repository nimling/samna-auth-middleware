use std::collections::BTreeMap;
use std::sync::LazyLock;

use regex::Regex;
use serde::{Deserialize, Serialize};
use serde_json::Value;
use uuid::Uuid;

use super::error::{Error, Result};

static PROFILE_FIELD_PATTERN: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"^[a-z][a-z0-9_.]{0,63}$").expect("profile field pattern"));
static USER_NAME_PATTERN: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"^[a-z][a-z0-9_-]{2,31}$").expect("user name pattern"));
const PROFILE_TYPES: [&str; 7] = [
    "string", "number", "integer", "boolean", "array", "object", "null",
];
const PROFILE_FORMATS: [&str; 14] = [
    "multiline",
    "markdown",
    "uri",
    "email",
    "phone",
    "date",
    "time",
    "datetime",
    "image",
    "color",
    "country",
    "currency",
    "percent",
    "duration",
];
const PROFILE_COMPONENTS: [&str; 19] = [
    "text", "textarea", "markdown", "link", "badge", "chip", "tag", "list", "image", "avatar",
    "progress", "rating", "table", "keyvalue", "date", "boolean", "number", "card", "hidden",
];

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct ProfileSchema {
    #[serde(default)]
    pub r#type: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub title: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub description: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub format: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub component: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub icon: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub order: Option<i32>,
    #[serde(default, skip_serializing_if = "std::ops::Not::not")]
    pub hidden: bool,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub r#enum: Vec<Value>,
    #[serde(default, skip_serializing_if = "Value::is_null")]
    pub default: Value,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub minimum: Option<f64>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub maximum: Option<f64>,
    #[serde(default, skip_serializing_if = "Option::is_none", rename = "minLength")]
    pub min_length: Option<i64>,
    #[serde(default, skip_serializing_if = "Option::is_none", rename = "maxLength")]
    pub max_length: Option<i64>,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub pattern: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub link: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub items: Option<Box<ProfileSchema>>,
    #[serde(default, skip_serializing_if = "BTreeMap::is_empty")]
    pub properties: BTreeMap<String, ProfileSchema>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub required: Vec<String>,
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct ProfileItem {
    pub schema: ProfileSchema,
    #[serde(default)]
    pub value: Value,
}

pub type Profile = BTreeMap<String, ProfileItem>;

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct BodySamProfileItem {
    pub user_id: Uuid,
    pub field: String,
    pub schema: ProfileSchema,
    #[serde(default)]
    pub value: Value,
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct BodySamProfileItemKey {
    pub user_id: Uuid,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub field: Option<String>,
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct BodySamUserName {
    pub user_id: Uuid,
    pub user_name: Option<String>,
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct UserNameAvailable {
    #[serde(default)]
    pub user_name: String,
    #[serde(default)]
    pub available: bool,
}

fn bad(message: String) -> Error {
    Error::BadProfileItem(message)
}

impl BodySamProfileItem {
    pub fn item(&self) -> ProfileItem {
        ProfileItem {
            schema: self.schema.clone(),
            value: self.value.clone(),
        }
    }

    pub fn validate(&self) -> Result<()> {
        BodySamProfileItemKey {
            user_id: self.user_id,
            field: Some(self.field.clone()),
        }
        .validate()?;
        self.item().validate()
    }
}

impl ProfileItem {
    pub fn validate(&self) -> Result<()> {
        self.schema.validate("schema")?;
        if self.value.is_null() && false {
            return Err(bad("value must be JSON".to_string()));
        }
        self.schema.check(&self.value, "value")
    }
}

impl ProfileSchema {
    pub fn validate(&self, at: &str) -> Result<()> {
        if !PROFILE_TYPES.contains(&self.r#type.as_str()) {
            return Err(bad(format!(
                "{at} type must be one of {}",
                PROFILE_TYPES.join(", ")
            )));
        }
        if !self.format.is_empty() && !PROFILE_FORMATS.contains(&self.format.as_str()) {
            return Err(bad(format!(
                "{at} format must be one of {}",
                PROFILE_FORMATS.join(", ")
            )));
        }
        if !self.component.is_empty() && !PROFILE_COMPONENTS.contains(&self.component.as_str()) {
            return Err(bad(format!(
                "{at} component must be one of {}",
                PROFILE_COMPONENTS.join(", ")
            )));
        }
        if !self.pattern.is_empty() && Regex::new(&self.pattern).is_err() {
            return Err(bad(format!("{at} pattern does not compile")));
        }
        if let Some(items) = &self.items {
            items.validate(&format!("{at}.items"))?;
        }
        for (name, property) in &self.properties {
            property.validate(&format!("{at}.{name}"))?;
        }
        if !self.default.is_null() {
            self.check(&self.default, &format!("{at}.default"))?;
        }
        Ok(())
    }

    pub fn check(&self, value: &Value, at: &str) -> Result<()> {
        match self.r#type.as_str() {
            "string" => {
                let Some(s) = value.as_str() else {
                    return Err(bad(format!("{at} must be a string")));
                };
                let length = s.chars().count() as i64;
                if let Some(min) = self.min_length
                    && length < min
                {
                    return Err(bad(format!("{at} is shorter than minLength")));
                }
                if let Some(max) = self.max_length
                    && length > max
                {
                    return Err(bad(format!("{at} is longer than maxLength")));
                }
                if !self.pattern.is_empty()
                    && !Regex::new(&self.pattern)
                        .map(|r| r.is_match(s))
                        .unwrap_or(false)
                {
                    return Err(bad(format!("{at} does not match its pattern")));
                }
                if (self.format == "uri" || self.format == "image") && !s.starts_with("https://") {
                    return Err(bad(format!("{at} must be an https url")));
                }
            }
            "number" | "integer" => {
                let Some(f) = value.as_f64() else {
                    return Err(bad(format!("{at} must be a {}", self.r#type)));
                };
                if self.r#type == "integer" && f != f.trunc() {
                    return Err(bad(format!("{at} must be an integer")));
                }
                if let Some(min) = self.minimum
                    && f < min
                {
                    return Err(bad(format!("{at} is below minimum")));
                }
                if let Some(max) = self.maximum
                    && f > max
                {
                    return Err(bad(format!("{at} is above maximum")));
                }
            }
            "boolean" => {
                if !value.is_boolean() {
                    return Err(bad(format!("{at} must be a boolean")));
                }
            }
            "null" => {
                if !value.is_null() {
                    return Err(bad(format!("{at} must be null")));
                }
            }
            "array" => {
                let Some(list) = value.as_array() else {
                    return Err(bad(format!("{at} must be an array")));
                };
                if let Some(items) = &self.items {
                    for (n, element) in list.iter().enumerate() {
                        items.check(element, &format!("{at}[{n}]"))?;
                    }
                }
            }
            "object" => {
                let Some(object) = value.as_object() else {
                    return Err(bad(format!("{at} must be an object")));
                };
                for (name, property) in &self.properties {
                    if let Some(field) = object.get(name) {
                        property.check(field, &format!("{at}.{name}"))?;
                    }
                }
                for name in &self.required {
                    if !object.contains_key(name) {
                        return Err(bad(format!("{at} lacks {name}")));
                    }
                }
            }
            _ => {}
        }
        if !self.r#enum.is_empty() {
            let found = self.r#enum.iter().any(|candidate| candidate == value);
            if !found {
                return Err(bad(format!("{at} is not one of its enum")));
            }
        }
        Ok(())
    }
}

impl BodySamProfileItemKey {
    pub fn validate(&self) -> Result<()> {
        if self.user_id.is_nil() {
            return Err(bad("user_id is required".to_string()));
        }
        if let Some(field) = &self.field
            && !PROFILE_FIELD_PATTERN.is_match(field)
        {
            return Err(bad(
                "field must be lower case letters, digits, _ and . and start with a letter"
                    .to_string(),
            ));
        }
        Ok(())
    }
}

pub fn validate_user_name(user_name: &str) -> Result<()> {
    if !USER_NAME_PATTERN.is_match(user_name) {
        return Err(Error::BadUserName(
            "user_name must be 3 to 32 lower case letters, digits, _ and - and start with a letter"
                .to_string(),
        ));
    }
    Ok(())
}

impl BodySamUserName {
    pub fn validate(&self) -> Result<()> {
        if self.user_id.is_nil() {
            return Err(Error::BadUserName("user_id is required".to_string()));
        }
        if let Some(name) = &self.user_name {
            return validate_user_name(name);
        }
        Ok(())
    }
}
