---
layout: doc
title: Claimius
description: The Postgres schema that governs access for every Samna service, stated as a program over its tables, with the formula for what it stores, reads and writes per operation as functions of the data, and the Go middleware that carries it for Samna Auth.
tags: claimius, access control, authorization, prophet, disciple, user_object, inheritance_info, access_diff, claim, organization, location, middleware
lastUpdated: true
---

# Claimius

Claimius is a PostgreSQL schema named `claimius`. It is the access control system every Samna service runs inside its own database. It answers one question, what may this user or client do to this object, and it answers it from a per user index of pairs in the database that already holds the service's data.

Claimius owns and validates its own types. It never constructs or reads the implementing side's data beyond the `sa_*` columns and a name and description. To claimius an object is an `object_type` and `object_id` pair that a `claim_object` binds and that `user_object` records access for. The implementing side registers its tables with `init_claimius_tables`, binds claims to its rows, and reads the answer through functions.

## Two halves

1. Claimius is the schema: the model, the tables, the functions and their guarantees, independent of any language or host. Everything under "Claimius" below.

2. The middleware, `samna_auth_middleware`, is the Samna Auth centric implementation of claimius in Go. It carries the schema for Samna Auth as the prophet and for every product service as a disciple, and adds authentication, routed writes, the pull loop and the Go access calls. Everything under "The middleware" below.

## Claimius

| File | Covers |
|---|---|
| [Types](./types.md) | The four types, the access relation, how every common model reduces to them, and why the model stays below the dynamic reachability bound |
| [Organizations](./organizations.md) | Roots, children, the ownership closure, `create_root_organization`, `app_object` |
| [Locations](./locations.md) | The location closure and `sa_location_id` |
| [Users](./users.md) | `samna_user`, `user_id` and `id`, machine clients, delegation, `user_users` and its count |
| [Claims](./claims.md) | Claims, bindings, grants, the access bits, the effective mask, composition, time windows, actor tokens, the single object gate |
| [Denials](./denials.md) | The subtraction formula, what a creator keeps, a worked scenario |
| [Redaction](./redaction.md) | Field scope on an object binding and how scopes merge on a pair |
| [Capabilities](./capabilities.md) | Scope grants with no object, and their cost |
| [Objects](./objects.md) | Registration, `table_info`, the `inheritance_info` closure, the `sa_*` columns |
| [Apps](./apps.md) | `samna_app`, `app_object`, app scoped tables |
| [Materialization](./materialization.md) | `user_object`, `user_users`, `access_diff`, the cost of a settle, and why the maintenance is exact |
| [Reads](./reads.md) | The read surface and what each read reads and writes |
| [Writes](./writes.md) | What each write does inside its own transaction |
| [Search](./search.md) | Full text search over what a user can reach |
| [Graphs](./graphs.md) | The owner and claim graphs and the claim graph cache |
| [Usage](./usage.md) | Per user relations and the prophet's consent gated capture |
| [Sync](./sync.md) | The outbox, what it carries per write, the cursors, the consistency token |
| [Prophets and disciples](./prophets.md) | The authority, its mirrors, ids, federation, failure |
| [Security](./security.md) | Roles, secrets, ephemeral tables, soft delete, a compromised disciple |
| [Audit](./audit.md) | What the trail holds and what one row costs |
| [Storage](./storage.md) | Row widths, indexes, index entries per write, growth |
| [Comparison](./comparison.md) | Claimius against Zanzibar, SpiceDB, OpenFGA and Auth0 FGA, Ory Keto and Entra ID in rows stored, read and written |

## The middleware

| File | Covers |
|---|---|
| [The middleware](./middleware/index.md) | What the Go module carries, package by package, and authentication |
| [Access in Go](./middleware/access.md) | The lookups on `sdb.Instance()`, error mapping, listings |
| [Writes in Go](./middleware/writes.md) | Routed and local writes, stamping the actor, debug mode |
| [Sync in Go](./middleware/sync.md) | The `/sam/sync` routes and the pull loop |

## The model

Claimius is one fixed program over its tables. Writing authorization as Datalog is the approach of logic based security languages such as J. DeTreville's [Binder](https://www.microsoft.com/en-us/research/wp-content/uploads/2016/02/tr-2002-21.pdf); claimius's rules never change, which is what lets each result in these files be proved once.

### Base relations

| Relation | Table | Meaning |
|---|---|---|
| `org(o, p)` | `organization` | `p = sa_owner_id`, `p = o` on a root |
| `loc(l, p, o)` | `location` | parent location `p`, owning organization `o` |
| `obj(x, o, l, q)` | a registered table | owner `o`, location pin `l`, parent `q`, each possibly absent |
| `claim(c, m, i)` | `claim` | mask `m`, inherit flag `i` |
| `grant(g, u, c)` | `user_claim` | live |
| `bind(c, y, b, i, s)` | `claim_object` | object `y`, binding mask `b`, inherit flag `i`, scope `s` |
| `link(a, c, l, i)` | `claim_object` with `object_type = 'claimius.claim'` | claim `a` composes claim `c` |
| `creator(x, g)` | `sa_created_by` | the grant `x` was created under |

### Derived relations

Each of the three trees `t` has its own up edge: ownership through `sa_owner_id`, location through `sa_location_id` and a location's `sa_parent_id`, parenthood through a registered table's `sa_parent_id`. A location sits in the ownership tree under its owner and in the location tree under its parent.

```
up_o(x, y) ← obj(x, y, _, _) ; org(x, y), x ≠ y ; loc(x, _, y)
up_l(x, y) ← obj(x, _, y, _) ; loc(x, y, _), x ≠ y
up_q(x, y) ← obj(x, _, _, y), x ≠ y
anc_t(x, x, 0)
anc_t(x, z, h + 1) ← anc_t(x, y, h), up_t(y, z)
```

