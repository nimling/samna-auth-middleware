# Claimius

A distributed claim based access control system for PostgreSQL.

Claimius is a Postgres schema (`claimius`) that manages user access to objects in your application via named permission templates called **claims**. Claims are bound to users (`user_claim`) and to objects (`claim_object`), with cascading inheritance through three orthogonal hierarchies: organization ownership, physical location, and per object self referencing parent chains.

The terminology of **prophet** and **disciple** describes the deployment shape. A prophet is the source of truth for an app's access state; disciples are read only mirrors that receive state via a sync stream and serve reads identically. The same schema runs on both. The role is enforced by Postgres role grants and the set of apps registered in `samna_app`.

## Why Claimius

Most access control libraries either live in app code (you have to remember to filter every query) or are limited to flat role based models. Claimius pushes access calculation into the database. Triggers keep the closure of the three trees, settle the writer's own access, and record what else a write changed as one area row; each reader settles its own share of an area when it reads. A check is then one lookup of a pair in the reverse index `user_object`, and a listing one range of it, however complex the access graph behind them.

Once your app tables register with Claimius, every read can ask one question: "what can this user see?" and get an answer in a single index scan.

## Core concepts

**Claim**: a permission template with an `sa_access` INTEGER bitwise mask and an `inherits` flag for cascading. The bits are owner `1`, write `2`, read `4`, execute `8`, deny `16` and member `32`; `ACCESS_ALL` is `15`. An access check is `(stored & required) = required`. `insert_claim` expands a grant mask: owner implies write, read and execute; write implies read and execute; execute implies read. The member bit stands alone. A claim with the deny bit set is a deny claim, covered in "Deny claims" below. The same mask sits on `claim_object.sa_access`, where NULL inherits the claim mask, and on `user_object.sa_access`, the effective mask per user and object.

**user_claim**: a grant of a claim to a user, optionally time bounded.

**claim_object**: a binding of a claim to a target row in any registered table. Polymorphic via `(object_type, object_id)`.

**Three hierarchies**:

- **Ownership**: organizations form trees via `sa_owner_id`. Every registered row points into an organization via its own `sa_owner_id`. Claims with `inherits = true` cascade down ownership trees.
- **Location**: locations form trees via `sa_parent_id`, owned by an organization. Registered rows can pin into a location via `sa_location_id`. Claims cascade through location trees.
- **Parenthood**: any registered table can declare its own self referencing tree by including `sa_parent_id` and `sa_root_id`. Claims cascade through that tree too.

**Roots**: a row is a tree root when `sa_owner_id == sa_root_id == id` (organization), or `sa_parent_id == sa_root_id == id` (location, parenthood). Root rows always self reference. There are no nullable parent columns.

**Auto root trigger**: a `BEFORE INSERT` trigger (`tg_self_root`) handles two cases automatically:
- **Root row** (parent column NULL or set to `NEW.id`): self references both columns to `NEW.id`.
- **Child row** (parent column set, `sa_root_id` NULL): copies parent's `sa_root_id`.

Implementers don't need to know about `sa_root_id`. Just set the parent (`sa_owner_id` for orgs, `sa_parent_id` for locations and parenthood tables) or leave both NULL for a root, and the trigger fills in the rest. Attached to `claimius.organization` and `claimius.location` by claimius itself, and attached to implementer parenthood tables when registered via `upsert_table_info`.

**Composite ids**: identifiers that span polymorphic tables use the format `<object_type>:<object_id>` where `object_type` is the schema qualified table name (`public.device`, `claimius.organization`).

**Trees**: `inheritance_info` is a closure table: one row per ancestor and descendant pair across the three tree types (ownership, location, parenthood). Self edges at depth 0 are included so a node is its own ancestor. Calc functions maintain the rows incrementally on writes and under disciple replay: an insert writes the node's own chain, a reparent splices its subtree with `_splice_subtree`, a soft delete drops its subtree, and a restore rebuilds only the restored subtree with `build_ownership_tree`, `build_location_tree` or `build_parenthood_tree`. Reads use indexed JOINs against the closure or the `get_subtree` / `get_*_ancestors` helpers.

**user_object**: the materialized reverse access index. One row per `(app, user, object)` with access, keyed on that tuple, the primary key carrying `sa_access` and the index `idx_user_object_object_user` reading the same pairs from the object side. Carries the effective mask, the contributing grants array, the field scope and the usage count of the pair. A pair is settled when its user reads it, not when the change is written; see `access_diff` below. The rendering fields and the search tsvector of each object live once in **object_info**, written when the object is written. This is the table consumers read from indirectly through `get_*` functions.

**access_diff**: the open areas of changes not yet settled, one row per changed object, claim or assignment, with `raise_count` counting the writes that raised it. `access_diff_user` records which holder settled which raise; an area clears once every holder has settled it. `drain_access_diff` settles, off the request path, the shares of holders who read more often than the area is raised.

**sync_event**: append only outbox of changes to the source tables, one event per source row written, keyed for a consumer by `(txid, seq)`. Derived tables, `user_object`, `user_users` and `audit`, never publish. Prophets write; disciples and peer prophets pull.

**Owner claim rule**: every resource that participates in the access model must carry at least one direct `claim_object` binding to a claim. The "owner claim" is the claim used to create the resource, captured from the actor's `user_claim_id` at write time, or specified explicitly when the writer wants a different binding. The direct binding exists so access is never reliant solely on cascade: even if the cascade machinery is bypassed, disabled per binding, or interrupted by an `inherits = FALSE` flag somewhere up the chain, the resource still has at least one user who can see and modify it. Two exceptions: `samna_user` rows (handled by `user_users` denormalization and `upsert_app_user`), and the claim itself (a claim does not bind itself via `claim_object`; that would be circular). Implementer write paths that insert organizations, locations, apps, or registered application table rows must follow up with an `insert_claim_object` (or direct `claim_object` insert during bootstrap) so the owner claim binding exists before any read tries to find the row.

