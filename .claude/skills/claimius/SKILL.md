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
5. Inserting into `claim`, `user_claim`, or `claim_object` directly instead of using the write functions; OR writing to computed tables like `user_object`, `object_info`, `user_users`, `user_users_diff`, `usage_diff`, `inheritance_info`, `table_info`, `sync_event`, `access_diff`, `user_activity`, `claim_graph_cache` at all.
6. Modifying access calculations by editing `user_object` rows.
7. Forgetting `app_id` in filters, leaking access across apps.
8. Confusing `sa_created_by`, a `user_claim.id`, with a user identifier.
9. Hard deleting from registered tables.
10. Reparenting organizations across roots.
11. Adding `p_system_app_secret` or any equivalent to `upsert_system_app`. The app secret is consumer facing, generated externally on demand by the implementing service, written via UPDATE to `samna_app.app_secret`. It is NOT a bootstrap input. `private_key` and `private_seed` ARE init inputs because those are fixed identity. `app_secret` is rotatable credentials and lives outside init.

If you are about to do any of those, STOP and explain to the user what you're considering. Get explicit confirmation before proceeding.

## How Claimius works in one paragraph

Apps register their tables via `upsert_table_info`. Triggers attached at registration keep the closure and `claimius.object_info`, settle the acting user's pairs in `claimius.user_object`, the materialized "what can this user see" index, and record the area of the change in `claimius.access_diff` for every other user, whose next check settles the one pair and whose next listing settles their share. Reads go through `get_*` functions. Writes to claim metadata go through write functions. The same schema runs on prophets, the sources of truth, and disciples, the read replicas. Disciples pull the source rows of their app from a prophet's `sync_event` feed and apply each page in one transaction under `replay_mode = true`: no actor is settled and no event is emitted, but the closure is kept and the areas the rows move are raised, so every pair settles on the disciple's own reads. Derived rows never travel. Roles enforce the boundary: `claimius_writer` can write, `claimius_reader` cannot.

## Decision rules

### "Did I bind the resource to a claim after creating it?"

Every resource that participates in the access model must have at least one direct `claim_object` row pointing at it. After inserting an organization, location, app, or registered application table row, follow up with `insert_claim_object`. The owner claim is normally the claim the actor used to create the resource, the one referenced by `user_claim_id`. Pass an explicit claim id when the writer wants a different binding.

Two things are exempt: `samna_user` rows are handled by `user_users` and `upsert_app_user`, and a claim does not bind itself via `claim_object`.

### "Will this binding cascade?"

A cascade fires only when BOTH `claim.inherits = TRUE` AND the `claim_object.inherits = TRUE` for that specific binding. Either flag set to FALSE blocks cascade for that binding. A claim with `inherits = FALSE` blocks all of its cascades regardless of per binding flags.

### "Should I read directly from a claimius table?"

NO. Always use `get_*` functions:

- `get_access(user_id, app_id, object_id, object_type, min_level)` for access checks
- `get_direct_access(user_id, app_id, object_id, object_type, required?, raise?)` to find the closest user_claim via ancestor walk when `get_access` returns nothing
- `get_objects(user_id, app_id, object_type, min_level, after?, limit?)` to list visible objects, a page in `object_id` order after `after`
- `get_organizations`, `get_locations`, `get_secrets` for typed pages, each taking `after` and `limit`; `get_users`, `get_claims`, `get_audit` for typed lists
- `get_scope_access(user_id, app_id, jsonpath, min_level)` for grants with no object, such as page access
- `get_owner_graph`, `get_claim_graph` for hierarchy and access visualizations. Both take optional `start_id` and `depth` for subgraph queries, and a window `width`, `sort` (`name` or `usage`) and `next_id` that bounds the children of each type per parent. Both cover all registered tables in the deployment. Output rows have fields `object_id`, `object_type`, `label`, `parent_id`, and `data jsonb` where `object_type` is schema qualified. `data` always carries `access` and `level` plus per type fallback fields with `sa_` prefixes stripped.
- `search(user_id, app_id, query, types, min_level, limit, offset)` for full text search

### "Should I INSERT/UPDATE a claimius table directly?"

It depends on the table.

**Use the write function** for these:

- `claim` → `insert_claim`, `update_claim`, `delete_claim`
- `user_claim` → `insert_user_claim`, `update_user_claim`, `delete_user_claim`
- `claim_object` → `insert_claim_object`, `update_claim_object`, `delete_claim_object`
- `samna_user` per app row creation → `upsert_app_user`
- `samna_secret` creation → `insert_secret`, which stores the encrypted value and binds the secret to the actor's claim
- Guests → `upsert_guest_user`, prophet only
- `peer_grant` → `upsert_peer_grant`, prophet only; `get_peer_grant` checks a peer secret
- Cross app moves → `merge_user`, prophet set only; samna_auth owns the merge flow and calls it from `MergeUser` in `internal/service/user.go`
- Cross root moves → `migrate_root`, which hangs the object and its subtree under a live root of the same type, walking organizations through `sa_owner_id`, under `claimius.migrating_root`; flag every call for human review
- App creation → `upsert_system_app` for the system app, every row stamped with the system user's `user_claim.id`; `upsert_app(p_actor_user_claim_id, p_slug, p_name, p_org_id, p_private_key, p_private_seed, p_redirect_uris, p_sync_uri, p_description, p_connection, p_owner_access, p_default_claim, p_app_id)` for every other app, by an actor holding owner on `p_org_id` in its own app
- Bootstrap → `upsert_system_app` on the prophet set, taking the system app fields, and `upsert_disciple_state` on the disciple set, taking `p_disciple_app_slug`; the prophet set carries `upsert_disciple_state` and `upsert_prophet_peer` too. `upsert_prophet_peer` takes the system app fields plus `p_upstream_prophet`, `p_upstream_endpoint`, `p_upstream_app_slug` and `p_upstream_app_secret`, runs `upsert_system_app`, and registers the upstream in `prophet_peer`: a hybrid is a prophet mirroring one app of a peer prophet
- External table registration → `upsert_table_info`; deregistration → `delete_table_info`
- Self identity → `get_app()` returns the system app on prophet and hybrid, raises on disciple. `get_disciple_app()` returns the upstream app on a pure disciple and raises on a prophet and on a hybrid, which has no `disciple_state` row; `get_disciple_app_id()` raises with it. Both return `SETOF claimius.samna_app`. Scalar UUID variants `get_prophet_app_id()`, `get_disciple_app_id()`, and `get_app_id()` are available for column DEFAULTs and other places that reject subqueries. `get_app_id()` resolves to the system app id on prophet and hybrid and to the disciple app id on a pure disciple; it is the one to use on a hybrid. These helpers ship in the prophet set; the disciple set mirrors one app, carries none of them, and its read functions take no app argument. Use these instead of maintaining a local helper.

**Direct INSERT/UPDATE is fine** on the prophet for:

- `samna_app` updates only. An app is created by `upsert_system_app` or `upsert_app`.
- `organization`, `location`
- `samna_client`, `samna_secret` updates
- `object_field`, `user_field`, `user_relation`, `object_asset`
- `prophet_state`, `disciple_state`
- `prophet_peer`, by the prophet's admin only. `app_handover` only through `insert_app_handover` and `update_app_handover`

The triggers attached to each table fire either way, so calc, audit, and sync emission run identically. The function vs INSERT distinction is about validation, ergonomics, and getting non obvious sequences right.

**NEVER write directly** to these tables; they are managed by triggers and the sync layer:

- `user_object`, `object_info`, `user_users` for computed access state; the reverse lookup is the index `idx_user_object_object_user`, and `used_count` moves only through `touch_object_usage`
- `access_diff`, `user_activity`, `claim_graph_cache` managed by the settle, the drain and the graph: the areas, the readers' cursors and the pairs' marks
- `peer_collision` written by `apply_peer_event`
- `inheritance_info`, `table_info` computed by registration and calc functions
- `sync_event` append only, written by triggers
- `audit`. Use `claimius.insert_audit` if you must.
- `user_users_diff` and `usage_diff`, raised by the statement triggers and the usage flush and folded by the reads; there is no `reconcile_queue` and no `access_diff_user`

### "Should I DELETE from a registered table?"

NO. Soft delete by setting `sa_deleted_at = now()`. Hard delete only happens inside `merge_user` and calc functions cleaning up materialized state.

### "Can I add a `sa_parent_id` column to my table for self referencing?"

YES, but you MUST also add `sa_root_id`. Both come together. The root row has `sa_parent_id = id` and `sa_root_id = id`.

After adding columns, rerun `upsert_table_info` with the same table list. It will pick up the new columns.

### "What user identifier do I use?"

`samna_user.user_id`, the stable application identifier. NOT `samna_user.id`, the row PK. The same human user has multiple rows in `samna_user`, one per app, all sharing the same `user_id` but with distinct `id`. References everywhere are by `user_id`.

The `id` column exists only because Postgres needs a PK. Treat it as internal.

### "What goes in `sa_created_by`?"

A `user_claim.id`. Not a `samna_user.user_id`. The actor under whose authority the action was taken.

