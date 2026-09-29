---
layout: doc
title: Claimius
description: The Postgres schema that governs access for every Samna service, stated as a fixed Datalog program over its tables, with the rows each part stores, reads and writes as functions of the data, and the Go middleware that carries it for Samna Auth.
tags: claimius, access control, authorization, formal model, datalog, prophet, disciple, user_object, object_info, inheritance_info, access_diff, claim, middleware
lastUpdated: true
---

# Claimius

Claimius is a PostgreSQL schema named `claimius`. It is the access control system every Samna service runs inside its own database. It answers one question, what may this user or client do to this object, and it answers it from a per user index of pairs in the database that already holds the service's data.

Claimius owns and validates its own types. It reads no more of the implementing side's rows than the `sa_*` columns, `name` and `description`. To claimius an object is an `object_type` and `object_id` pair that a `claim_object` binds and that `user_object` records access for. The implementing side registers its tables with `init_claimius_tables`, binds claims to its rows, and reads the answer through functions.

## Two halves

1. Claimius is the schema: the model, the tables, the functions and their guarantees, independent of any language or host. Everything under "Claimius" below, and every formula in these files, is derived from the SQL in `prophet/database` and `disciple/database`.

2. The middleware, `samna_auth_middleware`, is the Samna Auth centric implementation of claimius in Go. It carries the schema for Samna Auth as the prophet and for every product service as a disciple, and adds authentication, routed writes, the pull loop, the usage counter and the Go access calls. Everything under "The middleware" below.

## Claimius

| File | Covers |
|---|---|
| [Theory](./theory.md) | The results the design rests on, each with its proof and its sources |
| [Types](./types.md) | The four types, the access relation, how the common models reduce to them, the reachability bound |
| [Organizations](./organizations.md) | Roots, children, the ownership closure and what each write costs it |
| [Locations](./locations.md) | The location closure, pins, a location in two trees |
| [Objects](./objects.md) | Registration, `table_info`, the `inheritance_info` closure, `object_info` |
| [Users](./users.md) | `samna_user`, machine clients, delegation, `user_users` and its count |
| [Claims](./claims.md) | Grants, bindings, the bits, the effective mask, composition, windows, actor tokens, the gate |
| [Denials](./denials.md) | Deny classes, subtraction, closure under implication, what a creator keeps |
| [Redaction](./redaction.md) | Scopes on object bindings as field masks |
| [Capabilities](./capabilities.md) | Scope grants with no object and `get_scope_access` |
| [Apps](./apps.md) | `samna_app`, the two scoping mechanisms, the identity helpers |
| [Materialization](./materialization.md) | `user_object`, the per pair and per reader settle, `access_diff`, the drain rule |
| [Reads](./reads.md) | Every read function and what it reads and writes |
| [Writes](./writes.md) | Every write and what it does inside its transaction |
| [Search](./search.md) | Full text search ranked by text and decayed usage |
| [Graphs](./graphs.md) | The owner and claim graphs, windows, the snapshot validated cache |
| [Usage](./usage.md) | The per pair usage count, relations, and the prophet's capture |
| [Sync](./sync.md) | The outbox of source rows, the `(txid, seq)` cursor, pages, trimming |
| [Prophets and disciples](./prophets.md) | Roles, ids, federation with peers, homes, handovers, failure |
| [Security](./security.md) | Roles, definer reads, secrets, a compromised disciple |
| [Audit](./audit.md) | The trail, written in the change's transaction, on monthly partitions |
| [Storage](./storage.md) | Rows, bytes and index entries per table and per write |
| [Comparison](./comparison.md) | Claimius against Zanzibar and its descendants and against Entra ID, in counts |

## The middleware

| File | Covers |
|---|---|
| [The middleware](./middleware/index.md) | What the Go module carries, package by package, and authentication |
| [Access in Go](./middleware/access.md) | The lookups, error mapping, listings, graphs, the usage counter, replica routing |
| [Writes in Go](./middleware/writes.md) | Routed and local writes, stamping the actor, audit, debug mode |
| [Sync in Go](./middleware/sync.md) | The `/sam/sync` routes, the pull loop, peers |

## The model

Claimius is one fixed program over its tables. Writing authorization as Datalog is the approach of logic based security languages such as J. DeTreville's [Binder](https://www.microsoft.com/en-us/research/wp-content/uploads/2016/02/tr-2002-21.pdf); claimius's rules never change, which is what lets each result in these files be proved once.

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

The four grant bits are owner 1, write 2, read 4 and execute 8; deny is 16 and member 32. Two functions on masks, from `V2.1__claimius_functions_base.sql`:

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

The creator of a row is the user behind its `sa_created_by`, a grant, or the user it names directly, which is how the bootstrap grant `ensure_app_user` writes is stamped. The last rule but one is the claim itself as an object: holding a claim, or holding a claim bound on it, reads it. The composition rules consume a binding of the linked claim and never another link, so a link is followed once. `_build_grants_for_objects` evaluates exactly these rules, set based, for one user on a set of objects of one type.

