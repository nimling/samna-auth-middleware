---
name: claimius
description: "Writing or reviewing code in any Samna repo that talks to Claimius, the Postgres claim based access control schema named `claimius` that samna_auth_middleware ships to the prophet samna_auth and to every disciple. Use when SQL or app code reads from or writes to `claimius.*` tables, views or functions; when code touches `sa_owner_id`, `sa_root_id`, `sa_parent_id`, `sa_location_id`, `sa_created_by`, `sa_deleted_at` or `app_id` columns; when a migration registers an application table for access control; when a Go handler checks access through `MutationAccess`, `GetAccess`, `UserAccess`, `RootAccess`, `ScopeAccess` or `sdb.ObjectWhere`, or writes claims, grants and bindings through the builder; when code creates claims, user_claims, claim_objects, apps, organizations, locations, guests, secrets or asset links; when a change touches the prophet/disciple architecture, the sync, `replay_mode`, denies, levels or the access mask; any time the schema name `claimius` appears in code, migrations or comments. Holds the function surface of both role sets, the Go access calls and the SQL patterns, and stops the changes that have historically broken this codebase. Not for running a migration, which is the smig skill, not for creating apps, clients and grants on a live prophet, which is the apps skill with sauth, and not for releasing the schema, which is the middleware deploy skill."
allowed-tools: Read, Grep, Glob
---

How code reads, writes and checks access through `claimius`. Running migrations is the `smig` skill; apps, clients and grants on a live prophet are the `apps` skill; releasing the schema is the middleware `deploy` skill.

## Stop first

Claimius has been broken several times by confident changes that bypassed the architecture. Before doing any of the following, stop, tell the user what is about to happen and get explicit confirmation.

1. Reading `claimius.user_object` directly instead of through a `get_*` function.
2. Treating `samna_user.id` as the user identifier; it is `samna_user.user_id`.
3. Adding `sa_parent_id` without `sa_root_id`, or the reverse.
4. Making `sa_owner_id` or `sa_parent_id` nullable for a root. Every parent column is NOT NULL; roots self reference.
5. Inserting into `claim`, `user_claim` or `claim_object` directly instead of the write functions, or writing at all to `user_object`, `object_info`, `user_users`, `user_users_diff`, `usage_diff`, `inheritance_info`, `table_info`, `sync_event`, `access_diff`, `user_activity` or `claim_graph_cache`.
6. Changing access by editing `user_object` rows.
7. Leaving `app_id` out of a filter, which leaks access across apps.
8. Using a user identifier for `sa_created_by`, which is a `user_claim.id`.
9. Hard deleting from a registered table.
10. Reparenting an organization across roots.
11. Adding `p_system_app_secret` or anything like it to `upsert_system_app`. The app secret is rotatable, generated on demand by the implementing service and written by UPDATE to `samna_app.app_secret`; `private_key` and `private_seed` are the fixed identity and are init inputs.

## The model

1. Apps register their tables with `upsert_table_info`. The triggers attached at registration keep the closure and `claimius.object_info`, settle the acting user's pairs in `claimius.user_object`, and record the area of the change in `claimius.access_diff` for every other user.
2. Another user's next check settles the one pair and its next listing settles its share; never assume another user's `user_object` row exists after a write.
3. Reads go through `get_*` functions; claim metadata is written through the write functions.
4. The same schema runs on prophets, the sources of truth, and disciples, the read replicas. A disciple pulls its app's source rows from the prophet's `sync_event` feed and applies each page in one transaction under `replay_mode = true`: no actor settles and no event is emitted, the closure is kept and the areas are raised, so every pair settles on the disciple's own reads. Derived rows never travel.
5. Roles hold the boundary: `claimius_writer` writes, `claimius_reader` cannot.

## Reads

