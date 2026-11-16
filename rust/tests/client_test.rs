use std::net::SocketAddr;
use std::sync::{Arc, Mutex};

use axum::Router;
use axum::extract::Request;
use axum::response::{IntoResponse, Response};
use axum::routing::any;
use http::{HeaderMap, HeaderValue, StatusCode};
use samna_auth_middleware::client::{
    self, Ctx, ErrorKind, SyncCursor, Target, get_asset_range, join_url, parse_sync_cursor,
};
use serde_json::Value;

static ENV: Mutex<()> = Mutex::new(());

fn env_lock() -> std::sync::MutexGuard<'static, ()> {
    ENV.lock().unwrap_or_else(|e| e.into_inner())
}

#[derive(Clone, Default)]
struct Seen {
    paths: Vec<String>,
    headers: Vec<HeaderMap>,
}

async fn fake_prophet(
    status: StatusCode,
    headers: Vec<(&'static str, String)>,
    body: &'static str,
) -> (String, Arc<Mutex<Seen>>) {
    let seen = Arc::new(Mutex::new(Seen::default()));
    let record = seen.clone();
    let app = Router::new().route(
        "/{*path}",
        any(move |req: Request| {
            let record = record.clone();
            let headers = headers.clone();
            async move {
                record.lock().expect("seen").paths.push(format!(
                    "{} {}",
                    req.method(),
                    req.uri().path()
                ));
                record
                    .lock()
                    .expect("seen")
                    .headers
                    .push(req.headers().clone());
                let mut response: Response = (status, body).into_response();
                response.headers_mut().insert(
                    http::header::CONTENT_TYPE,
                    HeaderValue::from_static("application/json"),
                );
                for (k, v) in headers {
                    response
                        .headers_mut()
                        .insert(k, HeaderValue::from_str(&v).expect("header"));
                }
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
    (format!("http://{addr}"), seen)
}

#[test]
fn join_url_relative_segment_stays_under_base() {
    assert_eq!(
        join_url("https://auth.example.io/sam", &["user/token"]).expect("join"),
        "https://auth.example.io/sam/user/token"
    );
}

#[test]
fn join_url_query_segment_keeps_query() {
    assert_eq!(
        join_url("https://auth.example.io/sam", &["consent?anonymous_id=abc"]).expect("join"),
        "https://auth.example.io/sam/consent?anonymous_id=abc"
    );
}

#[test]
fn join_url_absolute_segment_drops_base_path() {
    assert_eq!(
        join_url(
            "https://auth.example.io/sam",
            &["/api/app/docs/subscription/client"]
        )
        .expect("join"),
        "https://auth.example.io/api/app/docs/subscription/client"
    );
}

#[test]
fn an_asset_range_answers_the_bytes_a_range_request_asks_for() {
    let etag = "\"v1\"";
    let modified = "Tue, 13 Oct 2026 10:00:00 GMT";
    let cases: Vec<(&str, Vec<(&str, &str)>, i64, StatusCode, i64, i64, &str)> = vec![
        (
            "no range answers the whole body",
            vec![],
            10,
            StatusCode::OK,
            0,
            10,
            "",
        ),
        (
            "a closed range",
            vec![("Range", "bytes=2-5")],
            10,
            StatusCode::PARTIAL_CONTENT,
            2,
            4,
            "bytes 2-5/10",
        ),
        (
            "an open range runs to the end",
            vec![("Range", "bytes=7-")],
            10,
            StatusCode::PARTIAL_CONTENT,
            7,
            3,
            "bytes 7-9/10",
        ),
        (
            "a suffix range takes the last bytes",
            vec![("Range", "bytes=-3")],
            10,
            StatusCode::PARTIAL_CONTENT,
            7,
            3,
            "bytes 7-9/10",
        ),
        (
            "a suffix longer than the asset takes all of it",
            vec![("Range", "bytes=-50")],
            10,
            StatusCode::PARTIAL_CONTENT,
            0,
            10,
            "bytes 0-9/10",
        ),
        (
            "an end past the asset stops at its last byte",
            vec![("Range", "bytes=8-100")],
            10,
            StatusCode::PARTIAL_CONTENT,
            8,
            2,
            "bytes 8-9/10",
        ),
        (
            "a start past the asset is unsatisfiable",
            vec![("Range", "bytes=10-12")],
            10,
            StatusCode::RANGE_NOT_SATISFIABLE,
            0,
            0,
            "bytes */10",
        ),
        (
            "an empty suffix is unsatisfiable",
            vec![("Range", "bytes=-0")],
            10,
            StatusCode::RANGE_NOT_SATISFIABLE,
            0,
            0,
            "bytes */10",
        ),
        (
            "any range of an empty asset is unsatisfiable",
            vec![("Range", "bytes=0-")],
            0,
            StatusCode::RANGE_NOT_SATISFIABLE,
            0,
            0,
            "bytes */0",
        ),
        (
            "several ranges answer the whole body",
            vec![("Range", "bytes=0-1,4-5")],
            10,
            StatusCode::OK,
            0,
            10,
            "",
        ),
        (
            "a unit other than bytes answers the whole body",
            vec![("Range", "items=0-1")],
            10,
            StatusCode::OK,
            0,
            10,
            "",
        ),
        (
            "a malformed range answers the whole body",
            vec![("Range", "bytes=5-2")],
            10,
            StatusCode::OK,
            0,
            10,
            "",
        ),
        (
            "an unknown size answers the whole body",
            vec![("Range", "bytes=0-1")],
            -1,
            StatusCode::OK,
            0,
            -1,
            "",
        ),
        (
            "If-Range with the current ETag keeps the range",
            vec![("Range", "bytes=0-1"), ("If-Range", etag)],
            10,
            StatusCode::PARTIAL_CONTENT,
            0,
            2,
            "bytes 0-1/10",
        ),
        (
            "If-Range with an older ETag answers the whole body",
            vec![("Range", "bytes=0-1"), ("If-Range", "\"v0\"")],
            10,
            StatusCode::OK,
            0,
            10,
            "",
        ),
        (
            "If-Range with a weak ETag answers the whole body",
            vec![("Range", "bytes=0-1"), ("If-Range", "W/\"v1\"")],
            10,
            StatusCode::OK,
            0,
            10,
            "",
        ),
        (
            "If-Range with the current Last-Modified keeps the range",
            vec![("Range", "bytes=0-1"), ("If-Range", modified)],
            10,
            StatusCode::PARTIAL_CONTENT,
            0,
            2,
            "bytes 0-1/10",
        ),
        (
            "If-Range with an older date answers the whole body",
            vec![
                ("Range", "bytes=0-1"),
                ("If-Range", "Mon, 12 Oct 2026 10:00:00 GMT"),
            ],
            10,
            StatusCode::OK,
            0,
            10,
            "",
        ),
    ];
    for (name, header, size, status, start, length, content) in cases {
        let mut headers = HeaderMap::new();
        for (k, v) in header {
            headers.insert(k, HeaderValue::from_str(v).expect("header"));
        }
        let r = get_asset_range(&headers, size, etag, modified);
        assert!(
            r.status == status && r.start == start && r.length == length,
            "{name}: got status {} start {} length {}",
            r.status,
            r.start,
            r.length
        );
        if !content.is_empty() {
            assert_eq!(r.get_content_range(), content, "{name}");
        }
    }
}

#[tokio::test]
async fn with_sync_seq_records_the_sequence_of_a_write() {
    let _env = env_lock();
    let (url, _) = fake_prophet(
        StatusCode::OK,
        vec![(client::HEADER_SYNC_SEQ, "7.42".to_string())],
        r#"{"data":{},"count":1}"#,
    )
    .await;
    unsafe { std::env::set_var("SAUTH_ENDPOINT", &url) };
    client::bind("", "");
    let seq = Arc::new(Mutex::new(SyncCursor::default()));
    let ctx = Ctx::new().with_sync_seq(seq.clone());
    let _: Value = client::post(&ctx, "organization", &serde_json::json!({}))
        .await
        .expect("post");
    assert_eq!(
        *seq.lock().expect("seq"),
        SyncCursor { txid: 7, seq: 42 },
        "the write must record the prophet's position"
    );
}

#[test]
fn sync_cursor_orders_by_transaction_then_sequence() {
    let early = SyncCursor { txid: 10, seq: 900 };
    let late = SyncCursor { txid: 11, seq: 5 };
    assert!(
        early.less(late) && !late.less(early),
        "a lower transaction comes first whatever its sequence"
    );
    assert!(
        SyncCursor { txid: 11, seq: 4 }.less(late),
        "within one transaction the sequence orders"
    );
    assert_eq!(
        parse_sync_cursor(&late.to_string()).expect("round trip"),
        late
    );
    for bad in ["", "12", "a.1", "1.b", "-1.2"] {
        assert!(parse_sync_cursor(bad).is_err(), "{bad:?} must not parse");
    }
}

#[tokio::test]
async fn with_target_reaches_another_prophet_and_records_who_answered() {
    let _env = env_lock();
    let (url, seen) = fake_prophet(
        StatusCode::OK,
        vec![(client::HEADER_PROPHET, "peer".to_string())],
        r#"{"data":[],"count":0}"#,
    )
    .await;
    unsafe { std::env::set_var("SAUTH_ENDPOINT", "http://127.0.0.1:1") };
    client::bind("", "");
    let prophet = Arc::new(Mutex::new(String::new()));
    let ctx = Ctx::new()
        .with_target(Target {
            endpoint: url,
            slug: "mirrored".to_string(),
            secret: "s".to_string(),
            peer_secret: String::new(),
        })
        .with_prophet(prophet.clone());
    let _: Value = client::get(&ctx, "sync/0.0").await.expect("get");
    let headers = seen.lock().expect("seen").headers[0].clone();
    assert_eq!(
        headers.get("SAuth-App-Slug").and_then(|h| h.to_str().ok()),
        Some("mirrored")
    );
    assert_eq!(
        headers
            .get("SAuth-App-Secret")
            .and_then(|h| h.to_str().ok()),
        Some("s")
    );
    assert_eq!(*prophet.lock().expect("prophet"), "peer");
}

#[tokio::test]
async fn a_refusal_naming_the_home_is_not_home() {
    let _env = env_lock();
    let (url, _) = fake_prophet(
        StatusCode::CONFLICT,
        vec![(client::HEADER_HOME, "elsewhere".to_string())],
        "",
    )
    .await;
    unsafe { std::env::set_var("SAUTH_ENDPOINT", &url) };
    client::bind("", "");
    let err = client::post::<Value, Value>(&Ctx::new(), "claim", &serde_json::json!({}))
        .await
        .expect_err("a 409 naming a home");
    assert!(
        err.is(ErrorKind::NotHome)
            && err.is(ErrorKind::Conflict)
            && err.to_string().contains("elsewhere"),
        "got {err}"
    );
}

#[tokio::test]
async fn bind_points_requests_at_the_bound_prophet() {
    let _env = env_lock();
    let (url, seen) = fake_prophet(StatusCode::OK, vec![], r#"{"data":{},"count":1}"#).await;
    unsafe { std::env::set_var("SAUTH_ENDPOINT", "http://127.0.0.1:1") };
    client::bind(&url, "");
    let result: Result<Value, _> =
        client::post(&Ctx::new(), "organization", &serde_json::json!({})).await;
    client::bind("", "");
    assert!(
        result.is_ok(),
        "a request must go to the bound prophet, got {result:?}"
    );
    assert!(!seen.lock().expect("seen").paths.is_empty());
}

#[tokio::test]
async fn a_refused_write_carries_the_prophets_reason() {
    let _env = env_lock();
    let (url, _) = fake_prophet(
        StatusCode::BAD_REQUEST,
        vec![],
        r#"{"data":[],"error":{"Message":"type invalid"},"count":0}"#,
    )
    .await;
    unsafe { std::env::set_var("SAUTH_ENDPOINT", &url) };
    client::bind("", "");
    let err = client::post::<Value, Value>(&Ctx::new(), "claim", &serde_json::json!({}))
        .await
        .expect_err("a refused write");
    assert!(
        err.is(ErrorKind::BadRequest) && err.to_string().contains("type invalid"),
        "got {err}"
    );
}
