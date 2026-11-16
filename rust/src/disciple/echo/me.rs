use axum::extract::Request;
use axum::response::Response;
use http::StatusCode;
use uuid::Uuid;

use super::respond::{get_request_id, respond_error, respond_json};
use crate::disciple::sdb;
use crate::shared::{AccessFormat, Body, Ctx, Error, SamnaUser};
use crate::smw::{Actor, RequestCtx};

pub fn actor(req: &Request) -> Option<Uuid> {
    req.extensions()
        .get::<Actor>()
        .map(|a| a.0)
        .filter(|a| !a.is_nil())
}

pub fn ctx(req: &Request) -> Ctx {
    req.extensions()
        .get::<RequestCtx>()
        .map(|c| c.0.clone())
        .unwrap_or_default()
}

pub fn query_values(req: &Request, name: &str) -> Vec<String> {
    req.uri()
        .query()
        .map(|q| {
            url::form_urlencoded::parse(q.as_bytes())
                .filter(|(k, _)| k == name)
                .map(|(_, v)| v.into_owned())
                .collect()
        })
        .unwrap_or_default()
}

pub fn query_value(req: &Request, name: &str) -> Option<String> {
    query_values(req, name).into_iter().next()
}

pub async fn get_me(req: Request) -> Response {
    let request_id = get_request_id(&req);
    let Some(db) = sdb::instance() else {
        return respond_error(request_id, StatusCode::NOT_FOUND, "Not found");
    };
    match db.get_me(&ctx(&req), actor(&req)).await {
        Ok(user) => respond_json(request_id, StatusCode::OK, user, 1),
        Err(e) => {
            tracing::error!("GetMe: {e}");
            respond_error(request_id, StatusCode::NOT_FOUND, "Not found")
        }
    }
}

async fn bind_user(req: Request) -> Result<(Request, SamnaUser), Response> {
    let request_id = get_request_id(&req);
    let (parts, body) = req.into_parts();
    let bytes = axum::body::to_bytes(body, 1 << 20)
        .await
        .map_err(|_| respond_error(request_id, StatusCode::BAD_REQUEST, "Bad request"))?;
    let (keys, value) = Body::bound(&bytes)
        .map_err(|_| respond_error(request_id, StatusCode::BAD_REQUEST, "Bad request"))?;
    let mut user: SamnaUser = serde_json::from_value(value)
        .map_err(|_| respond_error(request_id, StatusCode::BAD_REQUEST, "Bad request"))?;
    user.body = keys;
    Ok((Request::from_parts(parts, axum::body::Body::empty()), user))
}

async fn write_me(req: Request, patch: bool) -> Response {
    let request_id = get_request_id(&req);
    let (req, mut user) = match bind_user(req).await {
        Ok(v) => v,
        Err(response) => return response,
    };
    let validated = if patch {
        user.validate_patch()
    } else {
        user.validate_put()
    };
    if let Err(e) = validated {
        return respond_error(
            request_id,
            StatusCode::BAD_REQUEST,
            &e.to_string()
                .trim_start_matches("bad request: ")
                .to_string(),
        );
    }
    let Some(db) = sdb::instance() else {
        return respond_error(
            request_id,
            StatusCode::INTERNAL_SERVER_ERROR,
            "Internal server error",
        );
    };
    match db.update_me(&ctx(&req), actor(&req), &mut user).await {
        Ok(updated) => respond_json(request_id, StatusCode::OK, updated, 1),
        Err(e) if e.is_no_access() => respond_error(request_id, StatusCode::FORBIDDEN, "Forbidden"),
        Err(e) => {
            tracing::error!("{}: {e}", if patch { "PatchMe" } else { "PutMe" });
            respond_error(
                request_id,
                StatusCode::INTERNAL_SERVER_ERROR,
                "Internal server error",
            )
        }
    }
}

pub async fn put_me(req: Request) -> Response {
    write_me(req, false).await
}

pub async fn patch_me(req: Request) -> Response {
    write_me(req, true).await
}

pub async fn get_me_claims(req: Request) -> Response {
    let request_id = get_request_id(&req);
    let Some(db) = sdb::instance() else {
        return respond_error(
            request_id,
            StatusCode::INTERNAL_SERVER_ERROR,
            "Internal server error",
        );
    };
    match db
        .get_me_claims(&ctx(&req), actor(&req), &query_values(&req, "with"))
        .await
    {
        Ok(mut claims) => {
            for claim in &mut claims {
                claim.format_access(AccessFormat {
                    slice: true,
                    string: true,
                });
            }
            let count = claims.len() as i64;
            respond_json(request_id, StatusCode::OK, claims, count)
        }
        Err(e) => {
            tracing::error!("GetMeClaims: {e}");
            respond_error(
                request_id,
                StatusCode::INTERNAL_SERVER_ERROR,
                "Internal server error",
            )
        }
    }
}