1. Never read a claimius table directly for access; use the functions. Full signatures: `references/functions.md` Reads.
2. `get_access(user_id, app_id, object_id, object_type, required_access)` is the access check: the strongest surviving grant or no row. Gate with `SELECT EXISTS (SELECT 1 FROM claimius.get_access(...))`.
3. `get_direct_access(user_id, app_id, object_id, object_type, required?, raise?)` walks the owner, location and parenthood chains for the closest, highest privilege `user_claim` when `get_access` returns nothing. Same row shape.
4. `get_objects(user_id, app_id, object_type, required_access, after?, limit?)` lists what a user sees in `object_id` order, with the name, description, link, owner and location from `object_info`, the mask, the scope and the actor token; pass the last id as `after` for the next page. Already filtered for active claims, windows and soft delete.
5. `get_organizations`, `get_locations`, `get_secrets` are typed pages with `after` and `limit`; `get_users`, `get_claims`, `get_audit` typed lists.
6. `get_scope_access(user_id, app_id, jsonpath, required_access)` reads grants with no object, such as page access.
7. `get_owner_graph` and `get_claim_graph` take optional `start_id` and `depth`, and a window `width`, `sort` (`name` or `usage`) and `next_id` bounding the children of each type per parent. Rows are `object_id`, `object_type` schema qualified, `label`, `parent_id`, and `data jsonb` with `access`, `level` and the per type fields with `sa_` stripped.
8. `search(user_id, app_id, query, types, required_access, limit, offset)` is full text search; the prophet set adds a 12 argument overload with id, owner subtree and parent filters and a `total_count` column. The prophet set also carries `get_ancestors` and `get_descendants(p_object_type, p_object_id)` and the graph link builders `get_graph_links_owner` and `get_graph_links_claim`: `references/functions.md` Reads 14 to 16.
9. On a disciple, the lookups by id with no reader and no access filter, soft deleted rows included, for what an admitted row names: `get_users`, `get_organizations`, `get_locations`, `get_claims`, `get_grants`, `get_bindings` with each binding's `effective_access`, `get_user_identities`, `get_object_types`, and `get_object_usage` for the reader's `used_count`. Also `get_relations(user_id, ids)`, `get_audit(user_id, required, limit, offset, ids)` and `get_descendants(object_id)` for a subtree of unknown type.
10. A bun select or belongs-to join on `samna_user`, `organization`, `location`, `claim` or `user_claim` reads the views `claimius.get_users`, `get_organizations`, `get_locations`, `get_claims`, `get_grants`; the shared models carry `select:claimius.get_<name>`, so bun reads the view and writes the table. Never join the table itself.
11. `shared.UserWhere` filters a `samna_user` listing: a user always reads its own row and `type = 'system'` users stay hidden from everyone else.

## The actor token

1. `sa_created_by` is a `user_claim.id`, the actor under whose authority the action ran, never a `samna_user.user_id`.
2. `get_access`, `get_objects` and `search` return `user_claim_id` beside the access; when `claim_id` is non null, `user_claim_id` is populated. Use it directly:

```sql
SELECT user_claim_id INTO :acting_user_claim
FROM claimius.get_access($user_id, $app_id, $object_id, $object_type, $required_access);

UPDATE public.recipe SET name = :new_name, sa_created_by = :acting_user_claim
WHERE id = :recipe_id;
```

3. No row from `get_access`: fall back to `get_direct_access`.
4. `get_organizations`, `get_locations`, `get_users`, `get_claims`, `get_audit`, `get_secrets` and the graphs return no `user_claim_id`; call `get_access` on the object before writing.

## Writes through a function

