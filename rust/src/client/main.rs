use std::sync::{Mutex, OnceLock};

use serde::{Deserialize, Serialize};
use uuid::Uuid;

use super::http::{Ctx, get};
use super::models::ClientError;
use crate::utils::cfg;

struct Bound {
    endpoint: String,
    prophet: String,
}

fn bound() -> &'static Mutex<Bound> {
    static BOUND: OnceLock<Mutex<Bound>> = OnceLock::new();
    BOUND.get_or_init(|| {
        Mutex::new(Bound {
            endpoint: String::new(),
            prophet: String::new(),
        })
    })
}

pub fn bind(endpoint: &str, prophet: &str) {
    let mut held = bound().lock().expect("bound");
    held.endpoint = endpoint.to_string();
    held.prophet = prophet.to_string();
}

pub fn get_prophet() -> String {
    bound().lock().expect("bound").prophet.clone()
}

pub(super) struct SAuthClient {
    pub api_url: String,
    pub slug: String,
    pub secret: String,
}

pub(super) fn new_client(ctx: &Ctx) -> SAuthClient {
    let c = cfg();
    let mut endpoint = c.endpoint;
    let mut slug = c.app_slug;
    let mut secret = c.app_secret;
    {
        let held = bound().lock().expect("bound");
        if !held.endpoint.is_empty() {
            endpoint = held.endpoint.clone();
        }
    }
    if let Some(t) = &ctx.target {
        endpoint = t.endpoint.clone();
        slug = t.slug.clone();
        secret = t.secret.clone();
    }
    SAuthClient {
        api_url: format!("{endpoint}/sam"),
        slug,
        secret,
    }
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct AppSeed {
    pub private_seed: String,
    pub owner_id: Uuid,
}

fn app_seed() -> &'static Mutex<Option<AppSeed>> {
    static SEED: OnceLock<Mutex<Option<AppSeed>>> = OnceLock::new();
    SEED.get_or_init(|| Mutex::new(None))
}

pub async fn get_app_seed(ctx: &Ctx) -> Result<AppSeed, ClientError> {
    if let Some(seed) = app_seed().lock().expect("seed").clone() {
        return Ok(seed);
    }
    let delivered: AppSeed = get(ctx, "app")
        .await
        .map_err(|e| ClientError::new(e.kind, format!("get app seed from the prophet: {e}")))?;
    if delivered.private_seed.is_empty() {
        return Err(ClientError::other("the prophet delivered no app seed"));
    }
    *app_seed().lock().expect("seed") = Some(delivered.clone());
    Ok(delivered)
}

pub fn delete_app_seed() {
    *app_seed().lock().expect("seed") = None;
}
