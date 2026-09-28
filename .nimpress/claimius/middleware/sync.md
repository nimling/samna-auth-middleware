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
| `GET /sam/sync/:seq` | `claimius.get_sync_events(app, seq, 1000)`, the next page after the disciple's cursor, held open up to 25 s while it is empty; a `409` naming the snapshot when the app's events after the cursor outnumber its source rows |

A request for the page after `seq` is the disciple's proof that it applied everything up to `seq`. The prophet records it in `prophet_state.last_applied_seq` and `last_contact_at`, which is what `trim_sync_event` trims against.

## The pull loop

`sam.SyncStart(ctx)` returns at once and owns its goroutine.

1. With the cursor at zero it fetches the snapshot, applies it and records the snapshot sequence. The snapshot is upserted only where a row differs, so rows the disciple already holds cost a probe and raise no area.

2. It then fetches pages after the cursor. A page is applied in one transaction under `replay_mode`: every payload upserted or deleted by primary key with `app_id` stripped, the claim areas each claim, binding and grant moves raised in `access_diff`, and the cursor moved, all together. One commit per page, whatever its size.

3. Between pulls the loop drains outstanding `access_diff` areas in transactions of about 1,000 pairs, the slices of the holders who pass the drain rule, most recently active first, so a disciple's listings rarely meet an area the feed raised; see [Materialization](../materialization.md#the-settle-as-a-queue).

4. It pulls again as soon as a page is applied. A pull that finds nothing is held open by the prophet, which listens on its feed channel and answers the moment an event of the app commits, or after 25 s with an empty page. A failure doubles the delay up to 30 s.

5. A cursor above the prophet's sequence for the app, a `409` answer to a pull, or ten failed pulls trigger a fresh snapshot and an audit row naming the cursor: none of them recovers by retrying. The `409` is the prophet's judgement that the snapshot is cheaper than the backlog, which holds once the backlog exceeds the app's source rows; see [Sync](../sync.md#catch-up). An empty pull is the normal answer of an idle app and triggers nothing.

A snapshot upserts; it never deletes a row the prophet did not send. In debug mode the loop does not start.

## Cost

A page of `n` events costs `n` upserts, the up set rows of its hierarchy rows, one area row per claim it moves, and one commit. With source rows only on the feed, `n` is the number of source changes of the app since the last pull, independent of how many users they reach.

The held pull costs the prophet one parked goroutine per disciple and one `LISTEN` connection per process, and replaces up to ten pulls a second per active disciple and a wait of up to 15 s after an idle period with one pull per page and a wait of half a round trip, the answer's way back. Throughput, staleness, catch up and the page size are derived in [Sync](../sync.md#the-feed-as-a-pipeline).
