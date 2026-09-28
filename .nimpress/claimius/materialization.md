---
layout: doc
title: Materialization
description: The per user access index user_object, the per object row object_info, how a write records an area in access_diff, how each reader settles its own slice of it, what each step costs, and why claimius is built this way.
tags: user_object, object_info, user_users, access_diff, reconcile_if_pending, settle, slice, recompute_user_object, on demand
lastUpdated: true
---

# Materialization

Claimius stores the access relation of [Types](./types.md) per user. A check is then one primary key probe and a listing one index range, whatever the shape of the grants behind it. The work of keeping that index true is split in two: a write records what it changed and settles nothing it did not write; the reader who needs the change settles its own part of it.

## The tables

| Table | One row per | Holds |
|---|---|---|
| `user_object` | user, app, object | `sa_access`, `grants`, `scope`, `direct_grant`, and the usage counters `used_count`, `last_used_at` |
| `object_info` | object | What every user sees the same: `sa_owner_id`, `sa_location_id`, `sa_root_id`, `sa_name`, `sa_description`, `sa_link`, `search_vector` |
| `user_users` | app, viewer, target | That two users share an object, `sharing_object_count`, first and last time |
| `access_diff` | app and area, and a reader's marker on it | An area whose rows no longer reflect their inputs, and which readers have settled their slice of it |
| `user_activity` | user, app | `last_used_at`, the latest use the usage flush saw, which orders the drain |

The primary key of `user_object` is `(user_id, app_id, object_type, object_id) INCLUDE (sa_access)`. A check probes it with all four values, a listing reads the reader's range of it, and the mask test of both reads the included column. `user_object_id` in an answer is `composite_id(app_id, user_id, object_type, object_id)`, computed, not stored. `idx_user_object_object (object_id, object_type)` is the one other index, the members of an object.

`object_info` is keyed by `(object_type, object_id)` and written by the calc trigger in the transaction of the object's own write. Its search vector is under a GIN index, one entry per object.

`grants` is the array of `{claim_id, access, scope, cascaded_from, tree_type}` that produced the row, the provenance a check turns into an actor token.

None of these is written by application code. Access changes by changing grants.

### Why the pair is narrow

Everything a row holds is paid once per user who reaches the object, `Ū_o` times, where `Ū_o = Σ_u V_u / O` is the users an object is reached by, 19 in the benchmark. A column belongs on the pair only if it differs between users; the name, the text, the owner and the location do not, so they are stored once, in `object_info`. The inequality is `Ū_o > 1`, which holds for any object two people can see.

| | Pair carrying object fields, two tables | Narrow pair, `object_info` |
|---|---|---|
| Bytes per pair, heap and index | about 1 kB | about 350 B |
| Heap rows per settled pair | 2 | 1 |
| Index entries per settled pair | 8 to 9 | 2 |
| A rename | an area over `Ū_o` pairs | one row |
| Pairs the cache holds, 6 GB | 6 million | 17 million |

The usage counters sit on the pair because they differ between users, and on no index, so a usage flush is a heap only update; see [Usage](./usage.md). The pair ordered by the reader makes one user's slice of `n` pairs `⌈n / 87⌉` leaf pages, where a hashed key would scatter it over `n`; see [Scaling](./scaling.md).

## Writes record areas

An area is the smallest description of what a write changed, keyed on what changed and never on the users it affects.

| Write | Settles synchronously | Records in `access_diff` |
|---|---|---|
| Insert, update or soft delete of a registered row | The actor's pair for the row | `kind = 'object'` on the row |
| The same on an organization, location or parenthood row | The actor's pair for the row | `kind = 'object'` with `cascade` on the row, carrying its subtree |
| An update that changes neither owner, location nor soft delete | Nothing; a rename writes `object_info` | Nothing |
| `claim`, `claim_object` | Nothing | `kind = 'claim'` on the claim, and on the linked claim for a composition link |
| `user_claim` | Nothing | `kind = 'claim'` on the claim with `user_id` the assignee: the assignee's slice |

The id of an area is deterministic on the area, `composite_id('object', app, type, id)`, `composite_id('claim', app, claim)` or `composite_id('claim', app, claim, user)`, so repeated changes collapse into one outstanding row with `ON CONFLICT DO UPDATE`, which moves `sa_updated_at`. Every write is constant in the number of users and in the reach of the claim it touches.

