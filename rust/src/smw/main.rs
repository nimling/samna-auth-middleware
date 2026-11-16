use std::collections::HashMap;
use std::sync::Arc;
use std::time::Duration;

use axum::extract::{Request, State};
use axum::middleware::Next;
use axum::response::{IntoResponse, Response};
use http::header::LOCATION;
use http::{HeaderMap, HeaderValue, StatusCode};
use url::Url;
use uuid::Uuid;

use super::client_auth::{authenticate_client, forward_user, read_client_credentials};
use super::code::{CodeSource, read_code, serve_anonymous, serve_code};
use super::models::{AuthError, AuthMiddlewareConfig, DebugClient, UserAuthOptions};
use super::session::{CookieScopeKey, authenticate_cookie};
use super::sync_seq::SEQ_TIMEOUT;
use super::token::{Jwks, RequestFacts, validate_token};
use crate::disciple::sdb;
use crate::dpop::PostgresReplayStore;
use crate::shared::Ctx;
use crate::utils::{DebugMode, cfg, get_debug_mode};

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Actor(pub Uuid);

#[derive(Clone, Debug)]
pub struct AccessTokenKey(pub String);

#[derive(Clone, Debug, Default)]
pub struct RequestCtx(pub Ctx);

pub struct UserAuth {
    pub config: AuthMiddlewareConfig,
    pub jwks: Option<Arc<Jwks>>,
    pub mode: DebugMode,
    pub inactive: bool,
}

impl UserAuth {
    pub fn new(opts: Option<UserAuthOptions>) -> Arc<UserAuth> {
        let mut config = config_from_user_auth_options(opts);
        let mode = get_debug_mode();
        if mode != DebugMode::None {
            return Arc::new(UserAuth {
                config,
                jwks: None,
                mode,
                inactive: false,
            });
        }
        let c = cfg();
        if c.auth_disabled {
            return Arc::new(UserAuth {
                config,
                jwks: None,
                mode,
                inactive: true,
            });
        }
        config.expected_token_issuer = c.issuer.clone();
        config.jwks_url = c.jwks_url.clone();
        config.token_url = c.token_url.clone();
        let jwks_url = config
            .jwks_url
            .as_ref()
            .map(|u| u.to_string())
            .unwrap_or_else(|| panic!("JWK client initialization failed: no jwks url"));
        Arc::new(UserAuth {
            config,
            jwks: Some(Jwks::new(&jwks_url)),
            mode,
            inactive: false,
        })
    }
}

fn facts<'a>(
    req: &'a Request,
    method: &'a str,
    path: &'a str,
    host: &'a str,
    scheme: &'a str,
) -> RequestFacts<'a> {
    RequestFacts {
        method,
        path,
        host,
        scheme,
        headers: req.headers(),
        tls_thumbprint: "",
    }
}

fn scheme_of(req: &Request) -> &'static str {
    if let Some(proto) = req
        .headers()
        .get("X-Forwarded-Proto")
        .and_then(|h| h.to_str().ok())
        && proto == "https"
    {
        return "https";
    }
    match req.uri().scheme_str() {
        Some("https") => "https",
        _ => "http",
    }
}

fn host_of(req: &Request) -> String {
    req.headers()
        .get(http::header::HOST)
        .and_then(|h| h.to_str().ok())
        .map(str::to_string)
        .or_else(|| req.uri().host().map(str::to_string))
        .unwrap_or_default()
}

fn ensure_ctx(req: &mut Request) -> Ctx {
    if let Some(ctx) = req.extensions().get::<RequestCtx>() {
        return ctx.0.clone();
    }
    let ctx = Ctx::new();
    req.extensions_mut().insert(RequestCtx(ctx.clone()));
    ctx
}