1. `claim`: `insert_claim`, `update_claim`, `delete_claim`. `user_claim`: `insert_user_claim`, `update_user_claim`, `delete_user_claim`. `claim_object`: `insert_claim_object`, `update_claim_object`, `delete_claim_object`.
2. The per app `samna_user` row: `upsert_app_user`. Guests: `upsert_guest_user`, prophet only, and `update_claim_guest`, which sets `claim.guest_mode`; a guest reads at most. Access codes: `insert_code`, `delete_code`, `update_code_used`, `redeem_code`, written on the prophet by hash, never by value, and resolved on a disciple by `get_code`. Secrets: `insert_secret`, which stores the encrypted value and binds it to the actor's claim. The value is AES GCM ciphertext under the app's `private_seed`, stored on the prophet wrapped under `SAUTH_MASTER_KEY`. A deployed disciple gets its own app's seed from `GET /sam/app` at startup and holds it in memory only; a debug disciple uses `SAUTH_DEBUG_PRIVATE_SEED`. `SamnaSecret.Resolve(ctx, db, userID)` (`shared/secret.go:141`) reads the value through `get_secret_value(user, secret)`, which answers it only to a user with read on the secret, and opens it in process; a background job passes the user of the row's `sa_created_by` grant. No `/sam` route answers plaintext. `peer_grant`: `upsert_peer_grant`, prophet only; `get_peer_grant` checks a peer secret.
3. Cross app moves: `merge_user`, prophet set only, called by samna_auth from `MergeUser` in `samna_auth/internal/service/user.go`. It hard deletes the source user's rows; confirm first.
4. Cross root moves: `migrate_root`, which hangs the object and its subtree under a live root of the same type, organizations through `sa_owner_id`, under `claimius.migrating_root`. Flag every call for human review.
5. Apps: `upsert_system_app` for the system app, every row stamped with the system user's `user_claim.id`; `upsert_app(p_actor_user_claim_id, p_slug, p_name, p_org_id, p_private_key, p_private_seed, p_redirect_uris, p_sync_uri, p_description, p_connection, p_owner_access, p_default_claim, p_app_id, p_parent_id)` for every other app, by an actor holding owner on `p_org_id` in its own app; with `p_parent_id` it registers an Initiate app, another frontend of the parent with its own slug, redirect URIs, pages and style, and creates nothing else. Never INSERT a `samna_app` row.
6. Bootstrap: `upsert_system_app` on the prophet set; `upsert_disciple_state(p_disciple_app_slug)` on the disciple set. The prophet set also carries `upsert_disciple_state` and `upsert_prophet_peer`, which takes the system app fields plus `p_upstream_prophet`, `p_upstream_endpoint`, `p_upstream_app_slug`, `p_upstream_app_secret`, runs `upsert_system_app` and registers the upstream in `prophet_peer`: a hybrid mirrors one app of a peer prophet.
7. Registration: `upsert_table_info` registers, `delete_table_info` deregisters.
8. Self identity, prophet set only: `get_app()` returns the system app and raises on a pure disciple; `get_disciple_app()` returns the upstream app on a pure disciple and raises on a prophet and on a hybrid; both return `SETOF claimius.samna_app`. Scalar `get_prophet_app_id()`, `get_disciple_app_id()` and `get_app_id()` serve column DEFAULTs; `get_app_id()` is the one on a hybrid. The disciple set carries none of them and its reads take no app argument. Never keep a local helper for this.
9. Write functions return one jsonb keyed by table, `{"claim": {...}}`; read ids as `(v -> 'claim' ->> 'id')::uuid`.
10. Notifications, prophet set: `notify_object` fans out to every user holding read on the object, the actor excluded; `notify_user` writes to named users: `references/functions.md` Writes 13.

## Writes direct, on the prophet

1. `samna_app` updates only; an app is created by `upsert_system_app` or `upsert_app`.
2. `organization`, `location`, `samna_client`, `samna_secret` updates, `object_field`, `user_field`, `user_relation`, `object_asset`, `prophet_state`, `disciple_state`, with every required `sa_*` column populated.
3. `prophet_peer` by the prophet's admin only; `app_handover` only through `insert_app_handover` and `update_app_handover`.
4. Triggers fire either way, so calc, audit and sync emission run the same; the function exists for validation and for the order of non obvious sequences.

## Never written

1. `user_object`, `object_info`, `user_users`: computed access. The reverse lookup is the index `idx_user_object_object_user`; `used_count` moves only through `touch_object_usage`.
2. `access_diff`, `user_activity`, `claim_graph_cache`: the settle, the drain and the graph.
3. `peer_collision`, written by `apply_peer_event`. `inheritance_info`, `table_info`, by registration and calc. `sync_event`, append only by triggers.
4. `audit`: `claimius.insert_audit` if a row must be written.
5. `user_users_diff` and `usage_diff`, raised by the statement triggers and the usage flush and folded by the reads. There is no `reconcile_queue` and no `access_diff_user`.

