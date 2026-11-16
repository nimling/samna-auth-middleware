use std::collections::HashMap;
use std::sync::{Arc, LazyLock, Mutex};

use axum::extract::{Request, State};
use axum::middleware::Next;
use axum::response::{IntoResponse, Response};
use base64::Engine;
use chrono::Utc;
use http::{HeaderMap, StatusCode};
use sha2::{Digest, Sha256};
use subtle::ConstantTimeEq;
use uuid::Uuid;

use super::main::{Actor, RequestCtx, parse_debug_clients, parse_uuid, respond_auth_error};
use super::models::{AuthError, DebugClient};
use crate::disciple::sdb;
use crate::shared::Ctx;
use crate::shared::models::{ACCESS_READ, OBJECT_TYPE_ORGANIZATION, SamnaClient};
use crate::utils::cfg;

pub const USER_TOKEN_HEADER: &str = "SAuth-User-Token";
pub const USER_EXTERNAL_HEADER: &str = "SAuth-User-ID";

pub struct ClientAuth {
    debug_user_id: Option<Uuid>,
    debug_clients: Option<HashMap<String, DebugClient>>,
}

impl ClientAuth {
    pub fn new() -> Arc<ClientAuth> {
        Arc::new(ClientAuth {
            debug_user_id: parse_uuid(&cfg().debug_user_id),
            debug_clients: parse_debug_clients(),
        })
    }
}

pub async fn client_auth(
    State(auth): State<Arc<ClientAuth>>,
    mut req: Request,
    next: Next,
) -> Response {
    let ctx = req
        .extensions()
        .get::<RequestCtx>()
        .map(|c| c.0.clone())
        .unwrap_or_default();
    if let Some(debug_user) = auth.debug_user_id {
        req.extensions_mut().insert(Actor(debug_user));
        if let Err(e) = forward_user(&ctx, req.headers(), debug_user)
            .await
            .and_then(|user| match user {
                Some(u) => Ok(Some(u)),
                None => Ok(None),
            })
        {
            return respond_auth_error(req.headers(), &e);
        }
        return next.run(req).await;
    }
    let (client_id, body_form) = read_client_credentials(req.headers(), None);
    let _ = body_form;
    let actor = match authenticate_client(
        &ctx,
        req.headers(),
        auth.debug_clients.as_ref(),
        client_id,
    )
    .await
    {
        Err(AuthError::CredentialMissing) => {
            return (StatusCode::UNAUTHORIZED, "Unauthorized").into_response();
        }
        Err(e) => return respond_auth_error(req.headers(), &e),
        Ok(actor) => actor,
    };
    let actor = match forward_user(&ctx, req.headers(), actor).await {
        Ok(Some(user)) => user,
        Ok(None) => actor,
        Err(e) => return respond_auth_error(req.headers(), &e),
    };
    req.extensions_mut().insert(Actor(actor));
    next.run(req).await
}

pub fn read_client_credentials(
    headers: &HeaderMap,
    form: Option<&HashMap<String, String>>,
) -> (Option<(String, String)>, bool) {
    if let Some(auth) = headers
        .get(http::header::AUTHORIZATION)
        .and_then(|h| h.to_str().ok())
        && let Some(encoded) = auth.strip_prefix("Basic ")
        && let Ok(payload) = base64::engine::general_purpose::STANDARD.decode(encoded)
        && let Ok(text) = String::from_utf8(payload)
        && let Some((id, secret)) = text.split_once(':')
    {
        return (Some((id.to_string(), secret.to_string())), false);
    }
    if let Some(form) = form
        && let (Some(id), Some(secret)) = (form.get("client_id"), form.get("client_secret"))
    {
        return (Some((id.clone(), secret.clone())), true);
    }
    (None, false)
}

pub async fn authenticate_client(
    ctx: &Ctx,
    _headers: &HeaderMap,
    debug_clients: Option<&HashMap<String, DebugClient>>,
    credentials: Option<(String, String)>,
) -> Result<Uuid, AuthError> {
    let Some((client_id, client_secret)) = credentials else {
        return Err(AuthError::CredentialMissing);
    };
    if client_id.is_empty() || client_secret.is_empty() {
        return Err(AuthError::CredentialMissing);
    }
    if let Some(debug) = debug_clients
        && !debug.is_empty()
    {
        for dc in debug.values() {
            if let Some(status) = &dc.status
                && !status.is_empty()
                && status != "active"
            {
                continue;
            }
            if let Some(expires) = dc.expires_at
                && Utc::now() > expires
            {
                continue;
            }
            if client_id == dc.client_id.to_string() && client_secret == dc.client_secret {
                return Ok(dc.client_id);
            }
        }
        return Err(AuthError::Unauthorized(
            "debug client basic auth mismatch".to_string(),
        ));
    }
    let client_uuid = Uuid::parse_str(&client_id)
        .map_err(|e| AuthError::Unauthorized(format!("parse client_id: {e}")))?;
    let Some(db) = sdb::instance() else {
        return Err(AuthError::Unauthorized(
            "lookup client: database not connected".to_string(),
        ));
    };
    let samna_client = db
        .get_client(ctx, client_uuid)
        .await
        .map_err(|e| AuthError::Unauthorized(format!("lookup client: {e}")))?;
    verify_client_secret(&samna_client, &client_secret)
        .map_err(|e| AuthError::Unauthorized(format!("verify client_secret: {e}")))?;
    Ok(samna_client.client_id)
}

