use std::net::SocketAddr;
use std::sync::Mutex;
use std::time::Duration;

use axum::Router;
use axum::body::Body;
use axum::extract::Request;
use axum::response::{IntoResponse, Response};
use axum::routing::{any, get};
use http::{HeaderMap, HeaderValue, StatusCode};
use samna_auth_middleware::smw::{self, RateLimiter, UserAuth};
use samna_auth_middleware::utils;
use tower::ServiceExt;
use url::Url;

static ENV: Mutex<()> = Mutex::new(());

fn env_lock() -> std::sync::MutexGuard<'static, ()> {
    ENV.lock().unwrap_or_else(|e| e.into_inner())
}

async fn auth_server(refresh: Option<&'static str>) -> String {
    let app = Router::new().route(
        "/{*path}",
        any(move |req: Request| async move {
            if req.uri().path().ends_with("/jwks") {
                let mut response: Response = (StatusCode::OK, r#"{"keys":[]}"#).into_response();
                response.headers_mut().insert(
                    http::header::CONTENT_TYPE,
                    HeaderValue::from_static("application/json"),
                );
                return response;
            }
            match refresh {
                Some(body) => {
                    let mut response: Response = (StatusCode::OK, body).into_response();
                    response.headers_mut().insert(
                        http::header::CONTENT_TYPE,
                        HeaderValue::from_static("application/json"),
                    );
                    response
                }
                None => StatusCode::INTERNAL_SERVER_ERROR.into_response(),
            }
        }),
    );
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0")
        .await
        .expect("bind");
    let addr: SocketAddr = listener.local_addr().expect("addr");
    tokio::spawn(async move {
        axum::serve(listener, app).await.expect("serve");
    });
    format!("http://{addr}")
}

fn protected(server: &str) -> Router {
    unsafe {
        std::env::set_var("SAUTH_DEBUG_USER_ID", "");
        std::env::set_var("SAUTH_DEBUG_CLIENTS", "");
        std::env::set_var("SAUTH_APP_SLUG", "testapp");
        std::env::set_var("SAUTH_APP_SECRET", "secret");
        std::env::set_var(
            "SAUTH_REDIRECT_URI",
            "http://localhost:4200/api/auth/callback",
        );
    }
    let token_url =
        Url::parse(&format!("{server}/api/app/testapp/oauth/token")).expect("token url");
    let jwks_url = Url::parse(&format!("{server}/jwks")).expect("jwks url");
    utils::set_oidc(server, token_url, jwks_url);
    let auth = UserAuth::new(None);
    Router::new()
        .route("/{*path}", get(|| async { (StatusCode::OK, "ok") }))
        .layer(axum::middleware::from_fn_with_state(auth, smw::user_auth))
}

async fn call(router: Router, target: &str, headers: &[(&str, &str)]) -> (StatusCode, HeaderMap) {
    let mut builder = Request::builder().method("GET").uri(target);
    for (k, v) in headers {
        builder = builder.header(*k, *v);
    }
    let response = router
        .oneshot(builder.body(Body::empty()).expect("request"))
        .await
        .expect("response");
    (response.status(), response.headers().clone())
}

fn cookie(headers: &HeaderMap, name: &str) -> Option<(String, i64)> {
    for value in headers.get_all(http::header::SET_COOKIE) {
        let text = value.to_str().ok()?;
        let mut parts = text.split(';').map(str::trim);
        let first = parts.next()?;
        let (k, v) = first.split_once('=')?;
        if k != name {
            continue;
        }
        let max_age = parts
            .find_map(|p| p.strip_prefix("Max-Age="))
            .and_then(|a| a.parse().ok())
            .unwrap_or(0);
        return Some((v.to_string(), max_age));
    }
    None
}

#[tokio::test]
async fn refresh_auth_server_error_answers_unauthorized_on_fetch() {
    let _env = env_lock();
    let server = auth_server(None).await;
    let (status, _) = call(
        protected(&server),
        "/api/me/employee",
        &[("Cookie", "sauth_refresh_token=ref")],
    )
    .await;
    assert_eq!(status, StatusCode::UNAUTHORIZED);
}

#[tokio::test]
async fn refresh_auth_server_error_redirects_to_login_on_navigation() {
    let _env = env_lock();
    let server = auth_server(None).await;
    let (status, headers) = call(
        protected(&server),
        "/nimployment",
        &[
            ("Accept", "text/html"),
            ("Cookie", "sauth_refresh_token=ref"),
        ],
    )
    .await;
    assert_eq!(status, StatusCode::FOUND);
    assert_eq!(
        headers
            .get(http::header::LOCATION)
            .and_then(|h| h.to_str().ok()),
        Some("/api/auth/login")
    );
}

#[tokio::test]
async fn a_request_carrying_no_credential_is_refused_when_nothing_is_public() {
    let _env = env_lock();
    let server = auth_server(None).await;
    let (status, _) = call(protected(&server), "/api/me/employee", &[]).await;
    assert_eq!(status, StatusCode::UNAUTHORIZED);
}

#[tokio::test]
async fn a_refresh_keeps_the_csrf_token_the_browser_already_holds() {
    let _env = env_lock();
    let server = auth_server(Some(r#"{"access_token":"fresh","refresh_token":"next","token_type":"Bearer","expires_in":900,"refresh_expires_in":2592000}"#)).await;
    let (_, headers) = call(
        protected(&server),
        "/api/thing",
        &[
            ("Sec-Fetch-Mode", "cors"),
            ("Cookie", "sauth_refresh_token=old; sauth_csrf_token=held"),
        ],
    )
    .await;
    let (value, max_age) = cookie(&headers, "sauth_csrf_token")
        .expect("a refresh must rewrite the csrf cookie so it outlives a day");
    assert_eq!(
        value, "held",
        "a refresh must keep the csrf token the browser holds"
    );
    assert!(max_age > 0, "a refresh must extend the csrf cookie");
}

#[tokio::test]
async fn navigation_redirects_to_the_login_beside_the_redirect_uri() {
    let _env = env_lock();
    let server = auth_server(None).await;
    let router = protected(&server);
    unsafe {
        std::env::set_var(
            "SAUTH_REDIRECT_URI",
            "https://sign.example.test/auth/callback",
        )
    };
    let (status, headers) = call(
        router,
        "/screens",
        &[
            ("Accept", "text/html"),
            ("Cookie", "sauth_refresh_token=ref"),
        ],
    )
    .await;
    assert_eq!(status, StatusCode::FOUND);
    assert_eq!(
        headers
            .get(http::header::LOCATION)
            .and_then(|h| h.to_str().ok()),
        Some("/auth/login")
    );
}

#[tokio::test]
async fn get_origin_allowed_admits_the_initiate_app_origins() {
    let _env = env_lock();
    unsafe {
        std::env::set_var(
            "SAUTH_REDIRECT_URI",
            "http://localhost:4200/api/auth/callback",
        );
        std::env::set_var(
            "SAUTH_DEBUG_USER_ID",
            "a0000000-0000-4000-8000-00000000000a",
        );
        std::env::set_var(
            "SAUTH_DEBUG_INITIATE_APPS",
            "testapp-web=http://web.example",
        );
    }
    let origins = vec!["http://portal.example".to_string()];
    for (origin, want) in [
        ("http://portal.example", true),
        ("http://web.example", true),
        ("http://evil.example", false),
    ] {
        assert_eq!(
            smw::get_origin_allowed(&origins, origin).await,
            want,
            "origin {origin}"
        );
    }
    unsafe {
        std::env::set_var("SAUTH_DEBUG_USER_ID", "");
        std::env::set_var("SAUTH_DEBUG_INITIATE_APPS", "");
    }
}

#[test]
fn a_rate_limiter_counts_per_key_inside_its_window() {
    let limiter = RateLimiter::new(None);
    for _ in 0..3 {
        assert!(limiter.allow("a", 3, Duration::from_secs(60)));
    }
    assert!(
        !limiter.allow("a", 3, Duration::from_secs(60)),
        "the fourth call in the window is refused"
    );
    assert!(
        limiter.allow("b", 3, Duration::from_secs(60)),
        "another key has its own window"
    );
}

#[test]
fn a_go_duration_parses() {
    assert_eq!(
        smw::rate_limit::parse_go_duration("1m30s"),
        Some(Duration::from_secs(90))
    );
    assert_eq!(
        smw::rate_limit::parse_go_duration("250ms"),
        Some(Duration::from_millis(250))
    );
    assert_eq!(
        smw::rate_limit::parse_go_duration("2h"),
        Some(Duration::from_secs(7200))
    );
    assert_eq!(smw::rate_limit::parse_go_duration("soon"), None);
}
