---
layout: doc
title: Theory
description: The results the claimius design rests on, each stated as a claim, proved from the SQL and the definitions of the model, tied to the papers and PostgreSQL documentation it rests on, and read back into what it means for claimius.
tags: theory, formal model, datalog, incremental view maintenance, counting, dynamic reachability, omv, closure table, list labeling, materialization, lazy maintenance, poisson, snapshot, xmin, sync cursor
lastUpdated: true
---

# Theory

Every result below has one shape: the claim, the proof, the sources, and what it means for claimius. The model, the masks, the derivations and the symbols are those of [the index](./index.md#the-model). Costs are counted in index entries read, rows written and statements, never in time.

## Result 1: access is a nonrecursive view

**Claim.** With `anc_t` stored in `inheritance_info`, the access relation `A` is a nonrecursive stratified Datalog program: no tuple of `A` takes part in the derivation of another tuple of `A`. Every change to the base relations is therefore maintained exactly by recomputing the pairs its support can reach, and never needs a delete and rederive step.

**Proof.**

1. The rules for `reach` in [Derivations](./index.md#derivations) read `grant`, `claim`, `bind`, `link`, `creator` and `anc_t`. The composition rules consume a binding of the linked claim, `bind(c, y, b, _, _)`, and never a `link` of `c`; `_build_grants_for_objects` joins `claim_object link` once and then `claim_object co` on `co.claim_id = link.object_id`, with no second hop. No rule has `reach` or `A` in its body, so the dependency graph is acyclic.

2. `A` aggregates the derivations of one pair `(u, x)` and applies negation, the deny terms of `_apply_denies`, to derivations of that same pair only. Aggregation and negation over a lower stratum are stratified.

3. The only recursion is `anc_t`. Each tree is acyclic by the types of its edges: an organization goes up only to organizations, a location to locations and to its owner, a registered row to its owner, its pin and its own parent, and every root points at itself, `sa_owner_id = id` or `sa_parent_id = id`, which `up_t` excludes. `calc_hierarchical_access` refuses a change of `sa_root_id` outside replay, and `_splice_subtree` drops a subtree whose new parent lies inside it, so no cycle is ever stored.

4. Gupta, Mumick and Subrahmanian prove that for nonrecursive views, with negation and aggregation, the counting algorithm computes exactly the view tuples inserted or deleted, where DRed, needed for recursive views, first deletes a superset and rederives it. By 1 to 3 the first case applies.

5. The support of a change is an area. A change to claim `c`, to one of its bindings or to a link into or out of it changes `reach` only for holders of `c` or of a claim linking to `c`, on objects of `T_c` and on the pairs whose `grants` name `c`: the claim area. A change to a grant of `c` changes only the assignee's slice of it: the claim area with `user_id`. A change to an object `x` changes `reach` only on `x` and, when `x` is a node, on `desc(x)`: the object area, with `cascade`. So recomputing the pairs of an area touches every pair the change can affect.

**Sources.** A. Gupta, I. S. Mumick, V. S. Subrahmanian, [Maintaining Views Incrementally](https://dl.acm.org/doi/10.1145/170035.170066), SIGMOD 1993. The overdeletion of DRed: B. Motik et al., [Incremental Update of Datalog Materialisation: the Backward/Forward Algorithm](https://www.cs.ox.ac.uk/people/boris.motik/pubs/mnph15incremental-BF.pdf), AAAI 2015. Stored derivations as provenance: T. J. Green, G. Karvounarakis, V. Tannen, [Provenance Semirings](https://web.cs.ucdavis.edu/~green/papers/pods07.pdf), PODS 2007.

**For claimius.** Single hop composition keeps the program nonrecursive, and a nonrecursive program needs only first order deltas, each found by key. The `grants` array of a pair is its why provenance, `user_users.sharing_object_count` the derivation count of the counting algorithm on `A ⋈_x A` (Result 7), and an `access_diff` row the key of a change's support. A transitive link would make `reach` recursive and every settle a DRed step with overdeletion.

## Result 2: the model stays below the reachability bound

**Claim.** A model whose group nesting or composition is transitive over data must solve dynamic reachability, for which no algorithm has both update and query time polynomially below the edge count `m`. The recursion of claimius is bounded by the depth of three trees, and deriving one pair reads a number of index entries independent of `m`, `U` and `O`.

**Proof.**

1. A Zanzibar rewrite that follows a group into its subgroups asks whether a path of membership edges leads from a user to a group; the paper states that "group membership can be considered as a reachability problem in a graph". Fong's ReBAC evaluates a policy by local model checking whose cost is that of a depth first search bounded by the nesting of its modal operators.

2. Henzinger, Krinninger, Nanongkai and Saranurak show that, under the OMv conjecture, no fully dynamic s-t subgraph connectivity algorithm has polynomial preprocessing, `m^{α−ε}` amortized update time and `m^{1−α−ε}` amortized query time for any `0 ≤ α ≤ 1`, and that the bound carries to every reachability problem, transitive closure included.

3. In claimius a link is followed once (Result 1) and `|anc(x)| = a_x ≤ D_o + D_l + D_q`. Deriving the pair `(u, x)` reads the reader's `C` grants, the `a_x` ancestors of `x` from one range of `idx_inheritance_info_descendant`, and the `B_x` bindings on `x` and its ancestors with one probe of `idx_claim_object_object` per node; see [Materialization](./materialization.md#recomputing-a-share). None of these terms grows with `m`.

4. The cost of storing `anc_t` is paid on a move: moving a subtree `S` changes the ancestors of exactly its `n_S` nodes, and any representation that answers `anc(x)` without a walk must rewrite those nodes' ancestor sets (Result 3).

**Sources.** R. Pang et al., [Zanzibar: Google's Consistent, Global Authorization System](https://www.usenix.org/system/files/atc19-pang.pdf), USENIX ATC 2019, sections 2 and 3.2.4. M. Henzinger, S. Krinninger, D. Nanongkai, T. Saranurak, [Unifying and Strengthening Hardness for Dynamic Problems via the Online Matrix-Vector Multiplication Conjecture](https://arxiv.org/abs/1511.06773), STOC 2015. P. W. L. Fong, [Relationship-Based Access Control: Protection Model and Policy Language](https://pages.cpsc.ucalgary.ca/~pwlfong/Pub/codaspy2011.pdf), CODASPY 2011, section 4.3.

**For claimius.** Zanzibar pays the bound with Leopard, a precomputed group to group index of which the paper says "a single Zanzibar tuple addition or deletion may yield potentially tens of thousands of discrete Leopard tuple events". Claimius does not pay it, because its only recursion is three trees of bounded depth, stored where they move. Three trees and one hop are the condition under which a derivation is constant in the size of the data.

## Result 3: the closure table against labeling

**Claim.** A closure table of one row per `(tree, ancestor, descendant)` with two covering indexes answers both directions a settle needs with one descent each, writes `d + 1` rows per inserted node and relabels nothing, where any interval labeling pays `Ω(log² n)` relabelled nodes per insert in a label space linear in `n`.

**Proof.** Let `d` be the ancestors of a new node in one tree, `n_S` the size of a moved subtree and `D_p` the ancestors of its new parent.

1. Reads. `idx_inheritance_info_descendant (descendant_type, descendant_id, tree_type) INCLUDE (ancestor_type, ancestor_id, root_id, depth)` returns the `a_x + t_x` rows above `x`, self edges included, in one descent, index only. `idx_inheritance_info_ancestor` returns the `|desc(y)| + 1` rows below `y` the same way. A walk of the parent columns pays one dependent probe per level for the first and one per node for the second.

2. Insert. `_attach_object_to_trees` inserts the self edge and copies the owner's, or the pin's, ancestor rows with `depth + 1` in one `INSERT ... SELECT`: `d + 1` rows per tree, three index entries each. For a node of a hierarchical table `calc_hierarchical_access` inserts the self edge and `_splice_subtree` with the new parent inserts `D_p + 1` rows, the same `d + 1`.

3. Move. `_splice_subtree` deletes the rows linking the subtree's `n_S` nodes to their former `D_old` proper ancestors above the moved root, `n_S × D_old` rows, and inserts `n_S × (D_p + 1)` rows linking them to the new parent's chain; the rows inside the subtree stay. That is the output of the move and nothing more.

4. Labeling. A pre and post order interval per node answers an ancestor test with one comparison, but each insert must find a free label between its neighbours, the online list labeling problem. Bulánek, Koucký and Saks prove `Ω(log² n)` relabelled items per insert when the label space is linear in `n`.

**Sources.** [PostgreSQL, Index-Only Scans and Covering Indexes](https://www.postgresql.org/docs/17/indexes-index-only-scans.html). J. Bulánek, M. Koucký, M. Saks, [Tight Lower Bounds for the Online Labeling Problem](https://iuuk.mff.cuni.cz/~koucky/papers/monlab.pdf), STOC 2012. B. Karwin, [Moving Subtrees in Closure Table Hierarchies](https://www.percona.com/blog/moving-subtrees-in-closure-table/).

**For claimius.** `inheritance_info` is the one table of ancestor pairs, and every write keeps it incrementally: an insert pays `d + 1` rows, a move pays its own subtree, a soft delete drops the subtree's rows and a restore rebuilds only the restored subtree through `build_ownership_tree`, `build_location_tree` or `build_parenthood_tree` with a node. The cost of a write is bounded by what it moves, never by the size of its root.

## Result 4: a listing reads stored pairs

**Claim.** Storing `A` per pair and listing it from the reader's range costs less than deriving each listed pair from the base relations whenever a pair is listed at least as often as its inputs change, and a pair whose inputs change without being read costs one area row and nothing else.

**Proof.** Let `R_x = 6 + 4a_x + 2t_x + 4B_x` be the index entries one pair's derivation reads, [Materialization](./materialization.md#recomputing-a-share), and `c` the entries a stored listing reads per row, the pair, its `object_info` row, the root organization and `2 G_x` grant probes, `c = 3 + 2 G_x`, [Reads](./reads.md#getobjects).

1. Derived at read time, a listing of `u` over type `τ` must enumerate the candidates: the descendants of every inheriting binding the reader holds, with multiplicity `δ ≥ 1` for overlapping bindings, and derive each, `δ R_x + c` entries per listed row, then sort, since the candidates come in no key order.

2. Stored, a listing reads `c` per row in `object_id` order and sorts nothing. A change of the pair's inputs raises one area row; the pair is recomputed only when the reader next reads, once however many changes came before: `min(W, L)` recomputes of `R_x` over `L` listings and `W` changes.

3. Stored wins when `L c + min(W, L) R_x < L (δ R_x + c)`, that is `min(W, L) < δ L`, which holds for every `δ ≥ 1` and `L ≥ 1`, and holds strictly when `W < L` or `δ > 1`. With `L = 0` the stored side pays `W` area raises and no recompute, the derived side nothing.

4. The rows of an area whose holder never reads are never recomputed, so the stored side's work is bounded by reads, not by changes.

**Sources.** E. N. Hanson, [A Performance Analysis of View Materialization Strategies](https://www2.eecs.berkeley.edu/Pubs/TechRpts/1986/789.html), SIGMOD 1987. R. Chirkova and J. Yang, [Materialized Views](https://dsf.berkeley.edu/cs286/papers/mv-fntdb2012.pdf), Foundations and Trends in Databases 2012, on deferred maintenance.

**For claimius.** `user_object` is the listing index. `get_objects`, `search` and the graphs read it after the reader's share settles, and a page reads `p` entries in key order; the measured page of 50 against a range of 5,000 read 50 `user_object` entries where the unpaged listing read 5,000 and sorted them, see [Reads](./reads.md#getobjects). The check reads the same stored pair, so a listing row and a check agree by construction.

## Result 5: the settle is per pair and per reader

**Claim.** A check that settles only the pair it asks about, when an open area covers that pair, and a listing that settles only the reader's own share of each area it meets, return the pairs a full recompute would, and the work of an area is paid once per holder who reads, never `|H_c| × |T_c|` inside one read.

**Proof.**

1. By Result 1 a pair `(u, x)` can be stale only when an open area's support contains it. `reconcile_if_pending(app, u, τ, x)` tests exactly those areas: an object area on `x`, or on an ancestor of `x` with `cascade`; a claim area of a claim `u` holds, links to, or was assigned (`user_id = u`), whose bindings, live or removed, sit on `x` or an ancestor, directly or through a claim it links to, or whose claim the pair's `grants` name as `claim_id` or `composed_from`, or which is `x` itself. An area is skipped only when `access_diff_user` records `u` at the area's current `raise_count`. When any area is open the pair is recomputed, so the pair read is current.

2. A listing settles, through `_settle_diff_share`, the reader's share of every open object area of the app that can hold a pair of the type it lists, every one when it lists no type, and of every claim area of a claim the reader holds, links to or was assigned, all of them however many are open and none while another read of the reader is settling, and records it at `raise_count`, until the pairs it moves reach the lock budget, past which the areas left stay open for the next read, see [Materialization](./materialization.md#the-lock-budget). A later raise increments `raise_count`, which makes the record stale and the share open again.

3. The share of an object area is `{x} ∪ desc(x)` restricted to objects on which `u` holds a pair or which a binding of `u`'s claims, direct or through one link, reaches, plus `x` for its creator; the share of a claim area is the claim, its bindings and their cascading subtrees, those of the claims it links to, and `u`'s pairs naming it, with bindings changed since the area opened counted too. Each is the slice of Result 1's support that can hold a pair of `u`, so settling it leaves every pair of `u` in the area current.

4. An area is deleted by `_clear_diff` only when every user `_diff_holders` returns has a record at the current `raise_count`, under `FOR UPDATE SKIP LOCKED` and a matching `raise_count`, so a raise that lands between the test and the delete keeps the row.

5. So each holder pays its own share when it reads, and a holder who never reads pays nothing.

**Sources.** Deferred, per query maintenance of materialized views: R. Chirkova and J. Yang, [Materialized Views](https://dsf.berkeley.edu/cs286/papers/mv-fntdb2012.pdf), section on lazy and deferred maintenance.

**For claimius.** On the `settle_granularity` test, a claim bound over a node with three descendants and held by three users, the first read settled 12 pairs, every holder on every object, before the settle was split; a check now settles 1 pair and a listing the reader's 4, and the other holders stay untouched until they read.

## Result 6: the drain rule

**Claim.** Settling a holder's share of an area ahead of its read is worth its cost only when the holder is likelier to read before the area is raised again than after. With reads of holder `j` a Poisson process of rate `r_j` and raises of area `a` one of rate `λ_a`, the share is worth taking when `r_j ≥ λ_a`.

**Proof.** A share settled ahead costs its recompute `R_j`. If the holder reads before the next raise the settle is used, and the read finds nothing open; if the area is raised first the share opens again and the settle is wasted. For independent exponential clocks the read comes first with probability `r_j / (r_j + λ_a)`. The expected useful part of `R_j` is `R_j r_j / (r_j + λ_a)`, at least half of `R_j` exactly when `r_j ≥ λ_a`. Below that most of the drain's work is thrown away, and the holder pays the same `R_j` on its own read, once.

The estimators are rows claimius already holds: `r̂_j = 1 / (now − user_activity.last_used_at)` and `λ̂_a = raise_count / (now − access_diff.sa_created_at)`. `drain_access_diff` tests `now − sa_created_at ≥ raise_count × (now − last_used_at)`, the rule multiplied out, and takes at most `p_limit_pairs` shares, most recently active user first.

**Sources.** The minimum of two exponential random variables: any text on Poisson processes, for example [the exponential distribution](https://en.wikipedia.org/wiki/Exponential_distribution#Distribution_of_the_minimum_of_exponential_random_variables).

**For claimius.** The drain settles ahead only the shares whose holders read often compared to how often their area moves. A holder with no `user_activity` row is never drained; its share waits for its own read. See [Materialization](./materialization.md#the-drain).

## Result 7: counting keeps user_users exact

**Claim.** `user_users` is kept exact by moving `sharing_object_count(u, v)` and `sharing_object_count(v, u)` by one for every other user `v` of `x` when a pair `(u, x)` of a type other than `claimius.claim` enters or leaves `user_object`, the moves of one statement summed per ordered pair.

**Proof.** `sharing_object_count(u, v) = |{x : τ(x) ≠ claim, (u, x) ∈ P, (v, x) ∈ P}|`, a count over the self join `P ⋈_x P`, a nonrecursive view. A tuple `(u, x)` entering or leaving `P` changes exactly `(u, v)` and `(v, u)` for each other `v` holding `x`, by one: the counting algorithm of Result 1 on the join. Within a statement `_count_user_users` counts each ordered pair of users once per object, the pairs of the statement joined with themselves on the delete side and excluded from the other side on the insert side. Across transactions it is exact because they serialize per object: the trigger holds `pg_advisory_xact_lock` on every object of the statement before it reads the other holders, so of two transactions adding pairs on one object the second reads the first's committed pair. The settle took those keys already, sorted, through `_lock_objects`, so the trigger waits on nothing and no two settles take common keys in different orders.

**Sources.** Gupta, Mumick, Subrahmanian as in Result 1. [PostgreSQL, Advisory Locks](https://www.postgresql.org/docs/17/explicit-locking.html#ADVISORY-LOCKS).

**For claimius.** A pair entering reads `U_x` entries and writes at most `2 (U_x − 1)` rows; the `pair_storage` test measures 2 rows written for a pair entering on an object one other user holds. Holding a claim reads the claim, but a claim is a template, not an object two users share, so its pairs move no count. See [Materialization](./materialization.md#what-a-changed-pair-writes).

## Result 8: the minimal set of stored relations

**Claim.** The relations claimius stores beyond its base tables are exactly those without which a query of the model reads more than its own answer: `inheritance_info`, `user_object`, `object_info`, `user_users`, and the maintenance state `access_diff`, `access_diff_user` and `user_activity`.

**Proof.** For each, the cheapest plan without it:

1. Without `inheritance_info`, every derivation walks `a_x` levels one dependent probe at a time and a settle finds `desc(y)` one node at a time. It is the only recursion (Result 1), so nothing else replaces it.

2. Without `user_object`, a listing derives `δ R_x` entries per row and sorts (Result 4).

3. Without `object_info`, the name, owner, location and search vector of an object are either read from the implementing side's table per row, one dynamic statement per type, or copied onto each of its `U_x` pairs; a rename would then rewrite `U_x` rows and raise an area for every holder. With it, a rename writes one row and raises nothing.

4. Without `user_users`, `get_users` for `u` is the self join `Σ_{x ∈ V_u} U_x` per call, where the stored relation is one range of `S_u` rows.

5. `access_diff` and `access_diff_user` are the state of lazy maintenance (Result 5), `user_activity` the estimator of Result 6.

6. `claim_graph_cache` stores a rendering, not a relation of the model, validated on read; see [Graphs](./graphs.md). `reconcile_queue` holds recomputes that failed.

**For claimius.** Nothing else is materialized. The member listing of an object is the reverse index `idx_user_object_object_user` on the same rows, not a second table.

## Result 9: the cursor below the snapshot xmin

**Claim.** A consumer that pulls `get_sync_events(app, txid, seq)` pages in `(txid, seq)` order and moves its cursor to the last event of each page never passes over an event, whatever order the sequences of concurrent transactions commit in.

**Proof.** Let the reading snapshot have `xmin`, the lowest transaction id still running when it was taken.

1. `get_sync_events` serves an event only when `txid < pg_snapshot_xmin(pg_current_snapshot())`, or when it is the reader's own transaction. A transaction id below `xmin` belongs to a transaction that has ended; an event of an aborted transaction does not exist, so every served event is committed and visible.

2. Every event not yet visible to the snapshot belongs to a transaction running at the snapshot or started after it. A running transaction has `txid ≥ xmin` by the definition of `xmin`; a later one has `txid ≥ xmax ≥ xmin`, since transaction ids are assigned in increasing order and `xid8` never wraps.

3. Hence every event served later has `txid ≥ xmin > txid(c)` for the cursor `c` taken from this page, so it sorts after `c`. Events below `xmin` not served because of the page limit sort after `c` as well, and come on the next page.

4. The events of one transaction share its `txid` and become visible together, so their `seq` order within the transaction is complete when any of them is served.

A `seq` alone does not have this property: sequences are taken at insert and made visible at commit, so a higher `seq` can commit before a lower one, and a cursor on `seq` would pass the lower one over.

**Sources.** [PostgreSQL, Transaction ID and Snapshot Information Functions](https://www.postgresql.org/docs/17/functions-info.html#FUNCTIONS-PG-SNAPSHOT): `pg_current_xact_id`, `pg_current_snapshot`, `pg_snapshot_xmin`, and the definition of `xmin` as the earliest transaction still active. [PostgreSQL, Sequence Manipulation Functions](https://www.postgresql.org/docs/17/functions-sequence.html), on `nextval` never being rolled back.

**For claimius.** The cursor is the pair `(txid, seq)`, written `<txid>.<seq>` on the wire, and one index `idx_sync_event_app_txid_seq (app_id, txid, seq)` serves a page as one range. The snapshot sent to a new consumer carries the cursor `(xmin, 0)` of the snapshot it was read under, so the feed resumes exactly after it. See [Sync](./sync.md).