`inheritance_info` stores `anc_t` for every tree, self edges included, one row per `(tree, ancestor, descendant)`. A grant reaches an object directly, through one tree, or through one composition link:

```
e(c, b)                    = m_c | coalesce(b, m_c)
reach(u, x, e, 'direct')   ← grant(_, u, c), bind(c, x, b, _, _), e = e(c, b)
reach(u, x, e, t)          ← grant(_, u, c), claim(c, m_c, true), bind(c, y, b, true, _),
                             anc_t(x, y, h), h > 0, e = e(c, b)
reach(u, x, e, 'composition') ← grant(_, u, a), claim(a, m_a, _), link(a, c, l, true), claim(c, m_c, true),
                             bind(c, x, b, _, _),
                             e = (coalesce(l, m_a) & coalesce(b, m_c)) | ((coalesce(l, m_a) | coalesce(b, m_c)) & 16)
direct_grant(u, x)         ← creator(x, g), grant(g, u, _), ¬ a deny bound on x itself held by u carries bit 1
```

A derivation with bit 16 set is a deny of its bits `e & 15`; every other derivation is a grant of `e & 15`. The access of a user on an object is the aggregate of [Denials](./denials.md):

```
A(u, x) = 15·[direct_grant(u, x)]  |  ⋁ over direct and composed grants of (e & ~X_d)  |  ⋁ over inherited grants of (e & ~(X_d | X_o))
```

with `X_d` the OR of the denies bound on `x` itself and `X_o` the OR of every other deny reaching `u` on `x`.

### What claimius stores beyond the base relations

| Stored | Holds | Rows |
|---|---|---|
| `inheritance_info` | `anc_t(x, y, h)` for every tree, self edges included | `Σ_x Σ_{t ∋ x} (d_t(x) + 1)` |
| `user_object` | `A(u, x) ≠ 0` with its derivations in `grants` and the object's name, owner, location and search vector | `P = Σ_u V_u` |
| `user_users` | `|V_u ∩ V_v|` for every pair of users sharing an object | `Σ_u S_u` |
| `access_diff` | the outstanding areas of changes not yet settled | at most one per claim and one per object |

### The queries

| Query | Relational algebra | Served by |
|---|---|---|
| Check `(u, x, r)` | `σ_{u, x, A & r = r} A` | one pair of `user_object` |
| Listing `(u, τ, r)` | `π_x σ_{u, type = τ, A & r = r} A` | the reader's range of `idx_user_object_user_type` |
| Search | the listing restricted by `search_vector @@ q` | `user_object` |
| Members of `x` | `π_u σ_x A` | `idx_user_object_object_user` |
| Visible users of `u` | `π_v σ_u user_users` | `idx_user_users_viewer` |
| Capability `(u, path, r)` | `σ_{object = ⊥, jsonpath} (grant ⋈ claim ⋈ bind)` | the base relations, see [Capabilities](./capabilities.md) |

## Symbols

Every formula in these files uses these symbols.

| Symbol | Meaning |
|---|---|
| `U`, `O` | Users of an app, objects of an app |
| `D_o`, `D_l`, `D_q` | Depth of the ownership, location and parenthood trees |
| `d_t(x)`, `a_x` | Ancestors of `x` in tree `t`; `a_x = d_o(x) + d_l(x) + d_q(x)` |
| `C` | Live grants a user holds in an app |
| `L` | Composition links out of those claims |
| `B_x` | Bindings on `x` and its ancestors |
| `G_x` | Derivations in the `grants` array of one pair |
| `V_u`, `V_{u,τ}` | Objects user `u` reaches, of any type and of type `τ` |
| `U_x` | Users reaching `x`, the `user_object` rows of `x` in `idx_user_object_object_user` |
| `Ū_o` | `P / O`, the users an object is reached by on average |
| `S_u` | Users `u` shares at least one object with, the `user_users` rows of `u` |
| `T_c` | Objects claim `c` reaches: its bound objects and, when it inherits, their descendants |
| `H_c` | Holders of `c`: users granted `c` or a claim linking to `c` |
| `N_r` | Closure rows of one root in one tree |
| `Δ_a` | Outstanding `access_diff` rows of app `a` |
| `k` | Users granted in one request |

The measured counts cited in these files come from one data set built on the prophet schema: 782 organizations in a five way tree of depth four, 1,000 users, 781 claims each bound to its organization, 5,012 grants, and 20,000 rows of a registered table over the 625 leaf organizations. It holds 393,908 `user_object` rows, so `Ū_o = 19`, and 111,472 `user_users` rows.

## Principles

1. Access is a mask on a binding. A claim says what may be done, a `user_claim` says who holds it, a `claim_object` says what it covers. Nothing else grants access, except the creator of a row.

2. The only recursion is the up edge of three trees, stored as a closure. Composition is one hop. The access relation is therefore a nonrecursive view over stored relations, maintained exactly by recomputing the pairs a change can affect; see [Materialization](./materialization.md).

3. A write settles the pairs of its own actor, and of the assignee for a grant, and records the rest as one area row keyed on what changed. The users of an area are settled by the first read in the app that meets it.

4. `get_access` and every listing read stored pairs, and neither walks a tree. Only `get_direct_access`, the fallback of the gate, walks the ancestors of one object.

5. The answer lives in the service's own Postgres. A disciple answers every access question from its own copy, without a network call to the prophet.

6. Claimius knows its own types and nothing else. The implementing side's tables join by registration, and nothing about them is written into a claimius function.