pub async fn get_me_relations(req: Request) -> Response {
    let request_id = get_request_id(&req);
    let Some(db) = sdb::instance() else {
        return respond_error(
            request_id,
            StatusCode::INTERNAL_SERVER_ERROR,
            "Internal server error",
        );
    };
    match db.get_me_relations(&ctx(&req), actor(&req)).await {
        Ok(relations) => {
            let count = relations.len() as i64;
            respond_json(request_id, StatusCode::OK, relations, count)
        }
        Err(e) => {
            tracing::error!("GetMeRelations: {e}");
            respond_error(
                request_id,
                StatusCode::INTERNAL_SERVER_ERROR,
                "Internal server error",
            )
        }
    }
}

pub async fn get_me_organizations(req: Request) -> Response {
    let request_id = get_request_id(&req);
    let Some(db) = sdb::instance() else {
        return respond_error(
            request_id,
            StatusCode::INTERNAL_SERVER_ERROR,
            "Internal server error",
        );
    };
    let cursor = query_value(&req, "cursor");
    let limit = query_value(&req, "limit");
    match db
        .get_me_organizations(
            &ctx(&req),
            actor(&req),
            &query_values(&req, "with"),
            cursor.as_deref(),
            limit.as_deref(),
        )
        .await
    {
        Ok(orgs) => {
            let count = orgs.len() as i64;
            respond_json(request_id, StatusCode::OK, orgs, count)
        }
        Err(e) => {
            tracing::error!("GetMeOrganizations: {e}");
            respond_error(
                request_id,
                StatusCode::INTERNAL_SERVER_ERROR,
                "Internal server error",
            )
        }
    }
}

pub async fn get_me_locations(req: Request) -> Response {
    let request_id = get_request_id(&req);
    let Some(db) = sdb::instance() else {
        return respond_error(
            request_id,
            StatusCode::INTERNAL_SERVER_ERROR,
            "Internal server error",
        );
    };
    let cursor = query_value(&req, "cursor");
    let limit = query_value(&req, "limit");
    match db
        .get_me_locations(
            &ctx(&req),
            actor(&req),
            &query_values(&req, "with"),
            cursor.as_deref(),
            limit.as_deref(),
        )
        .await
    {
        Ok(locs) => {
            let count = locs.len() as i64;
            respond_json(request_id, StatusCode::OK, locs, count)
        }
        Err(e) => {
            tracing::error!("GetMeLocations: {e}");
            respond_error(
                request_id,
                StatusCode::INTERNAL_SERVER_ERROR,
                "Internal server error",
            )
        }
    }
}

pub async fn get_me_assets(req: Request) -> Response {
    let request_id = get_request_id(&req);
    let Some(db) = sdb::instance() else {
        return respond_error(
            request_id,
            StatusCode::INTERNAL_SERVER_ERROR,
            "Internal server error",
        );
    };
    match db.get_me_assets(&ctx(&req), actor(&req)).await {
        Ok(assets) => {
            let count = assets.len() as i64;
            respond_json(request_id, StatusCode::OK, assets, count)
        }
        Err(e) => {
            tracing::error!("GetMeAssets: {e}");
            respond_error(
                request_id,
                StatusCode::INTERNAL_SERVER_ERROR,
                "Internal server error",
            )
        }
    }
}

pub async fn get_me_audit(req: Request) -> Response {
    let request_id = get_request_id(&req);
    let Some(db) = sdb::instance() else {
        return respond_error(
            request_id,
            StatusCode::INTERNAL_SERVER_ERROR,
            "Internal server error",
        );
    };
    match db.get_me_audit(&ctx(&req), actor(&req)).await {
        Ok(audits) => {
            let count = audits.len() as i64;
            respond_json(request_id, StatusCode::OK, audits, count)
        }
        Err(e) => {
            tracing::error!("GetMeAudit: {e}");
            respond_error(
                request_id,
                StatusCode::INTERNAL_SERVER_ERROR,
                "Internal server error",
            )
        }
    }
}

pub fn status_of(err: &Error) -> StatusCode {
    crate::errorsx::err_to_http_code(err)
}
