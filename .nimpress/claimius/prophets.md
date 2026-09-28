---
layout: doc
title: Prophets and disciples
description: The authority and its mirrors, what each holds and trusts, how ids stay unique, how prophets federate by mirroring each other's apps, what the network carries, and what happens when any of them fails.
tags: prophet, disciple, hybrid, federation, init, init_disciple, init_hybrid, trust, uuid, failure
lastUpdated: true
---

# Prophets and disciples

The same schema runs in two roles and their combination.

| Role | Bootstrap | Holds | Writes |
|---|---|---|---|
| Prophet | `init` | Every app it is home for, with keys and seeds | Every grant and shared row of its apps; serves their feeds |
| Disciple | `init_disciple` | One app, mirrored, without `app_id` or `app_object` | Its own registered tables; routes shared writes to its prophet |
| Hybrid | `init_hybrid` | Its own system app, and one app mirrored from an upstream prophet | Both of the above |

Samna Auth is the prophet of the ecosystem. Bookable and Nimployer are disciples.

## The principle

1. A prophet is the home of an app. It is the only place a shared row of the app, a grant, a binding, an organization it reaches, a user row, is decided. Every other copy is a mirror of it.

2. A disciple relies on exactly one prophet. Its cursor is a sequence of that prophet's outbox and means nothing anywhere else. It applies what its prophet sends as authoritative, under `replay_mode`, without deciding it again.

3. A disciple answers every read from its own database. The prophet is on the path of a shared write and of the feed, never of a read.

4. A prophet trusts a disciple as an app, no more. The app key authenticates the disciple to `/sam` for its feed and its routed writes; every routed write is authorized again on the prophet against the end user the write is made for.

## Ids