**Inheritance**: cascades fire only when BOTH `claim.inherits` and the specific `claim_object.inherits` are TRUE. The claim's flag is the master switch for the whole claim; the per binding flag narrows that to specific bindings. Either FALSE blocks cascade for that binding. A claim with `inherits = FALSE` means none of its bindings cascade regardless of their per row flag.

## Roles

Four Postgres roles enforce the access boundary:

- **claimius_admin**: schema owner. Used during migrations.
- **claimius_writer**: prophet runtime. EXECUTE on every function, SELECT on every table, INSERT and UPDATE on the tables without write functions (`samna_app`, `samna_user`, `samna_client`, `samna_secret`, `organization`, `location`, `object_field`, `user_field`, `user_relation`, `notification`, `prophet_state`, `sync_consumer`, `peer_grant`), INSERT, UPDATE and DELETE on `claim_graph_cache`, UPDATE on `prophet_peer`.
- **claimius_reader**: app code runtime. No table privilege. EXECUTE on every function except the claim, grant and binding writers, `merge_user`, `migrate_root`, the bootstrap functions, `upsert_app`, `insert_app_handover`, `update_app_handover`, `apply_peer_event`, `insert_secret`, `upsert_peer_grant`, `upsert_guest_user`, `upsert_table_info`, `upsert_internal_table_info` and `recompute_state`. The read surface runs as its owner with a pinned `search_path`, so the reader gets answers through it and nothing else.
- **claimius_disciple_client**: disciple runtime and sync layer. EXECUTE on every function, SELECT on every table, and INSERT, UPDATE and DELETE on the mirrored source tables, the derived tables, `disciple_state`, `audit`, `inheritance_info`, `table_info`, `notification` and `claim_graph_cache`.

App connections never own the schema. They inherit the appropriate role.

The grants are written by `_grant_prophet_roles` and `_grant_disciple_roles`, defined in `V6.0__claimius_grants.sql`, the last file of the base set. That file runs the one that fits at the top level, the disciple helper on a database initialized through `upsert_disciple_state` and the prophet helper otherwise, so every build and every replay of the set leaves the roles holding their grants on every table and function the set carries, and no consumer calls a `_grant_*` function. It also brings every `audit` partition to exactly the grants of `audit` through `_match_audit_partition_grants`, which `upsert_audit_partition` runs on each month it attaches. A change to what a role holds is made in that file.

## Deployment shapes

**Prophet only**: production single instance, the home of its apps. Full read and write. Calc triggers fire on every change, keep the closure, settle the writer, raise areas and emit one `sync_event` row per source row.

**Disciple only**: a mirror of one app. Empty database; the sync layer applies the source rows of the prophet's feed under `replay_mode = true`, where the calc triggers keep the closure and raise the areas each row moves but settle no actor and emit no event. The disciple settles its own `user_object` from them on its reads, so its reads serve the same results as the prophet's.

**Hybrid**: a prophet that is home for its own system app and mirrors one app of a peer prophet, the way prophets federate. `upsert_prophet_peer` runs `upsert_system_app` and registers the peer in `prophet_peer`; the peer pull applies the peer's feed of that app through `apply_peer_event`. The mirrored app's writes belong to the peer, and this prophet refuses them.

The role is determined by which bootstrap function is called: `upsert_system_app`, `upsert_disciple_state`, or `upsert_prophet_peer`. There is no separate config table for role. A prophet and a hybrid carry the system app slug in `prophet_state`; a disciple carries its app slug in `disciple_state`, and a hybrid has no `disciple_state` row.

## Migration layout

The prophet base set, read by smig as a git include of `prophet/database` at a pinned ref and applied in version order:

```
V1.0__claimius_roles.sql              claimius_admin, _writer, _reader, _disciple_client
V1.1__claimius_baseline.sql           All tables, indexes, comments, enums
V1.2__claimius_notification.sql       Notification tables
V2.1__claimius_functions_base.sql     Utilities: composite_id, masks, insert_audit, get_subtree
V2.2__claimius_functions_internal.sql Tree builders, calc helpers, recompute_user_object
V2.3__claimius_functions_access.sql   Trigger functions: calc_*_access, emit_sync_event, app home,
                                      reconcile_if_pending, drain_access_diff
V2.4__claimius_functions_external.sql Public surface: get_*, search, claim management,
                                      merge_user, upsert_system_app, upsert_app_user, sync cursor,
                                      trimming, handover, apply_peer_event
V3.0__claimius_triggers.sql           Attaches trigger functions to internal tables
V4.0__claimius_init.sql               Calls upsert_internal_table_info()
V5.x__claimius_*.sql                  Prophet services: session, dpop, rate limit, par,
                                      jwks, sync feed, app client and bff, analytics
V6.0__claimius_grants.sql             Role grants and audit partition grants, run on every
                                      build and replay
```

After Claimius migrations finish, the implementing app runs:

