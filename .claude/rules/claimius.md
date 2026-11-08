# Claimius

This repo ships the `claimius` schema: `prophet/database` for samna_auth, `disciple/database` for every disciple. Function names here are the truth; where `../.claude/rules/claimius.md` disagrees, this file wins. Long form design: `.nimpress/claimius/`.

## Materialization is paid on read

A write settles only the acting user on the rows it names and raises the area in `access_diff`, stamped with its transaction and the object's owner root. Each other user settles their share on their next `get_*` read: a listing settles the reader's share of every area raised since its cursor, `user_activity.settled_txid`, in one pass, and a check settles the one pair when an area on its nodes is newer than the pair's mark, `user_object.settled_txid`. `drain_access_diff` settles frequent readers off the request path and trims the areas every active cursor has passed, recording the position in `access_trimmed_txid`. Never assume another user's `user_object` row exists after a write. `reconcile_if_pending` runs only at the top of the `get_*` family, so a direct SELECT never settles. `notify_object` settles the pair of every user who may hold the object before it fans out, so a reader who has not read since the last write is reached.

## Reads

1. `get_access(user, app, object_id, object_type, min)`: strongest grant, at most one row, carries `user_claim_id`, the actor token for `sa_created_by` on follow on writes.
2. `get_direct_access(user, app, object_id, object_type, required?, raise?)`: closest ancestor grant over owner, location and parenthood chains; same row shape.
3. `get_objects(user, app, type, min, after?, limit?)`; `get_organizations`, `get_locations`, `get_secrets` are one `get_objects` page. `get_users`, `get_claims`, `get_audit`.
4. `get_user_access(actor, app, target, min)`: target as external_id, user_id or row id, resolved in that order.
5. `get_scope_access(user, app, jsonpath, min)`: object less scope bindings.
6. `get_owner_graph` and `get_claim_graph(user, app, start_id?, depth?, min?, width?, sort?, next_id?)`: flat rows `object_id, object_type, label, parent_id, data{access, level}`. `width` bounds rows returned, not rows read. The default claim graph is cached in `claim_graph_cache`.
7. `search(user, app, query, types, min, limit, offset)`: ranked by text match plus the reader's decayed usage.
8. Listings and graphs return no `user_claim_id`; call `get_access` on the object before acting.
9. `shared.UserWhere(user, req)` filters `samna_user` listings: a user always reads its own row, and `type = 'system'` users stay hidden from every other user.
10. Lookups by id on a disciple, with no reader and no access filter, soft deleted rows included, for a row a read already admitted: `get_users(user_ids, ids, emails?, external_ids?)`, `get_organizations(ids, owner_ids, external_ids?)`, `get_locations(ids, parent_ids, external_ids?)`, `get_claims(ids, owner_ids)`, `get_grants(ids?, user_ids?, claim_ids?, owner_ids?, clients?)` over `user_claim`, `get_bindings(object_type?, object_ids?, claim_ids?)` over `claim_object` with `effective_access` on each row, `get_user_tokens(user_ids)`, `get_object_types()`. Every given list applies; no list gives no rows; an email compares without case. Pass both leading arrays, cast `?::uuid[]`, NULL for an unused one. Go: `GetUsersMany(ctx, userIDs, ids, emails, externalIDs)`, `GetOrganizationsMany(ctx, ids, ownerIDs, externalIDs)`, `GetLocationsMany(ctx, ids, parentIDs, externalIDs)`, `GetClaimsMany`, `GetGrants(GrantFilters)`, `GetBindings(BindingFilters)`, `GetUserTokens`, `GetObjectTypes`.
11. `get_object_usage(user, object_ids, object_types?)`: per object its `object_info` name and the reader's `used_count`, NULL without a pair; a NULL type takes the type of the reader's pair. Go: `GetObjectUsage`.
12. `get_relations(user, ids?)` on a disciple: the reader's live `user_relation` rows. Go: `GetUserRelationsQuery`, `GetRelation`, `GetRelationQuery`. `get_audit(user, required, limit, offset, ids?)` on a disciple: with `ids` only those rows. Go: `GetAudit(ctx, user, required, ids)`. `get_descendants(object_id)`: the typed walk for whichever registered type holds the id. Go: `GetDescendants`.
13. The views `get_users`, `get_organizations`, `get_locations`, `get_claims`, `get_grants` are every row of `samna_user`, `organization`, `location`, `claim`, `user_claim`, soft deleted included, created in `V6.0` with `SELECT` for `claimius_reader`. They are the read surface of bun selects and belongs-to joins: the `shared` models carry `select:claimius.get_<name>` beside `table:`, so bun reads the view and writes the table. A consumer model over a claimius table adds the same `select:`. Never name a `claimius` table in app SQL; a join names the view.

