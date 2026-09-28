---
layout: doc
title: Reads
description: The claimius read surface, what each read does, what it costs in index probes and time, and the rule that a read writes nothing but the settle of the areas it targets.
tags: get_access, get_direct_access, mutation_access, resolve_access, get_objects, get_users, get_claims, read purity, plan cache
lastUpdated: true
---

# Reads

Every read takes the user and the app first, settles the areas it targets, checks that the user is active, and answers from `user_object`. Soft deleted rows, expired grants and denies never reach the caller.

## The surface

| Function | Answers | Cost |
|---|---|---|
| `get_access(user, app, object_id, object_type, required)` | The strongest grant on one object, at most one row, with `user_claim_id` | One primary key probe on `user_object`, the row's grants resolved against the reader's `C` grants |
| `get_direct_access(user, app, object_id, object_type, required, raise)` | The closest, strongest grant on any ancestor | `3(D + 1)` index only closure rows joined to `claim_object` and the user's grants |
| `resolve_access(user, app, object_id text, object_type, required)` | The verdict for any type, text id | Dispatch to the two above, or to the audit, relation and user lookups |
| `mutation_access(user, object_id text, object_type, required)` | The gate: a row, `P0403` or `P0404` | `resolve_access` plus one existence probe on a miss |
| `get_user_access(actor, app, target, required)` | The actor's access on a user named three ways | One resolution query over `samna_user`, then the grant branches |
| `get_objects(user, app, object_type, required)` | Every reachable object of a type, with rendering fields and `user_claim_id` | One range of the `user_object` primary key, `V` rows, joined to `object_info` |
| `get_organizations`, `get_locations`, `get_users`, `get_claims`, `get_secrets`, `get_audit` | Listings by type | One index range each |
| `search`, `get_owner_graph`, `get_claim_graph` | See [Search](./search.md) and [Graphs](./graphs.md) | |

## What a check costs

A check is one SQL statement, and resolves in the order the rows are indexed.

1. The reader's grants, one range on `idx_user_claim_user_app` and `C` claim probes, read once and used twice: for the area ids and for the grants of the row.

2. The ancestors of the object, one covering range on `idx_inheritance_info_descendant`, `D + 1` rows.

3. The outstanding areas, one `= ANY` probe of the `access_diff` primary key with the `C + D + 1` area ids computed by `composite_id`. In the common case it finds nothing and the statement goes on; when it finds an area the entry point takes the settling branch of [Materialization](./materialization.md) first.

4. `check_user_active`, one probe of `idx_samna_user_user_app`.

5. `user_object` probed by its primary key `(user_id, app_id, object_type, object_id)`: one descent, one heap row, measured 0.066 ms and 3 buffers.

6. The live grants of the row resolved against the reader's grants from step 1, and the best grant chosen: fewest hops, most bits, oldest claim.

That is `2C + D + 5` descents, `2C + 2` heap rows and one statement: `t_s + (2C + D + 5) × t_d`, 0.11 ms at `C = 5`, `D = 4`, from the constants of [Scaling](./scaling.md). It does not depend on the number of users, objects or the size of the trees.

Why one statement: a statement costs 60 µs from a client and 20 µs inside a function, a descent 2.5 µs. The same check written as eight PL/pgSQL statements measured 0.30 to 0.35 ms on the server, 0.39 to 0.44 ms with the round trip, and the model gives 0.27 ms; the statements, not the probes, were the cost. The settling branch is PL/pgSQL because it writes, and it runs only when step 3 finds an area.

## What a listing costs

A listing reads the reader's range of the `user_object` primary key for one type, tests the mask on the included `sa_access`, resolves each row's best grant against the reader's grants, fetched once and hashed, and takes the rendering fields from `object_info` by primary key. Measured on the pair carrying its own fields: 288 rows in 1.18 ms, 20,000 rows in 14.4 ms, about 0.7 µs per row, of which the sort by mask and id is the largest term above a few thousand rows. A caller that needs only ids, the `ObjectWhere` filter an implementing side puts on its own table, reads the primary key alone, index only: `⌈V / 87⌉` leaf pages, 230 for 20,000 rows, where reading the heap row of each took 15,778 page reads.

