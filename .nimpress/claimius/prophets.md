---
layout: doc
title: Prophets and disciples
description: The authority and its mirrors, what each holds and trusts, why ids are random and what that costs in collision probability, how prophets federate, what each role reads and writes per request, and what happens when any of them fails.
tags: prophet, disciple, hybrid, federation, init, init_disciple, init_hybrid, trust, uuid, availability, failure
lastUpdated: true
---

# Prophets and disciples

The same schema runs in two roles and their combination.

| Role | Bootstrap | Holds | Writes |
|---|---|---|---|
| Prophet | `init` | Every app it is home for, with keys and seeds | Every grant and shared row of its apps; writes their feeds |
| Disciple | `init_disciple` | One app, mirrored, without `app_id` or `app_object` | Its own registered tables; routes shared writes to its prophet |
| Hybrid | `init_hybrid` | Its own system app, and one app mirrored from an upstream prophet | Both of the above |

Samna Auth is the prophet of the ecosystem. Bookable and Nimployer are disciples.

## The principle

1. A prophet is the home of an app. It is the only place a shared row of the app, a grant, a binding, an organization it reaches, a user row, is decided. Every other copy is a mirror of it.

2. A disciple relies on exactly one prophet. Its cursor is a sequence of that prophet's outbox and means nothing anywhere else. It applies what its prophet sends under `replay_mode`, without deciding it again.

3. A disciple answers every read from its own database. The prophet is on the path of a shared write and of the feed, never of a read.

4. A prophet trusts a disciple as an app, no more. The app key authenticates the disciple for its feed and its routed writes; every routed write is authorized again on the prophet against the end user it is made for.

## What each role reads and writes

| Request | On the disciple | On the prophet | Messages between them |
|---|---|---|---|
| A check or a listing | The read of [Reads](./reads.md) | nothing | 0 |
| A write to the disciple's own table | The write of [Writes](./writes.md) | nothing | 0 |
| A shared write | One upsert of the returned row | The write, its settles and its events | 1 request and its answer |
| A pull | `n` applies | `n_a + 2h` entries read | 1 request and its answer per page |

So `N` disciples of one prophet answer their reads with no shared term: the prophet reads nothing for them. The writes of an app are decided on its home, and each event the home writes is read once per consumer of the app.

## Ids

Every row id is a version 4 UUID from `gen_random_uuid()`: 122 random bits ([RFC 9562, section 5.4](https://www.rfc-editor.org/rfc/rfc9562#section-5.4)). The id is given once, by the home prophet, or by the disciple for its own tables, and kept by every copy. The ids of the derived tables are `composite_id` hashes of source ids, so every copy derives the same ones.

The probability that `n` independent random ids contain a collision is at most `n² / 2^123`: `9.4 × 10^-20` for a billion ids and `9.4 × 10^-14` for a trillion. So no prophet reserves ranges or asks another before creating a row, and creating a row never waits on a peer.

Why random and not brokered: a prophet that drew its ids from ranges of `B_r` handed out by a broker would send one message per `B_r` rows and never collide, but would stop creating rows once cut off from the broker for longer than its range lasts, so every create would carry the broker's availability, `A_create = A_db × A_broker`. Random ids send no message and add no factor, and trade them for the collision probability above.

## Federation

Prophets federate by mirroring each other's apps. A prophet that serves users of an app homed on a peer is a disciple of the peer for that app, the shape `init_hybrid` sets up: its own apps at home, the peer's app mirrored.

1. Agreement is per app. Two prophets agree which of them is home for an app before either serves it. The home decides every shared row of the app; the other mirrors it and routes its writes home.

2. A person is one `user_id` everywhere. The prophet that is home for the person's identity authenticates them and signs their tokens; a peer verifies those tokens against the home's published keys and holds its own per app `samna_user` rows under the same `user_id`.

3. A peer's rows keep their ids. Because ids are random and never reassigned, a row mirrored across three prophets is the same row in all three, and a grant made at home binds the same object id everywhere.

4. Moving a disciple to another prophet is a change of home. It works only when the new prophet holds the disciple's app with the same ids, and the disciple starts again from the new prophet's snapshot with its cursor at zero.

A disciple of a mirror is `h` feeds from the home: a change reaches it after `h` pulls, one per hop, and a write routed from it travels `h` hops to the home and back.

## Failure

| Down | Reads | Writes | Recovery |
|---|---|---|---|
| The prophet | Every disciple answers from its copy | Shared writes fail; a disciple's own tables keep working | The pull resumes from the cursor |
| A disciple | The prophet and other disciples are unaffected | None of its writes happen | It resumes from its cursor, or from a snapshot below the trimmed watermark |
| The link between them | As when the prophet is down | As when the prophet is down | The same |

### Availability by shape

With independent failures, a request succeeds when every component on its path is up. `A_api` is the service tier, `A_db` a database, `A_net` a link.

| Shape | Read | Shared write |
|---|---|---|
| One node, the service and its database | `A_api A_db` | `A_api A_db` |
| Prophet with disciples | `A_api,d A_db,d` | `A_api,d A_db,d A_net A_api,p A_db,p` |
| Disciple of a mirroring prophet | `A_api,d A_db,d` | `A_d A_net A_m A_net A_h`, each `A` a service and its database |

A read's availability never has a factor for a prophet or a peer, so a disciple's read is as available as the service and the database it already needs for its data. A write's availability is the product along the chain to its home, which is why a shared write is routed to the home in as few hops as the shape allows.

A disciple keeps answering while its prophet is away, and the cost is staleness. When the prophet itself is down no grant or revoke can be made at all, so the disciple serves nothing staler than the events it had not yet pulled when the prophet went away. When only the link is down, the prophet goes on deciding and the disciple falls behind by the events written meanwhile, which it replays on return.
