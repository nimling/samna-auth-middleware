use samna_auth_middleware::disciple::sdb;
use samna_auth_middleware::shared::{ACCESS_OWNER, ACCESS_READ, ACCESS_WRITE, Ctx, apply_event};
use serde_json::json;
use uuid::Uuid;

fn database_available() -> bool {
    std::env::var("PGHOST").is_ok_and(|h| !h.is_empty())
}

async fn init() -> &'static samna_auth_middleware::shared::Db {
    unsafe {
        std::env::set_var(
            "SAUTH_DEBUG_USER_ID",
            "a0000000-0000-4000-8000-00000000000a",
        );
        std::env::set_var("SAUTH_APP_SLUG", "testapp");
        std::env::set_var("SAUTH_APP_SECRET", "app-secret");
        std::env::set_var(
            "SAUTH_REDIRECT_URI",
            "http://localhost:4200/api/auth/callback",
        );
    }
    match sdb::instance() {
        Some(db) => db,
        None => sdb::init().await.expect("sdb init"),
    }
}

#[tokio::test]
#[ignore]
async fn a_synced_root_organization_and_grant_answer_get_access_on_the_disciple() {
    if !database_available() {
        return;
    }
    let db = init().await;
    let ctx = Ctx::new();
    let root = Uuid::new_v4();
    let user = Uuid::new_v4();
    let claim = Uuid::new_v4();
    let grant = Uuid::new_v4();
    let binding = Uuid::new_v4();
    let conn = db.conn(&ctx).await.expect("conn");
    conn.begin().await.expect("begin");
    conn.execute("SET LOCAL claimius.replay_mode = 'true'", &[])
        .await
        .expect("replay");
    apply_event(&conn, "upsert", "claimius.organization", json!({ "id": root, "name": "Root", "owner_id": root, "root_id": root, "created_by": grant, "level": 0, "type": "entity" })).await.expect("organization");
    apply_event(&conn, "upsert", "claimius.samna_user", json!({ "id": Uuid::new_v4(), "user_id": user, "email": "ada@example.com", "status": "active", "type": "user" })).await.expect("user");
    apply_event(&conn, "upsert", "claimius.claim", json!({ "id": claim, "name": "Root writer", "access": (ACCESS_WRITE | ACCESS_READ).0, "inherits": true, "level": 0, "type": "user", "owner_id": root, "root_id": root, "created_by": grant })).await.expect("claim");
    apply_event(&conn, "upsert", "claimius.user_claim", json!({ "id": grant, "claim_id": claim, "user_id": user, "owner_id": root, "created_by": grant })).await.expect("grant");
    apply_event(&conn, "upsert", "claimius.claim_object", json!({ "id": binding, "claim_id": claim, "object_id": root, "object_type": "claimius.organization", "inherits": true, "owner_id": root, "root_id": root, "created_by": grant })).await.expect("binding");
    conn.commit().await.expect("commit");
    let access = db
        .get_access(&ctx, user, root, "claimius.organization", ACCESS_WRITE)
        .await
        .expect("the synced grant answers the check");
    assert_eq!(access.claim_name.as_deref(), Some("Root writer"));
    assert_eq!(access.root_id, root);
    let owner = db
        .get_access(&ctx, user, root, "claimius.organization", ACCESS_OWNER)
        .await;
    assert!(
        owner
            .map(|a| a.claim_name.as_deref() == Some("direct"))
            .unwrap_or(false)
            || owner_is_refused(db, &ctx, user, root).await,
        "owner on the root is the creator rule or a refusal"
    );
    let orgs = db
        .get_organizations(&ctx, user, ACCESS_READ, None, 0)
        .await
        .expect("list");
    assert!(
        orgs.iter().any(|o| o.id == root),
        "the listing settles the reader's share"
    );
}

async fn owner_is_refused(
    db: &samna_auth_middleware::shared::Db,
    ctx: &Ctx,
    user: Uuid,
    root: Uuid,
) -> bool {
    db.get_access(ctx, user, root, "claimius.organization", ACCESS_OWNER)
        .await
        .is_err()
}
