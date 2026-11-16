use std::sync::LazyLock;

use chrono::{DateTime, Utc};
use regex::Regex;
use serde::{Deserialize, Serialize};
use serde_json::{Map, Value};
use uuid::Uuid;

use super::db::{Conn, Db};
use super::error::{Error, Result};
use super::models::*;
use super::query::{EndpointSpec, Write, stamp_access_claim};
use crate::client::Ctx;

pub const COST_SCHEMA_ID: &str = "https://samna.io/schema/cost/1";

pub const COST_KIND_USAGE: &str = "usage";
pub const COST_KIND_FEE: &str = "fee";
pub const COST_KIND_PERIOD: &str = "period";
pub const COST_KIND_CREDIT: &str = "credit";

pub const COST_STATUS_ESTIMATED: &str = "estimated";
pub const COST_STATUS_SETTLED: &str = "settled";
pub const COST_STATUS_INVOICED: &str = "invoiced";

pub const COST_GRANULARITY_HOUR: &str = "hour";
pub const COST_GRANULARITY_DAY: &str = "day";
pub const COST_GRANULARITY_MONTH: &str = "month";

const COST_AMOUNT_TOLERANCE: f64 = 0.005;

pub const COST_KINDS: [&str; 4] = [
    COST_KIND_USAGE,
    COST_KIND_FEE,
    COST_KIND_PERIOD,
    COST_KIND_CREDIT,
];
pub const COST_STATUSES: [&str; 3] = [
    COST_STATUS_ESTIMATED,
    COST_STATUS_SETTLED,
    COST_STATUS_INVOICED,
];
const COST_ACCUMULATE_METHODS: [&str; 4] = ["sum", "max", "last", "count"];
const COST_ACCUMULATE_PRORATION: [&str; 2] = ["overlap", "start"];

pub static APP_COST_CURRENCY: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"^[A-Z]{3}$").expect("currency pattern"));

pub const COST_SCHEMA: &str = r#"{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://samna.io/schema/cost/1",
  "title": "Cost evidence",
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "subject": {
      "type": "object",
      "required": ["type", "id"],
      "additionalProperties": false,
      "properties": {
        "type": {"type": "string", "pattern": "^[a-z_][a-z0-9_]*\\.[a-z_][a-z0-9_]*$"},
        "id": {"type": "string", "format": "uuid"},
        "name": {"type": "string"}
      }
    },
    "source": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "vendor": {"type": "string"},
        "ids": {"type": "array", "items": {"type": "string"}},
        "refs": {
          "type": "array",
          "items": {
            "type": "object",
            "required": ["table", "id"],
            "additionalProperties": false,
            "properties": {"table": {"type": "string"}, "id": {"type": "string"}}
          }
        },
        "run_id": {"type": "string"}
      }
    },
    "items": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["description", "quantity", "unit", "amount"],
        "additionalProperties": false,
        "properties": {
          "description": {"type": "string"},
          "quantity": {"type": "number"},
          "unit": {"type": "string", "minLength": 1},
          "unit_price": {"type": "number"},
          "amount": {"type": "number"},
          "ref": {"type": "string"},
          "at": {"type": "string", "format": "date-time"}
        }
      }
    },
    "pricing": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "list": {"type": "string"},
        "tier": {"type": "string"},
        "prorated": {
          "type": "object",
          "required": ["from", "to", "fraction"],
          "additionalProperties": false,
          "properties": {
            "from": {"type": "string", "format": "date-time"},
            "to": {"type": "string", "format": "date-time"},
            "fraction": {"type": "number", "minimum": 0, "maximum": 1}
          }
        }
      }
    },
    "notes": {"type": "string"}
  },
  "x-samna": {
    "title": "Cost",
    "columns": ["subject.name", "items", "pricing.tier", "notes"],
    "unit_label": "Unit",
    "amount_label": "Amount",
    "accumulate": {"method": "sum", "proration": "overlap", "group": ["subject.id"]}
  }
}"#;

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct AppCostBucket {
    #[serde(default)]
    pub app_id: Uuid,
    #[serde(default)]
    pub module: String,
    #[serde(default)]
    pub kind: String,
    #[serde(default)]
    pub schema_id: Option<String>,
    #[serde(default)]
    pub group_key: Value,
    #[serde(default = "epoch")]
    pub period_start: DateTime<Utc>,
    #[serde(default)]
    pub unit: String,
    #[serde(default)]
    pub currency: String,
    #[serde(default)]
    pub quantity: f64,
    #[serde(default)]
    pub amount: f64,
    #[serde(default)]
    pub hours: f64,
    #[serde(default)]
    pub rows: i64,
}

