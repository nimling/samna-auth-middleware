---
gate: samna-developer
layout: doc
title: Claimius
description: The Postgres schema that governs access for every Samna service, stated as a fixed Datalog program over its tables, with the rows each part stores, reads and writes as functions of the data, and the Go middleware that carries it for Samna Auth.
tags: claimius, access control, authorization, formal model, datalog, prophet, disciple, user_object, object_info, inheritance_info, access_diff, claim, middleware
lastUpdated: true
---

# Claimius

Claimius is a PostgreSQL schema named `claimius`. It is the access control system every Samna service runs inside its own database. It answers one question, what may this user or client do to this object, and it answers it from a per user index of pairs in the database that already holds the service's data.

Claimius owns and validates its own types. It reads no more of the implementing side's rows than the `sa_*` columns, `name` and `description`. To claimius an object is an `object_type` and `object_id` pair that a `claim_object` binds and that `user_object` records access for. The implementing side registers its tables with `upsert_table_info`, binds claims to its rows, and reads the answer through functions.

## Two halves

1. Claimius is the schema: the model, the tables, the functions and their guarantees, independent of any language or host. Everything under "Claimius" below, and every formula in these files, is derived from the SQL of the two role sets.

2. The middleware, `samna_auth_middleware`, is the Samna Auth centric implementation of claimius in Go. It carries the schema for Samna Auth as the prophet and for every product service as a disciple, and adds authentication, routed writes, the pull loop, the usage counter and the Go access calls. Everything under "The middleware" below.

## Claimius

| File | Covers |
|---|---|
| [In plain words](./plain.md) | What claimius is, how a check is answered, what a change costs, and what it takes to run it in your own PostgreSQL, without the formulas |
| [Types](./types.md) | The four types, the access relation, how the common models reduce to them, the reachability bound |
| [Organizations](./organizations.md) | Roots, children, the ownership closure and what each write costs it |
| [Locations](./locations.md) | The location closure, pins, a location in two trees |
| [Objects](./objects.md) | Registration, `table_info`, the `inheritance_info` closure, `object_info` |
| [Users](./users.md) | `samna_user`, machine clients, delegation, `user_users`, its count and its fold |
| [Claims](./claims.md) | Grants, bindings, the bits, the effective mask, composition, windows, actor tokens, the gate |
| [Denials](./denials.md) | Deny classes, subtraction, closure under implication, what a creator keeps |
| [Redaction](./redaction.md) | Scopes on object bindings as field masks |
| [Capabilities](./capabilities.md) | Scope grants with no object and `get_scope_access` |
| [Apps](./apps.md) | `samna_app`, the two scoping mechanisms, the identity helpers |
| [Materialization](./materialization.md) | `user_object` and the pair's mark, the reader's cursor, `access_diff` keyed by transaction and root, the set based share settle, the `user_users` fold for viewers, the locks a settle takes, the drain rule and the trim |
| [Reads](./reads.md) | Every read function and what it reads and writes |
| [Writes](./writes.md) | Every write and what it does inside its transaction |
| [Search](./search.md) | Search inside a scope, with or without a query, from the smallest of the reader's pairs, the scope and the matches, extended by host search sources |
| [Graphs](./graphs.md) | The owner and claim graphs, windows, the snapshot validated cache |
| [Usage](./usage.md) | The per pair usage count, relations, and the prophet's capture |
| [Sync](./sync.md) | The outbox of source rows, the `(txid, seq)` cursor, pages, trimming |
| [Assets](./assets.md) | `object_asset`, the shared link from a home app's asset to a claimius object, which apps it reaches, and fetching the file through the prophet |
| [Cost](./cost.md) | `app_cost`, the hourly cost rows every app reports to the prophet, who writes them, how they sync and how they are read back in buckets |
| [Prophets and disciples](./prophets.md) | Roles, ids, federation with peers, homes, handovers, failure |
| [Security](./security.md) | Roles, definer reads, secrets, a compromised disciple |
| [Audit](./audit.md) | The trail, written in the change's transaction, on monthly partitions |
| [Storage](./storage.md) | Rows, bytes and index entries per table and per write |
| [Comparison](./comparison.md) | Claimius against Zanzibar and its descendants and against Entra ID, in counts |
| [A worked example](./example.md) | The tree live on the tst and prd prophets, Samna Auth, Samna, Nimtech, Executive and Consult, with its claims, scopes and inheritance |

