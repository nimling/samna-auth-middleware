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
   app_object app_object_ids apps_for_object link_app_object app_has_object
   unlink_app_object create_root_organization, scope claim ScopeAccess
   jsonb_path_exists machine client service to service client credentials,
   init init_disciple init_hybrid init_claimius_tables
   init_claimius_internal ensure_app_user merge_user migrate_root,
   calc_object_access calc_hierarchical_access calc_claim_access
   emit_sync_event audit_trigger replay_mode reconcile_queue access_diff
   prophet_state disciple_state, register a table for access control,
   permission template, role grant claimius_writer claimius_reader
   claimius_disciple_client, org chart access graph hierarchy rendering,
   full text search over accessible objects.
hard_constraints:
   - reads always go through `get_*` functions, never direct SELECT on claimius tables
   - writes go through a write function when one exists; otherwise direct INSERT/UPDATE on the prophet side is fine. Triggers fire either way: they settle `user_object` for the acting user and mark the area in `access_diff` for everyone else.
   - `user_object` for a user other than the writer is settled by that user's next read, through `reconcile_if_pending`. Never assume another user's row exists immediately after a write.
   - hard delete is forbidden in app code; default is soft delete via `sa_deleted_at`
   - `sa_created_by` is a `user_claim.id`, not a `samna_user.user_id`
   - `samna_user.user_id` is the human stable identifier; `samna_user.id` is the row PK
   - tree roots are self referential. Orgs root when `sa_owner_id == sa_root_id == id`. Locations and parenthood tables root when `sa_parent_id == sa_root_id == id`. No nullable parents.
   - `sa_parent_id` and `sa_root_id` come together or not at all
   - every materialized state query must filter on `app_id`
   - `organization` and `location` are the exception: no `app_id` column, scoped through `claimius.app_object` links, filtered with `app_object_ids(app_id, object_type)`
   - registered external tables may omit the `app_id` column. Claimius infers it from `get_app_id()` at trigger time. On prophets that host more than one app the column is required, and registration is rejected without it.
   - object types are always schema qualified like `public.device`, never bare `device`
   - `claimius.replay_mode` is set ONLY by the disciple sync layer, never by app code
   - cross root reparenting is forbidden in normal flow; admin must use `migrate_root`
   - composite ids use the format `<schema>.<table>:<uuid>` joined by colons
ask_user_when_unsure: true
do_not_invent: true
---

# Claimius rules

This project uses **Claimius** for access control. Claimius is a Postgres schema named `claimius` that materializes user access into a denormalized index. Application code reads through `get_*` functions, never direct SELECTs against claimius tables.

Materialization is paid on read, not on write. A write settles the acting user, so a writer reads its own change back inside the same request, and records the changed area in `claimius.access_diff`. The next read that targets that area recalculates it for every user in it and clears the row, first come first serve. An area nobody reads is never computed. This keeps write cost constant in the number of users and is why a write must never assume another user's `user_object` row exists yet.

This document is a usage guide. It tells you which functions to call, which tables you may write to, and which patterns to follow. It does not describe how the engine computes access. Trust the engine and call the documented entry points.

## Mental model

Three orthogonal hierarchies. Every registered row has a position in at least one.

1. **Ownership**: rooted at organizations. Reached via `sa_owner_id`. A row is a tree root iff `sa_owner_id == sa_root_id == id`.
2. **Location**: rooted at top level locations. Reached on registered rows via `sa_location_id`. Location rows form a tree via `sa_parent_id`.
3. **Parenthood**: any registered table can declare its own self referencing tree by including `sa_parent_id` AND `sa_root_id`. Both must come together.

Claims are permission templates. They are granted to users via `user_claim` and bound to objects via `claim_object`. Set `inherits = TRUE` on both the claim and the binding to cascade through whichever hierarchy applies.

## Hard rules

### Owner claim rule

Every resource that participates in the access model must carry at least one direct `claim_object` row binding it to some claim. The owner claim is the claim used to create the resource, captured from the actor's `user_claim_id` at write time, or one specified explicitly by the writer. Without a direct binding, access depends entirely on cascade and the resource is invisible the moment the cascade chain is interrupted.

Two resources are exempt: `samna_user` rows are handled by `user_users` denormalization and `ensure_app_user`, and a claim does not bind itself via `claim_object`.

When the implementer inserts an organization, location, app, or registered application table row, follow with `assign_claim_object` before any read attempts to find the row.

### Inheritance is a logical AND

