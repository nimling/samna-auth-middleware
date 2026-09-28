---
layout: doc
title: Reads
description: The claimius read surface, what each read function reads and writes in index entries and rows as a function of the data, and why a check and a listing are independent of tree depth and of the number of users.
tags: get_access, get_direct_access, mutation_access, resolve_access, get_objects, get_users, get_claims, get_audit, _best_grant, reconcile_if_pending, read cost
lastUpdated: true
---

# Reads

Every read function takes the user and the app first. It runs `reconcile_if_pending(app, user)`, which settles the outstanding areas the read meets, then `check_user_active`, one probe, and then answers from `user_object` or the base relations. Soft deleted rows, grants outside their window and denied bits never reach the caller.

## The surface

| Function | Answers |
|---|---|
| `get_access(user, app, object_id, object_type, required)` | The chosen grant on one object, at most one row, with `user_claim_id` |
| `get_direct_access(user, app, object_id, object_type, required, raise)` | The closest, strongest grant bound on the object or an ancestor |
| `resolve_access(user, app, object_id text, object_type, required)` | The verdict for any type: audit, relation and user lookups, otherwise the two above |
| `mutation_access(user, object_id text, object_type, required)` | The gate: a row, `P0403` or `P0404` |
| `get_user_access(actor, app, target, required)` | The actor's access on a user named three ways; see [Users](./users.md) |
| `get_objects(user, app, object_type, required)` | Every object of a type the user reaches, with rendering fields and `user_claim_id` |
| `get_organizations`, `get_locations` | `get_objects` intersected with the app's `app_object` links |
| `get_users`, `get_claims`, `get_audit`, `get_secrets` | Listings by type |
| `search`, `get_owner_graph`, `get_claim_graph` | See [Search](./search.md) and [Graphs](./graphs.md) |

## What every read pays first

`reconcile_if_pending` reads the app's `Δ_a` outstanding areas and, for each claim area, tests the reader's grants and links against it. With nothing outstanding it reads `Δ_a = 0` rows and writes nothing. When it finds areas it settles them for every user they reach, at the cost of [Materialization](./materialization.md#what-a-settle-costs), inside the read.

## What a check costs

`get_access` reads the pair and chooses a grant:

1. The pair, filtered on `user_id`, `app_id`, `object_type`, `object_id` and the mask. The planner answers it from the shorter of two ranges: `idx_user_object_object` returns the `U_x` pairs of the object, `idx_user_object_user_type` the `V_{u,τ}` pairs of the reader's type with the mask tested in the index. The pair's primary key, a composite id of the same four values, is not part of the filter.

2. `_best_grant` over the pair's `G_x` derivations: per derivation one probe of `idx_user_claim_unique` for the live grant inside its window and one `claim` primary key probe.

3. The owner's root, one `organization` probe.

```
entries(get_access) = Δ_a + 1 + min(U_x, V_{u,τ}) + 2 G_x + 1          rows written = 0 when Δ_a = 0
```

Nothing in it depends on the depth of the trees, the number of claims bound above the object, or the number of users beyond `U_x`: the derivation was paid when the pair was settled. That is the point of storing the pair.

## What a walk to the closest grant costs

`get_direct_access` answers without `user_object`, for a caller that expects to act on an object whose pair is not there:

1. `get_ancestors`: the object's ownership chain, its location chain and, for each location on it, the owner's chain, and its parenthood chain, one range of `idx_inheritance_info_descendant` each. With `a'_x = d_o + d_l + d_q + Σ_{l on the location chain} (d(owner(l)) + 1)` ancestors this reads `a'_x + O(1)` entries.

2. The bindings on each ancestor, `idx_claim_object_object`, joined to the reader's live grant on each and its claim. A binding on an ancestor above hop 0 counts only when claim and binding both inherit.

3. The same walk again for denies, in a `NOT EXISTS`: a deny on any ancestor that covers a required bit empties the answer.

4. The first row by hops, then bits, then grant id.

```
entries(get_direct_access) ≈ 2 (a'_x + 3 B'_x) + O(1)
```

for `B'_x` bindings on those ancestors. Its location term follows each location's owner, a wider set of ancestors than the closure the settle reads; see [Locations](./locations.md).

## What the gate costs

`mutation_access` calls `resolve_access`, which tries `get_access` and on a miss `get_direct_access`; on a second miss `mutation_access` calls `get_direct_access` again with `raise`, which probes the object's live row once and raises `P0403` or `P0404`. A hit costs one check; a miss costs one check, two walks and one probe, and runs `reconcile_if_pending` three times. For `claimius.samna_user` the target is resolved in the app's `samna_user` rows by `external_id`, `user_id` or `id`; `external_id` carries no index there, so the resolution reads the app's `U` user rows.

## What a listing costs

`get_objects` reads the reader's range of `idx_user_object_user_type` for one type, the heap row of each pair for its `grants`, name and owner, and chooses a grant per row:

```
entries(get_objects) = Δ_a + 1 + V_{u,τ} (1 + 2 G + 1)          comparisons = V_{u,τ} log₂ V_{u,τ}
```

Rows are ordered by the number of bits set, then `object_id`, and returned whole; the implementing side joins the range to its own table or pages over it. A listing is linear in what the reader reaches and independent of the depth of the trees, the number of bindings and the number of other users.

| Listing | Reads |
|---|---|
| `get_organizations`, `get_locations` | One range of `app_object_ids` for the type intersected with `get_objects` |
| `get_users` | `S_u` rows of `idx_user_users_viewer`, the reader's grants and their bindings on `claimius.samna_user`, then one `samna_user` probe per target |
| `get_claims(user, app, required)` | Every live claim row, each tested against the reader's grants and pairs with two probes |
| `get_claims(user, app, object_id, object_type)` | The reader's pair on the object and the claims of its `G_x` derivations |
| `get_audit` | The app's audit rows newest first through `idx_audit_sa_created_at`, kept when their owner is among the reader's organizations, until `limit + offset` are kept |

## A check writes nothing, a read writes only its settle

Every read function is `VOLATILE` in effect, because `reconcile_if_pending` locks with `FOR UPDATE SKIP LOCKED` and writes when it finds an area. Nothing else in a read path writes.

1. No cascade runs in a read. The soft delete of a user cascades to its grants, relations and field values through `tg_cascade_user_soft_delete`, once, in the write that deleted the user. `check_user_active` is a predicate.

2. A settle locks the areas it takes and the pairs it writes, for one statement each. A concurrent read that meets an area being settled skips it and reads the pairs as they stand.

3. Every statement runs under `statement_timeout` and every session under `idle_in_transaction_session_timeout`, so no statement outlives its client.

## Plans

PL/pgSQL caches a plan per statement and, under `plan_cache_mode = auto`, switches to a generic plan after five executions when its estimated cost is close to the custom plans ([PostgreSQL, PREPARE](https://www.postgresql.org/docs/17/sql-prepare.html)). Every hot statement of a read filters on equalities over indexed columns, so its generic plan is the same index path as its custom plans. The listings, whose row counts vary from one user to the next by orders of magnitude, read the reader's own index range, so the plan does not depend on the count either. A client that sends each access statement as a named prepared statement pays bind and execute per call, and parse and plan once per connection.
