# samna_auth

Claim based access control schema shared across all Samna services. Uses a precomputed reverse index (Zanzibar pattern) for fast reads. All access is computed at write time by triggers and stored in `user_object`. Every read is an indexed lookup, no graph traversal.

This folder is copied identically to every service that uses samna_auth (samna_auth_middleware, bookable_server, etc). Never add service specific files here. Never create new SQL files. All changes go inline in the existing three files.

## How Access Works

Users hold claims. Claims bind to objects via claim_objects. When any table with `sa_owner_id` receives an INSERT, UPDATE, or DELETE, `tg_object_mutation` fires and recomputes the `user_object` reverse index for all affected users.

Access flows: User -> UserClaim -> Claim -> ClaimObject -> Object

The inheritance tree is walked by `compute_object_claims` (the only place traversal happens):

1. Direct claim_object bindings on the exact object
2. Organization chain: claims on organizations expand upward through `sa_owner_id` parents when `inherits = TRUE`
3. Location chain: claims on locations expand upward through `sa_parent_id` parents when `inherits = TRUE`
4. Resource hierarchy: claims on parent resources expand through `sa_parent_id` when `inherits = TRUE`

## Prophet and Disciple

The prophet (samna_auth server) is the source of truth. Disciples (bookable_server, etc) receive synced copies of the samna_auth tables.

Sync has two paths:
- **Snapshot** (pull): disciple calls `GET /sam/sync/snapshot` at startup, receives all rows, applies them locally
- **Incremental** (push): triggers write to `user_object_event`, the Go sync sender reads events and POSTs them to each disciple's `sync_uri`

The disciple sets `samna_auth.replay_mode = 'true'` before applying sync payloads. All triggers check this and skip when replay_mode is active, preventing loops.

All creates and updates go through the `/sam` endpoints on the prophet. The disciple never writes to its local samna_auth tables directly. Changes flow back through sync.

The `samna_app` table exists in the schema but is only populated on the disciple side. The prophet converts `app` events to `samna_app` in the Go sync sender before pushing to disciples. This gives disciples the app's `private_seed` for secret decryption without exposing the full app table.

## Secrets

Secrets are encrypted with AES-256-GCM using the app's `private_seed` as the key. The `value` column stores hex encoded ciphertext. The prophet encrypts on write and stores ciphertext. The disciple decrypts locally using the seed from its `samna_app` row via `ResolveSecret`.

## Clients

`samna_client` is a service account. Claims are assigned to clients via `user_claim` using the client's ID as `user_id`. The triggers compute `user_object` rows for clients the same way as for users.

## Tables

**Access control source tables:** `samna_user`, `organization`, `location`, `claim`, `user_claim`, `claim_object`

**Application data tables:** `samna_client`, `samna_secret`, `object_field`, `user_field`, `user_relation`, `audit`

**Reverse index tables:**
- `user_object` — one row per (user, object, claim) triple. Composite string PK via `composite_id`
- `user_users` — user visibility. Populated when users share objects
- `object_users` — reverse of user_object, for trigger delta computation

**Sync tables:**
- `user_object_event` — event log. Triggers write here, sync sender reads
- `prophet_state` — per app seq tracking on the prophet side
- `disciple_state` — seq tracking on the disciple side

**Disciple only tables:**
- `samna_app` — slim projection of the app table, synced from prophet. Contains `private_seed` for secret decryption

**Search tables:**
- `search_index` — full text search vectors for samna_user, claim, organization, location

## Triggers

**`tg_object_mutation`** is the core trigger. Attached to every application table (public schema via `init_auth_tables`, samna_auth schema via direct attachment in V1.2). On INSERT/DELETE it always fires. On UPDATE it only fires when `sa_owner_id` or `sa_deleted_at` changes. It finds affected users, calls `reconcile_user_object` for each, and writes to `user_object_event`.

**`tg_claim_mutation`** handles changes to claim, user_claim, and claim_object. Recomputes access when claims change and writes to `user_object_event`.

**`tg_hierarchy_mutation`** handles organization/location hierarchy changes. When `sa_owner_id` (org) or `sa_parent_id` (location) changes, recomputes access for all descendants.

**`update_current_timestamp`** sets `sa_updated_at` on every UPDATE. Attached to all tables.

**`audit_trigger_function`** writes audit rows for INSERT/UPDATE on organization, location, claim, claim_object, user_claim.

**`tg_search_index`** maintains `search_index` for samna_user, claim, organization, location.

## Read Functions (PUBLIC)

All read from the `user_object` reverse index. No graph traversal at read time. All call `reconcile_if_pending` first to heal stale reads from the reconcile queue.

- `get_access(user_id, object_id, level)` — most privileged claim for one object
- `get_claims(user_id, object_id)` — all claims against one object
- `get_claims(user_id, level)` — all claims across all objects
- `get_objects(user_id, type, level)` — object IDs of a type the user can see
- `get_organizations(user_id, level)` — accessible organizations
- `get_locations(user_id, level)` — accessible locations
- `get_users(user_id, level)` — visible users (self + direct claims + shared history)
- `get_audit(user_id, level)` — audit rows for accessible organizations
- `get_secrets(user_id, level)` — accessible secrets

## Write Functions (PUBLIC)

- `create_claim`, `update_claim`, `remove_claim`
- `assign_claim_user`, `update_claim_user`, `remove_user_claim`
- `assign_claim_object`, `update_claim_object`, `remove_claim_object`
- `merge_user` — merges source user into target, moves all references, soft deletes source

## Init Functions

- `init_auth_tables(table_names...)` — registers public schema tables with `tg_object_mutation` and populates `user_object` for existing rows. Called from the system seed, not from the samna_auth schema files. samna_auth schema tables have `tg_object_mutation` attached directly in V1.2.

## Error Recovery

When `compute_object_claims` fails inside a trigger, the write succeeds and the failed computation is queued in `reconcile_queue`. Read functions check the queue for the queried user and reconcile inline before returning.

## Access Levels

0 = system, 1 = owner, 2 = admin, 3 = write, 4 = update, 5 = action, 6 = read, 7 = member, 8 = guest

## File Structure

- `V1.0__baseline.sql` — all table definitions, indexes, `composite_id`/`decompose_id`
- `V1.1__functions.sql` — all functions (CRUD, hierarchy walkers, compute helpers, read functions, init, graph, search)
- `V1.2__triggers.sql` — all trigger functions and trigger attachments

Never add new files. Never create migration scripts. All changes go inline in these three files. The database is rebuilt from these files. Production gets upgraded by running the diff manually.

## Rules

- Every table with `sa_owner_id` must have `tg_object_mutation` attached so the reverse index is computed and sync events are generated
- Never create claims from application code to grant access. Set `sa_owner_id` correctly and the triggers handle it
- All multi row reads must go through the read functions or through the `ObjectWhere`/`UserWhere`/`ClaimWhere` helpers in Go, never manual `sa_owner_id` filters
- `sa_created_by` is always a `user_claim.id`, never a raw user_id (except bootstrap paths)
- Never set the `id` field on inserts, the database generates it
- Soft deletes only (`sa_deleted_at = NOW()`), never hard delete