## A read writes only its settle

Every `get_*` is a read with one side effect: the settle of the reader's own slice of the `access_diff` areas it targets, described in [Materialization](./materialization.md). Nothing else in a read path writes.

1. No cascade runs in a read. The soft delete of a user cascades to its grants, relations and field values through `tg_cascade_user_soft_delete`, once, in the write that deleted the user. `check_user_active` is a predicate.

2. The settle locks only the reader's marker and the reader's own `user_object` rows it upserts, for the duration of one short statement. Two readers of one area settle their own slices without meeting; two requests of the same reader coalesce on the marker.

3. Every statement runs under `statement_timeout`, 120 s, and every session under `idle_in_transaction_session_timeout`, 60 s, on every server, paired with the Go driver's read timeout. A backend whose client has gone cannot run on.

4. The pure part of each read, the part after the settle, is written as `LANGUAGE sql STABLE` helpers such as `_best_grant`, which the planner inlines into the calling query ([PostgreSQL wiki, Inlining of SQL functions](https://wiki.postgresql.org/wiki/Inlining_of_SQL_functions)). A function that settles cannot be `STABLE`, because `STABLE` promises no database modification ([PostgreSQL, CREATE FUNCTION](https://www.postgresql.org/docs/17/sql-createfunction.html)); the entry points that call `reconcile_if_pending` are `VOLATILE` and say so.

### Why

The rule that a read does not write came from one incident, on 2026-06-11, on the dev server of Bookable. `check_user_active` ran at the top of every `get_*` and, for a soft deleted actor, called `cascade_user_soft_delete`, three `UPDATE` statements on every read. The actor was the soft deleted synthetic debug user. One `/api/me` held its locks for more than 21 minutes, and a `get_user_access` backend kept running for more than 50 minutes after its Go client disconnected at the 120 s read timeout, because the server had no `statement_timeout`. The dev server ran on a Burstable instance out of CPU credits, with five second tickers opening transactions, and replicas crashed on connection exhaustion. The user's words that afternoon: "one /api/me request alone drives the server to 62 %", then "and it is not timing out either!?", then "cancel 1901 and set the three timeouts".

The fixes were the trigger for the cascade, the server timeouts, the connection pool shape, and slower tickers. What the incident proves is narrower than "a read must not write": a read must not run an unbounded write it did not need, and no statement may outlive its client. The settle is bounded to the areas the read targets, set based, coalesced, and capped by the same timeouts.

## Plans

The read functions are called with the same argument shapes millions of times. PL/pgSQL caches a plan per statement and, under `plan_cache_mode = auto`, switches to a generic plan after five executions when its estimated cost is close to the custom plans ([PostgreSQL, PREPARE](https://www.postgresql.org/docs/17/sql-prepare.html)). Every hot statement in a read filters on an equality over indexed columns, so its generic plan is the same index path as its custom plans and the switch is free. The listings, whose row counts vary from one user to the next by orders of magnitude, filter on the reader's own index range, so the plan does not depend on the count either.

The client side matches it: the middleware sends each access statement as a named prepared statement, so a call pays bind and execute, not parse and plan, about 25 µs where an unnamed statement pays 60 µs.

## Where a read runs

A read whose area probe finds nothing writes nothing, and runs on a hot standby as well as on the primary ([Hot Standby](https://www.postgresql.org/docs/17/hot-standby.html)). A read that finds an area settles, which a standby refuses with SQLSTATE `25006`, and the middleware repeats that one call on the primary. The standby answers as of its replay position, the same consistency a disciple has against its prophet, and a reader that must see a write it just made reads the primary. With `R` standbys taking reads, one service's database ceiling is `(1 + R) × 0.7 k / c_r` for `k` cores, where one primary alone saturates at 5.5 API cores per 2 vCores; see [Scaling](./scaling.md).