```sql
-- For a fresh prophet instance.
-- private_key, private_seed are generated externally in Go and passed in
-- (Claimius does not generate keys). app_secret is intentionally NOT a
-- bootstrap input: it is the API key disciples authenticate with against
-- this prophet's sync endpoints, and it's filled in by the implementing
-- service on demand via samna_app.app_secret. See "App secret" below.
-- Returns a JSONB object keyed by table name, with the full row each table
-- received. Capture it if you need ids for follow on inserts.
SELECT claimius.upsert_system_app(
           p_system_app_slug          => 'my_app',
           p_system_app_name          => 'My Application',
           p_system_app_private_key   => '<rsa private key from Go>',
           p_system_app_private_seed  => '<32 random bytes hex from Go>',
           p_system_app_redirect_uri  => 'https://my.app/oauth/callback',
           p_system_app_sync_uri      => 'https://my.app/claimius/sync',
           p_default_claim            => jsonb_build_object(
               'name',            'Guest',
               'description',     'Default claim granted to every regular user on first login. Marks account existence; grants no resource access.',
               'sa_access',       4
                                         )
       );
-- Result shape (JSONB), seven keys:
--   {
--     "samna_app":     { full samna_app row, app_secret is NULL until set by implementer },
--     "samna_user":    { full samna_user row, the system user },
--     "organization":  { full organization row, the system org },
--     "claim":         { full claim row, the system administrator claim },
--     "default_claim": { full claim row, the implementer defined default claim },
--     "claim_object":  { full claim_object row, system claim bound to system org },
--     "user_claim":    { full user_claim row, system user granted system claim }
--   }
-- prophet_state and inheritance_info are internal bookkeeping, not returned.

-- Or a disciple. State arrives from upstream prophet via the sync layer.
-- p_disciple_app_slug identifies which prophet app this disciple mirrors;
-- typically read from an env var like CLAIMIUS_DISCIPLE_APP_SLUG so the
-- same migration artefact deploys against any prophet. Returns empty
-- jsonb {}.
SELECT claimius.upsert_disciple_state(
           p_disciple_app_slug => 'mainapp'
       );

-- Or a hybrid: a prophet home for its own system app that mirrors one app
-- of an upstream prophet as its peer. Runs upsert_system_app, then writes the prophet_peer
-- row the peer pull reads. Returns the same jsonb shape as upsert_system_app.
SELECT claimius.upsert_prophet_peer(
           p_system_app_slug          => 'my_app',
           p_system_app_name          => 'My Application',
           p_system_app_private_key   => '<rsa private key from Go>',
           p_system_app_private_seed  => '<32 random bytes hex from Go>',
           p_upstream_prophet         => 'upstream_prophet_slug',
           p_upstream_endpoint        => 'https://upstream.example/sam',
           p_upstream_app_slug        => 'mirrored_app',
           p_upstream_app_secret      => '<the mirrored app secret>',
           p_default_claim            => jsonb_build_object(
               'name',            'Guest',
               'description',     'Default claim for new users.',
               'sa_access',       4
                                         )
       );

-- Then register app tables:
SELECT claimius.upsert_table_info(
           'public.device',
           'public.sensor',
           'public.recipe'
       );
```

## Bootstrap and creation order on a prophet

A fresh prophet has nothing in the database. Bringing it up requires a precise order because tables reference each other circularly (app references org, org links back to the app through `app_object`, claims reference org, user_claim references claim, etc). `upsert_system_app` already encapsulates this for the system app and system org. When you create additional apps, organizations, and resources later, you follow the same pattern.

**Order for a fresh prophet (executed automatically by `upsert_system_app`):**

0. **The system user's `user_claim.id`** is settled first: the live grant of the system claim on a rerun, or a fresh id the grant row takes in step 8. Every row `upsert_system_app` writes carries it in `sa_created_by`, so the bootstrap keeps the rule that `sa_created_by` is a `user_claim.id`.
1. **System app** is inserted first, with a placeholder `sa_owner_id` referencing a UUID that will become the system org.
2. **System samna_user** is inserted, scoped to the system app.
3. **System organization** is inserted with `sa_owner_id = sa_root_id = id` (self rooted), and an `app_object` link ties it to the system app.
4. **App's `sa_owner_id`** is reconciled with an UPDATE to point at the real system org.
5. **`inheritance_info` self edge** for the system org is inserted (`ownership` tree, depth 0), bootstrapping the closure for that root.
6. **System claim** ('System Administrator', `sa_access = 15`, `ACCESS_ALL`) is created, owned by the system org. This claim is sidelined: granted only to the system user, never via `upsert_app_user`.
   6b. **Default claim** is created from `p_default_claim` (`name` and `description` required, `sa_access` mask optional and defaulting to `4`, read). Type is `'user'`, `inherits = false`, no `claim_object` binding. Holding this claim alone grants no resource access; it is a "you have an account" marker.
7. **App's `claim_id`** is updated to point at the **default claim** (NOT the system claim). This is the claim every regular user receives via `upsert_app_user`.
8. **user_claim** grants the system claim to the system user under the id of step 0, and the system user's pairs on the system org and the system app are recomputed, so the bindings below are judged against the access the system user holds as creator of both.
9. **claim_object bindings** bind the system claim to the system org with `inherits = TRUE` (cascades to everything in the system org tree) and to the system app directly.
10. **prophet_state row** is inserted for the system app, marked with `system_app_slug`, its cursor `(last_applied_txid, last_applied_seq)` at `(0, 0)`.
11. **Role grants** are applied so `claimius_writer` can do its job; `V6.0__claimius_grants.sql` applies them again on every replay of the set.

After this, the system user has full access to everything in the system org tree. Regular users created via `upsert_app_user` automatically receive the default claim, which by itself gives them no resource access. The implementing app grants additional claims as users earn permissions through normal flow.

**Order for a new (non system) app on an existing prophet:**

1. **Owning organization must exist first**. Either pick an existing org (typically the system org, or a tenant org) or create a fresh root org for the new app. The actor creating the app must hold owner on it in its own app.
2. **`claimius.upsert_app`** with the actor's `user_claim.id`. It writes the `samna_app` row and its `prophet_state` row, cursor `(0, 0)`, copies the actor's `samna_user` into the app, links the organization tree, creates the owner claim, binds it on the org and the app and grants it to the actor, and creates the default claim, binds it and sets it as `samna_app.claim_id`. See "A new app" below.
3. **Register external app tables** if any: `SELECT claimius.upsert_table_info(...)` with the table list.

**Order for adding resources to an existing app (the common case):**

1. **Organization** (if needed): direct INSERT with `sa_owner_id` pointing at parent org, or self referential for a new root.
2. **Location** (if needed): direct INSERT with `sa_owner_id` pointing at owning org and `sa_parent_id` pointing at parent location, or self referential for a root.
3. **Application table rows** (devices, recipes, etc): direct INSERT with all required `sa_*` columns.
4. **Grant access**: `insert_claim` (if needed) → `insert_claim_object` (bind the claim to the resource) → `insert_user_claim` (grant to a user). See "Granting a user access to something" in the "Creating things" section.

Calc triggers handle tree maintenance, the writer's own `user_object` pairs and the areas for everyone else automatically at every step. You do not need to call any "rebuild" or "refresh" functions in the normal flow.

