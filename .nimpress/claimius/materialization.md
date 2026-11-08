---
layout: doc
title: Materialization
description: The per pair access index user_object, the set based recompute every settle is made of with its read and write counts, how a write raises an area in access_diff keyed by its transaction and its root, how a check settles one pair by the pair's mark and a listing the reader's whole share from its cursor in one pass, how a pair entering or leaving raises a user_users_diff row that the reads of viewers fold, how the drain settles shares ahead of reads and trims the areas every active cursor has passed, and why every read returns current pairs.
tags: user_object, object_info, access_diff, user_users_diff, user_activity, settled_txid, cursor, mark, access_trimmed_txid, recompute_user_object, _build_grants_for_objects, reconcile_if_pending, _settle_pair, _settle_diff_share, _settle_user_users_diff, _settle_directory, drain_access_diff
lastUpdated: true
---

# Materialization

Claimius stores the access relation `A` of [the model](./index.md#the-access-of-a-pair) per pair in `user_object`, so a check reads one pair and a listing one index range. A write does not settle the users it affects. It settles its own actor on the rows it names and records what changed as one area row stamped with its transaction; each user then settles its own part of the areas when it reads, a check the one pair it asks about against the pair's mark, a listing its whole share against its cursor, in one pass. Why recomputing an area is exact is shown in [the model](./index.md#why-the-view-is-maintained-exactly), and why settling per pair and per reader returns current pairs in [Why a settle returns current pairs](#why-a-settle-returns-current-pairs).

The same holds one level up. A pair that enters or leaves `user_object` does not count itself into `user_users`; it raises one `user_users_diff` row while anyone views the directory, and the reads that show who sees whom fold the rows they need; see [The user_users fold](#the-user-users-fold). So every derived table claimius keeps is maintained the same way: a write records what changed with no lock and no fan out, and a read pays for the part it needs.

## The tables

| Table | One row per | Holds | Indexes |
|---|---|---|---|
| `user_object` | app, user and object with `A ≠ 0` | `sa_access`, `grants`, `scope`, `direct_grant`, `settled_txid` the pair's mark, `used_count`, `last_used_at` | primary key `(user_id, app_id, object_type, object_id) INCLUDE (sa_access)`; `idx_user_object_object_user (app_id, object_type, object_id, user_id)` |
| `object_info` | live registered object | owner, location, root, `sa_created_by`, `sa_owner_root_id`, `sa_name`, `sa_description`, `sa_link`, and `search_vector` generated from name and description | primary key `(object_type, object_id)`; `(sa_owner_id, sa_name, object_id)`; `(sa_location_id, sa_name, object_id)`; `idx_object_info_created_by (sa_created_by, sa_owner_root_id)`; GIN on `search_vector` |
| `access_diff` | open area | `kind` (`object` or `assignment`), the object with `cascade` and its owner `root_id`, or `claim_id` and `user_id` for an assignment, `txid` of the latest raise, `sa_created_at` | primary key; `(app_id, object_type, object_id) WHERE kind = 'object'`; `(app_id, root_id, txid)`; `(app_id, txid)`; `(app_id, user_id, txid) WHERE kind = 'assignment'` |
| `user_users_diff` | pair that entered or left `user_object` on an object that is not a claim while the app had a viewer, not yet folded into `user_users` | `user_id`, the object, `delta` of `+1` or `−1` | primary key `(app_id, object_type, object_id, id)`; `(app_id, user_id)` |
| `usage_diff` | counted use a flush recorded on a pair, not yet folded into the pair | `used_count`, `used_at` | primary key `(app_id, user_id, id)` |
| `user_activity` | user and app that used it | `settled_txid` the cursor, `settled_at`, `directory_at`, `last_used_at` | primary key `(user_id, app_id)`; `(app_id, directory_at) WHERE directory_at IS NOT NULL` |
| `prophet_state`, `disciple_state` | app | `access_trimmed_txid`, the trimmed position, beside the sync state | the app |

A disciple carries the same tables without `app_id`, keyed `(user_id, object_type, object_id)`, because it mirrors one app.

`grants` is the array of the surviving derivations of the pair, each `{claim_id, user_claim_id, claim_name, claim_created_at, starts_at, ends_at, access, scope, cascaded_from, composed_from, tree_type, level}`, the pair's why provenance from which a read picks its actor token without reading the grant or the claim; every write that can change one of those fields raises an area covering the pair. `scope` holds the field masks of [Redaction](./redaction.md). The per object fields live once in `object_info`, so a pair holds only what is per user. None of these tables is written by application code: access changes by changing grants and bindings.

`user_object` is stored with `fillfactor = 90`, which leaves room on each page for an update that changes only `grants`, `scope`, `direct_grant` or `settled_txid` to stay on its page as a heap only tuple and write no index entry; an update of `sa_access`, which the primary key includes, writes both entries again ([PostgreSQL, Heap Only Tuples](https://www.postgresql.org/docs/17/storage-hot.html)). `user_object`, `access_diff` and `user_users_diff` run autovacuum at a scale factor of 0.02 instead of 0.2, because all three turn over a small part of their rows on every settle.

### The cursor and the mark

Every area carries the `txid` of the transaction that raised it. A listing takes the snapshot `xmin` of its own transaction, `pg_snapshot_xmin(pg_current_snapshot())`, before it reads the areas: every transaction below it has committed or aborted, so every area with `txid` below it is visible to the listing, and an area a still running transaction raises carries a `txid` at or above it. The listing settles its share of every area with `txid` at or after its cursor and then writes `xmin` as its new cursor, `user_activity.settled_txid`, and as the mark of every pair it wrote, `user_object.settled_txid`. A check does the same for one pair against the pair's mark. An area raised by a transaction open during a settle has a `txid` at or above the cursor the settle wrote, so the next read meets it; nothing is ever passed over.

## Recomputing a share

`recompute_user_object(app, u, types, ids, cascades, xmin)` recomputes the pairs of one user on a set of roots and, where a root cascades, on every descendant reached from it, all types together in one statement over `object_info`, `_build_grants_for_objects`. Every settle is one call to it. It takes no lock of its own beyond the rows it writes, which are the user's own pairs:

| Step | Reads | Entries |
|---|---|---|
| The reader's grants and their claims, and the claims they link to | one range of `idx_user_claim_user_app`, a claim probe per grant, the links | `1 + 2C + L` |
| The chains of the roots, for the bindings above and for the roots one root covers | one range of `idx_inheritance_info_descendant` per root | `Σ_r (a_r + t_r)` |
| The area: the descendants of each cascading root in each tree it is not covered in by another cascading root | one range of `idx_inheritance_info_ancestor` per root and tree | `Σ_r |desc(r)|` |
| The objects: owner, location, owner root and creator | one `object_info` probe per object | `|Y|` |
| The bindings of the reader's claims on the objects and on the chains | one range of `idx_claim_object_claim_id` per claim, joined by hash | `n_u` |
| The bound nodes inside the area that are not cascading roots of their own | one range of `idx_inheritance_info_ancestor` per such node | `Σ_b |desc(b)|` |
| The other trees of objects pinned in them | one range of `idx_inheritance_info_descendant` per pinned object and tree | `Σ_x (a_x + t_x)`, 0 for organizations in one tree |
| The stored pairs, for the comparison | one `user_object` probe per object | `|Y|` |

```
per object of a share:  1 closure entry + 1 object_info row + 1 pair probe, and one descent per bound node inside the subtree
```

The derivations of every object come from the bindings on the object, on a root or on an ancestor of a root in the tree the object was reached through, on a bound node inside the area descended once, and on the object's own chains in its other trees, held directly or through one composition link. A root whose descent another cascading root covers in a tree is not descended again, and a bound node inside the area is descended once however many claims bind it; the targets and their derivations are then assembled by one `GROUP BY`, never by joining two derived sets, because PostgreSQL estimates a scan of a common table expression from the expression and a join of two of them then runs as a nested loop: written as a join, the settle of 16,000 pairs took 52 seconds, and the listing that settles them takes 0.92 seconds as it stands. Nothing in the count grows with `U`, `O` or the number of claims in the app; it grows with the reader's grants, the objects of the share and the bindings inside it, which keeps the model [below the reachability bound](./types.md#why-the-model-stays-below-the-reachability-bound). A pair with no deny and no scope settles as the OR of its grants in SQL; the others go through `_apply_denies`, `G × X` mask operations in memory. An object with no `object_info` row is not live and comes back with mask 0, like an object no derivation reaches, so the caller deletes the pair without a second descent.

Measured in the `settle_share` bench suite, run by `just bench`: the first listing after a grant on a node over 1,000 organizations at depth 1 reads 4,057 index entries on the prophet and 4,030 on a disciple, the settle's closure entry and `object_info` row per object and the two per row of the page of 1,001, and writes 1,001 pairs; the same share at depth 4 under five bindings on the chain reads 8,136 and 8,113, one more descent of the subtree per binding inside the chain and nothing else per object; the second listing reads 2,009 and 2,005, the page alone.

### What a changed pair writes

The pairs of the area that no derivation and no creation reaches, or whose object is gone, are deleted; the others are compared with the stored pair first and only the ones that differ, or whose mark is below the settle's `xmin`, are upserted, so a pair whose every column and mark is current writes nothing, fires no trigger and takes no lock. A pair that differs only in its mark stays on its page as a heap only update. An `ON CONFLICT ... DO UPDATE ... WHERE` alone would lock every pair it meets even where the `WHERE` leaves it as it is ([PostgreSQL, INSERT, ON CONFLICT](https://www.postgresql.org/docs/17/sql-insert.html#SQL-ON-CONFLICT)), so the comparison runs before the write.

| Change | Rows | Index entries |
|---|---|---|
| A pair inserted or deleted | 1 `user_object` row | 2 |
| A pair updated in `sa_access` | 1 row version | 2 |
| A pair updated in `grants`, `scope`, `direct_grant` or `settled_txid` only | 1 row version | 0 when it stays on its page |
| A pair inserted or deleted on `x`, not a claim, while the app has a viewer | 1 `user_users_diff` row, one range of the viewer index read per statement | 2 |
| Any pair written | one delete of `claim_graph_cache` per user of the statement | 0 or 1 row |

The `user_users_diff` row is written by a statement trigger over the pairs of the statement, one row per pair, with no lock and no read of the other users of `x`; see [Users](./users.md#why-the-count-is-exact). The claim graph cache is dropped by statement triggers over the transition tables of the pairs inserted, deleted, or updated in a column the graph shows. Before `d4c71ff` a settled pair wrote 19 index entries, one per index of the per object columns and the GIN postings of `grants` and the search vector; it now writes 2.

## Writes raise areas

An area is the smallest description of what a write changed, keyed on what changed and never on the users it affects, stamped with the raising transaction and the owner root of its object so a reader ranges the areas of its own roots and a check those of the object's root.

| Write | Settles in the write | Raises |
|---|---|---|
| Insert, owner or location change, or soft delete of a registered row | the actor's pair on the row | `object` on the row |
| Any write of an organization, a location or a parenthood row that is not a rename | the actor's pair on the row | `object` with `cascade` on the row |
| A soft delete of a subtree of `n_S` nodes | the actor's pair on the row | `object` with `cascade` on the row and one `object` per descendant whose closure rows went with it, `n_S` rows |
| A rename, `name` or `description` only | the actor's pair on the row, and the one `object_info` row | nothing |
| `claim`: name, mask, level, inherit flag or deletion | the actor's pair on the claim | `object` on every live binding of the claim, `cascade` where the binding inherits and the claim did before or after, and `object` on the claim itself: `n_c + 1` rows |
| `claim_object` on an object | the actor's pairs on the claim and on the bound object | `object` on the bound object, with `cascade` when the binding inherits before or after; on an update that moves it, on the old object as well |
| `claim_object` link | the actor's pairs on both claims | the same as a claim change on both claims: `n_A + n_B + 2` rows |
| `claim_object` scope binding | the actor's pair on the claim | nothing: no pair carries it |
| `user_claim` | the actor's pair on the claim | `assignment` for the assignee: one row per user and claim |

`_raise_object_diff` and `_raise_claim_diff` insert with a deterministic id, `composite_id('object', app, type, id)` or `composite_id('assignment', app, claim, user)`, `ON CONFLICT (id) DO UPDATE SET txid = pg_current_xact_id()`, keeping `cascade` once set. So `m` raises of one area before it is trimmed leave one row stamped with the latest raise: one probe and one row version per raise, whatever the area reaches. The actor is `_actor_user_for_row`, the user behind `sa_created_by`, one probe; claimius is always entered in the context of a user or a client, so it is known. Under `replay_mode` no actor is settled and only the areas are raised, with the applying transaction's `txid`, which is how a disciple and a mirroring prophet apply a page.

## A check settles one pair

`get_access`, `mutation_access`, `resolve_access` and `get_claims` for one object read the pair through `_settle_pair(app, u, τ, x)`, the point settle and the read in one pass:

1. It reads the pair and the object's `object_info` row, two probes, and takes the mark from the pair, or from the reader's cursor when the reader holds no pair, or `'0'`.

2. It reads the trimmed position of the app, one probe. A mark below it is covered: the areas it would have tested are gone.

3. Otherwise it reads the areas of the object's owner root with `txid` at or after the mark, one range of `idx_access_diff_root_txid`, at most 16 of them, and the reader's assignment areas since the mark, one range of `idx_access_diff_user`. With nothing in either range the pair is current: 7 entries in all on the prophet, with `check_user_active`, and 4 on a disciple, measured in `check_mark`, and 3 in the bench on analyzed tables where the planner reads the small tables sequentially.

4. With fewer than 16 areas it tests them in memory against the object and, for a cascade area, against the object's closure range, `a_x + t_x` entries: covered when an area sits on the object, or with cascade on an ancestor, or on the claim object `x` itself, or an assignment of the reader was raised. With 16 the range was cut, and it probes `idx_access_diff_object` once per node of the chain instead, `a_x + 1` probes.

5. When covered, it recomputes the one pair, `recompute_user_object` on one root without cascade, with the bindings found by one probe per node of the chain instead of the reader's whole binding set. When not covered and the pair's mark is below the snapshot `xmin`, it moves the mark to `xmin`, a heap only update: 15 entries on the prophet and 10 on a disciple after an unrelated raise in the same root, no recompute, measured in `check_mark`, and the next check reads the pair after its mark and nothing more.

A recompute that waits on a lock past `_settle_lock_timeout()` or is chosen as a deadlock victim leaves the pair as it stands for the next read and reports it, so `get_access` walks the ancestors through `get_direct_access` for that one answer; every other error raises from the check. On the `settle_granularity` test, a claim bound over a node with three descendants and granted to three holders, a check settles 1 pair, a listing settles the reader's 4, and the other holders stay untouched until they read.

## A listing settles the reader's share

Every listing, search and graph begins with `reconcile_if_pending(app, u)`. It takes the transaction advisory lock of the reader's share, one key per app and user, with `pg_try_advisory_xact_lock`; a read that finds it held by another read of the same user settles nothing and reads the pairs as they stand, which the other read is bringing current. Holding it, `_settle_diff_share(app, u)` settles the whole share in one pass:

1. It takes the snapshot `xmin`, reads the cursor and the trimmed position of the app, two probes.

2. A reader with no cursor, a reader whose cursor is below the trimmed position, and a reader whose pairs number fewer than twice the areas raised in the app since its cursor, counted up to twice the areas, take the full build: the roots are the objects bound by the claims the reader holds or links to, with cascade where the binding and the claim inherit, the reader's own pairs, the objects the reader created through one range of `idx_object_info_created_by` per grant, and the claims it holds; no area is read. A first listing after a grant is this: the reach, read once.

3. Every other reader takes the area path: one range of `idx_access_diff_root_txid` per root of its bindings, its claims and its creations, and one of `idx_access_diff_user` for its assignments, from the cursor. An object area is kept when the reader holds a pair on the node, the node is bound by a claim it holds or links to, the node is its own creation, or an ancestor of the node is bound with cascade by such a claim, the relevance test of one pair probe, one `object_info` probe and one closure range per area; a cascade area none of that holds for still contributes the reader's bound nodes inside it. An assignment area contributes the bindings of the claim and of the claims it links to, with the claim object.

4. `recompute_user_object` over those roots with `xmin`, then the cursor moves to `xmin` and `settled_at` to now, one `user_activity` row.

Then `_settle_usage_diff` folds the reader's usage rows into its pairs, see [Usage](./usage.md), and the read goes on. A share settle that waits on a lock past `_settle_lock_timeout()` or is chosen as a deadlock victim leaves the cursor where it was and the read takes the pairs as they stand; every other error raises to the caller: a settle that fails is never read as pairs that stand.

Measured in the bench on one prophet, fresh tables, as the suite runs it: one claim on a root over 16,000 new organizations, the holder's first listing lists 16,001 rows reading 64,048 index entries in 0.92 seconds; the second listing reads 32,005, two per row, in 0.10 seconds; a grant on a node over 10,000 organizations costs the grantee's first listing 40,035 entries and 0.64 seconds; the revoke of the claim over 16,000 organizations is settled by the holder's next listing, which deletes the 16,001 pairs reading 64,038 entries in 2.0 seconds with two viewers in the app raising the `user_users_diff` rows. On analyzed tables the first listing reads 64,046 in 0.83 seconds, 32,005 of `object_info` for the settle and the page, 16,013 of the reader's pair range for the page and 16,004 of the root's descent, the second 32,005 in 87 ms, and the grant's first listing 50,033 in 0.72 seconds, where the planner descended the node twice. At alpha0026 the same three listings read 1,577,903, 96,082 and 201,374 entries, the second one testing 16,000 open areas a holder who never read left behind.

### Locks a settle takes

A settle takes one advisory lock, the reader's share lock, and row locks on rows of its own, and never a lock per object or per area. Every advisory lock a transaction holds is an entry in the server's shared lock table of `max_locks_per_transaction × max_connections` entries, which a transaction that ran out of it would fail for every session on the server; a row lock is a mark in the row's own header and takes no entry, however many rows one transaction locks ([PostgreSQL, Row-Level Locks](https://www.postgresql.org/docs/17/explicit-locking.html#LOCKING-ROWS)).

| A settle | Takes | Held until | A writer waits on it |
|---|---|---|---|
| The reader's share | one advisory lock per app and user, tried, never waited for | the read's commit | never: no write takes it |
| The areas it reads | nothing: areas are read, never locked | | never: a raise writes its row and is done |
| The pairs it writes | the row locks of the reader's own pairs that changed or whose mark moved, none for a pair left as it was | the read's commit | only a write acting as the same user on a pair the read changed |
| Its cursor | the reader's own `user_activity` row | the read's commit | a usage flush skips a row a read holds |
| The `user_users_diff` rows it folds | `FOR UPDATE SKIP LOCKED` and their delete | the read's commit | never: a write only inserts new ones |
| The `user_users` rows it moves | the row locks of those rows | the read's commit | never: no write touches `user_users` |
| The `usage_diff` rows of the reader | `FOR UPDATE SKIP LOCKED` and their delete, and the reader's pairs they move | the read's commit | never: a flush only inserts new ones |

So the shared lock table holds one entry per reading transaction and at most 64 per drain, whatever the size of the share; a writer meets a read only on a pair of its own user; and nothing a read does depends on how many pairs it moves. Every row lock a settle waits for is bounded by `_settle_lock_timeout()`, a twentieth of `deadlock_timeout`: a settle that waits longer is given up and its unit stays for the next read.

Measured in the `listing_writer` suite, a writer beside a reader still in its transaction: one with 600 open areas that move nothing completed in 2.0 ms, one beside a reader settling 700 new pairs in 9 ms, and a usage flush on a pair the reader holds in 3.6 ms, where at `v2.0.0-alpha0022` the writer waited for the whole of the reader's transaction behind the lock every object shared.

### Why a settle returns current pairs

A check that settles only the pair it asks about, and a listing that settles only the reader's own share of the areas raised since its cursor, return the pairs a full recompute would:

1. A pair `(u, x)` can be stale only when an area's support contains it, by [the model](./index.md#why-the-view-is-maintained-exactly): an object area on `x` or with cascade on an ancestor of `x`, an area on a binding's object or on the claim object of a claim `u` holds or links to, or an assignment of `u`. Every change to a claim, a link or a binding raises object areas on the bound objects, so the support of every change is named by objects and by assignments, and nothing per held claim.

2. Every area carries the `txid` of its latest raise, and a settle takes its snapshot `xmin` before reading the areas, so every area with `txid` below `xmin` is visible to it, and an area raised by a transaction still open has `txid ≥ xmin`. The settle writes `xmin` as the mark of the pairs it recomputed and as the reader's cursor. So an area a settle did not see has `txid` at or above the mark or cursor it wrote, and the next read meets it.

3. `_settle_pair` tests exactly the areas of 1 raised at or after the pair's mark: those of the object's owner root, where every object area on `x` and on its ancestors lives, and the reader's assignments, and reads the closure for the cascade test. When any covers the pair it is recomputed, so the pair read is current; when none does, the mark moves to `xmin`, which records that every area below `xmin` was tested.

4. `_settle_diff_share` settles, from the cursor, every object area of the roots the reader can hold a pair in, kept by the relevance test or standing in for the reader's bound nodes inside it, and every assignment of the reader; an area in a root where the reader holds no binding, no pair and no creation has a support with no pair of the reader. A reader whose cursor is below the trimmed position, or who has no cursor, recomputes its whole reach from its bindings, pairs and creations, which is the support of every area it could have missed. So after the settle every pair of the reader is current and the cursor moves.

5. The trim deletes only areas with `txid` below the lowest cursor among the users used within 7 days and records that cursor as the trimmed position, so every active reader has settled them and every other reader is sent to the full build by its mark or cursor being below the position.

6. A settle that cannot run, the share lock held by another read of the same user or a lock wait past `_settle_lock_timeout()`, leaves the mark and the cursor where they were, and the read takes the pairs as they stand; the next read settles. Every other error raises from the read.

7. So each holder pays its own share when it reads, and a holder who never reads pays nothing, and leaves nothing open for the others.

**Sources.** Deferred, per query maintenance of materialized views: R. Chirkova and J. Yang, [Materialized Views](https://dsf.berkeley.edu/cs286/papers/mv-fntdb2012.pdf), Foundations and Trends in Databases 2012, on lazy and deferred maintenance. Snapshot visibility: [PostgreSQL, Transaction Isolation](https://www.postgresql.org/docs/17/transaction-iso.html).

## The user_users fold

`user_users` counts, for a viewer and a target of one app, the objects that are not claims they both hold a pair on; see [Users](./users.md#who-can-see-whom). It is kept for viewers, the users with `user_activity.directory_at` set, derived from `user_object` the way `user_object` is derived from the grants, and kept the same way: the write records what changed, and the read that needs the count settles it.

1. A user becomes a viewer on its first `get_users`, `get_user_access` as the actor or `get_claim_graph`, or its first after 30 days away: `_settle_directory(app, u)` deletes its rows and builds them from its pairs as the count over the pairs less the `user_users_diff` rows still pending on its objects, which it holds `FOR KEY SHARE` so no fold moves them meanwhile, `Σ_{x ∈ V_u} U_x` holder pairs once, then stamps `directory_at`, refreshed at most once a day by later reads. Measured in the bench for one of 200 holders over 2,000 organizations: 400,000 holder pairs through a hash join, 6,212 index entries and 2.0 seconds on fresh tables, 2,415 and 1.3 seconds on analyzed ones, then 7 entries and 10 ms a view.

2. A pair that enters or leaves `user_object` on an object that is not a claim raises one `user_users_diff` row, `(app, object, user, +1)` or `−1`, while the app has a viewer who read the directory within 30 days: `tg_raise_user_users_diff_insert` and `tg_raise_user_users_diff_delete` are statement triggers that write the rows of the whole statement in one insert from the transition table of the pairs, after one range of the viewer index. Nothing else is read and nothing is locked, under `replay_mode` as everywhere. An app with no viewer raises no row.

3. `_settle_user_users_diff(app, u)` folds the rows a read of `u` needs: those of every object `u` holds a pair on or has a row of its own on. It reads every row of those objects in one statement, takes them `FOR UPDATE SKIP LOCKED`, and folds an object only when it took every row of it the statement sees; an object with a row held by another fold stays for the next read. For each object folded it reads the users holding a pair on it, the users holding it before the rows are those less the rows' sum, and every ordered pair of users of which at least one moved changes by `now(a) now(b) − before(a) before(b)`, summed over the objects of the statement, written for the viewers among them only. The same statement deletes the rows it folded; the next adds the sums to `user_users` in key order and the last deletes the rows that reached zero. It waits on no writer: a write never touches a row it takes or a `user_users` row. A `user_users` row held by another fold is waited on for at most `_settle_lock_timeout()`, past which the whole fold is given up and its rows stay.

4. `_user_users_for_viewer(app, u)` answers the count of every user `u` sees as the statement's snapshot holds it: the stored `user_users` range of `u`, plus, for every object with a row still pending that `u` holds or moved on, the same `now(a) now(b) − before(a) before(b)` read from the pending rows. A fold that was given up, a row of a running transaction or of the reader's own, all count, so the answer equals a count taken with every pair committed at that snapshot whatever the folds do.

5. `get_users`, `get_claim_graph` and `get_graph_links_claim`, after `reconcile_if_pending`, make the reader a viewer, fold its rows and read through `_user_users_for_viewer`. `get_user_access` makes the actor a viewer and reads the one `user_users` row of the actor and the target by key with the pending rows of the two of them; see [Users](./users.md#acting-for-another-user). `get_claim_graph` serves its cache only when the fold left none of the reader's rows pending.

6. `drain_access_diff` folds the oldest objects of every app after its shares, at most `p_limit_pairs` of them. On a prophet it folds every app's rows, since derived rows never travel; a disciple folds the rows of its own pairs, which it derives from the feed like any of its pairs.

| Step | Reads | Writes |
|---|---|---|
| A pair entering or leaving with a viewer in the app | one range of the viewer index per statement | 1 `user_users_diff` row, 2 entries |
| A user's first directory view | `Σ_{x ∈ V_u} U_x` | the viewer's `S_u` rows |
| A fold of object `x` with `m_x` rows | the `m_x` rows and the `U_x` users of `x` | at most `2 m_x (U_x − 1)` `user_users` rows for the viewers among them, 3 entries each, and `m_x` deletes |
| A read of `u` with `T_u` objects pending | the `S_u` stored rows and `Σ_{x ∈ T_u} (m_x + U_x)` | nothing |

The work of a pair is the same `U_x` it was when the trigger counted it, paid once by the first read or drain that folds it instead of inside the write, and only while someone views the directory; a pair that enters and leaves before anyone folds it costs two rows and no count. The fold and the read aggregate per object and pair the users of an object from one array per object, never by joining two derived sets, because PostgreSQL estimates a scan of a common table expression from the expression and a join of two of them then runs as a nested loop: written that way, folding the 48,012 rows of one reader over 48,000 organizations took 274 seconds, and it takes 0.67 seconds as it stands, the read of the same rows pending 0.28 seconds. Why the fold is exact under any number of concurrent writers and folds, with no lock per object, is shown in [Users](./users.md#why-the-count-is-exact).

## The drain and the trim

`drain_access_diff(p_limit_pairs)` settles shares ahead of their reads, outside any request, under the drain rule: user `j` is taken when its read rate is at least the raise rate of the areas since its cursor,

```
r̂_j = 1 / (now − user_activity.last_used_at)      λ̂_j = n_j / (now − user_activity.settled_at)      take when r̂_j ≥ λ̂_j
```

with `n_j` the areas of the app with `txid` at or after `j`'s cursor, counted up to 1,000, tested as `now − settled_at ≥ n_j × (now − last_used_at)`. A user with no `user_activity` row is never drained. It takes the users whose cursor is below the newest area of their app, most recently used first, at most 64, each through `_settle_diff_share` under its own share lock, so a share a read is settling at the same moment is skipped; so one drain holds at most 64 entries of the shared lock table, whatever the shares move. On a prophet it drains only the users of its own system app and of apps with no consumer in `sync_consumer` in contact for 7 days; the areas of an app a disciple serves are drained on that disciple, so each settle is paid once. A disciple drains every user it holds.

Then it trims: per app, every area with `txid` below the lowest cursor among the users used within 7 days is deleted and that cursor recorded as `access_trimmed_txid` in `prophet_state` or `disciple_state`, so `access_diff` holds the areas since the oldest active cursor and nothing a reader who comes back later could miss: a mark or a cursor below the trimmed position sends that reader to the full build of its reach. Then it folds the `usage_diff` and `user_users_diff` rows of the oldest `p_limit_pairs` objects of every app; see [The user_users fold](#the-user-users-fold). It returns the shares settled. Measured in the `drain` suite: of three holders of one grant, the one who reads more often than areas are raised is settled ahead of its read, the one who reads less often and the one with no activity wait for their own reads, and the drain moves only the cursor of the share it settled.

### Why the drain rule

Settling a user's share ahead of its read is worth its cost only when the user is likelier to read before the next area is raised than after. Take the reads of user `j` as a Poisson process of rate `r_j` and the raises of areas in its app as one of rate `λ_j`. A share settled ahead costs its recompute `R_j`. If the user reads before the next raise the settle is used, and the read finds nothing since its cursor; if an area is raised first the share opens again and the settle is wasted. For independent exponential clocks the read comes first with probability `r_j / (r_j + λ_j)`, so the expected useful part of `R_j` is

```
R_j · r_j / (r_j + λ_j)  ≥  R_j / 2    exactly when    r_j ≥ λ_j
```

Below that most of the drain's work is thrown away, and the user pays the same `R_j` on its own read, once. The estimators are rows claimius already holds, the areas since the cursor over the time since the cursor moved, and the test above is the rule multiplied out.

**Sources.** The minimum of two exponential random variables: any text on Poisson processes, for example [the exponential distribution](https://en.wikipedia.org/wiki/Exponential_distribution#Distribution_of_the_minimum_of_exponential_random_variables).

## Rebuilding

`recompute_user_object` can be called to force a set of pairs. `rebuild_user_object` empties `user_object`, `user_users_diff` and `user_users` together, so whatever drift `user_users` carried is gone and the empty pairs, rows and counts agree, recomputes the pairs of every holder on every object a live binding names directly, one recompute per holder over its bound objects, each pair raising its `user_users_diff` row while the app has a viewer, and folds every row. Descendants reached only through a cascade fill on the next settle that covers them, which every reader's next listing is, its cursor being below the areas the rebuild raised.
