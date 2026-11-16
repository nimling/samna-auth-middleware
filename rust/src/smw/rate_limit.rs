use std::collections::HashMap;
use std::net::SocketAddr;
use std::sync::{Arc, LazyLock, Mutex};
use std::time::{Duration, Instant};

use axum::extract::{ConnectInfo, Request, State};
use axum::middleware::Next;
use axum::response::{IntoResponse, Response};
use http::{HeaderMap, StatusCode};

use crate::analytics::ANONYMOUS_COOKIE;
use crate::utils::cfg;
use crate::utils::cookie::cookie_value;

const RATE_LIMIT_DEFAULT: i64 = 60;
const RATE_PERIOD_DEFAULT: Duration = Duration::from_secs(60);

struct RateWindow {
    count: i64,
    reset: Instant,
}

pub struct RateLimiter {
    windows: Mutex<HashMap<String, RateWindow>>,
    last_sweep: Mutex<Instant>,
    limit: Option<(i64, Duration)>,
}

static RATE_LIMIT: LazyLock<RateLimiter> = LazyLock::new(|| RateLimiter::new(None));

impl RateLimiter {
    pub fn new(limit: Option<(i64, Duration)>) -> RateLimiter {
        RateLimiter {
            windows: Mutex::new(HashMap::new()),
            last_sweep: Mutex::new(Instant::now()),
            limit,
        }
    }

    pub fn allow(&self, key: &str, limit: i64, period: Duration) -> bool {
        let now = Instant::now();
        let mut windows = self.windows.lock().expect("rate windows");
        {
            let mut sweep = self.last_sweep.lock().expect("rate sweep");
            if now.duration_since(*sweep) >= period {
                windows.retain(|_, w| now <= w.reset);
                *sweep = now;
            }
        }
        match windows.get_mut(key) {
            Some(w) if now <= w.reset => {
                if w.count >= limit {
                    return false;
                }
                w.count += 1;
                true
            }
            _ => {
                windows.insert(
                    key.to_string(),
                    RateWindow {
                        count: 1,
                        reset: now + period,
                    },
                );
                true
            }
        }
    }
}

pub fn rate_settings() -> (i64, Duration) {
    let c = cfg();
    let mut limit = RATE_LIMIT_DEFAULT;
    let mut period = RATE_PERIOD_DEFAULT;
    if let Ok(n) = c.rate_limit.parse::<i64>()
        && n > 0
    {
        limit = n;
    }
    if let Some(d) = parse_go_duration(&c.rate_period)
        && d > Duration::ZERO
    {
        period = d;
    }
    (limit, period)
}

pub fn parse_go_duration(text: &str) -> Option<Duration> {
    let text = text.trim();
    if text.is_empty() {
        return None;
    }
    let mut total = Duration::ZERO;
    let mut number = String::new();
    let mut unit = String::new();
    let mut chars = text.chars().peekable();
    while let Some(c) = chars.next() {
        if c.is_ascii_digit() || c == '.' {
            if !unit.is_empty() {
                total += unit_duration(&number, &unit)?;
                number.clear();
                unit.clear();
            }
            number.push(c);
        } else {
            unit.push(c);
        }
        if chars.peek().is_none() {
            total += unit_duration(&number, &unit)?;
        }
    }
    Some(total)
}

fn unit_duration(number: &str, unit: &str) -> Option<Duration> {
    let n: f64 = number.parse().ok()?;
    let secs = match unit {
        "ns" => n / 1e9,
        "us" | "µs" => n / 1e6,
        "ms" => n / 1e3,
        "s" => n,
        "m" => n * 60.0,
        "h" => n * 3600.0,
        _ => return None,
    };
    Some(Duration::from_secs_f64(secs))
}

pub fn real_ip(headers: &HeaderMap, addr: Option<SocketAddr>) -> String {
    if let Some(forwarded) = headers.get("X-Forwarded-For").and_then(|h| h.to_str().ok())
        && let Some(first) = forwarded.split(',').next()
        && !first.trim().is_empty()
    {
        return first.trim().to_string();
    }
    if let Some(real) = headers.get("X-Real-IP").and_then(|h| h.to_str().ok())
        && !real.is_empty()
    {
        return real.to_string();
    }
    addr.map(|a| a.ip().to_string()).unwrap_or_default()
}

pub fn rate_key(headers: &HeaderMap, addr: Option<SocketAddr>) -> String {
    let mut key = real_ip(headers, addr);
    if let Some(cookie) = cookie_value(headers, ANONYMOUS_COOKIE) {
        key.push('|');
        key.push_str(&cookie);
    }
    key
}

pub fn respond_rate_limited(period: Duration) -> Response {
    let mut response = (StatusCode::TOO_MANY_REQUESTS, "Too Many Requests").into_response();
    if let Ok(v) = period.as_secs().to_string().parse() {
        response.headers_mut().insert("Retry-After", v);
    }
    response
}

pub fn allow_default(key: &str) -> Result<(), Duration> {
    let (limit, period) = rate_settings();
    if RATE_LIMIT.allow(key, limit, period) {
        Ok(())
    } else {
        Err(period)
    }
}

pub async fn rate_limit(req: Request, next: Next) -> Response {
    let addr = req
        .extensions()
        .get::<ConnectInfo<SocketAddr>>()
        .map(|c| c.0);
    let key = rate_key(req.headers(), addr);
    if let Err(period) = allow_default(&key) {
        return respond_rate_limited(period);
    }
    next.run(req).await
}

pub fn rate_limiter(limit: i64, period: Duration) -> Arc<RateLimiter> {
    Arc::new(RateLimiter::new(Some((limit, period))))
}

pub async fn rate_limited(
    State(limiter): State<Arc<RateLimiter>>,
    req: Request,
    next: Next,
) -> Response {
    let addr = req
        .extensions()
        .get::<ConnectInfo<SocketAddr>>()
        .map(|c| c.0);
    let (limit, period) = limiter.limit.unwrap_or_else(rate_settings);
    if !limiter.allow(&real_ip(req.headers(), addr), limit, period) {
        return respond_rate_limited(period);
    }
    next.run(req).await
}