## A disciple service in Go

1. It calls no claimius write function from its own SQL or Go.
2. It checks with `sdb.Instance().MutationAccess`, filters listings with `sdb.ObjectWhere`, and writes `claim`, `claim_object` and `user_claim` through the builder, which routes them to the prophet. Which call when: `references/access.md`.
3. The handler shape, the way `bookable_server` does it: the workspace rule `.claude/rules/claimius.md`.
4. On a disciple only the sync layer writes claimius tables.

## Binding and cascade

1. Every resource in the access model has at least one direct `claim_object` row. After inserting an organization, location, app or registered row, call `insert_claim_object` before any read looks for it. The claim is normally the one behind the actor's `user_claim_id`; pass another claim id for a different binding.
2. Exempt: `samna_user` rows, handled by `user_users` and `upsert_app_user`, and claims themselves.
3. A cascade fires only when both `claim.inherits` and that binding's `claim_object.inherits` are TRUE. A claim with `inherits = FALSE` blocks all its cascades.

## App scope

1. Always filter on `app_id`. `samna_user`, `user_claim`, `user_object`, `user_users`, `user_relation` and `user_field` carry it on a prophet; one human can hold separate access in each app.
2. The prophet refuses with `P0403` a `claim`, `user_claim` or `claim_object` whose owning organization, or bound organization or location, is not linked to the row's app through `app_object` (`_check_app_scope` over `app_has_scope`). Link it with `upsert_app_object` first.

## Denies, paths and grants

1. A deny is a claim whose `sa_access` carries `0x10` plus the bits to subtract, stored as given, bound with `insert_claim_object` and granted with `insert_user_claim`.
2. Bound on the object it subtracts from every grant there; reached through a tree it subtracts only from grants that also arrive through a tree, so a grant bound or composed on the object survives an inherited deny. A field scoped deny subtracts from those fields only. The remainder is closed under implication: denying read drops execute, write and owner.
3. Levels come first: when a grant and a deny meet at different levels the higher level wins, whatever the class; at the same level the classes decide; level 0 everywhere is the old rule. Binding a claim whose level is not 0 needs an actor holding that level on the object; granting it or increasing its level needs write on the claim and must not lift a deny the actor faces. Only `claim.level` compares; the `level` of an organization or location is tree depth. `level` is never an access mask.
4. The settle stores the result; reads never filter denies.
5. A path is `tree_type, cascaded_from`: `direct` on the object, `ownership`, `location` or `parenthood` through an ancestor. A composed grant also carries `composed_from` and takes the class of the binding it came through.
6. `user_object.grants` carries one entry per contributing claim; `sa_access` is the OR of the surviving grants after denies. Each grant carries `claim & expand(coalesce(binding, claim))`, a composed one the bits both link and target binding carry. The row stays while some grant or `direct_grant` survives.

## Asset links

1. Store the asset and its bytes in the own service, then write a `claimius.object_asset` link: `object_type` a `claimius.*` type, `object_id` the object (a user's `user_id`), `asset_id`, `kind` (`profile`, `banner`, `logo`, `file`) and `mime_type`. Only the link syncs, to every app that sees the object. It takes no `claim_object`; access to a link is READ on its object.
2. On a disciple write it through the builder, `NewInsert().Model(&shared.ObjectAsset{...}).Access(ac)` with `ac` WRITE on the object, self for the actor's own user. The prophet checks WRITE again and stamps owner and grant; a link on the actor's own user takes its default claim grant in the home app and is refused without one.
3. On the prophet insert, update or soft delete it directly with `app_id` the home app.
4. Read with `get_object_assets`, in Go `GetObjectAssets(ctx, user, type, id, kind)` and `GetObjectAsset(ctx, user, linkID)`; never SELECT the table to decide what a user sees.
5. Serve the file from the own route with `disciple/echo.GetObjectAssetData(ctx, linkID)`, which fetches through the prophet; hand the middleware the storage as `sdb.Options.AssetSource` at `sdb.Init`, optionally an `AssetCache`. `Content-Type`, `Content-Length`, `ETag`, `Last-Modified` and `Cache-Control` pass through untouched. Long form: `samna_auth_middleware/.nimpress/claimius/assets.md`.

## A registered application table

1. Columns, with `sa_parent_id` and `sa_root_id` both or neither:

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
    sa_parent_id    UUID NOT NULL,
    sa_root_id      UUID NOT NULL,
    name            TEXT,
    description     TEXT
);

