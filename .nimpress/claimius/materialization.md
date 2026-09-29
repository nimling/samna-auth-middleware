---
layout: doc
title: Materialization
description: The per pair access index user_object, the recompute every settle is made of with its read and write counts, how a write raises an area in access_diff, how a check settles one pair and a listing the reader's own share under a bounded number of locks, when an area clears, and the drain rule that settles shares ahead of reads.
tags: user_object, object_info, access_diff, access_diff_user, user_activity, recompute_user_object, reconcile_if_pending, _object_lock_budget, _lock_objects, _lock_all_objects, _settle_diff_share, _settle_object_diff, _settle_claim_diff, _diff_holders, _clear_diff, drain_access_diff, raise_count, reconcile_queue
lastUpdated: true
---

# Materialization

Claimius stores the access relation `A` of [the model](./index.md#the-access-of-a-pair) per pair in `user_object`, so a check reads one pair and a listing one index range. A write does not settle the users it affects. It settles its own actor on the rows it names and records what changed as one area row; each user then settles its own part of the area when it reads, a check the one pair it asks about, a listing its share of the areas it needs. Why recomputing an area is exact is shown in [the model](./index.md#why-the-view-is-maintained-exactly), and why settling per pair and per reader returns current pairs in [Why a settle returns current pairs](#why-a-settle-returns-current-pairs).

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

with `t_x ≤ 3` the trees holding `x`. The descents that find nothing are counted as one entry. Nothing in `R` grows with `U`, `O` or the number of claims in the app; it grows with the reader's grants, the depth of `x` and the bindings on its chain, which keeps the model [below the reachability bound](./types.md#why-the-model-stays-below-the-reachability-bound). The derivations are then settled in memory by `_apply_denies`, `G × X` mask operations for `G` grants and `X` denies on the pair.

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

The `user_users` counting is a statement trigger over the pairs of the statement; see [Users](./users.md#why-the-count-is-exact). Before `d4c71ff` a settled pair wrote 19 index entries, one per index of the per object columns and the GIN postings of `grants` and the search vector; it now writes 2.

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

Every listing, search and graph begins with `reconcile_if_pending(app, u)`, and `get_objects`, `get_organizations` and `get_locations` pass the type they list, `reconcile_if_pending(app, u, τ)`. It first takes the transaction advisory lock of the reader's whole share, one key per app and user, with `pg_try_advisory_xact_lock`; a read that finds it held by another read of the same user settles nothing and reads the pairs as they stand. Holding it, it reads the app's open areas `Δ_a` the read needs: with `τ` the `object` areas on an object of type `τ` or with `cascade` on an ancestor of an object of type `τ`, one closure probe per such area, and without `τ` every `object` area; then every `claim` area with `user_id = u`, and every other `claim` area of a claim `u` holds or links to. Each is tested against `access_diff_user` at its `raise_count`, and at most `_object_lock_budget()`, 512, are taken, oldest first by `sa_created_at`, `FOR KEY SHARE SKIP LOCKED`; the rest wait for the next read or the drain. For each it calls `_settle_diff_share`:

1. It takes the area `FOR KEY SHARE SKIP LOCKED`, which a writer raising the area again does not wait on, and the share lock of the reader, which the read already holds, so two reads of one user never settle at once and a share lock is one lock however many areas are settled under it; an area held by another settle is skipped and read as it stands.

2. It settles the share, `_settle_object_diff` or `_settle_claim_diff`, set based: the per object locks of the whole share once, sorted, then one `recompute_user_object` per object type.

3. It records `(area, u, raise_count)` in `access_diff_user`.

4. It calls `_clear_diff`, which deletes the area once every user of `_diff_holders` has a record at the current `raise_count`, under `FOR UPDATE SKIP LOCKED` and only when `raise_count` is unchanged, so a raise between the test and the delete keeps the row.

5. A settle that fails is left open and answers false; the read goes on.

It then recomputes every `reconcile_queue` row of the reader. Nothing in the current sets writes to `reconcile_queue`; rows it holds drain on their user's next read.

### The lock budget

Every advisory lock a transaction holds is an entry in the server's shared lock table, which holds `max_locks_per_transaction × max_connections` entries for every transaction together. A settle takes one per object of its share, so a read whose open areas reach thousands of objects would take thousands of entries from one connection, and a table that runs out fails every transaction on the server, not only that one. `_object_lock_budget()`, 512, bounds it:

1. A read holds one share lock, whatever number of areas it settles.

2. A listing of one type settles only the areas that can hold a pair of that type, and at most 512 of them per call.

3. `_lock_objects` counts the per object locks the transaction holds in `claimius.object_locks`. The first call of a transaction takes the covering lock every per object locker holds shared, `hashtext('claimius._lock_objects')`. A call that would carry the count past 512 takes the covering lock exclusive instead, one lock standing for every object, and every later call in the transaction returns at once. So a transaction holds at most 512 per object locks, the covering lock and its share locks.

4. Taking the covering lock exclusive while holding it shared is an upgrade, which can deadlock with a transaction waiting on one of the per object locks held. At four object locks an area, a batch of more than `512 / 4 = 128` areas spends the budget, so a read that settles that many takes the covering lock exclusive through `_lock_all_objects` before its first per object lock, and `_lock_all_objects` refuses, answering false, in a transaction that already holds one.

5. Exclusion is kept past the budget: the exclusive covering lock conflicts with the shared one every per object locker holds, so a transaction over the budget excludes every settle at once where it would have excluded its objects one by one. That is the price of a crossing, paid only by a read with that many areas open.

With `A_u` the reader's open areas that a read needs:

```
share locks     = 1
areas settled   = min(A_u, 512), the oldest
object locks    ≤ 512, then one exclusive covering lock in place of any more, taken first when areas settled > 128
```

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

### Why a settle returns current pairs

A check that settles only the pair it asks about, when an open area covers that pair, and a listing that settles only the reader's own share of each area it meets, return the pairs a full recompute would:

1. A pair `(u, x)` can be stale only when an open area's support contains it, by [the model](./index.md#why-the-view-is-maintained-exactly). `reconcile_if_pending(app, u, τ, x)` tests exactly those areas: an object area on `x`, or on an ancestor of `x` with `cascade`; a claim area of a claim `u` holds, links to, or was assigned (`user_id = u`), whose bindings, live or removed, sit on `x` or an ancestor, directly or through a claim it links to, or whose claim the pair's `grants` name as `claim_id` or `composed_from`, or which is `x` itself. An area is skipped only when `access_diff_user` records `u` at the area's current `raise_count`. When any area is open the pair is recomputed, so the pair read is current.

2. A listing of type `τ` settles, through `_settle_diff_share`, the reader's share of every open object area that can hold a pair of type `τ`, one on an object of type `τ` or one with `cascade` over a descendant of type `τ`, a listing without a type every open object area, and every claim area of a claim the reader holds, links to or was assigned, and records it at `raise_count`. An object area of another type without such a descendant has a support with no pair of type `τ`, so leaving it open leaves no pair the listing returns stale. A later raise increments `raise_count`, which makes the record stale and the share open again.

3. The bound is part of the claim. A listing settles at most 512 areas, the oldest, and a read that finds the reader's share lock held by another read of the same user settles none. The pairs it returns are current when the reader has at most 512 open areas it needs and no other read of the reader is settling; otherwise the areas left open settle on the next read or the drain, and the pairs they cover are read as they stand until then.

4. The share of an area is the slice of the change's support that can hold a pair of `u`, as defined above, with bindings changed since the area opened counted too, so settling it leaves every pair of `u` in the area current.

5. An area is deleted by `_clear_diff` only when every user `_diff_holders` returns has a record at the current `raise_count`, under `FOR UPDATE SKIP LOCKED` and a matching `raise_count`, so a raise that lands between the test and the delete keeps the row.

6. So each holder pays its own share when it reads, and a holder who never reads pays nothing.

**Sources.** Deferred, per query maintenance of materialized views: R. Chirkova and J. Yang, [Materialized Views](https://dsf.berkeley.edu/cs286/papers/mv-fntdb2012.pdf), Foundations and Trends in Databases 2012, on lazy and deferred maintenance.

## The drain

`drain_access_diff(p_limit_pairs)` settles shares ahead of their reads, outside any request, under the drain rule: holder `j` of area `a` is taken when its read rate is at least the area's raise rate,

```
r̂_j = 1 / (now − user_activity.last_used_at)      λ̂_a = raise_count / (now − access_diff.sa_created_at)      take when r̂_j ≥ λ̂_a
```

tested as `now − sa_created_at ≥ raise_count × (now − last_used_at)`. A holder with no `user_activity` row is never drained. Shares are taken most recently active user first, at most `p_limit_pairs` of them, each through `_settle_diff_share`, so a share a read is settling at the same moment is skipped. A drain holds the share locks of at most 64 users, and once it holds 64 it takes only further shares of those users. It stops once its per object locks reach half of `_object_lock_budget()`, 256, which leaves the next share half the budget before a crossing; the rest waits for the next drain. So one drain holds at most 64 share locks and, like a read, at most 512 per object locks. On a prophet it drains only the areas of its own system app and of apps with no consumer in `sync_consumer` in contact for 7 days; the areas of an app a disciple serves are drained on that disciple, so each settle is paid once. A disciple drains every area it holds. It returns the shares settled.

### Why the drain rule

Settling a holder's share ahead of its read is worth its cost only when the holder is likelier to read before the area is raised again than after. Take the reads of holder `j` as a Poisson process of rate `r_j` and the raises of area `a` as one of rate `λ_a`. A share settled ahead costs its recompute `R_j`. If the holder reads before the next raise the settle is used, and the read finds nothing open; if the area is raised first the share opens again and the settle is wasted. For independent exponential clocks the read comes first with probability `r_j / (r_j + λ_a)`, so the expected useful part of `R_j` is

```
R_j · r_j / (r_j + λ_a)  ≥  R_j / 2    exactly when    r_j ≥ λ_a
```

Below that most of the drain's work is thrown away, and the holder pays the same `R_j` on its own read, once. The estimators are rows claimius already holds, and the test above is the rule multiplied out. A holder with no `user_activity` row is never drained; its share waits for its own read.

**Sources.** The minimum of two exponential random variables: any text on Poisson processes, for example [the exponential distribution](https://en.wikipedia.org/wiki/Exponential_distribution#Distribution_of_the_minimum_of_exponential_random_variables).

## Rebuilding

`recompute_user_object` can be called to force a set of pairs. `rebuild_user_object` empties `user_object`, recomputes the pair of every holder on every object a live binding names directly, one recompute per distinct `(app, user, bound object)`, and rebuilds every user's `user_users` rows with `_refresh_user_users`. Descendants reached only through a cascade fill on the next settle that covers them.
