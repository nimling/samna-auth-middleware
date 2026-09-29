---
layout: doc
title: Theory
description: Claimius stated formally as a Datalog program, and the results that fix its methodology, each as a claim, a proof from definitions and formulas, the papers it rests on, and what it means for claimius; the tree encoding, the check evaluated from its sources, the per user index for listings, maintenance by counting, the drain rule, the minimal set of materialized relations, and the prices of 2013 and 2026 fed into the materialization break-even.
tags: theory, formal model, datalog, relational algebra, incremental view maintenance, counting, dred, dynamic reachability, omv, list labeling, path encoding, five minute rule, materialization, break-even, zanzibar, leopard, prices
lastUpdated: true
---

# Theory

This file states claimius as a program over its tables and proves the methodology the other files describe. Writing authorization as Datalog is the approach of logic based security languages such as J. DeTreville's [Binder](https://www.microsoft.com/en-us/research/wp-content/uploads/2016/02/tr-2002-21.pdf), where each policy is a program; claimius is one fixed program whose rules never change, which is what lets every result below be proved once. Every result has the same shape: the claim, the proof, the sources, and what it means for claimius. The symbols of [Scaling](./scaling.md) are used unchanged: `t_s` a client statement, `t_d` a cached B-tree descent, `t` a settled pair, `u` a planner cost unit, `σ` the speed of a database thread against the calibration core, `C` the grants a user holds, `D` the depth of a tree, `V_u` the objects a user reaches.

## The model

### Base relations

| Relation | Table | Meaning |
|---|---|---|
| `org(o, p)` | `organization` | `p = sa_owner_id`, `p = o` on a root |
| `loc(l, p, o)` | `location` | parent location `p`, owning organization `o` |
| `obj(x, o, l, q)` | a registered table | owner `o`, location pin `l`, parent `q`, each possibly absent |
| `claim(c, m, i)` | `claim` | mask `m`, inherit flag `i` |
| `grant(g, u, c)` | `user_claim` | live and inside its window |
| `bind(c, y, b, i, σ)` | `claim_object` | object `y`, binding mask `b`, inherit flag `i`, scope `σ` |
| `link(a, c, b, i)` | `claim_object` with `object_type = 'claimius.claim'` | claim `a` composes claim `c` |
| `creator(x, g)` | `sa_created_by` | the grant `x` was created under |
| `deny(c)` | `claim.sa_access & 16 ≠ 0` | `c` subtracts |

### Derived relations

The up edges of an object are its owner, its parent, its location pin, and for a location its parent and its owner:

```
up(x, y) ← obj(x, y, _, _) ; obj(x, _, y, _) ; obj(x, _, _, y) ; org(x, y), x ≠ y ;
           loc(x, y, _), x ≠ y ; loc(x, _, y)
anc(x, y, 0) ← x = y
anc(x, z, h + 1) ← anc(x, y, h), up(y, z)
```

A grant reaches an object directly, through a tree, or through one composition link, with the effective mask of [Claims](./claims.md):

```
eff(c, b) = m_c & expand(coalesce(b, m_c))
reach(u, x, g, e, 'direct')    ← grant(g, u, c), bind(c, x, b, _, _), e = eff(c, b)
reach(u, x, g, e, 'inherited') ← grant(g, u, c), claim(c, _, true), bind(c, y, b, true, _),
                                 anc(x, y, h), h > 0, e = eff(c, b)
reach(u, x, g, e, k)           ← grant(g, u, a), link(a, c, l, true), claim(c, _, true),
                                 reach'(c, x, e', k), e = expand(coalesce(l, m_a)) & e'
reach(u, x, g, 15, 'creator')  ← creator(x, g), grant(g, u, _)
```

where `reach'(c, x, e', k)` is the first two rules with the grant removed, so a link is followed once and never through another link. The access of a user on an object is an aggregate over its derivations, with `G_d`, `G_i`, `G_c` the OR of the direct, inherited and creator grant masks and `X_d`, `X_i` the OR of the direct and inherited deny masks, from [Denials](./denials.md):

```
A(u, x) = close( (G_d | G_c) & ~X_d  |  G_i & ~(X_d | X_i) )
```

Claimius stores four derived relations:

