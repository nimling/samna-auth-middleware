---
name: claimius
description: "Use this skill whenever you are writing or modifying code that interacts with Claimius, a Postgres claim based access control schema named `claimius`. Triggers include: any SQL or app code that reads from or writes to `claimius.*` tables; any code that touches `sa_owner_id`, `sa_root_id`, `sa_parent_id`, `sa_location_id`, `sa_created_by`, `sa_deleted_at`, or `app_id` columns; any code that defines or registers a new application table that should participate in Claimius access control; any access check or permission related code; any time you see references to claims, user_claims, or the prophet/disciple architecture; any time you see the schema name `claimius` in code, migrations, or comments. This skill enforces the correct usage patterns and stops you from making the changes that have historically broken this codebase."
---

# Claimius skill

## Stop. Read this first.

You are working with Claimius, a distributed claim based access control system. It has been broken multiple times by AI assistants making confident changes that bypassed the architecture. The user has explicitly told the assistant about this. Do NOT repeat those mistakes.

The mistakes that historically caused damage:

1. Reading directly from `claimius.user_object` instead of going through `get_*` functions.
2. Treating `samna_user.id` as the user identifier when `samna_user.user_id` is.
3. Adding `sa_parent_id` without `sa_root_id`, or vice versa.
4. Allowing nullable `sa_owner_id` or `sa_parent_id` for the root case instead of using self references. All parent columns are NOT NULL; roots self reference.
5. Inserting into `claim`, `user_claim`, or `claim_object` directly instead of using the write functions; OR writing to computed tables like `user_object`, `object_users`, `user_users`, `inheritance_info`, `table_info`, `sync_event` at all.
6. Modifying access calculations by editing `user_object` rows.
7. Forgetting `app_id` in filters, leaking access across apps.
8. Confusing `sa_created_by`, a `user_claim.id`, with a user identifier.
9. Hard deleting from registered tables.
10. Reparenting organizations across roots.
11. Adding `p_system_app_secret` or any equivalent to `init_prophet`. The app secret is consumer facing, generated externally on demand by the implementing service, written via UPDATE to `samna_app.app_secret`. It is NOT a bootstrap input. `private_key` and `private_seed` ARE init inputs because those are fixed identity. `app_secret` is rotatable credentials and lives outside init.

If you are about to do any of those, STOP and explain to the user what you're considering. Get explicit confirmation before proceeding.

## How Claimius works in one paragraph

Apps register their tables via `init_claimius_tables`. Triggers attached at registration maintain `claimius.user_object`, the materialized "what can this user see" index. Reads go through `get_*` functions. Writes to claim metadata go through write functions. The same schema runs on prophets, the sources of truth, and disciples, the read replicas. Disciples receive state via `sync_event` rows from a prophet and apply them under `replay_mode = true` so calc triggers don't fire. Roles enforce the boundary: `claimius_writer` can write, `claimius_reader` cannot.

## Decision rules

### "Did I bind the resource to a claim after creating it?"

Every resource that participates in the access model must have at least one direct `claim_object` row pointing at it. After inserting an organization, location, app, or registered application table row, follow up with `assign_claim_object`. The owner claim is normally the claim the actor used to create the resource, the one referenced by `user_claim_id`. Pass an explicit claim id when the writer wants a different binding.

Two things are exempt: `samna_user` rows are handled by `user_users` and `ensure_app_user`, and a claim does not bind itself via `claim_object`.

### "Will this binding cascade?"

A cascade fires only when BOTH `claim.inherits = TRUE` AND the `claim_object.inherits = TRUE` for that specific binding. Either flag set to FALSE blocks cascade for that binding. A claim with `inherits = FALSE` blocks all of its cascades regardless of per binding flags.

### "Should I read directly from a claimius table?"

NO. Always use `get_*` functions:

- `get_access(user_id, app_id, object_id, object_type, min_level)` for access checks
- `get_direct_access(user_id, app_id, object_id, object_type)` to find the closest user_claim via ancestor walk when `get_access` returns nothing
- `get_objects(user_id, app_id, object_type, min_level)` to list visible objects
- `get_organizations`, `get_locations`, `get_users`, `get_claims`, `get_audit`, `get_secrets` for typed lists
- `get_owner_graph`, `get_claim_graph` for hierarchy and access visualizations. Both take optional `start_id` and `depth` for subgraph queries. Both cover all registered tables in the deployment. Output rows have fields `object_id`, `object_type`, `label`, `parent_id`, and `data jsonb` where `object_type` is schema qualified. `data` always carries `access_level` and `level` plus per type fallback fields with `sa_` prefixes stripped.
- `search(user_id, app_id, query, types, min_level, limit, offset)` for full text search

### "Should I INSERT/UPDATE a claimius table directly?"

It depends on the table.

**Use the write function** for these:

- `claim` → `create_claim`, `update_claim`, `remove_claim`
- `user_claim` → `assign_claim_user`, `update_claim_user`, `remove_user_claim`
- `claim_object` → `assign_claim_object`, `update_claim_object`, `remove_claim_object`
- `samna_user` per app row creation → `ensure_app_user`
- Cross app moves → `merge_user`
- Cross root moves → `migrate_root`
- Bootstrap → `init_prophet`, `init_disciple` requiring `p_disciple_app_slug`, `init_hybrid` requiring both `p_system_app_slug` and `p_disciple_app_slug`
- External table registration → `init_claimius_tables`
- Self identity → `get_prophet_app()` returns the system app on prophet and hybrid, raises on disciple. `get_disciple_app()` returns the upstream app on disciple and hybrid, raises on prophet. Both return `SETOF claimius.samna_app`. Scalar UUID variants `get_prophet_app_id()`, `get_disciple_app_id()`, and `get_app_id()` are available for column DEFAULTs and other places that reject subqueries. `get_app_id()` resolves to the prophet/system app id on prophet and hybrid, disciple app id on pure disciple. Use these instead of maintaining a local helper.

**Direct INSERT/UPDATE is fine** on the prophet for:

- `samna_app`. Use `init_prophet` only for the FIRST app.
- `organization`, `location`
- `samna_client`, `samna_secret`
- `object_field`, `user_field`, `user_relation`
- `prophet_state`, `disciple_state`

The triggers attached to each table fire either way, so calc, audit, and sync emission run identically. The function vs INSERT distinction is about validation, ergonomics, and getting non obvious sequences right.

**NEVER write directly** to these tables; they are managed by triggers and the sync layer:

- `user_object`, `object_users`, `user_users` for computed access state
- `inheritance_info`, `table_info` computed by registration and calc functions
- `sync_event` append only, written by triggers
- `audit`. Use `claimius.write_audit` if you must.
- `reconcile_queue` managed by sync layer

### "Should I DELETE from a registered table?"

NO. Soft delete by setting `sa_deleted_at = now()`. Hard delete only happens inside `merge_user` and calc functions cleaning up materialized state.

### "Can I add a `sa_parent_id` column to my table for self referencing?"

YES, but you MUST also add `sa_root_id`. Both come together. The root row has `sa_parent_id = id` and `sa_root_id = id`.

After adding columns, re-run `init_claimius_tables` with the same table list. It will pick up the new columns.

### "What user identifier do I use?"

`samna_user.user_id`, the stable application identifier. NOT `samna_user.id`, the row PK. The same human user has multiple rows in `samna_user`, one per app, all sharing the same `user_id` but with distinct `id`. References everywhere are by `user_id`.

The `id` column exists only because Postgres needs a PK. Treat it as internal.

### "What goes in `sa_created_by`?"

A `user_claim.id`. Not a `samna_user.user_id`. The actor under whose authority the action was taken.

### "How do I get the user_claim.id to pass as `sa_created_by` for a write?"

Call `get_access`, `get_objects`, or `search_objects`. Each returns a `user_claim_id` column alongside the access info. That's the actor token. **Contract: when `claim_id` is non null, `user_claim_id` is also populated.** Use it directly:

```sql
SELECT user_claim_id INTO :acting_user_claim
FROM claimius.get_access($user_id, $app_id, $object_id, $object_type, $required_level);

UPDATE public.recipe SET name = :new_name, sa_created_by = :acting_user_claim
WHERE id = :recipe_id;
```

When `get_access` returns no rows, fall back to `get_direct_access`. It walks the owner, location, and parenthood ancestor chains and returns the closest, highest privilege user_claim the user holds. Same row shape as `get_access`.

Other `get_*` functions, including `get_organizations`, `get_locations`, `get_users`, `get_claims`, `get_audit`, `get_secrets`, and the graphs, do not return `user_claim_id`. Call `get_access` for the specific object before writing.

### "How do I scope a query to one app?"

Always filter on `app_id`. Every materialized state table — `samna_user`, `user_claim`, `user_object`, `object_users`, `user_users`, `user_relation`, `user_field` — carries `app_id`. The same human can have entirely separate access in different apps.

### "How do I check if a user can see something?"

```sql
SELECT EXISTS (
    SELECT 1 FROM claimius.get_access($user_id, $app_id, $object_id, $object_type, $required_level)
);
```

`get_access` returns the strongest surviving grant or no row.

### "How do I list everything a user can see?"

```sql
SELECT * FROM claimius.get_objects($user_id, $app_id, $object_type, $min_level);
```

Returns object id, denormalized name, description, link, and tree position columns. Already filtered for active claims.

### "How do I implement a deny?"

Create a claim with `is_deny = true` and a specific `sa_access_level`. Bind it via `assign_claim_object`. The deny erases grants on the same path at level `<= deny.sa_access_level`. Calc functions process this; you don't filter denies in your reads.

### "What's a path?"

A path is the pair `tree_type, cascaded_from`. A claim binding cascades through one of the three trees: ownership, location, parenthood. Each grant in `user_object.grants` records which path it came through. Denies are evaluated path by path.

### "Can I have multiple grants on the same object?"

Yes. The `user_object.grants` jsonb array carries one entry per contributing claim. The effective level, `sa_access_level` on the row, is the strongest of contributing levels. Removing one claim's contribution shrinks the array; the row stays as long as some grant or `direct_grant` survives.

## Required columns on a registered application table

```sql
CREATE TABLE public.my_table (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id          UUID NOT NULL,
    sa_owner_id     UUID NOT NULL,
    sa_created_by   UUID NOT NULL,
    sa_created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    sa_updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    sa_deleted_at   TIMESTAMPTZ,

    sa_location_id  UUID,

    -- BOTH must be present, or NEITHER:
    -- sa_parent_id  UUID NOT NULL,
    -- sa_root_id    UUID NOT NULL,

    name            TEXT,
    description     TEXT,

    UNIQUE (...)
);

SELECT claimius.init_claimius_tables('public.my_table');
```

`init_claimius_tables` validates required columns. If you forgot `sa_created_by`, it raises and refuses registration.

## Creation order

Tables in claimius reference each other circularly: app↔org, claim→org, user_claim→claim, claim_object→claim+object. Skip a step or do them out of order and either FKs fail or calc triggers misfire.

**Fresh prophet bootstrap** is encapsulated in `init_prophet`. If the user is creating "the first app on this database", call `init_prophet` and don't replicate its body.

**A new app on an existing prophet:**

1. Owning org must exist.
2. INSERT `samna_app`.
3. INSERT `prophet_state` row with `last_applied_seq = 0`.
4. Optional: create default claim, UPDATE `samna_app.claim_id`.
5. Optional: `init_claimius_tables(...)` for any external tables.

**A new resource — org, location, app table row — inside an existing app:**

1. Direct INSERT with all required `sa_*` columns.
2. To grant someone access: `create_claim` → `assign_claim_object` → `assign_claim_user`.

If a user asks you to create something and the order is non obvious, lay out the steps before writing SQL. Ask if you're not sure which step belongs first.

## Common patterns

### A user logs into an app

```sql
SELECT claimius.ensure_app_user(
    p_user_id => $stable_user_uuid,
    p_app_id  => $app_id,
    p_first_name => $first,
    p_last_name  => $last,
    p_email      => $email
);
```

