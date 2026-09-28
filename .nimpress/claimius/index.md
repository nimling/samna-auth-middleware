---
layout: doc
title: Claimius
description: The Postgres schema that governs access for every Samna service, its four types, its per user access index settled on demand, the cost of every operation, and the Go middleware that carries it for Samna Auth.
tags: claimius, access control, authorization, prophet, disciple, user_object, access_diff, claim, organization, location, middleware
lastUpdated: true
---

# Claimius

Claimius is a PostgreSQL schema named `claimius`. It is the access control system every Samna service runs inside its own database. It answers one question, what may this user or client do to this object, and it answers it with an index lookup in the database that already holds the service's data.

Claimius owns and validates its own types. It never constructs or reads the implementing side's data. To claimius an object is a relation: an `object_type` and `object_id` pair that a `claim_object` binds and that `user_object` computes access for. The implementing side registers its tables with `init_claimius_tables`, binds claims to its rows, and reads the answer through functions, not tables.

## Two halves

1. Claimius is the schema: the model, the tables, the functions and their guarantees, independent of any language or host. Everything under "Claimius" below.

2. The middleware, `samna_auth_middleware`, is the Samna Auth centric implementation of claimius in Go. It carries the schema for Samna Auth as the prophet and for every product service as a disciple, and adds authentication, routed writes, the pull loop and the Go access calls. Everything under "The middleware" below.

## Claimius

| File | Covers |
|---|---|
| [Types](./types.md) | Why organization, location, user and claim are the types authorization needs, and how every common model reduces to them |
| [Organizations](./organizations.md) | Roots, children, the ownership tree, `create_root_organization`, `app_object` |
| [Locations](./locations.md) | The location tree and `sa_location_id` |
| [Users](./users.md) | `samna_user`, `user_id` and `id`, machine clients, delegation, `user_relation`, `user_users`, fields |
| [Claims](./claims.md) | Claims, bindings, grants, access bits and their expansion, composition, time windows, actor tokens, the single object gate |
| [Denials](./denials.md) | Deny claims, direct and cascaded deny, what a creator keeps, a worked scenario |
| [Redaction](./redaction.md) | Field scope on an object binding and how several grants combine |
| [Capabilities](./capabilities.md) | Scope grants with no object: page and functional access, and where claimius stops |
| [Objects](./objects.md) | Registration, `table_info`, `inheritance_info`, the `sa_*` columns, `object_field` |
| [Apps](./apps.md) | `samna_app`, `app_object`, app scoped tables |
| [Materialization](./materialization.md) | `user_object`, `object_info`, `user_users`, `access_diff`, the reader's slice, and why |
| [Reads](./reads.md) | The read surface, what each read costs, and why a read writes only its settle |
| [Writes](./writes.md) | What each write does synchronously and what it leaves for a read |
| [Usage](./usage.md) | How often and how recently a user reached an object |
| [Search](./search.md) | Full text search over what a user can reach |
| [Graphs](./graphs.md) | The owner and claim graphs, depth, width and paging |
| [Sync](./sync.md) | The outbox, what it carries, the cursors, the consistency token |
| [Prophets and disciples](./prophets.md) | The authority, its mirrors, ids, federation, failure |
| [Security](./security.md) | Roles, secrets, ephemeral tables, soft delete, a compromised disciple |
| [Audit](./audit.md) | Why the trail is written by the database in the transaction of the change |
| [Storage](./storage.md) | Row widths, indexes, fill factors, vacuum, time ordered data, connections |
| [Scaling](./scaling.md) | Every cost as a formula in the hardware it runs on, the ceilings on the Azure servers of today and a self hosted reference node, the bottleneck, and the configuration the formulas choose |
| [Comparison](./comparison.md) | Claimius against Zanzibar, SpiceDB, OpenFGA and Auth0 FGA, Ory Keto and Entra ID, dimension by dimension, with the condition under which claimius wins |
| [The extension, future work](./extension.md) | Where claimius is going once hosting allows it |

## The middleware

| File | Covers |
|---|---|
| [The middleware](./middleware/index.md) | What the Go module carries, package by package, and authentication |
| [Access in Go](./middleware/access.md) | The lookups on `sdb.Instance()`, error mapping, listings, the usage counter |
| [Writes in Go](./middleware/writes.md) | Routed and local writes, stamping the actor, debug mode |
| [Sync in Go](./middleware/sync.md) | The `/sam/sync` routes and the pull loop |

## Principles

1. Access is a mask on a binding. A claim says what may be done, a `user_claim` says who holds it, a `claim_object` says what it covers. Nothing else grants access.

2. Every write records the area it changed in `access_diff` and settles at most the actor's own pair of the row it wrote. No write enumerates the users an area touches, and none grows with the reach of a claim.

3. Every read settles its own part of the areas it targets before it answers, the one pair of a single object read or the slice of a listing, then reads `user_object` by primary key or by the reader's own range of it. A read never pays for a user it is not about.

4. The answer lives in the service's own Postgres. A disciple answers every access question from its own replica, without a network call to the prophet.

5. Claimius knows its own types and nothing else. The implementing side's tables join by registration, and nothing about them is written into a claimius function.

## Numbers the design rests on

Every performance statement in these files cites a formula, a measurement, or a published figure, and says which. The measurements come from one benchmark: the prophet schema built by `smig test` on Postgres 17, one app with 782 organizations in a five way tree of depth four, 1,000 users, 781 claims each bound to its organization, 5,012 grants, and 20,000 rows of a registered table spread over the 625 leaf organizations.

| Operation | Measured |
|---|---|
| `user_object` primary key probe | 0.066 ms, 3 buffers |
| A check with its targeted settle, one statement from a client | 0.39 to 0.44 ms, of which 0.094 ms is the round trip |
| Finding the reader's outstanding claim areas among 771 | 0.30 ms |
| Settling one pair, set based | 0.019 ms |
| Listing with actor tokens | 288 rows in 1.18 ms, 20,000 rows in 14.4 ms |
| Usage flush of 1,000 pairs | 13 to 26 ms, every update heap only |
