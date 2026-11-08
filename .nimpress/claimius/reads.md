---
gate: samna-developer
layout: doc
title: Reads
description: The claimius read surface and what each read function reads and writes in index entries and rows as a function of the data, why a check and a page of a listing are independent of tree depth and of the number of users, and which reads may run on a replica.
tags: get_access, get_direct_access, mutation_access, resolve_access, get_user_access, get_objects, get_organizations, get_locations, get_secrets, get_users, get_claims, get_audit, get_grants, get_bindings, get_user_identities, get_object_types, get_object_usage, get_relations, get_descendants, get views, select tag, _best_grant, reconcile_if_pending, _settle_user_users_diff, _user_users_for_viewer, key paging, security definer
lastUpdated: true
---

# Reads

Every read function takes the user first and, on the prophet, the app second. It settles what the read meets, a check the one pair by its mark and a listing the reader's share from its cursor, see [Materialization](./materialization.md), checks the reader with `check_user_active`, and then answers from `user_object` or from the base relations. Soft deleted rows, grants outside their window and denied bits never reach the caller.

On a disciple the same functions exist without the app argument, because a disciple mirrors one app and every row it holds is in scope: `get_access(user, object_id, object_type, required)`, `get_objects(user, object_type, required, after, limit)`, and so on.

## The surface

