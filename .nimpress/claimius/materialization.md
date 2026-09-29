---
layout: doc
title: Materialization
description: The per pair access index user_object, the recompute every settle is made of with its read and write counts, how a write raises an area in access_diff, how a check settles one pair and a listing the reader's own share, when an area clears, and the drain rule that settles shares ahead of reads.
tags: user_object, object_info, access_diff, access_diff_user, user_activity, recompute_user_object, reconcile_if_pending, _settle_diff_share, _settle_object_diff, _settle_claim_diff, _diff_holders, _clear_diff, drain_access_diff, raise_count, reconcile_queue
lastUpdated: true
---

# Materialization

Claimius stores the access relation `A` of [the model](./index.md#the-access-of-a-pair) per pair in `user_object`, so a check reads one pair and a listing one index range. A write does not settle the users it affects. It settles its own actor on the rows it names and records what changed as one area row; each user then settles its own part of the area when it reads, a check the one pair it asks about, a listing its whole share. The proofs are [Result 1](./theory.md#result-1-access-is-a-nonrecursive-view) and [Result 5](./theory.md#result-5-the-settle-is-per-pair-and-per-reader).

## The tables

| Table | One row per | Holds | Indexes |
|---|---|---|---|
| `user_object` | app, user and object with `A ≠ 0` | `sa_access`, `grants`, `scope`, `direct_grant`, `used_count`, `last_used_at` | primary key `(user_id, app_id, object_type, object_id) INCLUDE (sa_access)`; `idx_user_object_object_user (app_id, object_type, object_id, user_id)` |
| `object_info` | live registered object | owner, location, root, `sa_name`, `sa_description`, `sa_link`, and `search_vector` generated from name and description | primary key `(object_type, object_id)`; `(sa_owner_id, sa_name, object_id)`; `(sa_location_id, sa_name, object_id)`; GIN on `search_vector` |
| `access_diff` | open area | `kind` (`object` or `claim`), the object or the claim, `user_id` for an assignment, `cascade`, `raise_count`, `sa_created_at` | primary key; `(app_id, object_type, object_id) WHERE kind = 'object'`; `(app_id, claim_id) WHERE kind = 'claim'`; `(app_id, user_id) WHERE kind = 'claim' AND user_id IS NOT NULL` |
| `access_diff_user` | area and user that settled its share | the `raise_count` settled at | primary key `(diff_id, user_id)`, removed with its area |
| `user_activity` | user and app that used it | `last_used_at` | primary key `(user_id, app_id)` |
| `reconcile_queue` | app, user and object | a pair waiting for its user's next read | primary key; `(app_id, user_id)` |

A disciple carries the same tables without `app_id`, keyed `(user_id, object_type, object_id)`, because it mirrors one app.

`grants` is the array of the surviving derivations of the pair, each `{claim_id, access, scope, cascaded_from, composed_from, tree_type}`, the pair's why provenance from which a read picks its actor token. `scope` holds the field masks of [Redaction](./redaction.md). The per object fields live once in `object_info`, so a pair holds only what is per user. None of these tables is written by application code: access changes by changing grants and bindings.

`user_object` is stored with `fillfactor = 90`, which leaves room on each page for an update that changes only `grants`, `scope` or `direct_grant` to stay on its page as a heap only tuple and write no index entry; an update of `sa_access`, which the primary key includes, writes both entries again ([PostgreSQL, Heap Only Tuples](https://www.postgresql.org/docs/17/storage-hot.html)). `user_object` and `access_diff` run autovacuum at a scale factor of 0.02 instead of 0.2, because both turn over a small part of their rows on every settle.

## Recomputing a share

`recompute_user_object(app, u, τ, S)` recomputes the pairs of one user on a set `S` of objects of one type in one pass of set based statements. Every settle is made of calls to it, one per type of a share. It takes the per object advisory locks of `S` sorted by key, `_lock_objects`, unless the caller already holds them, then:

| Step | Reads | Entries |
|---|---|---|
| `table_info` of `τ`, twice | primary key probes | 2 |
| The live rows of `S`, `_denormalize_objects`, each joined to its owner and its location | a probe of the row, the organization and the location per object | `3 |S|` |
| The creator of each row, `sa_created_by` to its live `user_claim` | one probe per object | `|S|` |
| The user's grants and their claims, the `user_claims` step of `_build_grants_for_objects` | one range of `idx_user_claim_user_app` and a claim probe per grant | `1 + 2C` |
| Direct bindings on each object | one probe of `idx_claim_object_object` per object | `Σ_x (1 + b_x)` |
| Ancestors of each object | one range of `idx_inheritance_info_descendant` per object, self edges included | `Σ_x (a_x + t_x)` |
| Bindings on each ancestor, kept when they inherit | one probe per ancestor | `Σ_x (B_x − b_x)` |
| The composed branch: the object and its ancestors again, the bindings on each, the bound claim and the links into it | a closure range, a binding probe per node, a claim probe and a link probe per binding | `Σ_x (a_x + t_x + 1 + 3 B_x)` |

```
R(u, S) = 3 + 2C + Σ_{x ∈ S} R_x          R_x = 6 + 4 a_x + 2 t_x + 4 B_x
```

with `t_x ≤ 3` the trees holding `x`. The descents that find nothing are counted as one entry. Nothing in `R` grows with `U`, `O` or the number of claims in the app; it grows with the reader's grants, the depth of `x` and the bindings on its chain, which is [Result 2](./theory.md#result-2-the-model-stays-below-the-reachability-bound). The derivations are then settled in memory by `_apply_denies`, `G × X` mask operations for `G` grants and `X` denies on the pair.

### What a changed pair writes

The pairs of `S` that no derivation and no creation reaches, or whose object is gone, are deleted; the others are upserted with `ON CONFLICT ... DO UPDATE ... WHERE ... IS DISTINCT FROM`, so a pair whose every column is unchanged writes nothing and fires no trigger.

| Change | Rows | Index entries |
|---|---|---|
| A pair inserted or deleted | 1 `user_object` row | 2 |
| A pair updated in `sa_access` | 1 row version | 2 |
| A pair updated in `grants`, `scope` or `direct_grant` only | 1 row version | 0 when it stays on its page |
| A pair on an object with no `object_info` row yet | 1 `object_info` row, `ON CONFLICT DO NOTHING` | 3 btree entries and one GIN entry per lexeme |
| A pair inserted or deleted on `x`, not a claim | up to `2 (U_x − 1)` `user_users` rows, from `U_x` entries read | 3 per row written |
| Any pair written | one delete probe of `claim_graph_cache` for its user | 0 or 1 row |

The `user_users` counting is a statement trigger over the pairs of the statement; see [Users](./users.md) and [Result 7](./theory.md#result-7-counting-keeps-userusers-exact). Before `d4c71ff` a settled pair wrote 19 index entries, one per index of the per object columns and the GIN postings of `grants` and the search vector; it now writes 2.

## Writes raise areas

An area is the smallest description of what a write changed, keyed on what changed and never on the users it affects.

| Write | Settles in the write | Raises |
|---|---|---|
| Insert, owner or location change, or soft delete of a registered row | the actor's pair on the row | `object` on the row |
| Any write of an organization, a location or a parenthood row that is not a rename | the actor's pair on the row | `object` with `cascade` on the row |
| A rename, `name` or `description` only | the actor's pair on the row, and the one `object_info` row | nothing |
| `claim` | the actor's pair on the claim | `claim` on the claim |
| `claim_object` | the actor's pairs on the claim and on the bound object | `claim` on the claim, and on the linked claim for a link |
| `user_claim` | the actor's pair on the claim | `claim` on the claim with `user_id` the assignee |

`_raise_object_diff` and `_raise_claim_diff` insert with a deterministic id, `composite_id('object', app, type, id)`, `composite_id('claim', app, claim)` or `composite_id('claim', app, claim, user)`, `ON CONFLICT (id) DO UPDATE SET raise_count = raise_count + 1`. So `m` raises of one area before it clears leave one row with `raise_count = m`: one probe and one row version per raise, whatever the area reaches. The actor is `_actor_user_for_row`, the user behind `sa_created_by`, one probe; claimius is always entered in the context of a user or a client, so it is known. Under `replay_mode` no actor is settled and only the areas are raised, which is how a disciple and a mirroring prophet apply a page.

## A check settles one pair

`get_access`, `get_direct_access` and `get_claims` for one object begin with `reconcile_if_pending(app, u, τ, x)`:

1. It deletes any `reconcile_queue` row of the pair, one probe.

2. When the app holds no open area, two probes of the partial `access_diff` indexes find nothing and the check goes on: 3 entries in all.

3. Otherwise it tests whether an open area covers the pair, not yet settled by `u` at its current `raise_count`: an object area on `x`, or on an ancestor of `x` with `cascade`; a claim area of a claim `u` holds or links to, or one raised by an assignment to `u`, whose bindings, live or removed, sit on `x` or an ancestor, directly or through a claim it links to, or which the pair's `grants` name as `claim_id` or `composed_from`, or which is `x` itself. The test reads the `a_x + t_x` closure rows of `x`, the reader's `C` grants and their `L` links, one `access_diff` probe per node and per held claim, and the bindings of the claims found open on the nodes.

4. When covered, it recomputes the one pair, `R(u, {x})`.

Nothing is recorded in `access_diff_user`: the rest of the reader's share is still open, and the next check of another pair tests again. On the `settle_granularity` test, a claim bound over a node with three descendants and granted to three holders, the first read settled 12 pairs, every holder on every object, before `e1ad529`; a check now settles 1 pair, a listing settles the reader's 4, and the other holders stay untouched until they read.

## A listing settles the reader's share

Every listing, search and graph begins with `reconcile_if_pending(app, u)`. It reads the app's open areas `Δ_a`: every `object` area, every `claim` area with `user_id = u`, and every other `claim` area of a claim `u` holds or links to, each tested against `access_diff_user` at its `raise_count`, in `sa_created_at` order, `FOR KEY SHARE SKIP LOCKED`. For each it calls `_settle_diff_share`:

1. It takes the area `FOR KEY SHARE SKIP LOCKED`, which a writer raising the area again does not wait on, and `pg_try_advisory_xact_lock` on the key of area and user, so two reads of one user never settle the same share at once; an area held by another settle is skipped and read as it stands.

2. It settles the share, `_settle_object_diff` or `_settle_claim_diff`, set based: the per object locks of the whole share once, sorted, then one `recompute_user_object` per object type.

3. It records `(area, u, raise_count)` in `access_diff_user`.

4. It calls `_clear_diff`, which deletes the area once every user of `_diff_holders` has a record at the current `raise_count`, under `FOR UPDATE SKIP LOCKED` and only when `raise_count` is unchanged, so a raise between the test and the delete keeps the row.

5. A settle that fails is left open and answers false; the read goes on.

It then recomputes every `reconcile_queue` row of the reader. Nothing in the current sets writes to `reconcile_queue`; rows it holds drain on their user's next read.

### The share of an area

With `Y = {x} ∪ desc(x)` for an object area with `cascade` and `Y = {x}` without, `Bd(u)` the objects bound by `u`'s claims, directly or through one link, and `Bi(u) ⊆ Bd(u)` those whose binding and claim both inherit:

```
share(u, object area x) = {x} ∪ { y ∈ Y : (u, y) ∈ P  ∨  y ∈ Bd(u)  ∨  anc(y) ∩ Bi(u) ≠ ∅ }
share(u, claim area c)  = {c} ∪ Bc ∪ ⋃_{y ∈ Bc, cascades} desc(y) ∪ { y : (u, y) ∈ P, grants name c }
```

where `Bc` holds the objects bound by `c` and by the claims `c` links to, each binding counted while live or when changed since the area opened, `sa_updated_at ≥ access_diff.sa_created_at`, so the pairs a removed binding used to reach are settled away. A binding cascades when it and its claim inherit, or changed since the area opened.

| Share | Entries read to find it |
|---|---|
| Object area | `1 + |desc(x)|` closure entries, the reader's grants, bindings and links, then per object of `Y` one pair probe and its closure range, `Σ_{y ∈ Y} (2 + a_y + t_y)` |
| Claim area | the bindings of `c` and of its linked claims, one closure range per cascading binding, `|T_c|` entries, and the reader's range of pairs in the app, `V_u` entries with their `grants` |

The share is then recomputed at `Σ_τ R(u, share_τ)`.

### The holders of an area

`_diff_holders` names the users whose share of an area is theirs to settle, with repeats:

```
holders(claim area c, user v) = {v}
holders(claim area c)         = H_c
holders(object area x)        = creator(x) ∪ U_x ∪ [cascade] ⋃_{y ∈ desc(x)} U_y ∪ { holders of bindings on x and its ancestors }
```

An area clears after each of its holders has read once, or the drain settled them; `_clear_diff` probes `access_diff_user` for holders until the first one unsettled, at most `|holders|` probes per call.

### What an area costs over its life

A binding change on a claim `c` costs the writer its own row, its actor's two pairs and one area row. Each holder `j` that reads pays its share, about `|T_c|` pairs, once per raise it reads after; a holder that never reads pays nothing. So the total is `Σ_{j reads} R(j, share_j)`, never `|H_c| × |T_c|` inside one transaction, and never more than the holders that read.

## The drain

`drain_access_diff(p_limit_pairs)` settles shares ahead of their reads, outside any request, under the rule of [Result 6](./theory.md#result-6-the-drain-rule): holder `j` of area `a` is taken when its read rate is at least the area's raise rate,

```
r̂_j = 1 / (now − user_activity.last_used_at)      λ̂_a = raise_count / (now − access_diff.sa_created_at)      take when r̂_j ≥ λ̂_a
```

tested as `now − sa_created_at ≥ raise_count × (now − last_used_at)`. A holder with no `user_activity` row is never drained. Shares are taken most recently active user first, at most `p_limit_pairs` of them, each through `_settle_diff_share`, so a share a read is settling at the same moment is skipped. On a prophet it drains only the areas of its own system app and of apps with no consumer in `sync_consumer` in contact for 7 days; the areas of an app a disciple serves are drained on that disciple, so each settle is paid once. A disciple drains every area it holds. It returns the shares settled.

## Rebuilding

`recompute_user_object` can be called to force a set of pairs. `rebuild_user_object` empties `user_object`, recomputes the pair of every holder on every object a live binding names directly, one recompute per distinct `(app, user, bound object)`, and rebuilds every user's `user_users` rows with `_refresh_user_users`. Descendants reached only through a cascade fill on the next settle that covers them.
