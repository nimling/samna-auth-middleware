use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use url::Url;
use uuid::Uuid;

pub const USER_ID_KEY: &str = "user_id";
pub const ACCESS_TOKEN_KEY: &str = "access_token";
pub const ANONYMOUS_KEY: &str = "anonymous";
pub const CODE_KEY: &str = "sauth_code";

pub const AUTH_CODE_PARAM_KEY: &str = "code";

pub const ACCESS_TOKEN_COOKIE_KEY: &str = "sauth_access_token";
pub const REFRESH_TOKEN_COOKIE_KEY: &str = "sauth_refresh_token";
pub const CSRF_TOKEN_COOKIE_KEY: &str = "sauth_csrf_token";

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum AuthError {
    TokenExpired,
    Unauthorized(String),
    Forbidden(String),
    CredentialMissing,
    UserTokenNotRecognized(String),
    UserExternalNotRecognized(String),
    Other(String),
}

impl AuthError {
    pub fn message(&self) -> &'static str {
        match self {
            AuthError::TokenExpired => "Token is expired",
            AuthError::Unauthorized(_) => "Unauthorized",
            AuthError::Forbidden(_) => "Forbidden",
            AuthError::CredentialMissing => "Credential missing",
            AuthError::UserTokenNotRecognized(_) => "User token not recognized",
            AuthError::UserExternalNotRecognized(_) => "User external id not recognized",
            AuthError::Other(_) => "auth error",
        }
    }

    pub fn is_credential_missing(&self) -> bool {
        matches!(self, AuthError::CredentialMissing)
    }
}

impl std::fmt::Display for AuthError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            AuthError::Unauthorized(d)
            | AuthError::Forbidden(d)
            | AuthError::UserTokenNotRecognized(d)
            | AuthError::UserExternalNotRecognized(d)
            | AuthError::Other(d)
                if !d.is_empty() =>
            {
                write!(f, "{}: {d}", self.message())
            }
            _ => f.write_str(self.message()),
        }
    }
}

pub type UserType = String;
pub const USER_TYPE_USER: &str = "user";
pub const USER_TYPE_CLIENT: &str = "client";
pub const USER_TYPE_SYSTEM: &str = "system";
pub const USER_TYPE_GUEST: &str = "guest";
pub const USER_TYPE_ANONYMOUS: &str = "anonymous";

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct DebugClient {
    #[serde(default)]
    pub name: String,
    pub client_id: Uuid,
    #[serde(default)]
    pub client_secret: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub description: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub status: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub expires_at: Option<DateTime<Utc>>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub owner_id: Option<Uuid>,
}

#[derive(Clone, Debug, Default)]
pub struct AuthMiddlewareConfig {
    pub client_slug: String,
    pub client_secret: String,
    pub token_url: Option<Url>,
    pub jwks_url: Option<Url>,
    pub expected_token_issuer: String,
    pub redirect_uri: String,
    pub page_redirect_uri: String,
    pub openid_config_url: Option<Url>,
    pub debug_user_id: Option<Uuid>,
    pub debug_clients: Option<std::collections::HashMap<String, DebugClient>>,
    pub seq_timeout: std::time::Duration,
}

#[derive(Clone, Debug, Default)]
pub struct UserAuthOptions {
    pub client_slug: String,
    pub client_secret: String,
    pub redirect_uri: String,
    pub page_redirect_uri: String,
    pub seq_timeout: std::time::Duration,
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct UserDetail {
    pub key: String,
    pub value: String,
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct Confirmation {
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub jkt: String,
    #[serde(default, skip_serializing_if = "String::is_empty", rename = "x5t#S256")]
    pub x5t_s256: String,
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct ClaimUser {
    pub id: Uuid,
    #[serde(default)]
    pub first_name: String,
    #[serde(default)]
    pub last_name: String,
    #[serde(default)]
    pub user_name: String,
    #[serde(default)]
    pub phone: String,
    #[serde(default)]
    pub email: String,
    #[serde(default)]
    pub location: String,
    #[serde(default)]
    pub r#type: UserType,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub details: Vec<UserDetail>,
}
