---
layout: doc
title: Sync in Go
description: The /sam/sync routes on the prophet, the pull loop SyncStart on the disciple, pages applied in one transaction, backoff, reconciling from a snapshot, and the drain between pulls.
tags: SyncStart, PullSync, PullSnapshot, ApplyPayload, /sam/sync, backoff, snapshot, drain
lastUpdated: true
---

# Sync in Go

Sync is pull. The prophet serves two routes under `/sam`, behind `smw.AppMiddleware`, and nothing pushes.

| Route | Serves |
|---|---|
| `GET /sam/sync/snapshot` | Every source row of the app, organizations and locations through `app_object_ids`, and the prophet's highest sequence |
| `GET /sam/sync/:seq` | `claimius.get_sync_events(app, seq, 1000)`, the next page after the disciple's cursor, held open up to 25 s while it is empty |

A request for the page after `seq` is the disciple's proof that it applied everything up to `seq`. The prophet records it in `prophet_state.last_applied_seq` and `last_contact_at`, which is what `trim_sync_event` trims against.

## The pull loop

`sam.SyncStart(ctx)` returns at once and owns its goroutine.

1. With the cursor at zero it fetches the snapshot, applies it and records the snapshot sequence.

2. It then fetches pages after the cursor. A page is applied in one transaction under `replay_mode`: every payload upserted or deleted by primary key with `app_id` stripped, the claim areas each claim, binding and grant moves raised in `access_diff`, and the cursor moved, all together. One commit per page, whatever its size.

3. Between pulls the loop drains outstanding `access_diff` areas in batches, so a disciple's reads rarely meet an area the feed raised.

4. It pulls again as soon as a page is applied. A pull that finds nothing is held open by the prophet, which listens on its feed channel and answers the moment an event of the app commits, or after 25 s with an empty page. A failure doubles the delay up to 30 s.

5. A cursor above the prophet's sequence for the app, or ten failed pulls, trigger a fresh snapshot and an audit row naming the stalled cursor: neither recovers by retrying. An empty pull is the normal answer of an idle app and triggers nothing.

A snapshot upserts; it never deletes a row the prophet did not send. In debug mode the loop does not start.

## Cost

A page of `n` events costs `n` upserts, the closure edges of its hierarchy rows, one area row per claim it moves, and one commit. With source rows only on the feed, `n` is the number of source changes of the app since the last pull, independent of how many users they reach.

The held pull costs the prophet one parked goroutine per disciple and one `LISTEN` connection per process, and replaces up to ten pulls a second per active disciple and a wait of up to 15 s after an idle period with one pull per page and a wait of one round trip. See [Scaling](../scaling.md).
