---
layout: doc
title: Storage
description: Row widths and sizes of the claimius tables, fill factors, the indexes each table carries, vacuum settings for the tables that churn, and how claimius fits the connection budget.
tags: storage, fillfactor, autovacuum, index, user_object, inheritance_info, access_diff, sync_event, connection pool
lastUpdated: true
---

# Storage

## Sizes

Measured on the benchmark data set: 782 organizations, 1,000 users, 5,012 grants, 20,000 registered rows, with the pair carrying its object fields in its own row. The last column is the designed row.

| Table | Rows | Bytes per row, measured | Bytes per row, designed | Grows with |
|---|---|---|---|---|
| `user_object` | 393,908 | 606, and 144 more in the pair's inverse row | about 250 with the usage counters, 350 with its two indexes | Users times reach, `Σ_u V_u` |
| `object_info` | one per object, 20,782 | | about 300 with its GIN entry | Objects, `O` |
| `inheritance_info` | 142,743 | about 130 with its two covering indexes | the same | Rows times depth, `N(D̄ + 1)` |
| `user_users` | 111,472 | about 120 | the same | Pairs of users sharing an object |
| `access_diff` | 0 to thousands | about 100 | the same, and one marker per reader of an outstanding area | Outstanding areas, drained within seconds |
| `sync_event` | Source changes over the trim interval | the row as jsonb, about 500 | the same | Writes |

The measured `user_object` row was wide because of what every user of an object shares: `search_vector`, 119 bytes, the name, description and link, and the owner, location and root. Those are stored once per object in `object_info`, which is `Ū_o = 19` times fewer rows in the benchmark. What stays on the pair is what differs between users: the mask, the grants that produced it, the scope and the usage. `grants`, 239 bytes on average, is read on every check, which is why it is stored rather than recomputed.

## Indexes

| Table | Indexes |
|---|---|
| `user_object` | Primary key `(user_id, app_id, object_type, object_id) INCLUDE (sa_access)`; `idx_user_object_object (object_id, object_type)` |
| `object_info` | Primary key `(object_type, object_id)`; `(sa_owner_id)`; `(sa_root_id)`; `(sa_location_id)` partial; GIN on `search_vector` |
| `inheritance_info` | Primary key; the descendant and ancestor covering indexes |
| `access_diff` | Primary key `id`; `(app_id, object_type, object_id) WHERE kind = 'object'`; `(app_id, claim_id, user_id) WHERE kind = 'claim'` |
| `sync_event` | Primary key `seq`; `(app_id, seq)`; BRIN on `written_at` |

