---
name: claimius
type: project_rules
applies_to: any project that has the `claimius` schema deployed in its Postgres database
priority: critical
description: |
  claimius schema, claim based access control, prophet disciple sync,
  user_object materialized index, get_access get_objects get_users
  get_organizations get_locations get_claims search_objects access guard
  authorization permission check, claim user_claim claim_object assign,
  organization location ownership tree parent root sa_owner_id sa_root_id
  sa_parent_id sa_location_id sa_created_by sa_deleted_at app_id, deny
  is_deny inherits cascaded_from grants direct_grant, samna_app samna_user
  samna_client samna_secret inheritance_info table_info sync_event,
  init_prophet init_disciple init_hybrid init_claimius_tables
  init_claimius_internal ensure_app_user merge_user migrate_root,
  calc_object_access calc_hierarchical_access calc_claim_access
  emit_sync_event audit_trigger replay_mode reconcile_queue
  prophet_state disciple_state, register a table for access control,
  permission template, role grant claimius_writer claimius_reader
  claimius_disciple_client, org chart access graph hierarchy rendering,
  full text search over accessible objects.
hard_constraints:
  - reads always go through `get_*` functions, never direct SELECT on claimius tables
  - writes go through a write function when one exists; otherwise direct INSERT/UPDATE on the prophet side is fine. Triggers fire either way and maintain `user_object`. (See "Creating things" in the README for which entities have functions.)
  - hard delete is forbidden in app code; default is soft delete via `sa_deleted_at`
  - `sa_created_by` is a `user_claim.id`, not a `samna_user.user_id` (except in bootstrap)
  - `samna_user.user_id` is the human-stable identifier; `samna_user.id` is the row PK
  - tree roots are self referential (`sa_owner_id == sa_root_id == id`); no nullable parents
  - `sa_parent_id` and `sa_root_id` come together or not at all
  - every materialized state query must filter on `app_id`
  - object types are always schema qualified (`public.device`, never bare `device`)
  - `claimius.replay_mode` is set ONLY by the disciple sync layer, never by app code
  - cross root reparenting is forbidden in normal flow; admin must use `migrate_root`
  - composite ids use the format `<schema>.<table>:<uuid>` joined by colons
ask_user_when_unsure: true
do_not_invent: true
---

# Claimius rules

This project uses **Claimius** for access control. Claimius is a Postgres schema (`claimius`) that materializes user access into a denormalized index. Application code reads through `get_*` functions, never direct SELECTs against claimius tables.

If you are writing or modifying code that touches access checks, registered application tables, or claim assignments, follow the rules below.

## Mental model

Three orthogonal hierarchies. Every registered row has a position in at least one.

1. **Ownership**: rooted at organizations. Reached via `sa_owner_id`. A row is a tree root iff `sa_owner_id == sa_root_id == id`.
2. **Location**: rooted at top level locations. Reached on registered rows via `sa_location_id`. Location rows form a tree via `sa_parent_id`.
3. **Parenthood**: any registered table can declare its own self referencing tree by including `sa_parent_id` AND `sa_root_id` (must come together).

Claims (`claim`) are permission templates. They are granted to users (`user_claim`) and bound to objects (`claim_object`). A claim with `inherits = true` cascades through whichever hierarchy applies to the bound object.

A claim with `is_deny = true` erases grants at level `<= claim.sa_access_level` on the same path.

## Hard rules

### Owner claim rule

Every resource that participates in the access model must carry at least one direct `claim_object` row binding it to some claim. The owner claim is the claim used to create the resource, captured from the actor's `user_claim_id` at write time, or one specified explicitly by the writer. Without a direct binding, access depends entirely on cascade and the resource is invisible the moment the cascade chain is interrupted. Two exceptions: `samna_user` rows (handled by `user_users` denormalization and `ensure_app_user`) and a claim itself (a claim does not bind itself via `claim_object`; circular). When the implementer inserts an organization, location, app, or registered application table row, an `assign_claim_object` (or direct `claim_object` insert during bootstrap) must follow before any read attempts to find the row.