To cascade, both `claim.inherits = TRUE` and `claim_object.inherits = TRUE` must hold. Either flag set to FALSE blocks cascade for that binding.

### Reads only through `get_*` functions

```sql
-- WRONG. Bypasses access control. Will fail under claimius_reader role.
SELECT * FROM claimius.user_object WHERE user_id = $1;

-- WRONG. Same.
SELECT * FROM public.device WHERE id = $1;

-- RIGHT. Access checked, soft delete cascade, expired claim filtering, all handled.
SELECT * FROM claimius.get_objects($user_id, $app_id, 'public.device', $min_level);
```

The `get_*` family:

- `get_access(user, app, object_id, object_type, min_level)` — strongest active claim grant. Returns `user_claim_id` as the actor token. Only returns a row when at least one surviving claim covers the object.
- `get_direct_access(user, app, object_id, object_type)` — same row shape as `get_access`. Walks the owner, location, and parenthood ancestor chains and returns the closest, highest privilege user_claim the user holds on any ancestor. Use as a fallback when `get_access` returns nothing but the caller still expects an actor token.
- `get_objects(user, app, object_type, min_level)` — all visible objects of one type.
- `get_organizations(user, app, min_level)` — visible organizations.
- `get_locations(user, app, min_level)` — visible locations.
- `get_users(user, app, min_level)` — visible users.
- `get_claims(user, app, min_level)` — claims the user holds.
- `get_claims(user, app, object_id, object_type)` — claims against one object.
- `get_audit(user, app, min_level, limit, offset)` — audit log.
- `get_secrets(user, app, min_level)` — visible secrets.
- `get_owner_graph(user, app, start_id?, depth?, min_level?)` — ownership graph covering orgs, locations, and registered tables with `sa_owner_id` or `sa_location_id`.
   - `start_id` is an org to root from. NULL means all roots.
   - `depth` is levels of descent. 0 means unlimited.
   - Each row has fields `object_id`, `object_type`, `label`, `parent_id`, and `data jsonb`. `object_type` is schema qualified, like `'claimius.organization'` or `'public.bookable'`.
   - `data` always carries `access`, the bitwise mask from `user_object.sa_access`, and `level`, plus per type fallback fields with `sa_` prefixes stripped, so `owner_id` rather than `sa_owner_id`.
- `get_claim_graph(user, app, start_id?, depth?, min_level?)` — access graph covering the user, other users seen, claims, user_claims, claim_object connectors, and reachable objects.
   - `start_id` is a single claim. NULL means all claims the user holds.
   - `depth` 0 includes cascaded objects. depth >= 1 returns direct only.
   - Same row shape as `get_owner_graph`. Topology connects user → user_claim and claim → claim_object → object so the frontend gets a connected graph.
- `search(user, app, query, types, min_level, limit, offset)` — full text search.

### Writes: function if it exists, direct INSERT otherwise

Write functions return a single jsonb object keyed by the affected table name. `create_claim` returns `{"claim": {...}}`, `assign_claim_user` returns `{"user_claim": {...}}`, `init` returns six keys: `samna_app`, `samna_user`, `organization`, `claim`, `claim_object`, `user_claim`. Capture and unpack with the `->` and `->>` jsonb operators.

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
        p_app_id        => $app_id,
        p_name          => 'Editor',
        p_description   => 'Read and write',
        p_sa_access     => 6,
        p_sa_owner_id   => $org_id,
        p_sa_root_id    => $root_org_id,
        p_sa_created_by => $user_claim_id
    );
    v_claim_id := (v_result -> 'claim' ->> 'id')::UUID;
END $$;

-- Direct INSERT is fine on tables without a write function. Calc triggers
-- attached to the table maintain everything afterward. organization and
-- location carry no app_id; link the row to the app afterwards.
INSERT INTO claimius.organization (
    name, sa_owner_id, sa_root_id, sa_level, sa_created_by
) VALUES (
    'Acme NYC', $parent_org_id, $parent_org_root_id, 1, $user_claim_id
) RETURNING id INTO v_org_id;

