---
layout: doc
title: Reads
description: The claimius read surface and what each read function reads and writes in index entries and rows as a function of the data, why a check and a page of a listing are independent of tree depth and of the number of users, and which reads may run on a replica.
tags: get_access, get_direct_access, mutation_access, resolve_access, get_user_access, get_objects, get_organizations, get_locations, get_secrets, get_users, get_claims, get_audit, _best_grant, reconcile_if_pending, key paging, security definer
lastUpdated: true
---

# Reads

Every read function takes the user and the app first. It runs `reconcile_if_pending`, which settles what the read meets, see [Materialization](./materialization.md), then `check_user_active`, one probe of `samna_user` by `(user_id, app_id)`, and then answers from `user_object` or from the base relations. Soft deleted rows, grants outside their window and denied bits never reach the caller. On a disciple the same functions exist with the same arguments, and the app argument is accepted and ignored, because every row it holds is in scope.

## The surface

| Function | Answers |
|---|---|
| `get_access(user, app, object_id, object_type, required)` | The chosen grant on one object, at most one row, with its `user_claim_id` |
| `get_direct_access(user, app, object_id, object_type, required, raise)` | The closest, strongest grant bound on the object or an ancestor, walking the trees |
| `resolve_access(user, app, object_id text, object_type, required)` | The verdict for any type: audit, relation and user lookups, otherwise the two above |
| `mutation_access(user, object_id text, object_type, required)` | The gate: a row, SQLSTATE `P0403` or `P0404` |
| `get_user_access(actor, app, target text, required)` | The actor's access on a user named by `external_id`, `user_id` or row id; see [Users](./users.md) |
| `get_objects(user, app, object_type, required, after, limit)` | One page of the objects of a type the user reaches, with rendering fields and `user_claim_id` |
| `get_organizations`, `get_locations`, `get_secrets` | One `get_objects` page joined to the table, with the same `after` and `limit` |
| `get_users(user, app, required)` | The users the reader sees |
| `get_claims(user, app, required)`, `get_claims(user, app, object_id, object_type)` | The claims the reader holds or reads; the claims behind one pair |
| `get_audit(user, app, required, limit, offset)` | Audit rows of the organizations the reader reaches; see [Audit](./audit.md) |
| `get_scope_access(user, app, path, required)` | Scope grants matching a jsonpath; see [Capabilities](./capabilities.md) |
| `search`, `get_owner_graph`, `get_claim_graph` | See [Search](./search.md) and [Graphs](./graphs.md) |

## get_access

`get_access` reads the stored pair and chooses a grant:

| Step | Entries |
|---|---|
| `reconcile_if_pending(app, u, τ, x)` with no open area in the app: the `reconcile_queue` probe and two probes of the partial `access_diff` indexes | 3 |
| `check_user_active` | 1 |
| The pair, one probe of the `user_object` primary key, the mask tested on the `sa_access` it includes | 1 |
| The object's `object_info` row, for owner and location | 1 |
| `_best_grant` over the pair's `G_x` derivations: the live grant inside its window, one probe of `idx_user_claim_unique`, and its claim | `2 G_x` |
| The root of the owning organization | 1 |

```
entries(get_access) = 7 + 2 G_x                           rows written = 0
```

When the app holds open areas the covering test is added, `a_x + t_x` closure entries, the reader's grants and links, and one `access_diff` probe per node and per held claim, and at most one recompute `R(u, {x})` when an area covers the pair. Nothing in the answer depends on the depth of the trees, the bindings above the object, or the number of users: the derivation was paid when the pair settled.

`_best_grant(grants, user, app, required)` keeps the derivations whose mask covers `required` and whose grant is live and inside its window, and returns the one with the most bits set, then the highest mask, then the oldest claim. `scope` is the pair's field masks. `user_claim_id` is the actor token a follow on write stamps into `sa_created_by`; see [Claims](./claims.md).

## get_direct_access

`get_direct_access` answers without `user_object`, for a caller that expects to act on an object whose pair is absent:

1. `reconcile_if_pending` for the pair and `check_user_active`, as above.

2. `get_ancestors`: the object's ownership chain, its location chain and, for each location on it, the owning organization's chain, and its parenthood chain, one range of `idx_inheritance_info_descendant` each. With `a'_x = d_o + d_l + d_q + Σ_{l on the location chain} (d(owner(l)) + 1)` this reads `a'_x + O(1)` entries.

