use std::collections::{BTreeMap, HashMap};

use chrono::{DateTime, FixedOffset, NaiveDate, NaiveDateTime, TimeZone, Utc};
use uuid::Uuid;

pub const ERR_NO_TIME_PARAM: &str = "time parameter not found";
pub const ERR_INVALID_TIME_FORMAT: &str = "invalid time format";

const PAGINATION_PARAMS: [&str; 5] = ["column", "cursor", "limit", "order", "page"];

pub type Values = BTreeMap<String, Vec<String>>;

pub fn parse_query(raw: &str) -> Values {
    let mut values = Values::new();
    for (key, value) in url::form_urlencoded::parse(raw.as_bytes()) {
        values
            .entry(key.into_owned())
            .or_default()
            .push(value.into_owned());
    }
    values
}

pub struct QueryParams {
    pub values: Values,
}

impl QueryParams {
    pub fn has(&self, name: &str) -> bool {
        self.values.contains_key(name)
    }

    pub fn get(&self, name: &str) -> String {
        self.values
            .get(name)
            .and_then(|v| v.first())
            .cloned()
            .unwrap_or_default()
    }

    pub fn get_time(&self, name: &str) -> Result<Option<DateTime<Utc>>, String> {
        get_time_param(name, &self.values)
    }

    pub fn get_uuid(&self, name: &str) -> Result<Option<Uuid>, uuid::Error> {
        let raw = self.get(name);
        if raw.is_empty() {
            return Ok(None);
        }
        Uuid::parse_str(&raw).map(Some)
    }

    pub fn get_slice(&self, name: &str) -> Vec<String> {
        let mut out = Vec::new();
        for v in self.values.get(name).into_iter().flatten() {
            for part in v.split(',') {
                let part = part.trim();
                if !part.is_empty() {
                    out.push(part.to_string());
                }
            }
        }
        out
    }
}

pub fn validate_query_params(
    incoming: &Values,
    paginated: bool,
    allowed: &[&str],
) -> Result<QueryParams, String> {
    let mut canonical = Values::new();
    let mut group_of: HashMap<String, String> = HashMap::new();
    for entry in allowed {
        let names: Vec<&str> = entry.split('|').collect();
        let head = names[0];
        for name in &names {
            group_of.insert(name.to_string(), head.to_string());
        }
    }
    let mut seen_group: HashMap<String, String> = HashMap::new();
    let mut unknown = Vec::new();
    for (key, values) in incoming {
        if paginated && PAGINATION_PARAMS.contains(&key.as_str()) {
            continue;
        }
        let Some(group) = group_of.get(key) else {
            unknown.push(key.clone());
            continue;
        };
        if let Some(prev) = seen_group.get(group)
            && prev != key
        {
            return Err(format!("conflicting query parameters {prev:?} and {key:?}"));
        }
        seen_group.insert(group.clone(), key.clone());
        canonical.insert(group.clone(), values.clone());
    }
    if !unknown.is_empty() {
        unknown.sort();
        return Err(format!(
            "unsupported query parameters: {}",
            unknown.join(", ")
        ));
    }
    Ok(QueryParams { values: canonical })
}

const LAYOUTS: [&str; 14] = [
    "%Y-%m-%dT%H:%M:%S",
    "%Y-%m-%d %H:%M:%S",
    "%a, %d %b %Y %H:%M:%S GMT",
    "%a, %d %b %Y %H:%M:%S %z",
    "%d %b %y %H:%M GMT",
    "%d %b %y %H:%M %z",
    "%Y-%m-%d",
    "%m/%d/%Y",
    "%d/%m/%Y",
    "%Y/%m/%d",
    "%B %d, %Y",
    "%d %B %Y",
    "%b %d, %Y",
    "%d %b %Y",
];

fn parse_time_text(text: &str) -> Option<NaiveDateTime> {
    if let Ok(t) = DateTime::parse_from_rfc3339(text) {
        return Some(t.naive_utc());
    }
    for layout in LAYOUTS {
        if let Ok(t) = NaiveDateTime::parse_from_str(text, layout) {
            return Some(t);
        }
        if let Ok(t) = DateTime::<FixedOffset>::parse_from_str(text, layout) {
            return Some(t.naive_utc());
        }
        if let Ok(d) = NaiveDate::parse_from_str(text, layout) {
            return d.and_hms_opt(0, 0, 0);
        }
    }
    None
}

pub fn get_time_param(name: &str, values: &Values) -> Result<Option<DateTime<Utc>>, String> {
    let Some(first) = values.get(name).and_then(|v| v.first()) else {
        return Err(ERR_NO_TIME_PARAM.to_string());
    };
    if first.is_empty() {
        return Err(ERR_NO_TIME_PARAM.to_string());
    }
    if let Ok(unix) = first.parse::<i64>() {
        if unix > 1_000_000_000_000 {
            return Ok(DateTime::from_timestamp_millis(unix));
        }
        return Ok(DateTime::from_timestamp(unix, 0));
    }
    match parse_time_text(first) {
        Some(t) => Ok(Some(Utc.from_utc_datetime(&t))),
        None => Err(ERR_INVALID_TIME_FORMAT.to_string()),
    }
}

pub fn get_time_param_in_location<Tz: TimeZone>(
    name: &str,
    values: &Values,
    loc: &Tz,
) -> Result<DateTime<Tz>, String> {
    let Some(first) = values.get(name).and_then(|v| v.first()) else {
        return Err(ERR_NO_TIME_PARAM.to_string());
    };
    if first.is_empty() {
        return Err(ERR_NO_TIME_PARAM.to_string());
    }
    if let Ok(unix) = first.parse::<i64>() {
        let utc = if unix > 1_000_000_000_000 {
            DateTime::from_timestamp_millis(unix)
        } else {
            DateTime::from_timestamp(unix, 0)
        };
        return utc
            .map(|t| t.with_timezone(loc))
            .ok_or_else(|| ERR_INVALID_TIME_FORMAT.to_string());
    }
    match parse_time_text(first) {
        Some(t) => loc
            .from_local_datetime(&t)
            .single()
            .ok_or_else(|| ERR_INVALID_TIME_FORMAT.to_string()),
        None => Err(ERR_INVALID_TIME_FORMAT.to_string()),
    }
}