SELECT claimius.link_app_object($app_id, 'claimius.organization', v_org_id, $user_claim_id);
```

Tables that have a write function:

- `claim` → `create_claim`, `update_claim`, `remove_claim`
- `user_claim` → `assign_claim_user`, `update_claim_user`, `remove_user_claim`
- `claim_object` → `assign_claim_object`, `update_claim_object`, `remove_claim_object`
- `samna_user` → `ensure_app_user` for the per app row
- Cross app moves → `merge_user`
- Cross root moves → `migrate_root`. Admin only, rare.
- Bootstrap → `init`, which on the prophet set takes the system app fields and on the disciple set takes `p_disciple_app_slug`; the prophet set also carries `init_disciple` and `init_hybrid`, the latter requiring both `p_system_app_slug` and `p_disciple_app_slug`
- Table registration → `init_claimius_tables`
- Self identity → `get_prophet_app()` returns the system app on prophet and hybrid. `get_disciple_app()` returns the upstream app on disciple and hybrid. Both return `SETOF claimius.samna_app`. Scalar UUID variants `get_prophet_app_id()`, `get_disciple_app_id()`, and `get_app_id()` exist for `DEFAULT` expressions and other places that reject subqueries. `get_app_id()` returns the prophet/system app id on prophet and hybrid, the disciple app id on pure disciple. Use these instead of maintaining a local helper.

Tables where direct INSERT/UPDATE is the documented path:

- `samna_app`. Use `init` for the FIRST app only.
- `organization`. Prefer `create_root_organization` for roots; it links `app_object` for you.
- `location`
- `samna_client`, `samna_secret`
- `object_field`, `user_field`, `user_relation`
- `prophet_state`, `disciple_state`

`app_object` has write functions and is never written directly: `link_app_object`, `unlink_app_object`. Both maintain the materialized access rows for the pair.

You may NEVER write directly to:

- `user_object`, `object_users`, `user_users`. Materialized state, computed by triggers.
- `inheritance_info`, `table_info`. Computed by registration and calc functions.
- `sync_event`. Append only, written by triggers.
- `audit`. Write via `claimius.write_audit` if you must.
- `reconcile_queue`. Managed by sync layer.
- `access_diff`. Written by the calc triggers, cleared by the read that settles the area.

### `sa_created_by` is `user_claim.id`, never `samna_user.user_id`

The actor recorded in every row is the assignment under whose authority the action was taken. This decouples actor identity from user identity, so `merge_user` can move users without breaking authorship references.

### `user_claim_id` from `get_access` is the actor token for follow on writes

`get_access`, `get_objects`, and `search` return a `user_claim_id` column. **Contract: when a row is returned, `user_claim_id` is populated.** Callers use it as `sa_created_by` on follow on writes within the same request.

When `get_access` returns no rows but the user is expected to have access via structural relationship, call `get_direct_access`. It walks the owner, location, and parenthood ancestor chains and returns the closest, highest privilege user_claim the user holds on any ancestor, in the same row shape as `get_access`. If both functions return nothing, the user has no path to act on the object.

The list and graph functions `get_organizations`, `get_locations`, `get_users`, `get_claims`, `get_audit`, `get_secrets`, `get_owner_graph`, `get_claim_graph` do NOT return `user_claim_id`. To get the actor token after listing, call `get_access` for the specific object the caller is about to act on.

### Composite ids are `<schema>.<table>:<uuid>`

Object types are always schema qualified, in the database and on the wire. `public.device`, never bare `device`. `init_claimius_tables` raises on an unqualified name. The middleware carries a consumer type untouched in both directions and validates only claimius's own: `shared.ResolveObjectType` resolves a bare claimius name, refuses a `claimius.` type the schema does not register, and passes every other type through. `ObjectTypeOf` returns a model's `table:` tag as written, so a consumer qualifies its tags. `ObjectType.Validate` is the opt in check a consumer calls on its own types. The prophet has no resolver of its own.

The composite id format is colon separated: `claimius.composite_id('public.device', 'aaaa...')` returns `'public.device:aaaa...'`.

### `sa_owner_id == sa_root_id == id` defines a tree root

There are no nullable parent columns. Roots self reference. `sa_owner_id = id` on orgs. `sa_parent_id = id` on locations and parenthood tables. Walking ownership: stop when `sa_owner_id = id`. Walking location and parenthood: stop when `sa_parent_id = id`.

For root creation with an idempotency guard on app and name, use `claimius.create_root_organization`. New root rows leave the parent column NULL or set it to `NEW.id`; the auto root trigger self references both. Children set the parent and leave `sa_root_id` NULL; the trigger copies the parent's `sa_root_id`. Internal claimius tables get this trigger automatically. Implementer registered parenthood tables get it via `init_claimius_tables`.

### App scoping

There are two scoping mechanisms and the table decides which one applies.

Own `app_id` column. `samna_user`, `claim`, `user_claim`, `claim_object`, `user_object`, `object_users`, `user_users`, `user_relation`, `user_field`, `object_field`, `samna_client`, `samna_secret`, `audit`. One row belongs to exactly one app. The same human user gets one `samna_user` row per app, all sharing the same `user_id`. Any query hitting these tables includes `app_id` in the filter.

`app_object` links. `organization` and `location` carry no `app_id` column. They are shared hierarchy objects, and an object belongs to every app holding a live `claimius.app_object` row for it. One organization can serve several apps without duplication.

The helpers on the prophet:

- `app_object_ids(app_id, object_type)` returns the object ids of one type linked to an app. This is the filter to use in place of `app_id = ?` for organizations and locations.
- `apps_for_object(object_type, object_id)` returns the app ids holding a live link to one object.
- `app_has_object(app_id, object_type, object_id)` is the boolean guard.
- `link_app_object(app_id, object_type, object_id, created_by)` creates the link and materializes access for the pair. Idempotent.
- `unlink_app_object` soft deletes the link and purges the materialized rows it granted.
- `create_root_organization(app_id, name, ...)` creates the root and links it in one call, idempotent on app and name.

The unique index is `(app_id, object_type, object_id)` where `sa_deleted_at IS NULL`, so a live link is unique per app and object.

Disciples do not carry `app_object` at all, and their `organization` and `location` tables have no `app_id`. A disciple mirrors exactly one app, so every row it holds is already in scope. The sync layer strips `app_id` from each payload before upsert, and the prophet selects organizations and locations for the snapshot through `app_object_ids`. Write the app filter on the prophet side only.

### Access bits

`sa_access` is an INTEGER bitwise mask on `claim`, `claim_object` and `user_object`. Check with `(stored & required) = required`. The Go constants are `shared.ClaimAccess` in `shared/models.go`.

| Bit | Value | Constant |
|-----|-------|----------|
| owner | `0x01` | `ACCESS_OWNER` |
| write | `0x02` | `ACCESS_WRITE` |
| read | `0x04` | `ACCESS_READ` |
| execute | `0x08` | `ACCESS_EXECUTE` |
| deny | `0x10` | `ACCESS_DENY` |
| member | `0x20` | `ACCESS_MEMBER` |

`ACCESS_ALL` is owner, write, read and execute unioned, so `15`. Common grants: read only is `4`, read with write is `6`, read with execute is `12`.

On a grant claim with the deny bit clear, `create_claim` expands the mask: owner implies write, read and execute; write implies read and execute; execute implies read. The member bit stands alone. A `claim_object.sa_access` of NULL inherits the claim mask at read time.

There is no access level. Any column, parameter or payload key naming one is wrong.

### Deny semantics

To deny, create a claim with the deny bit `0x10` set on `sa_access` and the other bits enumerating which permissions to subtract, then bind it via `assign_claim_object`. Deny claims store the supplied bits verbatim, so any subset of read, write, execute and owner can be denied. Calc functions remove blocked grants from `user_object` at write time, so reads never need to filter for denies.

### Adding a column to a registered table

To add an inheritance column `sa_parent_id`, `sa_root_id`, or `sa_location_id` to a registered table after it is in production:

1. Add the column with a migration.
2. Backfill values.
3. Re-run `init_claimius_tables(...)` with the same table list. The function is idempotent. It picks up the new columns and rebuilds the relevant `inheritance_info` rows. The bulk seed runs under `replay_mode` so triggers stay quiet during the rebuild.

Never add `sa_parent_id` without `sa_root_id`. `init_claimius_tables` will refuse the registration.

### Soft delete vs hard delete

Default for everything is soft delete: set `sa_deleted_at = now()`. Never `DELETE FROM` registered tables in app code.

### Cross root reparenting is forbidden

To move an organization to a different root tree, the calc trigger will raise. Use `claimius.migrate_root(object_type, object_id, new_root_id)`. This is rare and should be flagged for human review.

## Creation order on a prophet

A prophet has tables that reference each other circularly. Get the order wrong and you'll get FK violations or rows that the calc triggers can't make sense of.

### Fresh prophet bootstrap

Encapsulated in `init`. Call `init` and don't replicate its body.

### A new non system app on an existing prophet

1. Owning org must exist, either an existing org or a fresh root org.
2. INSERT `samna_app` with all per app fields. `sa_owner_id` points at the owning org.
3. INSERT `prophet_state` row with `last_applied_seq = 0`.
4. Optionally create a default claim for the app and set `samna_app.claim_id`.
5. Call `init_claimius_tables(...)` to register any external tables.

### Resources inside an existing app

1. Org if needed: direct INSERT.
2. Location if needed: direct INSERT.
3. App table rows: direct INSERT.
4. Grants: `create_claim` if needed, then `assign_claim_object`, then `assign_claim_user`.

If you're tempted to skip steps because the FK doesn't exist yet anyway, stop and check the order. Calc triggers fire on every INSERT and assume the order above held.

## What to do when

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
SELECT EXISTS (
   SELECT 1 FROM claimius.get_access($user_id, $app_id, $object_id, $object_type, $required_level)
);
```