Every row id is a version 4 UUID from `gen_random_uuid()`: 122 random bits ([RFC 9562, section 5.4](https://www.rfc-editor.org/rfc/rfc9562#section-5.4)). The id is given once, by the home prophet, or by the disciple for its own tables, and kept by every copy. The deterministic ids of the derived tables are hashes of source ids through `composite_id`, so every copy derives the same ones.

The probability that `n` independent random ids contain a collision is at most `n² / 2^123`. For a billion ids that is `9.4 × 10^-20`; for a trillion, `9.4 × 10^-14`. That is far below the rate of undetected storage errors under any database, so no prophet reserves ranges or asks another before creating a row, and creating a row never waits on a peer. The one rule that remains is ownership: a row arriving from a peer with an id that already exists locally under another home is refused, never merged, so even that improbable event fails closed.

Why random and not brokered. A prophet that drew its ids from ranges of size `B_r` handed out by a broker would pay one round trip per `B_r` rows and never collide; it would also stop creating rows once cut off from the broker for longer than `B_r / λ_id`, so every create would carry the broker's availability, `A_create = A_db × A_broker` for any partition longer than the range lasts. Random ids cost no round trip and no availability, and trade them for a collision probability that stays below `10^-13` at a trillion rows. The refusal costs nothing either: the mirrored upsert already probes the primary key, and the ownership test reads the row it found.

## Federation

Prophets federate by mirroring each other's apps. A prophet that serves users of an app homed on a peer is a disciple of the peer for that app, the shape `init_hybrid` sets up: its own apps at home, the peer's app mirrored.

1. Agreement is per app. Two prophets agree which of them is home for an app before either serves it. The home prophet decides every shared row of the app; the other mirrors it and routes its writes home.

2. A person is one `user_id` everywhere. The prophet that is home for the person's identity authenticates them and signs their tokens; a peer verifies those tokens against the home prophet's published keys and holds its own per app `samna_user` rows under the same `user_id`.

3. A peer's rows keep their ids. Because ids are random and never reassigned, a row mirrored across three prophets is the same row in all three, and a grant made at home binds the same object id everywhere.

4. Moving a disciple from one prophet to another is a change of home. It works only when the new prophet holds the disciple's app with the same ids, by mirroring it or by an agreed hand over, and the disciple starts again from the new prophet's snapshot with its cursor at zero. Two prophets that have not agreed share no ids and no sequence, so a disciple cannot be pointed at a prophet that does not hold its app.

### Mirror lag

A mirror is a consumer of the home prophet's feed, so its lag is the staleness of [Sync](./sync.md#staleness), `τ_0 = τ_x + r_net / 2 + a_e / σ`, with `r_net` the round trip between the two prophets. A disciple of the mirror adds its own hop, and a chain of `h` feeds adds up:

```
τ_chain = Σ_{i=1..h} τ_0,i
```

A home prophet in West Europe, a mirror in Norway East and a disciple beside the mirror, all on `D2ds_v5` servers: 11 ms for the prophets' hop, 0.65 ms for the disciple's, 11.8 ms from a commit at home to a read on the disciple. A write routed from that disciple travels to the mirror and on to the home, `r_{d,m} + r_{m,h} + c_w / σ + L_c` before it is acknowledged, and is readable on the disciple after the chain's `τ_chain` more, when the disciple waits on its sequence.

## Capacity of the network

Anyone can host a prophet or a disciple, and prophets mirror each other's apps, so the question is where the network's ceiling sits. The formulas and machines are those of [Scaling](./scaling.md): `K` the capacity of a database in calibration cores, `ρ` its utilization budget, `σ` the speed of one thread.

1. Reads have no shared term. A disciple answers from its own database, so `N` disciples carry `Σ_i min(N_a,i / c_a, (1 + R_i) ρ_i K_i / c_r)`, linear in the disciples and in each one's standbys: 4,380 checks a second for each `D2ds_v5` and 247,000 for each reference node, times `1 + R`. No prophet is on a read's path, and none bounds the sum.

2. Writes of one app are bounded by its home prophet, `ρ K_p / c_w`: 401 API first writes a second on a `D2ds_v5`, 22,700 on the reference node. Federation adds write capacity by adding homes, `Σ_h ρ K_h / c_w`, never by splitting one app's writes between prophets, because one home decides every shared row of its app.

3. The settle of a write is paid where the app is read. A disciple settles its app; the prophet settles only the apps it serves reads for, and for a disciple's app only the slices of the actors whose routed writes it checks. A write therefore costs its settle once in the network, not once per database holding the app.

4. The feed costs the prophet `s_e` per delivered event, `K / s_e` deliveries a second, 137,500 on a `D2ds_v5`. At that server's write ceiling, six events a write and fan out three, the feed takes 5 % of it. Fan out never binds before writes do.

5. A disciple applies events on one thread at `σ / a_e`, 6,900 a second on a `D2ds_v5`, above any home prophet's write rate on the same hardware: a disciple never falls behind a healthy prophet by throughput, only by being away, and catches up at `μ − λ_e`; see [Sync](./sync.md#catch-up).

The ceiling of the network is therefore per app, the write rate of its home prophet, and per service, the reads of its own database. Neither is shared.

## Failure

| Down | Reads | Writes | Recovery |
|---|---|---|---|
| The prophet | Every disciple answers from its replica | Shared writes fail; a disciple's own tables keep working | The pull resumes from the cursor |
| A disciple | The prophet and other disciples are unaffected | None of its writes happen | It resumes from its cursor, or from a snapshot once the backlog exceeds its app's source rows |
| The network between them | As when the prophet is down | As when the prophet is down | The same |

### Availability by deployment shape

With independent failures, a request succeeds when every component on its path is up. `A_api` is the service tier, `A_db` a database, `A_net` a link, `p` the share of reads that find no area to settle, `R` standbys.

| Shape | Read | Shared write |
|---|---|---|
| One node, the service and its database | `A_api A_db` | `A_api A_db` |
| Prophet with disciples | `A_api,d A_db,d`, the prophet absent | `A_api,d A_db,d A_net A_api,p A_db,p` |
| Disciple with `R` standbys | `A_api [p (1 − (1 − A_db)^{1+R}) + (1 − p) A_db]` | as above |
| Disciple of a mirroring prophet | `A_api,d A_db,d` | `A_d A_net A_m A_net A_h`, each `A` a service and its database |

On Azure, with the published SLAs of 99.95 % for the service tier and 99.9 %, 99.95 % and 99.99 % for a Flexible Server without HA, with same zone HA and with zone redundant HA ([High availability](https://learn.microsoft.com/en-us/azure/postgresql/high-availability/concepts-high-availability), [Service Level Agreements](https://www.microsoft.com/licensing/docs/view/Service-Level-Agreements-SLA-for-Online-Services)), and an assumed 99.99 % for a link:

| Shape | Read | Hours down a year | Shared write | Hours down a year |
|---|---|---|---|---|
| One node, no HA, the only option on Burstable | 99.85 % | 13.1 | 99.85 % | 13.1 |
| One node, zone redundant HA | 99.94 % | 5.3 | 99.94 % | 5.3 |
| Prophet with disciples, zone redundant HA | 99.94 % | 5.3 | 99.87 % | 11.4 |
| Disciple, no HA, one standby, `p = 0.99` | 99.95 % | 4.5 | as above | |
| Disciple of a mirroring prophet, zone redundant HA | 99.94 % | 5.3 | 99.80 % | 17.5 |

A read's availability never has a factor for a prophet or a peer, and one standby makes the database's own term negligible, so a disciple's read is as available as its API tier. A write's availability is the product along the chain to its home, which is why a shared write is routed one hop to the home where it can be, and why the home prophet is the server that earns zone redundant HA. The reference node publishes no SLA; the same formulas take the operator's own figures.

A replica keeps answering while its prophet is away, and the cost of that is staleness. What it serves in that time is bounded. A change committed at home at `t_0` reaches a connected disciple at `t_0 + τ`, with `τ` the staleness of [Sync](./sync.md#staleness). Across a partition of length `P` that starts after `t_0` it arrives at `t_0 + P + τ`. When the prophet itself is down no grant or revoke can be made at all, so the disciple serves nothing staler than the feed lag at the moment the prophet went away; when only the link is down, the prophet goes on deciding and the partitioned disciple is at most `P + τ` behind.

Why local answers: an access check sits on the path of every request, so its availability multiplies into the service's. A disciple's check depends on one database, the one the service already needs for its data, so it adds no failure mode the service did not already have. See [Comparison](./comparison.md) for the arithmetic.
