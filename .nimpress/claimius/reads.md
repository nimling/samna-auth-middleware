---
layout: doc
title: Reads
description: The claimius read surface, what each read does, what it costs in index probes and time, the check evaluated from the sources of access, and the rule that a listing writes nothing but the settle of the areas it targets.
tags: get_access, get_direct_access, mutation_access, resolve_access, _build_grants_for_object, get_objects, get_users, get_claims, read purity, plan cache
lastUpdated: true
---

# Reads

Every read takes the user and the app first and checks that the user is active. A check answers from the sources of access, evaluated in one statement; a listing settles the reader's slice of the areas it targets and answers from `user_object`. Soft deleted rows, expired grants and denies never reach the caller.

## The surface

| Function | Answers | Cost |
|---|---|---|
| `get_access(user, app, object_id, object_type, required)` | The strongest grant on one object, at most one row, with `user_claim_id` | One evaluation of `_build_grants_for_object` and `_best_grant`, below |
| `get_direct_access(user, app, object_id, object_type, required, raise)` | The closest, strongest grant on any ancestor | The same evaluation, its first row by hops |
| `resolve_access(user, app, object_id text, object_type, required)` | The verdict for any type, text id | Dispatch to the two above, or to the audit, relation and user lookups |
| `mutation_access(user, object_id text, object_type, required)` | The gate: a row, `P0403` or `P0404` | `resolve_access` plus one existence probe on a miss |
| `get_user_access(actor, app, target, required)` | The actor's access on a user named three ways | One resolution query over `samna_user`, then the grant branches |
| `get_objects(user, app, object_type, required)` | Every reachable object of a type, with rendering fields and `user_claim_id` | One range of the `user_object` primary key, `V` rows, joined to `object_info` |
| `get_organizations`, `get_locations`, `get_users`, `get_claims`, `get_secrets`, `get_audit` | Listings by type | One index range each |
| `search`, `get_owner_graph`, `get_claim_graph` | See [Search](./search.md) and [Graphs](./graphs.md) | |

## What a check costs

A check is one SQL statement, `STABLE`, evaluated from the sources of access: `_build_grants_for_object` returns the derivations of the pair and `_best_grant` picks the answer. It reads, in the order the rows are indexed:

1. The reader's grants, one range on `idx_user_claim_user_app` and `C` claim probes.

2. The links out of those claims, `C` probes of `idx_claim_object_lookup`, and the `L` claims they compose.

3. The object's `object_info` row by primary key: one descent that proves the object exists, gives its creator, and gives its up set in `owner_ids`, `location_ids` and `parent_ids`, `a` ancestors; see [Objects](./objects.md#the-up-set).

4. The bindings on the object and on each ancestor, `a + 1` probes of `idx_claim_object_object`, kept when their claim is one of the reader's or composed by one of them. When an ancestor carries many bindings of claims the reader does not hold, the planner probes `idx_claim_object_lookup` with the reader's claims instead.

5. `check_user_active`, one probe of `idx_samna_user_user_app`.

6. The derivations combined by the formula of [Denials](./denials.md), the scope merged by [Redaction](./redaction.md), and the best grant chosen: fewest hops, most bits, oldest claim.

That is `2C + L + a + 4` descents and one statement: `t_s + (2C + L + a + 4) × t_d`, 0.11 ms at `C = 5`, `L = 0`, `a = 4`, from the constants of [Scaling](./scaling.md). It does not depend on the number of users, objects or the size of the trees, it never waits on a settle, and it is current the moment a grant, a binding or a move commits.

Why the check is evaluated and not read from `user_object`: a pair maintained on demand must be proved current before it is trusted, and the proof reads the same grants and ancestors and adds one probe per claim, `3C + D + 5` descents against `2C + L + a + 4`, and a settle when it finds an area. The evaluation is cheaper, writes nothing and has no staleness. The settle writes `user_object` from the same `_build_grants_for_object`, so a listing and a check agree by construction. The proof is Result 4 of [Theory](./theory.md).

Why one statement: a statement costs 60 µs from a client and 20 µs inside a function, a descent 2.5 µs. The same check written as eight PL/pgSQL statements measured 0.30 to 0.35 ms on the server, 0.39 to 0.44 ms with the round trip, and the model gives 0.27 ms; the statements, not the probes, were the cost.

Why no round trip of its own: on any network the round trip to the database costs more than the check. A check issued alone takes `r_db + c_r / σ`, of which the wire is `r_db / (r_db + c_r / σ)`: 83 % on the Azure servers, 42 % on the reference node. A service reads the object anyway, so the check rides in that query as a predicate, the `get_access` evaluation for the row's id, and the read of one object costs the service one round trip, not two. On the Azure servers that cuts the latency the check adds from 1.2 ms to 0.2 ms.

## What a listing costs