pub async fn user_auth(
    State(auth): State<Arc<UserAuth>>,
    mut req: Request,
    next: Next,
) -> Response {
    let ctx = ensure_ctx(&mut req);
    match auth.mode {
        DebugMode::User => {
            let debug_user = auth.config.debug_user_id.expect("debug user");
            req.extensions_mut().insert(Actor(debug_user));
            let actor = match forward_user(&ctx, req.headers(), debug_user).await {
                Ok(Some(user)) => user,
                Ok(None) => debug_user,
                Err(e) => return respond_auth_error(req.headers(), &e),
            };
            req.extensions_mut().insert(Actor(actor));
            let (value, source) = read_code(&mut req);
            if !value.is_empty() {
                return serve_code(req, value, source, next).await;
            }
            return next.run(req).await;
        }
        DebugMode::Clients => {
            let (credentials, _) = read_client_credentials(req.headers(), None);
            match authenticate_client(
                &ctx,
                req.headers(),
                auth.config.debug_clients.as_ref(),
                credentials,
            )
            .await
            {
                Err(AuthError::CredentialMissing) => {
                    let (value, source) = read_code(&mut req);
                    return serve_anonymous(req, value, source, next).await;
                }
                Err(e) => return respond_auth_error(req.headers(), &e),
                Ok(client) => {
                    let actor = match forward_user(&ctx, req.headers(), client).await {
                        Ok(Some(user)) => user,
                        Ok(None) => client,
                        Err(e) => return respond_auth_error(req.headers(), &e),
                    };
                    req.extensions_mut().insert(Actor(actor));
                }
            }
            let (value, source) = read_code(&mut req);
            if !value.is_empty() {
                return serve_code(req, value, source, next).await;
            }
            return next.run(req).await;
        }
        DebugMode::None => {}
    }
    if auth.inactive {
        return (StatusCode::SERVICE_UNAVAILABLE, "Auth service unavailable").into_response();
    }
    let mut set_cookies = HeaderMap::new();
    let (ctx, seq, fresh) = ctx.with_request_seq();
    if fresh {
        seq.set_timeout(if auth.config.seq_timeout.is_zero() {
            SEQ_TIMEOUT
        } else {
            auth.config.seq_timeout
        });
        req.extensions_mut().insert(RequestCtx(ctx.clone()));
    }
    let authenticated = match authenticate_request(&auth, &ctx, &mut req, &mut set_cookies).await {
        Ok(a) => a,
        Err(e) => return with_cookies(respond_auth_error(req.headers(), &e), set_cookies),
    };
    let (value, source) = read_code(&mut req);
    let response = if authenticated {
        if !value.is_empty() {
            serve_code(req, value, source, next).await
        } else {
            next.run(req).await
        }
    } else {
        serve_anonymous(req, value, source, next).await
    };
    let response = with_cookies(response, set_cookies);
    if fresh {
        let target = seq.get();
        if !target.is_zero()
            && let Some(db) = sdb::instance()
            && db.route_to_prophet
            && let Err(e) = db.wait_for_seq(&ctx, target, seq.timeout()).await
        {
            tracing::warn!("routed write {target} not yet applied, answering anyway: {e}");
        }
    }
    response
}

fn with_cookies(mut response: Response, cookies: HeaderMap) -> Response {
    for value in cookies.get_all(http::header::SET_COOKIE) {
        response
            .headers_mut()
            .append(http::header::SET_COOKIE, value.clone());
    }
    response
}

async fn authenticate_request(
    auth: &UserAuth,
    ctx: &Ctx,
    req: &mut Request,
    set_cookies: &mut HeaderMap,
) -> Result<bool, AuthError> {
    let method = req.method().to_string();
    let path = req.uri().path().to_string();
    let host = host_of(req);
    let scheme = scheme_of(req);
    let replay = sdb::instance().map(|db| PostgresReplayStore { db });
    let jwks = auth.jwks.as_ref().expect("jwks");
    {
        let f = facts(req, &method, &path, &host, scheme);
        match authenticate_bearer(&f, &auth.config, jwks, replay.as_ref()).await {
            Err(AuthError::CredentialMissing) => {}
            Err(e) => return Err(e),
            Ok(a) => {
                req.extensions_mut().insert(Actor(a.user_id));
                req.extensions_mut().insert(AccessTokenKey(a.access_token));
                return Ok(true);
            }
        }
    }
    let scope = req
        .extensions()
        .get::<CookieScopeKey>()
        .map(|s| s.0.clone());
    {
        let f = facts(req, &method, &path, &host, scheme);
        match authenticate_cookie(
            &f,
            set_cookies,
            scope.as_ref(),
            &auth.config,
            jwks,
            replay.as_ref(),
        )
        .await
        {
            Err(AuthError::CredentialMissing) => {}
            Err(e) => return Err(e),
            Ok(a) => {
                req.extensions_mut().insert(Actor(a.user_id));
                req.extensions_mut().insert(AccessTokenKey(a.access_token));
                return Ok(true);
            }
        }
    }
    let (credentials, _) = read_client_credentials(req.headers(), None);
    match authenticate_client(ctx, req.headers(), None, credentials).await {
        Err(AuthError::CredentialMissing) => Ok(false),
        Err(e) => Err(e),
        Ok(client) => {
            let actor = match forward_user(ctx, req.headers(), client).await? {
                Some(user) => user,
                None => client,
            };
            req.extensions_mut().insert(Actor(actor));
            Ok(true)
        }
    }
}

async fn authenticate_bearer(
    req: &RequestFacts<'_>,
    config: &AuthMiddlewareConfig,
    jwks: &Jwks,
    replay: Option<&PostgresReplayStore>,
) -> Result<super::session::Authenticated, AuthError> {
    let auth_header = req
        .headers
        .get(http::header::AUTHORIZATION)
        .and_then(|h| h.to_str().ok())
        .unwrap_or("");
    let Some(token) = auth_header.strip_prefix("Bearer ") else {
        return Err(AuthError::CredentialMissing);
    };
    match validate_token(req, token, config, jwks, replay).await {
        Ok(user) => Ok(super::session::Authenticated {
            user_id: user.id,
            access_token: token.to_string(),
        }),
        Err(e) => {
            tracing::error!("Failed to validate the bearer access_token: {e}");
            Err(AuthError::Unauthorized(format!(
                "validate bearer token: {e}"
            )))
        }
    }
}

