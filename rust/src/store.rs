use serde_json::{Map, Value};
use tokio_postgres::GenericClient;

use crate::shared::Error;

pub async fn upsert(
    conn: &impl GenericClient,
    table: &str,
    row: &Map<String, Value>,
    columns: &[String],
) -> Result<bool, Error> {
    if columns.is_empty() {
        return Ok(false);
    }
    let quoted: Vec<String> = columns.iter().map(|c| format!("\"{c}\"")).collect();
    let sets: Vec<String> = quoted
        .iter()
        .map(|c| format!("{c} = EXCLUDED.{c}"))
        .collect();
    let current: Vec<String> = quoted.iter().map(|c| format!("t.{c}")).collect();
    let incoming: Vec<String> = quoted.iter().map(|c| format!("EXCLUDED.{c}")).collect();
    let sql = format!(
        "INSERT INTO {table} AS t ({cols}) SELECT {cols} FROM jsonb_populate_record(NULL::{table}, $1::jsonb) ON CONFLICT (id) DO UPDATE SET {sets} WHERE ROW({current}) IS DISTINCT FROM ROW({incoming})",
        cols = quoted.join(", "),
        sets = sets.join(", "),
        current = current.join(", "),
        incoming = incoming.join(", "),
    );
    let n = conn.execute(&sql, &[&Value::Object(row.clone())]).await?;
    Ok(n > 0)
}
