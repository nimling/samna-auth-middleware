---
layout: doc
title: Sync
description: The outbox a prophet writes, the rows it carries per write, how a pull reads it, how a disciple applies it, the cursors and trimming, and the sequence as a consistency token, in rows as functions of the data.
tags: sync_event, emit_sync_event, get_sync_events, trim_sync_event, prophet_state, disciple_state, replay_mode, cursor, consistency
lastUpdated: true
---

# Sync

A prophet appends every change of a mirrored table to `sync_event`. A disciple of an app reads the events of its app after its cursor and applies them to its own copy. The protocol that moves the events is pull, and it belongs to the middleware; see [Sync in Go](./middleware/sync.md).

## The row

`sync_event (seq bigserial, operation, event_type, payload jsonb, app_id, written_at)`, with four btree indexes: the primary key `seq`, `(app_id, seq)`, `(event_type, seq)` and `written_at`.

`emit_sync_event`, an after trigger on the source tables, writes one row per row written, in the transaction of the change, so an event exists exactly when its change committed. An `upsert` carries the full row; a `delete` carries its `id` and `app_id`. The trigger does nothing on a database with no `prophet_state` row, so a pure disciple writes no events.

## What is carried

| Carried | Never carried |
|---|---|
| `samna_user`, `samna_client`, `samna_secret` | `samna_app`: its events carry no `app_id` and no pull serves them |
| | `inheritance_info`, `table_info`, `access_diff`, `reconcile_queue` |
| `organization`, `location` | `claim_graph_cache` |
| `claim`, `claim_object`, `user_claim` | the implementing side's own tables |
| `object_field`, `user_field`, `user_relation` | `user_object`, `user_users`: derived, see below |
| | `audit` |

The feed carries the source rows of access and nothing derived from them. A disciple derives its own pairs: applying a source row keeps its closure and raises the areas the row moves, and the disciple's reads settle them as the prophet's do; see [Materialization](./materialization.md). An `app_object` link publishes the linked organization or location to the linked app, stamped with its id, and an unlink publishes the row's delete to that app alone, so a row linked after it was written reaches the disciple and an unlinked one leaves it. `get_sync_events` and the snapshot serve no `user_object`, `user_users` or `audit` row, and the base files delete any left in the outbox.

### Events per write

A write emits one event per source row it writes, and a settle emits none:

```
events = source rows
```

For the request of [Writes](./writes.md#one-request-one-transaction), a claim bound to a node and granted to `k` users, `events = 2 + k`, whatever the claim reaches. Measured on a prophet with one root, 200 organizations under it and 50 users, granting a claim bound to the root to the 50 users and letting each list its organizations: the feed took 50 `user_claim` events for the grants and nothing for the 12,651 pairs and 2,450 `user_users` rows the reads settled, where publishing the derived rows took 12,651 `user_object` and 227,850 `user_users` events more. On the data set of [the index](./index.md#symbols), building the data held 5,186,276 events when the derived rows were published, of which 9,355 were source rows: the fan out of a grant to the pairs it reaches, and of a pair to the users who share it, is what the feed no longer carries.

## The feed as a pipeline

1. A prophet writes events in its transactions. Every event of an app carries the app's id; an organization or location event carries none, because the row has no `app_id`.

2. `get_sync_events(app_id, seq, limit)` returns the events after a sequence in `seq` order, 1,000 by default: the app's own, one range of `idx_sync_event_app_seq`, and every organization and location event after the cursor whose row the app reaches through `app_object_ids`. With `n_a` events of the app and `h` hierarchy events of every app after the cursor, a pull reads

```
entries = n_a + 2h
```

entries, one per event and one `app_object` probe per hierarchy event, and returns the `n_a` and the hierarchy events of its own app. Another app's rows never leave the prophet.

3. Sequences are handed out at insert and become visible at commit, so the feed serves the committed events above the cursor in allocation order.

4. A disciple applies a page in one transaction under `replay_mode`, each event by its primary key: the calc triggers settle no actor but keep the closure and raise the object areas a structural row moves. For a `claim`, `claim_object` or `user_claim` event the disciple raises the claim areas it moves in its own `access_diff`, one or two rows, so every pair it holds, on the shared types and on its own registered tables, settles on the next local read. Applying is idempotent: the same event twice produces the same row.

5. `disciple_state.last_applied_seq` moves to the page's last sequence first in the same transaction, and only from the cursor the page was pulled at. A page of `p` events costs one commit; a page that fails anywhere rolls back whole, cursor included, and is fetched again from the same cursor.

6. One process per disciple database applies. The pull loop runs on every pod, and each holds a session level `pg_try_advisory_lock` on a key of the app slug on a dedicated connection for as long as it pulls; a pod without the lock does no sync work and tries again every 5 s. The lock dies with its session, so when the holder goes another pod takes over, and a holder whose connection stops answering stops pulling. `N` pods apply `E` events once, `E` applies instead of `N · E`.

## Cursors and trimming

`prophet_state` holds, per app, `last_applied_seq`, the sequence the app's consumer last proved it applied by asking for what comes after it, and `last_contact_at`. `trim_sync_event(stale_after)` deletes every event at or below the lowest cursor among apps in contact within `stale_after`, 7 days by default, and returns the count. An app out of contact longer is treated as gone, so one dead disciple cannot hold the outbox. The outbox holds, per app, the events since the lowest live cursor: proportional to the source rows written.

## Catch up

A disciple away for a while returns `n_b` events behind, all of which the outbox still holds when it was in contact within `stale_after`. It replays them in pages, `⌈n_b / 1000⌉` pulls and `n_b` applies. A disciple further behind than the trimmed watermark cannot be replayed and starts again from a snapshot of its app's rows, which the middleware serves.

## The sequence is the consistency token

A routed write returns the prophet sequence of its change. A read on a disciple that must see that write waits until `disciple_state.last_applied_seq` reaches the sequence. This is the zookie of Zanzibar, "at least as fresh as" a known write ([Pang et al. 2019, section 2.2](https://www.usenix.org/system/files/atc19-pang.pdf)), and the `at_least_as_fresh` mode of SpiceDB ([SpiceDB, Consistency](https://authzed.com/docs/spicedb/concepts/consistency)). Without a token a disciple read is consistent as of its cursor. A prophet read settles what it meets before it answers, in the same database.

## A disciple's own writes

The implementing side's own tables never enter the feed. Bindings on them are stored on the prophet, addressed by `object_type` and `object_id`, travel as any binding does, and settle on the disciple that holds the table.
