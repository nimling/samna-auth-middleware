use std::net::SocketAddr;
use std::time::Duration;

use axum::Router;
use axum::extract::{Path, Request};
use axum::response::{IntoResponse, Response};
use axum::routing::{get, post};
use http::StatusCode;
use samna_auth_middleware::disciple::{sam, sdb};
use samna_auth_middleware::notification;
use samna_auth_middleware::shared::access::AccessClaimSetLookup;
use samna_auth_middleware::shared::{
    self, ACCESS_READ, AccessRequest, ClaimAccess, GraphWindow, Location, Organization,
};
use samna_auth_middleware::smw::{self, Actor, RequestCtx};
use samna_auth_middleware::uptime;
use serde::{Deserialize, Serialize};
use tokio_util::sync::CancellationToken;
use uuid::Uuid;

#[derive(Deserialize)]
struct AccessCheck {
    object_id: Uuid,
    object_type: String,
    required: ClaimAccess,
}

#[derive(Serialize)]
struct AccessVerdict {
    object_id: Uuid,
    granted: bool,
    claim_name: Option<String>,
}

#[tokio::main]
async fn main() {
    tracing_init();
    if let Err(e) = sdb::init().await {
        eprintln!("{e}");
        std::process::exit(1);
    }
    let auth = smw::UserAuth::new(None);
    let api = Router::new()
        .route("/api/access/{type}/{id}", get(get_access))
        .route("/api/access", post(post_access_batch))
        .route("/api/user/{target}", get(get_user_access))
        .route("/api/root", get(get_root_access))
        .route("/api/graph/owner", get(get_owner_graph))
        .route("/api/graph/claim", get(get_claim_graph))
        .route("/api/organizations", get(get_organizations))
        .route("/api/locations", get(get_locations))
        .merge(sam::register_endpoints(Router::new()))
        .layer(axum::middleware::from_fn_with_state(auth, smw::user_auth));
    let app = Router::new()
        .nest(
            uptime::STATUS_BASE_PATH,
            sam::register_status(Router::new()),
        )
        .nest("/api/auth", sam::register_cli(Router::new()))
        .merge(api);
    let stop = CancellationToken::new();
    if let Err(e) = sam::init(stop.clone()).await {
        eprintln!("{e}");
        std::process::exit(1);
    }
    let port = std::env::var("PORT")
        .ok()
        .filter(|p| !p.is_empty())
        .unwrap_or_else(|| "8080".to_string());
    let addr: SocketAddr = format!("0.0.0.0:{port}").parse().expect("address");
    let listener = tokio::net::TcpListener::bind(addr).await.expect("bind");
    let shutdown = stop.clone();
    let server = axum::serve(
        listener,
        app.into_make_service_with_connect_info::<SocketAddr>(),
    )
    .with_graceful_shutdown(async move {
        let _ = tokio::signal::ctrl_c().await;
        notification::end_streams();
        shutdown.cancel();
    });
    if let Err(e) = server.await {
        eprintln!("{e}");
    }
    tokio::time::timeout(Duration::from_secs(10), sam::done())
        .await
        .ok();
    if let Some(db) = sdb::instance() {
        db.close().await;
    }
}

fn tracing_init() {
    let _ = tracing::subscriber::set_global_default(tracing::subscriber::NoSubscriber::default());
}

fn actor(req: &Request) -> Uuid {
    req.extensions()
        .get::<Actor>()
        .map(|a| a.0)
        .unwrap_or(Uuid::nil())
}

fn ctx(req: &Request) -> shared::Ctx {
    req.extensions()
        .get::<RequestCtx>()
        .map(|c| c.0.clone())
        .unwrap_or_default()
}

fn json<T: Serialize>(status: StatusCode, body: &T) -> Response {
    (
        status,
        axum::Json(serde_json::to_value(body).unwrap_or_default()),
    )
        .into_response()
}

fn access_error(err: &shared::Error) -> Response {
    if err.is_no_access() {
        return json(
            StatusCode::FORBIDDEN,
            &serde_json::json!({ "error": "no access" }),
        );
    }
    if err.is_not_found() {
        return json(
            StatusCode::NOT_FOUND,
            &serde_json::json!({ "error": "not found" }),
        );
    }
    json(
        StatusCode::INTERNAL_SERVER_ERROR,
        &serde_json::json!({ "error": err.to_string() }),
    )
}

fn query(req: &Request, key: &str) -> Option<String> {
    req.uri().query().and_then(|q| {
        url::form_urlencoded::parse(q.as_bytes())
            .find(|(k, _)| k == key)
            .map(|(_, v)| v.into_owned())
    })
}

fn int_query(req: &Request, key: &str, fallback: i32) -> i32 {
    query(req, key)
        .and_then(|v| v.parse().ok())
        .unwrap_or(fallback)
}

fn uuid_query(req: &Request, key: &str) -> Option<Uuid> {
    query(req, key).and_then(|v| Uuid::parse_str(&v).ok())
}

fn graph_window(req: &Request) -> GraphWindow {
    GraphWindow {
        width: int_query(req, "width", 0),
        sort: query(req, "sort").unwrap_or_default(),
        next_id: uuid_query(req, "next_id"),
    }
}