A listing reads the reader's range of the `user_object` primary key for one type, tests the mask on the included `sa_access`, resolves each row's best grant against the reader's grants, fetched once and hashed, and takes the rendering fields from `object_info` by primary key. Measured on the pair carrying its own fields: 288 rows in 1.18 ms, 20,000 rows in 14.4 ms, about 0.7 µs per row, of which the sort was the largest term above a few thousand rows. The rows are returned in the order of the primary key, `(object_type, object_id)` within the reader's range, which the index already holds, and paged by keyset on `object_id`, so the sort is gone: 0.043 cost units a row instead of 0.114, and a page of `l` rows reads `l` entries whatever page it is. A caller that needs only ids, the `ObjectWhere` filter an implementing side puts on its own table, reads the primary key alone, index only: `⌈V / 87⌉` leaf pages, 230 for 20,000 rows, where reading the heap row of each took 15,778 page reads.

## A check writes nothing, a listing writes only its settle

A check is a pure read. Every listing, `get_objects`, `get_organizations`, `get_locations`, `get_users`, `search` and the graphs, is a read with one side effect: the settle of the reader's own slice of the `access_diff` areas it targets, described in [Materialization](./materialization.md). Nothing else in a read path writes.

1. No cascade runs in a read. The soft delete of a user cascades to its grants, relations and field values through `tg_cascade_user_soft_delete`, once, in the write that deleted the user. `check_user_active` is a predicate.

2. The settle locks only the reader's marker and the reader's own `user_object` rows it upserts, for the duration of one short statement. Two readers of one area settle their own slices without meeting; two requests of the same reader coalesce on the marker.

3. Every statement runs under `statement_timeout`, 120 s, and every session under `idle_in_transaction_session_timeout`, 60 s, on every server, paired with the Go driver's read timeout. A backend whose client has gone cannot run on.

4. `get_access`, `get_direct_access`, `mutation_access`, `resolve_access` and the helpers they call, `_build_grants_for_object` and `_best_grant`, are `LANGUAGE sql STABLE`, which the planner inlines into the calling query ([PostgreSQL wiki, Inlining of SQL functions](https://wiki.postgresql.org/wiki/Inlining_of_SQL_functions)). A function that settles cannot be `STABLE`, because `STABLE` promises no database modification ([PostgreSQL, CREATE FUNCTION](https://www.postgresql.org/docs/17/sql-createfunction.html)); the listings that call `reconcile_if_pending` are `VOLATILE` and say so. The one check branch that reads a listing relation, the shared visibility branch of `get_user_access` on `user_users`, settles the reader's slice first like a listing.

### Why

The rule that a read does not write came from one incident, on 2026-06-11, on the dev server of Bookable. `check_user_active` ran at the top of every `get_*` and, for a soft deleted actor, called `cascade_user_soft_delete`, three `UPDATE` statements on every read. The actor was the soft deleted synthetic debug user. One `/api/me` held its locks for more than 21 minutes, and a `get_user_access` backend kept running for more than 50 minutes after its Go client disconnected at the 120 s read timeout, because the server had no `statement_timeout`. The dev server ran on a Burstable instance out of CPU credits, with five second tickers opening transactions, and replicas crashed on connection exhaustion. The user's words that afternoon: "one /api/me request alone drives the server to 62 %", then "and it is not timing out either!?", then "cancel 1901 and set the three timeouts".

The fixes were the trigger for the cascade, the server timeouts, the connection pool shape, and slower tickers. What the incident proves is narrower than "a read must not write": a read must not run an unbounded write it did not need, and no statement may outlive its client. The settle is bounded to the areas the read targets, set based, coalesced, and capped by the same timeouts.

## Plans

The read functions are called with the same argument shapes millions of times. PL/pgSQL caches a plan per statement and, under `plan_cache_mode = auto`, switches to a generic plan after five executions when its estimated cost is close to the custom plans ([PostgreSQL, PREPARE](https://www.postgresql.org/docs/17/sql-prepare.html)). Every hot statement in a read filters on an equality over indexed columns, so its generic plan is the same index path as its custom plans and the switch is free. The listings, whose row counts vary from one user to the next by orders of magnitude, filter on the reader's own index range, so the plan does not depend on the count either.

The client side matches it: the middleware sends each access statement as a named prepared statement, so a call pays bind and execute, not parse and plan, about 25 µs where an unnamed statement pays 60 µs.

## Where a read runs

A check writes nothing and runs on a hot standby as well as on the primary ([Hot Standby](https://www.postgresql.org/docs/17/hot-standby.html)). A listing whose area probe finds nothing runs there too; a listing that finds an area settles, which a standby refuses with SQLSTATE `25006`, and the middleware repeats that one call on the primary. The standby answers as of its replay position, the same consistency a disciple has against its prophet, and a reader that must see a write it just made reads the primary. With `R` standbys taking reads, one service's database ceiling is `(1 + R) ρ K / c_r` for a database of `K` calibration cores, where one primary alone saturates behind 2.7 API cores per database core of the same class, 5.5 vCPUs of API per `D2ds_v5`; see [Scaling](./scaling.md).

