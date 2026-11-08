---
gate: auth-middleware
layout: doc
title: Sync in Go
description: The /sam/sync routes on the prophet, the pull loop SyncStart on the disciple under one leader per database, pages applied in one transaction, binding to one prophet, the peer pull, reconciling from a snapshot, and the drain between pulls.
tags: SyncStart, PeerSyncStart, Replica, Move, LeadSync, SyncLockKey, PullSync, PullSnapshot, ApplyPage, ApplyPayload, SyncCursor, WaitForSeq, SAuth-Prophet, SAuth-Home, SAuth-Seq, advisory lock, /sam/sync, snapshot, drain
lastUpdated: true
---

# Sync in Go

Sync is pull. The prophet serves two routes under `/sam`, behind `smw.AppMiddleware`, and nothing pushes: there is no inbound sync route on a disciple or on the prophet.

| Route | Serves |
|---|---|
| `GET /sam/sync/snapshot` | Every source row of the app, organizations and locations through `app_object_ids`, read in one repeatable read, read only transaction, and the cursor `(xmin, 0)` of that transaction's snapshot: every event below the snapshot's `xmin` is in it and every later one sorts after it. With `peer=true` the rows come from `claimius.get_peer_snapshot`, the app row and its handovers first |
| `GET /sam/sync/:cursor` | `claimius.get_sync_events(app, txid, seq, 1000, peer)` for the cursor `<txid>.<seq>`, the next page after the replica's cursor. With `wait` it holds an empty answer open up to that many seconds, at most `SyncHoldMax`, 25 s, and answers the moment an event of the app commits. A cursor `check_sync_cursor` judges `ahead`, `trimmed` or `backlog` answers `409` and the replica takes the snapshot |

Both routes take `consumer`, the replica's state id, and `prophet`, the slug the replica is bound to. A request for the page after a cursor is the replica's proof that it applied everything up to it, so the prophet records that cursor, not the served one, per consumer in `sync_consumer` through `update_sync_cursor`, which is what `trim_sync_event` trims against. The snapshot records its own cursor the same way. `get_sync_events` serves no `user_object`, `user_users` or `audit` row, and the snapshot carries source rows only: the replica derives its own pairs. See [Sync](../sync.md).

`client.SyncCursor{Txid, Seq}` is a position on a prophet's feed, ordered by `txid` and then `seq`, written `<txid>.<seq>` by `String` and read by `ParseSyncCursor`. The same form travels in the path of a pull and in `SAuth-Seq`, the header `smw.SyncSeq` stamps on every successful write answer with the app's position from `claimius.get_sync_seq`. `sam.GetSeq` reads the disciple's cursor, and `sam.WaitForSeq(ctx, cursor, timeout)` waits until it reaches a routed write's position.

## One leader per database

`sam.SyncStart(ctx)` returns at once and owns its goroutines. In debug mode it does nothing. On every pod it runs `LeadSync(ctx, db, SyncLockKey(app slug), 5 s, Disciple().Pull)`: the pod that takes the session level advisory lock `pg_try_advisory_lock(hashtextextended(key, 0))` on a dedicated connection runs the pull for as long as it holds it, and checks the connection every 5 s; every other pod tries the lock every 5 s and does no sync work. A holder whose work ends, or whose connection stops answering, discards the connection, which releases the lock, and the next pod to try takes over. `N` pods apply each event once. Every pod also follows the binding of the disciple, below, through `client.Bind`.

## The pull loop

`Replica.Pull` keeps a `sam.Replica` current, `sam.Disciple()` for the disciple database.

1. With the cursor at zero it fetches the snapshot and applies it with the snapshot cursor, as a page. The snapshot is upserted only where a row differs, so rows the replica already holds cost a probe and raise no area. A snapshot upserts; it never deletes a row the prophet did not send.

