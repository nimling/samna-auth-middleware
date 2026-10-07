---
layout: doc
title: Prophets and disciples
description: The authority and its mirrors, the three bootstraps, why ids are random and what that costs in collision probability, how prophets federate through peers with one home per app, refused collisions and agreed handovers, what each role reads and writes per request, and what happens when any of them fails.
tags: prophet, disciple, hybrid, federation, upsert_system_app, upsert_disciple_state, upsert_prophet_peer, prophet_peer, home_prophet, get_app_home, enforce_app_home, check_sync_home, apply_peer_event, peer_collision, app_handover, insert_app_handover, update_app_handover, availability, failure
lastUpdated: true
---

# Prophets and disciples

The same schema runs in two roles and their combination.

| Role | Bootstrap | Holds | Writes |
|---|---|---|---|
| Prophet | `upsert_system_app` | Every app it is home for, with keys and seeds, and every app it mirrors from a peer | Every shared row of the apps it is home for; their feeds |
| Disciple | `upsert_disciple_state(p_disciple_app_slug)` | One app, mirrored, without `app_id` and without `app_object` | Its own registered tables; routes shared writes to its prophet |
| Hybrid | `upsert_prophet_peer` | Its own system app, and one app of an upstream prophet mirrored as a peer | As a prophet |

Samna Auth is the prophet of the ecosystem. Bookable and Nimployer are disciples.

## The bootstraps

1. `upsert_system_app` creates the system app, its root organization, its system user, its administrator claim with its binding and grant, and the system app's `prophet_state` row marked with `system_app_slug`, and returns the six rows as one jsonb object.

2. `upsert_disciple_state(p_disciple_app_slug)` writes the one `disciple_state` row naming the mirrored app and grants the disciple roles. The rows arrive from the prophet's snapshot.

3. `upsert_prophet_peer(p_system_app_slug, p_system_app_name, p_system_app_private_key, p_system_app_private_seed, p_upstream_prophet, p_upstream_endpoint, p_upstream_app_slug, p_upstream_app_secret, ...)` refuses an empty upstream and an upstream named like itself, runs `upsert_system_app` with the system app fields, and upserts one `prophet_peer` row on `app_slug`: the upstream's slug and endpoint and the mirrored app's slug and secret, created by the system grant `upsert_system_app` returned. A hybrid is a prophet that mirrors one app the way prophets federate; it writes no `disciple_state` row, so `get_disciple_app` and `get_disciple_app_id` raise on a hybrid and `get_app_id` returns its system app.

## The principle

1. A prophet is the home of an app. It is the only place a shared row of the app, a grant, a binding, an organization linked to it, a user row, is decided. Every other copy mirrors it.

2. A replica relies on exactly one prophet. Its cursor is a position on that prophet's feed and means nothing anywhere else. It applies what that prophet sends under `replay_mode`, without deciding it again.

3. A disciple answers every read from its own database. The prophet is on the path of a shared write and of the feed, never of a read.

4. A prophet trusts a disciple as an app, no more. The app key authenticates the disciple for its feed and its routed writes; every routed write runs for that app alone and carries its actor as a grant of that app in `sa_created_by`, and the prophet's triggers judge the write against that grant: a binding may not hand out more than the grant's user holds on the object.

## What each role reads and writes

| Request | On the disciple | On the prophet | Messages between them |
|---|---|---|---|
| A check or a listing | The read of [Reads](./reads.md) | nothing | 0 |
| A write to the disciple's own table | The write of [Writes](./writes.md) | nothing | 0 |
| A shared write | One upsert of the returned row | The write, its actor's settle, its events | 1 request and its answer |
| A pull | `n` applies in one transaction | `n_a` entries of `idx_sync_event_app_txid_seq`, one cursor upsert | 1 request and its answer per page |

`N` disciples of one prophet answer their reads with no shared term. The writes of an app are decided on its home, and each event the home writes is read once per consumer of the app.

## Ids

