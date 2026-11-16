use std::time::Duration;

use base64::Engine;
use base64::engine::general_purpose::URL_SAFE_NO_PAD;
use chrono::Utc;
use jsonwebtoken::jwk::Jwk;
use jsonwebtoken::{Algorithm, DecodingKey, Validation, decode, decode_header};
use serde_json::Value;
use sha2::{Digest, Sha256};
use url::Url;

use crate::shared::{Ctx, Db, Error, Result};

pub const HEADER: &str = "DPoP";
pub const TYP: &str = "dpop+jwt";
pub const REPLAY_TTL: Duration = Duration::from_secs(300);
pub const CLOCK_SKEW: i64 = 60;

pub struct Proof {
    pub jkt: String,
    pub jti: String,
}

pub struct PostgresReplayStore {
    pub db: &'static Db,
}

impl PostgresReplayStore {
    pub async fn check_and_store(&self, jti: &str, ttl: Duration) -> Result<bool> {
        let expires = Utc::now() + chrono::Duration::from_std(ttl).unwrap_or_default();
        let n = self.db.execute(&Ctx::new(), "INSERT INTO claimius.dpop_jti (jti, expires_at) VALUES ($1, $2) ON CONFLICT (jti) DO NOTHING", &[&jti, &expires]).await?;
        if rand::random_range(0..100) == 0 {
            let _ = self
                .db
                .execute(
                    &Ctx::new(),
                    "DELETE FROM claimius.dpop_jti WHERE expires_at < now()",
                    &[],
                )
                .await;
        }
        Ok(n > 0)
    }
}

fn thumbprint(jwk: &Value) -> Result<String> {
    let kty = jwk.get("kty").and_then(|v| v.as_str()).unwrap_or("");
    let canonical = match kty {
        "RSA" => serde_json::json!({ "e": jwk.get("e"), "kty": "RSA", "n": jwk.get("n") }),
        "EC" => {
            serde_json::json!({ "crv": jwk.get("crv"), "kty": "EC", "x": jwk.get("x"), "y": jwk.get("y") })
        }
        "OKP" => serde_json::json!({ "crv": jwk.get("crv"), "kty": "OKP", "x": jwk.get("x") }),
        other => {
            return Err(Error::other(format!(
                "dpop jwk thumbprint: unsupported kty {other}"
            )));
        }
    };
    let text = serde_json::to_string(&canonical)?;
    Ok(URL_SAFE_NO_PAD.encode(Sha256::digest(text.as_bytes())))
}

pub async fn verify(
    method: &str,
    scheme: &str,
    host: &str,
    path: &str,
    header: &str,
    expected_access_token: &str,
    store: &PostgresReplayStore,
) -> Result<Option<Proof>> {
    if header.is_empty() {
        return Ok(None);
    }
    let parsed =
        decode_header(header).map_err(|e| Error::other(format!("parse dpop proof: {e}")))?;
    if parsed.typ.as_deref() != Some(TYP) {
        return Err(Error::other("dpop proof typ must be dpop+jwt"));
    }
    let Some(jwk) = parsed.jwk.clone() else {
        return Err(Error::other("dpop proof missing jwk header"));
    };
    let jwk_value =
        serde_json::to_value(&jwk).map_err(|e| Error::other(format!("marshal dpop jwk: {e}")))?;
    let jkt = thumbprint(&jwk_value)?;
    let key =
        DecodingKey::from_jwk(&jwk).map_err(|e| Error::other(format!("parse dpop jwk: {e}")))?;
    if !matches!(
        parsed.alg,
        Algorithm::RS256 | Algorithm::PS256 | Algorithm::ES256
    ) {
        return Err(Error::other("verify dpop signature: algorithm not allowed"));
    }
    let mut validation = Validation::new(parsed.alg);
    validation.validate_exp = false;
    validation.validate_aud = false;
    validation.validate_nbf = false;
    validation.required_spec_claims.clear();
    let data = decode::<Value>(header, &key, &validation)
        .map_err(|e| Error::other(format!("verify dpop signature: {e}")))?;
    let claims = data.claims;
    let htm = claims.get("htm").and_then(|v| v.as_str()).unwrap_or("");
    if !htm.eq_ignore_ascii_case(method) {
        return Err(Error::other(format!(
            "dpop htm mismatch, got={htm} want={method}"
        )));
    }
    let htu = claims.get("htu").and_then(|v| v.as_str()).unwrap_or("");
    if !same_url(htu, scheme, host, path) {
        return Err(Error::other(format!("dpop htu mismatch, got={htu}")));
    }
    let iat = claims.get("iat").and_then(|v| v.as_f64()).unwrap_or(0.0) as i64;
    let now = Utc::now().timestamp();
    if iat == 0 || iat > now + CLOCK_SKEW || iat < now - CLOCK_SKEW {
        return Err(Error::other(format!(
            "dpop iat outside window, iat={iat} now={now}"
        )));
    }
    let jti = claims.get("jti").and_then(|v| v.as_str()).unwrap_or("");
    if jti.is_empty() {
        return Err(Error::other("dpop proof missing jti"));
    }
    let fresh = tokio::time::timeout(
        Duration::from_secs(5),
        store.check_and_store(jti, REPLAY_TTL),
    )
    .await
    .map_err(|_| Error::other("dpop replay store: timeout"))?
    .map_err(|e| Error::other(format!("dpop replay store: {e}")))?;
    if !fresh {
        return Err(Error::other("dpop jti replay"));
    }
    if !expected_access_token.is_empty() {
        let ath = claims.get("ath").and_then(|v| v.as_str()).unwrap_or("");
        if ath.is_empty() {
            return Err(Error::other(
                "dpop proof missing ath when access token is present",
            ));
        }
        let expected = URL_SAFE_NO_PAD.encode(Sha256::digest(expected_access_token.as_bytes()));
        if ath != expected {
            return Err(Error::other("dpop ath does not match access token"));
        }
    }
    Ok(Some(Proof {
        jkt,
        jti: jti.to_string(),
    }))
}

fn same_url(htu: &str, scheme: &str, host: &str, path: &str) -> bool {
    if htu.is_empty() {
        return false;
    }
    let Ok(parsed) = Url::parse(htu) else {
        return false;
    };
    if parsed.scheme() != scheme {
        return false;
    }
    let parsed_host = match parsed.port() {
        Some(port) => format!("{}:{port}", parsed.host_str().unwrap_or("")),
        None => parsed.host_str().unwrap_or("").to_string(),
    };
    if parsed_host != host {
        return false;
    }
    parsed.path() == path
}

pub fn tls_cert_thumbprint(der: Option<&[u8]>) -> String {
    match der {
        Some(raw) => URL_SAFE_NO_PAD.encode(Sha256::digest(raw)),
        None => String::new(),
    }
}

pub fn jwk_of(value: &Value) -> Option<Jwk> {
    serde_json::from_value(value.clone()).ok()
}