### Inheritance is a logical AND

A cascade fires for a (claim, binding) pair only when BOTH `claim.inherits = TRUE` AND `claim_object.inherits = TRUE`. The claim's flag is the master switch; the per-binding flag narrows. A claim with `inherits = FALSE` cascades nothing regardless of the per-binding flag. A binding with `inherits = FALSE` does not cascade even when the claim says it can. Both flags must agree.

### Reads only through `get_*` functions

```sql
-- WRONG. Bypasses access control. Will fail under claimius_reader role.
SELECT * FROM claimius.user_object WHERE user_id = $1;

-- WRONG. Same.
SELECT * FROM public.device WHERE id = $1;

-- RIGHT. Access checked, soft delete cascade, expired claim filtering, all handled.
SELECT * FROM claimius.get_objects($user_id, $app_id, 'public.device', $min_level);
```

The `get_*` family includes:
- `get_access(user, app, object_id, object_type, min_level)` - strongest active grant; returns user_claim_id as actor token
- `get_direct_access(user, app, object_id, object_type)` - creator grant boolean
- `get_objects(user, app, object_type, min_level)` - all visible objects of one type
- `get_organizations(user, app, min_level)` - visible organizations
- `get_locations(user, app, min_level)` - visible locations
- `get_users(user, app, min_level)` - visible users
- `get_claims(user, app, min_level)` - claims the user holds
- `get_claims(user, app, object_id, object_type)` - claims against one object
- `get_audit(user, app, min_level, limit, offset)` - audit log
- `get_secrets(user, app, min_level)` - visible secrets
- `get_owner_graph(user, app, min_level)` - hierarchy nodes for rendering
- `get_claim_graph(user, app, min_level)` - access graph for rendering
- `search_objects(user, app, query, types, min_level, limit, offset)` - full text search

### Writes: function if it exists, direct INSERT otherwise

Write functions return a **single jsonb object** keyed by the affected table name. `create_claim` returns `{"claim": {...}}`, `assign_claim_user` returns `{"user_claim": {...}}`, `init_prophet` returns six keys (`samna_app`, `samna_user`, `organization`, `claim`, `claim_object`, `user_claim`). Capture and unpack with the `->` and `->>` jsonb operators.

```sql
-- For claim machinery, ALWAYS use the function. Returns jsonb.
SELECT claimius.assign_claim_user(
    p_app_id        => $app_id,
    p_claim_id      => $claim_id,
    p_user_id       => $user_id,
    p_sa_owner_id   => $org_id,
    p_sa_created_by => $user_claim_id
);

-- Inside a DO block, capture and pull what you need:
DO $$
DECLARE
    v_result   JSONB;
    v_claim_id UUID;
BEGIN
    v_result := claimius.create_claim(
        p_app_id          => $app_id,
        p_name            => 'Editor',
        p_sa_access_level => 5,
        p_sa_owner_id     => $org_id,
        p_sa_root_id      => $root_org_id,
        p_sa_created_by   => $user_claim_id
    );
    v_claim_id := (v_result -> 'claim' ->> 'id')::UUID;
    -- now use v_claim_id in further calls
END $$;

-- For tables WITHOUT a function (samna_app, organization, location,
-- samna_user, samna_client, samna_secret, object_field, user_field,
-- user_relation), direct INSERT is fine on the prophet. Calc triggers
-- attached to the table maintain everything afterward.
INSERT INTO claimius.organization (
    app_id, name, sa_owner_id, sa_root_id, sa_level, sa_created_by
) VALUES (
    $app_id, 'Acme NYC', $parent_org_id, $parent_org_root_id, 1, $user_claim_id
);
```