pub fn get_login_path() -> String {
    let Ok(redirect) = Url::parse(&cfg().redirect_uri) else {
        return String::new();
    };
    let path = redirect.path();
    if path.is_empty() || path == "/" {
        return String::new();
    }
    let dir = match path.rfind('/') {
        Some(0) => "/".to_string(),
        Some(i) => path[..i].to_string(),
        None => String::new(),
    };
    if dir.ends_with('/') {
        format!("{dir}login")
    } else {
        format!("{dir}/login")
    }
}

pub fn is_navigation(headers: &HeaderMap) -> bool {
    if headers.get("Sec-Fetch-Mode").and_then(|h| h.to_str().ok()) == Some("navigate") {
        return true;
    }
    headers
        .get(http::header::ACCEPT)
        .and_then(|h| h.to_str().ok())
        .is_some_and(|a| a.contains("text/html"))
}

pub fn respond_unauthenticated(headers: &HeaderMap) -> Response {
    let login = get_login_path();
    if !login.is_empty()
        && is_navigation(headers)
        && let Ok(v) = HeaderValue::from_str(&login)
    {
        let mut response = StatusCode::FOUND.into_response();
        response.headers_mut().insert(LOCATION, v);
        return response;
    }
    (StatusCode::UNAUTHORIZED, "Unauthorized").into_response()
}

pub fn respond_auth_error(headers: &HeaderMap, err: &AuthError) -> Response {
    match err {
        AuthError::UserTokenNotRecognized(_) => {
            (StatusCode::NOT_FOUND, "User token not recognized").into_response()
        }
        AuthError::UserExternalNotRecognized(_) => {
            (StatusCode::NOT_FOUND, "User external id not recognized").into_response()
        }
        AuthError::Forbidden(_) => StatusCode::FORBIDDEN.into_response(),
        AuthError::Unauthorized(_) | AuthError::TokenExpired => respond_unauthenticated(headers),
        other => {
            tracing::error!("auth error: {other}");
            (StatusCode::INTERNAL_SERVER_ERROR, "Internal Server Error").into_response()
        }
    }
}

pub fn config_from_user_auth_options(opts: Option<UserAuthOptions>) -> AuthMiddlewareConfig {
    let opts = opts.unwrap_or_default();
    let c = cfg();
    let slug = if opts.client_slug.is_empty() {
        c.app_slug.clone()
    } else {
        opts.client_slug
    };
    let secret = if opts.client_secret.is_empty() {
        c.app_secret.clone()
    } else {
        opts.client_secret
    };
    let redirect_uri = if opts.redirect_uri.is_empty() {
        c.redirect_uri.clone()
    } else {
        opts.redirect_uri
    };
    let page_redirect_uri = if opts.page_redirect_uri.is_empty() {
        c.page_redirect.clone()
    } else {
        opts.page_redirect_uri
    };
    AuthMiddlewareConfig {
        client_slug: slug,
        client_secret: secret,
        redirect_uri,
        page_redirect_uri,
        debug_user_id: parse_uuid(&c.debug_user_id),
        debug_clients: parse_debug_clients(),
        seq_timeout: if opts.seq_timeout.is_zero() {
            Duration::ZERO
        } else {
            opts.seq_timeout
        },
        ..AuthMiddlewareConfig::default()
    }
}

pub fn parse_debug_clients() -> Option<HashMap<String, DebugClient>> {
    let raw = cfg().debug_clients;
    if raw.is_empty() {
        return None;
    }
    match serde_json::from_str::<HashMap<String, DebugClient>>(&raw) {
        Ok(clients) => Some(clients),
        Err(e) => panic!("Environment variable SAUTH_DEBUG_CLIENTS is not valid JSON: {e}"),
    }
}

pub fn parse_uuid(value: &str) -> Option<Uuid> {
    if value.is_empty() {
        return None;
    }
    match Uuid::parse_str(value) {
        Ok(id) => Some(id),
        Err(_) => panic!("Environment variable SAUTH_DEBUG_USER_ID is not a valid UUID"),
    }
}

pub fn actor_of(req: &Request) -> Option<Uuid> {
    req.extensions()
        .get::<Actor>()
        .map(|a| a.0)
        .filter(|a| !a.is_nil())
}

pub fn code_source_name(source: CodeSource) -> &'static str {
    match source {
        CodeSource::None => "none",
        CodeSource::Query => "query",
        CodeSource::Header => "header",
        CodeSource::Cookie => "cookie",
    }
}