2. It then fetches pages after the cursor. `ApplyPage` applies a page in one transaction under `replay_mode`: the cursor moved first, from the position the page was pulled at and nowhere else, then every payload upserted or deleted by primary key with `app_id` stripped and `samna_app` skipped, and the claim areas each claim, binding and grant moves raised in `access_diff`, all together. One commit per page, whatever its size; a page that fails rolls back whole and is pulled again, and a page pulled at a cursor that has since moved is refused.

3. On the disciple, between pulls and at most once a second, the loop calls `PostAccessDiffDrain(ctx, 1000)`, `claimius.drain_access_diff`, which settles up to 1,000 holder shares of open areas that pass the drain rule, most recently active holder first, of at most 64 users, and then folds the `user_users_diff` and `usage_diff` rows of the oldest 1,000 objects and flushes; see [Materialization](../materialization.md#the-drain).

4. It pulls again as soon as a page is applied, with `wait` 25, so an idle app costs one held request per hold.

5. A failed pull doubles the delay up to 30 s. A `409` answer to a pull, or ten failed pulls in a row, trigger a fresh snapshot, and a snapshot that fails to apply waits out the same doubling delay before the next pull: the delay and the failure count reset only once a pull or a snapshot applies, and the snapshot's transaction writes an audit row naming the stalled cursor through `sdb.Write`: neither recovers by retrying. The `409` is the prophet's judgement that the snapshot serves the replica better than the feed: its cursor is above anything the prophet issued, below what it trimmed, or behind by more events than the app has source rows. An empty pull is the normal answer of an idle app and triggers nothing.

## One prophet per replica

A replica relies on exactly one prophet, whose feed its cursor is a position on. Every pull names the bound prophet in `prophet`, and the prophet answers with its own slug in `SAuth-Prophet`. The disciple keeps both in `disciple_state.prophet` and `disciple_state.endpoint`, binds on its first answer, and routes its writes there through `client.Bind` on every pod.

A prophet serves a pull only when `check_sync_home` agrees: it is the app's home and the consumer is unbound, bound to it, or bound to a prophet that handed the app over to it and saw the handover accepted. Otherwise it answers `409` with the prophet the consumer stays with in `SAuth-Home`, which the client returns as `client.ErrNotHome`, never as a snapshot. A page answered by a prophet other than the bound one sends the disciple to the snapshot instead of applying it.

When the configured `SAUTH_ENDPOINT` differs from the bound endpoint, `Replica.Move` asks the configured prophet for its snapshot, naming the bound prophet, at most once a minute. A refusal keeps the disciple on its prophet, cursor and binding untouched; an answer is applied as the snapshot and rebinds the disciple, cursor included, in one transaction.

## The peer pull

`sam.PeerSyncStart(ctx, db)` runs the same loop on a prophet for every row of `prophet_peer`, `shared.ProphetPeer`, one leader per row under the lock key `claimius.prophet_peer:<id>`, and picks up a row added later within 5 s. The replica is named by the row id, authenticates as the mirrored app through `client.WithTarget` with the row's endpoint, slug and secret, and pulls with `peer=true`, which serves the column shaped peer snapshot and the app's handovers. Each row applies through `claimius.apply_peer_event` under `replay_mode`; a row refused as a collision is recorded in `peer_collision` and the page goes on. It pulls while the row is `active` and `get_app_home` names the peer, and stops once this prophet accepts the app. No drain runs there; the prophet schedules its own. The mirrored `samna_app` row carries the peer in `home_prophet`, read as `SamnaApp.HomeProphet`. See [Prophets and disciples](../prophets.md).

## Cost

A page of `n` events costs `n` upserts, the closure rows of its hierarchy rows, one area row per claim it moves, and one commit. With source rows only on the feed, `n` is the number of source changes of the app since the last pull, independent of how many users they reach. The held pull costs the prophet one parked request per replica and one `LISTEN` connection per process on `claimius_sync`. See [Sync](../sync.md).