fn epoch() -> DateTime<Utc> {
    DateTime::<Utc>::from_timestamp(0, 0).unwrap_or_default()
}

#[derive(Clone, Debug, Default)]
pub struct AppCostQuery {
    pub module: String,
    pub status: String,
    pub subject_type: String,
    pub subject_id: Uuid,
    pub from: Option<DateTime<Utc>>,
    pub to: Option<DateTime<Utc>>,
    pub granularity: String,
}

pub async fn get_app_cost(
    ctx: &Ctx,
    db: &Db,
    user_id: Uuid,
    _app_id: Uuid,
    query: &AppCostQuery,
) -> Result<Vec<AppCostBucket>> {
    let module = (!query.module.is_empty()).then(|| query.module.clone());
    let status = (!query.status.is_empty()).then(|| query.status.clone());
    let subject_type = (!query.subject_type.is_empty()).then(|| query.subject_type.clone());
    let subject_id = (!query.subject_id.is_nil()).then_some(query.subject_id);
    let granularity = if query.granularity.is_empty() {
        COST_GRANULARITY_HOUR.to_string()
    } else {
        query.granularity.clone()
    };
    db.query_values::<AppCostBucket>(
        ctx,
        "SELECT * FROM claimius.get_app_cost($1, $2, $3, $4, $5, $6, $7, $8)",
        &[
            &user_id,
            &module,
            &query.from,
            &query.to,
            &granularity,
            &status,
            &subject_type,
            &subject_id,
        ],
    )
    .await
}

fn cost_error(message: String) -> Error {
    Error::BadAppCost(message)
}

fn cost_amount_matches(amount: f64, quantity: f64, unit_price: f64) -> bool {
    (amount - quantity * unit_price).abs() <= COST_AMOUNT_TOLERANCE
}

