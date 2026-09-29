---
layout: doc
title: Materialization
description: The per user access index user_object and its projections, how a write records an area in access_diff, how reconcile_if_pending settles it on the first read, the rows each step reads and writes, and why the maintenance is exact and the shape is right.
tags: user_object, user_users, access_diff, reconcile_if_pending, recompute_user_object, _settle_object_diff, _settle_claim_diff, reconcile_queue, incremental view maintenance
lastUpdated: true
---

# Materialization

Claimius stores the access relation `A` of [the model](./index.md#the-model) per pair, so a check is one pair and a listing one index range, whatever the grants behind them. A write records what it changed as an area and settles only its own actor; the first read in the app that meets the area settles it for every user it reaches.

## The tables

| Table | One row per | Holds | Indexes |
|---|---|---|---|
| `user_object` | app, user, object with `A ≠ 0` | `sa_access`, `grants`, `scope`, `direct_grant`, and the object's `sa_owner_id`, `sa_location_id`, `sa_root_id`, `sa_name`, `sa_description`, `sa_link`, `search_vector` | primary key `id = composite_id(app, user, type, object)`; `idx_user_object_user_type (user_id, app_id, object_type, sa_access)`; `idx_user_object_object_user (app_id, object_type, object_id, user_id)`; `idx_user_object_owner`, `idx_user_object_location`, `idx_user_object_root`; GIN on `grants`; GIN on `search_vector` |
| `user_users` | app, viewer, target | `sharing_object_count = |V_u ∩ V_v|`, kept by counting, first and last time; see [Users](./users.md) | primary key; `(app_id, viewer_id)`; `(app_id, target_user_id)` |
| `access_diff` | outstanding area | `kind` (`object` or `claim`), the object pair or the claim, `cascade` | primary key; `(app_id, object_type, object_id) WHERE kind = 'object'`; `(app_id, claim_id) WHERE kind = 'claim'` |
| `reconcile_queue` | app, user, object | a pair whose recompute failed and waits for its user's next read | primary key; `(app_id, user_id)` |

`grants` is the array of `{claim_id, access, scope, cascaded_from, tree_type}` of the surviving derivations, the pair's provenance, from which a read picks its actor token. None of these tables is written by application code. Access changes by changing grants and bindings.

## Recomputing one pair

`recompute_user_object(app, user, type, object)` is the unit every settle is made of. It reads:

| Step | Index entries |
|---|---|
| `table_info` twice, the object's live row once | 3 |
| `_build_grants_for_objects`: the user's grants and their claims | `1 + C` |
| the bindings on the object | `1 + B_0` |
| the object's ancestors in every tree, then the bindings on each | `1 + a_x + t_x`, then `a_x` probes returning `B_x − B_0` |
| the links out of the user's claims and the linked claims' bindings on the object | `C + L` |
| the creator test: the creator, its grant, a direct owner deny | 3 |
| `_denormalize_objects`: `table_info`, the row, its root | 3 |

```
R_read(u, x) = 2C + L + 2 a_x + B_x + t_x + O(1)
```

It writes, when the pair changed: one `user_object` row with its five btree entries, a sixth when pinned, `≈ 5 G_x` entries in the `grants` GIN index and one per lexeme in the search GIN index; on insert or delete the `user_users` counts against the other users of the object, see [Users](./users.md); one `sync_event` row per written row; and one delete probe on `claim_graph_cache`. The upsert is `ON CONFLICT (id) DO UPDATE ... WHERE ... IS DISTINCT FROM`, so a pair whose every column is unchanged writes nothing and fires no trigger. When the object is gone or no derivation survives, it deletes the pair instead.

## Writes record areas

An area is the smallest description of what a write changed, keyed on what changed and never on the users it affects.

| Write | Settles in the write | Raises in `access_diff` |
|---|---|---|
| Insert, owner or location change, or soft delete of a registered row | The actor's pair on the row | `object` on the row |
| Any write of an organization, a location or a parenthood row | The actor's pair on the row | `object` with `cascade` on the row |
| An update that changes neither owner, location nor soft delete | The actor's pair on the row | nothing |
| `claim`, `claim_object` | The actor's pairs on `T_c` | `claim` on the claim, and on the linked claim for a composition link |
| `user_claim` | The actor's and the assignee's pairs on `T_c` | `claim` on the claim |

The id of an area is `composite_id('object', app, type, id)` or `composite_id('claim', app, claim)`, and the raise is `INSERT ... ON CONFLICT (id) DO UPDATE`, so `m` raises of one area before it is settled leave one row: one probe and one row written per raise, whatever the area reaches. After the synchronous part, `_refresh_user_users` runs for each settled user; see [Users](./users.md).

## The first read settles

Every read function begins with `reconcile_if_pending(app, user)`:

1. It selects the app's outstanding areas: every `object` area, and every `claim` area on a claim the reader holds or holds a link to. `Δ_a` rows are read, each claim area tested with the reader's grants.

2. It locks them `FOR UPDATE SKIP LOCKED`. An area another read is settling is skipped, and this read goes on with the pairs as they stand.

3. It settles each area for every user it reaches, deletes the row, drains the reader's own `reconcile_queue` rows; `user_users` moves with each pair that entered or left.

With nothing outstanding the settle costs `Δ_a = 0` rows read and nothing written, and that is the common case.

### What a settle costs

An `object` area on `x` recomputes, for `x` and, with `cascade`, every descendant `y` of `x`, the pair of every user in `_affected_users_for_object(y)`: the creator of `y`, the users holding a pair on `y`, read through `idx_user_object_object_user`, the holders of bindings on `y`, and the holders of inheriting bindings on the ancestors of `y`.

```
pairs(object area x) = Σ_{y ∈ {x} ∪ desc(x)} |affected(y)|
pairs(claim area c)  = |H_c| × |T_c|
cost(area)           = pairs × R_read + (rows written for the pairs that changed) + Σ_{pairs entering or leaving} (U_x + 2 (U_x − 1))
```

On the data set of [the index](./index.md#symbols), a binding change on a leaf claim with 6 holders over 33 objects settles 198 pairs; a binding change on a root claim with 2 holders over 20,782 objects settles 41,564.

## The settle as a queue

The outstanding areas are a queue whose arrivals are writes and whose departures are reads.

1. Arrivals collapse. An area is one row however many times it is raised before it is settled, so the queue is bounded by the distinct claims and objects raised since the last read in the app, never by the number of writes.

2. The writer pays a constant. A binding change costs its own row, one area row and the actor's `|T_c|` pairs; settling every holder in the write would cost `|H_c| × |T_c|` pairs inside the writer's transaction, with every one of those rows locked until commit.

3. The first reader pays the area. It settles every user of the area, not only itself, so the work is done once per area, not once per reader. Any read in the app settles every outstanding object area; a claim area waits for a read by a holder of the claim.

4. Work nobody reads is not done. An area raised and raised again before any read is settled once, and a claim area no holder reads stays one row.

5. Nothing is queued outside the database. Every area is written in the transaction of its change, committed or rolled back with it.

## Why the maintenance is exact

**Claim.** Once `anc` is stored, `A` is a nonrecursive stratified program, so every change is maintained exactly by recomputing the pairs its area covers, and no delete and rederive step is ever needed.

**Proof.**

1. The rules of `reach` read `grant`, `claim`, `bind`, `link`, `creator` and `anc`. The composition rule consumes a binding of the linked claim, never another link, so no rule has `reach` in its body and the dependency graph is acyclic.

2. `A` aggregates the derivations of one pair and negates, the deny terms, only derivations of that same pair. Aggregation and negation over a lower stratum are stratified.

3. The only recursion is `anc`, and it is stored. For nonrecursive views, including negation and aggregation, Gupta, Mumick and Subrahmanian prove that the counting algorithm computes exactly the view tuples inserted or deleted, where DRed, needed for recursive views, first deletes a superset.

4. The support of a change is its area. A change to claim `c`, a binding or a link of it changes `reach` only for holders of `c` or of a claim linking to `c`, on objects of `T_c`: the claim area. A change to an object `x` changes `reach` only on `x` and, when `x` is a node, `desc(x)`: the object area. Recomputing the pairs of an area therefore touches every pair the change can affect.

**Sources.** A. Gupta, I. S. Mumick, V. S. Subrahmanian, [Maintaining Views Incrementally](https://dl.acm.org/doi/10.1145/170035.170066), SIGMOD 1993. The overdeletion cost of DRed: B. Motik et al., [Incremental Update of Datalog Materialisation: the Backward/Forward Algorithm](https://www.cs.ox.ac.uk/people/boris.motik/pubs/mnph15incremental-BF.pdf). The stored derivations as provenance: T. J. Green, G. Karvounarakis, V. Tannen, [Provenance Semirings](https://web.cs.ucdavis.edu/~green/papers/pods07.pdf), PODS 2007.

**For claimius.** Single hop composition keeps the program nonrecursive, and a nonrecursive program needs only first order deltas: the higher order delta views of [DBToaster](https://arxiv.org/abs/1207.0137) and the nested iteration of [differential dataflow](https://www.cidrdb.org/cidr2013/Papers/CIDR13_Paper111.pdf) exist for queries whose delta is itself recursive. The `grants` array is the pair's why provenance, `user_users.sharing_object_count` the derivation count on `A ⋈_x A`, and an area the key of a change's support. A transitive link would make `reach` recursive and every settle a DRed step with overdeletion.

## Why a listing reads stored pairs

**Claim.** Storing `A` per pair is cheaper than deriving it at read time whenever a pair is listed more than `R_read / (a_x + B_x)` times per change of its inputs, a small constant.

**Proof.** A listing of `u` over type `τ` from stored pairs reads one range, `V_{u,τ}` entries, and per row its heap tuple and `2 G_x` probes to choose the grant. A listing derived at read time must enumerate the descendants of every inheriting binding the reader holds, `δ V_{u,τ}` candidates with `δ ≥ 1` the multiplicity of overlapping bindings, and per candidate find the bindings on its ancestors to apply the deny terms, `a_x + B_x` entries, before it can emit the row. Over a period in which a pair is listed `L` times and its inputs change `W` times, stored costs `L (2 + 2G_x) + W R_read` and derived costs `L (δ + a_x + B_x + 2G_x)`, so storing wins when

```
L / W  >  R_read / (δ − 2 + a_x + B_x)
```

With `R_read ≈ 2C + 2a_x + B_x` the right side is a few units for any tree of depth two or more. Access data is read far more often than it changes: a person opens lists many times between changes to their grants.

**Sources.** E. N. Hanson, [A Performance Analysis of View Materialization Strategies](https://www2.eecs.berkeley.edu/Pubs/TechRpts/1986/789.html), SIGMOD 1987, which compares a view answered by query modification with immediate and deferred maintenance and finds none best everywhere; R. Chirkova and J. Yang, [Materialized Views](https://dsf.berkeley.edu/cs286/papers/mv-fntdb2012.pdf), Foundations and Trends in Databases 2012, on deferred maintenance.

## Why the pair carries the fields of the object

`user_object` holds the object's name, description, link, owner, location and search vector, so a listing, a search and a graph render without reading the implementing side's table: one range, no join. The cost is that these are stored `U_x` times per object, `Ū_o = 19` times on the data set of [the index](./index.md#symbols), and that a rename, which raises no area, rewrites only the actor's copy; the other copies take the new name when a later area covering the object is settled.

## The minimal set of stored relations

1. `inheritance_info` is the only recursion (above), so nothing else can replace it: without it every derivation walks `a_x` levels one dependent probe at a time.

2. `user_object` is what makes a check one pair and a listing one range.

3. `user_users` turns the self join `Σ_{x ∈ V_u} U_x` of `get_users` into one range of `S_u` rows.

5. `access_diff` and `reconcile_queue` are the state of lazy maintenance. `claim_graph_cache` stores a rendering; see [Graphs](./graphs.md).

`recompute_user_object` can be called directly to force one pair, and `rebuild_user_object` empties `user_object` and recomputes the pair of every holder on every object a live binding names directly, one recompute per distinct `(user, bound object)`, then refreshes every user's `user_users` rows. Descendants reached only through a cascade are filled by the next settle that covers them.
