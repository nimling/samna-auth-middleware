---
layout: doc
title: Comparison
description: Claimius against Google Zanzibar, SpiceDB, OpenFGA and Auth0 FGA, Ory Keto and Microsoft Entra ID, in what each stores, and what each reads, writes and sends over the network per check, per listing and per change, with the condition under which claimius is the better shape.
tags: comparison, zanzibar, leopard, spicedb, authzed, openfga, auth0 fga, ory keto, entra id, check, listing, consistency, availability
lastUpdated: true
---

# Comparison

Five systems stand where claimius stands. Zanzibar is Google's; SpiceDB, OpenFGA with Auth0 FGA, and Ory Keto implement its model of relation tuples and userset rewrites; Entra ID carries groups and roles in the token. Every claimius figure below is a formula of these files; every other figure is published by its vendor or its authors, and says where.

## At a glance

| | Claimius | Zanzibar and the systems built on it | Entra ID |
|---|---|---|---|
| Where a check runs | In the service's own Postgres | A separate service | In the service, from token claims |
| Network messages per check | 0 beyond the service's own database | 1 request and answer to the service, plus the service's internal reads | 0 |
| Stored per grant | 1 `user_claim` row; the pairs it yields, `|T_c|` for its holder | 1 tuple | the group in the token |
| Stored per object | its closure rows, `Σ_t (d_t + 1)`, and one pair per user reaching it, `U_x` | the tuples naming it | nothing |
| Check reads | one pair and `2 G_x` grant probes | one lookup per level of rewrite, at least the depth of the chain | the token |
| Listing reads | one range, `V_{u,τ}` pairs | an enumeration of the reachable graph, capped | the token's groups |
| Change writes | the source row, one area row, the actor's and assignee's pairs | the tuple, then derived index events | a new token |

## Stored

A tuple store stores the relation tuples and nothing derived from them, except the group index Zanzibar calls Leopard. The Zanzibar paper states the principle: "To facilitate consistency, Zanzibar avoids storage denormalization and relies only on normalized data" ([Pang et al. 2019](https://www.usenix.org/system/files/atc19-pang.pdf)).

Claimius stores the same source rows, one per claim, binding and grant, and three derived relations: the closure, `Σ_x rows(x)` rows, the pairs, `P = Σ_u V_u` rows in `user_object` and `object_users`, and the user pairs, `Σ_u S_u`. The pairs are the one term that grows with users times reach. Claimius pays that storage so that a check and a listing read the answer instead of deriving it; see [Materialization](./materialization.md#why-a-listing-reads-stored-pairs).

## Check

A check against a separate service is at least one request and its answer on the network, plus the service's own evaluation. A Zanzibar check expands the userset rewrites of the relation: a document viewer through a folder chain of depth `D` is `D` dependent lookups, and a group nested `n` deep is a reachability question of `n` hops that Zanzibar answers from Leopard, "a single Zanzibar tuple addition or deletion may yield potentially tens of thousands of discrete Leopard tuple events" to keep it current ([Pang et al. 2019, section 3.2.4](https://www.usenix.org/system/files/atc19-pang.pdf)).

A claimius check is a query in the database the service already reads:

```
entries(get_access) = Δ_a + 1 + min(U_x, V_{u,τ}) + 2 G_x + 1
```

with no term in the depth of the trees, no term in the nesting of claims, which is one hop by construction, and no network message beyond the service's own database connection. It is the better shape whenever the service reads its own database on the request anyway, which is every request that touches data.

## Listing

"Which objects may this user see" is the question a relation tuple store answers least well. The Zanzibar paper publishes no list API. OpenFGA's `ListObjects` returns at most 1,000 results by default and is advised for small collections ([OpenFGA, Relationship queries](https://openfga.dev/docs/interacting/relationship-queries)); SpiceDB advises its lookup only below about 10,000 reachable resources ([Protecting a list endpoint](https://authzed.com/docs/spicedb/modeling/protecting-a-list-endpoint)). Each enumerates the reachable graph from the user at read time.

Claimius lists from the reader's range:

```
entries(get_objects) = Δ_a + 1 + V_{u,τ} (2 + 2 G)
```

linear in what the reader reaches, with no cap, and joined by the implementing side to its own table in the same query. Claimius is the better shape at every `V_{u,τ}`, and the difference grows with it.

## Change

A tuple store writes one tuple per relation: a claim bound to a node and granted to `k` users is `1 + k` tuples, and a group index is updated from the changelog afterwards.

Claimius writes the same `2 + k` source rows and, in the same transaction, the closure rows of the claim, one area row, and the actor's and assignees' pairs: `≤ 1 + (1 + k) |T_c|` pairs; see [Writes](./writes.md#one-request-one-transaction). Every other user of the change settles on the first read that meets its area, `|H_c| × |T_c|` pairs once. The write is linear in the reach of the node and in the users it names, and constant in every other user of the app. What claimius pays that a tuple store does not is the materialized pairs, once per change; what it saves is the enumeration a tuple store pays on every listing and the rewrite a tuple store pays on every check. It is the better shape when pairs are read more often than they change, which [Materialization](./materialization.md#why-a-listing-reads-stored-pairs) bounds at a few reads per change.

## Consistency

| System | A read without a token | A read that must see a write |
|---|---|---|
| Claimius | On the prophet, settled before it answers; on a disciple, as of its cursor | Waits until the disciple's cursor reaches the write's sequence |
| Zanzibar | A snapshot chosen by the server | Zookie |
| SpiceDB | `minimize_latency`, a quantized revision | `at_least_as_fresh` with a ZedToken ([Consistency](https://authzed.com/docs/spicedb/concepts/consistency)) |
| Auth0 FGA | `MINIMIZE_LATENCY`, served from a cache | `HIGHER_CONSISTENCY` ([Consistency modes](https://docs.fga.dev/writing-data/consistency-modes)) |
| Entra ID | The token's claims until it expires | A new token |

The token is the same idea in each: a sequence, a zookie, a revision. A disciple is not a cache with a window; it applies each event in order, so it is behind by the events it has not pulled and by nothing else.

## Availability

A request that needs its data and an access decision succeeds when every component on its path is up. With independent failures:

```
separate service:  A = A_db × A_net × A_authz          claimius:  A = A_db
```

The claimius decision adds no factor, because it runs in the database the request already needs. Claimius is the better shape for any `A_authz × A_net < 1`. What it costs is freshness while a disciple's prophet is unreachable; see [Prophets and disciples](./prophets.md#failure).

## Entra ID

Entra ID carries groups and app roles in the token, at most 200 groups before the token carries an overage claim instead ([Access token claims, groups overage](https://learn.microsoft.com/en-us/entra/identity-platform/access-token-claims-reference)). It stores nothing per object and decides nothing per object: an application that needs to know whether a user may open one document builds that decision, which is what claimius is.

## Ease of use

1. Claimius has four types and two bindings, and every consumer reads through the same functions. An implementing side registers its tables and binds claims to its rows; it writes no model language.

2. Zanzibar and the systems built on it take a schema of relations and rewrites per namespace. That is more expressive, and every question a service asks depends on it being written correctly.