impl AppCostInput {
    fn form(&self) -> &'static str {
        if self.at.is_some() {
            return "event";
        }
        if !self.items.is_empty() {
            return "items";
        }
        "row"
    }

    pub fn validate_post(&self) -> Result<()> {
        let unknown = self.body.get_unknown(AppCostInput::FIELDS);
        if let Some(first) = unknown.first() {
            return Err(cost_error(format!("{first} not allowed")));
        }
        if self.module.trim().is_empty() {
            return Err(cost_error("module required".to_string()));
        }
        if self.r#ref.trim().is_empty() {
            return Err(cost_error("ref required".to_string()));
        }
        if !APP_COST_CURRENCY.is_match(&self.currency) {
            return Err(cost_error("currency must be an ISO 4217 code".to_string()));
        }
        if !self.status.is_empty() && !COST_STATUSES.contains(&self.status.as_str()) {
            return Err(cost_error(
                "status must be one of estimated, settled, invoiced".to_string(),
            ));
        }
        if !self.kind.is_empty() && !COST_KINDS.contains(&self.kind.as_str()) {
            return Err(cost_error(
                "kind must be one of usage, fee, period, credit".to_string(),
            ));
        }
        let mut detail: Option<Map<String, Value>> = None;
        if !self.detail.is_null() {
            match &self.detail {
                Value::Object(m) => detail = Some(m.clone()),
                _ => return Err(cost_error("detail must be a JSON object".to_string())),
            }
        }
        match self.form() {
            "event" => {
                for (name, set) in [
                    ("period_start", self.period_start.is_some()),
                    ("period_end", self.period_end.is_some()),
                    ("quantity", self.quantity.is_some()),
                    ("unit", !self.unit.is_empty()),
                    ("unit_price", self.unit_price.is_some()),
                    ("items", !self.items.is_empty()),
                ] {
                    if set {
                        return Err(cost_error(format!("{name} not allowed with at")));
                    }
                }
                if !self.kind.is_empty() && self.kind != COST_KIND_FEE {
                    return Err(cost_error("kind must be fee with at".to_string()));
                }
                if self.amount.is_none() {
                    return Err(cost_error("amount required with at".to_string()));
                }
            }
            "items" => {
                for (name, set) in [
                    ("quantity", self.quantity.is_some()),
                    ("unit", !self.unit.is_empty()),
                    ("unit_price", self.unit_price.is_some()),
                    ("amount", self.amount.is_some()),
                ] {
                    if set {
                        return Err(cost_error(format!("{name} not allowed with items")));
                    }
                }
                if detail.as_ref().is_some_and(|d| d.contains_key("items")) {
                    return Err(cost_error(
                        "detail.items not allowed with items".to_string(),
                    ));
                }
                for (n, item) in self.items.iter().enumerate() {
                    if item.description.is_empty() {
                        return Err(cost_error(format!("items[{n}].description required")));
                    }
                    if item.unit.is_empty() {
                        return Err(cost_error(format!("items[{n}].unit required")));
                    }
                    if item.unit_price.is_none() && item.amount.is_none() {
                        return Err(cost_error(format!("items[{n}] needs unit_price or amount")));
                    }
                    if let (Some(price), Some(amount)) = (item.unit_price, item.amount)
                        && !cost_amount_matches(amount, item.quantity, price)
                    {
                        return Err(cost_error(format!(
                            "items[{n}].amount must equal quantity times unit_price"
                        )));
                    }
                }
            }
            _ => {
                if self.kind.is_empty() {
                    return Err(cost_error("kind required".to_string()));
                }
                let Some(quantity) = self.quantity else {
                    return Err(cost_error("quantity required".to_string()));
                };
                if self.unit.is_empty() {
                    return Err(cost_error("unit required".to_string()));
                }
                if self.amount.is_none() && self.unit_price.is_none() {
                    return Err(cost_error("amount or unit_price required".to_string()));
                }
                if let (Some(amount), Some(price)) = (self.amount, self.unit_price)
                    && !cost_amount_matches(amount, quantity, price)
                {
                    return Err(cost_error(
                        "amount must equal quantity times unit_price".to_string(),
                    ));
                }
            }
        }
        if self.at.is_none() {
            let Some(start) = self.period_start else {
                return Err(cost_error("period_start required".to_string()));
            };
            let Some(end) = self.period_end else {
                return Err(cost_error("period_end required".to_string()));
            };
            if end < start {
                return Err(cost_error(
                    "period_end must not be before period_start".to_string(),
                ));
            }
            if self.kind == COST_KIND_FEE && end != start {
                return Err(cost_error(
                    "a fee has period_end equal to period_start".to_string(),
                ));
            }
        }
        Ok(())
    }

    pub fn normalize(&self) -> Result<AppCost> {
        self.validate_post()?;
        let mut cost = AppCost {
            module: self.module.clone(),
            kind: self.kind.clone(),
            currency: self.currency.clone(),
            r#ref: self.r#ref.clone(),
            status: self.status.clone(),
            supersedes_id: self.supersedes_id,
            schema: self.schema.clone(),
            detail: self.detail.clone(),
            created_by: self.created_by,
            ..AppCost::default()
        };
        if cost.status.is_empty() {
            cost.status = COST_STATUS_SETTLED.to_string();
        }
        match self.form() {
            "event" => {
                let at = self.at.unwrap_or_default();
                cost.kind = COST_KIND_FEE.to_string();
                cost.period_start = at;
                cost.period_end = at;
                cost.quantity = 1.0;
                cost.unit = "event".to_string();
                cost.amount = self.amount.unwrap_or_default();
            }
            "items" => {
                if cost.kind.is_empty() {
                    cost.kind = COST_KIND_USAGE.to_string();
                }
                cost.period_start = self.period_start.unwrap_or_default();
                cost.period_end = self.period_end.unwrap_or_default();
                let mut items = Vec::with_capacity(self.items.len());
                let mut same_unit = true;
                for item in &self.items {
                    let mut item = item.clone();
                    if item.amount.is_none() {
                        item.amount = Some(item.quantity * item.unit_price.unwrap_or_default());
                    }
                    cost.amount += item.amount.unwrap_or_default();
                    cost.quantity += item.quantity;
                    same_unit = same_unit && item.unit == self.items[0].unit;
                    items.push(item);
                }
                cost.unit = self.items[0].unit.clone();
                if !same_unit {
                    cost.quantity = items.len() as f64;
                    cost.unit = "item".to_string();
                }
                let mut detail = match &self.detail {
                    Value::Object(m) => m.clone(),
                    Value::Null => Map::new(),
                    _ => return Err(cost_error("detail must be a JSON object".to_string())),
                };
                detail.insert("items".to_string(), serde_json::to_value(&items)?);
                cost.detail = Value::Object(detail);
                if cost.schema.is_null()
                    || cost.schema.as_str().is_some_and(|s| s.trim().is_empty())
                {
                    cost.schema = serde_json::from_str(COST_SCHEMA)?;
                }
            }
            _ => {
                cost.period_start = self.period_start.unwrap_or_default();
                cost.period_end = self.period_end.unwrap_or_default();
                cost.quantity = self.quantity.unwrap_or_default();
                cost.unit = self.unit.clone();
                cost.unit_price = self.unit_price;
                cost.amount = match self.amount {
                    Some(a) => a,
                    None => self.quantity.unwrap_or_default() * self.unit_price.unwrap_or_default(),
                };
            }
        }
        if cost.kind != COST_KIND_CREDIT && cost.amount < 0.0 {
            return Err(cost_error(format!(
                "amount must not be negative for kind {}",
                cost.kind
            )));
        }
        cost.normalize()?;
        Ok(cost)
    }
}

