use axum::response::{IntoResponse, Response};
use base64::Engine;
use http::header::{CONTENT_TYPE, LOCATION};
use http::{HeaderValue, StatusCode};
use serde::Serialize;
use serde_json::Value;
use uuid::Uuid;

#[derive(Clone, Debug, Default, Serialize, serde::Deserialize)]
pub struct ReturnError {
    pub message: String,
}

#[derive(Clone, Debug, Serialize)]
pub struct ReturnObject<T: Serialize> {
    pub data: T,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<ReturnError>,
    pub request_id: Uuid,
    pub count: i64,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub pagination: Option<ReturnPagination>,
}

#[derive(Clone, Debug, Default, Serialize, serde::Deserialize)]
pub struct Pagination {
    pub last_id: Option<Uuid>,
    pub page: i64,
    pub limit: i64,
    pub order: String,
    pub column: String,
    pub prev_cursor: Option<PageCursor>,
    pub next_cursor: Option<PageCursor>,
}

#[derive(Clone, Debug, Default, Serialize, serde::Deserialize)]
pub struct PageCursor {
    pub column: String,
    pub order: String,
    pub id: Uuid,
    pub limit: i64,
    pub page: i64,
}

#[derive(Clone, Debug, Default, Serialize, serde::Deserialize)]
pub struct ReturnPagination {
    #[serde(skip_serializing_if = "is_zero")]
    pub page: i64,
    #[serde(skip_serializing_if = "is_zero")]
    pub limit: i64,
    #[serde(skip_serializing_if = "is_zero")]
    pub total: i64,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub prev: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub next: Option<String>,
}

fn is_zero(n: &i64) -> bool {
    *n == 0
}

const DEFAULT_PAGE_SIZE: i64 = 20;
const RESERVED_PARAMS: [&str; 4] = ["cursor", "limit", "order", "page"];

pub fn redirect(status: StatusCode, url: &str) -> Response {
    let mut response = status.into_response();
    if let Ok(v) = HeaderValue::from_str(url) {
        response.headers_mut().insert(LOCATION, v);
    }
    response
}

pub fn parse_include(param: Option<&str>) -> std::collections::HashSet<String> {
    let mut include = std::collections::HashSet::new();
    let Some(param) = param else { return include };
    for item in param.split(',') {
        let item = item.trim();
        if !item.is_empty() {
            include.insert(item.to_string());
        }
    }
    include
}

fn encode<T: Serialize>(status: StatusCode, body: &T) -> Response {
    let mut text = serde_json::to_string_pretty(body).unwrap_or_else(|_| "{}".to_string());
    text.push('\n');
    let mut response = (status, text).into_response();
    response.headers_mut().insert(
        CONTENT_TYPE,
        HeaderValue::from_static("application/json; charset=UTF-8"),
    );
    response
}

pub fn json<T: Serialize>(request_id: Uuid, status: StatusCode, data: T, count: i64) -> Response {
    let count = match serde_json::to_value(&data) {
        Ok(Value::Array(a)) => a.len() as i64,
        Ok(_) => {
            if count == 0 {
                1
            } else {
                count
            }
        }
        Err(_) => count,
    };
    encode(
        status,
        &ReturnObject {
            data,
            error: None,
            request_id,
            count,
            pagination: None,
        },
    )
}

pub fn json_paginated<T: Serialize>(
    request_id: Uuid,
    status: StatusCode,
    data: T,
    count: i64,
    pagination: Option<ReturnPagination>,
) -> Response {
    encode(
        status,
        &ReturnObject {
            data,
            error: None,
            request_id,
            count,
            pagination,
        },
    )
}

pub fn error(request_id: Uuid, status: StatusCode, message: &str) -> Response {
    encode(
        status,
        &ReturnObject::<Vec<Value>> {
            data: Vec::new(),
            error: Some(ReturnError {
                message: message.to_string(),
            }),
            request_id,
            count: 0,
            pagination: None,
        },
    )
}

pub fn no_content(request_id: Uuid, status: StatusCode) -> Response {
    encode(
        status,
        &ReturnObject::<Vec<Value>> {
            data: Vec::new(),
            error: None,
            request_id,
            count: 0,
            pagination: None,
        },
    )
}