## The middleware

| File | Covers |
|---|---|
| [The middleware](./middleware/index.md) | What the Go module carries, package by package, and authentication |
| [Access in Go](./middleware/access.md) | The lookups, error mapping, listings, graphs, the usage counter, replica routing |
| [Writes in Go](./middleware/writes.md) | Routed and local writes, stamping the actor, audit, debug mode |
| [Sync in Go](./middleware/sync.md) | The `/sam/sync` routes, the pull loop, peers |

## How access is decided

1. A claim is a permission template: a mask of bits and an inherit flag. A `user_claim` grants the claim to one user, optionally inside a window from `starts_at` to `ends_at`. A `claim_object` binds the claim to one object, optionally with a binding mask that narrows the claim's mask and never widens it.

2. The bits are owner 1, write 2, read 4 and execute 8, with deny 16 and member 32. Owner implies write, read and execute; write implies read and execute; execute implies read. A mask with a bit but not the bits it implies keeps only what it fully implies.

3. Every object sits in up to three trees: ownership through `sa_owner_id`, location through `sa_location_id` and a location's `sa_parent_id`, and parenthood through a registered table's own `sa_parent_id`. A location is a node of the location tree under its parent and of the ownership tree under its owner. A binding whose claim and binding both inherit reaches every descendant of the bound object in every tree.

4. A binding whose object is another claim composes the two claims: a holder of the first gains the bindings of the second. At equal levels the composed grant carries only the bits both sides carry, and a deny on either side makes the result a deny; at different levels it takes the higher level and that claim's mask decides. Composition is followed one hop and never further.

5. Holding a claim, or holding a claim bound on it, lets the holder read the claim itself.

