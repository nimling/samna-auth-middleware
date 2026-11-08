---
layout: doc
title: Sync
description: The outbox of source rows a prophet writes, the (txid, seq) cursor served below the snapshot xmin, pages applied whole by one consumer per database, per consumer cursors and trimming capped at the source rows, and the cursor as the consistency token, in events and rows as functions of the data.
tags: sync_event, emit_sync_event, emit_app_object_sync_event, get_sync_events, get_sync_seq, check_sync_cursor, update_sync_cursor, trim_sync_event, sync_consumer, disciple_state, replay_mode, cursor, txid, xmin, consistency
lastUpdated: true
---

# Sync

A prophet appends every change of a mirrored source table to `sync_event`. A consumer of an app, a disciple database or a peer prophet, reads the app's events after its cursor and applies them to its own copy, where it derives its own pairs. The protocol that moves the events is pull, and it belongs to the middleware; see [Sync in Go](./middleware/sync.md).

## The row

`sync_event (seq bigserial, operation, event_type, payload jsonb, app_id, written_at, txid xid8)` carries three indexes: the primary key `seq`, `idx_sync_event_app_txid_seq (app_id, txid, seq)` and `idx_sync_event_written_brin`, a BRIN summary on `written_at`. `txid` defaults to `pg_current_xact_id()`, the transaction that wrote the event. An event costs one heap row and two btree entries; the BRIN summary changes only when a new range of pages starts ([PostgreSQL, BRIN Indexes](https://www.postgresql.org/docs/17/brin.html)).

`emit_sync_event`, an after row trigger on `samna_app`, `samna_user`, `samna_client`, `samna_secret`, `organization`, `location`, `claim`, `user_claim`, `claim_object`, `object_field`, `user_field`, `user_relation` and `app_handover`, writes in the transaction of the change, so an event exists exactly when its change committed. An `upsert` carries the full row less `txid`; a `delete` carries its `id` and `app_id`. The trigger writes nothing under `replay_mode` and nothing on a database with no `prophet_state` row, so a pure disciple writes no events.

An `organization` or `location` has no `app_id`. Its change is written once per app holding a live `app_object` link to it, stamped with that app, through `apps_for_object`. `emit_app_object_sync_event` on `app_object` publishes the linked row to the app when a link goes live and its delete to that app alone when the link ends, so a row linked after it was written reaches the app's consumers and an unlinked one leaves them.

## Events per write

The feed carries source rows and nothing derived from them. `user_object`, `user_users`, `user_users_diff`, `object_info`, `inheritance_info`, `access_diff` and `audit` never publish, `get_sync_events` excludes `claimius.user_object`, `claimius.user_users` and `claimius.audit` by event type, and the base files delete any such row left in the outbox.

```
events(write) = Σ_{source rows r written} n_apps(r)
```

with `n_apps(r) = 1` for a row carrying `app_id` and the number of live links for an organization or location. A settle writes pairs, `user_users_diff` rows and counts, never source rows, so it emits nothing; each database folds the rows of its own pairs. For the request of [Writes](./writes.md), a claim bound to a node and granted to `k` users, `events = 2 + k`, independent of `|T_c|` and `|H_c|`.

Measured on a prophet with one root, 200 organizations under it and 50 users, granting a claim bound to the root to the 50 users and letting each list its organizations: the feed took 50 `user_claim` events. Publishing the derived rows as well took 12,651 `user_object` and 227,850 `user_users` events more, 240,551 events against 50. The fan out of a grant to the pairs it reaches, `|T_c|` per holder, and of each pair to the users who share its object, `U_x − 1` per pair and direction, is exactly what a consumer derives locally instead of reading.

## The cursor

A position on the feed is `(txid, seq)`, written `<txid>.<seq>` on the wire. `get_sync_events(app_id, txid, seq, limit, peer)` returns the app's events with

```
(e.txid, e.seq) > (txid, seq)   and   (e.txid < pg_snapshot_xmin(pg_current_snapshot())  or  e.txid = the reader's own)
```

in `(txid, seq)` order, 1,000 by default, through one range of `idx_sync_event_app_txid_seq`. A pull with `n_a` of the app's events after its cursor reads `n_a` entries and no other app's.

Sequences are taken at insert and become visible at commit, so a higher `seq` can commit before a lower one, and a cursor on `seq` alone would pass the lower one over. A consumer that pages in `(txid, seq)` order and moves its cursor to the last event of each page never passes over an event, whatever order transactions commit in. Let the reading snapshot have `xmin`, the lowest transaction id still running when it was taken:

1. The feed serves an event only when `txid < xmin`, or when it is the reader's own transaction. A transaction id below `xmin` belongs to a transaction that has ended; an event of an aborted transaction does not exist, so every served event is committed and visible.

2. Every event not yet visible to the snapshot belongs to a transaction running at the snapshot or started after it. A running transaction has `txid ≥ xmin` by the definition of `xmin`; a later one has `txid ≥ xmax ≥ xmin`, since transaction ids are assigned in increasing order and `xid8` never wraps.

3. Hence every event served later has `txid ≥ xmin > txid(c)` for the cursor `c` taken from this page, so it sorts after `c`. Events below `xmin` not served because of the page limit sort after `c` as well, and come on the next page.

4. The events of one transaction share its `txid` and become visible together, so their `seq` order within the transaction is complete when any of them is served.

The snapshot sent to a new consumer carries the cursor `(xmin, 0)` of the snapshot it was read under, so the feed resumes exactly after it.

**Sources.** [PostgreSQL, Transaction ID and Snapshot Information Functions](https://www.postgresql.org/docs/17/functions-info.html#FUNCTIONS-PG-SNAPSHOT): `pg_current_xact_id`, `pg_current_snapshot`, `pg_snapshot_xmin`, and the definition of `xmin` as the earliest transaction still active. [PostgreSQL, Sequence Manipulation Functions](https://www.postgresql.org/docs/17/functions-sequence.html), on `nextval` never being rolled back.

`app_handover` events travel only with `peer = true`, to a peer prophet mirroring the app, never to a disciple.

## Applying a page

A consumer applies one page in one transaction under `replay_mode`:

1. It moves its cursor first, from the position the page was pulled at to the page's highest position, with an update whose `WHERE` names the old position. The row stays locked for the page, and a page pulled at a cursor that has since moved changes no row and is refused.

2. It upserts or deletes every payload by primary key. On a disciple `app_id` is stripped, `samna_app` events are skipped, and an upsert writes only when a column differs, so an unchanged row costs one probe and moves nothing.

3. For each `claim`, `claim_object` or `user_claim` payload that changed a row, it raises the claim areas the payload moves in its own `access_diff`: the claim itself, the linked claim for a composition link, and for an assignment the area of its one user. One or two rows, whatever the claim reaches. Hierarchy rows keep the closure through their calc triggers, which raise the object area under replay without settling any actor.

4. One commit. A page that fails anywhere rolls back whole, cursor included, and is pulled again from the same position.

```
per page of p events:  p upserts or deletes,  ≤ 2 area rows per claim event,  1 cursor row,  1 commit
```

Every pair a disciple holds, on the shared types and on its own registered tables, then settles on the next local read that meets the area; see [Materialization](./materialization.md).

A snapshot is applied the same way, as one page with the snapshot's cursor `(pg_snapshot_xmin, 0)` of the repeatable read transaction that read it: every event below that `xmin` is in the snapshot and every later event sorts after its cursor. It upserts and never deletes a row the prophet did not send.

## One consumer per database

The pull loop runs on every process of a service. Each takes a session level `pg_try_advisory_lock` on `hashtextextended(key, 0)`, the key `claimius.disciple_sync:<app slug>` for a disciple and `claimius.prophet_peer:<id>` for a peer row, on a dedicated connection, and only the holder pulls and applies. The lock dies with its session, so when the holder goes another process takes it. With `N` processes, `E` events are applied `E` times, not `N · E`, and no two consumers of one database race on its cursor.

## Cursors and trimming

`sync_consumer (app_id, consumer_id)` holds one cursor per consumer, named by its `disciple_state` or `prophet_peer` id. A pull for the page after a cursor is the consumer's proof that it applied everything up to it, and `update_sync_cursor` records it with the contact time. The app's `prophet_state` row carries the lowest cursor among its consumers in contact within 7 days.

`trim_sync_event(stale_after)` deletes, in two passes:

1. Every event at or below the lowest cursor among consumers in contact within `stale_after`, 7 days by default. A consumer out of contact longer is treated as gone, so one dead disciple cannot hold the outbox.

2. Per app, every event beyond as many as the app has source rows, `count_sync_source_rows`: the rows of the nine app scoped tables and the live organization and location links.

It records the highest position removed per app in `prophet_state (trimmed_txid, trimmed_seq)`. So the outbox of an app holds at most

```
events(app) ≤ R_a = Σ_{app scoped source tables} rows(app) + live links(app)
```

## Catch up

`check_sync_cursor(app_id, txid, seq)` decides, on every pull, whether the feed or the snapshot serves a consumer:

| Answer | When | The consumer |
|---|---|---|
| NULL | the feed can serve the cursor | pulls pages |
| `ahead` | `seq` above every sequence issued or `txid` above the snapshot's `xmax` | takes the snapshot |
| `trimmed` | the cursor is below `(trimmed_txid, trimmed_seq)` | takes the snapshot |
| `backlog` | the app's events after the cursor outnumber its source rows `R_a` | takes the snapshot |

The test reads at most 1,001 events and stops counting source rows at the number it compares against, so it costs `min(n_b, 1001)` entries for a consumer `n_b` events behind, and only past 1,000 events the count of `R_a`. A consumer `n_b ≤ R_a` events behind replays them in `⌈n_b / 1000⌉` pages and `n_b` applies; one further behind reads `R_a` rows once, which is fewer than the events it missed.

## The cursor is the consistency token

A write routed to the prophet answers with `SAuth-Seq`, the app's highest position after the write, from `get_sync_seq`, one descent of `idx_sync_event_app_txid_seq`. A read on the disciple that must see the write waits until the disciple's cursor reaches that position. This is the zookie of Zanzibar, a read "at least as fresh as" a known write ([Pang et al. 2019, section 2.2](https://www.usenix.org/system/files/atc19-pang.pdf)), and the `at_least_as_fresh` mode of SpiceDB ([SpiceDB, Consistency](https://authzed.com/docs/spicedb/concepts/consistency)). Without a token a disciple read is consistent as of its cursor, behind the prophet by the events it has not yet applied and by nothing else. A read on the prophet settles what it meets and answers in the database that holds the write.

## A disciple's own tables

The implementing side's own tables never enter the feed. Bindings on their rows are `claim_object` rows on the prophet, addressed by `object_type` and `object_id`, travel as any binding does, and settle on the disciple that holds the table.
