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
| `object_info` | Primary key `(object_type, object_id)`; `(sa_owner_id, sa_name, object_id)`; `(sa_root_id)`; `(sa_location_id, sa_name, object_id)` partial; GIN on `search_vector` |
| `inheritance_info` | Primary key; the descendant and ancestor covering indexes |
| `access_diff` | Primary key `id`; `(app_id, object_type, object_id) WHERE kind = 'object'`; `(app_id, claim_id, user_id) WHERE kind = 'claim'` |
| `sync_event` | Primary key `seq`; `(app_id, seq)`; BRIN on `written_at` |

Every index on `user_object` is a write on every settled pair, so it carries two: the primary key, which is the check's probe, the listing's range and the mask test at once, and the members of an object. An index with `INCLUDE` columns never deduplicates ([PostgreSQL, Deduplication](https://www.postgresql.org/docs/17/btree.html#BTREE-DEDUPLICATION)), so a second covering index over the same user range would pay its full width again for nothing. The per object indexes, owner, root, location and text, sit on `object_info`, where they are written once per object instead of once per pair. The primary key orders a user's pairs together, so one user's slice of `n` pairs dirties `⌈n / 87⌉` leaf pages, where a hashed id scatters it over `n`, and after a checkpoint each of those pages is a full page image in the WAL ([WAL configuration, full_page_writes](https://www.postgresql.org/docs/17/runtime-config-wal.html)).

On the write path only indexes a read uses are kept. `sync_event` is read by `(app_id, seq)` and trimmed by `seq`, and its `written_at` follows insertion order, so it is BRIN. The single column `sa_deleted_at` indexes of `claim`, `user_claim` and `claim_object` serve no read, because every read of them filters on a live row inside another index, and are not kept. See [Scaling](./scaling.md) for the entries each write saves.

## Growth and memory

Bytes per unit of the designed rows, heap and indexes, from the widths above and the entry sizes of [Scaling](./scaling.md):

| Table | Grows with | Bytes per unit |
|---|---|---|
| `user_object` | pairs `P = Σ_u V_u = O Ū_o` | 350 |
| `object_info` | objects `O` | 300 |
| `inheritance_info` | edges `O (D̄ + 1)` | about 480: 100 heap, 120 primary key, 130 for each covering index |
| `user_users` | pairs of users sharing an object, `U × Ū_seen`, at most `U²` | 466, measured |
| `user_claim` | grants `G` | about 400, measured 443 with the `sa_deleted_at` index that goes |
| `samna_user` | users `U` | 456, measured |
| `sync_event` | per app `min(λ_e × 7 days, N_src)` | about 580 with its two btrees |
| `audit` | `λ_a × retention` | 330 |
| `access_diff` | outstanding areas, at most the source rows | about 100 |

A year's growth is the sum of these over what the year adds. The pairs dominate: a year adding 10,000 users who each reach 2,000 objects, 200,000 objects at depth 4, 50,000 grants and 100 visible users each is 7.0 GB of `user_object`, 0.48 GB of closure, 0.47 GB of `user_users`, 0.06 GB of `object_info` and 0.02 GB of grants.

What must stay in memory is what a read touches, and each index leaves memory at its own threshold. With `E` the cache of a machine, 4 GiB on the Azure servers and 183 GiB on the reference node:

| Index or table | Touched by | Leaves memory when | Azure, `E` = 4 GiB | Reference, 183 GiB |
|---|---|---|---|---|
| `user_object` primary key and heap | Every check and listing of an active user | `U_act × V̄ × 350 B > E` | 12 million pairs: 6,100 active users at `V̄ = 2,000` | 560 million pairs: 280,000 active users |
| `idx_inheritance_info_descendant` | Every check, `D + 1` entries of the object | `O (D̄ + 1) × 130 B > E` | 6.6 million objects at depth 4 | 300 million |
| `object_info` | Listings, search, graphs | `O × 300 B > E` | 14 million objects | 650 million |
| `user_users` | `get_users` and the settle's increments, never a check | `U × Ū_seen × 466 B > E` | 3,000 users all sharing one object | 20,000 |

The per user index leaves first, and only for the active users: a user who does not read has pairs on disk that no read touches, and the primary key keeps a user's pairs on `⌈V_u / 87⌉` contiguous leaves, so the first read of a returning user costs that many reads and the rest of the session is cached. Past the threshold a check costs two random reads, and the ceiling is `I / 2` of [Scaling](./scaling.md). `user_users` grows with the square of the users who share objects; it sits off the check path, so outgrowing memory slows `get_users` and the settle's increments, not access.

## Fill factor and heap only updates

`user_object` is at fill factor 90. Its usage counters are updated in place by every usage flush, no index covers them, and the free space of each page keeps those updates heap only ([PostgreSQL, Heap Only Tuples](https://www.postgresql.org/docs/17/storage-hot.html)). A flush writes a new version of every row it touches, and within the flush's own statement the old versions are still live, so the page must hold `u_p` new versions at once for the `u_p` rows of that page the flush touches:

```
1 − ff  ≥  u_p × w_heap / 8,192
```

The settle writes a user's pairs together, so the rows of one heap page are mostly one user's, and `u_p` is the uses of one user on neighbouring objects within one flush interval, one to three in practice. At `w_heap ≈ 250 B`, fill factor 90 leaves 819 bytes, room for three versions, and 80 would leave room for six. The measurements agree with the inequality: 4,000 of 4,000 updates heap only on a 144 byte row at 90, and 699 of 3,000 on a 606 byte row, where one version barely fits. A settle changes `sa_access`, which the primary key includes, so a settle update is never heap only; the free space is for the flush.

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

Claimius runs inside the pools the service already has: each replica has a pool of its own and one for the middleware, `SetMaxOpenConns`, `SetMaxIdleConns` and `SetConnMaxIdleTime` in `shared/main.go`, and every statement runs under `statement_timeout`, 120 s, paired with the driver's read timeout. Claimius adds no connection of its own:

1. A read and its settle are one statement on one pooled connection.

2. The usage flush borrows one connection for one statement every flush interval.

3. The drain borrows one connection per batch, on the prophet's scheduled run and between a disciple's pulls.

### The size of a pool

A connection is held for `T_conn = r_db + c_r / σ` by a check, 1.2 ms on the Azure servers and 0.24 ms on the reference node. By Little's law a pod serving `X_pod` requests a second holds `L = X_pod × T_conn` connections busy on average, and with independent arrivals the busy count is Poisson with mean `L`. A pool of

```
P = q_{0.999}(L)
```

connections makes one request in a thousand wait for one. An Azure pod of 2 vCPUs at its API ceiling of 1,830 requests a second holds `L = 2.2`, so `P = 9`; a reference pod of four cores at 10,700 a second holds `L = 2.5`, so `P = 9`. A pool of 10 covers both, per pool, where the stack's budget gives 25.

The database, for its part, is kept busy by `h n × T_conn / (c_r / σ)` connections, 12 on a `D2ds_v5` and 111 on the reference node. More than that across all pods cannot make it faster; they queue inside it instead of in the pools. A server shared by many services therefore sits behind a pooler in transaction mode whose pool is that number ([PgBouncer, features](https://www.pgbouncer.org/features.html)), which claimius allows because it holds no session state across transactions; its named prepared statements become the pooler's protocol level ones.

### The idle count

A pool that keeps fewer idle connections than it has busy ones closes a connection whenever the busy count falls and opens one whenever it rises again. Simulating the pool of `database/sql` under Poisson arrivals and exponential holds with mean busy count `L`, the connections opened per request are:

| `L` | 2 kept idle | 4 | 8 | 12 |
|---|---|---|---|---|
| 1 | 0.147 | 0.014 | under 0.0001 | under 0.0001 |
| 2 | 0.228 | 0.063 | 0.0009 | 0.0001 |
| 4 | 0.279 | 0.118 | 0.016 | 0.0007 |
| 8 | 0.304 | 0.154 | 0.048 | 0.012 |

An open costs a TCP and TLS handshake, a SCRAM exchange whose client side runs 4,096 PBKDF2 iterations by default ([PostgreSQL, `scram_iterations`](https://www.postgresql.org/docs/17/runtime-config-connection.html#GUC-SCRAM-ITERATIONS)), and a backend start: three round trips or more and, assumed, about a millisecond of database CPU, several times the 0.2 ms of the check it serves on an Azure server. At `L = 2` with two kept idle, 23 % of requests pay it. The idle count therefore equals the pool size, and `SetConnMaxIdleTime` of 15 s closes what an idle period leaves open: scaling down is the idle timeout's work, not the idle count's.

### The budget

The per replica budget of the stack sums both pools and every dedicated `LISTEN` connection, and the replica budgeting inequality of the stack sums that over `maxReplicas`. At 10 per pool a replica holds at most 20, plus its listeners. A prophet adds one dedicated `LISTEN` connection per process for the feed, which holds a pull open until the next event commits; see [Sync](./sync.md).