Tables that have a write function (use the function):
- `claim` → `create_claim`, `update_claim`, `remove_claim`
- `user_claim` → `assign_claim_user`, `update_claim_user`, `remove_user_claim`
- `claim_object` → `assign_claim_object`, `update_claim_object`, `remove_claim_object`
- `samna_user` (per app row creation) → `ensure_app_user`
- Cross app moves → `merge_user`
- Cross root moves → `migrate_root` (admin only, rare)
- Bootstrap → `init_prophet`, `init_disciple` (requires `p_disciple_app_slug`), `init_hybrid` (requires both `p_system_app_slug` and `p_disciple_app_slug`)
- Table registration → `init_claimius_tables`
- Self identity → `get_prophet_app()` (system app on prophet/hybrid), `get_disciple_app()` (upstream app on disciple/hybrid). Both return `SETOF claimius.samna_app`. Scalar UUID variants `get_prophet_app_id()`, `get_disciple_app_id()`, and `get_app_id()` exist for use in `DEFAULT` expressions and other places that reject subqueries. `get_app_id()` returns the prophet/system app id on prophet and hybrid, the disciple app id on pure disciple. Use these instead of maintaining a local `app_id()` helper.

Tables where direct INSERT/UPDATE is the documented path:
- `samna_app` (use `init_prophet` for the FIRST app only)
- `organization`
- `location`
- `samna_client`, `samna_secret`
- `object_field`, `user_field`, `user_relation`
- `prophet_state`, `disciple_state`

You may NEVER write directly to:
- `user_object`, `object_users`, `user_users` (materialized state, computed by triggers)
- `inheritance_info`, `table_info` (computed by registration / calc functions)
- `sync_event` (append only, written by triggers)
- `audit` (write via `claimius.write_audit` if you must)
- `reconcile_queue` (managed by sync layer)

### `sa_created_by` is `user_claim.id`, never `samna_user.user_id`

The actor recorded in every row is the assignment under whose authority the action was taken. This decouples actor identity from user identity, so `merge_user` can move users without breaking authorship references.

The only exception is during bootstrap (inside `init_prophet`), where `sa_created_by` may briefly be `samna_user.user_id` because no `user_claim` exists yet. After bootstrap, every `sa_created_by` is a valid `user_claim.id`.

### `user_claim_id` from `get_access` is the actor token for follow on writes

`get_access`, `get_objects`, and `search_objects` return a `user_claim_id` column. **Contract: when `claim_id` is non null on the returned row, `user_claim_id` is also populated.** Callers use this as `sa_created_by` on follow on writes within the same request.

When access is direct only (`direct_grant = true`, `claim_id` null), `user_claim_id` is also null. Callers needing an actor for direct only access must resolve a fallback user_claim themselves, typically the user's bootstrap user_claim for the app.

The list and graph functions (`get_organizations`, `get_locations`, `get_users`, `get_claims`, `get_audit`, `get_secrets`, `get_owner_graph`, `get_claim_graph`) do NOT return `user_claim_id`. To get the actor token after listing, call `get_access` for the specific object the caller is about to act on.

### Composite ids are `<schema>.<table>:<uuid>`

Object types are always schema qualified. `public.device`, never bare `device`. The `init_claimius_tables` function defaults a missing schema to `public`, but stored values are always full.

The composite id format is colon separated: `claimius.composite_id('public.device', 'aaaa...')` returns `'public.device:aaaa...'`.

### `sa_owner_id == sa_root_id == id` defines a tree root

There are no nullable parent columns. Bootstrap creates self references on the root row. Walking ownership: stop when `sa_owner_id = id`. Same for location parent and parenthood parent.

### App scoping

Every materialized state row carries `app_id`. Same human user gets one `samna_user` row per app, all sharing the same `user_id`. Disciples only know about their own app's rows. The prophet holds rows for every app.

When writing a query that hits any of: `samna_user`, `user_claim`, `user_object`, `object_users`, `user_users`, `user_relation`, `user_field`, always include `app_id` in the filter.

### Deny semantics

A deny claim erases grants at level `<= deny.sa_access_level` on the same path. "Path" means the same `(tree_type, cascaded_from)` in `user_object.grants`. A deny bound directly to an object (not through inheritance) blocks all paths to that object at level `<= deny.sa_access_level`.

A deny at level 0 erases direct (creator) grants too.