### "How do I get the user_claim.id to pass as `sa_created_by` for a write?"

Call `get_access`, `get_objects`, or `search`. Each returns a `user_claim_id` column alongside the access info. That's the actor token. **Contract: when `claim_id` is non null, `user_claim_id` is also populated.** Use it directly:

```sql
SELECT user_claim_id INTO :acting_user_claim
FROM claimius.get_access($user_id, $app_id, $object_id, $object_type, $required_level);

UPDATE public.recipe SET name = :new_name, sa_created_by = :acting_user_claim
WHERE id = :recipe_id;
```

When `get_access` returns no rows, fall back to `get_direct_access`. It walks the owner, location, and parenthood ancestor chains and returns the closest, highest privilege user_claim the user holds. Same row shape as `get_access`.

Other `get_*` functions, including `get_organizations`, `get_locations`, `get_users`, `get_claims`, `get_audit`, `get_secrets`, and the graphs, do not return `user_claim_id`. Call `get_access` for the specific object before writing.

### "How do I scope a query to one app?"

Always filter on `app_id`. Every materialized state table — `samna_user`, `user_claim`, `user_object`, `user_users`, `user_relation`, `user_field` — carries `app_id` on a prophet. The same human can have entirely separate access in different apps.

A `claim`, `user_claim` or `claim_object` stays inside its app: the prophet refuses one with `P0403` when its owning organization, or its bound organization or location, is not linked to the row's app through `app_object` (`_check_app_scope` over `app_has_scope`). Link the organization with `upsert_app_object` first.

`shared.UserWhere` filters a `samna_user` listing: a user always reads its own row, and `type = 'system'` users stay hidden from every other user.

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

Returns object id, the name, description, link, owner and location from `object_info`, the mask, the scope and the actor token, in `object_id` order. Pass `after` (the last id of the previous page) and `limit` for a page. Already filtered for active claims, windows and soft delete.

### "How do I implement a deny?"

Create a claim whose `sa_access` carries the deny bit `0x10` plus the bits to subtract; it is stored as given. Bind it via `insert_claim_object` and grant it via `insert_user_claim`. A deny bound on the object itself subtracts from every grant on it; a deny reaching the object through a tree subtracts only from grants that also reach it through a tree, so a grant bound or composed on the object itself survives an inherited deny. A deny with a field scope subtracts from those fields only. What remains is closed under implication: denying read drops execute, write and owner too. Levels come first: give the claim a `level` above 0 and its deny subtracts from every grant below that level whatever the classes, while a grant above the deny's level keeps its bits; at the same level the classes above decide, and level 0 everywhere is the rule as it always was. Binding or granting a claim above level 0 needs an actor that holds that level on the object, or write on the claim and no lower deny of its own lifted. The settle stores the result; you don't filter denies in your reads. There is no access level; `level` is precedence.

### "What's a path?"

A path is the pair `tree_type, cascaded_from`. `tree_type` is `direct` for a binding on the object itself and `ownership`, `location` or `parenthood` for a binding on an ancestor reached through that tree. A composed grant also carries `composed_from`, the linked claim, and takes the class of the binding it came through. Denies subtract by class: a direct deny from everything, a tree deny from tree grants.

### "Can I have multiple grants on the same object?"

Yes. The `user_object.grants` jsonb array carries one entry per contributing claim. `sa_access` on the row is the OR of the surviving grants after denies. A binding mask narrows its claim: each grant carries `claim & expand(coalesce(binding, claim))`, a composed one the bits both the link and the target binding carry. Removing one claim's contribution shrinks the array; the row stays as long as some grant or `direct_grant` survives.

### "How do I attach a file to a user, organization or other claimius object?"