6. The creator of a row, the user behind its `sa_created_by` grant, holds full access to it less what a direct deny, or a deny of a higher level, takes away. A row whose owning organization is held but out of the ownership closure, soft deleted or under a soft deleted organization, keeps nothing by creation. A stamp that names a user directly reads as that user; see [Users](./users.md#one-identity-one-row).

7. A deny is a claim with bit 16 set; its other bits name what it subtracts. Every claim carries a level, 0 unless set, and when a grant and a deny meet at different levels the higher level wins: a deny of a higher level subtracts from the grant whatever its class, a deny of a lower level does not touch it. At equal levels the classes decide: a deny bound directly on the object subtracts from every grant on it; a deny reached through a tree subtracts only from grants reached through a tree. Denies may be scoped to fields. What is left is closed under implication. See [Denials](./denials.md) and [Redaction](./redaction.md).

8. The access of a user on an object is the OR of what every surviving grant and the creator rule leave. A nonzero result is one row of `user_object` holding the mask, the surviving derivations in `grants`, the field masks in `scope`, and `direct_grant` for the creator. A grant outside its time window is not counted by the reads; see [Claims](./claims.md#time-windows).

The exact rules are in [Math](#math).

## What claimius stores

Beyond its source tables, claimius keeps these. None is written by application code.

| Stored | Holds |
|---|---|
| `inheritance_info` | every ancestor of every node in every tree, self edges included, one row per tree, ancestor and descendant |
| `object_info` | each live registered object's owner, location, root, name, description, link and search vector, once |
| `user_object` | each nonzero pair of user and object with its mask, derivations, field masks and usage count |
| `user_users` | for each viewer who reads the directory, the number of objects that are not claims it shares with each other user |
| `access_diff` | the areas of changes since the oldest active cursor, each stamped with its transaction and its owner root |
| `user_users_diff` | the pairs that entered or left `user_object` on an object that is not a claim while a viewer existed, not yet folded into `user_users` |
| `usage_diff` | counted uses of pairs not yet folded into `used_count` |
| `user_activity` | each user's cursor, the snapshot `xmin` of its last share settle, when it last viewed the directory and when it last used the deployment |
| `claim_graph_cache` | the default claim graph of a user with the snapshot it was built under |

`access_diff` with the cursors and marks is the state of lazy maintenance of `user_object`, `user_users_diff` that of `user_users`, `usage_diff` that of the usage count on the pair, and `user_activity` the cursor and the inputs of the drain rule; see [Materialization](./materialization.md). `claim_graph_cache` stores a rendering, validated on read; see [Graphs](./graphs.md). Nothing else is materialized. The member listing of an object is the reverse index `idx_user_object_object_user` on the `user_object` rows, not a second table.

## How each question is answered

| Question | Served by |
|---|---|
| May user `u` do `r` to object `x` | one pair of `user_object`, read after its mark proves it current against the areas of its root |
| Which objects of a type may `u` reach | the reader's range of the `user_object` primary key, in `object_id` order |
| Search | that range joined to `object_info` on its search vector |
| Who holds `x` | `idx_user_object_object_user` |
| Which users does `u` see | `idx_user_users_viewer`, with the pending `user_users_diff` rows of the objects of `u` folded in |
| Does `u` hold a capability at path `p` | the grants, claims and objectless bindings directly; see [Capabilities](./capabilities.md) |

## Principles

1. Access is a mask on a binding. A claim says what may be done, a `user_claim` says who holds it, a `claim_object` says what it covers. Nothing else grants access, except the creator of a row.

2. The only recursion is the up edge of three trees, stored as a closure. Composition is one hop. The access relation is therefore a nonrecursive view over stored relations, maintained exactly by recomputing the pairs a change can affect; see [Why the view is maintained exactly](#why-the-view-is-maintained-exactly).

3. A write settles only its actor's pairs on the rows it names, and records the rest as one area row keyed on what changed and stamped with its transaction. A listing settles the reader's share of every area raised since its cursor, in one pass; a check settles the one pair it asks about when an area on its nodes is newer than the pair's mark; and an area leaves when every active cursor has passed it. A share no one reads is never computed. One level up the same: a pair entering or leaving raises one `user_users_diff` row while anyone views the directory, and the read that shows who sees whom folds the rows it needs. No write fans out, and no settle takes a lock per object or per area.

4. Every read answers from stored pairs, and neither a check nor a listing walks a tree. Only `get_direct_access`, the fallback of the gate, walks the ancestors of one object.

5. The answer lives in the service's own Postgres. A disciple answers every access question from its own copy without a network call, and the feed carries only source rows.

6. Claimius knows its own types and nothing else. The implementing side's tables join by registration, and nothing about them is written into a claimius function.

## Math

Claimius is one fixed program over its tables. Writing authorization as Datalog is the approach of logic based security languages such as J. DeTreville's [Binder](https://www.microsoft.com/en-us/research/wp-content/uploads/2016/02/tr-2002-21.pdf); claimius's rules never change, which is what lets each property in these files be proved once.

### Base relations

| Relation | Table | Meaning |
|---|---|---|
| `org(o, p)` | `organization` | owner `p = sa_owner_id`, `p = o` on a root |
| `loc(l, p, o)` | `location` | parent location `p = sa_parent_id`, owning organization `o` |
| `obj(x, o, l, q)` | a registered table | owner `o`, location pin `l`, parent `q` in its own tree, each possibly absent |
| `claim(c, m, i)` | `claim` | mask `m = sa_access`, inherit flag `i` |
| `grant(g, u, c)` | `user_claim` | live grant `g` of claim `c` to user `u`, with a window `[starts_at, ends_at)` |
| `bind(c, y, b, i, s)` | `claim_object` | binding of `c` on object `y`, binding mask `b`, inherit flag `i`, scope `s` |
| `link(a, c, l, i)` | `claim_object` with `object_type = 'claimius.claim'` | claim `a` composes claim `c` with link mask `l` |
| `cap(c, s, b)` | `claim_object` with no object | a scope grant of `c` carrying payload `s` |
| `creator(x, g)` | `sa_created_by` | the grant row `x` was created under |

### The tree relation

Each object has up edges in up to three trees `t`: ownership through `sa_owner_id`, location through `sa_location_id` and a location's `sa_parent_id`, parenthood through a registered table's own `sa_parent_id`. A location is a node of the location tree under its parent and of the ownership tree under its owner.

```
up_o(x, y) ← obj(x, y, _, _) ; org(x, y), x ≠ y ; loc(x, _, y)
up_l(x, y) ← obj(x, _, y, _) ; loc(x, y, _), x ≠ y
up_q(x, y) ← obj(x, _, _, y), x ≠ y
anc_t(x, x, 0)
anc_t(x, z, h + 1) ← anc_t(x, y, h), up_t(y, z)
```

`inheritance_info` stores `anc_t` for every tree, self edges included, one row per `(tree_type, ancestor, descendant)`. It is the only recursive relation, and it is stored.

### Masks

The four grant bits are owner 1, write 2, read 4 and execute 8; deny is 16 and member 32. Two functions on masks:

```
X(m)     = m | 14·[m & 1] | 12·[m & 2] | 4·[m & 8]                               _expand_access
close(m) = (m & ~15) | 4·[m & 4] | 8·[m & 12 = 12] | 2·[m & 14 = 14] | 1·[m & 15 = 15]   _close_access
```

`X` adds what a bit implies: owner implies write, read and execute, write implies read and execute, execute implies read. `close` keeps a bit only when every bit it implies is present. The mask a grant carries through a binding, and through a composition link, is

```
eff(c, b)          = m_c & X(coalesce(b, m_c))                                 _effective_access
comp(a, l, c, b)   = (eff(a, l) & eff(c, b) & ~16) | ((eff(a, l) | eff(c, b)) & 16)
```

so a binding mask narrows its claim and never widens it, and a composed grant carries the bits both sides carry, a deny on either side making it a deny.

### Derivations

A grant reaches an object directly, through one tree, or through one composition link; each derivation carries a class, `direct` or the name of its tree:

```
reach(u, x, e, direct) ← grant(_, u, c), bind(c, x, b, _, _),                          e = eff(c, b)
reach(u, x, e, t)      ← grant(_, u, c), claim(c, _, true), bind(c, y, b, true, _),
                         anc_t(x, y, h), h > 0,                                          e = eff(c, b)
reach(u, x, e, direct) ← grant(_, u, a), link(a, c, l, true), claim(c, _, true),
                         bind(c, x, b, _, _),                                            e = comp(a, l, c, b)
reach(u, x, e, t)      ← grant(_, u, a), link(a, c, l, true), claim(c, _, true),
                         bind(c, y, b, true, _), anc_t(x, y, h), h > 0,                  e = comp(a, l, c, b)
reach(u, c, 4, direct) ← grant(_, u, c) ; grant(_, u, a), bind(a, c, b, _, _), eff(a, b) & 16 = 0
made(u, x)             ← creator(x, g), grant(g, u, _) ; creator(x, u)
```


The creator of a row is the user behind its `sa_created_by`, a grant, or the user it names directly. `made` holds only while the row's owning organization is in the ownership closure or not held at all, so a soft delete that takes a subtree out of the closure takes the creator's access to every row owned in it too. The last rule but one is the claim itself as an object: holding a claim, or holding a claim bound on it, reads it. The composition rules consume a binding of the linked claim and never another link, so a link is followed once. `_build_grants_for_objects` evaluates exactly these rules, set based, for one user on a set of objects.


### The access of a pair

A derivation whose mask carries 16 is a deny of its bits `e & 15`; every other derivation is a grant of `e & 15`. `_apply_denies` settles them, with `X_d` the OR of the unscoped denies of class `direct`, `X_o` the OR of the other unscoped denies, and `F_d(f)`, `F_o(f)` the same for denies scoped to field `f`:

```
H_g    = ⋁ { e_d & 15 : deny d, l_d > l_g }                      the bits of every deny of a level higher than the grant's
D_g(f) = H_g | X_d | F_d(f) | [k_g ≠ direct] (X_o | F_o(f))     the bits denied to grant g of class k_g on field f, X and F over the denies of the same level
a_g(f) = close(e_g & 15 & ~D_g(f))                              f ∈ s_g for a scoped grant, every field otherwise
a_c(f) = close(15 & ~(H_c | X_d | F_d(f)))                      the creator, a direct grant at level 0, when made(u, x)
A(u, x) = ⋁_g ⋁_f a_g(f)  |  ⋁_f a_c(f)
M(u, x, f) = ⋁ over grants and the creator of a(f)               the field mask of redaction
```

A grant left with no bit is dropped. `A(u, x) ≠ 0` is a row of `user_object` with `sa_access = A`, the surviving derivations in `grants`, the field masks in `scope`, NULL when every field carries the same mask, and `direct_grant` for the creator. See [Denials](./denials.md) and [Redaction](./redaction.md). The time window of a grant is not part of `A`: the reads keep a derivation only while its grant is inside its window; see [Claims](./claims.md#time-windows).


### Stored relations

| Stored | Relation | Rows |
|---|---|---|
| `inheritance_info` | `anc_t` for every tree, self edges included | `Σ_x Σ_{t ∋ x} (d_t(x) + 1)` |
| `object_info` | the per object columns of `obj` | `O`, one per live registered object |
| `user_object` | `A(u, x) ≠ 0` with its derivations, field masks and usage count | `P = Σ_u V_u` |
| `user_users` | `|V_u ∩ V_v|` over objects that are not claims, for the viewers `u` | `Σ_{u viewing} S_u` |
| `access_diff` | the support keys of changes since the oldest active cursor | at most one per object raised and one per assignment |
| `user_users_diff` | first order deltas of `user_users` | one per pair change between folds |
| `usage_diff` | pending usage deltas | one per pair and flush between the listings of its user |
| `user_activity` | cursor and drain estimators | one per user that used the deployment |
| `claim_graph_cache` | a rendering | one per user who opened it |

### The queries in relational algebra

| Query | Relational algebra |
|---|---|
| Check `(u, x, r)` | `σ_{u, x, A & r = r} A` |
| Listing `(u, τ, r)` | `π_x σ_{u, type = τ, A & r = r} A` |
| Search | the listing restricted by `search_vector @@ q` |
| Members of `x` | `π_u σ_x A` |
| Visible users of `u` | `π_v σ_u user_users`, with the pending `user_users_diff` rows of the objects of `u` folded in |
| Capability `(u, p, r)` | `σ_{object = ⊥, jsonpath p} (grant ⋈ claim ⋈ cap)` |

### Symbols

Every formula in these files uses these symbols.

| Symbol | Meaning |
|---|---|
| `U`, `O` | Users of an app, live registered objects of an app |
| `D_o`, `D_l`, `D_q` | Depth of the ownership, location and parenthood trees |
| `d_t(x)`, `a_x` | Ancestors of `x` in tree `t`; `a_x = Σ_t d_t(x)`, the closure rows of `x` with `depth > 0` |
| `desc(y)`, `n_S` | The descendants of `y` in every tree; the size of a subtree `S` |
| `C` | Live grants a user holds in an app |
| `L` | Composition links out of those claims |
| `b_y`, `B_x` | Bindings on `y`; bindings on `x` and its ancestors, `B_x = Σ_{y ∈ {x} ∪ anc(x)} b_y` |
| `G_x` | Derivations in the `grants` array of one pair |
| `V_u`, `V_{u,τ}` | Objects user `u` reaches, of any type and of type `τ` |
| `U_x` | Users reaching `x`, the pairs of `x` in `idx_user_object_object_user` |
| `S_u` | Users `u` shares an object that is not a claim with, the `user_users` rows of `u` |
| `m_x`, `T_u` | `user_users_diff` rows pending on `x`; the objects with rows pending that `u` holds a pair on or has a row of its own on |
| `T_c` | Objects claim `c` reaches: its bound objects and, where it cascades, their descendants |
| `H_c` | Holders of `c`: users granted `c` or a claim linking to `c` |
| `N_r` | Closure rows of one root in one tree |
| `Δ_a` | Open `access_diff` areas of the deployment |
| `P` | Pairs, rows of `user_object` |
| `p`, `h` | Rows of a page; levels of a B-tree |
| `k` | Users granted in one request |


## Proofs and measurements

### Why the view is maintained exactly

With `anc_t` stored in `inheritance_info`, the access relation `A` is a nonrecursive stratified Datalog program: no tuple of `A` takes part in the derivation of another tuple of `A`. Every change to the base relations is therefore maintained exactly by recomputing the pairs its support can reach, and never needs a delete and rederive step.

1. The rules for `reach` in [Derivations](#derivations) read `grant`, `claim`, `bind`, `link`, `creator` and `anc_t`. The composition rules consume a binding of the linked claim, `bind(c, y, b, _, _)`, and never a `link` of `c`; `_build_grants_for_objects` joins `claim_object link` once and then `claim_object co` on `co.claim_id = link.object_id`, with no second hop. No rule has `reach` or `A` in its body, so the dependency graph is acyclic.

2. `A` aggregates the derivations of one pair `(u, x)` and applies negation, the deny terms of `_apply_denies`, to derivations of that same pair only. Aggregation and negation over a lower stratum are stratified.

3. The only recursion is `anc_t`. Each tree is acyclic by the types of its edges: an organization goes up only to organizations, a location to locations and to its owner, a registered row to its owner, its pin and its own parent, and every root points at itself, `sa_owner_id = id` or `sa_parent_id = id`, which `up_t` excludes. `calc_hierarchical_access` refuses a change of `sa_root_id` outside replay and `migrate_root`, and `_splice_subtree` drops a subtree whose new parent lies inside it, so no cycle is ever stored.

4. Gupta, Mumick and Subrahmanian prove that for nonrecursive views, with negation and aggregation, the counting algorithm computes exactly the view tuples inserted or deleted, where DRed, needed for recursive views, first deletes a superset and rederives it. By 1 to 3 the first case applies.

5. The support of a change is an area. A change to claim `c`, to one of its bindings or to a link into or out of it changes `reach` only on the objects `c` and the claims it links to are bound on, and below them where the bindings cascade, and on the claim object itself: one object area per binding and one on the claim. A change to a grant of `c` changes only the assignee's slice of it: the assignment area of that user. A change to an object `x` changes `reach` only on `x` and, when `x` is a node, on `desc(x)`: the object area, with `cascade`. So recomputing the pairs of an area touches every pair the change can affect, and every area is named by an object or an assignment.

Single hop composition keeps the program nonrecursive, and a nonrecursive program needs only first order deltas, each found by key. The `grants` array of a pair is its why provenance, `user_users.sharing_object_count` the derivation count of the counting algorithm on `A ⋈_x A` (see [Users](./users.md#why-the-count-is-exact)), a `user_users_diff` row one first order delta of that count, and an `access_diff` row the key of a change's support. A transitive link would make `reach` recursive and every settle a DRed step with overdeletion.

**Sources.** A. Gupta, I. S. Mumick, V. S. Subrahmanian, [Maintaining Views Incrementally](https://dl.acm.org/doi/10.1145/170035.170066), SIGMOD 1993. The overdeletion of DRed: B. Motik et al., [Incremental Update of Datalog Materialisation: the Backward/Forward Algorithm](https://www.cs.ox.ac.uk/people/boris.motik/pubs/mnph15incremental-BF.pdf), AAAI 2015. Stored derivations as provenance: T. J. Green, G. Karvounarakis, V. Tannen, [Provenance Semirings](https://web.cs.ucdavis.edu/~green/papers/pods07.pdf), PODS 2007.

### Why each stored relation is needed

Each stored relation is one without which a query of the model reads more than its own answer. For each, the cheapest plan without it:

1. Without `inheritance_info`, every derivation walks `a_x` levels one dependent probe at a time and a settle finds `desc(y)` one node at a time. It is the only recursion, so nothing else replaces it.

2. Without `user_object`, a listing derives `δ R_x` entries per row and sorts; see [Reads](./reads.md#why-the-pairs-are-stored).

3. Without `object_info`, the name, owner, location and search vector of an object are either read from the implementing side's table per row, one dynamic statement per type, or copied onto each of its `U_x` pairs; a rename would then rewrite `U_x` rows and raise an area for every holder. With it, a rename writes one row and raises nothing.

4. Without `user_users`, `get_users` for `u` is the self join `Σ_{x ∈ V_u} U_x` per call, where the stored relation is one range of `S_u` rows; it is paid once, on the user's first view, and never for a user who does not view the directory.
