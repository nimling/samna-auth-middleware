use url::Url;
use uuid::Uuid;

pub fn get_env(key: &str) -> String {
    match std::env::var(key) {
        Ok(value) => value,
        Err(_) => panic!("Environment variable {key} not set"),
    }
}

pub fn get_env_url(key: &str) -> Url {
    let value = match std::env::var(key) {
        Ok(value) => value,
        Err(_) => panic!("Environment variable {key} not set"),
    };
    match Url::parse(&value) {
        Ok(url) => url,
        Err(_) => panic!("Environment variable {key} is not a valid URL"),
    }
}

pub fn cookie_domain_from_uri(uri: &str) -> String {
    let Ok(parsed) = Url::parse(uri) else {
        return String::new();
    };
    let host = parsed.host_str().unwrap_or("");
    if host == "localhost" {
        return String::new();
    }
    host.to_string()
}

pub fn get_env_optional(key: &str) -> String {
    std::env::var(key).unwrap_or_default()
}

pub fn get_env_uuid_optional(key: &str) -> Option<Uuid> {
    let value = std::env::var(key).ok().filter(|v| !v.is_empty())?;
    match Uuid::parse_str(&value) {
        Ok(id) => Some(id),
        Err(_) => panic!("Environment variable {key} is not a valid UUID"),
    }
}
