---
layout: doc
title: Comparison
description: Claimius against Google Zanzibar and its descendants SpiceDB, OpenFGA with Auth0 FGA and Ory Keto, and against Microsoft Entra ID, operation by operation in index entries, rows, events and round trips on one stated data set and one stated workload, with every claimius figure measured on this release, the condition under which claimius wins each operation, and the operations it loses.
tags: comparison, zanzibar, leopard, spicedb, openfga, auth0 fga, ory keto, entra id, check, listing, first listing, directory, storage, revocation, consistency, availability, groups overage, token lifetime, bench
lastUpdated: true
---

# Comparison

Five systems stand where claimius stands. Zanzibar is Google's; SpiceDB, OpenFGA with Auth0 FGA, and Ory Keto implement its model of relation tuples and userset rewrites as a service beside the application. Entra ID carries groups and roles in a token and decides nothing per object. Every claimius figure below was measured on this release, as index entries counted by `pg_stat_get_xact_tuples_returned` over the claimius indexes inside the transaction of the operation, in the pgTAP suites of `test/sql` on a fresh database and in the bench `bench/prophet/bench.sql`, run by `just bench` on a prophet with analyzed tables; where the two plans differ the text says which one a figure comes from. Every other figure is published by its vendor or its authors and says where.

## The data set and the workload

| Symbol | Value | Meaning |
|---|---|---|
| `U`, `O` | `10⁴`, `10⁶` | users, objects |
| `D` | 4 | tree depth, so `a_x = 4` ancestors in one tree |
| `C`, `n_u` | 5, 25 | grants per user, bindings of those claims |
| `B_x` | 5 | bindings on an object and its ancestors |
| `G_x` | 1 | derivations per pair |
| `V_u`, `U_x` | `5·10³`, 50 | reach of a user, holders of an object |
| `S_u` | 500 | users one user shares an object with |
| `|T|`, `|H|` | `10⁴`, 100 | reach and holders of one broad binding |
| `p` | 50 | rows of a page |

The set is consistent: `P = U · V_u = O · U_x = 5·10⁷` pairs. The workload is `10⁶` requests: `7·10⁵` checks, one per request; `2.5·10⁵` listing pages, one per page view; `10⁴` directory views, each one `get_users` and twenty `get_user_access` rows; `2·10⁴` created objects; `2·10³` grants, revokes and bindings; 10 broad changes, each a binding over `|T|` objects held by `|H|` users of whom half list once and each checks 20 pairs; and every write synced to two consumers. A second total is given at 1 broad change, since a binding over `10⁴` objects is a weekly event in a workplace platform and `10⁶` requests is a few days.

## Check

