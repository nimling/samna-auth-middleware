---
layout: doc
title: Sync
description: The outbox a prophet writes, the rows it carries, how a disciple applies them and settles its own access, the cursors, trimming, and the sequence as a consistency token.
tags: sync_event, get_sync_events, trim_sync_event, prophet_state, disciple_state, replay_mode, cursor, consistency
lastUpdated: true
---

# Sync

A prophet appends every change to a source row of an app to `sync_event`. A disciple of that app reads the events after its cursor, applies them to its own copy, and settles its own access from them. The protocol that moves the events is pull, and it belongs to the middleware; see [Middleware sync](./middleware/sync.md).

## What is carried

| Carried | Never carried |
|---|---|
| `samna_user`, `samna_client`, `samna_secret` as ciphertext | `user_object`, `object_info`, `user_users` |
| `organization`, `location` the app reaches through `app_object` | `inheritance_info`, `table_info`, `access_diff` |
| `claim`, `claim_object`, `user_claim` | `claim_graph_cache`, `reconcile_queue`, `audit` |
| `object_field`, `user_field`, `user_relation` | `samna_app`, and every key and seed |

The feed carries source rows, the inputs of access, and never the access computed from them. A disciple derives its closure, its `user_object`, `object_info` and `user_users` itself, with the same triggers and the same settle as the prophet.

### Why source rows only

The derived tables change once per user a change reaches; the source rows once per change. The difference is the fan out claimius exists to avoid, and a feed that carried derived rows would reintroduce it on the wire: every settle on the prophet would become rows for every disciple to apply. Measured on the benchmark prophet, after building the data set the outbox held 5,186,276 events, of which 9,355 were source rows; the rest, 5,176,921, were rows of `user_users`, `user_object` and `object_users`. A disciple also carries tables the prophet has never seen, and those it settles locally in every case, so the settle is on the disciple already. This is the division [Authzed Materialize](https://authzed.com/docs/authzed/concepts/authzed-materialize) makes when it ships permission set changes rather than per permission changes.

## The row

`sync_event (seq bigserial, operation, event_type, payload jsonb, app_id, txid, written_at)`. `operation` is `upsert` with the full row as `payload`, or `delete` with its id. `txid` is the writing transaction, `pg_current_xact_id()`. `emit_sync_event` writes the row in the transaction of the change, so an event exists exactly when its change committed. An event of an app carries the app's id; an organization or location event, whose row has no `app_id`, is written once per app it reaches through `app_object_ids`, so every event of the feed belongs to one app.

`get_sync_events(app_id, seq, limit)` is the delta feed for one app after a sequence, ordered by `seq`, served from `idx_sync_event_app_seq (app_id, seq)` as one range. Another app's rows never leave the prophet.

### Only what can no longer be overtaken

A sequence is handed out when the row is inserted and becomes visible when its transaction commits, and two transactions commit in either order. A feed that served sequence 12 while 11 was still in flight would move the cursor past 11, and 11 would never be served. The feed therefore serves only events whose transaction is older than every transaction still running, `txid < pg_snapshot_xmin(pg_current_snapshot())` ([PostgreSQL, Snapshot Information Functions](https://www.postgresql.org/docs/17/functions-info.html#FUNCTIONS-PG-SNAPSHOT)). An event waits at most for the longest transaction running on the prophet when it committed, milliseconds for claimius's one transaction writes, and `statement_timeout` bounds it.

### Why one row per reached app

A hierarchy event with no app would be read by every pull of every app and tested against `app_object_ids` row by row, so a pull costs every hierarchy change of every app after its cursor. Written per reached app it costs `a_e` rows at write, for the few apps an organization serves, and a pull reads its own range and nothing else. The inequality is `a_e < pulls between trims`, which holds for any app that pulls.

## Applying

1. A disciple applies a page of events in one transaction under `replay_mode`, upserting each by its primary key. Delivery is at least once and every apply is idempotent: the same event twice produces the same row.

2. `replay_mode` marks rows as arriving from the prophet. The checks that authorize an actor, composition rules and escalation guards, are skipped, because the prophet made them. The derived state is maintained as for any write: closure edges, and an area in `access_diff` for every applied claim, binding, grant and hierarchy row, settled by the disciple's next read or its drain.

3. `disciple_state.last_applied_seq` moves to the last applied sequence in the same transaction as the page. A failed page rolls back whole and is fetched again from the same cursor.

## Cursors and trimming

`prophet_state.last_applied_seq` is, per app and per consumer of it, the sequence that consumer last proved it applied, by asking for what comes after it. The consumers of an app are its disciple and every peer prophet mirroring it, each authenticated by its own app key, so one consumer's cursor never moves another's. `trim_sync_event(stale_after)` deletes every event at or below the lowest cursor among apps in contact within `stale_after`, 7 days by default, and returns the count. An app out of contact longer is treated as gone, so one dead disciple cannot pin the outbox forever. A disciple that comes back below the trimmed watermark starts again from a snapshot.

The outbox holds `Σ` source changes over the trim interval, a number proportional to writes and independent of users.

## The sequence is the consistency token

A routed write returns the prophet sequence of its change. A read on a disciple that must see that write waits until `disciple_state.last_applied_seq` reaches the sequence. This is the zookie of Zanzibar, "at least as fresh as" a known write ([Pang et al. 2019, section 2.2](https://www.usenix.org/system/files/atc19-pang.pdf)), and the `at_least_as_fresh` mode of SpiceDB ([SpiceDB, Consistency](https://authzed.com/docs/spicedb/concepts/consistency)). Without a token a disciple read is consistent as of its cursor. A prophet read is always current, because the settle runs before the read in the same database.

## A disciple's own writes

The implementing side's own tables never enter the feed. Bindings on them are stored on the prophet, addressed by `object_type` and `object_id`, travel opaque, and settle on the disciple that holds the table.

## Staleness

A change committed on the prophet is applied on a disciple after `τ = τ_wait + τ_rtt + τ_apply(n) + τ_commit`. `τ_wait` is zero, because a pull that finds nothing is held open on the prophet until the next event of its app commits; `emit_sync_event` issues a `NOTIFY`, which PostgreSQL delivers only at commit ([PostgreSQL, NOTIFY](https://www.postgresql.org/docs/17/sql-notify.html)). `τ_apply` is about 80 µs an event, `τ_commit` the guard above. One event across regions is applied in about one round trip. The cost and the ceiling of the feed are in [Scaling](./scaling.md).