Every row id is a version 4 UUID from `gen_random_uuid()`, 122 random bits ([RFC 9562, section 5.4](https://www.rfc-editor.org/rfc/rfc9562#section-5.4)). The id is given once, by the home prophet, or by the disciple for its own tables, and kept by every copy. The keys of the derived tables are source ids or `composite_id` hashes of them, so every copy derives the same keys.

The probability that `n` independent random ids contain a collision is at most

```
p(n) ≤ n (n − 1) / 2 · 2^−122 < n² / 2^123
```

`9.4 × 10^−20` for a billion ids and `9.4 × 10^−14` for a trillion. So no prophet reserves ranges or asks another before creating a row, and creating a row never waits on a peer: a create sends 0 messages.

## Federation

Prophets federate by mirroring each other's apps. A prophet that serves users of an app homed on a peer mirrors the app from the peer; a hybrid is one prophet set up that way from the start.

### Peers and homes

1. One `prophet_peer` row per mirrored app: the peer's slug `prophet`, its `endpoint`, the `app_slug` and `app_secret` the peer's `/sam` app authentication checks, the `peer_secret` the home granted it, a `status` of `active` or `suspended`, and this prophet's cursor on the peer's feed. `idx_prophet_peer_app_slug` is unique, so an app is mirrored from one peer at a time.

2. The pull is a disciple's pull, a consumer of the peer's feed of that app named by the row id, with `peer = true`. The home serves it only against a live `peer_grant` of the app: its admin calls `upsert_peer_grant(app, prophet, secret, created_by)`, which stores the SHA-256 of a random secret, the mirror keeps the raw secret in `prophet_peer.peer_secret`, `upsert_prophet_peer` takes it as `p_upstream_peer_secret`, and the pull presents it in `SAuth-Peer-Secret`. `get_peer_grant(app, secret)` finds the grant, and a peer pull without one is answered `403`. The app credential alone, which every disciple of the app holds, is served the app as a disciple: no handovers, no app keys and no organization seeds. Its first page is `get_peer_snapshot`, every source row of the app in the shape its events carry, the app row and its handovers first and parents before children, so each row applies against rows already applied. Each row is applied through `apply_peer_event` under `replay_mode`. The mirrored `samna_app` row carries the peer in `home_prophet`; an app homed here carries NULL.

3. `get_app_home(app)` is the prophet deciding the app's writes when it is not this one: `home_prophet`, or the peer this prophet has offered the app to while the offer stands. It is one primary key probe and one probe of the partial unique index `idx_app_handover_offered`.

4. Each app has one writer. `smw.AppHome` refuses a write request for an app another prophet decides with `409` and the home in `SAuth-Home`. The `tg_enforce_app_home` trigger on every source table and on `app_object` refuses any write outside replay to a row of such an app with SQLSTATE `P0409` and the home in the hint; an organization or location is refused when any app linked to it is decided elsewhere.

5. `check_sync_home(app, prophet)` decides whether this prophet serves the app's feed to a consumer bound to `prophet`: only when this prophet is the app's home and the consumer is unbound, bound here, or bound to a prophet that handed the app over here and saw the handover accepted. Otherwise the pull is refused with `409` and the prophet the consumer stays with in `SAuth-Home`. A mirror serves no consumer of an app it mirrors, so a disciple is `h = 1` feed from its app's home and a change reaches it after one pull.

### Collisions are refused

`_check_peer_row` refuses a row a peer's feed carries when the app is not decided by that peer, when the feed of one app carries another app's row, or when the row's id is held here by another app, or, for an organization or location, by an app of another home. `apply_peer_event` writes the refused row with its reason and payload to `peer_collision`, returns false, and the page goes on. With the bound above, a refused row is a seeding mistake, not chance. An applied row that changed raises the object areas and the assignment area it moves, as a disciple's apply does, and an organization or location upsert links it to the mirrored app, its delete unlinks it.

### Handovers

A handover moves an app's home in two steps, each one row of `app_handover`:

1. The home calls `insert_app_handover(app, peer, created_by)`. It refuses unless this prophet is the home and the peer is an active peer, inserts the offer and an audit row. From that commit `get_app_home` names the peer, so this prophet decides none of the app's writes. The offer travels on the app's feed to peers only.

2. The peer, which mirrors the app and so holds every row the app has, calls `update_app_handover(offer, created_by)`. It refuses unless the offer names it and the app is mirrored from the offering prophet, marks the offer `accepted`, sets `home_prophet` to NULL, and writes an audit row. This prophet is now the home.

3. The acceptance travels back on the new home's feed. The old home, which now pulls the app from the new home, applies it in `apply_peer_event`: its own offer turns `accepted` and `home_prophet` names the new home.

A disciple bound to the old home is served by the new one only after the acceptance; until then the new prophet refuses the pull and the disciple keeps its prophet. An offer is not withdrawn, because a withdrawal racing the acceptance would leave the app two homes.

## Failure

| Down | Reads | Writes | Recovery |
|---|---|---|---|
| The prophet | Every disciple answers from its copy | Shared writes fail; a disciple's own tables keep working | The pull resumes from the cursor |
| A disciple | The prophet and other disciples are unaffected | None of its writes happen | It resumes from its cursor, or from the snapshot once `check_sync_cursor` says `trimmed` or `backlog` |
| The link between them | As when the prophet is down | As when the prophet is down | The same |
| A peer that is home for a mirrored app | The mirror answers from its copy | Writes of that app fail everywhere but on the home | The peer pull resumes from its cursor |

### Availability by shape

With independent failures a request succeeds when every component on its path is up. `A_db` is a database with the service in front of it, `A_net` a link.

| Shape | Access decision | Shared write |
|---|---|---|
| Claimius, any role | `A_db` | `A_db` on the home |
| Disciple routing a write | `A_db` | `A_db,d · A_net · A_db,p` |
| A decision service in front of the data | `A_db · A_net · A_authz` | `A_db · A_net · A_authz` |

A decision never has a factor for a prophet or a peer, because it runs in the database the request already needs for its data. A write's availability is the product along the path to its home, which is why a shared write goes straight to the home in one hop. A disciple keeps answering while its prophet is away, and the cost is staleness: it is behind by the events written since its last applied page, and it replays them on return.