You do not need to filter for denies in your reads; calc functions remove blocked grants from `user_object` at write time. The presence of a row in `user_object` already means access exists.

### Adding a column to a registered table

If you need to add an inheritance column (`sa_parent_id`, `sa_root_id`, `sa_location_id`) to a registered table after it's already in production:

1. Add the column with a migration.
2. Backfill values.
3. Re-run `init_claimius_tables(...)` with the same table list. The function is idempotent; it picks up the new columns and rebuilds the relevant `inheritance_info` rows.
4. The bulk seed runs under `replay_mode` so triggers stay quiet during the rebuild.

Never add `sa_parent_id` without `sa_root_id`. `init_claimius_tables` will refuse the registration.

### Soft delete vs hard delete

- Default for everything is soft delete: set `sa_deleted_at = now()`.
- Hard deletes happen ONLY in `merge_user` (cleaning up the source user's rows) and inside calc functions (cleaning up materialized state in `user_object`, `object_users`, `user_users`).
- Never `DELETE FROM` registered tables in app code.

### Cross root reparenting is forbidden

If you need to move an organization to a different root tree, the calc trigger will raise. Use `claimius.migrate_root(object_type, object_id, new_root_id)`. This is rare and should be flagged for human review.

## Creation order on a prophet

A prophet has tables that reference each other circularly. Get the order wrong and you'll get FK violations or rows that the calc triggers can't make sense of. The order below is what `init_prophet` does internally; if you find yourself replicating it, ask the user whether `init_prophet` is the right call instead.

### Fresh prophet bootstrap (handled by `init_prophet`)

1. System `samna_app` (with placeholder `sa_owner_id`)
2. System `samna_user` (scoped to the system app)
3. System `organization` (self rooted: `sa_owner_id = sa_root_id = id`, `app_id` points at the system app)
4. UPDATE the system app's `sa_owner_id` to the real system org id
5. `inheritance_info` row for the system org (empty tree)
6. System `claim` ("System Administrator", level 0). Sidelined: granted only to the system user.
   6b. Default `claim` (name, description, level supplied by the implementer via `p_default_claim`). `inherits = false`, no `claim_object`. Holding it grants no resource access. This is what every regular user receives.
7. UPDATE the system app's `claim_id` to the **default** claim id (NOT the system admin claim).
8. `claim_object` binding the system claim to the system org with `inherits = TRUE`
9. `user_claim` granting the system claim to the system user
10. `prophet_state` row for the system app
11. Role grants (`_grant_prophet_roles`)

The two-claim split is intentional. The system admin claim is sidelined: granted only to the system user. The default claim is `samna_app.claim_id` and is what `ensure_app_user` automatically grants on first login. It exists so users have a `user_claim` row in the app even before they have any resource access.

### A new (non system) app on an existing prophet

1. Owning org must exist (existing org or fresh root org)
2. INSERT `samna_app` with all per app fields, `sa_owner_id` pointing at the owning org
3. INSERT `prophet_state` row with `last_applied_seq = 0`
4. Optionally create a default claim for the app and set `samna_app.claim_id`
5. `init_claimius_tables(...)` to register any external tables

### Resources inside an existing app

1. Org (if needed): direct INSERT
2. Location (if needed): direct INSERT
3. App table rows: direct INSERT
4. Grants: `create_claim` (if needed) → `assign_claim_object` → `assign_claim_user`

If you're tempted to skip steps because "the FK doesn't exist yet anyway," stop and check the order. Calc triggers fire on every INSERT and assume the order above held.

## What to do when:

### A user logs in for the first time on app X

```sql
SELECT claimius.ensure_app_user(
               p_user_id    => $user_id,
               p_app_id     => $app_id,
               p_first_name => $first,
               p_last_name  => $last,
               p_email      => $email
       );
```

This creates the per app `samna_user` row idempotently. Use the returned row.

### You're checking access on a hot path

```sql
-- Simplest case: yes/no.
SELECT EXISTS (
    SELECT 1 FROM claimius.get_access($user_id, $app_id, $object_id, $object_type, $required_level)
);
```

`get_access` returns at most one row (the strongest surviving grant). Empty result means no access.

### You need to list everything a user can see

```sql
SELECT * FROM claimius.get_objects($user_id, $app_id, 'public.device', 8);
```

Returns object_id, sa_access_level, denormalized name/description/link, and tree position columns. Already filtered for active claims, soft delete, and time validity.

### You're rendering an org chart in a frontend

```sql
SELECT * FROM claimius.get_owner_graph($user_id, $app_id);
```

Returns flat `(id, label, type, parent_id, data)` rows. Render as a tree in the frontend by grouping on `parent_id`.

### A claim grant should expire automatically

Pass `p_starts_at` and/or `p_ends_at` to `assign_claim_user`. `get_*` functions filter by current time. No background job required; expiration is read time.

### The disciple keeps falling behind on sync

Check `disciple_state.last_applied_seq` vs `prophet_state.last_applied_seq` on the prophet. The gap is your sync lag. Failures land in `reconcile_queue` and drain on the next read for that user, so transient failures self heal.

### You see a `user_object` row with stale data

Its grants array may contain claim_ids whose `user_claim` is now expired or soft deleted. The read time filter handles this. The row will be cleaned up the next time any structural change touches that `(user, object)` pair.

If you absolutely need to force a refresh, call `claimius.recompute_user_object($app_id, $user_id, $object_type, $object_id)`. This is rarely necessary.

## Naming conventions in this codebase

- `sa_*` prefix on columns: claimius internal columns. Never rename.
- `claimius.*` schema. Never rename.
- `tg_*` trigger names. Never rename.
- `_*` function names: internal, do not call from app code.
- `get_*`, `create_*`, `update_*`, `remove_*`, `assign_*`, `init_*`, `ensure_*`: public functions for app code.
- `calc_*`: trigger functions, not called directly.
- `build_*_tree`: full tree rebuilds. Called by init and by `migrate_root`. Avoid in app code.

## Things that look wrong but are correct

- `samna_user` has both `id` and `user_id`. `user_id` is the application identifier. Everything references `user_id`, never `id`.
- `samna_user` has multiple rows per human (one per app). They share `user_id` but have distinct `id`.
- `sa_owner_id` is `NOT NULL` everywhere. The root row points to itself.
- `claim_object.object_id` has no FK because the target table is dynamic. Integrity comes from the calc trigger plus registration.
- `user_object.id` is `text`, not `uuid`. It is a deterministic composite. Same on prophet and disciple.
- `samna_app.app_secret` is NOT a Claimius bootstrap input and `init_prophet` does NOT accept it as a parameter. The implementing service generates the secret externally (typically `sha256(seed || raw)`, raw returned to caller once) and writes it to `samna_app.app_secret` on demand. This is the API key disciples authenticate sync calls with. Do NOT add `p_system_app_secret` back to `init_prophet`. `private_key` and `private_seed` ARE init time inputs because they are fixed identity for the app; `app_secret` is rotatable consumer-facing credentials and lives outside init.

## Things that are wrong if you see them

- Direct SELECT from any `claimius.*` table in app code (always go through `get_*`).
- Direct INSERT/UPDATE on `claim`, `user_claim`, or `claim_object` (use the write functions).
- Direct INSERT/UPDATE on `user_object`, `object_users`, `user_users`, `sync_event`, `inheritance_info`, `table_info`, `reconcile_queue`. These are managed by triggers and the sync layer, never by app code.
- A registered table without `sa_owner_id` or `sa_created_by`.
- A registered table with only one of (`sa_parent_id`, `sa_root_id`).
- `sa_created_by` set to a user_id outside of bootstrap.
- Hard delete in app code on a registered table.
- Adding an FK from app tables to `samna_user.id`. Use `samna_user.user_id`.
- A query against `samna_user` without `app_id` in the filter.
- Editing `user_object` rows directly to change access.
- Overriding `claimius.replay_mode` outside the disciple sync layer.
- Calling `migrate_root` without flagging for human review.
- Hard coding the system app slug. Different deployments use different slugs.