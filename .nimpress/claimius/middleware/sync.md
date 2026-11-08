---
gate: samna-developer
layout: doc
title: Sync in Go
description: The /sam/sync routes on the prophet, the pull loop sam.Init on the disciple under one leader per database, pages applied in one transaction, binding to one prophet, the peer pull, reconciling from a snapshot, and the drain between pulls.
tags: sam.Init, PeerInit, Replica, Move, LeadSync, SyncLockKey, PullSync, PullSnapshot, ApplyPage, ApplyPayload, SyncCursor, WaitForSeq, read your writes, SAuth-Prophet, SAuth-Home, SAuth-Seq, advisory lock, /sam/sync, snapshot, drain
lastUpdated: true
---

# Sync in Go

Sync is pull. The prophet serves two routes under `/sam`, behind `smw.AppMiddleware`, and nothing pushes: there is no inbound sync route on a disciple or on the prophet.

| Route | Serves |
|---|---|
| `GET /sam/sync/snapshot` | Every source row of the app, organizations and locations through `app_object_ids`, read in one repeatable read, read only transaction, and the cursor `(xmin, 0)` of that transaction's snapshot: every event below the snapshot's `xmin` is in it and every later one sorts after it. With `peer=true` the rows come from `claimius.get_peer_snapshot`, the app row and its handovers first |
| `GET /sam/sync/:cursor` | `claimius.get_sync_events(app, txid, seq, 1000, peer)` for the cursor `<txid>.<seq>`, the next page after the replica's cursor. With `wait` it holds an empty answer open up to that many seconds, at most `SyncHoldMax`, 25 s, and answers the moment an event of the app commits. A cursor `check_sync_cursor` judges `ahead`, `trimmed` or `backlog` answers `409` and the replica takes the snapshot |

Both routes take `consumer`, the replica's state id, and `prophet`, the slug the replica is bound to. A request for the page after a cursor is the replica's proof that it applied everything up to it, so the prophet records that cursor, not the served one, per consumer in `sync_consumer` through `update_sync_cursor`, which is what `trim_sync_event` trims against. The snapshot records its own cursor the same way. `get_sync_events` serves no `user_object`, `user_users` or `audit` row, and the snapshot carries source rows only: the replica derives its own pairs. See [Sync](../sync.md).

`client.SyncCursor{Txid, Seq}` is a position on a prophet's feed, ordered by `txid` and then `seq`, written `<txid>.<seq>` by `String` and read by `ParseSyncCursor`. The same form travels in the path of a pull and in `SAuth-Seq`, the header `smw.SyncSeq` stamps on every successful write answer with the app's position from `claimius.get_sync_seq`. `sdb.Instance().GetSeq(ctx)` reads the disciple's cursor, and `sdb.Instance().WaitForSeq(ctx, cursor, timeout)` waits until it reaches a routed write's position.

## Read your writes

A disciple answers a request that made routed writes only once its cursor has reached the highest position those writes answered with, so the client's next request, on any pod, sees them as a single local database would.

1. `smw.UserAuthMiddleware` holds every request it authenticates in this way; `smw.WaitForSeq(timeout)` does the same for a route outside it, and a request held by both waits once.
2. Every routed write of the request raises the position, `NewInsert`, `NewUpdate`, `NewDelete` and every helper that writes through them. A request that made none answers at once.
3. The wait runs when the answer is about to be written, after the handler has returned and its transaction has committed, so it holds no lock the apply of the page needs.
4. It lasts at most `UserAuthOptions.SeqTimeout`, 10 s when unset. When it runs out the answer still goes out, and a warning naming the position is logged.
5. An object gate, `MutationAccess`, `MutationAccessMany`, `GetAccess`, `GetDirectAccess`, `UserAccess` and `RootAccess`, called inside the handler after a routed write waits the same way before it reads, so the handler reads its own write. It runs on the primary outside any transaction of the handler; a gate called while the handler holds a transaction that copied a routed answer locally waits out the timeout, so gate before the transaction or after its commit.
6. A prophet, a disciple in debug mode, and a database without a `disciple_state` row never wait.

## One leader per database

