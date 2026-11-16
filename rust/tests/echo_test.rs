use std::collections::HashMap;
use std::net::SocketAddr;
use std::sync::{Arc, Mutex};

use axum::Router;
use axum::body::Body;
use axum::extract::Request;
use axum::response::{IntoResponse, Response};
use axum::routing::{any, get, post};
use http::{HeaderMap, HeaderValue, StatusCode};
use samna_auth_middleware::client;
use samna_auth_middleware::disciple::echo as h;
use tower::ServiceExt;
use url::Url;

static ENV: Mutex<()> = Mutex::new(());

fn env_lock() -> std::sync::MutexGuard<'static, ()> {
    ENV.lock().unwrap_or_else(|e| e.into_inner())
}

const CLI_CHALLENGE: &str = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM";

#[derive(Default)]
struct FakeProphet {
    paths: Vec<String>,
    forms: Vec<HashMap<String, String>>,
    slugs: Vec<String>,
}

async fn serve_prophet(status: StatusCode, body: &'static str) -> Arc<Mutex<FakeProphet>> {
    let seen = Arc::new(Mutex::new(FakeProphet::default()));
    let record = seen.clone();
    let app = Router::new().route(
        "/{*path}",
        any(move |req: Request| {
            let record = record.clone();
            async move {
                let path = format!("{} {}", req.method(), req.uri().path());
                let slug = format!(
                    "{}:{}",
                    req.headers()
                        .get("SAuth-App-Slug")
                        .and_then(|h| h.to_str().ok())
                        .unwrap_or(""),
                    req.headers()
                        .get("SAuth-App-Secret")
                        .and_then(|h| h.to_str().ok())
                        .unwrap_or("")
                );
                let bytes = axum::body::to_bytes(req.into_body(), 1 << 20)
                    .await
                    .unwrap_or_default();
                let form: HashMap<String, String> = url::form_urlencoded::parse(&bytes)
                    .map(|(k, v)| (k.into_owned(), v.into_owned()))
                    .collect();
                let mut held = record.lock().expect("prophet");
                held.paths.push(path);
                held.forms.push(form);
                held.slugs.push(slug);
                let mut response: Response = (status, body).into_response();
                response.headers_mut().insert(
                    http::header::CONTENT_TYPE,
                    HeaderValue::from_static("application/json"),
                );
                response
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
    unsafe {
        std::env::set_var("SAUTH_ENDPOINT", format!("http://{addr}"));
        std::env::set_var("SAUTH_APP_SLUG", "testapp");
        std::env::set_var("SAUTH_APP_SECRET", "app-secret");
        std::env::set_var("SAUTH_REDIRECT_URI", "http://svc.example/api/auth/callback");
        std::env::set_var("SAUTH_DEBUG_USER_ID", "");
        std::env::set_var("SAUTH_DEBUG_CLIENTS", "");
    }
    client::bind("", "");
    seen
}

fn routes() -> Router {
    Router::new()
        .route("/api/auth/login", get(h::get_login))
        .route("/api/auth/callback", get(h::get_callback))
        .route("/api/auth/cli/token", post(h::post_cli_token))
        .route("/api/auth/cli/revoke", post(h::post_cli_revoke))
}

async fn call(
    method: &str,
    target: &str,
    form: Option<&[(&str, &str)]>,
    cookies: &[(&str, &str)],
) -> (StatusCode, HeaderMap, String) {
    let mut builder = Request::builder().method(method).uri(target);
    let body = match form {
        Some(form) => {
            builder = builder.header("Content-Type", "application/x-www-form-urlencoded");
            Body::from(
                url::form_urlencoded::Serializer::new(String::new())
                    .extend_pairs(form.iter())
                    .finish(),
            )
        }
        None => Body::empty(),
    };
    if !cookies.is_empty() {
        builder = builder.header(
            "Cookie",
            cookies
                .iter()
                .map(|(k, v)| format!("{k}={v}"))
                .collect::<Vec<_>>()
                .join("; "),
        );
    }
    let response = routes()
        .oneshot(builder.body(body).expect("request"))
        .await
        .expect("response");
    let status = response.status();
    let headers = response.headers().clone();
    let bytes = axum::body::to_bytes(response.into_body(), 1 << 20)
        .await
        .expect("body");
    (status, headers, String::from_utf8_lossy(&bytes).to_string())
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

fn location(headers: &HeaderMap) -> String {
    headers
        .get(http::header::LOCATION)
        .and_then(|h| h.to_str().ok())
        .unwrap_or("")
        .to_string()
}

#[tokio::test]
async fn get_login_sends_the_clis_challenge_and_keeps_no_verifier() {
    let _env = env_lock();
    serve_prophet(StatusCode::OK, "{}").await;
    let redirect: String =
        url::form_urlencoded::byte_serialize(b"http://127.0.0.1:53682/callback").collect();
    let (status, headers, _) = call("GET", &format!("/api/auth/login?cli_challenge={CLI_CHALLENGE}&cli_redirect={redirect}&cli_state=abcdefghijklmnop"), None, &[]).await;
    assert_eq!(
        status,
        StatusCode::TEMPORARY_REDIRECT,
        "location {}",
        location(&headers)
    );
    let parsed = Url::parse(&location(&headers)).expect("location");
    let query: HashMap<String, String> = parsed
        .query_pairs()
        .map(|(k, v)| (k.into_owned(), v.into_owned()))
        .collect();
    assert_eq!(
        query.get("code_challenge").map(String::as_str),
        Some(CLI_CHALLENGE),
        "want the cli's own challenge"
    );
    assert_eq!(
        query.get("redirect_uri").map(String::as_str),
        Some("http://svc.example/api/auth/callback"),
        "want the service's registered callback"
    );
    assert!(
        cookie(&headers, client::PKCE_VERIFIER_COOKIE).is_none_or(|(v, _)| v.is_empty()),
        "only the cli holds the verifier"
    );
    assert_eq!(
        cookie(&headers, client::CLI_TARGET_COOKIE).map(|(v, _)| v),
        Some("http://127.0.0.1:53682/callback?state=abcdefghijklmnop".to_string())
    );
}

#[tokio::test]
async fn get_login_refuses_a_non_loopback_redirect() {
    let _env = env_lock();
    serve_prophet(StatusCode::OK, "{}").await;
    for redirect in [
        "https://evil.example/callback",
        "http://127.0.0.1/callback",
        "http://user@127.0.0.1:8080/",
        "http://10.0.0.1:8080/",
    ] {
        let encoded: String = url::form_urlencoded::byte_serialize(redirect.as_bytes()).collect();
        let (status, _, _) = call("GET", &format!("/api/auth/login?cli_challenge={CLI_CHALLENGE}&cli_redirect={encoded}&cli_state=abcdefghijklmnop"), None, &[]).await;
        assert_eq!(status, StatusCode::BAD_REQUEST, "cli_redirect {redirect:?}");
    }
}

#[tokio::test]
async fn get_login_without_cli_clears_a_stale_cli_target() {
    let _env = env_lock();
    serve_prophet(StatusCode::OK, "{}").await;
    let (_, headers, _) = call(
        "GET",
        "/api/auth/login",
        None,
        &[(client::CLI_TARGET_COOKIE, "paste")],
    )
    .await;
    let (_, max_age) =
        cookie(&headers, client::CLI_TARGET_COOKIE).expect("cli target cookie cleared");
    assert!(
        max_age < 0,
        "want it cleared so a browser login is never handed to a cli"
    );
    assert!(
        cookie(&headers, client::PKCE_VERIFIER_COOKIE).is_some_and(|(v, _)| !v.is_empty()),
        "verifier cookie missing on a browser login"
    );
}

#[tokio::test]
async fn get_callback_hands_the_code_to_the_loopback_without_exchanging() {
    let _env = env_lock();
    let prophet = serve_prophet(StatusCode::OK, "{}").await;
    let cookies = [
        (client::OAUTH_STATE_COOKIE, "st1"),
        (
            client::CLI_TARGET_COOKIE,
            "http://127.0.0.1:53682/callback?state=abcdefghijklmnop",
        ),
    ];
    let (status, headers, _) = call(
        "GET",
        "/api/auth/callback?code=thecode&state=st1",
        None,
        &cookies,
    )
    .await;
    assert_eq!(status, StatusCode::FOUND);
    assert_eq!(
        location(&headers),
        "http://127.0.0.1:53682/callback?state=abcdefghijklmnop&code=thecode"
    );
    assert!(
        prophet.lock().expect("prophet").paths.is_empty(),
        "the cli exchanges the code with its verifier"
    );
    assert!(
        cookie(&headers, "sauth_access_token").is_none_or(|(v, _)| v.is_empty()),
        "access cookie written on a cli login"
    );
}

#[tokio::test]
async fn get_callback_shows_the_code_to_paste_when_the_cli_has_no_loopback() {
    let _env = env_lock();
    serve_prophet(StatusCode::OK, "{}").await;
    let (status, headers, body) = call(
        "GET",
        "/api/auth/callback?code=thecode&state=st1",
        None,
        &[
            (client::OAUTH_STATE_COOKIE, "st1"),
            (client::CLI_TARGET_COOKIE, "paste"),
        ],
    )
    .await;
    assert!(
        status == StatusCode::OK && body.contains("thecode"),
        "status {status}, want the paste page with the code"
    );
    assert_eq!(
        headers.get("Cache-Control").and_then(|h| h.to_str().ok()),
        Some("no-store")
    );
    assert_eq!(
        headers.get("X-Frame-Options").and_then(|h| h.to_str().ok()),
        Some("DENY")
    );
}

#[tokio::test]
async fn get_callback_refuses_a_cli_login_with_a_foreign_state() {
    let _env = env_lock();
    serve_prophet(StatusCode::OK, "{}").await;
    let (status, headers, body) = call(
        "GET",
        "/api/auth/callback?code=thecode&state=other",
        None,
        &[
            (client::OAUTH_STATE_COOKIE, "st1"),
            (client::CLI_TARGET_COOKIE, "paste"),
        ],
    )
    .await;
    assert!(
        status == StatusCode::TEMPORARY_REDIRECT && !body.contains("thecode"),
        "status {status}, want the login asked for afresh without showing the code"
    );
    let loc = location(&headers);
    assert!(
        loc.starts_with("/api/auth/login?") && loc.contains("prompt=login"),
        "location {loc}"
    );
}

#[tokio::test]
async fn post_cli_token_exchanges_the_code_with_the_clis_verifier() {
    let _env = env_lock();
    let prophet = serve_prophet(StatusCode::OK, r#"{"access_token":"acc","refresh_token":"ref","expires_in":900,"refresh_expires_in":2592000,"token_type":"Bearer"}"#).await;
    let (status, headers, body) = call(
        "POST",
        "/api/auth/cli/token",
        Some(&[
            ("grant_type", "authorization_code"),
            ("code", "thecode"),
            ("code_verifier", "theverifier"),
        ]),
        &[],
    )
    .await;
    assert_eq!(status, StatusCode::OK, "body {body}");
    let got: serde_json::Value = serde_json::from_str(&body).expect("json");
    assert_eq!(got["access_token"], "acc");
    assert_eq!(got["refresh_token"], "ref");
    assert_eq!(got["expires_in"], 900);
    assert_eq!(got["token_type"], "Bearer");
    let held = prophet.lock().expect("prophet");
    assert_eq!(held.paths[0], "POST /api/app/testapp/oauth/token");
    assert_eq!(
        held.forms[0].get("code").map(String::as_str),
        Some("thecode")
    );
    assert_eq!(
        held.forms[0].get("code_verifier").map(String::as_str),
        Some("theverifier")
    );
    assert_eq!(
        held.forms[0].get("redirect_uri").map(String::as_str),
        Some("http://svc.example/api/auth/callback")
    );
    assert_eq!(held.slugs[0], "testapp:app-secret");
    assert_eq!(
        headers.get("Cache-Control").and_then(|h| h.to_str().ok()),
        Some("no-store")
    );
}

#[tokio::test]
async fn post_cli_token_relays_the_prophets_refusal() {
    let _env = env_lock();
    serve_prophet(StatusCode::BAD_REQUEST, r#"{"error":"invalid_grant","error_description":"code_verifier does not match code_challenge"}"#).await;
    let (status, _, body) = call(
        "POST",
        "/api/auth/cli/token",
        Some(&[
            ("grant_type", "authorization_code"),
            ("code", "thecode"),
            ("code_verifier", "wrong"),
        ]),
        &[],
    )
    .await;
    assert!(
        status == StatusCode::BAD_REQUEST && body.contains(r#""error":"invalid_grant""#),
        "status {status} body {body}"
    );
}

#[tokio::test]
async fn post_cli_token_refuses_an_unknown_grant() {
    let _env = env_lock();
    let prophet = serve_prophet(StatusCode::OK, "{}").await;
    let (status, _, body) = call(
        "POST",
        "/api/auth/cli/token",
        Some(&[("grant_type", "password")]),
        &[],
    )
    .await;
    assert!(
        status == StatusCode::BAD_REQUEST && body.contains("unsupported_grant_type"),
        "status {status} body {body}"
    );
    assert!(prophet.lock().expect("prophet").paths.is_empty());
}

#[tokio::test]
async fn post_cli_revoke_revokes_the_clis_refresh_family() {
    let _env = env_lock();
    let prophet = serve_prophet(StatusCode::OK, "").await;
    let (status, _, _) = call(
        "POST",
        "/api/auth/cli/revoke",
        Some(&[("token", "ref"), ("token_type_hint", "refresh_token")]),
        &[],
    )
    .await;
    assert_eq!(status, StatusCode::OK);
    let held = prophet.lock().expect("prophet");
    assert_eq!(held.paths[0], "POST /api/app/testapp/oauth/revoke");
    assert_eq!(held.forms[0].get("token").map(String::as_str), Some("ref"));
}

#[tokio::test]
async fn post_cli_token_relays_a_dead_refresh_token_as_invalid_grant() {
    let _env = env_lock();
    serve_prophet(
        StatusCode::UNAUTHORIZED,
        r#"{"error":"invalid_grant","error_description":"refresh token expired"}"#,
    )
    .await;
    let (status, _, body) = call(
        "POST",
        "/api/auth/cli/token",
        Some(&[("grant_type", "refresh_token"), ("refresh_token", "dead")]),
        &[],
    )
    .await;
    assert!(
        status == StatusCode::BAD_REQUEST && body.contains(r#""error":"invalid_grant""#),
        "status {status} body {body}, want 400 invalid_grant so the cli asks to log in again"
    );
}

#[tokio::test]
async fn post_cli_token_hides_the_services_own_client_failure() {
    let _env = env_lock();
    serve_prophet(
        StatusCode::UNAUTHORIZED,
        r#"{"error":"invalid_client","error_description":"invalid app secret"}"#,
    )
    .await;
    let (status, _, body) = call(
        "POST",
        "/api/auth/cli/token",
        Some(&[("grant_type", "refresh_token"), ("refresh_token", "any")]),
        &[],
    )
    .await;
    assert!(
        status == StatusCode::BAD_GATEWAY && body.contains(r#""error":"server_error""#),
        "status {status} body {body}: the service's own secret is not the cli's fault"
    );
}
