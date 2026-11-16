use std::net::UdpSocket;

use url::Url;
use uuid::Uuid;

pub fn generate_request_id() -> String {
    Uuid::new_v4().to_string()
}

pub fn local_ip() -> String {
    let Ok(socket) = UdpSocket::bind("0.0.0.0:0") else {
        return "unknown".to_string();
    };
    if socket.connect("10.255.255.255:1").is_err() {
        return "unknown".to_string();
    }
    match socket.local_addr() {
        Ok(addr) if !addr.ip().is_loopback() && addr.is_ipv4() => addr.ip().to_string(),
        _ => "unknown".to_string(),
    }
}

pub fn join_url(base_url: &str, segments: &[&str]) -> Result<String, String> {
    let mut base = Url::parse(base_url).map_err(|e| e.to_string())?;
    let mut segments: Vec<String> = segments.iter().map(|s| s.to_string()).collect();
    if let Some(last) = segments.last_mut()
        && let Some(i) = last.find('?')
    {
        base.set_query(Some(&last[i + 1..]));
        last.truncate(i);
    }
    let mut root = base.path().to_string();
    if segments.iter().any(|s| s.starts_with('/')) {
        root = String::new();
    }
    let mut parts: Vec<&str> = Vec::with_capacity(segments.len() + 1);
    parts.push(&root);
    for s in &segments {
        parts.push(s);
    }
    let joined = path_join(&parts);
    base.set_path(&joined);
    Ok(base.to_string())
}

fn path_join(parts: &[&str]) -> String {
    let mut out: Vec<&str> = Vec::new();
    let absolute = parts.first().is_some_and(|p| p.starts_with('/'))
        || parts.iter().any(|p| p.starts_with('/'));
    for part in parts {
        for piece in part.split('/') {
            match piece {
                "" | "." => {}
                ".." => {
                    out.pop();
                }
                other => out.push(other),
            }
        }
    }
    let joined = out.join("/");
    if absolute || !joined.is_empty() {
        format!("/{joined}")
    } else {
        String::new()
    }
}

pub fn append_graph_query(
    u: &str,
    start_id: Option<Uuid>,
    depth: i32,
    required_access: i32,
) -> String {
    let mut query = url::form_urlencoded::Serializer::new(String::new());
    if let Some(id) = start_id {
        query.append_pair("start_id", &id.to_string());
    }
    if depth != 0 {
        query.append_pair("depth", &depth.to_string());
    }
    query.append_pair("required_access", &required_access.to_string());
    let encoded = query.finish();
    if encoded.is_empty() {
        return u.to_string();
    }
    match Url::parse(u) {
        Ok(mut parsed) => {
            parsed.set_query(Some(&encoded));
            parsed.to_string()
        }
        Err(_) => format!("{u}?{encoded}"),
    }
}