| Stored | Holds | Definition |
|---|---|---|
| `object_info` ancestor arrays | `anc(x, ·, h)`, `h > 0`, per tree, nearest first | the only recursive relation |
| `user_object` | `A(u, x) ≠ 0` with the derivations in `grants` | the access relation, per user |
| `user_users` | `|{x : A(u, x) ≠ 0 ∧ A(v, x) ≠ 0}|` | the sharing count of two users |
| `access_diff` | the support of a pending change | maintenance state |

### The queries

| Query | Relational algebra | Served by |
|---|---|---|
| Check `(u, x, r)` | `σ_{u, x, A & r = r} A` | evaluated from the base relations and the ancestor arrays |
| Listing `(u, τ, r)` | `π_x σ_{u, type = τ, A & r = r} A` | a range of `user_object` |
| Search, graphs | the listing joined to `object_info` | `user_object` and `object_info` |
| Members of `x` | `π_u σ_x A` | `idx_user_object_object` |
| Visible users of `u` | `π_v σ_{u} user_users` | `user_users` |
| Capability `(u, path, r)` | `σ_{object = ⊥, jsonpath} (grant ⋈ claim ⋈ bind)` | the base relations, see [Capabilities](./capabilities.md) |

## Result 1: access is a nonrecursive view over the ancestor relation

**Claim.** Once `anc` is stored, `A` is a nonrecursive stratified Datalog program: no derived tuple of `A` takes part in the derivation of another tuple of `A`. Every change to the base relations is then maintained exactly by recomputing the affected tuples from their derivations, the counting algorithm, and never needs the delete and rederive algorithm.

**Proof.**

1. The rules for `reach` read `grant`, `claim`, `bind`, `link`, `creator` and `anc`, and `reach'` reads the same relations without `link`. No rule of `reach` has `reach` in its body, and `reach'` is not `reach`: the composition rule consumes a tuple of `reach'`, which has no composition rule. The dependency graph of `reach` is therefore acyclic.

2. `A` aggregates `reach` per `(u, x)` and applies negation, the deny terms, to derivations of the same pair only. Aggregation and negation over a lower stratum are stratified.

3. The only recursion is `anc`. The up graph is acyclic by the types of its edges: an organization goes up only to organizations, a location to locations or organizations, a registered row to rows of its own type, locations or organizations, and every tree's roots point at themselves. With `anc` stored, it is a base relation of the program for `A`.

4. Gupta, Mumick and Subrahmanian prove that for nonrecursive views, including negation and aggregation, the counting algorithm "computes exactly those view tuples that are inserted or deleted", while DRed is needed for recursive views and first deletes a superset of the affected tuples. By 1 to 3 the first case applies.

5. The support of a change is exactly an area. A change to claim `c`, one of its bindings or one of its links changes `reach` only for `u ∈ holders(c) ∪ holders(links into c)` and `x ∈ {y} ∪ desc(y)` for the bindings `y` of `c`; a change to a grant `g` of claim `c` changes only the assignee's slice of that set; a change to an object `x` changes only `x` and `desc(x)` for every user. These are the claim area, the reader's slice and the object area of [Materialization](./materialization.md), so a settle that recomputes the pairs of an area touches no pair the change cannot affect.