Idempotent. Inserts the per app `samna_user` row if missing AND grants the app's default claim, `samna_app.claim_id` configured during `init_prophet`, as a `user_claim`. Returns `{"samna_user": {...}, "user_claim": {...}}` when the app has a default claim, or `{"samna_user": {...}}` when it doesn't. The default claim is intentionally minimal: holding it alone grants no resource access. It is the "you have an account in this app" marker. The implementing app grants additional claims later as users earn permissions.

### Granting a permission

Write functions return jsonb with table name keys. `create_claim` returns `{"claim": {...}}`, `assign_claim_object` returns `{"claim_object": {...}}`, `assign_claim_user` returns `{"user_claim": {...}}`. Capture into a variable inside a DO block when you need ids for follow on calls; otherwise the SELECT result is fine.

```sql
SELECT claimius.create_claim(
    p_app_id          => $app_id,
    p_name            => 'Recipe Editor',
    p_description     => 'Can edit recipes in this kitchen',
    p_sa_access_level => 4,
    p_sa_owner_id     => $org_id,
    p_sa_root_id      => $root_org_id,
    p_sa_created_by   => $acting_user_claim
);

SELECT claimius.assign_claim_object(
    p_app_id        => $app_id,
    p_claim_id      => $claim_id,
    p_object_id     => $kitchen_org_id,
    p_object_type   => 'claimius.organization',
    p_sa_owner_id   => $org_id,
    p_sa_root_id    => $root_org_id,
    p_sa_created_by => $acting_user_claim,
    p_inherits      => TRUE
);

SELECT claimius.assign_claim_user(
    p_app_id        => $app_id,
    p_claim_id      => $claim_id,
    p_user_id       => $user_id,
    p_sa_owner_id   => $org_id,
    p_sa_created_by => $acting_user_claim
);
```

Binding to the kitchen organization with `inherits = TRUE` cascades to all recipes owned by the org.

### Building a query against your own table

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

For the basics use `user_object`'s denormalized columns:

```sql
SELECT object_id, sa_name, sa_description, sa_access_level
FROM claimius.get_objects($user_id, $app_id, 'public.recipe', $min_level);
```

## When you're unsure

Ask the user. Do not guess. Specifically ask:

- "I see this table has `sa_parent_id` but not `sa_root_id`. Is that a missing column or do you not want self referencing?"
- "Should this query filter by `app_id`? I assume yes since this is reading materialized state."
- "I'm about to call `merge_user`. This will hard delete rows for the source user. Confirm?"
- "Is this code on the prophet side or the disciple side? On a disciple, only the sync layer should write to claimius tables."
- "I'm about to add a column to a registered table. Should I re-run `init_claimius_tables` after the migration?"

## What this skill prevents

When the AI assistant has Claimius rules loaded, it should:

- Refuse to write a SELECT from any `claimius.*` table directly. Always use `get_*` functions.
- Refuse to INSERT/UPDATE on `claim`, `user_claim`, `claim_object` without using the write functions.
- Refuse to INSERT/UPDATE/DELETE on `user_object`, `object_users`, `user_users`, `inheritance_info`, `table_info`, `sync_event`, `audit`, or `reconcile_queue` ever.
- Allow direct INSERT/UPDATE on `samna_app`, `organization`, `location`, `samna_user`, `samna_client`, `samna_secret`, `object_field`, `user_field`, `user_relation`, `prophet_state`, `disciple_state` on the prophet side, with the required `sa_*` columns populated.
- Reject any registered table missing required columns and tell the user.
- Always include `app_id` in filters against materialized state.
- Default to soft delete; raise hard delete as a flag for the user.
- Never bypass `replay_mode` outside the disciple sync layer.
- Never confuse `samna_user.id` with `samna_user.user_id`.
- Never set `sa_created_by` to a `samna_user.user_id`.
- Treat self referencing tree roots as `parent == root == id`, never null.

If the AI is about to break any of these, it must stop, surface the concern, and ask. The user's experience with this codebase taught them not to trust shortcuts. Be conservative.
