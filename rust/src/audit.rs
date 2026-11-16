use std::sync::{Arc, LazyLock};
use std::time::Duration;

use tokio::sync::Semaphore;
use uuid::Uuid;

use crate::shared::{Audit, Ctx, Db};

const WRITERS: usize = 64;

static SLOTS: LazyLock<Arc<Semaphore>> = LazyLock::new(|| Arc::new(Semaphore::new(WRITERS)));

pub fn write(
    db: &'static Db,
    operation: &str,
    audit_type: &str,
    object_type: &str,
    object_id: Uuid,
    owner_id: Uuid,
    created_by: Option<Uuid>,
    message: &str,
) {
    let (operation, audit_type, object_type, message) = (
        operation.to_string(),
        audit_type.to_string(),
        object_type.to_string(),
        message.to_string(),
    );
    let slots = SLOTS.clone();
    tokio::spawn(async move {
        let Ok(_permit) = slots.acquire().await else {
            return;
        };
        let mut last_err = None;
        for attempt in 0..3u32 {
            if attempt > 0 {
                tokio::time::sleep(Duration::from_secs(attempt as u64)).await;
            }
            let result = tokio::time::timeout(
                Duration::from_secs(10),
                db.query_model::<Audit>(
                    &Ctx::new(),
                    "SELECT * FROM claimius.insert_audit($1, $2, $3, $4, $5, $6, $7)",
                    &[
                        &operation,
                        &audit_type,
                        &object_type,
                        &object_id,
                        &owner_id,
                        &created_by,
                        &message,
                    ],
                ),
            )
            .await;
            match result {
                Ok(Ok(_)) => return,
                Ok(Err(e)) => last_err = Some(e.to_string()),
                Err(_) => last_err = Some("timed out".to_string()),
            }
        }
        tracing::error!(
            "audit.Write {audit_type} {operation} {object_type}:{object_id}: {}",
            last_err.unwrap_or_default()
        );
    });
}
