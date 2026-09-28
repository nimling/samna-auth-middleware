---
layout: doc
title: Scaling
description: The cost of every claimius operation in B-tree descents, page reads, tuples, WAL, locks and CPU time from the PostgreSQL planner constants, the transactions an API request spends, the queueing model of the API and database tiers, where the bottleneck sits for a mix of reads and writes, the ceiling of a network of prophets and disciples, and the shapes that lower each dominant term.
tags: scaling, cost model, btree, fanout, planner cost, queueing, erlang c, bottleneck, throughput, latency, wal, locks, transactions, disciple, prophet, staleness, availability
lastUpdated: true
---

# Scaling

Every number in this file comes from a formula, and every constant in a formula is either a PostgreSQL default, a published figure, or a measurement of the claimius benchmark described in [the index](./index.md), and says which. The benchmark ran on an Apple Silicon laptop under Docker with the data on tmpfs, so its times are CPU times with no disk in them; where a disk matters the Azure figures are used instead.

## The verdict

Per request, the API tier spends more CPU than claimius spends in the database, on the designed shape, for every read and for any mix whose writes settle fewer than about ten pairs per request. The governing inequality, with `c_a` the API CPU of a request, `c_r` the database CPU of a claimius read, `c_w` of a claimius write, `c_aw` the API CPU of a write, `w` the write fraction and `S` the settle work a write causes:

```
(1 − w)(c_a − c_r)  >  w (c_w − c_aw + S)          with S = U_a × A × t
```

With the constants below, `c_a − c_r ≈ 0.19 ms` and `t = 19 µs`, it reduces to

```
w × U_a × A  <  (c_a − c_r) / t  ≈ 10 settled pairs per request     (≈ 32 at t = 6 µs, the narrow pair row of "What lowers each term")
```

Three places where the thesis fails, each proved below:

1. **One service at scale.** The API tier scales out and a service's database does not. The database of one service saturates first once the service runs more than `k × c_a / c_r` API cores per `k` database cores: 5.5 API cores per 2 vCores on the designed shape. The fix is structural, not a faster query: reads that find nothing to settle run on a hot standby, which multiplies the database ceiling by `1 + R` for `R` standbys, and every disciple is its own database.

2. **A per user index larger than memory.** Once `Σ_u V_u × w_pair` exceeds the cache, a check costs two random reads, and the IOPS cap of the SKU, not any CPU, is the ceiling: 1,875 checks a second on `Standard_D2ds_v5`, 960 on `Standard_B2ms`.

3. **Writes, per request.** A write costs the prophet's database about 1.2 ms of CPU against about 0.7 ms of API CPU, so a write alone is database bound. The prophet's ceiling is about 1,170 composite writes a second on 2 vCores at 70 % utilization, which is above any rate at which people change grants, so writes never bound the network; they bound one prophet.

## Constants

