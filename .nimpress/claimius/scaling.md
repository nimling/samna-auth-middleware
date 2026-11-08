---
layout: doc
title: Scaling
description: The cost of every claimius operation as a formula in the parameters of the hardware it runs on, cores and clock, memory against index sizes, storage IOPS and latency, network round trips, standbys and pools, instantiated for the Azure servers claimius runs on today and for a self hosted reference node, with the queueing model, the bottleneck, the ceiling of a network of prophets and disciples, and the configuration each formula makes optimal.
tags: scaling, cost model, hardware, calibration, btree, planner cost, queueing, erlang c, little's law, bottleneck, throughput, latency, iops, wal, burstable, disciple, prophet, staleness
lastUpdated: true
---

# Scaling

The model is hardware independent. Every cost is measured once, on one core, in calibration units, and every ceiling is a formula in the parameters of the machine that runs it: its cores and their speed, its memory against the sizes of the indexes, its storage, its network. Azure Database for PostgreSQL Flexible Server is where claimius runs today, so the Azure servers of the Samna stack are the worked example and the baseline; a self hosted reference node shows how the ceilings move with the hardware. Neither is what claimius is designed for: the design is the formulas.

Every constant says where it comes from: a PostgreSQL default, a measurement of the claimius benchmark described in [the index](./index.md), or a published figure. What is assumed is marked as assumed.

## The parameters of a machine

| Symbol | Meaning |
|---|---|
| `n` | Physical cores of the database; threads `h n` with `h` hardware threads per core |
| `σ` | Speed of one database thread against the calibration core, running alone; within one microarchitecture `σ` scales with the clock, `σ = σ_arch × f / f_arch` |
| `η` | Throughput of a core with all its hardware threads busy against one thread alone; 1.25 for two way SMT, from Intel's own "up to 30 %" for Hyper-Threading ([Intel, Hyper-Threading Technology](https://www.intel.com/content/www/us/en/gaming/resources/hyper-threading.html)) |
| `K` | Capacity of the database in calibration cores: `K = n σ η`; on a burstable server sustained `K = b × v × σ` for base fraction `b` of `v` vCPUs |
| `ρ` | The utilization budget that keeps queueing below 2 × service time: 0.7 at two threads, 0.85 from sixty four; see Queueing |
| `M`, `s_b` | Memory and `shared_buffers` |
| `E` | Memory that caches distinct pages: `E = M − M_os − N_conn × m_conn − s_b`, with `m_conn ≈ 10 MB` per backend from the Samna connection budget and `s_b` subtracted once for pages cached twice |
| `I`, `L_r` | Random read IOPS, `min(I_vm, I_disk)`, and the latency of one random 8 kB read |
| `L_c` | Commit latency: the WAL flush, and the synchronous standby when one is configured |
| `B_wal` | Storage write bandwidth |
| `r_db` | Round trip between an API pod and its database |
| `r_net` | Round trip between a disciple and its prophet, or between two prophets |
| `R` | Hot standbys taking reads |
| `k_a`, `σ_a` | Cores of one API pod and their speed against the calibration core |
| `P` | Connections in one pool of one pod |

## Calibration

The calibration core is one performance core of the Apple M3 Pro laptop the benchmark ran on, under Docker with the data on tmpfs, so every calibrated time is CPU with no disk in it. A time `x` of the table runs in `x / σ` on one thread of another machine, and a machine completes `K / x` of them a second.

