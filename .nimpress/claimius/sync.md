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

3. `disciple_state.last_applied_seq` moves to the last applied sequence in the same transaction as the page. A failed page rolls back whole and is fetched again from the same cursor. The page commits with `synchronous_commit = off` ([PostgreSQL, Asynchronous Commit](https://www.postgresql.org/docs/17/wal-async-commit.html)): a crash can lose the last pages applied, but it loses the cursor that recorded them in the same transaction, so they are fetched again and nothing is lost that the feed does not still hold. The apply therefore never waits for a WAL flush, and `L_c` leaves the staleness and the page cycle below.

## Cursors and trimming

`prophet_state.last_applied_seq` is, per app and per consumer of it, the sequence that consumer last proved it applied, by asking for what comes after it. The consumers of an app are its disciple and every peer prophet mirroring it, each authenticated by its own app key, so one consumer's cursor never moves another's. `trim_sync_event(stale_after)` deletes every event at or below the lowest cursor among apps in contact within `stale_after`, 7 days by default, and returns the count. An app out of contact longer is treated as gone, so one dead disciple cannot pin the outbox forever. A disciple that comes back below the trimmed watermark, or further behind than a snapshot costs, starts again from a snapshot; see [Catch up](#catch-up).

The outbox holds, per app, the smaller of its source changes over the trim interval and its source rows: proportional to writes, bounded by the data, and independent of users.

## The sequence is the consistency token

A routed write returns the prophet sequence of its change. A read on a disciple that must see that write waits until `disciple_state.last_applied_seq` reaches the sequence. This is the zookie of Zanzibar, "at least as fresh as" a known write ([Pang et al. 2019, section 2.2](https://www.usenix.org/system/files/atc19-pang.pdf)), and the `at_least_as_fresh` mode of SpiceDB ([SpiceDB, Consistency](https://authzed.com/docs/spicedb/concepts/consistency)). Without a token a disciple read is consistent as of its cursor. A prophet read is always current, because the settle runs before the read in the same database.

## A disciple's own writes

The implementing side's own tables never enter the feed. Bindings on them are stored on the prophet, addressed by `object_type` and `object_id`, travel opaque, and settle on the disciple that holds the table.

## The feed as a pipeline

The feed is a pipeline from one prophet to the consumers of each app. The constants and machines are those of [Scaling](./scaling.md): `s_e` the prophet's cost to serve one event, `a_e` a disciple's cost to apply one, `σ` the speed of one thread, `r_net` the round trip between the two.

### Throughput

An app whose writes arrive at `λ_w` puts `λ_e = λ_w (3 + k)` events a second on the feed for the API first write. The prophet serves each to `1 + P_a` consumers, its disciple and every peer mirroring the app, at `s_e` each, plus a fixed `c_p` per pull, estimated at 0.3 ms for the route, the app key check and one statement. Its ceiling is `K / s_e` deliveries a second, 137,500 on a `D2ds_v5`; at the same server's write ceiling of 401 writes a second, six events each and three consumers, the feed takes `401 × 6 × 3 × s_e`, 5 % of it. The feed never binds before the writes it carries do.

A disciple applies one page at a time, in one transaction on one backend, so its rate is set by one thread, not by its cores. A page of `n` events takes a cycle

```
τ_c = c_0 + n a_e / σ          c_0 = r_net + c_p / σ
```

With events arriving at `λ_e`, each page carries what arrived during the previous cycle, `n = λ_e τ_c`, so

```
τ_c = c_0 / (1 − u)            u = λ_e a_e / σ
```

The pipeline is stable while `λ_e < σ / a_e`, 6,900 events a second on a `D2ds_v5` and 10,000 on the reference node: above any rate at which people change grants, and above the write ceiling of the prophet that feeds it.

### Staleness

An event committed on the prophet is applied on a connected disciple after

```
τ_0 = τ_x + r_net / 2 + a_e / σ        when the disciple is waiting
τ ≤ τ_0 + τ_c                                 when a page is in flight
```

`τ_x` is the wait for older transactions below; `r_net / 2` because the pull is already parked on the prophet, which answers the moment the event commits, so only the answer crosses the network. On a `D2ds_v5` that is 0.65 ms in region and 11 ms from Norway East to West Europe; on the reference node in region, 0.6 ms. Under load the event waits at most one more cycle, `c_0 / (1 − u)`: 3.1 ms in region at `u = 0.5`. Staleness is therefore half a round trip, and grows only as `1 / (1 − u)` as the disciple nears its apply ceiling.

### Catch up

A disciple away for `T` returns `n_b = λ_e T` events behind. Replaying them at the page rate `μ = L / (c_0 + L a_e / σ)` while new events keep arriving takes

```
t_cu = λ_e T / (μ − λ_e)
```

One API write a second, six events, and an hour away is 21,600 events: 3.2 s in region on a `D2ds_v5`, 3.6 s across regions.

A snapshot sends the app's `N_src` source rows instead, whatever the absence. The disciple upserts each with `ON CONFLICT ... DO UPDATE ... WHERE ... IS DISTINCT FROM`, so a row that did not change is one probe and no write, fires no trigger and raises no area; the settle after a snapshot is the settle of the rows that changed, the same as after the replay, and fewer, because a row changed many times is applied once. Replay is the cheaper path while

```
n_b a_e  <  N_src a_s          a_s ≤ a_e
```

that is, conservatively, while the backlog is smaller than the app's source rows. An app of 10,000 users, 1,000 claims, 30,000 grants, 1,000 bindings and 2,000 organizations and locations holds 44,000 source rows; at six events a second the crossover is an absence of two hours. A day away is 518,400 events, 77 s to replay and under 6.5 s as a snapshot.

So the prophet answers a pull whose backlog for the app exceeds the app's source rows with the snapshot rather than a page. It finds out by reading at most `N_src + 1` entries of `idx_sync_event_app_seq` after the cursor, `N_src / 204` leaves. And since no consumer is ever served more than `N_src` events of an app, `trim_sync_event` keeps at most that many of each app, beside the 7 day limit on contact: the outbox is bounded by the source rows of the apps, not by time.

### Page size

The page rate approaches `σ / a_e` as the page grows, and reaches the share `1 − ε` of it when

```
L ≥ (1 − ε) c_0 σ / (ε a_e)
```

A page of 1,000 events reaches 99 % in region and 87 % from Norway East to West Europe on a `D2ds_v5`. A larger page adds `L a_e / σ` to the staleness of its first event, 145 ms at 1,000, and only under a backlog. `get_sync_events` is called with 1,000.