## Reads settle their own slice

Every `get_*` computes the ids of the areas it targets and probes `access_diff` once with them.

1. A single object read, `get_access`, `get_direct_access`, `mutation_access`, `resolve_access`, targets the object areas of the object and of its ancestors, found through `idx_inheritance_info_descendant`, and the claim areas of the claims the reader holds, whole or sliced to the reader.

2. A listing of one type, `get_objects`, `get_organizations`, `get_locations`, targets the object areas of that type and the reader's claim areas.

3. `search` and the graphs target the reader's claim areas and the object areas under the reader's anchors.

The probe is one `= ANY` scan of the `access_diff` primary key with `2C + D + 1` ids for `C` grants and depth `D`, the whole area and the reader's slice of it for each claim, and one per ancestor: a one level index, measured 0.30 ms for the older two step form with 771 areas outstanding in the app. When it finds nothing, and that is the common case, the read answers in the same statement. When it finds both the whole area of a claim and the reader's slice of it, one settle covers both and writes the marker of each.

When it finds an area, the reader settles its own slice of it and nothing else: its pairs for the objects of the area. It records that in a marker, an `access_diff` row with `kind` of the area, `user_id` the reader, id `composite_id(area id, user)` and `sa_updated_at` copied from the area as it read it. A later read finds the marker at the same `sa_updated_at` and settles nothing; a write that raised the area again moved `sa_updated_at`, so the next read settles again.

A single object read settles less than its slice. `get_access`, `get_direct_access`, `mutation_access` and `resolve_access` answer one pair, so when they meet an area they settle the reader's pair for the target object alone, from the object's `D + 1` ancestors and the reader's `C` grants, and write no marker: the slice stays outstanding for the reader's next listing or the drain. The cost is `t + (C + D + 1) t_d` whatever the width of the area, where settling the slice would cost `A_u t`: for a reader under a new binding over 20,000 objects, about 0.1 ms on a `D2ds_v5` instead of 0.69 s. The settle of the slice is `ON CONFLICT ... WHERE ... IS DISTINCT FROM`, so the pair settled first is not written again.

1. Two readers of one area settle their own slices in parallel; they share no row.

2. Two reads by the same user of the same area meet on the marker's primary key: the second waits for the first to commit, finds the marker and reads. One settle per slice, however many requests arrive together. This is request coalescing, the lock table Zanzibar puts in front of its cache to keep a hot key from being computed twice ([Pang et al. 2019, section 3.2.5](https://www.usenix.org/system/files/atc19-pang.pdf)).

3. The marker records the `sa_updated_at` read at the start of the settle, never the time it ends, so a change committed during a settle under read committed is caught by the next read.