## Writes

Write functions return one jsonb keyed by table: `(v -> 'claim' ->> 'id')::uuid`.

1. `claim`, `user_claim`, `claim_object`: `insert_*`, `update_*`, `delete_*` only. `p_starts_at` and `p_ends_at` on `insert_user_claim` expire a grant at read time.
2. `samna_user`: `upsert_app_user`. Guests: `upsert_guest_user`. Secrets: `insert_secret`. Audit: `insert_audit`. External users: `IssueUserToken` and `UpsertUserExternal`, routed to the prophet, which writes the `user_token` row under the organization; the feed carries it to every app the organization is linked to, without `data`, and a forwarded identity resolves in any of those apps. Cross app: `merge_user`, prophet set only; samna_auth owns the merge flow and calls it from `internal/service/user.go` `MergeUser`.
3. Apps: `upsert_system_app` bootstraps a prophet; `upsert_app(p_actor_user_claim_id, ...)` creates every other app with its links, owner and default claims and grant. Never INSERT `samna_app`.
4. Disciple bootstrap: `upsert_disciple_state(p_disciple_app_slug)`. Hybrid: `upsert_prophet_peer`, a prophet mirroring one app of a peer, with no `disciple_state` row.
5. Registration: `upsert_table_info` registers the named tables, idempotent; `delete_table_info` deregisters the named tables. Rerun `upsert_table_info` after adding `sa_parent_id`, `sa_root_id` or `sa_location_id`, after the migration and backfill.
6. `app_object`: `upsert_app_object`, `delete_app_object`, never direct. Roots: `upsert_root_organization`, idempotent on app and name.
7. Direct INSERT or UPDATE: `organization`, `location`, `samna_client`, `object_field`, `user_field`, `user_relation`, `object_asset`, `prophet_state`, `disciple_state`, `samna_app` and `samna_secret` updates.
8. Never write: `user_object`, `object_info`, `user_users`, `user_users_diff`, `inheritance_info`, `table_info`, `sync_event`, `access_diff`, `user_activity`, `claim_graph_cache`, `peer_collision`, `used_count`, `last_used_at`. There is no `object_users`, `access_diff_user` or `reconcile_queue` table.
9. Bind every created resource with `insert_claim_object` before any read looks for it. Exempt: `samna_user` and claims themselves.
10. Order inside an app: org, location, app row, then `insert_claim`, `insert_claim_object`, `insert_user_claim`.
11. `_check_claim_composition` refuses a binding that hands out more than the actor holds. Two first bindings are judged on owner of an organization instead: an app's first binding (`upsert_app`) and a user's first binding under an org (`insert_organization_user`).
12. A binding on a row the prophet holds no `object_info` row for, outside `claimius.*`, is judged on the actor's hold on the binding's owning organization, and by the creator rule the disciple applies: a routed binding carries the row's `sa_created_by` as `object_created_by`, `PostClaimObject` sets it as `claimius.object_created_by` for its transaction, and when that is a live `user_claim` of the actor in the binding's app and the owning organization is live, the actor holds owner, write, read and execute on the row.

## Access mask

1. Owner 1, write 2, read 4, execute 8, deny 16, member 32; `ACCESS_ALL` is 15. Go constants: `shared.ClaimAccess` in `shared/models.go`.
2. On a grant, owner implies write, read, execute; write implies read and execute; execute implies read. Member stands alone.
3. A binding narrows its claim: `claim & expand(coalesce(binding, claim))`. NULL binding means the claim mask.
4. `claim.level` is precedence, not access. A deny subtracts from every grant below its level; level 0 everywhere behaves as before levels existed.
5. Deny: a claim with bit 16 and the bits to subtract. Bound on the object it subtracts from every grant; through a tree only from tree grants. The remainder is closed under implication. Reads never filter denies.
6. Cascade needs `inherits` on both the claim and the binding.