async fn get_access(Path((object_type, id)): Path<(String, String)>, req: Request) -> Response {
    let Ok(object_id) = Uuid::parse_str(&id) else {
        return json(
            StatusCode::BAD_REQUEST,
            &serde_json::json!({ "error": "id must be a uuid" }),
        );
    };
    let required = ClaimAccess(int_query(&req, "required", ACCESS_READ.0));
    let db = sdb::need().expect("database");
    match db
        .mutation_access(
            &ctx(&req),
            actor(&req),
            object_id,
            &format!("claimius.{object_type}"),
            required,
        )
        .await
    {
        Ok(ac) => json(StatusCode::OK, &ac),
        Err(e) => access_error(&e),
    }
}

async fn post_access_batch(req: Request) -> Response {
    let user = actor(&req);
    let ctx_value = ctx(&req);
    let Ok(bytes) = axum::body::to_bytes(req.into_body(), 1 << 20).await else {
        return json(
            StatusCode::BAD_REQUEST,
            &serde_json::json!({ "error": "body must be a list of checks" }),
        );
    };
    let Ok(checks) = serde_json::from_slice::<Vec<AccessCheck>>(&bytes) else {
        return json(
            StatusCode::BAD_REQUEST,
            &serde_json::json!({ "error": "body must be a list of checks" }),
        );
    };
    let requests: Vec<AccessRequest> = checks
        .iter()
        .map(|c| AccessRequest {
            user_id: user,
            object_id: c.object_id,
            object_type: c.object_type.clone(),
            required_access: c.required,
        })
        .collect();
    let db = sdb::need().expect("database");
    let set = match db.mutation_access_many(&ctx_value, &requests).await {
        Ok(set) => set,
        Err(e) => return access_error(&e),
    };
    let verdicts: Vec<AccessVerdict> = checks
        .iter()
        .map(|c| {
            let claim = set.get(&c.object_type, c.object_id, user);
            AccessVerdict {
                object_id: c.object_id,
                granted: set.has(&c.object_type, c.object_id, user, Some(c.required)),
                claim_name: claim.and_then(|cl| cl.claim_name.clone()),
            }
        })
        .collect();
    json(StatusCode::OK, &verdicts)
}

async fn get_user_access(Path(target): Path<String>, req: Request) -> Response {
    let db = sdb::need().expect("database");
    match db
        .user_access(&ctx(&req), actor(&req), &target, ClaimAccess(0))
        .await
    {
        Ok(u) => json(StatusCode::OK, &u),
        Err(e) => access_error(&e),
    }
}

async fn get_root_access(req: Request) -> Response {
    let db = sdb::need().expect("database");
    match db.root_access(&ctx(&req), actor(&req), ACCESS_READ).await {
        Ok(ac) => json(StatusCode::OK, &ac),
        Err(e) => access_error(&e),
    }
}

async fn get_owner_graph(req: Request) -> Response {
    let db = sdb::need().expect("database");
    match db
        .get_owner_graph(
            &ctx(&req),
            actor(&req),
            uuid_query(&req, "start_id"),
            int_query(&req, "depth", 0),
            ClaimAccess(int_query(&req, "access", ACCESS_READ.0)),
            &graph_window(&req),
        )
        .await
    {
        Ok(nodes) => json(StatusCode::OK, &nodes),
        Err(e) => access_error(&e),
    }
}

async fn get_claim_graph(req: Request) -> Response {
    let db = sdb::need().expect("database");
    match db
        .get_claim_graph(
            &ctx(&req),
            actor(&req),
            uuid_query(&req, "start_id"),
            int_query(&req, "depth", 0),
            ClaimAccess(int_query(&req, "access", ACCESS_READ.0)),
            &graph_window(&req),
        )
        .await
    {
        Ok(nodes) => json(StatusCode::OK, &nodes),
        Err(e) => access_error(&e),
    }
}

async fn get_organizations(req: Request) -> Response {
    let (after, limit) = match shared::query::query_page(
        query(&req, "cursor").as_deref(),
        query(&req, "limit").as_deref(),
    ) {
        Ok(v) => v,
        Err(e) => {
            return json(
                StatusCode::BAD_REQUEST,
                &serde_json::json!({ "error": e.to_string() }),
            );
        }
    };
    let required = int_query(&req, "required", ACCESS_READ.0);
    let page_limit: Option<i32> = if limit > 0 { Some(limit) } else { None };
    let db = sdb::need().expect("database");
    match db
        .query_models::<Organization>(
            &ctx(&req),
            "SELECT * FROM claimius.get_organizations($1, $2, $3, $4)",
            &[&actor(&req), &required, &after, &page_limit],
        )
        .await
    {
        Ok(orgs) => json(StatusCode::OK, &orgs),
        Err(e) => access_error(&e),
    }
}

async fn get_locations(req: Request) -> Response {
    let (after, limit) = match shared::query::query_page(
        query(&req, "cursor").as_deref(),
        query(&req, "limit").as_deref(),
    ) {
        Ok(v) => v,
        Err(e) => {
            return json(
                StatusCode::BAD_REQUEST,
                &serde_json::json!({ "error": e.to_string() }),
            );
        }
    };
    let required = int_query(&req, "required", ACCESS_READ.0);
    let page_limit: Option<i32> = if limit > 0 { Some(limit) } else { None };
    let db = sdb::need().expect("database");
    match db
        .query_models::<Location>(
            &ctx(&req),
            "SELECT * FROM claimius.get_locations($1, $2, $3, $4)",
            &[&actor(&req), &required, &after, &page_limit],
        )
        .await
    {
        Ok(locs) => json(StatusCode::OK, &locs),
        Err(e) => access_error(&e),
    }
}