impl AppCost {
    pub fn validate_patch(&self) -> Result<()> {
        if self.body.is_empty() {
            return Err(Error::bad_request("no fields to update"));
        }
        let unknown = self.body.get_unknown(AppCost::FIELDS);
        if let Some(first) = unknown.first() {
            return Err(Error::bad_request(format!("{first} not allowed")));
        }
        for key in self.body.keys() {
            if key != "status" && key != "detail" {
                return Err(Error::bad_request(format!("{key} not allowed")));
            }
        }
        if self.body.has_key("status") && !COST_STATUSES.contains(&self.status.as_str()) {
            return Err(Error::bad_request(
                "status must be one of estimated, settled, invoiced",
            ));
        }
        Ok(())
    }

    pub fn normalize(&mut self) -> Result<()> {
        let detail: Map<String, Value> = match &self.detail {
            Value::Object(m) => m.clone(),
            Value::Null => Map::new(),
            Value::String(s) if s.trim().is_empty() => Map::new(),
            _ => return Err(cost_error("detail must be a JSON object".to_string())),
        };
        let doc: Map<String, Value> = match &self.schema {
            Value::Object(m) => m.clone(),
            Value::Null => Map::new(),
            Value::String(s) if s.trim().is_empty() => Map::new(),
            _ => return Err(cost_error("schema must be a JSON object".to_string())),
        };
        if doc.is_empty() {
            self.schema = Value::Object(Map::new());
            self.detail = Value::Object(detail);
            return Ok(());
        }
        check_cost_presentation(&doc)?;
        let mut detail = detail;
        if doc.get("$id").and_then(|v| v.as_str()) == Some(COST_SCHEMA_ID) {
            normalize_cost_items(&mut detail, self.amount)?;
        }
        let schema_value = Value::Object(doc.clone());
        let validator = jsonschema::options()
            .build(&schema_value)
            .map_err(|e| cost_error(format!("schema: {e}")))?;
        let instance = Value::Object(detail.clone());
        if let Err(failed) = validator.validate(&instance) {
            return Err(cost_error(format!("detail {failed}")));
        }
        self.schema = Value::Object(doc);
        self.detail = Value::Object(detail);
        Ok(())
    }
}