## Scoping

1. `app_id` column: `samna_user`, `claim`, `user_claim`, `claim_object`, `user_object`, `user_users`, `user_relation`, `user_field`, `object_field`, `samna_client`, `samna_secret`, `audit`, `access_diff`. Always filter on it.
2. `organization` and `location` have no `app_id`; scope through `app_object_ids`, `app_has_object`, `apps_for_object` on the prophet.
3. The prophet refuses with P0403 a `claim`, `user_claim` or `claim_object` whose owning organization, or whose bound organization or location, is not linked to the row's app: `_check_app_scope` over `app_has_scope`. Link the organization first; an update that keeps app, owner and object passes.
4. Disciples carry no `app_object`: a disciple mirrors one app, the sync layer strips `app_id`, and the disciple's read functions take no app argument. Write the app filter on the prophet side only.
5. A registered table may omit `app_id`; it is inferred from `get_app_id()`. On a multi app prophet the column is required.
6. `get_app()` returns the system app and raises on a pure disciple; `get_disciple_app()` raises on a prophet and a hybrid. On a hybrid use `get_app_id()`. These helpers ship in the prophet set only.

## Asset links

`object_asset` links an asset its home app stores to a `claimius.*` object; the asset row and its bytes stay in the home, only links sync. Long form: `.nimpress/claimius/assets.md`.

1. Not a registered access table: access to a link is READ on its object, for a `samna_user` self or what `get_user_access` resolves. Read with `get_object_assets`, in Go `GetObjectAssets` and `GetObjectAsset`.
2. A link reaches the apps `apps_for_object` names: an organization's or location's `app_object` apps, every app holding the user, the app itself, a claim's app, else the home. A late `app_object` link or `samna_user` row publishes the object's live links; never write `sync_event` for them.
3. Prophet: direct insert, update or soft delete with `app_id` the home. Disciple: the builder routes `shared.ObjectAsset` to `/sam/object_asset`, where the prophet checks WRITE on the object for the forwarded actor, a `samna_user` through `get_user_access`, any other object through `get_access`, and stamps owner and grant; on the actor's own user the grant is its default claim grant in the home app, refused without one.
4. A file is fetched through the prophet only: the consumer route calls `disciple/echo.GetObjectAssetData`, the prophet serves or relays to the home's `/asset/:id/data`, and the home answers from the host's `sdb.Options.AssetSource`. Every hop passes `Content-Type`, `Content-Length`, `ETag`, `Last-Modified` and `Cache-Control` through; the middleware caches nothing, a host `AssetCache` is used only while fresh.

## Types and trees

1. Object types are schema qualified everywhere; `upsert_table_info` raises on a bare name. `shared.ResolveObjectType` resolves bare claimius names and passes consumer types through; `ObjectTypeOf` returns the model `table:` tag as written; `ObjectType.Validate` is the consumer's opt in check.
2. Composite id: `claimius.composite_id(type, id)` gives `<schema>.<table>:<uuid>`.
3. Roots self reference. Children set the parent and leave `sa_root_id` NULL for the trigger.
4. Cross root moves: `migrate_root(object_type, object_id, new_root_id)` only, flagged for human review.

## Traps

1. Role grants live in `V6.0__claimius_grants.sql`; never call `_grant_prophet_roles` or `_grant_disciple_roles`.
2. A write to an app another prophet is home for raises `P0409` with the home in the hint; `/sam` answers 409 with `SAuth-Home`. Send the write there.
3. `replay_mode` is set only by the middleware: the sync pull, and the local copy of a routed write's answer, which applies exactly as its feed row will. Never by app code.
4. Sync lag: compare `disciple_state (last_applied_txid, last_applied_seq)` with `get_sync_seq(app_id)` and `sync_consumer` on the prophet.
5. Stale pair: `recompute_user_object(app, user, type, id)`, rarely needed.
6. Public surface prefixes: `get_`, `insert_`, `update_`, `delete_`, `upsert_`. `_` prefixed functions are internal.
