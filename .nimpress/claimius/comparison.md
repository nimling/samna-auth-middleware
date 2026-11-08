---
layout: doc
title: Comparison
description: Claimius against Google Zanzibar and its descendants SpiceDB, OpenFGA with Auth0 FGA and Ory Keto, and against Microsoft Entra ID, dimension by dimension in rows, index entries, events and round trips on one stated data set, with the condition under which claimius wins each dimension and where it does not.
tags: comparison, zanzibar, leopard, spicedb, openfga, auth0 fga, ory keto, entra id, check, listing, storage, revocation, consistency, availability, groups overage, token lifetime
lastUpdated: true
---

# Comparison

Five systems stand where claimius stands. Zanzibar is Google's; SpiceDB, OpenFGA with Auth0 FGA, and Ory Keto implement its model of relation tuples and userset rewrites as a service beside the application. Entra ID carries groups and roles in a token and decides nothing per object. Every claimius figure below is a formula of these files with the data set plugged in; every other figure is published by its vendor or its authors and says where.

## The data set

| Symbol | Value | Meaning |
|---|---|---|
| `U` | `10⁴` | users of the app |
| `O` | `10⁶` | objects |
| `D` | 4 | depth of the tree, so `a_x = 4` in one tree |
| `C` | 5 | grants per user |
| `V` | `5·10³` | objects one user reaches |
| `U_x` | 50 | users reaching one object |
| `|T|` | `10⁴` | objects a bound node reaches |
| `|H|` | 100 | holders of one claim |

The set is consistent: `P = U · V = O · U_x = 5·10⁷` pairs. Where a formula needs them, one derivation per pair, `G_x = 1`, five bindings on an object and its ancestors, `B_x = 5`, and no composition links, `L = 0`.

## Check