| Symbol | Value | Source |
|---|---|---|
| `seq_page_cost`, `random_page_cost` | 1.0, 4.0 | PostgreSQL defaults ([Planner Cost Constants](https://www.postgresql.org/docs/17/runtime-config-query.html#RUNTIME-CONFIG-QUERY-CONSTANTS)) |
| `cpu_tuple_cost`, `cpu_index_tuple_cost`, `cpu_operator_cost` | 0.01, 0.005, 0.0025 | PostgreSQL defaults, same page |
| `r`, time of one cost unit on one core | 6 µs | Calibrated: the listing of 20,000 rows costs 0.114 units a row by the formula below and measured 14.4 ms, so 0.72 µs a row. It makes `cpu_operator_cost` 15 ns, `cpu_tuple_cost` 60 ns |
| `t_d`, one B-tree descent, cached | 2.5 µs | The planner's descent charge, `(⌈log₂ N⌉ + 50(h + 1)) × cpu_operator_cost` ([`btcostestimate`](https://github.com/postgres/postgres/blob/REL_17_STABLE/src/backend/utils/adt/selfuncs.c)), is 0.42 units at `N = 393,908`, `h = 2` |
| `t_s`, one client statement: protocol, parse, plan, executor | 60 µs | Measured: the `user_object` primary key probe as one statement took 0.066 ms of which the descent and two tuples are 2.7 µs |
| `t_i`, one statement inside PL/pgSQL with a cached plan | 20 µs | Calibrated: a check measured 0.30 to 0.35 ms on the server, and the model below puts eight inner statements in it |
| `t`, one settled pair, set based | 19 µs | Measured: 27,358 pairs in 522 ms |
| `t_io`, one random 8 kB read on Azure Premium SSD | 1 ms | Assumed; the SKU IOPS cap below binds before latency does |
| IOPS cap | 1,920 on `Standard_B2ms`, 3,750 on `Standard_D2ds_v5` | [Microsoft Learn, Compute options](https://learn.microsoft.com/en-us/azure/postgresql/compute-storage/concepts-compute) |
| RS256 verify, 2048 bit, Go | 8 to 121 µs | [golang/go issue 63516](https://github.com/golang/go/issues/63516): 8 µs on Go 1.19, 121 µs on Go 1.21 after the constant time rewrite |
| bcrypt compare, cost 10, Go | 72 to 86 ms | [bcrypt-cost-go](https://github.com/nsmithuk/bcrypt-cost-go), 86 ms on a DigitalOcean high CPU VM |
| `c_a`, API CPU of a read request | 0.15 to 0.5 ms, 0.3 central | Estimate: echo routing and headers 20 µs, RS256 verify, claims decode 5 µs, one database round trip in `lib/pq` and `bun` 20 µs, the service's own query 30 µs, a JSON response of 10 kB 100 µs, telemetry 20 µs |

The API figures are estimates, not measurements; every conclusion below is stated as an inequality and holds across the stated range unless it says otherwise.

## The shape of the indexes

A B-tree page is 8,192 bytes with a 24 byte header and a 16 byte special area; an index tuple is an 8 byte header plus its `MAXALIGN`ed key and include columns, plus a 4 byte line pointer ([Database Page Layout](https://www.postgresql.org/docs/17/storage-page-layout.html)). Leaves are built at fill factor 90, and over 99 % of pages are leaves ([B-Tree Structure](https://www.postgresql.org/docs/17/btree.html#BTREE-STRUCTURE)). Fanout `B = 8,152 × 0.9 / entry` and height `h = 1 + ⌈log_{B_int}(N / B)⌉`; the levels above the leaves are `1/B` of the index and stay cached, so an uncached descent misses one leaf.

| Index | Entry | Leaf fanout `B` | `h` at 400 k | `h` at 20 M |
|---|---|---|---|---|
| `user_object` primary key `(user_id, app_id, object_type, object_id) INCLUDE (sa_access)` | 84 B | 87 | 3 | 4 |
| `idx_user_object_object (object_id, object_type)`, deduplicated | measured 807 per page | 807 | 2 | 3 |
| `idx_inheritance_info_descendant`, covering | 116 B | 63 | 3 | 4 |
| `idx_user_claim_user_app (user_id, app_id)` | 44 B | 167, more with deduplication | 2 | 3 |
| `idx_sync_event_app_seq (app_id, seq)` | 36 B | 204 | 2 | 3 |
| `access_diff` primary key | 28 B | 262 | 1 | 1 |

An index with `INCLUDE` columns never deduplicates ([Deduplication](https://www.postgresql.org/docs/17/btree.html#BTREE-DEDUPLICATION)), so a covering index on a per user range pays its full entry width; that is why the `user_object` range and the probe share one index, the primary key, rather than two.

## Cost per operation

Variables: users `U`, objects `O`, tree depth `D`, grants per user `C`, bindings per claim `K`, fan out `F`, objects a user reaches `V_u`, pairs in an area `A`, holders of a claim `U_a`, grants in one `user_object` row `G`, matches of a search query `M_q`. Times are CPU on one core, cached, from `r`, `t_d`, `t_s`, `t_i`; "misses" are the random reads when nothing but the upper B-tree levels is cached.

| Operation | Statements | Descents | Heap rows | Tuples | Rows written | WAL | Locks | CPU cached | Misses |
|---|---|---|---|---|---|---|---|---|---|
| Check, `mutation_access`, nothing pending | 1 | `2C + D + 5` | `2C + 2` | `3C + 2D + 6` | 0 | 0 | none | `t_s + (2C + D + 5) t_d` = 0.11 ms at `C = 5, D = 4` | 2 |
| Listing, `get_objects` | 1 | `2C + 5 + V_u`, the last `V_u` into the small `object_info` | `V_u` from `object_info` | `V_u` | 0 | 0 | none | `V_u × 0.114 r + t_s`: 0.7 µs a row | `⌈V_u / 87⌉` |
| Listing ids, `ObjectWhere` | 1 | `2C + 5` | `V_u × f_nv` | `V_u` | 0 | 0 | none | `V_u × 0.03 r`: 0.2 µs a row | `⌈V_u / 87⌉` |
| Search | 1 | `2C + 5` | `min(V_u, M_q)` | `min(V_u, M_q)` | 0 | 0 | none | `min(V_u × 1 µs, M_q × h t_d)` | `min(⌈V_u / 87⌉, M_q)` |
| Owner graph window, depth `d`, width `W` | 1 | `n_w + 1` | `n_w` | `n_w` | 0 | 0 | none | `n_w (t_d + 0.015 r)`, `n_w = Σ_{i≤d} min(F, W)^i` | `n_w` |
| Claim graph, default shape | 1 | 1 | 1 | 1 | 0 | 0 | none | one row of `claim_graph_cache` | 1 |
| Organization or location insert | 1 | `(D + 1)` reads, `≈ 5(D + 1) + 20` inserts | `D + 7` written | `D + 1` | `D + 7` | `≈ 250(D + 7) + 60 × index entries` B | row locks on new rows; `FOR KEY SHARE` on parent and app | `≈ 0.6 ms` | constant |
| Grant, `assign_claim_user` | 1 | 16 inserts | 4 written | 0 | 4 | ≈ 2 kB | new rows; `FOR KEY SHARE` on claim, app, organization | ≈ 0.3 ms | constant |
| Revoke, `remove_user_claim` | 1 | 16 | 4 | 1 | 4 | ≈ 2 kB | the `user_claim` row | ≈ 0.3 ms | constant |
| Binding, `assign_claim_object` | 1 | one check + 16 inserts | 4 | 0 | 4 | ≈ 2 kB | the claim's area row until commit | ≈ 0.4 ms | constant |
| First read of a reader under a raised area | 1 | as the read | as the read | `A_u` | `A_u + 1` | `≈ 360 A_u` B | the reader's marker, the reader's pairs | read `+ A_u × t` | `⌈A_u / 87⌉` |
| Sync page of `n` events on a disciple | 1 transaction | `n (1 + i_e)` | `n` | `n` | `n + Σ(D + 1) + areas` | `≈ n (payload + 60 i_e)` | the page's rows | `≈ n × 80 µs` | `≈ n` |
| Audit row | inside the write | 3 | 1 | 0 | 1 | ≈ 400 B | none | 3.7 µs measured | 0 |

`f_nv` is the fraction of the reader's range on pages not yet marked all visible; an index only scan fetches the heap only for those ([Index Only Scans](https://www.postgresql.org/docs/17/indexes-index-only-scans.html)). `i_e` is the number of indexes on the event's table. `A_u` is the reader's own pairs in the area: the objects under the bound node for a claim area, `A = Σ_{i=0}^{D−D_n} F^i` for a node at depth `D_n` of a tree of fan out `F`, plus the registered rows under it.

### Where the listing formula comes from

Per row: one index tuple 0.005, one heap tuple 0.01, the mask test 0.005, `G = 1.5` grants each resolved against the reader's grants hashed once, `1.5 × 0.015`, and the sort, `2 × cpu_operator_cost × log₂ V_u` comparisons a row, 0.0715 at `V_u = 20,000`. Total 0.114 units, 0.69 µs at `r`, measured 0.72 µs. The sort is the largest term above a few thousand rows; a caller that pages by keyset on the primary key order reads rows already ordered and pays none of it.

### Where the check formula comes from

The reader's grants, one range on `idx_user_claim_user_app` and `C` claim probes; the ancestors of the object, one covering range on `idx_inheritance_info_descendant` returning `D + 1` rows; the outstanding areas, one `= ANY` probe of the `access_diff` primary key with the `C + D + 1` area ids the reader computes with `composite_id`, a one level index; `samna_user`; the `user_object` pair. At `C = 5`, `D = 4` that is 19 descents, 48 µs, and one client statement, 60 µs: 0.11 ms. The same check written as a PL/pgSQL function of eight statements adds `8 t_i = 160 µs`, 0.27 ms, which is what the benchmark measured. The check is therefore one SQL statement; see [Reads](./reads.md).

## Transactions per API request

| Request | Transactions | Round trips | What it locks, and for how long | Isolation |
|---|---|---|---|---|
| Read one object, nothing pending | 1, autocommit | 1, or 0 extra when the check is folded into the service's own query | Nothing but relation `AccessShareLock` | Read committed |
| Listing | 1 | 1: `ObjectWhere` is a predicate in the service's query | Nothing | Read committed |
| Read that finds a raised area | 1 | 1 | The reader's marker row and its own `A_u` pairs, for `A_u × t` | Read committed; the marker records the area's `sa_updated_at` read at the start, so a write that raises the area again during the settle is settled by the next read |
| Create a claim, bind it, grant it to `k` users | 1 on the prophet | 1 with the statements sent as one batch, `3 + k` without | New rows only; `FOR KEY SHARE` on the owning organization, the app and the claim; the claim's area row from the binding to commit | Read committed |
| Routed write from a disciple | 1 on the prophet, then 1 local upsert on the disciple | 2 HTTP hops, 2 database round trips | As above, on each side | Read committed |
| Sync page | 1 on the disciple | 1 HTTP pull, 1 commit | The page's rows until commit | Read committed |

The rules that fall out of the formulas:

1. **One statement per question.** A statement costs `t_s = 60 µs` from a client and `t_i = 20 µs` inside a function, while a descent costs 2.5 µs. The dominant term of a check is the number of statements, so every read is one statement and every write request one transaction.

2. **Areas are raised last.** The only row two writers of different objects can both lock is an area row, and a writer holds it from the raise to its commit. With writers on one claim arriving at `λ_c` and holding for `h`, they queue when `ρ = λ_c h ≥ 1`. The raise is the last statement of a write, so `h` is one commit, 1 to 3 ms, and `λ_c < 300` writes a second on one claim before any wait.

3. **Nothing on a read path waits on a write.** A read with nothing pending takes no row lock. A settling read locks only its own marker and its own pairs, so two readers of one area settle their slices in parallel, and two reads by the same user of the same area wait on the marker's unique key, the first settles, the second finds the marker and reads.

4. **Deferred, never dropped.** Every write's effect on other users is an area row in the write's own transaction, committed or rolled back with it, and settled by the reader or the drain. Nothing is queued outside the database.

## The API tier

The request path in `smw`: a bearer token is one RS256 verify against the cached JWKS; a DPoP bound token adds an ES256 verify and one insert into `dpop_jti`, a second round trip; a session cookie is the same verify; client credentials are one bcrypt compare. A request then spends one round trip per claimius call and one per service query.

| Credential | API CPU | Requests a second per core |
|---|---|---|
| Bearer | `c_a ≈ 0.15 to 0.5 ms` | 2,000 to 6,700 |
| Bearer with DPoP | `c_a + 0.1 ms` and one extra round trip | 1,700 to 4,000 |
| Client credentials, bcrypt on every request | 72 to 86 ms | 12 to 14 |
| Client credentials, verified once per credential and kept in process | `c_a` | 2,000 to 6,700 |

A machine client that sends its secret on every request costs a core 150 to 570 times what a user costs, so the middleware verifies a client secret once, keeps the SHA-256 of the presented secret per `client_id` in process, and on a later request compares that digest in constant time and rereads the `samna_client` row for status and expiry by primary key. The bcrypt hash still protects the stored secret; what is skipped is repeating the same proof.

### Queueing

Each tier is an M/M/c queue with `c` cores and service time `S`: throughput is bounded by `X < c / S`, and the time in system is `W = S (1 + C(c, ρ) / (c (1 − ρ)))` with `C` the Erlang C probability of waiting ([Erlang C formula](https://en.wikipedia.org/wiki/Erlang_(unit)#Erlang_C_formula)).

| `c` | `W / S` at ρ = 0.5 | 0.7 | 0.8 | 0.9 |
|---|---|---|---|---|
| 2 | 1.33 | 1.96 | 2.78 | 5.26 |
| 4 | 1.09 | 1.36 | 1.75 | 2.97 |
| 8 | 1.02 | 1.11 | 1.29 | 1.88 |
| 16 | 1.00 | 1.03 | 1.10 | 1.37 |

A 2 vCore database queues hardest, which is why its utilization budget is 0.7: `X_db ≤ 0.7 × k / c_d`. An API tier of eight or more cores runs at 0.8 with less than 30 % added latency.

Connections are not the constraint: by Little's law the connections in use are `X × T_conn` ([Little's law](https://en.wikipedia.org/wiki/Little%27s_law)), 5,000 requests a second holding a connection 0.5 ms each use 2.5, against 25 per replica pool in the connection budget of the Samna stack, `.claude/rules/pool.md` in the workspace. A pool binds only when the database is slow: at `T_conn = 5 ms`, cold reads, one replica's pool carries 5,000 a second.

### Ceilings

| Tier | Ceiling | `Standard_B2ms`, 60 % baseline | `Standard_D2ds_v5` |
|---|---|---|---|
| Database, checks, cached, `c_r = 0.11 ms` | `0.7 k / c_r` | 7,600 a second | 12,700 a second |
| Database, checks, cold | `IOPS / 2` | 960 a second | 1,875 a second |
| Database, composite writes, `c_w = 1.2 ms` | `0.7 k / c_w` | 700 a second | 1,170 a second |
| API, per core at 0.8 | `0.8 / c_a` | 1,600 to 5,300 a second | the same |

### Where the bottleneck is

The database of one service saturates before its API when the API's cores `N_c` exceed

```
N_c > k × c_a / c_d
```

With `c_d = c_r = 0.11 ms` and `c_a = 0.3 ms`: 2.7 API cores per database core, 5.5 per 2 vCore server. The connection budget caps a multi replica service at three replicas; at up to two cores each the API stays at or below the knee, and above it the database binds. Every hot standby that takes reads raises the knee by `k × c_a / c_r` again: with a read that finds nothing to settle running on a standby, `N_c > (1 + R) k c_a / c_r`.

With writes, `c_d = (1 − w) c_r + w (c_w + U_a A t)` and the governing inequality of the verdict follows by requiring `c_d < c_a` per request. Crossover write fractions, `c_a = 0.3 ms`:

| Pairs per write `U_a × A` | Settle `S` | Database is the larger consumer above |
|---|---|---|
| 50 | 1 ms | `w = 11 %` |
| 1,000 | 19 ms | `w = 0.96 %` |
| 10,000 | 190 ms | `w = 0.1 %` |
| 41,564, a root binding in the benchmark | 0.79 s | `w = 0.02 %` |

The settle is CPU on the database whichever path pays it, the first reader or the drain, so this inequality holds for both. What the on demand settle changes is who waits: a writer never waits on `S`, and a reader waits only on `A_u × t`, its own slice.

When the cache is exceeded the inequality changes shape: `c_d` gains `2 t_io` a check and the IOPS cap binds, `X ≤ IOPS / misses`. The cache holds `M / w_pair` pairs; at 6 GB of cache and `w_pair` of about 1 kB for a pair carrying its rendering fields in its own row and two tables, 6 million pairs; at 350 bytes for one narrow row, 17 million. The narrow row is the design; see below.

## The write side on its real shape

Claimius is API first. The write a person makes is one request: create a claim, bind it to an organization or location with `inherits`, grant it to `k` users. The fan out to everyone under the node happens only when someone reads.

| Step | Cost |
|---|---|
| The request, one transaction on the prophet | `≈ 7 + 4k` heap rows, `≈ 31 + 16k` index entries, `≈ 30` trigger statements: `c_w ≈ 1.2 ms` at `k = 3`, one commit, about 10 kB of WAL plus page images |
| The first read of one holder under the node | `A × t`: 6 ms for 300 objects, 0.38 s for 20,000 |
| Every holder who reads, over the life of the area | `R_a × A × t` on the request path, `R_a ≤ U_a` |
| The drain, off the request path | `(U_a − R_a) × A × t` |
| The same grants settled eagerly in the write | `k × A × t` inside the writer's transaction, every pair locked until commit |

Settling eagerly makes the write's latency and its lock time grow with `k × A`; on demand makes them constant, and the first reader pays `A t`, a factor `k` less than the eager write and `U_a` less than settling a whole area on its first reader. The pathological eager case is what the benchmark measured building its data set: 5,012 grants made one after another with every holder settled at once, a first read that materialized 393,490 pairs in 462 s in the per row code of that time, 1.17 ms a pair, which the set based settle does in 7.5 s and a per reader settle in `V_u × t`, 7.5 ms for the average user.

The prophet's write ceiling, `0.7 k / c_w`, is 1,170 composite writes a second on 2 vCores: a hundred thousand people each changing a grant every ninety seconds.

## The network

### Reads scale with disciples

A disciple answers every read from its own database and never calls its prophet. `N` disciples each bounded by `min(X_api,i, X_db,i)` give

```
X_read = Σ_i min(N_c,i / c_a, (1 + R_i) × 0.7 k_i / c_r)
```

linear in `N` and in the standbys of each, with no shared term. The read path has no ceiling in the network; it has one per service.

### Writes are bounded by the home prophet

A shared write of an app is decided by the app's home prophet, so `X_write(app) ≤ 0.7 k_p / c_w`, 1,170 a second on 2 vCores. Across `H` prophets each home for its own apps, `X_write = Σ_h 0.7 k_h / c_w`: federation adds write capacity by adding homes, never by sharing one app's writes between them. A routed write adds a disciple hop, `c_a` on each side and one HTTP round trip, to its latency and nothing to the prophet's CPU.

### Sync fan out

The feed carries source rows. An event of app `a` is pulled by the app's disciple and by every peer prophet mirroring the app, fan out `f_e = 1 + P_a`; an organization or location event is written once per app it reaches, `a_e` rows, so a pull reads one `(app_id, seq)` range and nothing else.

| Leg | Cost per event |
|---|---|
| Prophet, written | one heap row of about 500 bytes, measured 491 for a `user_claim` payload, and one entry in each `sync_event` index |
| Prophet, served | `1/204` of a leaf, `1/16` of a heap page, about 3 µs of JSON encoding: 5 µs a delivery |
| Disciple, applied | one upsert, its indexes, its closure edges and area: about 80 µs |

A prophet core serves `1 / 5 µs = 200,000` deliveries a second. At the write ceiling of 1,170 composite writes, each `3 + k = 6` events, with fan out 3, the feed costs the prophet `1,170 × 6 × 3 × 5 µs = 0.11` of a core. Fan out never binds before writes do.

### Staleness

A change committed on the prophet reaches a disciple after

```
τ = τ_wait + τ_rtt + τ_apply(n) + τ_commit
```

`τ_wait` is zero: a pull finding nothing is held open by the prophet until `emit_sync_event`'s `NOTIFY` arrives, delivered at commit ([NOTIFY](https://www.postgresql.org/docs/17/sql-notify.html)). `τ_apply(n) ≈ n × 80 µs` for a page of `n`. `τ_commit` is the wait for older transactions: the feed serves an event only once every transaction that could still commit a lower sequence has ended, `txid < pg_snapshot_xmin(pg_current_snapshot())` ([Snapshot information functions](https://www.postgresql.org/docs/17/functions-info.html#FUNCTIONS-PG-SNAPSHOT)), because sequences are handed out at insert and committed in another order, and a cursor that moved past a sequence still in flight would never see it. `τ_commit` is bounded by the longest prophet transaction, milliseconds, with `statement_timeout` as the hard cap. With one event on a cross region link, `τ ≈ 50 ms + 0.1 ms`. A reader that must see its own routed write waits on its sequence instead.

### Availability while the prophet is away

With independent failures, a disciple's read succeeds when its API and database are up, `A_read = A_api × A_db`, and a shared write needs the path to the home prophet, `A_write = A_api × A_db × A_net × A_prophet`. The disciple serves reads without contact for a fraction `1 − A_net × A_prophet` of the time: at 99.9 % each, 0.2 %, 17.5 hours a year in which a central decision would have failed every read and a disciple answers every one. The cost is bounded staleness: a revoke committed at `t_0` before a partition of length `P` reaches the disciple at `t_0 + P + τ`, and no revoke can be made while the prophet is unreachable, so the staleness a disciple serves never exceeds the feed lag at the moment contact was lost. A disciple out of contact longer than the trim interval, 7 days, restarts from a snapshot.

## What lowers each term

Each row names the dominant term, the mechanism, the formula before and after, and the factor.

| Term | Mechanism | Before | After | Factor |
|---|---|---|---|---|
| Statements per check | One SQL statement for the answer and the area probe; PL/pgSQL only on the branch that settles ([Inlining of SQL functions](https://wiki.postgresql.org/wiki/Inlining_of_SQL_functions)) | `t_s + 8 t_i + 19 t_d` = 0.27 ms | `t_s + 19 t_d` = 0.11 ms | 2.5 |
| Parse and plan per call | Named prepared statements, generic plans under `plan_cache_mode = auto` ([PREPARE](https://www.postgresql.org/docs/17/sql-prepare.html)) | `t_s = 60 µs` | about 25 µs | 1.6 on a check |
| Round trips per write request | The statements of one request sent as one batch in one transaction ([Pipeline mode](https://www.postgresql.org/docs/17/libpq-pipeline-mode.html)) | `(3 + k) × r_db` | `r_db` | `3 + k` |
| Heap reads of a listing | `user_object` keyed by the reader, index only with the visibility map | `V_u` pages, measured 15,778 for 20,000 rows | `⌈V_u / 87⌉`, 230 | 69 |
| Bytes per pair | Rendering and position fields once per object in `object_info`; usage on the pair; `object_users` folded into `user_object` | about 1 kB in two tables | about 350 B in one | 2.9, and the cache holds 2.9 times the pairs |
| Index entries per settled pair | `user_object` carries its primary key and `idx_user_object_object` only | 8 to 9 across `user_object` and `object_users` | 2 | 4 |
| Pages dirtied by one user's slice of `n` pairs | The primary key orders a user's pairs together, where a hashed id scatters them | `n` leaves of the id index | `⌈n / 87⌉` | up to 87, and as many fewer page images after a checkpoint ([WAL configuration, full_page_writes](https://www.postgresql.org/docs/17/runtime-config-wal.html)) |
| Settled pair time `t` | The two lines above | 19 µs | about 6 µs, write work being `(1 + 2) / (2 + 9)` of before | about 3 |
| First read after a binding | The reader settles its own slice | `U_a × A × t` | `A × t` | `U_a` |
| Grant latency and lock time | A grant raises the assignee's slice instead of settling it | `a + A_c × t`, measured 3.5 ms at 33 objects, 6.6 ms at 166 | `a`, constant | `1 + A_c t / a` |
| Rename of an object | One `object_info` row | `Ū_o` pairs through an area, 19 in the benchmark | 1 row, no area | `Ū_o` |
| Search candidates | `object_info.search_vector` under a GIN index, one entry per object, driving the reader's probe when the query is selective | `V_u` rows of 606 B | `min(V_u, M_q)` | `V_u / M_q` for a selective query |
| `user_users` writes per settle | Increments aggregated per `(viewer, target)` in the settle statement before they are written | `Σ U_o` over the settled pairs | the distinct pairs, at most `U_a × U_area` | `Σ U_o / distinct`, about 300 for 50 holders of a 300 object node reached by 20 others each |
| Database ceiling of one service | A read that finds no area runs on a hot standby; one that finds an area runs on the primary ([Hot Standby](https://www.postgresql.org/docs/17/hot-standby.html), [Azure read replicas](https://learn.microsoft.com/en-us/azure/postgresql/read-replica/concepts-read-replicas)) | `0.7 k / c_r` | `(1 + R) × 0.7 k / c_r` | `1 + R` |
| Feed wait | A held pull woken by `NOTIFY` | up to 15 s after an idle period | `τ_rtt` | up to 300 at 50 ms |
| Pull cost of shared hierarchy rows | Organization and location events written once per reached app, so the pull is one `(app_id, seq)` range | every pull reads every hierarchy event of every app after its cursor and probes `app_object_ids` per row | `n / 204` leaves | the share of hierarchy events of other apps |
| API CPU of a client call | Client secret verified once per credential, digest kept in process | 72 to 86 ms | `c_a` | 150 to 570 |
| Index writes of the write path | Only indexes a read uses: `sync_event` keeps its primary key and `(app_id, seq)`, and `written_at` is BRIN; the `sa_deleted_at` single column indexes of `claim`, `user_claim`, `claim_object` go, their reads filtering inside another index ([BRIN](https://www.postgresql.org/docs/17/brin.html)) | `31 + 16k` entries | `≈ 20 + 11k` | about 1.5 |
| Page images per write | `wal_compression = lz4` ([WAL configuration](https://www.postgresql.org/docs/17/runtime-config-wal.html)) | 8 kB per first touched page | 2 to 4 kB | 2 to 4 on WAL volume |
| Connections at scale | Transaction mode pooling between replicas and a shared server; claimius holds no session state across transactions ([PgBouncer](https://www.pgbouncer.org/features.html)) | `Σ maxReplicas × 50` | the pooler's server connections | `Σ maxReplicas × 50 / pool size` |

HOT updates are kept where no index covers the updated column: the usage counters on `user_object` sit on no index and the table is at fill factor 90, so a usage flush is heap only ([Heap Only Tuples](https://www.postgresql.org/docs/17/storage-hot.html)). A settle changes `sa_access`, which the primary key includes, so a settle update is never heap only and needs no free space kept for it; the fill factor is for the usage flush.

## Why

The request is where the work is. Claimius answers inside a database the service already calls, in one statement over indexes whose depth does not grow past four at twenty million pairs, and every write is constant in users and in reach, so the one term that grows with the data, the settle, is paid per reader slice and bounded by what that reader can see. What remains per request is fixed overhead, statements and round trips, and each is cut to one. The two ceilings left are physical: the memory that holds the per user index, which the narrow pair row stretches threefold, and the cores of one database, which standbys and disciples multiply.
