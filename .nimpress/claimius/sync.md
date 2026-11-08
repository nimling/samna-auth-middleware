---
gate: samna-developer
layout: doc
title: Sync
description: The outbox of source rows a prophet writes, the (txid, seq) cursor served below the snapshot xmin, pages applied whole by one consumer per database, per consumer cursors and trimming capped at the source rows, and the cursor as the consistency token, in events and rows as functions of the data.
tags: sync_event, emit_sync_event, emit_app_object_sync_event, get_sync_events, get_sync_payload, get_sync_seq, check_sync_cursor, update_sync_cursor, get_sync_consumers, trim_sync_event, sync_consumer, disciple_state, replay_mode, cursor, txid, xmin, consistency
lastUpdated: true
---

# Sync

A prophet appends every change of a mirrored source table to `sync_event`. A consumer of an app, a disciple database or a peer prophet, reads the app's events after its cursor and applies them to its own copy, where it derives its own pairs. The protocol that moves the events is pull, and it belongs to the middleware; see [Sync in Go](./middleware/sync.md).

The prophet to disciple stream is an append only log of state changes with a monotonic sequence, consumed idempotently, with failures retried rather than dropped. That is the delivery contract behind [RFC 8417](https://www.rfc-editor.org/rfc/rfc8417) security event tokens and the push and poll profiles in [RFC 8935](https://www.rfc-editor.org/rfc/rfc8935) and [RFC 8936](https://www.rfc-editor.org/rfc/rfc8936); the middleware takes the poll shape. Claimius carries the payload as source rows rather than as signed json web tokens, because both ends are databases under one authority rather than independent parties needing to verify each other: a consumer pulls with its own app credentials.

## The row

`sync_event (seq bigserial, operation, event_type, payload jsonb, app_id, written_at, txid xid8)` is indexed by its primary key `seq`, by `idx_sync_event_app_txid_seq (app_id, txid, seq)` and by `idx_sync_event_written_brin`, a BRIN summary on `written_at`. `txid` defaults to `pg_current_xact_id()`, the transaction that wrote the event.

`emit_sync_event`, an after row trigger on `samna_app`, `samna_user`, `samna_client`, `samna_secret`, `organization`, `location`, `claim`, `user_claim`, `claim_object`, `object_field`, `user_field`, `user_relation`, `object_asset`, `code` and `app_handover`, writes in the transaction of the change, so an event exists exactly when its change committed. An `upsert` carries the full row less `txid`; a `delete` carries its `id` and `app_id`. The trigger writes nothing under `replay_mode` and nothing on a database with no `prophet_state` row, so a pure disciple writes no events.

An `organization` or `location` has no `app_id`. Its change is written once per app holding a live `app_object` link to it, stamped with that app, through `apps_for_object`. `emit_app_object_sync_event` on `app_object` publishes the linked row and its live asset links to the app when a link goes live, and the delete of its asset links and of the row to that app alone when the link ends, so a row linked after it was written reaches the app's consumers and an unlinked one leaves them. An asset link is written once per app that sees its object; see [Assets](./assets.md).

A disciple therefore holds the organizations linked to its app and no other:

1. The root of a linked organization's tree may lie outside the app, and the `sa_root_id` of a claim or binding owned under it then names an organization the disciple never holds. On the disciple `claim.sa_root_id` and `claim_object.sa_root_id` carry no foreign key, like `organization.sa_root_id`, so such a row applies and the root stays an opaque id.

2. An organization or location whose parent the disciple does not hold is the top of its tree on the disciple: `get_owner_ancestors` returns it at hop 0 and its descendants hang under it, and the disciple init builds that closure for any such node that lacks one.

3. A parent the disciple holds but which has left the closure, a soft deleted one, still takes its subtree out.

## What the feed carries

The feed carries source rows and nothing derived from them. `user_object`, `user_users`, `user_users_diff`, `object_info`, `inheritance_info`, `access_diff` and `audit` never publish, `get_sync_events` excludes `claimius.user_object`, `claimius.user_users`, `claimius.user_users_diff` and `claimius.audit` by event type, and the base files delete any such row left in the outbox. A settle writes pairs, `user_users_diff` rows and counts, never source rows, so it emits nothing; each database derives and folds the rows of its own pairs.

Two kinds of event travel only to a peer prophet, a pull with `peer = true`: `app_handover` events, and an organization's `private_seed`, wrapped under the prophet's `SAUTH_MASTER_KEY`, which `get_sync_events` strips from every organization payload it serves to a disciple.

A `user_identity` reaches every app its organization is linked to without its `data`, and an app other than the identity's own `app_id` receives it with `claim_id` set to null: the claim belongs to the issuing app, a consumer of another app never holds it, and the consumer's foreign key on `claim_id` would refuse the row and stall the page. `get_sync_payload(app, event_type, payload, peer)` is the one rule for what a payload leaves out, called by `get_sync_events` and by the snapshot for every `user_identity` row.

The app's own `private_seed` is never in the feed. A disciple fetches it from `GET /sam/app` with its app slug and secret at startup and holds it in memory; see [Security](./security.md), Secrets 7.

## The cursor

A position on the feed is `(txid, seq)`, written `<txid>.<seq>` on the wire. `get_sync_events(app_id, txid, seq, limit, peer)` returns the app's events after the position, 1,000 by default, in `(txid, seq)` order, and only events whose transaction is below the `xmin` of the reading snapshot, or the reader's own. A consumer that moves its cursor to the last event of each page never passes over an event, whatever order transactions commit in; see [Why the cursor never passes an event](#why-the-cursor-never-passes-an-event).

The snapshot sent to a new consumer carries the cursor `(xmin, 0)` of the snapshot it was read under, so the feed resumes exactly after it.

## Applying a page

A consumer applies one page in one transaction under `replay_mode`:

1. It moves its cursor first, from the position the page was pulled at to the page's highest position, with an update whose `WHERE` names the old position. The row stays locked for the page, and a page pulled at a cursor that has since moved changes no row and is refused.

2. It upserts or deletes every payload by primary key. On a disciple `app_id` is stripped, `samna_app` events are skipped, and an upsert writes only when a column differs, so an unchanged row moves nothing.

3. For each `claim`, `claim_object` or `user_claim` payload that changed a row, it raises the areas the payload moves in its own `access_diff`, through the same functions a local write uses, stamped with the applying transaction: a claim payload one object area per live binding of the claim and one on the claim object, a link the same on both claims, a binding on an object that object's area with `cascade`, and an assignment the area of its one user. Hierarchy rows keep the closure through their calc triggers, which raise the object area under replay without settling any actor. A disciple's readers then settle from their own cursors and marks: nothing derived travels.

4. One commit. A page that fails anywhere rolls back whole, cursor included, and is pulled again from the same position.

Every pair a disciple holds, on the shared types and on its own registered tables, then settles on the next local read that meets the area; see [Materialization](./materialization.md).

A snapshot is applied the same way, as one page with the snapshot's cursor `(pg_snapshot_xmin, 0)` of the repeatable read transaction that read it: every event below that `xmin` is in the snapshot and every later event sorts after its cursor. It upserts and never deletes a row the prophet did not send.

## One consumer per database

The pull loop runs on every process of a service. Each takes a session level `pg_try_advisory_lock` on `hashtextextended(key, 0)`, the key `claimius.disciple_sync:<app slug>` for a disciple and `claimius.prophet_peer:<id>` for a peer row, on a dedicated connection, and only the holder pulls and applies. The lock dies with its session, so when the holder goes another process takes it. No two consumers of one database race on its cursor.

## Cursors and trimming

`sync_consumer (app_id, consumer_id)` holds one cursor per consumer, named by its `disciple_state` or `prophet_peer` id. A pull for the page after a cursor is the consumer's proof that it applied everything up to it, and `update_sync_cursor` records it with the contact time and, in `peer`, whether the pull was a peer prophet's, `peer = true`. The app's `prophet_state` row carries the lowest cursor among its consumers in contact within 7 days.

`trim_sync_event(stale_after)` deletes, in two passes:

1. Every event at or below the lowest cursor among all consumers in contact within `stale_after`, 7 days by default. A consumer out of contact longer is treated as gone, so one dead disciple cannot hold the outbox.

2. Per app, every event beyond as many as the app has source rows, `count_sync_source_rows`: the rows of the ten app scoped tables `samna_user`, `claim`, `user_claim`, `claim_object`, `samna_client`, `samna_secret`, `object_field`, `user_field`, `user_relation` and `code`, the live organization and location links, and the asset links published to the app.

It records the highest position removed per app in `prophet_state (trimmed_txid, trimmed_seq)`. It is the `sync_event` prune of the retention job, run once a day with `stale_after` the days of that row.

## Catch up

`check_sync_cursor(app_id, txid, seq)` decides, on every pull, whether the feed or the snapshot serves a consumer:

| Answer | When | The consumer |
|---|---|---|
| NULL | the feed can serve the cursor | pulls pages |
| `ahead` | `seq` above every sequence issued or `txid` above the snapshot's `xmax` | takes the snapshot |
| `trimmed` | the cursor is below `(trimmed_txid, trimmed_seq)` | takes the snapshot |
| `backlog` | the app's events after the cursor outnumber its source rows | takes the snapshot |

A consumer a little behind replays the events it missed; one so far behind that the events outnumber the app's rows reads the rows once instead.

## Where a consumer stands

`get_sync_consumers(app_id)` reads every consumer of an app's feed, the lowest cursor first, one row each:

| Column | Is |
|---|---|
| `consumer_id`, `peer` | the consumer's `disciple_state` or `prophet_peer` id, and whether it pulls as a peer prophet |
| `txid`, `seq` | its cursor, the position its last pull proved it applied |
| `last_contact_at`, `in_contact` | its last pull, and whether that lies within the 7 days `trim_sync_event` and `prophet_state` count as in contact |
| `head_txid`, `head_seq` | the app's head, `get_sync_seq` |
| `trimmed_txid`, `trimmed_seq` | the app's trimmed position in `prophet_state`; a cursor below it is answered `trimmed` |
| `behind` | the events the feed serves this consumer after its cursor, counted up to 1,001 as `check_sync_cursor` counts, so 1,001 reads as more than 1,000 |
| `next_txid`, `next_seq`, `next_operation`, `next_event_type`, `next_payload_id` | the first of them, the event the consumer's next page starts with; NULL at the head |

`behind` and the first event apply the feed's own filters per consumer: an event below the snapshot's `xmin`, no derived type, and `app_handover` only for a peer. Sync lag reads from it against `disciple_state (last_applied_txid, last_applied_seq)` on the disciple:

1. A consumer whose contact moves while its cursor stays and `behind` is above 0 pulls the page that starts at `next_*` and fails to apply it; the event names the row.
2. A consumer whose contact has stopped has stopped pulling; `in_contact` false means the trim no longer waits for it.
3. The disciple's own cursor runs at most one page ahead of the one recorded here, the page it applied after its last pull.

In Go it is `prophet/sdb.GetSyncConsumers(ctx, appID)`; the middleware mounts no route for it.

## The cursor is the consistency token

A write routed to the prophet answers with `SAuth-Seq`, the app's highest position after the write, from `get_sync_seq`. A read on the disciple that must see the write waits until the disciple's cursor reaches that position: a read at least as fresh as a known write. Without a token a disciple read is consistent as of its cursor, behind the prophet by the events it has not yet applied and by nothing else. A read on the prophet settles what it meets and answers in the database that holds the write.

## A disciple's own tables

The implementing side's own tables never enter the feed. Bindings on their rows are `claim_object` rows on the prophet, addressed by `object_type` and `object_id`, travel as any binding does, and settle on the disciple that holds the table.

## Cost

An event costs one heap row and two btree entries; the BRIN summary changes only when a new range of pages starts ([PostgreSQL, BRIN Indexes](https://www.postgresql.org/docs/17/brin.html)).

```
events(write) = Σ_{source rows r written} n_apps(r)
```

with `n_apps(r) = 1` for a row carrying `app_id`, the number of live links for an organization or location, and the number of apps that see the object for an asset link. For the request of [Writes](./writes.md), a claim bound to a node and granted to `k` users, `events = 2 + k`, independent of `|T_c|` and `|H_c|`.

A pull with `n_a` of the app's events after its cursor reads `n_a` entries of `idx_sync_event_app_txid_seq` and no other app's.

```
per page of p events:  p upserts or deletes,  ≤ 2 area rows per claim event,  1 cursor row,  1 commit
```

A claim payload raises `n_c + 1` areas, one per live binding of the claim and one on the claim object. With `N` processes of one service, `E` events are applied `E` times, not `N · E`.

After a trim the outbox of an app holds at most

```
events(app) ≤ R_a = Σ_{app scoped source tables} rows(app) + live links(app) + asset links(app)
```

`check_sync_cursor` reads at most 1,001 events and stops counting source rows at the number it compares against, so it costs `min(n_b, 1001)` entries for a consumer `n_b` events behind, and only past 1,000 events the count of `R_a`. A consumer `n_b ≤ R_a` events behind replays them in `⌈n_b / 1000⌉` pages and `n_b` applies; one further behind reads `R_a` rows once, which is fewer than the events it missed.

`get_sync_seq` is one descent of `idx_sync_event_app_txid_seq`.

`get_sync_consumers` reads the app's consumers by the primary key of `sync_consumer`, the head once and the `prophet_state` row once, and per consumer one descent of `idx_sync_event_app_txid_seq` reading `min(n_b, 1001)` entries for a consumer `n_b` events behind, the count and the first event out of the same ordered range:

```
reads(get_sync_consumers) = 1 + Σ_{consumers k} (1 + min(n_k, 1001))
```

It never scans `sync_event` and reads no other app's entries.

## Proofs and measurements

### Why the cursor never passes an event

`get_sync_events` serves

```
(e.txid, e.seq) > (txid, seq)   and   (e.txid < pg_snapshot_xmin(pg_current_snapshot())  or  e.txid = the reader's own)
```

Sequences are taken at insert and become visible at commit, so a higher `seq` can commit before a lower one, and a cursor on `seq` alone would pass the lower one over. Let the reading snapshot have `xmin`, the lowest transaction id still running when it was taken:

1. The feed serves an event only when `txid < xmin`, or when it is the reader's own transaction. A transaction id below `xmin` belongs to a transaction that has ended; an event of an aborted transaction does not exist, so every served event is committed and visible.

2. Every event not yet visible to the snapshot belongs to a transaction running at the snapshot or started after it. A running transaction has `txid ≥ xmin` by the definition of `xmin`; a later one has `txid ≥ xmax ≥ xmin`, since transaction ids are assigned in increasing order and `xid8` never wraps.

3. Hence every event served later has `txid ≥ xmin > txid(c)` for the cursor `c` taken from this page, so it sorts after `c`. Events below `xmin` not served because of the page limit sort after `c` as well, and come on the next page.

4. The events of one transaction share its `txid` and become visible together, so their `seq` order within the transaction is complete when any of them is served.

The snapshot's cursor `(xmin, 0)` holds by the same argument: every event below that `xmin` is in the snapshot, and every later event has `txid ≥ xmin` and sorts after it.

### Why the feed carries no derived rows

Measured on a prophet with one root, 200 organizations under it and 50 users, granting a claim bound to the root to the 50 users and letting each list its organizations: the feed took 50 `user_claim` events. Publishing the derived rows as well would take 12,651 `user_object` and 227,850 `user_users` events more, 240,551 events against 50. The fan out of a grant to the pairs it reaches, `|T_c|` per holder, and of each pair to the users who share its object, `U_x − 1` per pair and direction, is exactly what a consumer derives locally instead of reading.

### Where a consumer stands, measured

Measured on a prophet holding 200,000 events, 100,000 of them the app's, with three consumers of the app at `0.0`, two of the app's events behind the head and at the head, `EXPLAIN (ANALYZE, BUFFERS)` of `SELECT * FROM claimius.get_sync_consumers(app)`, which the planner inlines:

```
Index Only Scan Backward using idx_sync_event_app_txid_seq   rows=1     loops=1   the head
Index Scan using prophet_state_app_id_key                    rows=1     loops=1
Index Scan using idx_sync_event_app_txid_seq                 rows=334   loops=3   one descent per consumer
  Index Cond: (app_id = $1) AND (ROW(txid, seq) > ROW(c.last_applied_txid, c.last_applied_seq))
Buffers: shared hit=62
```

The three descents read 1,001, 2 and 0 entries, 1,003 together, and the whole read touched 62 buffers of a 200,000 row outbox. `TestGetSyncConsumersBoundsItsCountAndDescendsOncePerConsumer` in `prophet/echo/sync_db_test.go` pins the plan: every read of `sync_event` is a descent of `idx_sync_event_app_txid_seq`, one for the head and one per consumer.

### The consistency token

`SAuth-Seq` is the zookie of Zanzibar, a read "at least as fresh as" a known write ([Pang et al. 2019, section 2.2](https://www.usenix.org/system/files/atc19-pang.pdf)), and the `at_least_as_fresh` mode of SpiceDB ([SpiceDB, Consistency](https://authzed.com/docs/spicedb/concepts/consistency)).

**Sources.** [PostgreSQL, Transaction ID and Snapshot Information Functions](https://www.postgresql.org/docs/17/functions-info.html#FUNCTIONS-PG-SNAPSHOT): `pg_current_xact_id`, `pg_current_snapshot`, `pg_snapshot_xmin`, and the definition of `xmin` as the earliest transaction still active. [PostgreSQL, Sequence Manipulation Functions](https://www.postgresql.org/docs/17/functions-sequence.html), on `nextval` never being rolled back.