fn check_cost_presentation(doc: &Map<String, Value>) -> Result<()> {
    let Some(raw) = doc.get("x-samna") else {
        return Ok(());
    };
    let Some(hints) = raw.as_object() else {
        return Err(cost_error("schema x-samna must be an object".to_string()));
    };
    let Some(raw) = hints.get("accumulate") else {
        return Ok(());
    };
    let Some(accumulate) = raw.as_object() else {
        return Err(cost_error(
            "schema x-samna.accumulate must be an object".to_string(),
        ));
    };
    if let Some(method) = accumulate.get("method")
        && !method
            .as_str()
            .is_some_and(|s| COST_ACCUMULATE_METHODS.contains(&s))
    {
        return Err(cost_error(
            "schema x-samna.accumulate.method must be one of sum, max, last, count".to_string(),
        ));
    }
    if let Some(proration) = accumulate.get("proration")
        && !proration
            .as_str()
            .is_some_and(|s| COST_ACCUMULATE_PRORATION.contains(&s))
    {
        return Err(cost_error(
            "schema x-samna.accumulate.proration must be one of overlap, start".to_string(),
        ));
    }
    if let Some(group) = accumulate.get("group") {
        let Some(paths) = group.as_array() else {
            return Err(cost_error(
                "schema x-samna.accumulate.group must be an array of paths into detail".to_string(),
            ));
        };
        for path in paths {
            if !path.as_str().is_some_and(|s| !s.is_empty()) {
                return Err(cost_error(
                    "schema x-samna.accumulate.group must be an array of paths into detail"
                        .to_string(),
                ));
            }
        }
    }
    Ok(())
}

fn normalize_cost_items(detail: &mut Map<String, Value>, amount: f64) -> Result<()> {
    let Some(Value::Array(list)) = detail.get_mut("items") else {
        return Ok(());
    };
    let mut sum = 0.0;
    for element in list.iter_mut() {
        let Some(item) = element.as_object_mut() else {
            continue;
        };
        let quantity = item
            .get("quantity")
            .and_then(|v| v.as_f64())
            .unwrap_or_default();
        if !item.contains_key("amount")
            && let Some(unit_price) = item.get("unit_price").and_then(|v| v.as_f64())
        {
            item.insert("amount".to_string(), Value::from(quantity * unit_price));
        }
        if let Some(unit_price) = item.get("unit_price").and_then(|v| v.as_f64())
            && let Some(item_amount) = item.get("amount").and_then(|v| v.as_f64())
            && !cost_amount_matches(item_amount, quantity, unit_price)
        {
            return Err(cost_error(
                "detail.items amount must equal quantity times unit_price".to_string(),
            ));
        }
        sum += item
            .get("amount")
            .and_then(|v| v.as_f64())
            .unwrap_or_default();
    }
    if (sum - amount).abs() > COST_AMOUNT_TOLERANCE {
        return Err(cost_error(format!(
            "detail.items amounts sum to {sum}, amount is {amount}"
        )));
    }
    Ok(())
}