| Symbol | Value | Source |
|---|---|---|
| `seq_page_cost`, `random_page_cost` | 1.0, 4.0 | PostgreSQL defaults ([Planner Cost Constants](https://www.postgresql.org/docs/17/runtime-config-query.html#RUNTIME-CONFIG-QUERY-CONSTANTS)) |
| `cpu_tuple_cost`, `cpu_index_tuple_cost`, `cpu_operator_cost` | 0.01, 0.005, 0.0025 | PostgreSQL defaults, same page |
| `u`, time of one cost unit | 6 µs | Calibrated: the listing of 20,000 rows costs 0.114 units a row by the formula below and measured 14.4 ms, so 0.72 µs a row. It makes `cpu_operator_cost` 15 ns, `cpu_tuple_cost` 60 ns |
| `t_d`, one B-tree descent, cached | 2.5 µs | The planner's descent charge, `(⌈log₂ N⌉ + 50(h + 1)) × cpu_operator_cost` ([`btcostestimate`](https://github.com/postgres/postgres/blob/REL_17_STABLE/src/backend/utils/adt/selfuncs.c)), is 0.42 units at `N = 393,908`, `h = 2` |
| `t_s`, one client statement: protocol, parse, plan, executor | 60 µs | Measured: the `user_object` primary key probe as one statement took 0.066 ms, of which the descent and two tuples are 2.7 µs |
| `t_i`, one statement inside PL/pgSQL with a cached plan | 20 µs | Calibrated: a check measured 0.30 to 0.35 ms on the server, and the model puts eight inner statements in it |
| `t`, one settled pair, set based | 19 µs, about 6 µs on the narrow pair | Measured: 27,358 pairs in 522 ms; the narrow pair writes 3 heap and index entries where the measurement wrote 11 |
| `c_r`, a check | 0.11 ms | `t_s + (2C + L + a + 4) t_d` at `C = 5`, `L = 0`, `a = 4`, below |
| `c_w`, the API first write: a claim, a binding, `k = 3` grants | 1.2 ms | Estimate: about 30 trigger statements at `t_i` and 79 index entries |
| `a_e`, one feed event applied on a disciple | 80 µs | Estimate: an upsert and its trigger, four inner statements at `t_i` |
| `s_e`, one feed event served by a prophet | 5 µs | Estimate: `1/204` of a leaf, `1/16` of a heap page, 3 µs of JSON encoding |
| `c_a`, API CPU of a read request | 0.15 to 0.5 ms, 0.3 central | Estimate: echo routing and headers 20 µs, RS256 verify, claims decode 5 µs, one round trip in `lib/pq` and `bun` 20 µs, the service's own query 30 µs, a JSON response of 10 kB 100 µs, telemetry 20 µs |
| RS256 verify, 2048 bit, Go | 8 to 121 µs | [golang/go issue 63516](https://github.com/golang/go/issues/63516): 8 µs on Go 1.19, 121 µs on Go 1.21 after the constant time rewrite |
| bcrypt compare, cost 10, Go | 72 to 86 ms | [bcrypt-cost-go](https://github.com/nsmithuk/bcrypt-cost-go), 86 ms on a DigitalOcean high CPU VM |

Every conclusion below is an inequality and holds across the stated range of the estimates unless it says otherwise.

## Two machines

| | `Standard_B2ms` | `Standard_D2ds_v5` | Reference node, assumed |
|---|---|---|---|
| Cores | 2 vCPUs on Haswell to Ice Lake hosts, base 30 % of the VM: 0.6 vCPU sustained, 36 credits an hour, 864 banked ([Bv1 series](https://learn.microsoft.com/en-us/azure/virtual-machines/sizes/general-purpose/bv1-series)) | 2 vCPUs, the two hyper threads of one core, Xeon Platinum 8370C or 8573C, 3.5 GHz all core turbo ([Ddsv5 series](https://learn.microsoft.com/en-us/azure/virtual-machines/sizes/general-purpose/ddsv5-series)) | One socket, 32 cores, 64 threads near 4 GHz |
| `σ` | 0.45, assumed, the older host mix | 0.55, assumed: public Geekbench 6 single core results put the 8370C near 1,400 ([HMC, Xeon Platinum 8370C](https://hmc-tech.com/cpus/intel-xeon-platinum-8370c)) and the M3 Pro near 3,000 | 0.8, assumed |
| `K` | 0.27 sustained, 0.56 bursting | 0.69 | 32 |
| `M`, `s_b`, `E` | 8 GiB, 2 GiB, 4 GiB at 100 backends | 8 GiB, 2 GiB, 4 GiB | 256 GiB, 64 GiB, 183 GiB at 500 backends |
| `I` | `min(1,920, I_disk)` | `min(3,750, I_disk)` | 500,000, assumed, local NVMe |
| `I_disk`, Premium SSD | 500 at 128 GiB, 5,000 at 1 TiB, bursting to 3,500 up to 512 GiB ([Storage options](https://learn.microsoft.com/en-us/azure/postgresql/compute-storage/concepts-storage)) | the same | |
| `L_r` | 1 ms, assumed | 1 ms, assumed | 0.1 ms, assumed |
| `L_c` | 2 ms, assumed; Burstable carries no HA | 2 ms, assumed; zone redundant HA adds up to 2 ms ([High availability](https://learn.microsoft.com/en-us/azure/postgresql/high-availability/concepts-high-availability)) | 0.05 ms, assumed, power loss protected NVMe |
| `B_wal` | 22.5 MiB/s | 85 MiB/s ([Compute options](https://learn.microsoft.com/en-us/azure/postgresql/compute-storage/concepts-compute)) | 2,000 MiB/s, assumed |
| `r_db` | 1 ms, assumed, inside the 2 ms Microsoft gives between zones | 1 ms, assumed | 0.1 ms, assumed, one rack |
| `r_net` | 1 ms in region; 22 ms Norway East to West Europe, P50 ([Azure network round trip latency](https://learn.microsoft.com/en-us/azure/networking/azure-network-latency)) | the same | the same |
| Price | 0.175 USD an hour, Norway East | 0.239 USD an hour, Norway East ([Azure retail prices API](https://prices.azure.com/api/retail/prices)) | |

`σ` is the largest uncertainty in the table and the easiest to remove: one run of the benchmark on the target sets it. The burstable server bursts at `K = 0.56` for `864 / (2 − 0.6) = 617` minutes from a full bank, then falls to `K = 0.27`; its sustained figures are the ones it can be planned on.

## Ceilings

Each ceiling is a formula, and the table instantiates it. A time with no σ in it is calibrated.

| Ceiling | Formula | B2ms, sustained | D2ds_v5 | Reference |
|---|---|---|---|---|
| Checks, cached | `ρ K / c_r` | 1,720 a second | 4,380 | 247,000 |
| Checks, cold | `I / 2` | 250 at 128 GiB, 960 at 512 GiB | 250 at 128 GiB, 1,875 at 1 TiB | 250,000 |
| Check latency, cached | `r_db + c_r / σ` | 1.24 ms | 1.20 ms | 0.24 ms |
| Check latency, cold | `r_db + c_r / σ + 2 L_r` | 3.2 ms | 3.2 ms | 0.44 ms |
| API first writes, the write alone | `ρ K / c_w` | 158 a second | 401 | 22,700 |
| API first writes with their settle, `k = 3`, `A = 300` | `ρ K / (c_w + k A t)` | 10 at `t = 19 µs`, 29 at 6 µs | 26, 73 | 1,490, 4,120 |
| Write latency | `r_db + c_w / σ + L_c` | 5.7 ms | 5.2 ms | 1.65 ms |
| Writes by WAL bandwidth, 20 kB a write | `B_wal / w_wal` | 1,180 a second | 4,460 | 105,000 |
| Settled pairs | `K / t` | 14,200 a second | 36,200 | 1,680,000 |
| Feed deliveries served | `K / s_e` | 54,000 a second | 137,500 | 6,400,000 |
| Feed events applied, one page stream | `σ / a_e` | 5,600 a second | 6,900 | 10,000 |
| Pairs the cache holds | `E / w_pair`, `w_pair = 350 B` | 12 million | 12 million | 560 million |
| Connections that keep the database busy | `h n × (r_db + c_r / σ) / (c_r / σ)` | 10 | 12 | 111 |
| API cores before the database binds | `K c_a / (c_r σ_a η_a / h_a)`, same core class | 2.6 vCPUs | 5.5 vCPUs | 87 cores |

`k A t` is the settle a write causes, 17 ms at `t = 19 µs`: fourteen times the write itself. On the API first shape the database of a write is bound by the settle it leaves behind, not by the write, which is why who settles, and when, is the subject of [Materialization](./materialization.md).

## The verdict

Per request, the API tier spends more CPU than claimius spends in the database, for every read and for any mix whose writes settle fewer than about ten pairs per request. With `w` the write fraction, `c_aw` the API CPU of a write and `S = U_a × A × t` the settle a write causes:

```
(1 − w)(c_a − c_r)  >  w (c_w − c_aw + S)
w × U_a × A  <  (c_a − c_r) / t  ≈ 10 settled pairs per request, 32 at t = 6 µs
```

The inequality is in calibration units on both sides, so it holds on any hardware where the API and the database run the same class of core, and on mixed hardware after multiplying the right side by `σ_d / σ_a`.

Three places where the thesis fails:

1. **One service at scale.** The database of one service saturates first once its API runs more than `K_d c_a / c_r` calibration cores: 2.7 per database calibration core, 5.5 vCPUs of API against a `D2ds_v5`. The fix is structural: reads that find nothing to settle run on a hot standby, which multiplies the database ceiling by `1 + R`, and every disciple is its own database.

2. **A per user index larger than memory.** Once the pairs of the active users exceed `E / w_pair`, a check costs two random reads and `I / 2` is the ceiling. On the Azure servers at 128 GiB of storage that is 250 checks a second, below the CPU ceiling by a factor of 7 to 17, so on Azure the storage size, not the SKU, sets the cold ceiling. On the reference node the cold ceiling is 250,000 and memory stops mattering.

3. **The settle of wide writes.** A binding over `A` objects held by `U_a` users costs `U_a A t` wherever it is settled. It is paid once per database that settles it, so the network pays it once per disciple of the app, and never on the prophet for an app its disciples read; see [Materialization](./materialization.md).

## The shape of the indexes

A B-tree page is 8,192 bytes with a 24 byte header and a 16 byte special area; an index tuple is an 8 byte header plus its `MAXALIGN`ed key and include columns, plus a 4 byte line pointer ([Database Page Layout](https://www.postgresql.org/docs/17/storage-page-layout.html)). Leaves are built at fill factor 90, and over 99 % of pages are leaves ([B-Tree Structure](https://www.postgresql.org/docs/17/btree.html#BTREE-STRUCTURE)). Fanout `B = 8,152 × 0.9 / entry` and height `h = 1 + ⌈log_{B_int}(N / B)⌉`; the levels above the leaves are `1/B` of the index and stay cached, so an uncached descent misses one leaf.

| Index | Entry | Leaf fanout `B` | Bytes per entry | `h` at 400 k | `h` at 20 M |
|---|---|---|---|---|---|
| `user_object` primary key `(user_id, app_id, object_type, object_id) INCLUDE (sa_access)` | 84 B | 87 | 94 | 3 | 4 |
| `idx_user_object_object (object_id, object_type)`, deduplicated | measured 807 per page | 807 | 10 | 2 | 3 |
| `idx_user_claim_user_app (user_id, app_id)` | 44 B | 167, more with deduplication | 49 | 2 | 3 |
| `idx_sync_event_app_seq (app_id, seq)` | 36 B | 204 | 40 | 2 | 3 |
| `access_diff` primary key | 28 B | 262 | 31 | 1 | 1 |

An index with `INCLUDE` columns never deduplicates ([Deduplication](https://www.postgresql.org/docs/17/btree.html#BTREE-DEDUPLICATION)), so a covering index on a per user range pays its full entry width; that is why the `user_object` range and the probe share one index, the primary key, rather than two. The memory each index needs, and the threshold at which it leaves memory, are in [Storage](./storage.md).

## Cost per operation

Variables: users `U`, objects `O`, tree depth `D`, grants per user `C`, claims composed by them `L`, ancestors of an object `a`, bindings on an object and its ancestors `B_x`, bindings per claim `K_c`, fan out `F`, objects a user reaches `V_u`, pairs in an area `A`, holders of a claim `U_a`, grants in one `user_object` row `G`, matches of a search query `M_q`. Times are calibrated CPU on one core, cached; "misses" are the random reads when nothing but the upper B-tree levels is cached.

| Operation | Statements | Descents | Heap rows | Tuples | Rows written | WAL | Locks | CPU cached | Misses |
|---|---|---|---|---|---|---|---|---|---|
| Check, `mutation_access` | 1 | `2C + L + a + 4` | `2C + B_x + 2` | `2C + a + B_x + 3` | 0 | 0 | none | `t_s + (2C + L + a + 4) t_d` = 0.11 ms at `C = 5, L = 0, a = 4` | 2 |
| Listing, `get_objects` | 1 | `2C + 5 + V_u`, the last `V_u` into the small `object_info` | `V_u` from `object_info` | `V_u` | 0 | 0 | none | `V_u × 0.114 u + t_s`: 0.7 µs a row | `⌈V_u / 87⌉` |
| Listing ids, `ObjectWhere` | 1 | `2C + 5` | `V_u × f_nv` | `V_u` | 0 | 0 | none | `V_u × 0.03 u`: 0.2 µs a row | `⌈V_u / 87⌉` |
| Search | 1 | `2C + 5` | `min(V_u, M_q)` | `min(V_u, M_q)` | 0 | 0 | none | `min(V_u, M_q) × (t_d + t_m)`; see [Search](./search.md) | `min(⌈V_u / 87⌉, M_q)` |
| Owner graph window, depth `d`, width `W` | 1 | `n_r + 1` | `n_r` | `n_r` | 0 | 0 | none | `n_r × t_n`; see [Graphs](./graphs.md) | `n_r` |
| Organization or location insert | 1 | `(D + 1)` reads, `≈ 5(D + 1) + 20` inserts | `D + 7` written | `D + 1` | `D + 7` | `≈ 250(D + 7) + 60 × index entries` B | row locks on new rows; `FOR KEY SHARE` on parent and app | `≈ 0.6 ms` | constant |
| Grant, `insert_user_claim` | 1 | 16 inserts | 4 written | 0 | 4 | ≈ 2 kB | new rows; `FOR KEY SHARE` on claim, app, organization | ≈ 0.3 ms | constant |
| Revoke, `delete_user_claim` | 1 | 16 | 4 | 1 | 4 | ≈ 2 kB | the `user_claim` row | ≈ 0.3 ms | constant |
| Binding, `insert_claim_object` | 1 | one check + 16 inserts | 4 | 0 | 4 | ≈ 2 kB | the claim's area row until commit | ≈ 0.4 ms | constant |
| First listing of a reader under a raised area | 1 | as the read | as the read | `A_u` | `A_u + 1` | `≈ 360 A_u` B | the reader's marker, the reader's pairs | read `+ A_u × t` | `⌈A_u / 87⌉` |
| Sync page of `n` events on a disciple | 1 transaction | `n (1 + i_e)` | `n` | `n` | `n + Σ(D + 1) + areas` | `≈ n (payload + 60 i_e)` | the page's rows | `n × a_e` | `≈ n` |
| Audit row | inside the write | 3 | 1 | 0 | 1 | ≈ 400 B | none | 3.7 µs measured | 0 |

`f_nv` is the fraction of the reader's range on pages not yet marked all visible; an index only scan fetches the heap only for those ([Index Only Scans](https://www.postgresql.org/docs/17/indexes-index-only-scans.html)). `i_e` is the number of indexes on the event's table. `A_u` is the reader's own pairs in the area: the objects under the bound node for a claim area, `A = Σ_{i=0}^{D−D_n} F^i` for a node at depth `D_n` of a tree of fan out `F`, plus the registered rows under it.

### Where the listing formula comes from

Per row: one index tuple 0.005, one heap tuple 0.01, the mask test 0.005, `G = 1.5` grants each resolved against the reader's grants hashed once, `1.5 × 0.015`, and the sort, `2 × cpu_operator_cost × log₂ V_u` comparisons a row, 0.0715 at `V_u = 20,000`. Total 0.114 units, 0.69 µs at `u`, measured 0.72 µs. The sort is 63 % of it; a listing read in the primary key order, `(object_type, object_id)` within the reader's range, is already ordered and pays none of it, so `get_objects` returns rows in that order and pages by keyset on it.

### Where the check formula comes from

The reader's grants, one range on `idx_user_claim_user_app` and `C` claim probes; the links out of them, `C` probes of `idx_claim_object_lookup`, and the `L` composed claims; the object's `object_info` row, one descent that gives existence, creator and the `a` ancestors of its up set; the bindings on the object and each ancestor, `a + 1` probes of `idx_claim_object_object`, returning `B_x` rows; `samna_user`. At `C = 5`, `L = 0`, `a = 4` that is 19 descents, 48 µs, and one client statement, 60 µs: 0.11 ms. The same check written as a PL/pgSQL function of eight statements adds `8 t_i = 160 µs`, 0.27 ms, which is what the benchmark measured. The check is therefore one SQL statement, and it evaluates rather than reads a stored pair, because proving a stored pair current costs `3C + D + 5` descents and a settle; see [Reads](./reads.md#why-the-pairs-are-stored).

## Transactions per API request

| Request | Transactions | Round trips | What it locks, and for how long | Isolation |
|---|---|---|---|---|
| Read one object, nothing pending | 1, autocommit | 0 extra when the check is a predicate of the service's own query, 1 when issued alone | Nothing but relation `AccessShareLock` | Read committed |
| Listing | 1 | 0 extra: `ObjectWhere` is a predicate in the service's query | Nothing | Read committed |
| Read that finds a raised area | 1 | as above | The reader's marker row and its own `A_u` pairs, for `A_u t / σ` | Read committed; the marker records the area's `sa_updated_at` read at the start, so a write that raises the area again during the settle is settled by the next read |
| Create a claim, bind it, grant it to `k` users | 1 on the prophet | 1 with the statements sent as one batch, `3 + k` without | New rows only; `FOR KEY SHARE` on the owning organization, the app and the claim; the claim's area row from the binding to commit | Read committed |
| Routed write from a disciple | 1 on the prophet, then 1 local upsert on the disciple | 2 HTTP hops, 2 database round trips | As above, on each side | Read committed |
| Sync page | 1 on the disciple | 1 HTTP pull, 1 commit | The page's rows until commit | Read committed |

The rules that fall out of the formulas:

1. **One statement per question, and no round trip of its own.** A statement costs `t_s = 60 µs` from a client and `t_i = 20 µs` inside a function, while a descent costs 2.5 µs, so every read is one statement and every write request one transaction. The round trip costs more than the statement on every network: `r_db / (r_db + c_r / σ)` of a check issued alone is the wire, 83 % on the Azure servers and 42 % on the reference node. A read of one object therefore carries its check as a predicate of the service's own query, the way a listing carries `ObjectWhere`, and pays no round trip for it; see [Access in Go](./middleware/access.md).

2. **Areas are raised last.** The only row two writers of different objects can both lock is an area row, and a writer holds it from the raise to its commit. With writers on one claim arriving at `λ_c` and holding for `L_c`, they queue when `λ_c L_c ≥ 1`: 500 writes a second on one claim at a 2 ms commit, 20,000 at 0.05 ms.

3. **Nothing on a read path waits on a write.** A read with nothing pending takes no row lock. A settling read locks only its own marker and its own pairs, so two readers of one area settle their slices in parallel, and two reads by the same user of the same area wait on the marker's unique key: the first settles, the second finds the marker and reads.

4. **Deferred, never dropped.** Every write's effect on other users is an area row in the write's own transaction, committed or rolled back with it, and settled by the reader or the drain. Nothing is queued outside the database.

## The API tier

The request path in `smw`: a bearer token is one RS256 verify against the cached JWKS; a DPoP bound token adds an ES256 verify and one insert into `dpop_jti`, a second round trip; a session cookie is the same verify; client credentials are one bcrypt compare. A request then spends one round trip per claimius call issued alone and one per service query.

| Credential | API CPU | Requests a second per calibration core |
|---|---|---|
| Bearer | `c_a ≈ 0.15 to 0.5 ms` | 2,000 to 6,700 |
| Bearer with DPoP | `c_a + 0.1 ms` and one extra round trip | 1,700 to 4,000 |
| Client credentials, bcrypt on every request | 72 to 86 ms | 12 to 14 |
| Client credentials, verified once per credential and kept in process | `c_a` | 2,000 to 6,700 |

A machine client that sends its secret on every request costs a core 150 to 570 times what a user costs, so the middleware verifies a client secret once, keeps the SHA-256 of the presented secret per `client_id` in process, and on a later request compares that digest in constant time and rereads the `samna_client` row for status and expiry by primary key.

### Queueing

Each tier is an M/M/c queue with `c` threads and service time `S`: throughput is bounded by `X < c / S`, and the time in system is `W = S (1 + C(c, ρ) / (c (1 − ρ)))` with `C` the Erlang C probability of waiting ([Erlang C formula](https://en.wikipedia.org/wiki/Erlang_(unit)#Erlang_C_formula)).

| `c` | `W / S` at ρ = 0.5 | 0.7 | 0.8 | 0.9 |
|---|---|---|---|---|
| 2 | 1.33 | 1.96 | 2.78 | 5.26 |
| 4 | 1.09 | 1.36 | 1.75 | 2.97 |
| 8 | 1.02 | 1.11 | 1.29 | 1.88 |
| 16 | 1.00 | 1.03 | 1.10 | 1.37 |

The utilization budget `ρ` is the highest that keeps `W / S` near 2: 0.7 at two threads, 0.85 at sixty four. That is why a 2 vCPU server is planned at 70 % and the reference node at 85 %, and why an API tier of eight or more threads runs at 0.8.

### Connections

By Little's law the connections a pod holds busy are `L = X_pod × T_conn`, with `T_conn = r_db + c_r / σ` for a check ([Little's law](https://en.wikipedia.org/wiki/Little%27s_law)). With arrivals independent the busy count is Poisson with mean `L`, so a pool of `P = q_{0.999}(L)` connections waits for a free one on one request in a thousand. The database, for its part, is kept busy by `h n × T_conn / (c_r / σ)` connections, 12 on a `D2ds_v5` and 111 on the reference node; connections beyond that queue inside the database instead of in the pool. The sizing of the pools, and why the idle count must equal the pool size, are in [Storage](./storage.md).

### Where the bottleneck is

The database of one service saturates before its API when the API's calibration cores exceed

```
N_a > K_d × c_a / c_d
```

With `c_d = c_r = 0.11 ms` and `c_a = 0.3 ms`: 2.7 API calibration cores per database calibration core. On one core class that is 5.5 API vCPUs per `D2ds_v5`, whatever the clock, because the clock divides both sides. Every hot standby that takes reads raises the knee by `K_d c_a / c_r` again: `N_a > (1 + R) K_d c_a / c_r`.

With writes, `c_d = (1 − w) c_r + w (c_w + U_a A t)` and the inequality of the verdict follows by requiring `c_d < c_a` per request. Crossover write fractions, `c_a = 0.3 ms`:

| Pairs per write `U_a × A` | Settle `S` | Database is the larger consumer above |
|---|---|---|
| 50 | 1 ms | `w = 11 %` |
| 1,000 | 19 ms | `w = 0.96 %` |
| 10,000 | 190 ms | `w = 0.1 %` |
| 41,564, a root binding in the benchmark | 0.79 s | `w = 0.02 %` |

The settle is CPU on the database whichever path pays it, the first reader or the drain, so this inequality holds for both. What the on demand settle changes is who waits: a writer never waits on `S`, a check waits on nothing because it evaluates, and a listing on `A_u t / σ`, its own slice.

When the cache is exceeded, `c_d` gains `2 L_r` a check and the IOPS cap binds, `X ≤ I / 2`. The cache holds `E / w_pair` pairs: 12 million on either Azure server, 560 million on the reference node, at the 350 bytes of the narrow pair.

## The write side on its real shape

Claimius is API first. The write a person makes is one request: create a claim, bind it to an organization or location with `inherits`, grant it to `k` users. The fan out to everyone under the node happens only when someone reads.

| Step | Cost |
|---|---|
| The request, one transaction on the prophet | `≈ 7 + 4k` heap rows, `≈ 31 + 16k` index entries, `≈ 30` trigger statements: `c_w ≈ 1.2 ms` at `k = 3`, one commit, about 10 kB of WAL plus page images |
| The first read of one holder under the node | A check: nothing beyond its evaluation, `c_r / σ`. A listing: its slice, `A × t / σ`, 10 ms for 300 objects, 0.69 s for 20,000 |
| Every holder who reads, over the life of the area | `R_a × A × t` on the request path, `R_a ≤ U_a` |
| The drain, off the request path | `(U_a − R_a) × A × t` |
| The same grants settled eagerly in the write | `k × A × t` inside the writer's transaction, every pair locked until commit |

Settling eagerly makes the write's latency and its lock time grow with `k × A`; on demand makes them constant, and the first reader pays `A t`, a factor `k` less than the eager write and `U_a` less than settling a whole area on its first reader. The pathological eager case is what the benchmark measured building its data set: 5,012 grants made one after another with every holder settled at once, a first read that materialized 393,490 pairs in 462 s in the per row code of that time, 1.17 ms a pair, which the set based settle does in 7.5 s and a per reader settle in `V_u × t`, 7.5 ms for the average user.

The database that decides a write carries `ρ K / c_w` of them, 401 a second on a `D2ds_v5`; the database that settles it carries `ρ K / (c_w + k A t)`. Those are different databases for every app with disciples, which is why the prophet settles only what is read on it.

## The network

### Reads scale with disciples

A disciple answers every read from its own database and never calls its prophet. `N` disciples each bounded by their own API and database give

```
X_read = Σ_i min(N_a,i / c_a, (1 + R_i) × ρ_i K_i / c_r)
```

linear in `N` and in the standbys of each, with no shared term.

### Writes are bounded by the home prophet

A shared write of an app is decided by the app's home prophet, so `X_write(app) ≤ ρ K_p / c_w`. Across `H` prophets each home for its own apps, `X_write = Σ_h ρ K_h / c_w`: federation adds write capacity by adding homes, never by sharing one app's writes between them. The feed that carries the writes out, its throughput, its staleness and its catch up, is in [Sync](./sync.md); federation and availability are in [Prophets and disciples](./prophets.md).

## What lowers each term

Each row names the dominant term, the mechanism, the formula before and after, and the factor.

| Term | Mechanism | Before | After | Factor |
|---|---|---|---|---|
| Statements per check | One SQL statement for the answer and the area probe; PL/pgSQL only on the branch that settles ([Inlining of SQL functions](https://wiki.postgresql.org/wiki/Inlining_of_SQL_functions)) | `t_s + 8 t_i + 19 t_d` = 0.27 ms | `t_s + 19 t_d` = 0.11 ms | 2.5 |
| Parse and plan per call | Named prepared statements, generic plans under `plan_cache_mode = auto` ([PREPARE](https://www.postgresql.org/docs/17/sql-prepare.html)) | `t_s = 60 µs` | about 25 µs | 1.5 on a check |
| Round trip per single object read | The check as a predicate of the service's own query | `r_db + c_r / σ` | `c_r / σ` | 6 on Azure, 1.7 on the reference node |
| Round trips per write request | The statements of one request sent as one batch in one transaction ([Pipeline mode](https://www.postgresql.org/docs/17/libpq-pipeline-mode.html)) | `(3 + k) × r_db` | `r_db` | `3 + k` |
| Connection opens per request | Pool idle count equal to the pool size, idle connections closed by `ConnMaxIdleTime` | 0.23 at 2 busy with 2 idle kept | under 0.001 | the cost of a backend start on a fraction of requests; see [Storage](./storage.md) |
| Heap reads of a listing | `user_object` keyed by the reader, index only with the visibility map | `V_u` pages, measured 15,778 for 20,000 rows | `⌈V_u / 87⌉`, 230 | 69 |
| Sort in a listing | Rows returned in primary key order, keyset paged | 0.114 units a row | 0.043 | 2.7 |
| Bytes per pair | Rendering and position fields once per object in `object_info`; usage on the pair; `object_users` folded into `user_object` | about 1 kB in two tables | about 350 B in one | 2.9, and the cache holds 2.9 times the pairs |
| Index entries per settled pair | `user_object` carries its primary key and `idx_user_object_object` only | 8 to 9 across `user_object` and `object_users` | 2 | 4 |
| Pages dirtied by one user's slice of `n` pairs | The primary key orders a user's pairs together | `n` leaves of the id index | `⌈n / 87⌉` | up to 87, and as many fewer page images after a checkpoint ([WAL configuration, full_page_writes](https://www.postgresql.org/docs/17/runtime-config-wal.html)) |
| Settled pair time `t` | The two lines above | 19 µs | about 6 µs | about 3 |
| First read after a binding | A listing settles the reader's slice; a check evaluates and settles nothing | `U_a × A × t` | `A × t` for a listing, 0 for a check | `U_a` for a listing; the check never waits |
| Settle on the prophet | The prophet drains only the apps it serves reads for | `U_a A t` per app on the prophet and again on its disciple | on the disciple only, and on the prophet for the actors of routed writes | `1 + P_a` databases to 1, per write |
| Reader wait under a backlog | The drain orders slices by their holder's latest use | first in, first out | the order of least expected wait | see [Materialization](./materialization.md) |
| Grant latency and lock time | A grant raises the assignee's slice instead of settling it | `a + A_c × t`, measured 3.5 ms at 33 objects, 6.6 ms at 166 | `a`, constant | `1 + A_c t / a` |
| Rename of an object | One `object_info` row | `Ū_o` pairs through an area, 19 in the benchmark | 1 row, no area | `Ū_o` |
| Search candidates | `object_info.search_vector` under a GIN index, driving the reader's probe when the query is selective | `V_u` rows of 606 B | `min(V_u, M_q)` | `V_u / M_q` for a selective query |
| Children of a wide node in a name ordered graph window | `object_info (sa_owner_id, sa_name, object_id)` and its location twin read in order | all `F` children | `W / v` | `F v / W`; see [Graphs](./graphs.md) |
| Catch up after a long absence | A snapshot once the backlog exceeds the app's source rows | `n_b a_e` | `N_src a_e` | `n_b / N_src`; see [Sync](./sync.md) |
| `user_users` writes per settle | Increments aggregated per `(viewer, target)` in the settle statement | `Σ U_o` over the settled pairs | the distinct pairs | `Σ U_o / distinct`, about 300 for 50 holders of a 300 object node reached by 20 others each |
| Database ceiling of one service | A read that finds no area runs on a hot standby ([Hot Standby](https://www.postgresql.org/docs/17/hot-standby.html)) | `ρ K / c_r` | `(1 + R) ρ K / c_r` | `1 + R` |
| Feed wait | A held pull woken by `NOTIFY` | up to 15 s after an idle period | `r_net / 2` | up to 1,400 at 22 ms |
| Staleness on a disciple in region | The page applied with `synchronous_commit = off`, the cursor lost with it on a crash | `r_net / 2 + a_e / σ + L_c`, 2.6 ms on Azure | `r_net / 2 + a_e / σ`, 0.65 ms | 4 on Azure |
| Pull cost of shared hierarchy rows | Organization and location events written once per reached app | every hierarchy event of every app after the cursor | `n / 204` leaves | the share of hierarchy events of other apps |
| API CPU of a client call | Client secret verified once per credential, digest kept in process | 72 to 86 ms | `c_a` | 150 to 570 |
| Index writes of the write path | Only indexes a read uses; `sync_event.written_at` as BRIN ([BRIN](https://www.postgresql.org/docs/17/brin.html)) | `31 + 16k` entries | `≈ 20 + 11k` | about 1.5 |
| Page images per write | `wal_compression = lz4` ([WAL configuration](https://www.postgresql.org/docs/17/runtime-config-wal.html)) | 8 kB per first touched page | 2 to 4 kB | 2 to 4 on WAL volume |
| Connections at scale | Transaction mode pooling between replicas and a shared server ([PgBouncer](https://www.pgbouncer.org/features.html)) | `Σ maxReplicas × 2P` | the connections that keep the server busy | `Σ maxReplicas × 2P / (h n T_conn / (c_r / σ))` |

## The configuration the formulas choose

| Parameter | Governing inequality or optimum | Value | Where |
|---|---|---|---|
| Utilization budget `ρ` | `W / S ≤ 2` in the Erlang C table | 0.7 at two threads, 0.85 at sixty four | above |
| Pool size per pod `P` | `P = q_{0.999}(X_pod,max × T_conn)` | 10 on Azure pods of 2 vCPUs, 10 on the reference pods | [Storage](./storage.md) |
| Idle connections per pool | Opens per request under 0.001 needs `idle ≥ q_{0.999}(L)` | equal to `P`; `ConnMaxIdleTime` scales down | [Storage](./storage.md) |
| Drain batch `b` pairs | `(t_s / σ + L_c) / (ε t / σ) ≤ b ≤ τ_lock σ / t`, ε = 5 %, `τ_lock` = 50 ms | 1,000 | [Materialization](./materialization.md) |
| Drain order | Smith's rule on expected reader wait | holders by latest use, most recent first | [Materialization](./materialization.md) |
| Pull page `L` | `L ≥ (1 − ε) c_0 σ / (ε a_e)` for efficiency `1 − ε` | 1,000, 87 % across Norway East to West Europe, 99 % in region | [Sync](./sync.md) |
| Snapshot or replay | replay while `n_b < N_src` | the prophet answers a pull with a snapshot above it | [Sync](./sync.md) |
| Partition interval | `N_p p_c ≤ ε T_q` with `N_p = retention / interval` | a month for a 13 month audit, a day for 30 days of raw events | [Audit](./audit.md) |
| `user_object` fill factor | `1 − ff ≥ u_p w_heap / 8,192` | 90 | [Storage](./storage.md) |
| Prepared statements | parse and plan paid once per connection | named, for every access statement | [Reads](./reads.md) |

## Why

The request is where the work is. Claimius answers inside a database the service already calls, in one statement over indexes whose depth does not grow past four at twenty million pairs, and every write is constant in users and in reach, so the one term that grows with the data, the settle, is paid per reader slice and bounded by what that reader can see. Every ceiling is a ratio of a machine parameter to a calibrated cost, so moving to faster cores, more memory or local storage moves each ceiling by exactly the ratio of that parameter, and nothing in the design has to change for it. What remains per request is fixed overhead, statements and round trips, and each is cut to one or to none. The two ceilings left are physical: the memory that holds the per user index of the active users, which the narrow pair row stretches threefold, and the cores of one database, which standbys and disciples multiply.
