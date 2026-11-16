use crate::utils::cfg;

pub fn verify_request(slug: &str, secret: &str) -> bool {
    let c = cfg();
    if c.app_slug.is_empty() || c.app_secret.is_empty() {
        return false;
    }
    slug == c.app_slug && secret == c.app_secret
}