`get_access` returns at most one row, the strongest surviving grant. Empty result means no access.

### You need to list everything a user can see

```sql
SELECT * FROM claimius.get_objects($user_id, $app_id, 'public.device', 8);
```

Returns object_id, sa_access as a bitwise mask, denormalized name, description, and link, plus tree position columns. Already filtered for active claims, soft delete, and time validity.

### You're rendering an org chart in a frontend

```sql
-- Whole accessible ownership graph
SELECT * FROM claimius.get_owner_graph($user_id, $app_id);

-- Subtree of one specific org, two levels deep
SELECT * FROM claimius.get_owner_graph($user_id, $app_id, $start_org_id, 2);
```

Returns flat rows with fields `object_id`, `object_type`, `label`, `parent_id`, and `data`. `object_type` is schema qualified, like `'claimius.organization'` or `'public.bookable'`. The `data` jsonb always carries `access`, the bitwise mask, and `level`. Render as a tree in the frontend by grouping on `parent_id`. To enrich rows with table specific fields, write your own wrapper that UNIONs claimius's output with rows from your app tables filtered through `claimius.get_objects`.

### A claim grant should expire automatically

Pass `p_starts_at` or `p_ends_at` to `assign_claim_user`. `get_*` functions filter by current time. No background job required; expiration is read time.

