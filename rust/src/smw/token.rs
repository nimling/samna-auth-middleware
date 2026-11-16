use std::collections::HashMap;
use std::sync::{Arc, RwLock};
use std::time::{Duration, Instant};

use http::HeaderMap;
use jsonwebtoken::jwk::JwkSet;
use jsonwebtoken::{Algorithm, DecodingKey, Validation, decode, decode_header};
use serde::{Deserialize, Serialize};
use uuid::Uuid;

use super::models::{AuthError, AuthMiddlewareConfig, ClaimUser, Confirmation};
use crate::dpop;

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct TokenClaims {
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub iss: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub sub: String,
    #[serde(
        default,
        deserialize_with = "audience",
        skip_serializing_if = "Vec::is_empty"
    )]
    pub aud: Vec<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub exp: Option<i64>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub nbf: Option<i64>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub iat: Option<i64>,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub jti: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub scope: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub client_id: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub cnf: Option<Confirmation>,
}

fn audience<'de, D: serde::Deserializer<'de>>(d: D) -> Result<Vec<String>, D::Error> {
    #[derive(Deserialize)]
    #[serde(untagged)]
    enum Aud {
        One(String),
        Many(Vec<String>),
    }
    match Option::<Aud>::deserialize(d)? {
        None => Ok(Vec::new()),
        Some(Aud::One(s)) => Ok(vec![s]),
        Some(Aud::Many(v)) => Ok(v),
    }
}

struct Keys {
    set: HashMap<String, (DecodingKey, Algorithm)>,
    fetched: Instant,
}

pub struct Jwks {
    url: String,
    keys: RwLock<Option<Keys>>,
    client: reqwest::Client,
}

const JWKS_REFRESH_MIN: Duration = Duration::from_secs(60);

impl Jwks {
    pub fn new(url: &str) -> Arc<Jwks> {
        Arc::new(Jwks {
            url: url.to_string(),
            keys: RwLock::new(None),
            client: reqwest::Client::builder()
                .timeout(Duration::from_secs(10))
                .build()
                .expect("http client"),
        })
    }

    async fn fetch(&self) -> Result<(), String> {
        let set: JwkSet = self
            .client
            .get(&self.url)
            .send()
            .await
            .map_err(|e| format!("jwks: {e}"))?
            .json()
            .await
            .map_err(|e| format!("jwks: {e}"))?;
        let mut keys = HashMap::new();
        for jwk in set.keys {
            let Some(kid) = jwk.common.key_id.clone() else {
                continue;
            };
            let Ok(key) = DecodingKey::from_jwk(&jwk) else {
                continue;
            };
            let alg = jwk
                .common
                .key_algorithm
                .and_then(|a| Algorithm::from_str_lossy(a.to_string().as_str()))
                .unwrap_or(Algorithm::RS256);
            keys.insert(kid, (key, alg));
        }
        *self.keys.write().expect("jwks") = Some(Keys {
            set: keys,
            fetched: Instant::now(),
        });
        Ok(())
    }

    pub async fn key(&self, kid: &str) -> Result<(DecodingKey, Algorithm), String> {
        {
            let held = self.keys.read().expect("jwks");
            if let Some(keys) = held.as_ref()
                && let Some(found) = keys.set.get(kid)
            {
                return Ok(found.clone());
            }
            if let Some(keys) = held.as_ref()
                && keys.fetched.elapsed() < JWKS_REFRESH_MIN
            {
                return Err(format!("jwks: unknown kid {kid}"));
            }
        }
        self.fetch().await?;
        let held = self.keys.read().expect("jwks");
        held.as_ref()
            .and_then(|k| k.set.get(kid).cloned())
            .ok_or_else(|| format!("jwks: unknown kid {kid}"))
    }
}

trait AlgorithmLossy {
    fn from_str_lossy(s: &str) -> Option<Algorithm>;
}

impl AlgorithmLossy for Algorithm {
    fn from_str_lossy(s: &str) -> Option<Algorithm> {
        s.parse::<Algorithm>().ok()
    }
}