pub async fn forward_user(
    ctx: &Ctx,
    headers: &HeaderMap,
    client_id: Uuid,
) -> Result<Option<Uuid>, AuthError> {
    if let Some(user) = forward_user_token(ctx, headers, client_id).await? {
        if headers.get(USER_EXTERNAL_HEADER).is_some() {
            return Err(AuthError::Forbidden(String::new()));
        }
        return Ok(Some(user));
    }
    forward_user_external(ctx, headers, client_id).await
}

async fn forward_user_token(
    ctx: &Ctx,
    headers: &HeaderMap,
    client_id: Uuid,
) -> Result<Option<Uuid>, AuthError> {
    let token = headers
        .get(USER_TOKEN_HEADER)
        .and_then(|h| h.to_str().ok())
        .unwrap_or("");
    if token.is_empty() {
        return Ok(None);
    }
    let Some(db) = sdb::instance() else {
        return Err(AuthError::UserTokenNotRecognized(
            "database not connected".to_string(),
        ));
    };
    let (user, owner_id) = db.validate_token(ctx, token).await.map_err(|e| {
        AuthError::UserTokenNotRecognized(format!("validate forwarded user token: {e}"))
    })?;
    let Some(user) = user.filter(|u| !u.user_id.is_nil()) else {
        return Err(AuthError::UserTokenNotRecognized(String::new()));
    };
    if owner_id.is_nil()
        || !db
            .has_access(
                ctx,
                client_id,
                owner_id,
                OBJECT_TYPE_ORGANIZATION,
                ACCESS_READ,
            )
            .await
    {
        return Err(AuthError::UserTokenNotRecognized(String::new()));
    }
    Ok(Some(user.user_id))
}

async fn forward_user_external(
    ctx: &Ctx,
    headers: &HeaderMap,
    client_id: Uuid,
) -> Result<Option<Uuid>, AuthError> {
    let external_id = headers
        .get(USER_EXTERNAL_HEADER)
        .and_then(|h| h.to_str().ok())
        .unwrap_or("");
    if external_id.is_empty() {
        return Ok(None);
    }
    if headers.get(USER_TOKEN_HEADER).is_some() {
        return Err(AuthError::Forbidden(String::new()));
    }
    let Some(db) = sdb::instance() else {
        return Err(AuthError::UserExternalNotRecognized(
            "database not connected".to_string(),
        ));
    };
    let user = db
        .resolve_external_user(ctx, client_id, external_id)
        .await
        .map_err(|e| {
            AuthError::UserExternalNotRecognized(format!("resolve external user id: {e}"))
        })?;
    let Some(user) = user.filter(|u| !u.user_id.is_nil()) else {
        return Err(AuthError::UserExternalNotRecognized(String::new()));
    };
    Ok(Some(user.user_id))
}

struct VerifiedSecret {
    digest: [u8; 32],
    secret_hash: String,
}

static VERIFIED_SECRETS: LazyLock<Mutex<HashMap<Uuid, VerifiedSecret>>> =
    LazyLock::new(|| Mutex::new(HashMap::new()));

pub fn verify_client_secret(client: &SamnaClient, secret: &str) -> Result<(), String> {
    let digest: [u8; 32] = Sha256::digest(secret.as_bytes()).into();
    if let Some(known) = VERIFIED_SECRETS
        .lock()
        .expect("verified secrets")
        .get(&client.client_id)
        && known.secret_hash == client.secret_hash
        && known.digest.ct_eq(&digest).into()
    {
        return Ok(());
    }
    match bcrypt::verify(secret, &client.secret_hash) {
        Ok(true) => {}
        Ok(false) => return Err("hashedPassword is not the hash of the given password".to_string()),
        Err(e) => return Err(e.to_string()),
    }
    VERIFIED_SECRETS.lock().expect("verified secrets").insert(
        client.client_id,
        VerifiedSecret {
            digest,
            secret_hash: client.secret_hash.clone(),
        },
    );
    Ok(())
}
