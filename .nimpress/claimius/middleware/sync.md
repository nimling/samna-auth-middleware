---
layout: doc
title: Sync in Go
description: The /sam/sync routes on the prophet, the pull loop SyncStart on the disciple, pages applied in one transaction, backoff, reconciling from a snapshot, and the drain between pulls.
tags: SyncStart, LeadSync, PullSync, PullSnapshot, ApplyPage, ApplyPayload, advisory lock, /sam/sync, backoff, snapshot, drain
lastUpdated: true
---

# Sync in Go

Sync is pull. The prophet serves two routes under `/sam`, behind `smw.AppMiddleware`, and nothing pushes.

| Route | Serves |
|---|---|
| `GET /sam/sync/snapshot` | Every source row of the app, organizations and locations through `app_object_ids`, and the cursor `(xmin, 0)` of its snapshot: every event below the snapshot's `xmin` is in it and every later one sorts after it |
| `GET /sam/sync/:cursor` | `claimius.get_sync_events(app, txid, seq, 1000)` for the cursor `<txid>.<seq>`, the next page after the disciple's cursor, held open up to 25 s while it is empty; a `409` naming the snapshot when the app's events after the cursor outnumber its source rows |

A request for the page after a cursor is the disciple's proof that it applied everything up to it. The disciple names itself by its `disciple_state` id, and the prophet records the cursor per consumer in `sync_consumer` through `update_sync_cursor`, which is what `trim_sync_event` trims against. A write answers with the app's position in the `SAuth-Seq` header as `<txid>.<seq>`; the routed query exposes it as `Seq()`, a `client.SyncCursor`, and `WaitForSeq` waits on it.

## The pull loop

`sam.SyncStart(ctx)` returns at once and owns its goroutine. It runs `LeadSync` on every pod: the pod that takes the session level advisory lock `SyncLockKey(app slug)` on a dedicated connection runs the loop below for as long as it holds it, and checks the connection every 5 s; every other pod tries the lock every 5 s and does no sync work. A holder that stops, or whose connection stops answering, discards the connection, which releases the lock, and the next pod to try takes over.

1. With the cursor at zero it fetches the snapshot and applies it with the snapshot cursor in one transaction, as a page. The snapshot is upserted only where a row differs, so rows the disciple already holds cost a probe and raise no area.

2. It then fetches pages after the cursor. A page is applied in one transaction under `replay_mode`: the cursor moved first, from the position the page was pulled at and nowhere else, then every payload upserted or deleted by primary key with `app_id` stripped and the claim areas each claim, binding and grant moves raised in `access_diff`, all together. One commit per page, whatever its size; a page that fails rolls back whole and is pulled again, and a page pulled at a cursor that has since moved is refused.

3. Between pulls the loop drains outstanding `access_diff` areas in transactions of about 1,000 pairs, the slices of the holders who pass the drain rule, most recently active first, so a disciple's listings rarely meet an area the feed raised; see [Materialization](../materialization.md#the-settle-as-a-queue).

4. It pulls again as soon as a page is applied. A pull that finds nothing is held open by the prophet, which listens on its feed channel and answers the moment an event of the app commits, or after 25 s with an empty page. A failure doubles the delay up to 30 s.

5. A cursor above the prophet's sequence for the app, a `409` answer to a pull, or ten failed pulls trigger a fresh snapshot, and the snapshot's transaction writes an audit row naming the cursor: none of them recovers by retrying. The `409` is the prophet's judgement that the snapshot is cheaper than the backlog, which holds once the backlog exceeds the app's source rows; see [Sync](../sync.md#catch-up). An empty pull is the normal answer of an idle app and triggers nothing.

A snapshot upserts; it never deletes a row the prophet did not send. In debug mode the loop does not start.

## Cost

A page of `n` events costs `n` upserts, the up set rows of its hierarchy rows, one area row per claim it moves, and one commit. With source rows only on the feed, `n` is the number of source changes of the app since the last pull, independent of how many users they reach.

The held pull costs the prophet one parked goroutine per disciple and one `LISTEN` connection per process, and replaces up to ten pulls a second per active disciple and a wait of up to 15 s after an idle period with one pull per page and a wait of half a round trip, the answer's way back. Throughput, staleness, catch up and the page size are derived in [Sync](../sync.md#the-feed-as-a-pipeline).