`sam.Init(ctx)` returns at once and owns its goroutines; it returns an error only when it cannot start, and `sam.Done()` closes once the context has ended and every goroutine has returned ([Lifecycle](../../disciple.md#lifecycle)). In debug mode it starts nothing. On every pod it runs `LeadSync(ctx, db, SyncLockKey(app slug), 5 s, Disciple().Pull)`: the pod that takes the session level advisory lock `pg_try_advisory_lock(hashtextextended(key, 0))` on a dedicated connection runs the pull for as long as it holds it, and checks the connection every 5 s; every other pod tries the lock every 5 s and does no sync work. A holder whose work ends, or whose connection stops answering, discards the connection, which releases the lock, and the next pod to try takes over. However many pods run, each event is applied once. Every pod also follows the binding of the disciple, below, through `client.Bind`.

## The pull loop

`Replica.Pull` keeps a `sam.Replica` current, `sam.Disciple()` for the disciple database.

1. With the cursor at zero it fetches the snapshot and applies it with the snapshot cursor, as a page. The snapshot is upserted only where a row differs, so a row the replica already holds raises no area. A snapshot upserts; it never deletes a row the prophet did not send.

2. It then fetches pages after the cursor. `ApplyPage` applies a page in one transaction under `replay_mode`: the cursor moved first, from the position the page was pulled at and nowhere else, then every payload upserted or deleted by primary key with `app_id` stripped and `samna_app` skipped, an organization or location that leaves the app retired by `sa_deleted_at` rather than removed, since the host's own rows reference it, and the claim areas each claim, binding and grant moves raised in `access_diff`, all together. One commit per page, whatever its size; a page that fails rolls back whole and is pulled again, and a page pulled at a cursor that has since moved is refused.

3. On the disciple, between pulls and at most once a second, the loop calls `PostAccessDiffDrain(ctx, 1000)`, `claimius.drain_access_diff`. The drain settles the shares of at most 64 users whose reads keep pace with the areas raised since their cursor, most recently used first, trims the areas every active cursor has passed, and folds the `usage_diff` and `user_users_diff` rows of the oldest 1,000 objects; see [Materialization](../materialization.md#the-drain-and-the-trim).

4. It pulls again as soon as a page is applied, with `wait` 25, so an idle app holds one request open at a time.

5. A failed pull doubles the delay up to 30 s. A `409` answer to a pull, or ten failed pulls in a row, trigger a fresh snapshot, and a snapshot that fails to apply waits out the same doubling delay before the next pull: the delay and the failure count reset only once a pull or a snapshot applies, and the snapshot's transaction writes an audit row naming the stalled cursor through `sdb.Write`: neither recovers by retrying. The `409` is the prophet's judgement that the snapshot serves the replica better than the feed: its cursor is above anything the prophet issued, below what it trimmed, or behind by more events than the app has source rows. An empty pull is the normal answer of an idle app and triggers nothing.

## One prophet per replica

A replica relies on exactly one prophet, whose feed its cursor is a position on. Every pull names the bound prophet in `prophet`, and the prophet answers with its own slug in `SAuth-Prophet`. The disciple keeps both in `disciple_state.prophet` and `disciple_state.endpoint`, binds on its first answer, and routes its writes there through `client.Bind` on every pod.

A prophet serves a pull only when `check_sync_home` agrees: it is the app's home and the consumer is unbound, bound to it, or bound to a prophet that handed the app over to it and saw the handover accepted. Otherwise it answers `409` with the prophet the consumer stays with in `SAuth-Home`, which the client returns as `client.ErrNotHome`, never as a snapshot. A page answered by a prophet other than the bound one sends the disciple to the snapshot instead of applying it.

When the configured `SAUTH_ENDPOINT` differs from the bound endpoint, `Replica.Move` asks the configured prophet for its snapshot, naming the bound prophet, at most once a minute. A refusal keeps the disciple on its prophet, cursor and binding untouched; an answer is applied as the snapshot and rebinds the disciple, cursor included, in one transaction.

## The clean

A routed write commits on the prophet before the host's own transaction does, so a binding or a virtual claim can be left on the prophet for an object the host never committed. Those rows grant nothing and show nowhere, and the disciple is the only side that can tell them apart, holding both the bindings and the objects. Every `cleanInterval`, 10 minutes, the leader runs `CleanOrphans`: `get_orphan_bindings` lists the live bindings on a registered table of the host whose object is not there, and `get_orphan_claims` the live virtual claims with no live binding left, each older than `cleanGrace`, 10 minutes, so a binding copied ahead of the transaction that creates its object is never taken. Each is removed through the ordinary routed delete, so the prophet drops it and the feed carries that back. In debug mode a write is one local transaction and the clean finds nothing.

## The peer pull

`sam.PeerInit(ctx)` runs the same loop on a prophet for every row of `prophet_peer`, `shared.ProphetPeer`, one leader per row under the lock key `claimius.prophet_peer:<id>`, and picks up a row added later within 5 s. The replica is named by the row id, authenticates as the mirrored app through `client.WithTarget` with the row's endpoint, slug and secret, and pulls with `peer=true`, which serves the column shaped peer snapshot and the app's handovers. Each row applies through `claimius.apply_peer_event` under `replay_mode`; a row refused as a collision is recorded in `peer_collision` and the page goes on. It pulls while the row is `active` and `get_app_home` names the peer, and stops once this prophet accepts the app. No drain runs there; the prophet schedules its own. The mirrored `samna_app` row carries the peer in `home_prophet`, read as `SamnaApp.HomeProphet`. See [Prophets and disciples](../prophets.md).

## Cost

1. A page of `n` events costs `n` upserts, the closure rows of its hierarchy rows, one area row per claim it moves, and one commit. With source rows only on the feed, `n` is the number of source changes of the app since the last pull, independent of how many users they reach.
2. A snapshot row the replica already holds costs one probe and no write.
3. The held pull costs the prophet one parked request per replica and one `LISTEN` connection per process on `claimius_sync`. See [Sync](../sync.md).