impl Db {
    pub(crate) async fn put_app_cost(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        input: &AppCostInput,
    ) -> Result<(AppCost, bool)> {
        let Some(ac) = ac else {
            return Err(Error::no_access());
        };
        if ac.user_claim_id.is_none() {
            return Err(Error::no_access());
        }
        let mut cost = input.normalize()?;
        cost.owner_id = ac.owner_id;
        cost.created_by = ac.user_claim_id;
        cost.deleted_at = None;
        let held = self.conn_or(ctx, conn).await?;
        if let Some(supersedes) = cost.supersedes_id {
            let r#ref: String = match held
                .query_scalar_opt::<String>(
                    "SELECT ref FROM claimius.app_cost WHERE id = $1 AND sa_deleted_at IS NULL",
                    &[&supersedes],
                )
                .await?
            {
                Some(r) => r,
                None => {
                    return Err(Error::NotFound(
                        "supersedes_id names no live row".to_string(),
                    ));
                }
            };
            if r#ref == cost.r#ref {
                return Err(cost_error(
                    "supersedes_id must name another row".to_string(),
                ));
            }
        }
        let mut row = cost.to_row()?;
        for col in ["id", "sa_created_at", "sa_updated_at", "sa_deleted_at"] {
            row.remove(col);
        }
        row.retain(|_, v| !v.is_null());
        let columns: Vec<String> = row.keys().map(|c| format!("\"{c}\"")).collect();
        let sql = format!(
            "INSERT INTO claimius.app_cost AS t ({cols}) SELECT {cols} FROM jsonb_populate_record(NULL::claimius.app_cost, $1::jsonb)
			ON CONFLICT (module, ref) WHERE sa_deleted_at IS NULL DO UPDATE SET
			kind = EXCLUDED.kind, period_start = EXCLUDED.period_start, period_end = EXCLUDED.period_end, quantity = EXCLUDED.quantity, unit = EXCLUDED.unit,
			unit_price = EXCLUDED.unit_price, amount = EXCLUDED.amount, currency = EXCLUDED.currency, supersedes_id = EXCLUDED.supersedes_id, status = EXCLUDED.status,
			schema = EXCLUDED.schema, detail = EXCLUDED.detail, sa_created_by = EXCLUDED.sa_created_by
			RETURNING row_to_json(t)::jsonb",
            cols = columns.join(", ")
        );
        let rows = held.client().query(&sql, &[&Value::Object(row)]).await?;
        let Some(first) = rows.into_iter().next() else {
            return Err(Error::no_rows());
        };
        let written = AppCost::from_row(first.get::<_, Value>(0))?;
        let created = written.created_at == written.updated_at;
        Ok((written, created))
    }

    pub(crate) async fn delete_app_cost(
        &self,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        cost_id: Uuid,
    ) -> Result<AppCost> {
        if ac.is_none() {
            return Err(Error::no_access());
        }
        let held = self.conn_or(ctx, conn).await?;
        held.query_model::<AppCost>("UPDATE claimius.app_cost SET sa_deleted_at = NOW() WHERE id = $1 AND sa_deleted_at IS NULL RETURNING *", &[&cost_id]).await
    }
}

impl Model for AppCostInput {
    const TABLE: &'static str = "";
    const COLUMNS: &'static [(&'static str, &'static str)] = &[];
    const FIELDS: &'static [&'static str] = AppCostInput::FIELDS;

    fn id(&self) -> Uuid {
        Uuid::nil()
    }

    fn body(&self) -> Option<&super::bind::Body> {
        Some(&self.body)
    }
}

impl Write for AppCostInput {
    type Result = AppCost;

    fn payload_endpoint() -> Option<&'static str> {
        Some("cost")
    }

    fn stamp(&mut self, ac: &AccessClaim) {
        if let Some(uc) = ac.user_claim_id
            && !uc.is_nil()
            && self.created_by.is_none_or(|c| c.is_nil())
        {
            self.created_by = Some(uc);
        }
    }

    fn insert_local(
        &mut self,
        db: &Db,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
    ) -> impl std::future::Future<Output = Result<Option<(AppCost, bool)>>> + Send {
        async move { db.put_app_cost(ctx, conn, ac, self).await.map(Some) }
    }
}

impl Write for AppCost {
    type Result = AppCost;

    fn endpoint() -> Option<EndpointSpec> {
        Some(EndpointSpec {
            base: "cost",
            parents: &[],
        })
    }

    fn stamp(&mut self, ac: &AccessClaim) {
        stamp_access_claim(&mut self.created_by, &mut self.owner_id, ac);
    }

    fn replace_with(&mut self, result: &AppCost) {
        let body = self.body.clone();
        *self = result.clone();
        self.body = body;
    }

    fn delete_local(
        &mut self,
        db: &Db,
        ctx: &Ctx,
        conn: Option<&Conn>,
        ac: Option<&AccessClaim>,
        id: Uuid,
    ) -> impl std::future::Future<Output = Result<Option<AppCost>>> + Send {
        async move { db.delete_app_cost(ctx, conn, ac, id).await.map(Some) }
    }
}