pub struct RequestFacts<'a> {
    pub method: &'a str,
    pub path: &'a str,
    pub host: &'a str,
    pub scheme: &'a str,
    pub headers: &'a HeaderMap,
    pub tls_thumbprint: &'a str,
}

pub async fn validate_token(
    req: &RequestFacts<'_>,
    token: &str,
    config: &AuthMiddlewareConfig,
    jwks: &Jwks,
    replay: Option<&dpop::PostgresReplayStore>,
) -> Result<ClaimUser, AuthError> {
    let header = decode_header(token).map_err(|e| AuthError::Other(format!("parse token: {e}")))?;
    let kid = header.kid.clone().unwrap_or_default();
    let (key, alg) = jwks
        .key(&kid)
        .await
        .map_err(|e| AuthError::Other(format!("parse token: {e}")))?;
    let mut validation = Validation::new(alg);
    validation.validate_aud = false;
    validation.validate_exp = true;
    validation.required_spec_claims.clear();
    let data = decode::<TokenClaims>(token, &key, &validation).map_err(|e| match e.kind() {
        jsonwebtoken::errors::ErrorKind::ExpiredSignature => AuthError::TokenExpired,
        _ => AuthError::Other(format!("parse token: {e}")),
    })?;
    let claims = data.claims;
    if claims.iss != config.expected_token_issuer {
        return Err(AuthError::Other(format!(
            "unexpected token issuer: [{}], expected: [{}]",
            claims.iss, config.expected_token_issuer
        )));
    }
    if !claims.aud.iter().any(|a| *a == config.client_slug) {
        return Err(AuthError::Other(format!(
            "token audience {:?} does not include {:?}",
            claims.aud, config.client_slug
        )));
    }
    if let Some(cnf) = &claims.cnf {
        if !cnf.jkt.is_empty() {
            let Some(store) = replay else {
                return Err(AuthError::Other("dpop proof: no replay store".to_string()));
            };
            let header = req
                .headers
                .get(dpop::HEADER)
                .and_then(|h| h.to_str().ok())
                .unwrap_or("");
            let proof = dpop::verify(
                req.method, req.scheme, req.host, req.path, header, token, store,
            )
            .await
            .map_err(|e| AuthError::Other(format!("dpop proof: {e}")))?;
            let Some(proof) = proof else {
                return Err(AuthError::Other(
                    "cnf.jkt requires a dpop proof".to_string(),
                ));
            };
            if proof.jkt != cnf.jkt {
                return Err(AuthError::Other(
                    "dpop jkt does not match cnf.jkt".to_string(),
                ));
            }
        } else if !cnf.x5t_s256.is_empty() && req.tls_thumbprint != cnf.x5t_s256 {
            return Err(AuthError::Other(
                "mtls thumbprint does not match cnf.x5t#S256".to_string(),
            ));
        }
    }
    let user_id = Uuid::parse_str(&claims.sub)
        .map_err(|e| AuthError::Other(format!("claims subject is not a valid UUID: {e}")))?;
    Ok(ClaimUser {
        id: user_id,
        ..ClaimUser::default()
    })
}

pub fn claims_from_token_unsafe(token: &str) -> Result<ClaimUser, AuthError> {
    let claims = unverified_claims(token)?;
    let user_id = Uuid::parse_str(&claims.sub)
        .map_err(|e| AuthError::Other(format!("parse user id: {e}")))?;
    Ok(ClaimUser {
        id: user_id,
        ..ClaimUser::default()
    })
}

pub fn unverified_claims(token: &str) -> Result<TokenClaims, AuthError> {
    let mut parts = token.split('.');
    let (Some(_), Some(payload)) = (parts.next(), parts.next()) else {
        return Err(AuthError::Other(
            "parse token: token has no payload".to_string(),
        ));
    };
    use base64::Engine;
    let raw = base64::engine::general_purpose::URL_SAFE_NO_PAD
        .decode(payload)
        .map_err(|e| AuthError::Other(format!("parse token: {e}")))?;
    serde_json::from_slice(&raw).map_err(|e| AuthError::Other(format!("parse token: {e}")))
}
