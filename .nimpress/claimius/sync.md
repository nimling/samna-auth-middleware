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

`emit_sync_event`, an after trigger, writes one row per row written, in the transaction of the change, so an event exists exactly when its change committed. An `upsert` carries the full row; a `delete` carries its `id` and `app_id`. The trigger does nothing on a database with no `prophet_state` row, so a pure disciple writes no events.

## What is carried

| Carried | Never carried |
|---|---|
| `samna_user`, `samna_client`, `samna_secret` | `samna_app`: its events carry no `app_id` and no pull serves them |
| | `inheritance_info`, `table_info`, `access_diff`, `reconcile_queue` |
| `organization`, `location` | `claim_graph_cache` |
| `claim`, `claim_object`, `user_claim` | the implementing side's own tables |
| `object_field`, `user_field`, `user_relation` | |
| `user_object`, `user_users` | |

The feed carries the source rows of access and the pairs derived from them, so a disciple holds the prophet's pairs for the shared types without deriving them.

### Events per write

A write emits one event per source row it writes, one per pair it writes, and one per `user_users` row a pair entering or leaving moves, `2 (U_x − 1)` for a pair on `x`:

```
events = source rows + pairs written + Σ_{pairs entering or leaving} 2 (U_x − 1)
```

For the request of [Writes](./writes.md#one-request-one-transaction), a claim bound to a node and granted to `k` users:

```
events ≤ (2 + k) + 2 (1 + (1 + k) |T_c|) + Σ_{pairs entering or leaving} 2 (U_x − 1)
```

A settle on the prophet emits the same terms for every pair and every user it touches. On the data set of [the index](./index.md#symbols), building the data held 5,186,276 events, of which 9,355 were source rows and 5,176,921 rows of `user_object`, `user_users` and the `object_users` table the schema then carried: the fan out of a grant to the pairs it reaches, and of a pair to the users who share it, is what the feed carries.

## The feed as a pipeline

1. A prophet writes events in its transactions. Every event of an app carries the app's id; an organization or location event carries none, because the row has no `app_id`.

2. `get_sync_events(app_id, seq, limit)` returns the events after a sequence in `seq` order, 1,000 by default: the app's own, one range of `idx_sync_event_app_seq`, and every organization and location event after the cursor whose row the app reaches through `app_object_ids`. With `n_a` events of the app and `h` hierarchy events of every app after the cursor, a pull reads

```
entries = n_a + 2h
```

entries, one per event and one `app_object` probe per hierarchy event, and returns the `n_a` and the hierarchy events of its own app. Another app's rows never leave the prophet.

3. Sequences are handed out at insert and become visible at commit, so the feed serves the committed events above the cursor in allocation order.

4. A disciple applies each event by its primary key under `replay_mode`: the calc triggers return at once, so the prophet's pairs are taken as sent and nothing is derived again. For a `claim`, `claim_object` or `user_claim` event the disciple raises the claim areas it moves in its own `access_diff`, one or two rows, so the disciple's own registered tables, which the prophet never sees, settle on the next local read. Applying is idempotent: the same event twice produces the same row.

5. `disciple_state.last_applied_seq` moves to the last applied sequence. A failed apply is fetched again from the same cursor.

On a disciple the replay skips the closure too, so the closure of a mirrored root is built by `build_ownership_tree` the first time a local row attaches under an organization missing from it; see [Objects](./objects.md#what-each-write-does-to-it).

## Cursors and trimming

`prophet_state` holds, per app, `last_applied_seq`, the sequence the app's consumer last proved it applied by asking for what comes after it, and `last_contact_at`. `trim_sync_event(stale_after)` deletes every event at or below the lowest cursor among apps in contact within `stale_after`, 7 days by default, and returns the count. An app out of contact longer is treated as gone, so one dead disciple cannot hold the outbox. The outbox holds, per app, the events since the lowest live cursor: proportional to writes, and to the pairs and user pairs those writes touched.

## Catch up

A disciple away for a while returns `n_b` events behind, all of which the outbox still holds when it was in contact within `stale_after`. It replays them in pages, `⌈n_b / 1000⌉` pulls and `n_b` applies. A disciple further behind than the trimmed watermark cannot be replayed and starts again from a snapshot of its app's rows, which the middleware serves.

## The sequence is the consistency token

A routed write returns the prophet sequence of its change. A read on a disciple that must see that write waits until `disciple_state.last_applied_seq` reaches the sequence. This is the zookie of Zanzibar, "at least as fresh as" a known write ([Pang et al. 2019, section 2.2](https://www.usenix.org/system/files/atc19-pang.pdf)), and the `at_least_as_fresh` mode of SpiceDB ([SpiceDB, Consistency](https://authzed.com/docs/spicedb/concepts/consistency)). Without a token a disciple read is consistent as of its cursor. A prophet read settles what it meets before it answers, in the same database.

## A disciple's own writes

The implementing side's own tables never enter the feed. Bindings on them are stored on the prophet, addressed by `object_type` and `object_id`, travel as any binding does, and settle on the disciple that holds the table.