### The access of a pair

A derivation whose mask carries 16 is a deny of its bits `e & 15`; every other derivation is a grant of `e & 15`. `_apply_denies` settles them, with `X_d` the OR of the unscoped denies of class `direct`, `X_o` the OR of the other unscoped denies, and `F_d(f)`, `F_o(f)` the same for denies scoped to field `f`:

```
D_g(f) = X_d | F_d(f) | [k_g ≠ direct] (X_o | F_o(f))           the bits denied to grant g of class k_g on field f
a_g(f) = close(e_g & 15 & ~D_g(f))                              f ∈ s_g for a scoped grant, every field otherwise
a_c(f) = close(15 & ~(X_d | F_d(f)))                            the creator, when made(u, x)
A(u, x) = ⋁_g ⋁_f a_g(f)  |  ⋁_f a_c(f)
M(u, x, f) = ⋁ over grants and the creator of a(f)               the field mask of redaction
```

A grant left with no bit is dropped. `A(u, x) ≠ 0` is a row of `user_object` with `sa_access = A`, the surviving derivations in `grants`, the field masks in `scope`, NULL when every field carries the same mask, and `direct_grant` for the creator. See [Denials](./denials.md) and [Redaction](./redaction.md). The time window of a grant is not part of `A`: the reads keep a derivation only while its grant is inside its window; see [Claims](./claims.md#time-windows).

### What claimius stores beyond the base relations

| Stored | Holds | Rows |
|---|---|---|
| `inheritance_info` | `anc_t` for every tree, self edges included | `Σ_x Σ_{t ∋ x} (d_t(x) + 1)` |
| `object_info` | each live registered object's owner, location, root, name, description, link and search vector, once | one per live registered object |
| `user_object` | `A(u, x) ≠ 0` with its derivations, field masks and usage count | `P = Σ_u V_u` |
| `user_users` | `|V_u ∩ V_v|` over objects that are not claims, for users sharing one | `Σ_u S_u` |
| `access_diff`, `access_diff_user` | the open areas of changes and the shares settled of each | at most one area per claim, per assignment and per object raised |
| `user_activity` | when each user last used each app | one per user and app that used it |
| `claim_graph_cache` | the default claim graph of a user with the snapshot it was built under | one per user who opened it |

### The queries

| Query | Relational algebra | Served by |
|---|---|---|
| Check `(u, x, r)` | `σ_{u, x, A & r = r} A` | one pair of `user_object`, after the pair is settled |
| Listing `(u, τ, r)` | `π_x σ_{u, type = τ, A & r = r} A` | the reader's range of the `user_object` primary key, in `object_id` order |
| Search | the listing restricted by `search_vector @@ q` | `user_object` joined to `object_info` |
| Members of `x` | `π_u σ_x A` | `idx_user_object_object_user` |
| Visible users of `u` | `π_v σ_u user_users` | `idx_user_users_viewer` |
| Capability `(u, p, r)` | `σ_{object = ⊥, jsonpath p} (grant ⋈ claim ⋈ cap)` | the base relations; see [Capabilities](./capabilities.md) |

## Symbols

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
| `T_c` | Objects claim `c` reaches: its bound objects and, where it cascades, their descendants |
| `H_c` | Holders of `c`: users granted `c` or a claim linking to `c` |
| `N_r` | Closure rows of one root in one tree |
| `Δ_a` | Open `access_diff` areas of app `a` |
| `P` | Pairs, rows of `user_object` |
| `p`, `h` | Rows of a page; levels of a B-tree |
| `k` | Users granted in one request |

## Principles

1. Access is a mask on a binding. A claim says what may be done, a `user_claim` says who holds it, a `claim_object` says what it covers. Nothing else grants access, except the creator of a row.

2. The only recursion is the up edge of three trees, stored as a closure. Composition is one hop. The access relation is therefore a nonrecursive view over stored relations, maintained exactly by recomputing the pairs a change can affect; see [Theory](./theory.md#result-1-access-is-a-nonrecursive-view).

3. A write settles only its actor's pairs on the rows it names, and records the rest as one area row keyed on what changed. A check settles the one pair it asks about; a listing settles the reader's own share of each area it meets. An area clears when every holder has settled it, and a share no one reads is never computed.

4. Every read answers from stored pairs, and neither a check nor a listing walks a tree. Only `get_direct_access`, the fallback of the gate, walks the ancestors of one object.

5. The answer lives in the service's own Postgres. A disciple answers every access question from its own copy without a network call, and the feed carries only source rows.

6. Claimius knows its own types and nothing else. The implementing side's tables join by registration, and nothing about them is written into a claimius function.