### A user cannot see something another user just created

Expected until that user reads. The write marked the area in `access_diff` and settled only the writer. The next `get_*` call for the other user claims the area, recalculates it, and clears the row. If it persists past a read, check that the read entered through a `get_*` function rather than a direct SELECT, since `reconcile_if_pending` runs at the top of the `get_*` family and nowhere else.

### The disciple keeps falling behind on sync

Check `disciple_state.last_applied_seq` against `prophet_state.last_applied_seq` on the prophet. The gap is your sync lag. Failures land in `reconcile_queue` and drain on the next read for that user, so transient failures self heal.

### You see a `user_object` row with stale data

Its grants array may contain claim_ids whose `user_claim` is now expired or soft deleted. The read time filter handles this. The row will be cleaned up the next time any structural change touches that user and object pair.

To force a refresh, call `claimius.recompute_user_object($app_id, $user_id, $object_type, $object_id)`. This is rarely necessary.

## Naming conventions in this codebase

- `sa_*` prefix on columns: claimius internal columns. Never rename.
- `claimius.*` schema. Never rename.
- `_*` function names: internal, do not call from app code.
- `get_*`, `create_*`, `update_*`, `remove_*`, `assign_*`, `init_*`, `ensure_*`: public functions for app code.

## Things that are wrong if you see them

- Direct SELECT from any `claimius.*` table in app code. Always go through `get_*`.
- Direct INSERT/UPDATE on `claim`, `user_claim`, or `claim_object`. Use the write functions.
- Direct INSERT/UPDATE on `user_object`, `object_users`, `user_users`, `sync_event`, `inheritance_info`, `table_info`, `reconcile_queue`. These are managed by triggers and the sync layer, never by app code.
- A registered table without `sa_owner_id` or `sa_created_by`.
- A registered table with only one of `sa_parent_id` and `sa_root_id`.
- `sa_created_by` set to a `samna_user.user_id`.
- Hard delete in app code on a registered table.
- Adding an FK from app tables to `samna_user.id`. Use `samna_user.user_id`.
- A query against `samna_user` without `app_id` in the filter.
- A query filtering `organization` or `location` by `app_id`. That column does not exist; use `app_object_ids`.
- Direct INSERT/UPDATE on `app_object`. Use `link_app_object` and `unlink_app_object`.
- Editing `user_object` rows directly to change access.
- Overriding `claimius.replay_mode` outside the disciple sync layer.
- Calling `migrate_root` without flagging for human review.
- Hard coding the system app slug. Different deployments use different slugs.
