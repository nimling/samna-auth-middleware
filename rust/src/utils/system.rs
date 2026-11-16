use uuid::Uuid;

use super::config::cfg;

pub enum SystemAppValue<'a> {
    Text(&'a str),
    Id(Uuid),
}

impl<'a> From<&'a str> for SystemAppValue<'a> {
    fn from(value: &'a str) -> Self {
        SystemAppValue::Text(value)
    }
}

impl<'a> From<&'a String> for SystemAppValue<'a> {
    fn from(value: &'a String) -> Self {
        SystemAppValue::Text(value.as_str())
    }
}

impl From<Uuid> for SystemAppValue<'_> {
    fn from(value: Uuid) -> Self {
        SystemAppValue::Id(value)
    }
}

pub fn is_system_app<'a>(value: impl Into<SystemAppValue<'a>>) -> bool {
    match value.into() {
        SystemAppValue::Text(v) => {
            if v.is_empty() {
                return true;
            }
            if v == cfg().app_slug {
                return true;
            }
            matches!(Uuid::parse_str(v), Ok(parsed) if parsed.is_nil())
        }
        SystemAppValue::Id(v) => v.is_nil(),
    }
}
