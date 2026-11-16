use serde_json::{Map, Value};

#[derive(Clone, Debug, Default, PartialEq)]
pub struct Body {
    body: Option<Map<String, Value>>,
}

impl Body {
    pub fn bound(raw: &[u8]) -> Result<(Body, Value), serde_json::Error> {
        let value: Value = serde_json::from_slice(raw)?;
        let body = match &value {
            Value::Object(m) => Some(m.clone()),
            _ => Some(Map::new()),
        };
        Ok((Body { body }, value))
    }

    pub fn from_value(value: &Value) -> Body {
        match value {
            Value::Object(m) => Body {
                body: Some(m.clone()),
            },
            _ => Body {
                body: Some(Map::new()),
            },
        }
    }

    pub fn validator(&self) -> Option<&Map<String, Value>> {
        self.body.as_ref()
    }

    pub fn has_key(&self, key: &str) -> bool {
        self.body.as_ref().is_some_and(|b| b.contains_key(key))
    }

    pub fn has_value(&self, key: &str) -> bool {
        self.body
            .as_ref()
            .and_then(|b| b.get(key))
            .is_some_and(|v| !v.is_null())
    }

    pub fn is_empty(&self) -> bool {
        self.body.as_ref().is_none_or(|b| b.is_empty())
    }

    pub fn get_unknown(&self, allowed: &[&str]) -> Vec<String> {
        let Some(body) = &self.body else {
            return Vec::new();
        };
        let mut unknown: Vec<String> = body
            .keys()
            .filter(|k| !allowed.contains(&k.as_str()))
            .cloned()
            .collect();
        unknown.sort();
        unknown
    }

    pub fn deny(&self, fields: &[&str]) -> Vec<String> {
        let Some(body) = &self.body else {
            return Vec::new();
        };
        let mut denied: Vec<String> = body
            .keys()
            .filter(|key| {
                fields
                    .iter()
                    .any(|pattern| match_deny_pattern(pattern, key))
            })
            .cloned()
            .collect();
        denied.sort();
        denied
    }

    pub fn keys(&self) -> Vec<String> {
        self.body
            .as_ref()
            .map(|b| b.keys().cloned().collect())
            .unwrap_or_default()
    }
}

fn match_deny_pattern(pattern: &str, key: &str) -> bool {
    if let Some(prefix) = pattern.strip_suffix('*') {
        return key.starts_with(prefix);
    }
    pattern == key
}