Every index on `user_object` is a write on every settled pair, so it carries two: the primary key, which is the check's probe, the listing's range and the mask test at once, and the members of an object. An index with `INCLUDE` columns never deduplicates ([PostgreSQL, Deduplication](https://www.postgresql.org/docs/17/btree.html#BTREE-DEDUPLICATION)), so a second covering index over the same user range would pay its full width again for nothing. The per object indexes, owner, root, location and text, sit on `object_info`, where they are written once per object instead of once per pair. The primary key orders a user's pairs together, so one user's slice of `n` pairs dirties `⌈n / 87⌉` leaf pages, where a hashed id scatters it over `n`, and after a checkpoint each of those pages is a full page image in the WAL ([WAL configuration, full_page_writes](https://www.postgresql.org/docs/17/runtime-config-wal.html)).

On the write path only indexes a read uses are kept. `sync_event` is read by `(app_id, seq)` and trimmed by `seq`, and its `written_at` follows insertion order, so it is BRIN. The single column `sa_deleted_at` indexes of `claim`, `user_claim` and `claim_object` serve no read, because every read of them filters on a live row inside another index, and are not kept. See [Scaling](./scaling.md) for the entries each write saves.

## Fill factor and heap only updates

`user_object` is at fill factor 90. Its usage counters are updated in place by every usage flush, no index covers them, and the free tenth of each page keeps those updates heap only: measured 4,000 of 4,000 on the table that carried them before ([PostgreSQL, Heap Only Tuples](https://www.postgresql.org/docs/17/storage-hot.html)). A settle changes `sa_access`, which the primary key includes, so a settle update is never heap only; the free space is for the flush.

## Vacuum

| Table | Churn | Settings |
|---|---|---|
| `access_diff` | Every row is inserted and deleted within seconds | `autovacuum_vacuum_scale_factor = 0`, `autovacuum_vacuum_threshold = 1000` |
| `sync_event` | Deleted in bulk by `trim_sync_event` | Vacuumed after each trim |
| `user_object` | Upserts on settle, heap only usage updates pruned on the page | `autovacuum_vacuum_scale_factor = 0.05` |

A table whose rows live seconds is small only if it is vacuumed on a count, not on a fraction of its size: a scale factor of zero with a fixed threshold does that ([PostgreSQL, Automatic Vacuuming](https://www.postgresql.org/docs/17/runtime-config-autovacuum.html)). A large table vacuumed at the default 20 % carries a fifth of its size in dead rows before the vacuum starts; at 5 % the settle's dead versions are reclaimed before they cost reads, and the pages a vacuum marks all visible are the pages an index only listing reads without the heap.

## Time ordered data

Claimius holds three kinds of rows that arrive in time order and never change after.

| Table | Read by | Leaves by | Stored as |
|---|---|---|---|
| `audit` | Time window, object, owner | Age, months | Range partitions by month on `sa_created_at`, BRIN on `sa_created_at`, created and detached by `pg_partman` |
| `sync_event` | `(app_id, seq)` after a cursor | The lowest live cursor, hours to days | One table, btree on `(app_id, seq)`, trimmed by `trim_sync_event` and vacuumed after |
| The raw analytics events of the prophet | Time window per app and path | Age, 30 days | The same rule as `audit`, partitioned by day |

1. A table that leaves by age in whole periods is partitioned by that period, so retention is a detach and the planner prunes every period outside a window. Measured on 2,000,000 audit rows over 24 months: a month of 86,111 rows leaves in 0.6 ms, and the last 7 days are read in 3.1 ms with 24 of 25 partitions pruned. See [Audit](./audit.md).

2. A time column that follows insertion order is indexed with BRIN: 32 kB for 2,000,000 rows, where a btree on the same column is 43 MB and costs a btree insert per row ([PostgreSQL, BRIN Indexes](https://www.postgresql.org/docs/17/brin.html)).

3. `sync_event` leaves by cursor, not by period, and after the feed carries source rows only it holds writes over a trim interval, not users times writes. A plain table with a delete per trim is the right size for it.

4. Usage is not time ordered data. It is two counters on a pair, updated in place; see [Usage](./usage.md).

All of this is core Postgres plus `pg_partman`, and runs on Azure Database for PostgreSQL Flexible Server, where `pg_partman` 5.4.3 and `pg_cron` 1.6 are on the allow list for Postgres 17 ([Microsoft Learn, Extensions by engine](https://learn.microsoft.com/en-us/azure/postgresql/extensions/concepts-extensions-by-engine)). `pg_cron` runs the scheduled maintenance of a prophet: the partition upkeep, `trim_sync_event` and the drain of [Materialization](./materialization.md).

## Connections

Claimius runs inside the pool the service already has. The budget of the Samna stack gives each replica 25 connections of its own and 25 for the middleware's pool, idle connections closed after 15 seconds, lifetimes of two minutes, and a server side `statement_timeout` of 120 s paired with the driver's read timeout. Claimius adds no connection of its own:

1. A read and its settle are one statement on one pooled connection.

2. The usage flush borrows one connection for one statement every flush interval.

3. The drain borrows one connection per batch, on the prophet's scheduled run and between a disciple's pulls.

The per replica budget of 50, plus any dedicated `LISTEN` connection a service opens, is what the replica budgeting inequality of the stack sums over its `maxReplicas`.

Claimius holds no session state across transactions: every read and write is one transaction, prepared statements aside. A server shared by many services can therefore sit behind a pooler in transaction mode, where the server connections number the pooler's pool, not `Σ maxReplicas × 50` ([PgBouncer, features](https://www.pgbouncer.org/features.html)); the prepared statements are then the pooler's protocol level ones.

A prophet adds one dedicated `LISTEN` connection per process for the feed, which holds a pull open until the next event commits; see [Sync](./sync.md). It counts in the per replica budget like any other listener.