pub fn init_pagination(
    params: &std::collections::BTreeMap<String, Vec<String>>,
    limit: Option<i64>,
    column: Option<&str>,
) -> Result<Pagination, String> {
    let get = |name: &str| params.get(name).and_then(|v| v.first()).cloned();
    let mut pagination = Pagination {
        last_id: None,
        page: 1,
        limit: DEFAULT_PAGE_SIZE,
        order: "DESC".to_string(),
        column: "id".to_string(),
        prev_cursor: None,
        next_cursor: None,
    };
    if let Some(column) = column {
        pagination.column = column.to_string();
    }
    if let Some(limit) = limit {
        pagination.limit = limit;
    } else if let Some(raw) = get("limit") {
        pagination.limit = raw
            .parse()
            .map_err(|_| "limit must be an integer".to_string())?;
    }
    if let Some(raw) = get("page") {
        pagination.page = raw
            .parse()
            .map_err(|_| "page must be an integer".to_string())?;
    }
    if let Some(column) = get("column") {
        pagination.column = column;
    }
    if let Some(cursor) = get("cursor")
        && !cursor.is_empty()
    {
        let decoded = base64::engine::general_purpose::STANDARD
            .decode(&cursor)
            .map_err(|e| format!("invalid query params: {e}"))?;
        let mut parsed: PageCursor =
            serde_json::from_slice(&decoded).map_err(|e| format!("invalid query params: {e}"))?;
        if parsed.limit <= 0 {
            parsed.limit = DEFAULT_PAGE_SIZE;
        }
        pagination.limit = parsed.limit;
        pagination.page = parsed.page;
        pagination.column = parsed.column.clone();
        pagination.last_id = Some(parsed.id);
        pagination.prev_cursor = Some(parsed);
    }
    if let Some(order) = get("order") {
        pagination.order = if order == "asc" || order == "ASC" {
            "ASC".to_string()
        } else {
            "DESC".to_string()
        };
    }
    Ok(pagination)
}

impl Pagination {
    pub fn offset(&self) -> i64 {
        if self.last_id.is_none() {
            self.limit * (self.page - 1)
        } else {
            0
        }
    }

    pub fn query_sql(&self) -> String {
        if let Some(cursor) = &self.prev_cursor {
            let comparison = if cursor.order == "asc" || cursor.order == "ASC" {
                ">"
            } else {
                "<"
            };
            let order = if cursor.order == "asc" || cursor.order == "ASC" {
                "ASC"
            } else {
                "DESC"
            };
            return format!(
                "AND \"{col}\" {comparison} '{id}' ORDER BY \"{col}\" {order} LIMIT {limit}",
                col = cursor.column,
                id = cursor.id,
                limit = cursor.limit
            );
        }
        if self.page > 1 {
            return format!(
                "ORDER BY \"{}\" {} LIMIT {} OFFSET {}",
                self.column,
                self.order,
                self.limit,
                (self.page - 1) * self.limit
            );
        }
        format!(
            "ORDER BY \"{}\" {} LIMIT {}",
            self.column, self.order, self.limit
        )
    }

    pub fn return_pagination(
        &self,
        path: &str,
        params: &std::collections::BTreeMap<String, Vec<String>>,
        last_id: Option<Uuid>,
        total: i64,
    ) -> ReturnPagination {
        let mut out = ReturnPagination {
            page: self.page,
            limit: self.limit,
            total,
            prev: None,
            next: None,
        };
        let mut additional = String::new();
        for (key, values) in params {
            if RESERVED_PARAMS.contains(&key.as_str()) {
                continue;
            }
            for value in values {
                if !additional.is_empty() {
                    additional.push('&');
                }
                additional.push_str(&format!("{key}={value}"));
            }
        }
        let Some(last_id) = last_id else { return out };
        let mut cursor = PageCursor {
            order: "DESC".to_string(),
            id: last_id,
            limit: self.limit,
            page: self.page + 1,
            column: self.column.clone(),
        };
        if let Some(prev) = &self.prev_cursor {
            cursor.order = prev.order.clone();
            cursor.column = prev.column.clone();
            let encoded = prev.encoded();
            let mut prev_url = format!("{path}?cursor={encoded}");
            if !additional.is_empty() {
                prev_url.push('&');
                prev_url.push_str(&additional);
            }
            out.prev = Some(prev_url);
        }
        if self.page * self.limit < total {
            let encoded = cursor.encoded();
            let mut next_url = format!("{path}?cursor={encoded}");
            if !additional.is_empty() {
                next_url.push('&');
                next_url.push_str(&additional);
            }
            out.next = Some(next_url);
            out.page = self.page;
        }
        out
    }
}

impl PageCursor {
    pub fn encoded(&self) -> String {
        let bytes = serde_json::to_vec(self).unwrap_or_default();
        base64::engine::general_purpose::STANDARD.encode(bytes)
    }
}