| System | Per check |
|---|---|
| Claimius | 7 index entries on the prophet and 4 on a disciple with nothing raised since the pair's mark, in the service's own database, 0 rows written, 0 messages beyond the connection the request holds; 3 entries in the bench on analyzed tables, where the planner reads the small tables sequentially |
| Zanzibar | 1 request to the service; the rewrite of a relation that inherits through a parent is evaluated one level per dependent read, `2D + 1 = 9` reads for this tree ([Pang et al. 2019, sections 2 and 3](https://www.usenix.org/system/files/atc19-pang.pdf)) |
| SpiceDB, OpenFGA, Auth0 FGA, Keto | 1 request; the check "will resolve all prerequisite relationships" of the model, one dispatch per rewrite step, 9 reads for this tree, bounded in Keto by a global `max-depth` ([OpenFGA, Relationship queries](https://openfga.dev/docs/interacting/relationship-queries), [Ory Keto, API overview](https://www.ory.com/docs/keto/concepts/api-overview)) |
| Entra ID | 0 reads: the token's `groups` or `roles` claims, verified locally; no answer per object |

The claimius count is fixed by the pair: the pair with its mark, the object's `object_info` row, the trimmed position, the two empty ranges of the areas of the object's root and of the reader's assignments since the mark, and `samna_user`; the grant comes from the pair's stored derivations and reads nothing; see [Reads](./reads.md). It has no term in `D`, `U`, `O` or the nesting of claims. When something was raised in the object's root since the mark the check reads the areas and the object's closure range: 15 entries on the prophet and 10 on a disciple after an unrelated raise in the same root, measured in `check_mark`, and it moves the pair's mark as a heap only update, so the next check is the quiet one again. Only an area on the object or on an ancestor with cascade, or an assignment of the reader, recomputes the one pair, once.

**Claimius wins** whenever the request reads the service's database anyway, which every request that touches data does: it adds 7 entries and no round trip, where a service adds one round trip and `2D + 1` dependent reads, and it stays below the service's 9 in a busy root. It does not win for a request that touches no data, where a token check reads nothing.

## First check after a grant

A grant raises one assignment area for the one user it names. The grantee's next check finds that area at or after its pair's mark and recomputes the one pair before it answers, the point settle of [Materialization](./materialization.md#a-check-settles-one-pair): one closure range of the object, one binding probe per node of its chain, the reader's grants, the `object_info` row, and the pair written, two index entries. Measured in `check_mark`: the check after a raise on an ancestor recomputes the one pair once, and the check after it reads the pair after its mark and nothing more.

A tuple store pays nothing extra: the check after a grant is the same check, given a token at least as fresh as the write.

**Claimius loses** this operation by one pair settle, paid once per user and object after a change, and wins it back on every later check.

## Grant on a node

Binding a claim held by `|H| = 100` users on a node reaching `|T| = 10⁴` objects:

| System | In the write | Later |
|---|---|---|
| Claimius | 1 `claim_object` row, 6 index entries; 1 area row on the node with cascade; the actor's 2 pairs, the claim and the node; 1 feed event | Each holder's first listing settles its own share, `|T|` pairs; a holder that only checks settles 1 pair per object it checks; a holder that never reads settles nothing. At most `|H| · |T| = 10⁶` pairs over all holders |
| Zanzibar and descendants | 1 tuple | nothing stored; every check below the node walks up to it |
| Entra ID | a group or role assignment | every holder's next token |

The claimius write is constant in `|H|` and `|T|`; see [Writes](./writes.md). Zanzibar's own index Leopard pays for nested groups, not for a parent edge: "a single Zanzibar tuple addition or deletion may yield potentially tens of thousands of discrete Leopard tuple events" ([Pang et al. 2019, section 3.2.4](https://www.usenix.org/system/files/atc19-pang.pdf)).

**Claimius wins** the write, one row and one area whatever the reach. It pays the reach later, once per holder that lists, which a tuple store never stores and instead pays on every listing.

## Tree insert

A new object under a node at depth 4:

| System | Writes |
|---|---|
| Claimius | 5 closure rows, `d + 1`, 15 index entries; 1 `object_info` row with its creator and owner root, 4 btree entries and its lexemes; the actor's pair; 1 object area row under the owner root. Every other user reaching it settles 1 pair on its next read, `U_x = 50` pairs over all readers |
| Zanzibar and descendants | 1 parent tuple |
| Entra ID | nothing: no object |

**Claimius loses** this operation by `d + 1` closure rows and one `object_info` row per insert, a constant set by the depth of the tree, never by the number of objects or users. It is the cost that makes every later check and listing independent of `D`.

## First listing and a page

| System | Reads |
|---|---|
| Claimius | A page of `p` rows reads the reader's range of the `user_object` primary key in `object_id` order, one entry per row, and one `object_info` row per row, and sorts nothing: a page of 50 read 101 entries in 8 ms in the bench, and a full listing of 16,001 rows 32,005 entries in 0.10 seconds, two per row. The first listing after a change also settles the reader's share, once: a claim bound on a root over 16,000 new organizations costs the holder's first listing 64,048 entries, the closure entry and the `object_info` row of each object for the settle and the two per row of the page, in 0.92 seconds, against 1,577,903 entries at alpha0026; a grant on a node over 10,000 organizations costs the grantee's first listing 40,035 entries and 0.64 seconds, against 201,374 entries and 0.9 seconds at alpha0026. Those are the bench as the suite runs it, on fresh tables; on analyzed tables the same steps read 64,046 in 0.83 seconds, 32,005 in 87 ms and 50,033 in 0.72 seconds, where the planner descended the node twice. In the `settle_share` suite, the first listing after a grant on a node over 1,000 organizations at depth 1 reads 4,057 entries on the prophet and 4,030 on a disciple, and the same share at depth 4 under five bindings on the chain 8,136 and 8,113, one extra descent of the subtree per binding inside the chain and nothing else per object |
| SpiceDB | `LookupResources` enumerates the reachable graph at read time, `C + n_u` subject reads and one reverse read per reached node, about `1.1 V_u` for a full listing and about 85 for a page of 50 with a cursor; it is advised "if the number of resources that a user has access to is small (e.g. less than 10,000 resources)" and "can get slow quickly" ([SpiceDB, Protecting a list endpoint](https://authzed.com/docs/spicedb/modeling/protecting-a-list-endpoint)) |
| OpenFGA, Auth0 FGA | `ListObjects` returns results "up to the maximum number of results configured (`listObjectsMaxResults`, default: `1000`)" within a deadline ([OpenFGA, Relationship queries](https://openfga.dev/docs/interacting/relationship-queries)): with `V_u = 5·10³` the default returns a fifth of the answer, and every call enumerates again |
| Ory Keto | The List API returns stored relationships matching a partial tuple, not computed access ([Ory Keto, API overview](https://www.ory.com/docs/keto/concepts/api-overview)); a listing of what a user may reach is a check per candidate |
| Entra ID | No listing: the application stores and filters its own objects |

**Claimius wins** every page at every `V_u`, 101 entries against about 85 reads plus a round trip for SpiceDB and 1,000 every call for OpenFGA, and by more as `V_u` grows: a page is linear in its own size and joins the implementing side's table in the same query. Its one extra cost is the settle of a share the first time the reader lists after a change, paid once.

**Claimius loses** that first listing after a broad change: `4|T|` entries read and `2|T|` index entries written at the holder's first listing, 64,048 read over 16,000 organizations, where a tuple store pays no settle and enumerates on every listing instead. The loss is bounded by the reach of the change and paid once per holder that lists; the 1,577,903 entries and the 16,000 open areas every later listing tested at alpha0026 are gone, and the second listing reads two entries per row.

## Who sees whom

| System | Per directory view |
|---|---|
| Claimius | `get_users` of one of 200 holders over 2,000 organizations builds the viewer's rows on its first use, the directory build `Σ_{x ∈ V_u} U_x` of [Users](./users.md#who-can-see-whom), 400,000 holder pairs through a hash join, 6,212 index entries and 2.0 seconds in the bench, and reads 7 entries in 10 ms on every later view, the fold keeping its rows from then on; `get_user_access` over the 200 rows of that directory reads 5,008 entries in 71 ms, about 23 per row of which 20 resolve the target through `samna_user` by external id, user id and row id, against 437,835,068 at alpha0024. A user who never views the directory costs nothing |
| Zanzibar and descendants | none: `Expand` and `LookupSubjects` list the subjects of one object |
| Entra ID | `memberOf` then `members` per group through Microsoft Graph, `g · n` reads per view |

**Claimius wins** the operation outright, since no tuple store has it, and against Entra ID by answering per object. The build on first use is the self join a store would pay on every view.

## Sync per grant

| System | Per grant |
|---|---|
| Claimius | 1 `sync_event` row on the prophet, 1 applied row and 1 area row on each consumer of the app; 0 derived rows. Measured: 50 grants of a claim over 200 organizations took 50 events, where publishing the derived rows took 240,551 |
| Zanzibar and descendants | 1 tuple, replicated inside the service's own store; the application holds nothing to sync |
| Entra ID | 0 events; the grant reaches the application in the next token |

**Claimius wins** against any design that ships derived access, by the fan out `|T| · (1 + 2 (U_x − 1))`. Against a central service it adds one event per consumer, the cost of answering reads with no network call.

## Storage

| System | Rows |
|---|---|
| Claimius | `P = 5·10⁷` pairs, 2 index entries each, about 805 bytes per pair as measured, about `4·10¹⁰` bytes; `O (d + 1) = 5·10⁶` closure rows; `O = 10⁶` `object_info` rows; `user_users` rows for the users who viewed the directory only, `Σ_{u viewing} S_u`; the areas since the oldest active cursor; the source rows |
| Zanzibar and descendants | the source tuples only: `U · C = 5·10⁴` memberships, the bindings, and `O = 10⁶` parent tuples, about `1.05·10⁶`; "Zanzibar avoids storage denormalization and relies only on normalized data" ([Pang et al. 2019, section 3](https://www.usenix.org/system/files/atc19-pang.pdf)) |
| Entra ID | the directory's groups and memberships; nothing per object |

**Claimius loses** this operation by about two orders of magnitude on this set: `P` grows with users times reach. It pays that storage so that a check and a listing read the answer instead of deriving it, which wins whenever a pair is read more often than its inputs change; see [Reads](./reads.md#why-the-pairs-are-stored).

## Time until a revoke takes effect

| System | A read after the revoke commits |
|---|---|
| Claimius on the prophet | The next read: a check settles the pair the assignment area covers before it answers, a listing settles the reader's share. Measured in the bench: the revoke of the claim over 16,000 organizations is settled by the holder's next listing, which deletes the 16,001 pairs reading 64,038 entries in 2.0 seconds, with two viewers in the app raising the `user_users_diff` rows, and returns nothing |
| Claimius on a disciple | After the page carrying the event is applied: the held pull answers when the event commits, one pull, then the next read settles |
| Zanzibar | Immediate for a read with a zookie at least as fresh as the revoke; otherwise a snapshot chosen by the server ([Pang et al. 2019, section 2.2](https://www.usenix.org/system/files/atc19-pang.pdf)) |
| SpiceDB | Immediate with `at_least_as_fresh` or `fully_consistent`; under `minimize_latency` a quantized revision ([SpiceDB, Consistency](https://authzed.com/docs/spicedb/concepts/consistency)) |
| OpenFGA, Auth0 FGA | Immediate with `HIGHER_CONSISTENCY`; under `MINIMIZE_LATENCY` a cached answer ([Auth0 FGA, Consistency modes](https://docs.fga.dev/writing-data/consistency-modes)) |
| Entra ID | Up to the lifetime of tokens already issued: by default "a random value ranging between 60-90 minutes (75 minutes on average)", and for clients with Continuous Access Evaluation up to 28 hours, revoked "in near real time in response to critical events such as account disablement and password changes" ([Access tokens, Token lifetime](https://learn.microsoft.com/en-us/entra/identity-platform/access-tokens#token-lifetime); [Configurable token lifetimes](https://learn.microsoft.com/en-us/entra/identity-platform/configurable-token-lifetimes)) |

**Claimius wins** against Entra ID by the whole token lifetime, and equals the tuple stores' strongest mode on the prophet with no token to carry. A disciple trails its prophet by the pages it has not applied; the consistency token of [Sync](./sync.md) closes that for a read that must see a known write. **Claimius loses** the revoke's settle, `4|T|` entries at the holder's next listing where a store pays nothing beyond the tuple delete.

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

**Claimius wins** every per object operation, which Entra ID does not answer, and revocation. Entra ID wins issuance and storage for an application whose decisions are coarse enough to fit in fewer than 200 groups and roles; the two compose, Entra ID proving who the user is and claimius what the user may do to each object.

## The workload in entries

Entries per `10⁶` requests at the weights above, each term the weight times the measured figure of its operation, in millions. The claimius column uses the prophet's pgTAP check of 7 and the bench's page of 101, first listing over 10,000 of 40,035 and directory figures; the store columns use the published per operation counts of the sections above, 9 reads per check, 85 per SpiceDB page and 1,000 per OpenFGA call.

| Term | Claimius | SpiceDB | OpenFGA |
|---|---|---|---|
| Checks, nothing raised since the mark | 4.9 | 6.3, and `7·10⁵` round trips | 6.3, and `7·10⁵` round trips |
| Checks, something raised in the root since | 10.5 at 15 each | 6.3 | 6.3 |
| Pages | 25.3 | 21.3 | 250 |
| First listings after the 10 broad changes, 50 holders each | 20.0, and `2·10⁷` pair entries written; 2.0 at 1 broad change | 0.04 | 0.5 |
| Directory views, `10⁴` of `get_users` after the first | 0.07, and 6,212 with the build of the viewer's rows once per viewer | no such operation | no such operation |
| `get_user_access` rows, `2·10⁵` | 4.6, 20 of each 23 the resolution of the target | no such operation | no such operation |

So on the two operations that are 95 % of the requests claimius reads 30.2 million entries against SpiceDB's 27.6 million and OpenFGA's 256 million, with no round trip where every store pays one per request, and in a quiet app the checks fall to 4.9 million. The first listings after broad changes are the materialization: 20 million at 10 broad changes, 2 million at 1, paid once per holder who lists and never on a request. Writes and sync are not in the table; their per operation figures are in [Writes](./writes.md) and [Sync](./sync.md).

## Summary

| Operation | Claimius | Tuple store service | Entra ID | Claimius wins when |
|---|---|---|---|---|
| Check | 7 entries, 15 in a busy root, 0 messages | 1 round trip, `2D + 1 = 9` reads | 0 reads | the request reads its database anyway |
| First check after a grant | one pair recomputed, once | as any check | a new token | the pair is checked more than once |
| Grant on a node | 1 row, 1 area, 2 pairs | 1 tuple | next token | always for the write; the reach is paid per holder that lists |
| Tree insert | `d + 1 = 5` closure rows, 1 `object_info` row | 1 tuple | nothing | never on the write; it buys depth independent reads |
| Listing page of 50 | 101 entries, no sort | 85 reads and a round trip, or an enumeration capped at 1,000 by default in OpenFGA | none | always, more as `V_u` grows |
| First listing after a broad change | `4|T|` read and `2|T|` written, 64,048 over 16,000 organizations, once | an enumeration, every listing | none | never on that listing; every later page |
| Who sees whom | 7 entries a view after a build of `Σ_{x ∈ V_u} U_x` | no such operation | `g · n` Graph reads | always |
| Sync per grant | 1 event | 0 | 0 | against shipping derived rows, by the fan out |
| Storage | `5·10⁷` pairs | `≈ 1.05·10⁶` tuples | groups | reads per pair exceed changes per pair |
| Revoke | next read, `4|T|` at the holder's next listing | next read with a fresh token | up to the token lifetime | against tokens, always; the settle is a loss |
| Availability | `A_db` | `A_db · A_net · A_authz` | `A_db`, or `A_db · A_net · A_graph` above 200 groups | `A_net · A_authz < 1` |

The losses stay losses: the first listing after a broad change by `|T|` entries read and `2|T|` written against a store's 85 per page, the storage by about `50×`, the write of a grant or a binding by `6×` to `18×` against one tuple, and the revoke's settle at the holder's next listing. Every one of them is paid on a change, once, never on a request.