**Sources.** A. Gupta, I. S. Mumick, V. S. Subrahmanian, [Maintaining Views Incrementally](https://dl.acm.org/doi/10.1145/170035.170066), SIGMOD 1993, as summarized on the [UMD project page](http://www.cs.umd.edu/projects/hermes/publications/abstracts/mvi.html). The overdeletion cost of DRed: B. Motik et al., [Incremental Update of Datalog Materialisation: the Backward/Forward Algorithm](https://www.cs.ox.ac.uk/people/boris.motik/pubs/mnph15incremental-BF.pdf). The stored derivations as provenance: T. J. Green, G. Karvounarakis, V. Tannen, [Provenance Semirings](https://web.cs.ucdavis.edu/~green/papers/pods07.pdf), PODS 2007, where bag semantics and why provenance are instances of one semiring annotation.

**For claimius.** Single hop composition is what keeps the program nonrecursive, and a nonrecursive program needs only first order deltas: the higher order delta views of [DBToaster](https://arxiv.org/abs/1207.0137) and the arbitrarily nested iteration of [differential dataflow](https://www.cidrdb.org/cidr2013/Papers/CIDR13_Paper111.pdf) exist for queries whose delta is itself expensive or recursive, and claimius's delta is an area, found by key. The `grants` array of a `user_object` row is the pair's why provenance, `user_users.sharing_object_count` is literally the derivation count of the counting algorithm on the self join `A ⋈_x A`, and `access_diff` is keyed on the exact support of a change. A transitive link would make `reach` recursive, and every maintenance step would become a DRed step with overdeletion.

## Result 2: the model stays below the dynamic reachability lower bound

**Claim.** A model whose group nesting or composition is transitive over data must solve dynamic reachability, for which no algorithm has both update and query time polynomially below `m`. Claimius's check costs `O(C + |anc(x)|)` index probes, independent of the number of objects, users and edges.

**Proof.**

1. A Zanzibar userset rewrite that follows a group into its subgroups asks, for a user `U` and a group `G`, whether a path of membership edges leads from `U` to `G`. The Zanzibar paper states it: "Group membership can be considered as a reachability problem in a graph". Fong's ReBAC evaluates a policy by local model checking whose cost is that of a depth first search bounded by the nesting of modal operators.

2. Henzinger, Krinninger, Nanongkai and Saranurak show that, under the OMv conjecture, no fully dynamic s-t subgraph connectivity algorithm has polynomial preprocessing, `m^{α−ε}` amortized update time and `m^{1−α−ε}` amortized query time for any `0 ≤ α ≤ 1`, and that the bound carries to every reachability problem, transitive closure included. So `update × query ≥ m^{1−ε}` up to the exponent split.

3. In claimius a composition link is followed once (Result 1), and `anc` is a bounded up set: `|anc(x)| ≤ D_q + D_l + D_o (D_l + 2)` for parenthood, location and ownership depths. A check reads the reader's `C` grants, the up set of `x`, and the bindings on `x` and its up set; none of these grows with `m`.

4. The price of the up set is paid on a move: moving a subtree `S` changes `anc` for exactly the `n_S` nodes of `S`, and any representation that answers `anc(x)` from `x`'s own row must write those `n_S` rows. The move is output sensitive, so it is optimal among such representations.

**Sources.** R. Pang et al., [Zanzibar: Google's Consistent, Global Authorization System](https://www.usenix.org/system/files/atc19-pang.pdf), USENIX ATC 2019, sections 2 and 3.2.4. M. Henzinger, S. Krinninger, D. Nanongkai, T. Saranurak, [Unifying and Strengthening Hardness for Dynamic Problems via the Online Matrix-Vector Multiplication Conjecture](https://arxiv.org/abs/1511.06773), STOC 2015, section 1 and the footnotes to figure 1. P. W. L. Fong, [Relationship-Based Access Control: Protection Model and Policy Language](https://pages.cpsc.ucalgary.ca/~pwlfong/Pub/codaspy2011.pdf), CODASPY 2011, section 4.3. The role hierarchies claimius does not make transitive: [NIST RBAC](https://csrc.nist.gov/projects/role-based-access-control).

**For claimius.** Zanzibar pays the bound with Leopard, a precomputed group to group index of which the paper says "a single Zanzibar tuple addition or deletion may yield potentially tens of thousands of discrete Leopard tuple events". Claimius does not pay it, because its only recursion is the up set of a DAG of bounded depth, maintained in the rows that move. Three trees and one hop are not a limit of the implementation; they are the condition under which a check is constant in the size of the data.

## Result 3: the up set is stored as arrays on the object

**Claim.** Storing each object's ancestors as arrays in its own `object_info` row, `owner_ids`, `location_ids` and `parent_ids`, nearest first, under one GIN index on their union, costs less on insert, on move and in storage than a closure table of one row per ancestor pair, and less on insert than any interval labeling, at equal read cost.

**Proof.** Let `a = |anc(x)|`, `D` the depth, `n_S` the size of a moved subtree, and count heap rows plus index entries as the unit of write work.

1. Closure table, one row per `(ancestor, descendant)` with a primary key and two covering indexes: an insert writes `(D + 1)` rows, `4(D + 1)` entries; a move deletes `n_S D` and inserts `n_S D` rows, `8 n_S D` entries; storage is `(D̄ + 1) × 480 B` per object, 2,400 B at `D̄ = 4`.

2. Arrays on the object: an insert writes the object's own row once, the arrays copied from its parent's row with the parent prepended, so the write is the row's primary key entry plus `a` GIN postings, appended to the pending list and merged in bulk; a move rewrites `n_S` rows, `n_S (2 + a)` entries; storage is `3 × 24 + 16 a` bytes of arrays plus `a` postings of 1 to 7 bytes each, varbyte delta encoded, about 150 B per object at `a = 5`.

3. Interval labeling, a pre and post order interval per node: an ancestor test is a comparison of two labels, but every insert must find a free label between its neighbours. That is the online list labeling problem. Bulánek, Koucký and Saks prove `Ω(log² n)` moves per insert when the label space is linear in `n`, and the randomized lower bound for polynomial label spaces is `Ω(log n)`. At `n = 10^6` that is about 20 relabelled rows per insert, where the arrays write one.

4. Reads. A check needs `anc(x)`: one primary key descent reads the arrays, where the closure reads `D + 1` leaf entries after one descent; both are `t_d` plus a tuple. A settle needs `desc(y)`: the GIN posting list of `y` yields `n_S` tuple ids and `n_S` heap fetches of 60 ns, where the closure yields `n_S` index only entries of 30 ns; against a settled pair of `t ≥ 6 µs` the difference is `0.03 / 6 = 0.5 %`.

5. Ratios at `D = 4`, `a = 5`: insert 20 against 6 entries, 3.3 times; move `32 n_S` against `7 n_S`, 4.6 times; storage 2,400 against 150 B, 16 times.

**Sources.** J. Bulánek, M. Koucký, M. Saks, [Tight Lower Bounds for the Online Labeling Problem](https://iuuk.mff.cuni.cz/~koucky/papers/monlab.pdf), STOC 2012; the polynomial space bound as cited in [Online List Labeling with Near-Logarithmic Writes](https://arxiv.org/abs/2405.04467). The GIN posting list format: [PostgreSQL, GIN](https://www.postgresql.org/docs/17/gin.html) and [`ginpostinglist.c`](https://github.com/postgres/postgres/blob/REL_17_STABLE/src/backend/access/gin/ginpostinglist.c). The closure costs: [Karwin, Moving Subtrees in Closure Table Hierarchies](https://www.percona.com/blog/moving-subtrees-in-closure-table/).

**For claimius.** `object_info` carries the up set of every object, so the row a check reads for existence, owner and creator also gives it every ancestor, and no table of ancestor pairs exists. Why the arrays sit on `object_info` rather than in a table of their own: a check reads one row instead of two, `t_d = 2.5 µs` saved on every check, while a move pays the other four indexes of `object_info` on each moved row, about 15 µs an entry by the write estimate `c_w ≈ 1.2 ms` for 79 entries of [Scaling](./scaling.md), 60 µs a row; the first wins whenever checks outnumber moved rows 24 to one, which holds in any system that is used.

## Result 4: the check is evaluated from its sources

**Claim.** A check evaluated from the base relations and the up arrays costs no more index descents than a probe of a lazily maintained `user_object`, never writes, and is always current. The check therefore does not read `user_object` and does not probe `access_diff`.

**Proof.** Count descents, each `t_d = 2.5 µs` cached.

1. A probe of a lazily maintained per pair index must first prove the pair current. It reads the reader's grants, one range and `C` claim probes; the ancestors of `x`, one descent; the pending areas, one probe per area id, `2C + D + 1` of them for the whole area and the reader's slice of each claim and one per ancestor; the reader's `samna_user`; the pair. That is `(1 + C) + 1 + (2C + D + 1) + 1 + 1 = 3C + D + 5` descents, 24 at `C = 5`, `D = 4`, and when an area is found a settle, which writes.

2. The evaluation reads the reader's grants, `1 + C`; the links out of them, `C` probes of `idx_claim_object_lookup`, and the `L` linked claims; the object's `object_info` row, one descent, which gives existence, creator and the up arrays; the bindings on `x` and its `a` ancestors, `a + 1` probes of `idx_claim_object_object`; the reader's `samna_user`, one. That is `2C + L + a + 4` descents, 19 at `C = 5`, `L = 0`, `a = 4`.

3. The difference is `(3C + D + 5) − (2C + L + a + 4) = C − L + 1` descents in favour of the evaluation whenever `a ≤ D`, and the evaluation writes nothing, so it is `STABLE`, runs on a hot standby, and has no staleness to bound.

4. The tuples read are the bindings on the up set, `B_x`. When one ancestor carries many bindings of claims the reader does not hold, the planner probes `(claim_id, object_type, object_id)` for the reader's `C` claims instead, so the binding term is `min((a + 1) t_d + B_x × 60 ns, C (a + 1) t_d)`.

5. Eager maintenance would make the probe `(1 + C) + 1 + 1 = C + 3` descents, the grants for the actor token, `samna_user` and the pair, at the price of `U_a × A × t` inside every binding write, the cost [Materialization](./materialization.md) removes from writes. The evaluation is `C + L + a + 1` descents above it, 25 µs at the constants above, and keeps every write constant: at 17 ms of settle for a binding over 300 objects held by three users, the eager write pays for 680 checks.

**Sources.** The same argument in general form: E. N. Hanson, [A Performance Analysis of View Materialization Strategies](https://www2.eecs.berkeley.edu/Pubs/TechRpts/1986/789.html), SIGMOD 1987, which compares a virtual view answered by query modification with immediate and deferred maintenance and finds none best everywhere, as summarized by R. Chirkova and J. Yang, [Materialized Views](https://dsf.berkeley.edu/cs286/papers/mv-fntdb2012.pdf), Foundations and Trends in Databases 2012, section on deferred maintenance: "queries requiring up-to-date view contents must wait for any necessary view maintenance to complete". The descent constants: [Scaling](./scaling.md).

**For claimius.** `get_access`, `get_direct_access`, `mutation_access` and `resolve_access` answer from `_build_grants_for_objects`, set based and `STABLE`, and `_best_grant` over its rows. The settle writes `user_object` from the same function, so a listing row and a check agree by construction. The answer for the closest ancestor, which `get_direct_access` gives, is the first row of the same evaluation ordered by hops. The check's cost is `t_s + (2C + L + a + 4) t_d`, 0.11 ms at the constants of [Scaling](./scaling.md), and it never waits on a settle.

## Result 5: listings read a per user index of rows

**Claim.** For a listing, storing `A` per user is optimal when a pair is listed more than `t / Δc` times per change of its inputs, 11 to 36 times on the calibration core, and a per pair B-tree row is cheaper per listed row than a compressed bitmap of object ordinals.

**Proof.**

1. A listing evaluated from sources enumerates the descendants of the reader's inheriting bindings with multiplicity `δ = Σ n_S / V_u ≥ 1`, and per candidate pays a GIN posting, 0.005 `u`, a heap fetch of the object's row, 0.01 `u`, a hash deduplication, 0.005 `u`, and a deny set probe, 0.0025 `u`; per result row the grant resolution, `1.5 × 0.015 u`, the mask test, 0.005 `u`, and the sort by key, `2 log₂ V_u × 0.0025 u`. At `u = 6 µs`, `δ = 1.5`, `V_u = 20,000`: `c_v = 1.5 × 0.12 + 0.015 + 0.135 + 0.03 + 0.43 ≈ 0.79 µs` a row.

2. The per user index reads the reader's range in key order, index only: `c_m = 0.043 u = 0.26 µs` a row, measured in [Scaling](./scaling.md). So `Δc = c_v − c_m = 0.53 µs`.

3. Over a period in which a pair is listed `L` times and its inputs change `W` times, the lazy index costs `L c_m + min(W, L) t` plus rent, and evaluation costs `L c_v`. Materialization wins when `L Δc > min(W, L) t + rent`, which for `L > W` is

```
L / W  >  t / Δc  =  6 / 0.53 ≈ 11   at t = 6 µs,   19 / 0.53 ≈ 36   at t = 19 µs
```

4. Rent is the storage of a pair, 350 B, against the CPU it saves. At 2026 Azure prices (Result 9) a listed row saves `0.53 µs × 4.38 × 10⁻⁵ $/s = 2.3 × 10⁻¹¹ $`, and a pair costs `5.1 × 10⁻¹¹ $` an hour on Premium SSD v2 and `1.7 × 10⁻⁹ $` an hour in memory. The pair pays its disk rent when listed more than 2.2 times an hour and its memory rent when listed every 50 s or more often. The buffer cache decides memory residency by exactly this five minute rule inequality, so the design stores every pair on disk and lets the listed ones be cached.

5. A Roaring bitmap stores a set of 32 bit integers at about 2 bytes per element in array containers. It needs objects numbered densely, so a listing ends in one descent per row to map an ordinal back to `(object_type, object_id)`, `t_d = 2.5 µs`, ten times `c_m`; and a bitmap holds membership only, while the pair holds its mask, its provenance for the actor token, its scope and its usage. With storage cheaper than CPU the row wins by `(t_d − c_m) / c_m ≈ 9`.

**Sources.** Hanson and Chirkova and Yang as in Result 4. J. Gray and G. R. Putzolu's five minute rule, restated with today's ratios by R. Appuswamy, R. Borovica-Gajic, G. Graefe, A. Ailamaki, [The Five-minute Rule Thirty Years Later and its Impact on the Storage Hierarchy](https://renata.borovica-gajic.com/data/adms2017_5minuterule.pdf), ADMS 2017: the break-even interval is a technology ratio times an economic ratio. D. Lemire, G. Ssi-Yan-Kai, O. Kaser, [Consistently faster and smaller compressed bitmaps with Roaring](https://arxiv.org/abs/1603.06549), Software: Practice and Experience 2016.

**For claimius.** `user_object` is the listing index and only that: `get_objects`, `ObjectWhere`, `search` and the graphs read it, each after settling the reader's slice of the areas it targets. Access sets are rows, not bitmaps. Access data is read far more often than it changes, a person opens lists many times between changes to their grants, so `L / W` is above 36 for every active user, and the pairs of the others are what Result 6 leaves unsettled.

## Result 6: the drain settles only slices likely to be read first

**Claim.** Settling a holder's slice ahead of a read is worth its cost only when the holder is more likely to read before the area is raised again than after. With Poisson reads at rate `r_j` and raises at rate `λ_a`, the drain takes slice `j` when `r_j ≥ λ_a`.

**Proof.** A settle paid ahead costs `A_j t` and saves the reader a wait of `A_j t / σ` if the read comes first. If the area is raised first, the settle is repeated and the first one was wasted. With independent Poisson processes the read comes first with probability `r_j / (r_j + λ_a)`. The drain spends `A_j t` for an expected useful settle of `A_j t × r_j / (r_j + λ_a)`, at least half of what it spends exactly when `r_j ≥ λ_a`. Below that, most of the drain's work on the slice is thrown away, and the reader pays the same `A_j t` on demand, once.

The estimators are the ones claimius already holds: `r̂_j = 1 / (now − last_used_at)` from `user_activity`, and `λ̂_a = raise_count / (now − sa_created_at)` from the area row, `raise_count` incremented by each `ON CONFLICT` raise.

**Sources.** The order among the slices the drain does take is Smith's rule, in [Materialization](./materialization.md). The race of two exponential clocks: any text on Poisson processes, for example the [competing exponentials](https://en.wikipedia.org/wiki/Exponential_distribution#Distribution_of_the_minimum_of_exponential_random_variables).

**For claimius.** The drain takes the slices whose holder satisfies `r̂_j ≥ λ̂_a`, most recent first, and leaves the rest for their holder's own next listing. The work of a binding change that no one reads is never done, which was the reason for settling on demand in the first place.

## Result 7: the minimal set of stored relations

**Claim.** The relations claimius stores beyond its base tables are exactly those without which a query of the model leaves its latency target: the up arrays on `object_info`, `user_object`, `user_users`, and the maintenance state `access_diff` and `user_activity`.

**Proof.** For each query, the cheapest plan without the relation:

1. Without the up arrays, a check walks `up` one level per statement, `D` dependent probes in a recursive query, and a settle finds `desc(y)` by a recursive descent; with them each is one probe. They are the only recursion (Result 1), so no other relation can replace them.

2. Without `user_object`, a listing costs `c_v` instead of `c_m` per row and loses key order, so keyset paging of [Reads](./reads.md) becomes a sort (Result 5).

3. Without `user_users`, `get_users` for viewer `u` is the self join `A(u, ·) ⋈_x A(·, ·)`, `Σ_{x ∈ V_u} U_x` tuples, about 38,000 for a user reaching 2,000 objects each reached by 19 users, where the stored relation is one index range of the users actually shared.

4. `access_diff` and `user_activity` are the state of lazy maintenance and of Result 6.

5. `claim_graph_cache` stores a rendering, not a relation of the model, and is validated on read, see [Graphs](./graphs.md). The check stores nothing (Result 4).

**For claimius.** Nothing else is materialized, and the closure table of one row per ancestor pair does not exist.

## Result 8: counting keeps `user_users` exact

**Claim.** `user_users` is maintained exactly by adding `±1` to `sharing_object_count(u, v)` for every `v ≠ u` on `x` when a pair `(u, x)` enters or leaves `user_object`, summed per `(u, v)` within one settle.

**Proof.** `sharing_object_count(u, v) = |{x : (u, x) ∈ P ∧ (v, x) ∈ P}|` for the pair relation `P`, a nonrecursive view. A change `Δ⁺P` or `Δ⁻P` of one tuple `(u, x)` changes the count by exactly one for each `v` with `(v, x) ∈ P`, which is the counting algorithm of Gupta, Mumick and Subrahmanian on the join; summing the deltas of one statement before writing is the same count computed once per `(u, v)`.

**For claimius.** The settle writes the distinct pairs, not the increments, see [Users](./users.md).

## Result 9: what the prices of then and now decide

The thesis under test: the central systems were designed when storage and memory were dear against CPU and network, so they keep state minimal and compute on demand across a network; today storage and memory are cheap, which moves the optimum to materializing access beside the data.

### The price series

| Quantity | 2013 | 2026 | Factor | Source |
|---|---|---|---|---|
| Memory, cloud, `$` per GiB hour | 0.01667 | 0.00394 | 4.23 cheaper | 2013: `m2.2xlarge` 34.2 GiB at 0.82 and `m3.xlarge` 15 GiB at 0.50 `$`/h, both 4 vCPUs and 13 ECU, so 19.2 GiB cost 0.32 `$`/h ([AWS on demand prices, archived 2013-05-16](http://web.archive.org/web/20130516044534/http://aws.amazon.com/ec2/pricing/json/linux-od.json); [instance specifications, archived 2012-11-28](http://web.archive.org/web/20121128020902/http://aws.amazon.com/ec2/)). 2026: `r7i.xlarge` 32 GiB at 0.265 and `m7i.xlarge` 16 GiB at 0.202, both 4 vCPUs ([r7i.xlarge](https://instances.vantage.sh/aws/ec2/r7i.xlarge), [m7i.xlarge](https://instances.vantage.sh/aws/ec2/m7i.xlarge)) |
| vCPU, cloud, `$` per hour | 0.0625 | 0.03475 | 1.80 cheaper | the same pairs, the memory share subtracted |
| Speed of one thread | 1,467 | 3,113 | 2.12 faster | PassMark single thread, [Xeon E5-2670](https://www.cpubenchmark.net/cpu.php?cpu=Intel+Xeon+E5-2670+%40+2.60GHz&id=1220) behind M3 ([AWS, M3 on E5-2670](https://aws.amazon.com/about-aws/whats-new/2012/10/31/announcing-amazon-ec2-m3-instances-and-m1-price-drop/)) and [Xeon Platinum 8488C](https://www.cpubenchmark.net/cpu.php?cpu=Intel+Xeon+Platinum+8488C&id=5621), the Sapphire Rapids part of the 7th generation |
| CPU work, `$` per 2013 vCPU hour of work | 0.0625 | 0.01638 | 3.82 cheaper | the two rows above |
| Memory, retail, constant 2020 `$` per GB | 4.13 (2012) | 1.09 (2023) | 3.79 cheaper | [McCallum via Our World in Data](https://ourworldindata.org/grapher/historical-cost-of-computer-memory-and-storage) |
| SSD, retail, constant 2020 `$` per GB | 0.694 | 0.026 (2023) | 26.8 cheaper | the same series |
| Block SSD, cloud, `$` per GB hour | 0.000137 (gp2, 2014) | 0.000146 (Premium SSD v2, Norway East) | 1.07 dearer | [AWS, gp2 at 0.10 `$`/GB-month](https://aws.amazon.com/blogs/aws/new-ssd-backed-elastic-block-storage/); [Azure retail prices API](https://prices.azure.com/api/retail/prices) |
| Round trip in one datacenter | 0.5 ms (2009) | under about 2 ms between zones | not faster | J. Dean, [LADIS 2009 keynote](https://www.cs.cornell.edu/projects/ladis2009/talks/dean-keynote-ladis2009.pdf); [Azure availability zones](https://learn.microsoft.com/en-us/azure/reliability/availability-zones-overview) |

Zanzibar had been "in production use for more than 5 years" in 2019, so its design falls in the 2013 column; SpiceDB, OpenFGA and Keto implement its design. The directory systems are older still: memory was 1,045 `$` per GB in 2000 in the same series, 960 times its 2023 price, and they carry memberships in the token rather than per object state. In 2003 Gray priced the same trade, one dollar buying about 8 CPU hours, 1 GB of disk, or 1 GB sent over the WAN, and concluded that "one puts computing as close to the data as possible in order to avoid expensive network traffic" ([Distributed Computing Economics](https://arxiv.org/abs/cs/0403019)).

### The break-even

**Claim.** A derived tuple of width `w` bytes, held in a tier of price `π_s` per byte hour, saving `Δc` CPU seconds at price `π_c` each time it is read, costing `t` to maintain on each of its `W` changes, is worth storing when

```
r > r*  =  w π_s / (Δc π_c)          and          L / W > t / Δc
```

with `r` its reads an hour and `L` its reads over the `W` changes. The break-even interval is `T* = 1 / r* = (Δc / w) × (π_c / π_s)`, a technology ratio times an economic ratio, the form of the five minute rule.

**Proof.** Storing costs `w π_s` an hour and `t π_c` per change; not storing costs `Δc π_c` per read. Over an hour with `r` reads and `(W / L) r` changes, storing wins when `r Δc π_c > w π_s + (W / L) r t π_c`. Dividing by `r Δc π_c`:

```
r* / r  +  (W / L) (t / Δc)  <  1
```

Both terms are positive, so each inequality above is necessary, and together with a margin they are sufficient: the exact condition is this sum.

**What the prices do to it.**

1. Memory. `T*` moves with `π_c / π_s`. From 2013 to 2026 cloud memory fell 4.23 times and CPU work 3.82 times, so `T*` for memory residency grew by `4.23 / 3.82 = 1.11`. Retail memory fell 3.79 times over the same years. **The thesis is refuted for memory**: memory is no cheaper against CPU work than when Zanzibar was designed.

2. Owned SSD. Retail SSD fell 26.8 times against CPU work's 3.82, so `T*` for a tuple kept on owned flash grew `26.8 / 3.82 = 7.0` times. **The thesis holds for owned flash.**

3. Cloud block storage. It is priced for replication and IOPS, not capacity, and did not fall: against CPU work it became `3.82 × 1.07 = 4.1` times dearer. **The thesis fails for managed disks**, which is where claimius runs today on Azure.

4. The maintenance term `t / Δc` is a ratio of CPU times on the same core and has no price in it. It decided the design in 2013 and decides it now.

5. The network. A round trip has not become shorter while a thread has become 2.12 times faster, so a remote evaluation costs 2.12 times more work units of waiting than in 2013. The claimius check, `c_r / σ ≈ 0.2 ms` on a `D2ds_v5`, is below one datacenter round trip in both years. **The thesis holds for the network term, and this is the term that decides.**

6. Zanzibar's own reasons are not prices. The paper states "To facilitate consistency, Zanzibar avoids storage denormalization and relies only on normalized data", and its one denormalization, Leopard, emits tens of thousands of index events for one tuple change. Both are consequences of Result 2 and of global consistency across more than 30 replicas, and neither depends on the year.

### The crossover and where each system falls

| Decision | Governing inequality | 2013 | 2026 |
|---|---|---|---|
| Store the per user pair for listings | `L / W > t / Δc ≈ 11 to 36` | Yes for active users | Yes for active users |
| Keep a listed pair in memory | `T*` about 50 s on Azure memory | Cached by recency | The same, within 11 % |
| Keep every pair on disk | `r* ≈ 2.2` listings an hour on managed SSD | Yes | Yes |
| Store the check | `Δc_check ≤ 0` by Result 4 | No | No |
| Evaluate beside the data | round trip `> c_r / σ` | Yes | Yes, by 2.12 times more |

**For claimius.** The optimum is the same in both years: materialize what a listing enumerates, evaluate what a bounded probe answers, and do both in the database that holds the data. Claimius beats the central systems not because bytes became cheap, which for memory they did not, but because its model is bounded (Result 2), its writes are constant (Results 1 and 6), and its answer runs where no network round trip is paid, the one cost that has not fallen since any of them was designed.
