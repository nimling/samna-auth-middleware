use aes_gcm::aead::{Aead, KeyInit};
use aes_gcm::{Aes256Gcm, Nonce};
use rand::RngCore;

use super::error::{Error, Result};

pub fn aes_encrypt(plain: &str, seed: &[u8]) -> Result<Vec<u8>> {
    let cipher =
        Aes256Gcm::new_from_slice(seed).map_err(|e| Error::other(format!("create cipher: {e}")))?;
    let mut nonce = [0u8; 12];
    rand::rng().fill_bytes(&mut nonce);
    let sealed = cipher
        .encrypt(Nonce::from_slice(&nonce), plain.as_bytes())
        .map_err(|e| Error::other(format!("encrypt: {e}")))?;
    let mut out = nonce.to_vec();
    out.extend(sealed);
    Ok(out)
}

pub fn aes_decrypt(ciphertext: &[u8], seed: &[u8]) -> Result<String> {
    let cipher = Aes256Gcm::new_from_slice(seed).map_err(|e| Error::other(e.to_string()))?;
    if ciphertext.len() < 12 {
        return Err(Error::other("ciphertext too short"));
    }
    let (nonce, body) = ciphertext.split_at(12);
    let plain = cipher
        .decrypt(Nonce::from_slice(nonce), body)
        .map_err(|e| Error::other(format!("decrypt: {e}")))?;
    String::from_utf8(plain).map_err(|e| Error::other(format!("decrypt: {e}")))
}