## App secret

`samna_app.app_secret` is the API key disciples (or any other consumer) authenticate with against this prophet's sync endpoints. It is intentionally not a Claimius bootstrap input. The implementing service generates it (HMAC SHA256 of the raw secret keyed with the owning organization's `private_seed`, the raw returned to the caller exactly once) and writes the hash to `samna_app.app_secret` on demand. `secret_version` increments on each rotation. Set both fields directly via UPDATE; there is no Claimius helper for this since the secret format and validation are the implementer's concern.

For the system app on a fresh prophet, `app_secret` is NULL after `upsert_system_app` runs. The implementer fills it in if and when the system app needs to be authenticated against externally. For non system apps the implementer creates, the same applies: generate a secret, write it to `samna_app.app_secret`, return the raw to the caller once.

## Self identity

A disciple consumes from exactly one upstream prophet app. A prophet, and a hybrid, has exactly one system app. Both deployments need to answer "which app am I, structurally" without leaking the slug into application code. Two row returning functions cover this:

```sql
-- On prophet or hybrid: returns the system app row.
-- Raises on a pure disciple.
SELECT * FROM claimius.get_app();

-- On a disciple: returns the upstream prophet app row.
-- Raises on a prophet and on a hybrid, which carry no disciple_state row.
SELECT * FROM claimius.get_disciple_app();
```

Both return `SETOF claimius.samna_app` so they compose with `WHERE`/`JOIN` naturally. They are read only, fast (single indexed lookup), and granted to every role.

Three scalar variants exist for places that reject subqueries (column `DEFAULT` expressions, generated columns, check constraints):

```sql
claimius.get_prophet_app_id()  -- UUID, system app id; raises on pure disciple
claimius.get_disciple_app_id() -- UUID, upstream app id; raises on a prophet or hybrid
claimius.get_app_id()          -- UUID, deployment own app id (resolution below)
```

`get_app_id()` resolves to the system app on prophet and hybrid deployments, and to the disciple app on a disciple. A hybrid mirrors its peer's app as a prophet, through `prophet_peer`, not as a disciple, so `get_disciple_app()` raises there; the mirrored app is the `samna_app` row whose `home_prophet` names the peer.

Use these in column defaults instead of inlining a subquery:

```sql
-- Wrong: Postgres rejects subqueries in DEFAULT expressions
CREATE TABLE public.thing (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id UUID NOT NULL DEFAULT (SELECT id FROM claimius.get_app())
);

-- Right: function call in DEFAULT works
CREATE TABLE public.thing (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id UUID NOT NULL DEFAULT claimius.get_app_id()
);
```

The slugs are stored at bootstrap:

- `upsert_system_app(p_system_app_slug => 'mainapp', ...)` writes `'mainapp'` to `claimius.prophet_state.system_app_slug` on the system app's row. A partial unique index (`WHERE system_app_slug IS NOT NULL`) enforces "at most one system row."
- `upsert_disciple_state(p_disciple_app_slug => 'mainapp')` writes `'mainapp'` to `claimius.disciple_state.disciple_app_slug` (singleton, NOT NULL).

Implementer middleware should call `claimius.get_app_id()` for the deployment's own app, or `claimius.get_disciple_app()` on a disciple for the row, instead of maintaining a parallel `app_id()` helper. On a disciple `claimius.samna_app` only ever contains rows that arrive via sync; the slug keyed lookup gives you the canonical row even before any migration runs against it.

## Reading and writing

Every read goes through a `get_*` function. The functions first run `reconcile_if_pending`: a check settles the one pair it asks about when an open area covers it, a listing settles the reader's own share of the open areas it needs, those that can hold a pair of the type it lists, every one of them under one share lock per user, and both drain the reader's `reconcile_queue` rows. Then they check user status with the pure predicate `check_user_active`, filter by temporal validity, and return. Listings page in key order with `p_after` and `p_limit`.

```sql
-- Can this user read and write this object? 6 is read (4) with write (2);
-- a row comes back when (sa_access & 6) = 6.
SELECT * FROM claimius.get_access(
    p_user_id         => :user_id,
    p_app_id          => :app_id,
    p_object_id       => :object_id,
    p_object_type     => 'public.device',
    p_required_access => 6
);

-- Objects of this type the user can see, 50 at a time in object_id order.
-- The next page passes the last object_id of this one as p_after.
SELECT * FROM claimius.get_objects(
    p_user_id     => :user_id,
    p_app_id      => :app_id,
    p_object_type => 'public.device',
    p_after       => :last_object_id,
    p_limit       => 50
);

-- Full text search across accessible objects:
SELECT * FROM claimius.search(
    p_user_id => :user_id,
    p_app_id  => :app_id,
    p_query   => 'thermostat kitchen'
);
```

### `user_claim_id` and the actor token contract

`get_access`, `get_objects`, and `search` return a `user_claim_id` column alongside the access information. This is the **actor token** the caller uses as `sa_created_by` on follow on writes.