Store the asset and its bytes in your own service, then write a `claimius.object_asset` link: `object_type` a `claimius.*` type, `object_id` the object (a user's `user_id`), `asset_id` your asset's id, `kind` such as `profile`, `banner`, `logo` or `file`, and `mime_type`. The asset never moves; only the link syncs, to every app that sees the object. It is not a registered table and takes no `claim_object` binding: access to a link is READ on its object.

- On a disciple write it through the builder, `NewInsert().Model(&shared.ObjectAsset{...}).Access(ac)` with `ac` WRITE on the object (self for the actor's own user). The prophet checks that WRITE again and stamps owner and grant; a link on the actor's own user takes its default claim grant in the home app and is refused without one.
- On the prophet insert, update or soft delete it directly with `app_id` the home app.
- Read with `get_object_assets`, in Go `GetObjectAssets(ctx, user, type, id, kind)` and `GetObjectAsset(ctx, user, linkID)`. Never SELECT the table to decide what a user may see.
- Serve the file from your own route with `disciple/echo.GetObjectAssetData(ctx, linkID)`, which fetches through the prophet. Give the middleware your storage with `sdb.Options.AssetSource` at `sdb.Init`, and optionally an `AssetCache`. Pass `Content-Type`, `Content-Length`, `ETag`, `Last-Modified` and `Cache-Control` through untouched.

See `.nimpress/claimius/assets.md`.

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

SELECT claimius.upsert_table_info('public.my_table');
```

`upsert_table_info` validates required columns. If you forgot `sa_created_by`, it raises and refuses registration.

## Creation order

Tables in claimius reference each other circularly: app↔org, claim→org, user_claim→claim, claim_object→claim+object. Skip a step or do them out of order and either FKs fail or calc triggers misfire.

**Fresh prophet bootstrap** is encapsulated in `upsert_system_app`. If the user is creating "the first app on this database", call `upsert_system_app` and don't replicate its body.

**A new app on an existing prophet:**

1. Owning org must exist, and the actor must hold owner on it in its own app.
2. `upsert_app` with the actor's `user_claim.id`: the app, its `prophet_state`, the actor's `samna_user` in the app, the organization links, the owner claim bound and granted, the default claim bound and set as `claim_id`. Its first binding passes on the actor's owner bit on the owning organization, because the new app has no pairs yet.
3. Optional: `upsert_table_info(...)` for any external tables.

**A new resource — org, location, app table row — inside an existing app:**

1. Direct INSERT with all required `sa_*` columns.
2. To grant someone access: `insert_claim` → `insert_claim_object` → `insert_user_claim`.

If a user asks you to create something and the order is non obvious, lay out the steps before writing SQL. Ask if you're not sure which step belongs first.

## Common patterns

### A user logs into an app

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

### Granting a permission

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

For the basics use the rendering columns `get_objects` joins from `object_info`:

```sql
SELECT object_id, sa_name, sa_description, sa_access
FROM claimius.get_objects($user_id, $app_id, 'public.recipe', $min_level);
```

## When you're unsure

Ask the user. Do not guess. Specifically ask:

- "I see this table has `sa_parent_id` but not `sa_root_id`. Is that a missing column or do you not want self referencing?"
- "Should this query filter by `app_id`? I assume yes since this is reading materialized state."
- "I'm about to call `merge_user`. This will hard delete rows for the source user. Confirm?"
- "Is this code on the prophet side or the disciple side? On a disciple, only the sync layer should write to claimius tables."
- "I'm about to add a column to a registered table. Should I rerun `upsert_table_info` after the migration?"

## What this skill prevents

When the AI assistant has Claimius rules loaded, it should:

- Refuse to write a SELECT from any `claimius.*` table directly. Always use `get_*` functions.
- Refuse to INSERT/UPDATE on `claim`, `user_claim`, `claim_object` without using the write functions.
- Refuse to INSERT/UPDATE/DELETE on `user_object`, `object_info`, `user_users`, `user_users_diff`, `usage_diff`, `inheritance_info`, `table_info`, `sync_event`, `audit`, `access_diff`, `user_activity`, `claim_graph_cache` or `peer_collision` ever.
- Allow direct INSERT/UPDATE on `samna_app` updates, `organization`, `location`, `samna_client`, `samna_secret` updates, `object_field`, `user_field`, `user_relation`, `prophet_state`, `disciple_state` on the prophet side, with the required `sa_*` columns populated.
- Reject any registered table missing required columns and tell the user.
- Always include `app_id` in filters against materialized state.
- Default to soft delete; raise hard delete as a flag for the user.
- Never set `replay_mode` outside the sync layer, the disciple pull and a prophet's pull of a peer.
- Never write a row of an app another prophet decides: it is refused with `P0409`, or `409` with `SAuth-Home` over HTTP. Send it to the home.
- Never call `get_disciple_app` on a hybrid; use `get_app_id`.
- Never INSERT a `samna_app` row by hand; use `upsert_system_app` or `upsert_app`.
- Never call `_grant_prophet_roles` or `_grant_disciple_roles`. The role grants live in `V6.0__claimius_grants.sql`, the last base file, and run on every build and replay of the set.
- Never confuse `samna_user.id` with `samna_user.user_id`.
- Never set `sa_created_by` to a `samna_user.user_id`.
- Treat self referencing tree roots as `parent == root == id`, never null.

If the AI is about to break any of these, it must stop, surface the concern, and ask. The user's experience with this codebase taught them not to trust shortcuts. Be conservative.
