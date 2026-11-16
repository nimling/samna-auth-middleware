pub const TYPE_FALSE: i32 = 0;
pub const TYPE_TRUE: i32 = 1;
pub const TYPE_NULL: i32 = 2;
pub const TYPE_NUMBER: i32 = 3;
pub const TYPE_STRING: i32 = 4;
pub const TYPE_ARRAY: i32 = 5;
pub const TYPE_OBJECT: i32 = 6;

pub fn define_json_type(data: &[u8]) -> Result<i32, String> {
    let text = std::str::from_utf8(data).map_err(|e| e.to_string())?.trim();
    if text.is_empty() {
        return Err("empty input".to_string());
    }
    match text {
        "false" => return Ok(TYPE_FALSE),
        "true" => return Ok(TYPE_TRUE),
        "null" => return Ok(TYPE_NULL),
        _ => {}
    }
    let first = text.as_bytes()[0];
    let last = text.as_bytes()[text.len() - 1];
    if first == b'"' && last == b'"' {
        return Ok(TYPE_STRING);
    } else if first == b'[' && last == b']' {
        return Ok(TYPE_ARRAY);
    } else if first == b'{' && last == b'}' {
        return Ok(TYPE_OBJECT);
    }
    if text.parse::<f64>().is_ok() {
        return Ok(TYPE_NUMBER);
    }
    Err("unrecognized JSON type".to_string())
}
