use serde::Serialize;
use serde::de::DeserializeOwned;
use serde_json::{Map, Value};

pub trait Hidden {
    fn hidden(&self) -> Vec<(&'static str, Value)> {
        Vec::new()
    }

    fn put_hidden(&mut self, _name: &str, _value: &Value) {}
}

pub fn map<T: Serialize + Hidden>(model: &T) -> Result<Map<String, Value>, String> {
    let value = serde_json::to_value(model).map_err(|e| format!("sauth.Map marshal: {e}"))?;
    let mut out = match value {
        Value::Object(m) => m,
        Value::Null => Map::new(),
        _ => return Err("sauth.Map decode: not an object".to_string()),
    };
    out.retain(|_, v| !is_nil_uuid(v));
    for (name, value) in model.hidden() {
        if let Value::Null = value {
            continue;
        }
        out.insert(name.to_string(), value);
    }
    Ok(out)
}

fn is_nil_uuid(v: &Value) -> bool {
    matches!(v, Value::String(s) if s == "00000000-0000-0000-0000-000000000000")
}

pub fn marshal<T: Serialize + Hidden>(model: &T) -> Result<Vec<u8>, String> {
    let m = map(model)?;
    serde_json::to_vec(&Value::Object(m)).map_err(|e| e.to_string())
}

pub fn unmarshal<T: DeserializeOwned + Hidden>(data: &[u8]) -> Result<T, String> {
    let raw: Map<String, Value> =
        serde_json::from_slice(data).map_err(|e| format!("sauth.Unmarshal envelope: {e}"))?;
    unmarshal_value(Value::Object(raw))
}

pub fn unmarshal_value<T: DeserializeOwned + Hidden>(raw: Value) -> Result<T, String> {
    let Value::Object(mut object) = raw else {
        return Err("sauth.Unmarshal: expected an object".to_string());
    };
    object.retain(|_, v| !v.is_null());
    let mut model: T = serde_json::from_value(Value::Object(object.clone()))
        .map_err(|e| format!("sauth.Unmarshal: {e}"))?;
    for (name, value) in &object {
        model.put_hidden(name, value);
    }
    Ok(model)
}

pub fn marshal_rows<T: Serialize + Hidden>(models: &[T]) -> Result<Vec<Value>, String> {
    models.iter().map(|m| map(m).map(Value::Object)).collect()
}
