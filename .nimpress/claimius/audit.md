---
layout: doc
title: Audit
description: Why the audit trail lives in the claimius schema and is written by the database in the transaction of the change, what a row holds, what it costs, who can touch it, and how long it is kept.
tags: audit, write_audit, get_audit, sa_created_by, actor, retention, partition, integrity
lastUpdated: true
---

# Audit

`claimius.audit` is the append only record of what was done, by which grant, to what.

| Column | Holds |
|---|---|
| `app_id` | The app |
| `operation`, `type` | What happened: `insert`, `update`, `delete`, `error`, a denied attempt |
| `object_type`, `object_id` | What it happened to |
| `sa_owner_id` | The organization the object belongs to, which decides who may read the row |
| `sa_created_by` | The `user_claim.id` the actor acted under, NULL for system events with no actor |
| `message` | Free text, the error for an `error` row |
| `sa_created_at` | When |

## Written in the transaction of the change

1. Every audited write writes its row through `write_audit` from a trigger or a write function, in the same transaction as the change. The change and its record commit together or not at all.

2. The actor is the `user_claim.id` of `sa_created_by`: the grant that made the write legal, carrying its claim, its owning organization and its window. Later changes to a row, which carry no `sa_updated_by`, are attributed here.

3. A denied attempt has no transaction to join, because the check that denied it aborted. The middleware writes it separately, after the refusal; it is the one audit row that is not atomic with anything.

4. `get_audit(user, app, required, limit, offset)` reads the trail through the same access as everything else: a user reads the rows whose `sa_owner_id` is an organization they reach. `resolve_access` answers `claimius.audit` the same way.

## Why in the database

1. Integrity. A record written in the transaction of the change cannot describe a change that did not commit, and a change cannot commit without its record. A record written after the fact by the application can be lost to a crash, a timeout or a retry between the two.

2. Tamper surface. Only `claimius_admin` may update or delete `audit`. A service connects as reader or writer and can add rows through `write_audit` and nothing else, so a compromised service can add noise to the trail but cannot remove its own tracks.

3. One actor model. The actor is a grant, the same value every row carries in `sa_created_by`, so the trail joins to the grants, claims and organizations with the keys that already exist.

4. Cost. One insert of about 200 bytes and four index entries per audited write, in a transaction that is already writing. No second round trip, no queue, no consumer.

5. Scope. The trail of an app lives in the database of the service that did the work: a disciple's own tables are audited on the disciple, shared writes on the prophet that decided them. The trail is not synced.

## Retention

`audit` is a declarative range partitioned table by month on `sa_created_at`, with a BRIN index on `sa_created_at` and btree indexes on `app_id`, `sa_owner_id` and `(object_type, object_id)` ([PostgreSQL, Table Partitioning](https://www.postgresql.org/docs/17/ddl-partitioning.html), [BRIN Indexes](https://www.postgresql.org/docs/17/brin.html)). `pg_partman` creates each month ahead of time and detaches the months past retention ([pg_partman](https://github.com/pgpartman/pg_partman)). The retention period is the app's; the default is 13 months, one year plus the month being written.

Measured on 2,000,000 synthetic audit rows over 24 months:

| | Measured |
|---|---|
| Insert, per row, with the three btree indexes | 3.7 µs |
| Index on `sa_created_at` | 32 kB as BRIN, one summary per 128 pages |
| The last 7 days, 16,666 rows | 3.1 ms, 24 of 25 partitions pruned at plan time |
| Dropping a month of 86,111 rows | 0.6 ms, `DETACH PARTITION`, nothing left for vacuum |

Why: audit rows arrive in time order and are read by time window, by object and by owner, and leave by age. Arrival order makes the heap sorted by `sa_created_at`, which is the case a block range index summarizes in a few kilobytes and scans in page ranges. A month partition turns retention into a catalog operation and lets the planner skip every month outside a window. Both are in core Postgres and `pg_partman` is on the allow list of Azure Database for PostgreSQL Flexible Server, so the design runs where Samna runs today ([Microsoft Learn, Extensions by engine](https://learn.microsoft.com/en-us/azure/postgresql/extensions/concepts-extensions-by-engine)). Compressing old months in place is the one gain left for a time series extension; the TimescaleDB edition Azure provides is the Apache 2 edition, which has no compression ([Microsoft Learn, timescaledb](https://learn.microsoft.com/en-us/azure/postgresql/extensions/concepts-extensions-considerations#timescaledb), [Tiger Data, Compare TimescaleDB editions](https://www.tigerdata.com/docs/about/latest/timescaledb-editions)), so that step waits with the rest of [the extension](./extension.md).

### The partition interval and the cost of a row

With `λ_a` audit rows a second, `R_t` the retention and `Δ` the partition interval, the constants of [Scaling](./scaling.md) and the measurements above:

| Quantity | Formula | Value |
|---|---|---|
| Insert, per row | three btree entries and a BRIN summary, inside a transaction already writing | 3.7 µs calibrated, `3.7 / σ` on a machine |
| Rows per API first write | one per claim, binding and grant | `2 + k` |
| Bytes per row | about 200 heap and 130 index | 330 |
| Size held | `λ_a × (R_t + Δ / 2) × 330 B` | 11.7 GB at one row a second over 13 months, of which 3.8 % is the half month past retention |
| Partitions | `N_p = R_t / Δ + 1` | 14 by month |
| Planning, per partition | measured: 1.07 ms with 25 partitions against 0.06 ms on one table | `p_c ≈ 0.04 ms` |
| Retention | `DETACH PARTITION`, measured 0.6 ms for 86,111 rows, against 92.7 ms for the `DELETE` and a vacuum after it | constant in the rows removed |
| A range query | pruned partitions, then BRIN ranges of 128 pages, lossy by at most one range at each end | measured 3.1 ms for 7 days |

The interval balances two costs. A partition adds `p_c` to the planning of every query that is not a prepared statement, and a longer interval holds on average `Δ / 2` of rows past retention. Planning stays under a share `ε` of a query of time `T_q` when

```
Δ ≥ R_t p_c / (ε T_q)
```

For a 13 month trail read in windows of about 3 ms at `ε = 0.2`, `Δ ≥ 0.9` months: a month is the shortest interval that keeps planning below a fifth of the query, 0.6 ms at 14 partitions where a day would cost 16 ms at 396. The same inequality gives a day for the raw analytics events, 30 days read in windows of hours. A BRIN range of 128 pages at the measured 37 rows a page covers about 4,700 rows, so a window shorter than `4,700 / λ_a` seconds reads up to two ranges it does not need, 2 MB, which at one row a second is any window under 80 minutes; the trail is read by days and months, and the default of 128 stands.

## Relation to the settle and the roles

A settle writes derived state, not facts, so it writes no audit rows: the fact is the write that raised the area, already recorded. The roles that can write the trail are the roles that can write facts; the reader role, whose only side effect is the settle, cannot write it at all.
