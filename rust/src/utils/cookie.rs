use axum_extra::extract::cookie::{Cookie, SameSite};
use http::HeaderMap;
use http::header::{COOKIE, SET_COOKIE};

use super::config::cfg;
use super::env::cookie_domain_from_uri;

pub const COOKIE_SCOPE_KEY: &str = "sauth_cookie_scope";

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct CookieScope {
    pub domain: String,
    pub same_site: String,
}

pub struct CookieWrite {
    pub name: String,
    pub value: String,
    pub path: String,
    pub max_age: i64,
}

impl CookieWrite {
    pub fn new(name: &str, value: &str, path: &str, max_age: i64) -> Self {
        CookieWrite {
            name: name.to_string(),
            value: value.to_string(),
            path: path.to_string(),
            max_age,
        }
    }
}

fn cookie_same_site(scope: Option<&CookieScope>) -> SameSite {
    let mut same_site = std::env::var("SAUTH_COOKIE_SAMESITE").unwrap_or_default();
    if let Some(scope) = scope
        && !scope.same_site.is_empty()
    {
        same_site = scope.same_site.clone();
    }
    match same_site.to_lowercase().as_str() {
        "none" => SameSite::None,
        "strict" => SameSite::Strict,
        _ => SameSite::Lax,
    }
}

fn cookie_domain(scope: Option<&CookieScope>) -> (String, bool) {
    let mut domain = std::env::var("SAUTH_COOKIE_DOMAIN").unwrap_or_default();
    if domain.is_empty() {
        domain = cookie_domain_from_uri(&cfg().redirect_uri);
    }
    let mut secure = !domain.is_empty();
    if let Some(scope) = scope {
        domain = scope.domain.clone();
        secure = true;
    }
    if let Ok(v) = std::env::var("SAUTH_COOKIE_SECURE")
        && !v.is_empty()
    {
        secure = v == "true";
    }
    if cookie_same_site(scope) == SameSite::None {
        secure = true;
    }
    (domain, secure)
}

fn build(
    cookie: &CookieWrite,
    http_only: bool,
    scope: Option<&CookieScope>,
    domain: Option<&str>,
) -> Cookie<'static> {
    let (resolved, secure) = cookie_domain(scope);
    let mut built = Cookie::build((cookie.name.clone(), cookie.value.clone()))
        .http_only(http_only)
        .secure(secure)
        .same_site(cookie_same_site(scope))
        .path(cookie.path.clone())
        .max_age(time_duration(cookie.max_age))
        .build();
    let domain = domain.map(str::to_string).unwrap_or(resolved);
    if !domain.is_empty() {
        built.set_domain(domain);
    }
    built
}

fn time_duration(seconds: i64) -> time::Duration {
    time::Duration::seconds(seconds)
}

pub fn write_cookie(headers: &mut HeaderMap, scope: Option<&CookieScope>, cookie: &CookieWrite) {
    push(headers, build(cookie, true, scope, None));
}

pub fn write_public_cookie(
    headers: &mut HeaderMap,
    scope: Option<&CookieScope>,
    cookie: &CookieWrite,
) {
    push(headers, build(cookie, false, scope, None));
}

pub fn clear_cookie(headers: &mut HeaderMap, scope: Option<&CookieScope>, name: &str) {
    let (domain, _) = cookie_domain(scope);
    for target in clear_domains(scope, &domain) {
        let write = CookieWrite::new(name, "", "/", -1);
        push(headers, build(&write, true, scope, Some(&target)));
    }
}

fn clear_domains(scope: Option<&CookieScope>, domain: &str) -> Vec<String> {
    let mut domains = vec![domain.to_string()];
    let redirect = cookie_domain_from_uri(&cfg().redirect_uri);
    if !redirect.is_empty() && redirect != domain {
        domains.push(redirect.clone());
    }
    if let Some(scope) = scope
        && !scope.domain.is_empty()
        && scope.domain != domain
        && scope.domain != redirect
    {
        domains.push(scope.domain.clone());
    }
    domains
}

fn push(headers: &mut HeaderMap, cookie: Cookie<'static>) {
    if let Ok(value) = cookie.to_string().parse() {
        headers.append(SET_COOKIE, value);
    }
}

pub fn cookie_values(headers: &HeaderMap, name: &str) -> Vec<String> {
    let mut values = Vec::with_capacity(2);
    for header in headers.get_all(COOKIE) {
        let Ok(text) = header.to_str() else { continue };
        for part in text.split(';') {
            let Some((k, v)) = part.trim().split_once('=') else {
                continue;
            };
            if k == name && !v.is_empty() {
                values.push(v.to_string());
            }
        }
    }
    values
}

pub fn cookie_value(headers: &HeaderMap, name: &str) -> Option<String> {
    cookie_values(headers, name).into_iter().next()
}