| Function | Answers |
|---|---|
| `get_access(user, app, object_id, object_type, required)` | The chosen grant on one object, at most one row, with its `user_claim_id` |
| `get_access(user, app, scope_key, required)` | The strongest object less binding whose scope carries the key; see [Capabilities](./capabilities.md) |
| `get_direct_access(user, app, object_id, object_type, required, raise, settle)` | The closest, strongest grant bound on the object or an ancestor, walking the trees |
| `resolve_access(user, app, object_id text, object_type, required)` | The verdict for any type: audit, relation and user lookups, otherwise `get_access` |
| `mutation_access(user, object_id text, object_type, required)` | The gate: a row, SQLSTATE `P0403` or `P0404` |
| `get_user_access(actor, app, target text, required)` | The actor's access on a user named by `external_id`, `user_id` or row id; see [Users](./users.md) |
| `get_objects(user, app, object_type, required, after, limit)` | One page of the objects of a type the user reaches, with rendering fields and `user_claim_id` |
| `get_organizations`, `get_locations`, `get_secrets` | `get_objects` pages joined to the table, with the same `after` and `limit` |
| `get_users(user, app, required)` | The users the reader sees |
| `get_claims(user, app, required)`, `get_claims(user, app, object_id, object_type)` | The claims the reader holds through a live grant; the claims behind one pair |
| `get_audit(user, app, required, limit, offset)` | Audit rows of the organizations the reader reaches; on a disciple a fifth argument `ids` keeps only the rows named; see [Audit](./audit.md) |
| `get_scope_access(user, app, path, required)` | Scope grants matching a jsonpath; see [Capabilities](./capabilities.md) |
| `search`, `get_owner_graph`, `get_claim_graph` | See [Search](./search.md) and [Graphs](./graphs.md) |
| `get_users(user_ids, ids, emails, external_ids)`, `get_organizations(ids, owner_ids, external_ids)`, `get_locations(ids, parent_ids, external_ids)`, `get_claims(ids, owner_ids)` | Disciple only: the rows named by id, owner, parent, email or external id, no reader; see [Lookups](#lookups) |
| `get_grants(ids, user_ids, claim_ids, owner_ids, clients)`, `get_bindings(object_type, object_ids, claim_ids)`, `get_user_identities(user_ids)`, `get_object_types()` | Disciple only: grants, bindings, tokens and registered types by id, no reader; see [Lookups](#lookups) |
| `get_object_usage(user, object_ids, object_types)` | Disciple only: the reader's `used_count` and the name of each object |
| `get_relations(user, ids)` | Disciple only: the reader's live `user_relation` rows; see [Lookups](#lookups) |
| `get_descendants(object_type, object_id)`, `get_descendants(object_id)` | The owner, location and parenthood subtrees of an object, with or without its type |
| The views `get_users`, `get_organizations`, `get_locations`, `get_claims`, `get_grants` | Every row of the table, the read surface of the bun models; see [The get views](#the-get-views) |

## get_access

`get_access` answers one object for one user:

1. `check_user_active`: an inactive or soft deleted user gets no row.

2. `_settle_pair` reads the pair with its mark and the object's `object_info` row, tests the areas raised in the object's owner root and the reader's assignments since the mark, and recomputes the one pair when one of them covers it. Otherwise it moves the pair's mark forward and reads the pair as it stands; see [Materialization](./materialization.md#a-check-settles-one-pair).

3. When the pair's mask covers `required`, `_best_grant` chooses the derivation the row carries.

4. When the settle was skipped on a lock and the pair gives nothing, it walks the ancestors through `get_direct_access` for that one answer.

The row carries `user_claim_id`, `claim_id`, `claim_name`, `sa_access`, the pair's field `scope`, the object's owner, owner root and location, and the `level` of the chosen grant. The answer does not depend on the depth of the trees, the bindings above the object or the number of users: the derivation was paid when the pair settled.

`_best_grant(grants, required)` keeps the derivations whose mask covers `required`, that carry a grant id, and whose stored window holds at `now()`, and returns the one with the most bits set, then the highest mask, then the highest level, then the oldest claim. It reads nothing: `user_claim_id`, `starts_at`, `ends_at`, `claim_name` and `claim_created_at` are stored on the derivation, and every write that can change them raises an area covering the pair. `user_claim_id` is the actor token a follow on write stamps into `sa_created_by`; see [Claims](./claims.md).

## get_direct_access

`get_direct_access` answers without `user_object`, for a caller that expects to act on an object whose pair is absent, and is the walk of the gate when the point settle was skipped on a lock:

1. With `settle`, the default, it settles the pair through `reconcile_if_pending`. `get_access` passes `FALSE`, having settled the pair itself.

2. `check_user_active`.

3. `get_ancestors`: the object's ownership chain, its location chain and, for each location on it, the owning organization's chain, and its parenthood chain.

4. The reader's live grants inside their window.

5. The bindings on each node joined to those grants, a binding above the object counting only when claim and binding both inherit, and the composed bindings, each with its claim and its link.

6. The denies among the same derivations: an unscoped deny on the object itself subtracts from every candidate, one above it from the candidates found above; what remains is closed under implication. A binding scoped only to fields the type lacks grants nothing.

7. The first row by distance, then bits, then grant id.

With `raise`, a miss probes the object's live row once and raises `P0403` when it exists and `P0404` when it does not. It is `VOLATILE`, because its settle writes.

## resolve_access and mutation_access

`resolve_access` routes by type:

| Type | Answered by |
|---|---|
| `claimius.audit` | The row, kept when its owning organization is among the reader's organizations |
| `claimius.user_relation` | The row, kept when it is live and its `user_id` is the reader |
| `claimius.samna_user` | `get_user_access` |
| Every other type | `get_access` |

A malformed id returns no row, so a batch of `resolve_access` over `unnest` never aborts on one. On the prophet a four argument form uses the deployment's own app, `get_app_id()`.

`mutation_access` is the gate for a write, in the deployment's own app:

1. A malformed id, or a type that is neither registered nor `claimius.audit` or `claimius.user_relation`, is refused with `P0404` before any read.

2. It calls `resolve_access` and returns its row on a hit.

3. On a miss it probes the object once: `P0403` when a live row exists, `P0404` when it does not. A user target is probed by `external_id`, `user_id` or row id.

One round trip answers 200, 403 or 404, and the pair is settled once.

## get_objects

`get_objects(user, app, type, required, after, limit)` settles the reader's share, then reads the reader's range of the `user_object` primary key for the type from the first key past `after`, until `limit` rows are kept. Per row it chooses the grant with `_best_grant` and joins the object's `object_info` row for name, description, link, owner, location and owner root.

1. A row is kept when its mask covers `required` and it carries a surviving grant or the reader created the object. A row kept only by creation carries a NULL `user_claim_id` and `direct_grant` true.

2. Rows come in `object_id` order straight from the index, so nothing is sorted, and the next page starts after the last `object_id` of this one. Without `limit` the page is the whole range.

3. The mask test is written `(sa_access & required) >= required`, the equality for a submask of `required`, which the planner estimates as a range rather than a point, so it keeps the ordered scan instead of sorting a bitmap scan.

A page does not depend on the depth of the trees, the number of bindings, the number of other users or the size of the reader's range.

## The other listings

| Listing | Returns |
|---|---|
| `get_organizations`, `get_locations` | The table rows of `get_objects` pages, each kept when live and, on the prophet, linked to the app through `app_object`. With `limit` it reads further pages until `limit` rows are kept or the range ends, so a page holds `limit` rows of the app. A disciple has no `app_object` and keeps every live row |
| `get_secrets` | One `get_objects` page joined to `samna_secret`, kept when live and of the app, in id order |
| `get_users` | The reader itself with mask 47, every user it shares an object with through the directory count with mask 36, and the users bound to it by a claim on `claimius.samna_user`; the masks of one user are ORed and the result filtered by `required`. Only live, active users of the app are returned. The first call makes the reader a viewer and folds its pending directory rows; see [Users](./users.md#who-can-see-whom) |
| `get_claims(user, app, required)` | Every live claim the reader holds through a live grant inside its window whose mask covers `required`, most bits first. The claims the reader can see or manage are `get_objects` of type `claimius.claim` |
| `get_claims(user, app, object_id, object_type)` | The live claims named by the surviving derivations of the reader's pair on the object, after the point settle |
| `get_audit` | The app's audit rows owned by an organization the reader reaches, newest first, after `offset`, at most `limit` |

A listing carries no authority to act on a row in it: `get_objects` returns a `user_claim_id` per row, but a write goes through `mutation_access` for its one row.

## Lookups

A row a read already admitted names other rows: a booking its creator through `sa_created_by`, a bookable its organization and location, a notification the holders of a claim. On a disciple the lookups return those rows by id, with no reader and no access filter, so that a consumer shows the creator's name or the location's name of a row it may read without selecting a claimius table.

1. `get_users(user_ids, ids, emails, external_ids)`, `get_organizations(ids, owner_ids, external_ids)`, `get_locations(ids, parent_ids, external_ids)` and `get_claims(ids, owner_ids)` return the table rows named by id, by owner or parent, and by external id; a user also by email, compared without case. A root owns itself, so it comes back for its own id among the owners or parents. An email or an external id resolves to its row this way, the soft deleted ones included, so a caller resolving a live row keeps `sa_deleted_at IS NULL`.

2. `get_grants(ids, user_ids, claim_ids, owner_ids, clients)` returns `user_claim` rows: by id for the creator behind a `sa_created_by`, by user for the rows a user created, by claim for the holders of a claim, by owner for the grants under an organization. With `clients` false the grants a `samna_client` holds are left out.

3. `get_bindings(object_type, object_ids, claim_ids)` returns `claim_object` rows on objects of a type, on ids of that type, or of claims. With `get_grants` on their claim ids they are the holders of the bindings on an object; `get_claims(ids, NULL)` gives the claim each binding narrows. Each row carries `effective_access`, the mask the binding gives, `claim & expand(coalesce(binding, claim))`, so a caller never computes it from the internal `_effective_access`. Ids without a type give no rows.

4. `get_user_identities(user_ids)` returns the users' `user_identity` rows; `sa_created_by` on one is the grant that bound the user. `get_object_types()` returns the type of every registered table.

5. Every argument given applies, and a call with none gives no rows. Soft deleted rows, grants outside their window and bindings of deleted claims are returned as they are, so a caller keeps the filter it had on `sa_deleted_at`, `starts_at` and `ends_at`.

6. The array forms of `get_users`, `get_organizations`, `get_locations` and `get_claims` take arrays and the first two arguments without a default, the email and external id lists with one, so a call with a user id still resolves to the reader form.

None of them settles or writes, and none tells what the reader may do: a write still goes through `mutation_access`, and a listing of what the reader sees through the reader forms.

`get_object_usage(user, object_ids, object_types)` is a reader's read. It settles the reader's share, which folds its pending usage, and returns per object its `object_info` name and the reader's `used_count`, NULL when the reader holds no pair on it. A NULL type takes the type of the reader's pair, so an id alone resolves to the pair the reader holds.

`get_relations(user, ids)` returns the reader's live `user_relation` rows, with `ids` only the ones named there: a relation is its user's alone, the rule `resolve_access` applies to `claimius.user_relation`, so another user's row or a soft deleted one never comes back. A caller filters the rows further by object, pin, priority, parent, sibling or description, and selects one relation by id through the same function.

`get_audit(user, required, limit, offset, ids)` on a disciple keeps only the rows named in `ids`, so one row by id is `get_audit(user, required, p_ids => ARRAY[id])`; without `ids` it is the listing. The gate `mutation_access(user, id, 'claimius.audit', required)` still tells a missing row from a hidden one.

`get_descendants(object_id)` is `get_descendants(object_type, object_id)` for an object whose type the caller does not hold: it runs the typed walk for each registered type the id is an object or an ancestor of, so a graph starting at an id walks one type instead of every registered one.

## The get views

A bun model with a belongs-to relation, an `Owner`, a `User`, a `Location`, an `Organization` or a `Creator`, joins the related model's table in the same statement: `LEFT JOIN "claimius"."organization" AS "owner" ON ...`. A function cannot stand there without changing what the join costs: a `SECURITY DEFINER` function is never inlined, so a join through one reads every row of the table on each statement before the join condition applies.

Five views carry those joins instead: `claimius.get_users` over `samna_user`, `claimius.get_organizations` over `organization`, `claimius.get_locations` over `location`, `claimius.get_claims` over `claim` and `claimius.get_grants` over `user_claim`.

1. Each is `SELECT * FROM` its table and nothing else: every row, soft deleted and inactive ones included, with no reader and no access filter. They are the rows of the lookups by id, as a relation, so a join or a select through one returns exactly what it returned from the table, and a caller keeps the filters it had on `sa_deleted_at`, `starts_at` and `ends_at`.

2. A view runs with its owner's rights and the planner merges it into the statement, so the join condition reaches the table's own index: a join through `get_organizations` probes `organization_pkey` as the join on the table did.

3. `claimius_reader` holds `SELECT` on the five views, read and write on the token tables it keeps itself, `session`, `dpop_jti`, `rate_limit`, `par_request` and `jwks_cache`, `SELECT` on the consent and analytics tables, and nothing on a model or derived table, granted in `V6.0`. The views carry the `get_` names of the lookups they mirror, so the rule holds as written: app code names a `claimius` model relation only through a `get_` name.

4. The bun models `SamnaUser`, `Organization`, `Location`, `Claim` and `UserClaim` in `shared` carry `select:claimius.get_<name>` beside `table:claimius.<name>` in their `bun.BaseModel` tag. bun reads a select, a belongs-to join and a has-many load from the `select` name and writes an insert, an update and a delete to the `table` name, so a model that `bun:",extend"`s one of them, and every relation pointing at one, reads through the view with no change at the call. A consumer model of its own over a claimius table, such as a compact owner, adds the same `select:` option.

5. The views are created in `V6.0`, the last file of the set, which runs on every build and replay, so `SELECT *` expands after every column change of the set and a new column reaches the view.

They answer what a row names, never what the reader may do: a write still goes through `mutation_access`, and what the reader sees through the reader forms.

## Which reads write

A read writes only its settle: the pairs it recomputes and the marks it moves, the `user_users_diff` rows those raise while the app has a viewer, its cursor, the `user_users` rows of a first directory view, the `user_users_diff` rows it folds into `user_users`, the reader's `usage_diff` rows it folds into its pairs and the `claim_graph_cache` row a graph read stores. Nothing else in a read path writes.

1. No cascade runs in a read. The soft delete of a user cascades to its grants, relations and field values through `tg_cascade_user_soft_delete`, once, in the write that deleted the user. `check_user_active` is a predicate.

2. A settle locks nothing per area: the areas are read from the cursor or the mark, never taken. It locks the reader's whole share with one advisory lock, its own pairs and its cursor, and the `user_users_diff` and `user_users` rows of the objects it folds, and never anything per object; see [Materialization](./materialization.md#locks-a-settle-takes).

3. Every lock a settle waits for is bounded by `_settle_lock_timeout()`, a twentieth of `deadlock_timeout`, or the caller's `lock_timeout` when shorter. A settle that waits longer, or is chosen as a deadlock victim, leaves its pair or its cursor where it was, and the read takes the pairs as they stand, the gate walking the ancestors for that one answer. So a read gives up any wait it closes into a cycle before the deadlock check of the writer it blocks runs, and a write is never aborted for a read.

4. Any other error in a settle raises from the read, so a broken settle is seen, never read as stale pairs.

5. `get_scope_access` reads no pair and no area and is `STABLE`, so it answers on a hot standby. Every function that runs `reconcile_if_pending` may write, so it runs on the primary; the middleware routes a pure read to a replica and repeats a read the standby refuses with SQLSTATE `25006` on the primary; see [Access in Go](./middleware/access.md).

## Definer rights

Every `get_*`, `search`, `resolve_access` and `mutation_access` is `SECURITY DEFINER` with `search_path = claimius, pg_temp`. The `claimius_reader` role holds no privilege on a model or derived table, so a connection that only reads gets answers through these functions and the five [get views](#the-get-views), and cannot select the pairs, the closure or the grants directly. The settle a read performs is written with the owner's privileges, and the pinned `search_path` keeps a caller from shadowing a claimius object with its own. See [Security](./security.md).

## Plans

PL/pgSQL caches a plan per statement and, under `plan_cache_mode = auto`, switches to a generic plan after five executions when its estimated cost is close to that of the custom plans. Every hot statement of a read filters on equalities over indexed columns, so its generic plan is the same index path as its custom plans, and a listing reads the reader's own range, so its plan does not depend on how many pairs the reader holds. The middleware sends each access call as a named prepared statement, so a connection parses and plans it once.

## Cost

### get_access

| Step | Entries |
|---|---|
| `check_user_active`, one probe of `samna_user` by `(user_id, app_id)` | 1 |
| The guest probe of the same row, which caps a guest at read | 1 |
| The pair, one probe of the `user_object` primary key, with its mark | 1 |
| The object's `object_info` row, for owner, location and owner root | 1 |
| The trimmed position of the app | 1 |
| The areas of the object's root raised at or after the mark, one range of `idx_access_diff_root_txid`, and the reader's assignments since the mark, one range of `idx_access_diff_user`: both empty when nothing was raised | 2 |
| `_best_grant` over the pair's `G_x` derivations | 0 |

```
entries(get_access) = 8                                   rows written = 0
```

When something was raised in the object's root since the mark, the ranges return the areas, at most 16, and the check reads the object's closure range, `a_x + t_x` entries, to test them; the mark then moves to the snapshot `xmin` as a heap only update, so the next check is the quiet one again. An area on the object, with cascade on an ancestor, or an assignment of the reader recomputes the one pair, once.

### get_direct_access

With `a'_x = d_o + d_l + d_q + Σ_{l on the location chain} (d(owner(l)) + 1)` the ancestor ranges read `a'_x + O(1)` entries, the reader's grants `1 + 2C`, the bindings `a'_x + 1` probes returning `B'_x` bindings, and the composed branch `2 B'_x` more:

```
entries(get_direct_access) ≈ 4 + 2C + a'_x + 3 B'_x + O(1)
```

### resolve_access and mutation_access

A hit costs one check; a miss costs one check and one existence probe.

### get_objects

Per row the page reads the index entry, the heap tuple for `grants`, and one `object_info` probe by key:

```
entries(page) = s + h + 2p                    rows sorted = 0
```

for a page of `p` rows, a descent of `h` levels and the settle `s` of the reader's share, which runs before the range is read and does not depend on the page. Without `limit`, `p = V_{u,τ}`. A page is linear in its own size.

### The other listings

| Listing | Entries |
|---|---|
| `get_organizations`, `get_locations` | One `get_objects` page per round, plus one table probe and one `app_has_object` probe of `idx_app_object_unique` per row read |
| `get_secrets` | One `get_objects` page plus one `samna_secret` probe per row |
| `get_users` | `Σ_{x ∈ V_u} U_x` on the reader's first view and nothing after, then `1 + S_u + V_u + Σ_{x ∈ T_u} (m_x + U_x) + 2C + b + |targets|`: the reader's `S_u` rows of `idx_user_users_viewer`, the `V_u` probes of `user_users_diff` by key that find its `T_u` pending objects, the rows still pending on them, its grants with their `b` bindings on `claimius.samna_user`, and one `samna_user` probe per target |
| `get_claims(user, app, required)` | One range of `idx_user_claim_user_app` and one claim probe per live grant, or one grant probe per live claim of every app when the planner scans `claim`; the function filters no `app_id` |
| `get_claims(user, app, object_id, object_type)` | The point settle and one claim probe per derivation of the pair |
| `get_audit` | The reader's `V_{u,organization}` pairs, then the app's rows newest first until `limit + offset` are kept |
| `get_users(user_ids, ids, emails, external_ids)`, `get_organizations(ids, owner_ids, external_ids)`, `get_locations(ids, parent_ids, external_ids)`, `get_claims(ids, owner_ids)` | One index probe per id given, or one range of the owner or parent index per owner or parent, plus a heap tuple per row. An external id probes the `external_id` index of `organization` or `location`. A user named only by email or external id reads `samna_user` once: the email is compared on `lower(email)` and the `external_id` index holds live rows only |
| `get_relations` | One range of `idx_user_relation_user_id` |
| `get_descendants(object_id)` | One `object_info` and one `idx_inheritance_info_ancestor` probe per registered type, then the typed walk for each type that holds the id |
| The get views | What the same statement costs on the table: the view merges into it |
| `get_grants`, `get_bindings` | One probe or range of the index on the first list given, ids, users, claims, owners for a grant and type with ids, claims, type for a binding, the other lists tested on the rows read; with `clients` false one `samna_client` probe per row |
| `get_object_usage` | The settle of the reader's share, then per object one pair probe and one `object_info` probe; an untyped id reads the reader's range of pairs |

## Proofs and measurements

### Why the pairs are stored

Storing `A` per pair and listing it from the reader's range costs less than deriving each listed pair from the base relations whenever a pair is listed at least as often as its inputs change, and a pair whose inputs change without being read costs one area row and nothing else. Let `R_x` be the index entries one pair's derivation reads, one closure entry, one `object_info` row and one pair probe per object of a share plus a descent per bound node inside it, or `a_x + 1` binding probes with the closure range for a single pair, [Materialization](./materialization.md#recomputing-a-share), and `c = 2` the entries a stored listing reads per row, the pair and its `object_info` row.

1. Derived at read time, a listing of `u` over type `τ` must enumerate the candidates: the descendants of every inheriting binding the reader holds, with multiplicity `δ ≥ 1` for overlapping bindings, and derive each, `δ R_x + c` entries per listed row, then sort, since the candidates come in no key order.

2. Stored, a listing reads `c` per row in `object_id` order and sorts nothing. A change of the pair's inputs raises one area row; the pair is recomputed only when the reader next reads, once however many changes came before: `min(W, L)` recomputes of `R_x` over `L` listings and `W` changes.

3. Stored wins when

```
L c + min(W, L) R_x  <  L (δ R_x + c)    ⟺    min(W, L) < δ L
```

which holds for every `δ ≥ 1` and `L ≥ 1`, and holds strictly when `W < L` or `δ > 1`. With `L = 0` the stored side pays `W` area raises and no recompute, the derived side nothing.

4. The rows of an area whose holder never reads are never recomputed, so the stored side's work is bounded by reads, not by changes.

So `user_object` is the listing index. `get_objects`, `search` and the graphs read it after the reader's share settles, and the check reads the same stored pair, so a listing row and a check agree by construction.

### Measurements

1. `get_access` reads 7 entries on the prophet with nothing raised, 4 on a disciple, measured in the `check_mark` suite, and 3 in the bench on analyzed tables where the planner reads the small tables sequentially. After an unrelated raise in the same root it reads 15 entries on the prophet and 10 on a disciple.

2. On a prophet a page of 50 rows from `get_objects` read 101 index entries in 8 ms, and a full listing of 16,001 rows 32,005 entries in 0.10 seconds, two per row, with no sort. In the `settle_share` suite the second listing of 1,001 rows read 2,009 entries on the prophet and 2,005 on a disciple.

3. `get_users` for one of 200 holders over 2,000 organizations: the build of 400,000 holder pairs through a hash join, 6,212 index entries and 2.0 seconds on the first view, 7 entries and 10 ms on every later view.

4. The `no_bypass` suite refuses a write of the pair under the settle and sees the check and the listing raise.

**Sources.** E. N. Hanson, [A Performance Analysis of View Materialization Strategies](https://www2.eecs.berkeley.edu/Pubs/TechRpts/1986/789.html), SIGMOD 1987. R. Chirkova and J. Yang, [Materialized Views](https://dsf.berkeley.edu/cs286/papers/mv-fntdb2012.pdf), Foundations and Trends in Databases 2012, on deferred maintenance. [PostgreSQL, CREATE FUNCTION, Writing SECURITY DEFINER Functions Safely](https://www.postgresql.org/docs/17/sql-createfunction.html#SQL-CREATEFUNCTION-SECURITY). [PostgreSQL, PREPARE](https://www.postgresql.org/docs/17/sql-prepare.html), on generic and custom plans.
