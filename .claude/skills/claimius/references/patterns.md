# Claimius patterns

SQL for the common writes and reads, on the prophet set. A disciple's functions take no app argument; in Go on a disciple use the calls in `access.md` and the builder.

## A user logs into an app

```sql
SELECT claimius.upsert_app_user(
    p_user_id => $stable_user_uuid,
    p_app_id  => $app_id,
    p_first_name => $first,
    p_last_name  => $last,
    p_email      => $email
);
```

Idempotent. Inserts the per app `samna_user` row if missing AND grants the app's default claim, `samna_app.claim_id` configured during `upsert_system_app`, as a `user_claim`. Returns `{"samna_user": {...}, "user_claim": {...}}` when the app has a default claim, or `{"samna_user": {...}}` when it doesn't. The default claim is intentionally minimal: holding it alone grants no resource access. It is the "you have an account in this app" marker. The implementing app grants additional claims later as users earn permissions.

## Granting a permission

Write functions return jsonb with table name keys. `insert_claim` returns `{"claim": {...}}`, `insert_claim_object` returns `{"claim_object": {...}}`, `insert_user_claim` returns `{"user_claim": {...}}`. Capture into a variable inside a DO block when you need ids for follow on calls; otherwise the SELECT result is fine.

```sql
SELECT claimius.insert_claim(
    p_app_id          => $app_id,
    p_name            => 'Recipe Editor',
    p_description     => 'Can edit recipes in this kitchen',
    p_sa_access => 4,
    p_sa_owner_id     => $org_id,
    p_sa_root_id      => $root_org_id,
    p_sa_created_by   => $acting_user_claim
);

SELECT claimius.insert_claim_object(
    p_app_id        => $app_id,
    p_claim_id      => $claim_id,
    p_object_id     => $kitchen_org_id,
    p_object_type   => 'claimius.organization',
    p_sa_owner_id   => $org_id,
    p_sa_root_id    => $root_org_id,
    p_sa_created_by => $acting_user_claim,
    p_inherits      => TRUE
);

SELECT claimius.insert_user_claim(
    p_app_id        => $app_id,
    p_claim_id      => $claim_id,
    p_user_id       => $user_id,
    p_sa_owner_id   => $org_id,
    p_sa_created_by => $acting_user_claim
);
```

Binding to the kitchen organization with `inherits = TRUE` cascades to all recipes owned by the org.

## Building a query against your own table

```sql
-- WRONG: leaks rows the user shouldn't see
SELECT * FROM public.recipe WHERE kitchen_id = $kitchen_id;

-- RIGHT: gated by user access
SELECT r.*
FROM public.recipe r
WHERE r.id IN (
    SELECT object_id FROM claimius.get_objects($user_id, $app_id, 'public.recipe', $min_level)
)
  AND r.kitchen_id = $kitchen_id;
```

For the basics use the rendering columns `get_objects` joins from `object_info`:

```sql
SELECT object_id, sa_name, sa_description, sa_access
FROM claimius.get_objects($user_id, $app_id, 'public.recipe', $min_level);
```
