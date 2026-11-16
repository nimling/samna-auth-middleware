use std::sync::{Mutex, OnceLock};

use url::Url;

#[derive(Clone, Debug, Default)]
pub struct Config {
    pub endpoint: String,
    pub app_slug: String,
    pub app_secret: String,
    pub client_id: String,
    pub client_secret: String,
    pub redirect_uri: String,
    pub page_redirect: String,
    pub client_timeout: String,
    pub debug_user_id: String,
    pub debug_clients: String,
    pub debug_seed: String,
    pub debug_initiate_apps: String,
    pub replica_dsn: String,
    pub rate_limit: String,
    pub rate_period: String,

    pub issuer: String,
    pub token_url: Option<Url>,
    pub jwks_url: Option<Url>,
    pub auth_disabled: bool,
}

#[derive(Clone, Debug, Default)]
struct Oidc {
    issuer: String,
    token_url: Option<Url>,
    jwks_url: Option<Url>,
    auth_disabled: bool,
}

fn oidc() -> &'static Mutex<Oidc> {
    static OIDC: OnceLock<Mutex<Oidc>> = OnceLock::new();
    OIDC.get_or_init(|| Mutex::new(Oidc::default()))
}

fn env(key: &str) -> String {
    std::env::var(key).unwrap_or_default()
}

pub fn cfg() -> Config {
    let held = oidc().lock().expect("oidc").clone();
    Config {
        endpoint: env("SAUTH_ENDPOINT"),
        app_slug: env("SAUTH_APP_SLUG"),
        app_secret: env("SAUTH_APP_SECRET"),
        client_id: env("SAUTH_CLIENT_ID"),
        client_secret: env("SAUTH_CLIENT_SECRET"),
        redirect_uri: env("SAUTH_REDIRECT_URI"),
        page_redirect: env("SAUTH_PAGE_REDIRECT"),
        client_timeout: env("SAUTH_CLIENT_TIMEOUT"),
        debug_user_id: env("SAUTH_DEBUG_USER_ID"),
        debug_clients: env("SAUTH_DEBUG_CLIENTS"),
        debug_seed: env("SAUTH_DEBUG_PRIVATE_SEED"),
        debug_initiate_apps: env("SAUTH_DEBUG_INITIATE_APPS"),
        replica_dsn: env("SAUTH_REPLICA_DSN"),
        rate_limit: env("SAUTH_RATE_LIMIT"),
        rate_period: env("SAUTH_RATE_PERIOD"),
        issuer: held.issuer,
        token_url: held.token_url,
        jwks_url: held.jwks_url,
        auth_disabled: held.auth_disabled,
    }
}

pub fn set_oidc(issuer: &str, token_url: Url, jwks_url: Url) {
    let mut held = oidc().lock().expect("oidc");
    held.issuer = issuer.to_string();
    held.token_url = Some(token_url);
    held.jwks_url = Some(jwks_url);
    held.auth_disabled = false;
}

pub fn set_auth_disabled() {
    oidc().lock().expect("oidc").auth_disabled = true;
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum DebugMode {
    None,
    User,
    Clients,
}

impl DebugMode {
    pub fn as_str(self) -> &'static str {
        match self {
            DebugMode::None => "none",
            DebugMode::User => "user",
            DebugMode::Clients => "clients",
        }
    }
}

pub fn get_debug_mode() -> DebugMode {
    let c = cfg();
    if !c.debug_user_id.is_empty() {
        return DebugMode::User;
    }
    if !c.debug_clients.is_empty() {
        return DebugMode::Clients;
    }
    DebugMode::None
}