| System | Per check |
|---|---|
| Claimius | `7 + 2 G_x = 9` index descents in the service's own database with no open area in the app, 0 rows written, 0 messages beyond the database connection the request already holds |
| Zanzibar | 1 request to the service; the rewrite of a relation that inherits through a parent is evaluated one level per dependent read, `D + 1 = 5` reads for this tree ([Pang et al. 2019, sections 2 and 3](https://www.usenix.org/system/files/atc19-pang.pdf)) |
| SpiceDB, OpenFGA, Auth0 FGA, Keto | 1 request; the check "will resolve all prerequisite relationships" of the model, one dispatch per rewrite step, bounded in Keto by a global `max-depth` ([OpenFGA, Relationship queries](https://openfga.dev/docs/interacting/relationship-queries), [Ory Keto, API overview](https://www.ory.com/docs/keto/concepts/api-overview)) |
| Entra ID | 0 reads: the token's `groups` or `roles` claims, verified locally; no answer per object |

The claimius count is fixed by the pair: one probe of the reconcile queue, two of `access_diff`, one of `samna_user`, the pair, its `object_info` row, `2 G_x` for the grant and its claim, one for the root; see [Reads](./reads.md). It has no term in `D`, `U`, `O` or the nesting of claims.

**Claimius wins** whenever the request reads the service's database anyway, which every request that touches data does: it adds 9 descents and no round trip, where a service adds one round trip and `D + 1` dependent reads. It does not win for a request that touches no data, where a token check reads nothing.

## First check after a grant

A grant raises one area keyed on the claim and the one user it names. The grantee's next check finds the area open over its pair and settles that one pair before it answers: the covering test reads the object's `a_x` ancestors, the reader's `C` grants and their links, and the areas on them, about `4 (C + a_x) + 2 ≈ 38` entries, and the recompute of one pair reads `3 + C + 8 + 4 a_x + 4 B_x = 52` entries and writes the pair, two index entries, and the `user_users` counts against the `U_x − 1 = 49` other users of the object, 50 entries read and at most 98 rows written. In all about 100 entries read and one pair written, once; the check after it is the plain check. The measured reduction: a first check over an area of four objects held by three users settled 12 pairs before and 1 after; see [Materialization](./materialization.md#why-a-settle-returns-current-pairs).

A tuple store pays nothing extra: the check after a grant is the same check, given a token at least as fresh as the write.

**Claimius loses** this dimension by one pair settle, paid once per user and object after a change, and wins it back on every later check.

## Grant on a node

Binding a claim held by `|H| = 100` users on a node reaching `|T| = 10⁴` objects:

| System | In the write | Later |
|---|---|---|
| Claimius | 1 `claim_object` row, 6 index entries; 1 area row; the actor's 2 pairs, the claim and the node; 1 feed event | Each holder's first listing settles its own share, `|T| = 10⁴` pairs; a holder that only checks settles 1 pair per object it checks; a holder that never reads settles nothing. At most `|H| · |T| = 10⁶` pairs over all holders |
| Zanzibar and descendants | 1 tuple | nothing stored; every check below the node walks up to it |
| Entra ID | a group or role assignment | every holder's next token |

The claimius write is constant in `|H|` and `|T|`; see [Writes](./writes.md). Zanzibar's own index Leopard pays for nested groups, not for a parent edge: "a single Zanzibar tuple addition or deletion may yield potentially tens of thousands of discrete Leopard tuple events" ([Pang et al. 2019, section 3.2.4](https://www.usenix.org/system/files/atc19-pang.pdf)).

**Claimius wins** the write, one row and one area whatever the reach. It pays the reach later, once per holder that lists, which a tuple store never stores and instead pays on every listing.

## Tree insert

A new object under a node at depth 4:

| System | Writes |
|---|---|
| Claimius | 5 closure rows, `d + 1`, 15 index entries; 1 `object_info` row, 3 btree entries and its lexemes; the actor's pair; 1 object area row. Every other user reaching it settles 1 pair on its next read, `U_x = 50` pairs over all readers |
| Zanzibar and descendants | 1 parent tuple |
| Entra ID | nothing: no object |

**Claimius loses** this dimension by `d + 1` closure rows and one `object_info` row per insert, a constant set by the depth of the tree, never by the number of objects or users. It is the cost that makes every later check and listing independent of `D`.

## First listing and a page

| System | Reads |
|---|---|
| Claimius | The reader's range of the `user_object` primary key in `object_id` order: a page of `p` rows reads `h + p (1 + 2 G_x + 2)` entries and sorts nothing. Measured at `V = 5,000`, a page of 50 read 50 `user_object` entries and 110 across the schema, where the unpaged listing read 5,000 and 10,010 and sorted 5,000 rows. The first listing after a change also settles the reader's share of each open area it meets, up to `|T| = 10⁴` pairs after a grant on a node |
| SpiceDB | `LookupResources` enumerates the reachable graph at read time; it is advised "if the number of resources that a user has access to is small (e.g. less than 10,000 resources)" and "can get slow quickly" ([SpiceDB, Protecting a list endpoint](https://authzed.com/docs/spicedb/modeling/protecting-a-list-endpoint)) |
| OpenFGA, Auth0 FGA | `ListObjects` returns results "up to the maximum number of results configured (`listObjectsMaxResults`, default: `1000`)" within a deadline ([OpenFGA, Relationship queries](https://openfga.dev/docs/interacting/relationship-queries)): with `V = 5·10³` the default returns a fifth of the answer, and every call enumerates again |
| Ory Keto | The List API returns stored relationships matching a partial tuple, not computed access ([Ory Keto, API overview](https://www.ory.com/docs/keto/concepts/api-overview)); a listing of what a user may reach is a check per candidate |
| Entra ID | No listing: the application stores and filters its own objects |

**Claimius wins** at every `V`, and by more as `V` grows: a page is linear in its own size and joins the implementing side's table in the same query. Its one extra cost is the settle of a share the first time the reader lists after a change, paid once.

## Sync per grant

| System | Per grant |
|---|---|
| Claimius | 1 `sync_event` row on the prophet, 1 applied row and at most 1 area row on each consumer of the app; 0 derived rows. Measured: 50 grants of a claim over 200 organizations took 50 events, where publishing the derived rows took 240,551 |
| Zanzibar and descendants | 1 tuple, replicated inside the service's own store; the application holds nothing to sync |
| Entra ID | 0 events; the grant reaches the application in the next token |

**Claimius wins** against any design that ships derived access, by the fan out `|T| · (1 + 2 (U_x − 1))`. Against a central service it adds one event per consumer, the cost of answering reads with no network call.

## Storage

| System | Rows |
|---|---|
| Claimius | `P = 5·10⁷` pairs, 2 index entries each, about 805 bytes per pair as measured, about `4·10¹⁰` bytes; `O (d + 1) = 5·10⁶` closure rows; `O = 10⁶` `object_info` rows; `user_users` up to `U (U − 1) ≈ 10⁸` rows when every user shares an object with every other; the source rows |
| Zanzibar and descendants | the source tuples only: `U · C = 5·10⁴` memberships, the bindings, and `O = 10⁶` parent tuples, about `1.05·10⁶`; "Zanzibar avoids storage denormalization and relies only on normalized data" ([Pang et al. 2019, section 3](https://www.usenix.org/system/files/atc19-pang.pdf)) |
| Entra ID | the directory's groups and memberships; nothing per object |

**Claimius loses** this dimension by about two orders of magnitude on this set: `P` grows with users times reach. It pays that storage so that a check and a listing read the answer instead of deriving it, which wins whenever a pair is read more often than its inputs change; see [Reads](./reads.md#why-the-pairs-are-stored).

## Time until a revoke takes effect

| System | A read after the revoke commits |
|---|---|
| Claimius on the prophet | The next read: a check settles the pair the open area covers before it answers, a listing settles the reader's share |
| Claimius on a disciple | After the page carrying the event is applied: the held pull answers when the event commits, one pull, then the next read settles |
| Zanzibar | Immediate for a read with a zookie at least as fresh as the revoke; otherwise a snapshot chosen by the server ([Pang et al. 2019, section 2.2](https://www.usenix.org/system/files/atc19-pang.pdf)) |
| SpiceDB | Immediate with `at_least_as_fresh` or `fully_consistent`; under `minimize_latency` a quantized revision ([SpiceDB, Consistency](https://authzed.com/docs/spicedb/concepts/consistency)) |
| OpenFGA, Auth0 FGA | Immediate with `HIGHER_CONSISTENCY`; under `MINIMIZE_LATENCY` a cached answer ([Auth0 FGA, Consistency modes](https://docs.fga.dev/writing-data/consistency-modes)) |
| Entra ID | Up to the lifetime of tokens already issued: by default "a random value ranging between 60-90 minutes (75 minutes on average)", and for clients with Continuous Access Evaluation up to 28 hours, revoked "in near real time in response to critical events such as account disablement and password changes" ([Access tokens, Token lifetime](https://learn.microsoft.com/en-us/entra/identity-platform/access-tokens#token-lifetime); [Configurable token lifetimes](https://learn.microsoft.com/en-us/entra/identity-platform/configurable-token-lifetimes)) |

**Claimius wins** against Entra ID by the whole token lifetime, and equals the tuple stores' strongest mode on the prophet with no token to carry. A disciple trails its prophet by the pages it has not applied; the consistency token of [Sync](./sync.md) closes that for a read that must see a known write.

## Availability of a decision

A request that needs its data and an access decision succeeds when every component on its path is up. With independent failures:

```
claimius:                      A = A_db
a decision service:            A = A_db · A_net · A_authz
Entra ID, token claims:        A = A_db                        at issuance A_entra, once per token
Entra ID, groups overage:      A = A_db · A_net · A_graph
```

**Claimius wins** for any `A_net · A_authz < 1`, because its decision runs in the database the request already needs. Entra ID matches it while the token carries the groups, and loses it above the overage limit; see below.

## Entra ID in counts

Entra ID decides at token issuance, not per object:

1. Issuance. A token carrying the groups of a user who belongs to `g` groups nested `n` deep is counted as `g · n` directory reads, paid once per token and amortized over the `r` requests the token serves, `g · n / r` per request.

2. Overage. "If a user is a member of more groups than the overage limit (150 for SAML tokens, 200 for JWT tokens), then Microsoft Entra ID doesn't emit the groups claim in the token. Instead, it includes an overage claim in the token that indicates to the application to query the Microsoft Graph API to retrieve the group membership of the user" ([Access token claims reference, Groups overage claim](https://learn.microsoft.com/en-us/entra/identity-platform/access-token-claims-reference#groups-overage-claim)). Above 200 groups every request that needs the groups pays one Microsoft Graph call, one round trip and the factor `A_net · A_graph`.

3. No per object check and no listing. An application that must know whether a user may open one document, or which documents a user may see, builds that decision itself, which is what claimius is.

4. Revocation up to the token lifetime, above.

**Claimius wins** every per object dimension, which Entra ID does not answer, and revocation. Entra ID wins issuance and storage for an application whose decisions are coarse enough to fit in fewer than 200 groups and roles; the two compose, Entra ID proving who the user is and claimius what the user may do to each object.

## Summary

| Dimension | Claimius | Tuple store service | Entra ID | Claimius wins when |
|---|---|---|---|---|
| Check | 9 descents, 0 messages | 1 round trip, `D + 1` reads | 0 reads | the request reads its database anyway |
| First check after a grant | about 100 entries, 1 pair written, once | as any check | a new token | the pair is checked more than once |
| Grant on a node | 1 row, 1 area, 2 pairs | 1 tuple | next token | always for the write; the reach is paid per holder that lists |
| Tree insert | `d + 1 = 5` closure rows, 1 `object_info` row | 1 tuple | nothing | never on the write; it buys depth independent reads |
| Listing page of 50 | 50 pair entries, no sort | an enumeration, capped at 1,000 by default in OpenFGA | none | always, more as `V` grows |
| Sync per grant | 1 event | 0 | 0 | against shipping derived rows, by the fan out |
| Storage | `5·10⁷` pairs | `≈ 1.05·10⁶` tuples | groups | reads per pair exceed changes per pair |
| Revoke | next read | next read with a fresh token | up to the token lifetime | against tokens, always |
| Availability | `A_db` | `A_db · A_net · A_authz` | `A_db`, or `A_db · A_net · A_graph` above 200 groups | `A_net · A_authz < 1` |