SELECT claimius.upsert_table_info('public.my_table');
```

2. `upsert_table_info` validates the required columns and refuses registration when one, such as `sa_created_by`, is missing.
3. The root row has `sa_parent_id = id` and `sa_root_id = id`. Children set the parent and leave `sa_root_id` to the trigger.
4. After adding a column, rerun `upsert_table_info` with the same table list.
5. Soft delete with `sa_deleted_at = now()`. Hard deletes happen only inside `merge_user` and the calc functions.
6. The user reference is `samna_user.user_id`, shared by the per app rows of one human; `samna_user.id` is the row key and internal.

## Creation order

1. The tables reference each other in a circle: app and org, claim to org, user_claim to claim, claim_object to claim and object. Out of order, FKs fail or calc triggers misfire.
2. Fresh prophet: `upsert_system_app`; never replicate its body.
3. A new app on an existing prophet: the owning org exists and the actor holds owner on it in its own app; `upsert_app` with the actor's `user_claim.id` creates the app, its `prophet_state`, the actor's `samna_user`, the organization links, the owner claim bound and granted, and the default claim bound and set as `claim_id`; then `upsert_table_info` for any external tables.
4. A new org, location or registered row inside an app: direct INSERT with every `sa_*` column, then `insert_claim`, `insert_claim_object`, `insert_user_claim` to grant access.
5. When the order is not obvious, lay out the steps before writing SQL and ask.

## Ask when unsure

1. A table with `sa_parent_id` but no `sa_root_id`: a missing column, or no self reference wanted?
2. A query on materialized state without `app_id`: should it filter by app?
3. About to call `merge_user`, which hard deletes the source user's rows: confirm.
4. Prophet side or disciple side? On a disciple only the sync layer writes claimius tables.
5. A column added to a registered table: rerun `upsert_table_info` after the migration?

## Hard rules

1. Never set `replay_mode` outside the sync layer: the disciple pull and a prophet's pull of a peer.
2. Never write a row of an app another prophet is home for; it is refused with `P0409`, or `409` with `SAuth-Home` over HTTP. Send it to the home.
3. Never call `get_disciple_app` on a hybrid; use `get_app_id`.
4. Never call `_grant_prophet_roles` or `_grant_disciple_roles`; the role grants live in `V6.0__claimius_grants.sql`, the last base file, and run on every build and replay.
5. Tree roots are `parent == root == id`, never NULL.
6. Reject a registered table missing a required column and tell the user.

## References

1. `references/functions.md`: every public function of both role sets with its arguments and what it settles, the tables written directly and never, the access mask, app scoping, asset links, types and trees, and the traps.
2. `references/access.md`: the Go calls on `shared.DB`, `MutationAccess`, `MutationAccessMany`, `GetAccess`, `GetDirectAccess`, `UserAccess`, `RootAccess`, `ScopeAccess`, which one a handler uses, where each runs, and actor stamping.
3. `references/patterns.md`: the SQL for a user logging in, granting a permission and gating a query on an own table.
4. A disciple service in Go and the v1.x to v2 function names for a repo still on v1.x: the workspace rule `.claude/rules/claimius.md`.
5. The SQL in `samna_auth_middleware/prophet/database` and `samna_auth_middleware/disciple/database` is the truth; the long form design is `samna_auth_middleware/.nimpress/claimius/`.
