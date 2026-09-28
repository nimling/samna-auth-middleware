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

## Federation

Prophets federate by mirroring each other's apps. A prophet that serves users of an app homed on a peer is a disciple of the peer for that app, the shape `init_hybrid` sets up: its own apps at home, the peer's app mirrored.

1. Agreement is per app. Two prophets agree which of them is home for an app before either serves it. The home prophet decides every shared row of the app; the other mirrors it and routes its writes home.

2. A person is one `user_id` everywhere. The prophet that is home for the person's identity authenticates them and signs their tokens; a peer verifies those tokens against the home prophet's published keys and holds its own per app `samna_user` rows under the same `user_id`.

3. A peer's rows keep their ids. Because ids are random and never reassigned, a row mirrored across three prophets is the same row in all three, and a grant made at home binds the same object id everywhere.

4. Moving a disciple from one prophet to another is a change of home. It works only when the new prophet holds the disciple's app with the same ids, by mirroring it or by an agreed hand over, and the disciple starts again from the new prophet's snapshot with its cursor at zero. Two prophets that have not agreed share no ids and no sequence, so a disciple cannot be pointed at a prophet that does not hold its app.

## Capacity of the network

Anyone can host a prophet or a disciple, and prophets mirror each other's apps, so the question is where the network's ceiling sits. The formulas and constants are those of [Scaling](./scaling.md).

1. Reads have no shared term. A disciple answers from its own database, so `N` disciples carry `Σ_i min(N_c,i / c_a, (1 + R_i) × 0.7 k_i / c_r)`, linear in the disciples and in each one's standbys. No prophet is on a read's path, and none bounds the sum.

2. Writes of one app are bounded by its home prophet, `0.7 k_p / c_w`: 1,170 composite writes a second, a claim, a binding and three grants each, on 2 vCores. Federation adds write capacity by adding homes, `Σ_h 0.7 k_h / c_w`, never by splitting one app's writes between prophets, because one home decides every shared row of its app.

3. The feed costs the prophet about 5 µs per delivered event, so one core serves 200,000 deliveries a second. An event reaches its app's disciple and every peer mirroring the app, fan out `1 + P_a`. At the write ceiling, six events a write and fan out three, the feed takes 0.11 of a prophet core. Fan out never binds before writes do.

4. A disciple applies an event in about 80 µs, so its apply rate, 12,500 events a second on one core, exceeds any one prophet's write rate: a disciple never falls behind a healthy prophet by throughput, only by being away.

The ceiling of the network is therefore per app, the write rate of its home prophet, and per service, the reads of its own database. Neither is shared.

## Failure

| Down | Reads | Writes | Recovery |
|---|---|---|---|
| The prophet | Every disciple answers from its replica | Shared writes fail; a disciple's own tables keep working | The pull resumes from the cursor |
| A disciple | The prophet and other disciples are unaffected | None of its writes happen | It resumes from its cursor, or from a snapshot if trimmed past it |
| The network between them | As when the prophet is down | As when the prophet is down | The same |

A replica keeps answering while its prophet is away, and the cost of that is staleness. With independent failures a disciple's read needs only its own service, `A_read = A_api × A_db`, while a shared write needs the path home, `A_write = A_api × A_db × A_net × A_prophet`. The disciple answers without contact for a fraction `1 − A_net × A_prophet` of the time, 0.2 % or 17.5 hours a year at 99.9 % each; a central decision would have failed every read in those hours.

What it serves in them is bounded. A change committed at home at `t_0` reaches a connected disciple at `t_0 + τ`, `τ ≈ τ_rtt + n × 80 µs` with the held pull of [Sync](./sync.md). Across a partition of length `P` that starts after `t_0` it arrives at `t_0 + P + τ`. When the prophet itself is down no grant or revoke can be made at all, so the disciple serves nothing staler than the feed lag at the moment the prophet went away; when only the link is down, the prophet goes on deciding and the partitioned disciple is at most `P + τ` behind. A disciple away longer than the trim interval, 7 days, restarts from a snapshot.

Why local answers: an access check sits on the path of every request, so its availability multiplies into the service's. A disciple's check depends on one database, the one the service already needs for its data, so it adds no failure mode the service did not already have. See [Comparison](./comparison.md) for the arithmetic.