The contract: when `claim_id` is non null on the returned row, `user_claim_id` is also populated. `get_access` only returns rows when at least one surviving claim grants access, so a row from `get_access` always carries a usable `user_claim_id`. When the user has no surviving claim grant on the object (a possibility during seeding or in deployments that haven't yet bound every resource), call `get_direct_access` instead. It walks the object's owner, location, and parenthood ancestor chains and returns the closest, highest privilege user_claim the user holds anywhere on those chains, in the same row shape as `get_access`. If even that returns no rows, the user has no path to act on the object.

```sql
-- Read access AND grab the actor token for a follow on write.
SELECT user_claim_id INTO :acting_user_claim
FROM claimius.get_access(
    p_user_id         => :user_id,
    p_app_id          => :app_id,
    p_object_id       => :recipe_id,
    p_object_type     => 'public.recipe',
    p_required_access => 2
);

-- Now use it as the actor for the write.
UPDATE public.recipe SET name = :new_name, sa_created_by = :acting_user_claim
WHERE id = :recipe_id;
```

The list and graph functions (`get_organizations`, `get_locations`, `get_users`, `get_claims`, `get_audit`, `get_secrets`, `get_owner_graph`, `get_claim_graph`) do NOT include `user_claim_id`. If you need it after listing, call `get_access` for the specific object you're about to act on.

Writes go through functions when one exists for the operation (`insert_claim`, `insert_user_claim`, `insert_claim_object`, `merge_user`, etc). For tables without dedicated functions, direct INSERT/UPDATE on the prophet is fine; the prophet runtime role (`claimius_writer`) has the necessary grants. The triggers attached to each table fire either way.

Write functions return a single jsonb object keyed by the affected table name(s). The caller can pull values out with the `->` and `->>` jsonb operators, or pipe the whole object back to a service in Go without further translation.

```sql
-- Create a claim
SELECT claimius.insert_claim(
    p_app_id        => :app_id,
    p_name          => 'Device Operator',
    p_description   => 'Read and operate devices',
    p_sa_access     => 12,
    p_sa_owner_id   => :org_id,
    p_sa_root_id    => :root_org_id,
    p_sa_created_by => :acting_user_claim
);

-- Grant a claim to a user
SELECT claimius.insert_user_claim(
    p_app_id        => :app_id,
    p_claim_id      => :claim_id,
    p_user_id       => :user_id,
    p_sa_owner_id   => :org_id,
    p_sa_created_by => :acting_user_claim
);

-- Bind a claim to an object
SELECT claimius.insert_claim_object(
    p_app_id        => :app_id,
    p_claim_id      => :claim_id,
    p_object_id     => :device_id,
    p_object_type   => 'public.device',
    p_sa_owner_id   => :org_id,
    p_sa_root_id    => :root_org_id,
    p_sa_created_by => :acting_user_claim
);
```

To use the result inside a DO block for further work, capture and unpack with the jsonb operators:

```sql
DO $$
DECLARE
    v_init     JSONB;
    v_app_id   UUID;
    v_org_id   UUID;
    v_claim_id UUID;
BEGIN
    v_init := claimius.upsert_system_app(
        p_system_app_slug          => 'my_app',
        p_system_app_name          => 'My App',
        p_system_app_private_key   => :'private_key',
        p_system_app_private_seed  => :'private_seed',
        p_default_claim            => jsonb_build_object(
            'name',            'Guest',
            'description',     'Default claim for new users.',
            'sa_access',       4
        )
    );

    v_app_id   := (v_init -> 'samna_app'    ->> 'id')::UUID;
    v_org_id   := (v_init -> 'organization' ->> 'id')::UUID;
    v_claim_id := (v_init -> 'claim'        ->> 'id')::UUID;

    -- now use them in subsequent inserts
    INSERT INTO claimius.organization (
        name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by
    ) VALUES (
        'Acme NYC', 'office',
        v_org_id, v_org_id, 1,
        (v_init -> 'user_claim' ->> 'id')::UUID
    ) RETURNING id INTO v_org_id;

    PERFORM claimius.upsert_app_object(
        v_app_id, 'claimius.organization', v_org_id,
        (v_init -> 'user_claim' ->> 'id')::UUID
    );
END $$;
```

## Required columns on registered tables

Every external table registered through `upsert_table_info` must include:

- `id UUID PRIMARY KEY`
- `sa_owner_id UUID NOT NULL` (organization)
- `sa_created_by UUID NOT NULL` (user_claim id)
- `sa_created_at TIMESTAMPTZ`
- `sa_updated_at TIMESTAMPTZ`
- `sa_deleted_at TIMESTAMPTZ`

Optional:

- `app_id UUID NOT NULL` (when omitted, the deployment's own app id is inferred via `claimius.get_app_id()` at trigger time. Required on prophets that host more than one app, since inference would be ambiguous; rejected at registration in that case.)
- `sa_location_id UUID` (location the row is pinned to)
- `sa_parent_id UUID NOT NULL` and `sa_root_id UUID NOT NULL` (must come together; declares this table is self referencing)
- `name TEXT` and `description TEXT` (stored once per object in `object_info` for search and rendering)

`upsert_table_info` validates these and raises if any required column is missing or if the multi app guard fails on `app_id`.

## Creating things

For every create, `sa_created_by` must be a `user_claim.id` belonging to the actor in this app. Calc triggers handle tree maintenance, the actor's `user_object` pairs and the areas for everyone else automatically; the notes below say what you should do afterward.

**Function vs direct INSERT.** Some entities have dedicated write functions (`upsert_app`, `insert_claim`, `insert_user_claim`, `insert_claim_object`, `merge_user`, `upsert_app_user`). Use the function when it exists. For everything else, direct INSERT/UPDATE is allowed on the prophet side; the prophet runtime's role (`claimius_writer`) has the necessary grants. The triggers attached to each table fire either way, so the tree maintenance and access calculations are identical.

### A new app

Apps carry per app cryptographic material. `private_key` and `private_seed` (RSA keypair plus seed) are required at insert time and generated externally in your Go service. `app_secret` is the consumer facing API key for sync; it is generated and written separately, often after the app is created. Claimius does not generate any of these.

```sql
SELECT claimius.upsert_app(
    p_actor_user_claim_id => :acting_user_claim,
    p_slug                => :slug,
    p_name                => :name,
    p_org_id              => :owning_org_id,
    p_private_key         => :private_key,
    p_private_seed        => :private_seed,
    p_redirect_uris       => :redirect_uris,
    p_sync_uri            => :sync_uri,
    p_description         => :description,
    p_connection          => 'code',
    p_owner_access        => 15,
    p_default_claim       => '{"name": "Member", "description": "Signed in", "sa_access": 4}',
    p_app_id              => NULL
);
```

The full signature is `upsert_app(p_actor_user_claim_id UUID, p_slug TEXT, p_name TEXT, p_org_id UUID, p_private_key TEXT, p_private_seed TEXT, p_redirect_uris TEXT[] DEFAULT ARRAY[]::TEXT[], p_sync_uri TEXT DEFAULT '', p_description TEXT DEFAULT '', p_connection TEXT DEFAULT 'code', p_owner_access INTEGER DEFAULT 15, p_default_claim JSONB DEFAULT NULL, p_app_id UUID DEFAULT NULL) RETURNS JSONB`. Every row it writes carries `p_actor_user_claim_id` in `sa_created_by`. It refuses, SQLSTATE `22023`, an actor that is not a live `user_claim`, an organization that is not live, and a slug owned by another organization. It upserts the app on its slug and its `prophet_state` row, copies the actor's `samna_user` from the actor's app, links the organization and its tree with `upsert_app_organization`, creates `<name> Owner` with `p_owner_access`, binds it on the organization and the app and grants it to the actor's user, and creates the default claim, `<name> Reader` with read unless `p_default_claim` says otherwise, binds it on the app and the organization and sets it as `claim_id`. It is idempotent on the slug and on each claim's name, and returns jsonb keyed `samna_app`, `organization`, `samna_user`, `claim`, `default_claim`, `user_claim`, `claim_object`.

The owner claim's first binding is authorized by the actor holding owner on the organization that owns the app, judged in the actor's own app: `_check_claim_composition` accepts the first binding of an app that carries none on that ground, because the new app has no pairs anyone could hold. Every later binding is judged the usual way, against the pairs the grant to the actor gives it in the new app.

When the implementing service is ready to issue an API key for this app (typically returned to the caller of the create app endpoint), generate it externally and `UPDATE claimius.samna_app SET app_secret = :hash, secret_version = secret_version + 1 WHERE id = :new_app_id`.

For the system app (the very first app on a fresh prophet), use `claimius.upsert_system_app` instead. It handles the ordering between the system app and the system org and leaves `app_secret` NULL for the implementer to fill on demand.

### A new organization (root)

Use `upsert_root_organization`. The function handles the self reference on `sa_owner_id` and `sa_root_id` internally and is idempotent on `(app_id, name)`.

```sql
SELECT claimius.upsert_root_organization(
    p_app_id              => :app_id,
    p_name                => 'Acme Inc',
    p_actor_user_claim_id => :acting_user_claim,
    p_description         => NULL,
    p_type                => 'company'
);
```

Returns `{"organization": {...}}`. After: calc trigger inserts the org's edges into the `inheritance_info` ownership closure (self edge plus one edge per ancestor in the existing chain). Nobody has access to the new org yet. To give someone access, follow with the create claim and assign pattern below.

### A new organization (child of an existing org)

```sql
INSERT INTO claimius.organization (
    name, type,
    sa_owner_id, sa_root_id, sa_level, sa_created_by
) VALUES (
    'Acme NYC', 'office',
    :parent_org_id,
    :parent_org_root_id,    -- same as the parent's sa_root_id
    :parent_level + 1,
    :acting_user_claim
) RETURNING id;

SELECT claimius.upsert_app_object(:app_id, 'claimius.organization', :new_org_id, :acting_user_claim);
```

`organization` and `location` carry no `app_id` column. They belong to an app through a live `claimius.app_object` row, written by `upsert_app_object`.

After: calc trigger splices the new org into the existing ownership tree. Anyone with an inheriting claim on an ancestor reaches it: the actor's pair is settled in the write, and every other user's pair on that user's next check or listing.

### A new location

A location requires an owning organization. The BEFORE INSERT trigger handles the self references for roots and copies parent's `sa_root_id` for children, so the caller only needs to set the parent (or leave it NULL for a root).

```sql
-- Root location: parent and root left out, trigger self references both
INSERT INTO claimius.location (
    sa_owner_id, name, type,
    sa_level, sa_created_by
) VALUES (
    :org_id, 'Headquarters', 'building',
    0, :acting_user_claim
) RETURNING id;

-- Child location: only sa_parent_id needs to be set, trigger copies sa_root_id
INSERT INTO claimius.location (
    sa_owner_id, name, type,
    sa_parent_id, sa_level, sa_created_by
) VALUES (
    :org_id, 'Conference Room A', 'room',
    :parent_loc_id, :parent_level + 1, :acting_user_claim
) RETURNING id;

-- Link each new location to the app
SELECT claimius.upsert_app_object(:app_id, 'claimius.location', :new_loc_id, :acting_user_claim);
```

After: calc trigger updates the location tree and (if applicable) splices the location into the owning org's ownership tree.

### A row in your own registered table

Just a normal INSERT with the required `sa_*` columns. The calc trigger attached at registration time handles everything else.

```sql
INSERT INTO public.device (
    id, app_id, name, sa_owner_id, sa_created_by
) VALUES (
    gen_random_uuid(), :app_id, 'Thermostat 1', :org_id, :acting_user_claim
);
```

If your table has `sa_location_id`, set it to a valid location id and the row will also appear in that location's tree. If your table has `sa_parent_id`/`sa_root_id`, set both, and either point at a parent (child case) or point at self (root case).

After: nothing. The calc trigger updated `inheritance_info` and `object_info`, settled the actor's pair, and raised one object area; everyone else with reach settles on their next read.

### Granting a user access to something (3 steps)

This is the standard pattern whenever you create a new "thing" and want someone to access it. Steps 1 and 2 are skipped if you're reusing an existing claim. Each function returns a single key jsonb (`{"claim": {...}}`, `{"claim_object": {...}}`, `{"user_claim": {...}}`).

```sql
-- 1. Create the claim
SELECT claimius.insert_claim(
    p_app_id        => :app_id,
    p_name          => 'Admin: Acme NYC',
    p_description   => 'Full access to Acme NYC and descendants',
    p_sa_access     => 15,
    p_sa_owner_id   => :org_id,
    p_sa_root_id    => :root_org_id,
    p_sa_created_by => :acting_user_claim
);

-- 2. Bind it to the object
SELECT claimius.insert_claim_object(
    p_app_id        => :app_id,
    p_claim_id      => :claim_id,
    p_object_id     => :org_id,
    p_object_type   => 'claimius.organization',
    p_sa_owner_id   => :org_id,
    p_sa_root_id    => :root_org_id,
    p_sa_created_by => :acting_user_claim,
    p_inherits      => TRUE
);

-- 3. Grant the claim to the user
SELECT claimius.insert_user_claim(
    p_app_id        => :app_id,
    p_claim_id      => :claim_id,
    p_user_id       => :user_id,
    p_sa_owner_id   => :org_id,
    p_sa_created_by => :acting_user_claim
);
```

Each step settles the actor's own pairs on the rows it names and raises one claim area, the grant an area of the one assignee. By the end of step 3 the user reads the object through `get_*` functions: the first check or listing settles the user's share of the area. No write settles another user's pairs.

### A user logs in to an app for the first time

```sql
SELECT claimius.upsert_app_user(
    p_user_id    => :user_id,
    p_app_id     => :app_id,
    p_first_name => :first,
    p_last_name  => :last,
    p_user_name  => :user_name,
    p_email      => :email
);
```

Idempotent. Inserts the `samna_user` row if missing. If the app has `samna_app.claim_id` set (the default claim, configured during `upsert_system_app` via `p_default_claim`), also grants that claim to the user as a `user_claim` row. Returns:

```json
{
  "samna_user": { ... },
  "user_claim": { ... }   // only present when the app has a default claim
}
```

After: nothing else from the Claimius side. The user now has the default claim granted. The implementing app grants additional claims later as the user earns permissions (joining an org, being assigned a role, etc).

## Deny claims

A claim whose `sa_access` has the deny bit `16` set is a deny claim. `insert_claim` stores its bits verbatim, without the grant expansion, and the owner, write, read and execute bits enumerate what it subtracts: `20` (deny with read) removes read, `18` (deny with write) removes write, `31` removes all four. Bind it with `insert_claim_object` like any other claim.

A deny bound directly on the object subtracts its bits from every grant on that object, on any path.

A deny that arrives by cascade subtracts its bits from inherited grants in any tree and spares grants bound directly on the object.

A grant whose mask reaches `0` after subtraction is dropped. What remains of a grant is closed under implication: a bit whose implied bits were taken goes too, so denying read to an owner leaves nothing. A deny scoped to fields subtracts on those fields only. Denies are computed when a pair is settled (`_build_grants_for_objects`, `_apply_denies`); the resulting `user_object` row reflects only surviving grants, so reads never filter for denies.

`claim.level` is precedence, not access. A deny subtracts from every grant below its level whatever their classes; at the same level the direct and tree rules above decide. A link takes the higher level of its two claims. Binding or granting a claim above level 0 needs an actor that could hand it out: `_check_claim_level` requires write on the claim and refuses to lift a deny of a lower level the actor faces. Every grant and every `AccessClaim` carries the level of the claim it came through. Level 0 everywhere resolves as before levels existed.

## Sync model

Prophet:
1. Writes happen via functions or direct inserts, which run normal SQL.
2. `emit_sync_event` writes one `sync_event` row per source row written, in the transaction of the change, stamped with the app and the writing transaction's `txid`. An organization or location is written once per app linked to it. Derived tables never publish.
3. `get_sync_events(app, txid, seq, limit, peer)` serves a consumer the app's events after its `(txid, seq)` cursor, only those whose transaction is below the `xmin` of the reading snapshot, so a sequence that commits late is never passed over. `update_sync_cursor` records each consumer's cursor in `sync_consumer`, and `trim_sync_event` deletes what every consumer in contact applied and caps each app's outbox at its source rows. `check_sync_cursor` sends a consumer to the snapshot when its cursor is ahead, trimmed, or behind by more events than the app has source rows.

Disciple:
1. The pull loop, one process per database, fetches the next page after its cursor.
2. It applies the page in one transaction with `claimius.replay_mode = true`, moving `disciple_state (last_applied_txid, last_applied_seq)` first. Under replay the calc triggers keep the closure and raise the areas each row moves, settle no actor and emit no event, and each claim, binding and grant raises its claim area.
3. The disciple's reads settle its own pairs from those areas, as the prophet's do.
4. A page that fails anywhere rolls back whole, cursor included, and is pulled again. Nothing is filtered or parked: the prophet refuses a claim, grant or binding whose organization is not linked to its app, so every row of the feed applies. `reconcile_queue` is still drained at the top of `get_*` functions, but nothing in the base set writes it.

The same ids on prophet and disciple guarantee idempotent upserts. Apply an event twice, get the same row.

## Graphs

Two functions return flat node lists suitable for visualizing the deployment's access topology. Both return rows of shape `(object_id UUID, object_type TEXT, label TEXT, parent_id UUID, data JSONB)` so the frontend can pipe them straight into a graph layout (dagre, d3-force, whatever). Both go through `reconcile_if_pending` and `check_user_active` first.

Every row's `data` jsonb contains at minimum three keys:
- `access` (INTEGER or NULL): the bitwise access mask for this node. For objects it is the user's effective mask from `user_object.sa_access`; for a claim or a claim_object connector it is that row's own `sa_access`. NULL for the requesting user and for user_claim connectors.
- `scope` (JSONB or NULL): the scope payload carried alongside the mask.
- `level` (INTEGER or NULL): tree depth, never an access value. On organizations and locations it is `sa_level`; on objects reached from a start claim it is the depth below the bound object; NULL elsewhere.

The `p_required_access` argument on both functions is a mask filter: a node or claim passes when `(sa_access & p_required_access) = p_required_access`. The default `0` passes everything.

Both take a window. `p_width INTEGER DEFAULT 0` keeps at most that many children of each type under every parent, 0 meaning all; `p_sort TEXT DEFAULT 'name'` orders siblings by label then id, or with `'usage'` by the reader's decayed usage of the pair, `used_count × 2^(−age / 14 days)`, heaviest first; `p_next_id UUID DEFAULT NULL` continues after that node with its later siblings of its type. A node is kept when it is inside its parent's window and its parent is kept. The window bounds the rows returned; the graph is built whole first. The link graphs, `get_graph_links_owner(p_user_id, p_app_id, p_required_access, p_include_types, p_exclude_types, p_start_id, p_depth, p_width, p_sort, p_next_id)` and `get_graph_links_claim` with the same arguments, take the same window through `_window_links`: each link target is a node under its link source, and a link is kept when its target is kept under its source. The graph's own nodes that no link targets are top nodes too, and a windowed link graph returns `node_ids`, the ids of every kept node, beside `links`, so a kept node that keeps no child still reaches the object set.

Additional keys vary by node kind. All field names in `data` use plain naming without `sa_` prefixes (so `owner_id` not `sa_owner_id`, `location_id` not `sa_location_id`, `description` not `sa_description`, etc.).

### `get_owner_graph(p_user_id, p_app_id, p_start_id, p_depth, p_required_access, p_width, p_sort, p_next_id)`

Returns the ownership graph: organizations, locations, and every registered application table that has `sa_owner_id` and/or `sa_location_id`. Rows are scoped to what the calling user has access to via `get_objects`.

Arguments:
- `p_start_id UUID DEFAULT NULL`: organization id to start from. NULL walks every accessible root org. If the start id is not an accessible org, the function returns empty silently.
- `p_depth INTEGER DEFAULT 0`: how many levels of descent below the start. 0 means unlimited. The start org is depth 0; its direct children are depth 1; and so on.
- `p_required_access INTEGER DEFAULT 0`: access mask filter.

Per row `data` shape:
- Organizations (`object_type = 'claimius.organization'`): `{ access, scope, level, description, type, owner_id }`.
- Locations (`object_type = 'claimius.location'`): `{ access, scope, level, description, type, owner_id, longitude, latitude }`.
- Other registered tables: `{ access, scope, level (NULL), description, owner_id, location_id }`.

`label` comes from the object's `name`, read from its `object_info` row, otherwise the id as fallback.

`parent_id` is the canonical ancestor in priority order: `sa_parent_id`, then `sa_location_id`, then `sa_owner_id`. NULL for tree roots.

### `get_claim_graph(p_user_id, p_app_id, p_start_id, p_depth, p_required_access, p_width, p_sort, p_next_id)`

Returns the access graph: the requesting user, every other user this user can see (via `user_users`), every claim the user holds, the user_claim grants, claim_object connector nodes, and the actual objects reachable through those bindings. The default shape, no start, depth 0 and mask 0, is cached per user in `claim_graph_cache` with the snapshot it was built under, and served while every grant, claim and binding of the user was written by a transaction visible in that snapshot.

Arguments:
- `p_start_id UUID DEFAULT NULL`: claim id. NULL includes every claim the user holds. If the start id is not a claim the user holds, the function returns empty silently.
- `p_depth INTEGER DEFAULT 0`: 0 includes cascaded objects. Without a start id, >= 1 includes only directly bound objects. With a start id, >= 1 caps cascade descent at that many levels below each bound object.
- `p_required_access INTEGER DEFAULT 0`: access mask filter applied to the claims and, without a start id, to the objects.

Topology:
```
user (parent_id = NULL)
  '- user_claim    (parent_id = user.id)
  '- other users   (parent_id = user.id, via user_users)
claim (parent_id = NULL)
  '- claim_object  (parent_id = claim.id)
        '- object  (parent_id = claim_object.id)
```

Per row `data` shape:
- The requesting user (`object_type = 'claimius.samna_user'`, parent_id NULL): `{ access (NULL), scope (NULL), level (NULL), user_id, email, external_id, status }`.
- Other users (`object_type = 'claimius.samna_user'`, parent_id = requesting user id): `{ access, scope, level (NULL), user_id, email, external_id, status }`.
- user_claim (`object_type = 'claimius.user_claim'`): `{ access (NULL), scope (NULL), level (NULL), claim_id, starts_at, ends_at, reason }`.
- Claim (`object_type = 'claimius.claim'`): `{ access (the claim's own mask, deny bit included), scope (NULL), level (NULL), description, inherits, type }`.
- claim_object connector (`object_type = 'claimius.claim_object'`): `{ access (the binding's own mask, NULL when it inherits the claim mask), scope, level (NULL), target_object_id, target_object_type, inherits }`.
- Reachable object without a start id (`object_type = the object's schema qualified type`): `{ access, scope, level (NULL), description, owner_id, location_id, tree_type }`. `access` is the user's effective mask. `tree_type` is `'direct'` for directly bound objects or `'ownership'`/`'location'`/`'parenthood'` for cascaded ones.
- Reachable object with a start id: `{ access, deny, scope, level, tree_type }`. `access` is the binding mask with the deny bit removed, `deny` is TRUE when the binding carries the deny bit, and `level` is the depth below the bound object.

### Extending the graphs

The graph functions surface what claimius knows: the structural ownership chain and the access topology, with object data limited to the columns claimius keeps: `sa_name`, `sa_description`, `sa_owner_id` and `sa_location_id` from `object_info`, `sa_access` and `scope` from `user_object`. If you need richer per table fields from your own tables, write a wrapper in your own schema that UNIONs claimius's output with rows from your own tables, scoped through `get_objects`:

```sql
CREATE OR REPLACE FUNCTION public.get_owner_graph(p_user_id UUID, p_start_id UUID DEFAULT NULL, p_depth INTEGER DEFAULT 0, p_required_access INTEGER DEFAULT 0)
   RETURNS TABLE(object_id UUID, object_type TEXT, label TEXT, parent_id UUID, data JSONB) AS $$
DECLARE
   v_app_id UUID := claimius.get_app_id();
BEGIN
   RETURN QUERY SELECT * FROM claimius.get_owner_graph(p_user_id, v_app_id, p_start_id, p_depth, p_required_access);
   -- Add your own enrichment, override per row data jsonb where you want
   -- richer fields, etc. claimius's rows will still be there with the
   -- canonical schema-qualified types.
END;
$$ LANGUAGE plpgsql;
```