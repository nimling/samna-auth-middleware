---
layout: doc
title: Materialization
description: The per pair access index user_object, the recompute every settle is made of with its read and write counts, how a write raises an area in access_diff, how a check settles one pair and a listing the reader's whole share under one lock, how a pair entering or leaving raises a user_users_diff row that the reads of user_users fold, when an area clears, and the drain rule that settles shares ahead of reads.
tags: user_object, object_info, access_diff, access_diff_user, user_users_diff, user_activity, recompute_user_object, reconcile_if_pending, _settle_diff_share, _settle_object_diff, _settle_claim_diff, _settle_user_users_diff, _diff_holders, _clear_diff, drain_access_diff, raise_count, reconcile_queue
lastUpdated: true
---

# Materialization

Claimius stores the access relation `A` of [the model](./index.md#the-access-of-a-pair) per pair in `user_object`, so a check reads one pair and a listing one index range. A write does not settle the users it affects. It settles its own actor on the rows it names and records what changed as one area row; each user then settles its own part of the area when it reads, a check the one pair it asks about, a listing its share of the areas it needs. Why recomputing an area is exact is shown in [the model](./index.md#why-the-view-is-maintained-exactly), and why settling per pair and per reader returns current pairs in [Why a settle returns current pairs](#why-a-settle-returns-current-pairs).

The same holds one level up. A pair that enters or leaves `user_object` does not count itself into `user_users`; it raises one `user_users_diff` row, and the reads that show who sees whom fold the rows they need when they read; see [The user_users fold](#the-user-users-fold). So every derived table claimius keeps is maintained the same way: a write records what changed with no lock and no fan out, and a read pays for the part it needs.

## The tables

| Table | One row per | Holds | Indexes |
|---|---|---|---|
| `user_object` | app, user and object with `A ≠ 0` | `sa_access`, `grants`, `scope`, `direct_grant`, `used_count`, `last_used_at` | primary key `(user_id, app_id, object_type, object_id) INCLUDE (sa_access)`; `idx_user_object_object_user (app_id, object_type, object_id, user_id)` |
| `object_info` | live registered object | owner, location, root, `sa_name`, `sa_description`, `sa_link`, and `search_vector` generated from name and description | primary key `(object_type, object_id)`; `(sa_owner_id, sa_name, object_id)`; `(sa_location_id, sa_name, object_id)`; GIN on `search_vector` |
| `access_diff` | open area | `kind` (`object` or `claim`), the object or the claim, `user_id` for an assignment, `cascade`, `raise_count`, `sa_created_at` | primary key; `(app_id, object_type, object_id) WHERE kind = 'object'`; `(app_id, claim_id) WHERE kind = 'claim'`; `(app_id, user_id) WHERE kind = 'claim' AND user_id IS NOT NULL` |
| `access_diff_user` | area and user that settled its share | the `raise_count` settled at | primary key `(diff_id, user_id)`, removed with its area |
| `user_users_diff` | pair that entered or left `user_object` on an object that is not a claim, not yet folded into `user_users` | `user_id`, the object, `delta` of `+1` or `−1` | primary key `(app_id, object_type, object_id, id)`; `(app_id, user_id)` |
| `usage_diff` | counted use a flush recorded on a pair, not yet folded into the pair | `used_count`, `used_at` | primary key `(app_id, user_id, id)` |
| `user_activity` | user and app that used it | `last_used_at` | primary key `(user_id, app_id)` |
| `reconcile_queue` | app, user and object | a pair waiting for its user's next read | primary key; `(app_id, user_id)` |

A disciple carries the same tables without `app_id`, keyed `(user_id, object_type, object_id)`, because it mirrors one app.

`grants` is the array of the surviving derivations of the pair, each `{claim_id, access, scope, cascaded_from, composed_from, tree_type}`, the pair's why provenance from which a read picks its actor token. `scope` holds the field masks of [Redaction](./redaction.md). The per object fields live once in `object_info`, so a pair holds only what is per user. None of these tables is written by application code: access changes by changing grants and bindings.

`user_object` is stored with `fillfactor = 90`, which leaves room on each page for an update that changes only `grants`, `scope` or `direct_grant` to stay on its page as a heap only tuple and write no index entry; an update of `sa_access`, which the primary key includes, writes both entries again ([PostgreSQL, Heap Only Tuples](https://www.postgresql.org/docs/17/storage-hot.html)). `user_object`, `access_diff` and `user_users_diff` run autovacuum at a scale factor of 0.02 instead of 0.2, because all three turn over a small part of their rows on every settle.

## Recomputing a share

`recompute_user_object(app, u, τ, S)` recomputes the pairs of one user on a set `S` of objects of one type in one pass of set based statements. Every settle is made of calls to it, one per type of a share. It takes no lock of its own beyond the rows it writes, which are the user's own pairs:

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

The pairs of `S` that no derivation and no creation reaches, or whose object is gone, are deleted; the others are compared with the stored pair first and only the ones that differ are upserted, so a pair whose every column is unchanged writes nothing, fires no trigger and takes no lock. An `ON CONFLICT ... DO UPDATE ... WHERE` alone would lock every pair it meets even where the `WHERE` leaves it as it is ([PostgreSQL, INSERT, ON CONFLICT](https://www.postgresql.org/docs/17/sql-insert.html#SQL-ON-CONFLICT)), so a check that re-settled an unchanged pair, or a listing over thousands of them, held every one of those rows until it committed.

| Change | Rows | Index entries |
|---|---|---|
| A pair inserted or deleted | 1 `user_object` row | 2 |
| A pair updated in `sa_access` | 1 row version | 2 |
| A pair updated in `grants`, `scope` or `direct_grant` only | 1 row version | 0 when it stays on its page |
| A pair on an object with no `object_info` row yet | 1 `object_info` row, `ON CONFLICT DO NOTHING` | 3 btree entries and one GIN entry per lexeme |
| A pair inserted or deleted on `x`, not a claim | 1 `user_users_diff` row, nothing read | 2 |
| Any pair written | one delete probe of `claim_graph_cache` for its user | 0 or 1 row |

The `user_users_diff` row is written by a statement trigger over the pairs of the statement, one row per pair, with no lock and no read of the other users of `x`; see [Users](./users.md#why-the-count-is-exact). Before it, the trigger counted `user_users` in place, read the `U_x` users of every object and held a per object advisory lock until commit. Before `d4c71ff` a settled pair wrote 19 index entries, one per index of the per object columns and the GIN postings of `grants` and the search vector; it now writes 2.

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

Every listing, search and graph begins with `reconcile_if_pending(app, u)`, and `get_objects`, `get_organizations` and `get_locations` pass the type they list, `reconcile_if_pending(app, u, τ)`. It first takes the transaction advisory lock of the reader's whole share, one key per app and user, with `pg_try_advisory_xact_lock`; a read that finds it held by another read of the same user settles nothing and reads the pairs as they stand. Holding it, it reads the app's open areas `Δ_a` the read needs: with `τ` the `object` areas on an object of type `τ` or with `cascade` on an ancestor of an object of type `τ`, one closure probe per such area, and without `τ` every `object` area; then every `claim` area with `user_id = u`, and every other `claim` area of a claim `u` holds or links to. Each is tested against `access_diff_user` at its `raise_count`, and every one open is taken, oldest first by `sa_created_at`, `FOR KEY SHARE SKIP LOCKED`, however many there are, and handed to `_settle_diff_share` in batches of 128, every batch settled:

1. It takes the areas of the batch `FOR KEY SHARE SKIP LOCKED`, which a writer raising an area again does not wait on, and the share lock of the reader, which the read already holds, so two reads of one user never settle at once and a share lock is one lock however many areas are settled under it; an area held by another settle is skipped and read as it stands.

2. It settles every object area of the batch in one `_settle_object_diff`, set based: one `recompute_user_object` per object type for the batch. Each claim area settles in its own `_settle_claim_diff`.

3. It records `(area, u, raise_count)` in `access_diff_user` for every area of the batch in one statement.

4. It calls `_clear_diff` once for the batch, which deletes the areas every user of `_diff_holders` has a record for at the current `raise_count`, under `FOR UPDATE SKIP LOCKED`, the raise count compared on the row it locks, so a raise between the test and the delete keeps the row. `_diff_holders` takes the batch and reads the creators of its objects once per object type.

5. A batch that waits on a lock past `_settle_lock_timeout()` or fails otherwise is settled again one area at a time, so an area that cannot settle leaves the others settled; that area stays open and the read goes on.

The fixed cost of a recompute, its plan and its walk of the reader's claims, is paid once per object type and batch instead of once per area, and the walk to inherited grants starts at the objects recomputed and reaches their ancestors through the descendant index, so it costs the objects of the batch and not the size of the tree above them. Measured on one reader with one claim bound on a root over 16,000 new child organizations, 16,007 open areas: the first listing took about 2 minutes settling one area at a time, and 3.8 seconds in 126 batches while one read could still take every object at once. At `v2.0.0-alpha0023`, where a read stopped at 512 moved pairs, it took 32 listings, 14.5 seconds in all, the first returning 513 of the 16,001 organizations the reader reaches; one listing now returns all 16,001 in 8.1 seconds right after the inserts and in 1.9 seconds once autovacuum has analyzed the tables, against 32 listings and 7.0 seconds at `v2.0.0-alpha0023` on the same warm tables.

It then recomputes every `reconcile_queue` row of the reader. Nothing in the current sets writes to `reconcile_queue`; rows it holds drain on their user's next read.

### Locks a settle takes

A settle takes one advisory lock, the reader's share lock, and row locks on rows of its own, and never a lock per object. Every advisory lock a transaction holds is an entry in the server's shared lock table of `max_locks_per_transaction × max_connections` entries, which a transaction that ran out of it would fail for every session on the server; a row lock is a mark in the row's own header and takes no entry, however many rows one transaction locks ([PostgreSQL, Row-Level Locks](https://www.postgresql.org/docs/17/explicit-locking.html#LOCKING-ROWS)).

| A settle | Takes | Held until | A writer waits on it |
|---|---|---|---|
| The reader's share | one advisory lock per app and user, tried, never waited for | the read's commit | never: no write takes it |
| The areas it settles | `FOR KEY SHARE SKIP LOCKED` on each | the read's commit | never: a raise takes `FOR NO KEY UPDATE`, which does not conflict |
| The pairs it writes | the row locks of the reader's own pairs that changed, none for a pair it leaves as it was | the read's commit | only a write acting as the same user on a pair the read changed |
| Its `access_diff_user` records | row locks on the reader's own records | the read's commit | never: no write touches them |
| An area it clears | the delete of the area row | the read's commit | a raise of that same area waits for the read to end |
| The `user_users_diff` rows it folds | `FOR UPDATE SKIP LOCKED` and their delete | the read's commit | never: a write only inserts new ones |
| The `user_users` rows it moves | the row locks of those rows | the read's commit | never: no write touches `user_users` |
| The `usage_diff` rows of the reader | `FOR UPDATE SKIP LOCKED` and their delete, and the reader's pairs they move | the read's commit | never: a flush only inserts new ones |

So the shared lock table holds one entry per reading transaction and at most 64 per drain, whatever the size of the share; a writer meets a read only on a pair of its own user or on an area the read just cleared; and nothing a read does depends on how many pairs it moves. The per object advisory locks, the 512 lock budget, the lock every object shared and the read that stopped at the budget existed only so that `user_users` could be counted inside the write that moved a pair, which needed every other holder of the object at that moment; with the count folded on read nothing needs them, and they are gone. Every row lock a settle waits for is bounded by `_settle_lock_timeout()`, a twentieth of `deadlock_timeout`: a unit that waits longer is given up and stays open.

Measured in the `listing_writer` suite, a writer beside a reader still in its transaction, one with 600 open areas that move nothing and one with 700 new pairs: the writer waited for the whole of the reader's transaction behind the lock every object shared at `v2.0.0-alpha0022`, completed in 2 to 3 ms at `v2.0.0-alpha0023`, while the reader of 700 pairs needed two reads, and completes in 2 to 3 ms now, while the reader settles all 700 in its one read. A usage flush on a pair the reader holds waited 3,005 ms for the reader at `v2.0.0-alpha0023` and takes 3 ms now; see [Usage](./usage.md).

With `A_u` the reader's open areas that a read needs and `M_u` the pairs they move:

```
advisory locks  = 1
areas settled   = A_u, every one, oldest first, in ⌈A_u / 128⌉ batches
row locks       = the M_u pairs of the reader, its records, and the user_users_diff and user_users rows of the objects it folds
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

3. A listing settles every area it needs, however many are open and however many pairs they move, in one read, so a revoke among thousands of open areas is settled before the listing reads its pairs and the listing never returns an object the reader no longer reaches. Nothing in a settle depends on the number of pairs it moves: it takes no lock per object, and the row locks it takes are marks in the rows themselves, see [Locks a settle takes](#locks-a-settle-takes). The one exception is a read that finds the reader's share lock held by another read of the same user: it settles none and reads the pairs as they stand, which the other read is bringing current. The other is a settle that waits on a row longer than `_settle_lock_timeout()`, a twentieth of `deadlock_timeout`, which only a transaction writing one of the reader's own pairs can make it do: it gives up that area, which stays open, and the listing reads its pairs as they stand. That transaction commits within its own span, and the next read settles the area.

4. The share of an area is the slice of the change's support that can hold a pair of `u`, as defined above, with bindings changed since the area opened counted too, so settling it leaves every pair of `u` in the area current.

5. An area is deleted by `_clear_diff` only when every user `_diff_holders` returns has a record at the current `raise_count`, under `FOR UPDATE SKIP LOCKED` and a matching `raise_count`, so a raise that lands between the test and the delete keeps the row.

6. So each holder pays its own share when it reads, and a holder who never reads pays nothing.

**Sources.** Deferred, per query maintenance of materialized views: R. Chirkova and J. Yang, [Materialized Views](https://dsf.berkeley.edu/cs286/papers/mv-fntdb2012.pdf), Foundations and Trends in Databases 2012, on lazy and deferred maintenance.

## The user_users fold

`user_users` counts, for two users of one app, the objects that are not claims they both hold a pair on; see [Users](./users.md#who-can-see-whom). It is derived from `user_object` the way `user_object` is derived from the grants, and it is kept the same way: the write records what changed, and the read that needs the count settles it.

1. A pair that enters or leaves `user_object` on an object that is not a claim raises one `user_users_diff` row, `(app, object, user, +1)` or `−1`. `tg_raise_user_users_diff_insert` and `tg_raise_user_users_diff_delete` are statement triggers that write the rows of the whole statement in one insert, from the transition table of the pairs it inserted or deleted. Nothing else is read and nothing is locked, under `replay_mode` as everywhere, so the bulk seed and a rebuild raise their rows like any write.

2. `_settle_user_users_diff(app, u)` folds the rows a read of `u` needs: those of every object `u` holds a pair on or has a row of its own on. It reads every row of those objects in one statement, takes them `FOR UPDATE SKIP LOCKED`, and folds an object only when it took every row of it the statement sees; an object with a row held by another fold stays for the next read. For each object folded it reads the users holding a pair on it, the users holding it before the rows are those less the rows' sum, and every ordered pair of users of which at least one moved changes by `now(a) now(b) − before(a) before(b)`, summed over the objects of the statement. The same statement deletes the rows it folded; the next adds the sums to `user_users` in key order and the last deletes the rows that reached zero. It waits on no writer: a write never touches a row it takes or a `user_users` row. A `user_users` row held by another fold is waited on for at most `_settle_lock_timeout()`, past which the whole fold is given up and its rows stay.

3. `_user_users_for_viewer(app, u)` answers the count of every user `u` sees as the statement's snapshot holds it: the stored `user_users` range of `u`, plus, for every object with a row still pending that `u` holds or moved on, the same `now(a) now(b) − before(a) before(b)` read from the pending rows. A fold that was given up, a row of a running transaction or of the reader's own, all count, so the answer equals a count taken with every pair committed at that snapshot whatever the folds do.

4. `get_users`, `get_user_access`, `get_claim_graph` and `get_graph_links_claim`, after `reconcile_if_pending`, fold the reader's rows and read through `_user_users_for_viewer`. `get_claim_graph` serves its cache only when the fold left none of the reader's rows pending, since a pending row changes the users the graph shows without a `user_users` row moving.

5. `drain_access_diff` folds the oldest objects of every app after its shares, at most `p_limit_pairs` of them. On a prophet it folds every app's rows, since derived rows never travel and no other database holds them; a disciple folds the rows of its own pairs, which it derives from the feed like any of its pairs.

| Step | Reads | Writes |
|---|---|---|
| A pair entering or leaving | nothing | 1 `user_users_diff` row, 2 entries |
| A fold of object `x` with `m_x` rows | the `m_x` rows and the `U_x` users of `x` | at most `2 m_x (U_x − 1)` `user_users` rows, 3 entries each, and `m_x` deletes |
| A read of `u` with `T_u` objects pending | the `S_u` stored rows and `Σ_{x ∈ T_u} (m_x + U_x)` | nothing |

The work of a pair is the same `U_x` it was when the trigger counted it, paid once by the first read or drain that folds it instead of inside the write, and a pair that enters and leaves before anyone folds it costs two rows and no count. The fold and the read aggregate per object and pair the users of an object from one array per object, never by joining two derived sets, because PostgreSQL estimates a scan of a common table expression from the expression and a join of two of them then runs as a nested loop: written that way, folding the 48,012 rows of one reader over 48,000 organizations took 274 seconds, and it takes 0.67 seconds as it stands, the read of the same rows pending 0.28 seconds. Why the fold is exact under any number of concurrent writers and folds, with no lock per object, is shown in [Users](./users.md#why-the-count-is-exact).

## The drain

`drain_access_diff(p_limit_pairs)` settles shares ahead of their reads, outside any request, under the drain rule: holder `j` of area `a` is taken when its read rate is at least the area's raise rate,

```
r̂_j = 1 / (now − user_activity.last_used_at)      λ̂_a = raise_count / (now − access_diff.sa_created_at)      take when r̂_j ≥ λ̂_a
```

tested as `now − sa_created_at ≥ raise_count × (now − last_used_at)`. A holder with no `user_activity` row is never drained. Shares are taken most recently active user first, at most `p_limit_pairs` of them, each through `_settle_diff_share`, so a share a read is settling at the same moment is skipped. A drain holds the share locks of at most 64 users, and once it holds 64 it takes only further shares of those users, so one drain holds at most 64 entries of the shared lock table, whatever the shares move. On a prophet it drains only the areas of its own system app and of apps with no consumer in `sync_consumer` in contact for 7 days; the areas of an app a disciple serves are drained on that disciple, so each settle is paid once. A disciple drains every area it holds. It then folds the `user_users_diff` rows of the oldest `p_limit_pairs` objects, of every app; see [The user_users fold](#the-user-users-fold). It returns the shares settled.

### Why the drain rule

Settling a holder's share ahead of its read is worth its cost only when the holder is likelier to read before the area is raised again than after. Take the reads of holder `j` as a Poisson process of rate `r_j` and the raises of area `a` as one of rate `λ_a`. A share settled ahead costs its recompute `R_j`. If the holder reads before the next raise the settle is used, and the read finds nothing open; if the area is raised first the share opens again and the settle is wasted. For independent exponential clocks the read comes first with probability `r_j / (r_j + λ_a)`, so the expected useful part of `R_j` is

```
R_j · r_j / (r_j + λ_a)  ≥  R_j / 2    exactly when    r_j ≥ λ_a
```

Below that most of the drain's work is thrown away, and the holder pays the same `R_j` on its own read, once. The estimators are rows claimius already holds, and the test above is the rule multiplied out. A holder with no `user_activity` row is never drained; its share waits for its own read.

**Sources.** The minimum of two exponential random variables: any text on Poisson processes, for example [the exponential distribution](https://en.wikipedia.org/wiki/Exponential_distribution#Distribution_of_the_minimum_of_exponential_random_variables).

## Rebuilding

`recompute_user_object` can be called to force a set of pairs. `rebuild_user_object` empties `user_object`, `user_users_diff` and `user_users` together, so whatever drift `user_users` carried is gone and the empty pairs, rows and counts agree, recomputes the pair of every holder on every object a live binding names directly, one recompute per distinct `(app, user, bound object)`, each raising its `user_users_diff` row, and folds every row. Descendants reached only through a cascade fill on the next settle that covers them.
