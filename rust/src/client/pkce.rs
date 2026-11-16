use base64::Engine;
use base64::engine::general_purpose::URL_SAFE_NO_PAD;
use rand::RngCore;
use sha2::{Digest, Sha256};
use url::Url;
use uuid::Uuid;

use super::utils::join_url;
use crate::utils::cfg;

pub const PKCE_VERIFIER_COOKIE: &str = "sauth_pkce_verifier";
pub const OAUTH_STATE_COOKIE: &str = "sauth_oauth_state";
pub const LOGIN_NEXT_COOKIE: &str = "sauth_login_next";
pub const CLI_TARGET_COOKIE: &str = "sauth_cli_target";
pub const LOGIN_RETRY_COOKIE: &str = "sauth_login_retry";

pub fn generate_verifier() -> String {
    let mut b = [0u8; 32];
    rand::rng().fill_bytes(&mut b);
    URL_SAFE_NO_PAD.encode(b)
}

pub fn challenge_s256(verifier: &str) -> String {
    let sum = Sha256::digest(verifier.as_bytes());
    URL_SAFE_NO_PAD.encode(sum)
}

pub fn authorize_url(
    slug: &str,
    redirect_uri: &str,
    state: &str,
    challenge: &str,
    params: &[(String, String)],
) -> Result<String, String> {
    let endpoint = cfg().endpoint;
    let joined = join_url(&endpoint, &[&format!("api/app/{slug}/oauth/authorize")])
        .map_err(|e| format!("build authorize url: {e}"))?;
    let mut parsed = Url::parse(&joined).map_err(|e| format!("parse authorize url: {e}"))?;
    {
        let mut q = parsed.query_pairs_mut();
        q.append_pair("response_type", "code");
        q.append_pair("client_id", slug);
        q.append_pair("redirect_uri", redirect_uri);
        q.append_pair("state", state);
        q.append_pair("code_challenge", challenge);
        q.append_pair("code_challenge_method", "S256");
        for (key, value) in params {
            q.append_pair(key, value);
        }
    }
    Ok(parsed.to_string())
}

pub fn token_url_for(slug: &str) -> Result<String, String> {
    join_url(&cfg().endpoint, &[&format!("api/app/{slug}/oauth/token")])
}

pub fn end_session_url(slug: &str, page_redirect: &str) -> Result<String, String> {
    let joined = join_url(
        &cfg().endpoint,
        &[&format!("api/app/{slug}/oauth/end_session")],
    )
    .map_err(|e| format!("build end session url: {e}"))?;
    let mut parsed = Url::parse(&joined).map_err(|e| format!("parse end session url: {e}"))?;
    if !page_redirect.is_empty() {
        parsed
            .query_pairs_mut()
            .append_pair("post_logout_redirect_uri", page_redirect);
    }
    Ok(parsed.to_string())
}

pub fn revoke_url_for(slug: &str) -> Result<String, String> {
    join_url(&cfg().endpoint, &[&format!("api/app/{slug}/oauth/revoke")])
}

pub fn get_profile_link(user_id: Uuid) -> String {
    format!("{}/u/{user_id}", cfg().endpoint.trim_end_matches('/'))
}

pub const CODE_PARAM: &str = "samk";
pub const CODE_HEADER: &str = "SAuth-Code";

pub fn get_code_url(base: &str, path: &str, value: &str) -> Result<String, String> {
    let mut u = Url::parse(base).map_err(|e| format!("code url base: {e}"))?;
    if !path.is_empty() {
        u = u.join(path).map_err(|e| format!("code url path: {e}"))?;
    }
    u.query_pairs_mut().append_pair(CODE_PARAM, value);
    Ok(u.to_string())
}