The drain settles the slices no read has reached, off the request path, on the database that serves the reads of the app. A disciple drains between pulls. A prophet drains, on the scheduled run that also calls `trim_sync_event`, the apps it serves reads for: its own apps, and any app with no consumer in contact in `prophet_state`. An app whose disciple is in contact is read on the disciple, so its areas on the prophet are settled only on demand, by the check of a routed write reaching the actor's slice. The drain takes outstanding areas with `FOR UPDATE SKIP LOCKED`, orders their slices by the latest use of the holder in `user_activity`, most recent first, settles them set based with `_settle_object_diff` and `_settle_claim_diff` in transactions of about 1,000 pairs, and deletes the area and its markers when the area's `sa_updated_at` is still the one it read. Why each of these, in numbers, is under [The settle as a queue](#the-settle-as-a-queue).

## Settling is set based

`_settle_object_diff` and `_settle_claim_diff` compute every `(user, object)` row of a slice in one statement: the holders of the area's claims, the reader alone on the read path, joined to the area's objects through `inheritance_info`, grants aggregated per pair, denies subtracted by the formula of [Denials](./denials.md), and one `INSERT ... ON CONFLICT (user_id, app_id, object_type, object_id) DO UPDATE ... WHERE ... IS DISTINCT FROM` into `user_object`. Rows whose access vanished are deleted in the same statement. The update sets the access columns only and never the usage counters. The `user_users` increments of the pairs that entered and left are aggregated per `(viewer, target)` in the same statement before they are written; see [Users](./users.md).

Measured with the pair carrying object fields: one statement settled 27,358 pairs in 522 ms, 0.019 ms per pair. Dynamic SQL on the registered table, the `table_info` lookups and the grant aggregation are paid once per slice, not once per pair. The narrow pair writes three heap and index entries where that measurement wrote eleven, so `t` falls toward 6 µs; the tables below keep the measured 0.019 ms.

## What an area costs

With `t = 0.019 ms` per settled pair, a reader's slice of `A_u` pairs costs `A_u × t` on the reader's request, and an area of `U_a` holders over `A` objects costs `U_a × A × t` in all, paid by the readers who read and the drain for the rest.

| Area | Reader's slice `A_u` | On the reader's first listing | In all |
|---|---|---|---|
| One new row under a leaf, 18 users reach it | 1 | 0.02 ms | 0.3 ms |
| A binding change on a leaf claim, 6 holders, 33 objects | 33 | 0.6 ms | 3.8 ms |
| A binding change on a root claim, 2 holders, 20,782 objects | 20,782 | 0.39 s | 0.8 s |
| The same at 10,000 users, 1,000,000 objects, 20 root holders | 1,000,000 | 19 s | 380 s, in the drain |

A settle statement runs under `statement_timeout`, 120 s, which bounds one statement at `120 s × σ / t` pairs, 6.3 million on the calibration core and 3.5 million on a `D2ds_v5`. The drain settles a large area in slices by holder, each slice its own statement and transaction. A reader whose own slice exceeds what a request should wait for, a holder of a root binding over a million objects, is the case the drain exists for: it runs within seconds of the write, so the reader of a wide area finds its slice settled.

## The settle as a queue

The settle is a queue of pairs. Writes put pairs in; readers and the drain take them out. The constants and the machines are those of [Scaling](./scaling.md): `K` the capacity of the database in calibration cores, `σ` the speed of one of its threads, `L_c` its commit latency.

### Arrival

The API first write, a claim bound to a node of `A` objects and granted to `k` users, arrives at `λ_w` and leaves `k A` pairs to settle on each database that serves the app's reads: the disciple, and the prophet only for an app it serves reads for. Every other write leaves fewer: a registered row one pair per user who reaches it, a grant `A_c` pairs for its assignee. The work arriving is `λ_w k Ā t` calibration seconds a second.

### Service and stability

A reader takes its own slice, `A_u t / σ`, on its request; the drain takes the rest at the share `φ` of `K` that the reads leave. Whoever takes a pair, the database spends `t` on it, so the queue is stable while

```
X_r c_r + λ_w (c_w + k Ā t)  ≤  ρ K
```

On a `D2ds_v5` with no other load that is 26 API first writes a second at `k = 3`, `Ā = 300`, `t = 19 µs`, and 73 at the narrow pair's 6 µs; on the reference node 1,490 and 4,120. The write itself is 7 % of the sum at 19 µs: the settle is the cost of a write, and `t` is the constant worth lowering.

### A burst

`B` writes arriving together leave `Q_0 = B k Ā` pairs, drained in

```
T_d = B k Ā t / (φ K)
```

For a hundred such writes, 90,000 pairs, at `φ = 0.5`: 12.7 s on a sustained `B2ms`, 5.0 s on a `D2ds_v5`, 0.11 s on the reference node, and a third of each at `t = 6 µs`. A holder is fresh at the earlier of its own first read and the moment the drain reaches its slice. A holder whose first read is a check pays one pair; one whose first read is a listing pays its slice, `A_u t / σ`, 10 ms for 300 objects on a `D2ds_v5`, and the drain then skips that slice, because the marker is already there.

### Why on demand is the right shape

1. The writer's latency and lock time are constant, `c_w / σ + L_c`, where settling in the write costs `k A t / σ` more with every settled pair locked to commit: 31 ms more at `k = 3`, `A = 300` on a `D2ds_v5`, 2.1 s over 20,000 objects.

2. A check waits for one pair and a listing for the reader's own slice, never the area: `1 ≤ A_u ≤ A < U_a A` for any area with two holders.

3. Repeated raises collapse. An area raised `m` times before it is settled is settled once, because its id is deterministic and the raise is an `ON CONFLICT` update. Settling in the write would pay `m` times.

4. The total is the same as settling eagerly, `U_a A t`, and never more, so none of the above is bought with extra work.

### Which slice the drain takes first

A slice `j` of processing time `p_j = A_j t` whose holder reads at rate `r_j` and is drained at time `C_j` costs that holder an expected wait of about `r_j C_j p_j`: the chance the read comes first, times the settle it then pays. The total expected wait `Σ r_j p_j C_j` is a weighted completion time, and it is least when slices are taken in decreasing order of `(r_j p_j) / p_j = r_j` ([Smith's rule, weighted shortest processing time](https://en.wikipedia.org/wiki/Single-machine_scheduling#Minimizing_the_sum_of_weighted_completion_times)). The width of the slice cancels. The read rate of a holder is estimated by its latest use, so the drain takes the slices of the most recently active holders first, from `user_activity`, one row per user and app holding the latest `last_used_at` the usage flush wrote for that user. Holders who have not been seen for a long time are drained last, when nothing else waits.

### The batch

A drain transaction pays `t_s / σ + L_c` of overhead and holds the locks of its pairs for `b t / σ`. The overhead stays under a share `ε` of the work and the lock time under `τ_lock` when

```
(t_s / σ + L_c) / (ε t / σ)  ≤  b  ≤  τ_lock σ / t
```

At `ε = 5 %` and `τ_lock = 50 ms`, a `D2ds_v5` gives `1,220 ≤ b ≤ 1,450` and the reference node `105 ≤ b ≤ 2,100`. A batch of 1,000 pairs is within both, a larger slice is split by object range into batches of that size, and a reader that meets a batch in progress waits at most `τ_lock` on the marker.

### The cadence and the size of `access_diff`

A drain run that finds nothing costs one statement, `t_s / σ`, about 0.1 ms: run every second it is 0.01 % of a core, so the prophet runs it every second and a disciple after every page. Outstanding rows are bounded by the distinct areas, at most one per claim, one per claim and holder, and one per object, so `access_diff` is bounded by the source rows and never by time; a slice left for a long absent holder costs one row of about 100 bytes and nothing on any read, because the probe descends a one level index whatever its size.

## What the index costs to hold

`user_object` holds `Σ_u V_u` rows for users `u` reaching `V_u` objects each. At about 350 bytes a pair with its two indexes, 10,000 users each reaching 2,000 objects is 20 million rows and about 7 GB; `object_info` holds one row per object, a few hundred bytes each. This is the price of a check being one probe, and it is the trade claimius makes: storage grows with users times reach, every read is an index lookup, and no read ever evaluates a tree. When the index outgrows memory the ceiling moves from CPU to IOPS; see [Scaling](./scaling.md).

## Why claimius is built this way

1. Write time fan out was the first design: every write recomputed `user_object` for every user it touched. It made writes scale with the number of users. On 2026-07-01 a `POST claim` took 17 s, and the user relayed the developer's diagnosis: "Rebuilding it N times is pure redundant work and is the N² term".

2. The on demand settle came from one observation, stated by the user on 2026-08-11: "the point is that we only do this for ourself or if there is a diff registered! not all users! unly when that data is needed do we need to address that, but we are always in the context of a user or client are we not?" And the mechanism in the same session: "we store the diff when stuff happens and then we use that to determine the reconcile where any of the gets that we have targets one of the users or objects in the diff, then we need to recalculate that area, and here it is first come first serve that clears it". Commit `9af5ac3` landed it that evening.

3. The earlier framing, 2026-08-09: "We should update the object tables in calimius defined in the prophet/disciple on get instead of post/update/delete in the database, this would be an optimization as well as it is only the iser that needs to get this", and on 2026-08-10: "much of this also happens in the bakcgorund and we know whne we need to re calc forr certain objects and not". The drain is that background.

4. The result, in the user's words on 2026-09-28: "The latest changes have made search way faster, and the post/update and individaul gets much faster as we do not need to fan out for all users at once, we do it on demand".

5. The reader's slice follows from the same observation carried one step further: a reader is always one user, so the work a read waits on is that user's part of the area, `A_u × t`, and never the other holders', `U_a × A × t`. The inequality `A_u ≤ A < U_a × A` for any area with more than one holder is why the slice, not the area, is the unit a read settles; see [Scaling](./scaling.md).