3. The reader's live grants inside their window, `1 + 2C`.

4. The bindings on each node, joined to the reader's grants, a binding above hop 0 counting only when claim and binding both inherit; and the composed bindings, each with its claim and its link: `a'_x + 1` probes returning `B'_x` bindings, and `2 B'_x` more for the composed branch.

5. The denies among the same derivations: an unscoped deny on the object itself subtracts from every candidate, one above it from the candidates found above; what remains is closed under implication. A binding scoped only to fields the type lacks grants nothing.

6. The first row by hops, then bits, then grant id.

```
entries(get_direct_access) ≈ 4 + 2C + a'_x + 3 B'_x + O(1)
```

With `raise`, a miss probes the object's live row once and raises `P0403` when it exists and `P0404` when it does not. It is `VOLATILE`, because its settle writes.

## resolve_access and mutation_access

`resolve_access` sends `claimius.audit` to a probe of the row and of the reader's organizations, `claimius.user_relation` to one probe matching `id` and `user_id`, `claimius.samna_user` to `get_user_access`, and every other type to `get_access` and on a miss to `get_direct_access`. A malformed id returns no row, so a batch of `resolve_access` over `unnest` never aborts on one.

`mutation_access` refuses a malformed id or an unregistered type with `P0404` before any read, then calls `resolve_access`; on a miss it calls `get_direct_access` with `raise`. A hit costs one check; a miss costs one check, two walks and one existence probe, and runs `reconcile_if_pending` three times, each finding the pair settled after the first. One round trip answers 200, 403 or 404.

## get_objects

`get_objects(user, app, type, required, after, limit)` reads the reader's range of the `user_object` primary key, `(user_id, app_id, object_type, object_id)` with `sa_access` included, from the first key past `after`, until `limit` rows are kept. Per row it reads the heap tuple for `grants`, chooses the grant with `_best_grant`, and probes `object_info` by key for name, description, link, owner and location, and the owner's root:

```
entries(page) = s + h + p (3 + 2 G)           rows sorted = 0
```

for a page of `p` rows, a descent of `h` levels and the settle `s` of the reader's share, which runs before the range is read and does not depend on the page. Rows come in `object_id` order straight from the index, and the next page starts after the last `object_id` of this one. Without `limit` the page is the whole range, `p = V_{u,τ}`. The mask test is written `(sa_access & required) >= required`, the equality for a submask of `required`, which the planner estimates as a range rather than a point, so it keeps the ordered scan instead of sorting a bitmap scan.

Measured at `V_{u,τ} = 5,000` among 200,160 pairs, a page of 50 read 50 entries of the `user_object` indexes and 110 across the schema with no sort, where the unpaged listing read 5,000 and 10,010 and sorted 5,000 rows. A page is linear in its own size and independent of the depth of the trees, the number of bindings, the number of other users and the size of the reader's range.

### Why the pairs are stored

Storing `A` per pair and listing it from the reader's range costs less than deriving each listed pair from the base relations whenever a pair is listed at least as often as its inputs change, and a pair whose inputs change without being read costs one area row and nothing else. Let `R_x = 6 + 4a_x + 2t_x + 4B_x` be the index entries one pair's derivation reads, [Materialization](./materialization.md#recomputing-a-share), and `c = 3 + 2 G_x` the entries a stored listing reads per row, the pair, its `object_info` row, the root organization and `2 G_x` grant probes.

1. Derived at read time, a listing of `u` over type `τ` must enumerate the candidates: the descendants of every inheriting binding the reader holds, with multiplicity `δ ≥ 1` for overlapping bindings, and derive each, `δ R_x + c` entries per listed row, then sort, since the candidates come in no key order.

2. Stored, a listing reads `c` per row in `object_id` order and sorts nothing. A change of the pair's inputs raises one area row; the pair is recomputed only when the reader next reads, once however many changes came before: `min(W, L)` recomputes of `R_x` over `L` listings and `W` changes.

3. Stored wins when

```
L c + min(W, L) R_x  <  L (δ R_x + c)    ⟺    min(W, L) < δ L
```

which holds for every `δ ≥ 1` and `L ≥ 1`, and holds strictly when `W < L` or `δ > 1`. With `L = 0` the stored side pays `W` area raises and no recompute, the derived side nothing.

4. The rows of an area whose holder never reads are never recomputed, so the stored side's work is bounded by reads, not by changes.

So `user_object` is the listing index. `get_objects`, `search` and the graphs read it after the reader's share settles, and the check reads the same stored pair, so a listing row and a check agree by construction.

**Sources.** E. N. Hanson, [A Performance Analysis of View Materialization Strategies](https://www2.eecs.berkeley.edu/Pubs/TechRpts/1986/789.html), SIGMOD 1987. R. Chirkova and J. Yang, [Materialized Views](https://dsf.berkeley.edu/cs286/papers/mv-fntdb2012.pdf), Foundations and Trends in Databases 2012, on deferred maintenance.

## The other listings

| Listing | Reads |
|---|---|
| `get_organizations`, `get_locations` | One `get_objects` page, each row joined to its table row by primary key and kept when live and linked to the app, one probe of `idx_app_object_unique`: `p` more rows and `p` probes. A disciple has no `app_object`, and keeps every live row |
| `get_secrets` | One `get_objects` page joined to `samna_secret`, kept when live and of the app |
| `get_users` | The reader itself, one range of `idx_user_users_viewer`, `S_u` rows, and the reader's grants with their bindings on `claimius.samna_user`, then one `samna_user` probe per target: `1 + S_u + 2C + b + |targets|` |
| `get_claims(user, app, required)` | Every live claim row, each tested with a probe of the reader's grant and a probe of the reader's pair on it: `3` entries per live claim |
| `get_claims(user, app, object_id, object_type)` | The reader's pair on the object and, per derivation of its `grants`, the live grant and the claim: `1 + 2 G_x` |
| `get_audit` | The reader's `V_{u,organization}` pairs, then the app's rows newest first until `limit + offset` are kept |

A listing carries no authority to act on a row in it: `get_objects` returns a `user_claim_id` per row, but a write goes through `mutation_access` for its one row.

## Which reads write

A read writes only its settle: the pairs it recomputes, the `access_diff_user` records of the shares it settles, the areas it clears and the `claim_graph_cache` row a graph read stores. Nothing else in a read path writes.

1. No cascade runs in a read. The soft delete of a user cascades to its grants, relations and field values through `tg_cascade_user_soft_delete`, once, in the write that deleted the user. `check_user_active` is a predicate.

2. A settle locks the areas it takes `FOR KEY SHARE SKIP LOCKED`, the reader's whole share with one advisory lock, and the objects it writes with sorted advisory locks, at most 512 of them before one covering lock takes their place; see [Materialization](./materialization.md#the-lock-budget). A read that meets an area another read is settling skips it and reads the pairs as they stand. Every lock a settle waits for is bounded by `_settle_lock_timeout()`, a twentieth of `deadlock_timeout`, or the caller's `lock_timeout` when shorter; a settle that waits longer gives up its area, pair or queue row, and the read takes the pairs as they stand. A read therefore leaves any wait it closes into a cycle before the deadlock check of the writer it blocks runs, and a write is never aborted for a read.

3. `get_scope_access` reads no pair and no area and is `STABLE`, so it answers on a hot standby. Every function that runs `reconcile_if_pending` may write, so it runs on the primary; the middleware routes a pure read to a replica and repeats a read the standby refuses with SQLSTATE `25006` on the primary; see [Access in Go](./middleware/access.md).

## Definer rights

Every `get_*`, `search`, `resolve_access` and `mutation_access` is `SECURITY DEFINER` with `search_path = claimius, pg_temp`. The `claimius_reader` role holds no privilege on any table, so a connection that only reads gets answers through these functions and cannot select the pairs, the closure or the grants directly. The settle a read performs is written with the owner's privileges, and the pinned `search_path` keeps a caller from shadowing a claimius object with its own ([PostgreSQL, CREATE FUNCTION, Writing SECURITY DEFINER Functions Safely](https://www.postgresql.org/docs/17/sql-createfunction.html#SQL-CREATEFUNCTION-SECURITY)). See [Security](./security.md).

## Plans

PL/pgSQL caches a plan per statement and, under `plan_cache_mode = auto`, switches to a generic plan after five executions when its estimated cost is close to that of the custom plans ([PostgreSQL, PREPARE](https://www.postgresql.org/docs/17/sql-prepare.html)). Every hot statement of a read filters on equalities over indexed columns, so its generic plan is the same index path as its custom plans, and a listing reads the reader's own range, so its plan does not depend on how many pairs the reader holds. The middleware sends each access call as a named prepared statement, so a connection parses and plans it once.
