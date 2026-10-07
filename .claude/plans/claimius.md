# v2.0.0: complete claimius without a bypass, with exact reads and the routed paths a disciple needs

This plan completes `v2.0.0` after `v2.0.0-alpha0024`. It carries the case, the facts verified against the code, the design with the counts that decide it, the work in order and the tests. The design is the one in `.nimpress/claimius/`; this file says what changes and why. Every change is made in place in the existing functions of both role sets.

## 1. The case

1. No bypass. A settle that fails for any reason other than a lock it chose not to wait for must raise. At alpha0024 the check settle, the batch settle and the queue settle swallowed every error, so a broken settle read as stale pairs and nobody saw it.

2. The first listing after a broad change reads about 98 index entries per pair it settles, and every later listing keeps testing the areas a writer that never reads left open.

3. Who sees whom is counted in `user_users`, folded from deltas, and `get_user_access` read the viewer's whole visible set to answer one target; bookable calls it once per row of a user listing.

4. The docs contradicted themselves and the code: `materialization.md:50` against its own table, `comparison.md:43` on an eager count that no longer exists.

5. From the alpha0024 end to end run, samna_auth as prophet with bookable_test and nimployer-dev as disciples: a binding on a disciple's own table could not be stored on the prophet (E), a snapshot failed on a claim rooted at an ancestor the app is not linked to (F), a snapshot that failed was pulled every interval with no backoff (G), and `get_graph_links_owner` was quadratic in the user's objects (H).

## 2. Facts at alpha0024

### Bypasses

6. `reconcile_if_pending` wrapped the check settle in `EXCEPTION WHEN OTHERS THEN NULL`: prophet `V2.3__claimius_functions_access.sql:1078`, disciple `:830`. The `reconcile_queue` loop did the same, prophet `:1136`, disciple `:885`.

7. `_settle_diff_share` caught `WHEN OTHERS` and retried one area at a time, a single area returning 0: prophet `V2.2__claimius_functions_internal.sql:2532`, disciple `:2327`.

8. `_affected_users_for_object` caught `WHEN OTHERS` around the creator read, prophet `V2.2:1641`, disciple `:1468`. `_actor_user_for_row` returned NULL on `WHEN OTHERS`, prophet `V2.2:2106`, disciple `:1930`. `in_replay_mode` caught `WHEN OTHERS` around a `current_setting(..., TRUE)` that cannot raise, `V2.1:113` and `:112`.

9. `calc_object_access` and `calc_hierarchical_access` caught `WHEN OTHERS`, wrote an audit row and raised, prophet `V2.3:60` and `:315`, disciple `:51` and `:294`. The audit row rolls back with the raise, so it never persisted, and the handler opened a subtransaction for every row a statement wrote. `upsert_table_info`, `recompute_state` and `_restamp_created_by` restored `replay_mode` in a handler and raised, prophet `V2.4:2490`, `:2583`, `:2671`, disciple `:2298`, `:2391`, `:2466`; the raise rolls the setting back by itself.

10. Kept as the design: `_settle_user_users_diff` and `_settle_usage_diff` catch `lock_not_available OR deadlock_detected` and leave their rows for the next fold; the drain and the choice of areas catch `lock_not_available`. The baseline's `duplicate_object`, `duplicate_table` and `duplicate_column` handlers guard idempotent DDL.

### The settle

11. Measured at alpha0024 on one prophet, the bench of section 6, each figure an index delta inside one transaction:

| Case | Time | Index descents | Index entries |
|---|---|---|---|
| First listing, one claim on a root over 16,000 new child organizations | 3,529 ms | 377,028 | 1,577,903 |
| Second listing, nothing changed since | 662 ms | 80,034 | 96,082 |
| First listing after a grant on a node over 10,000 organizations | 1,020 ms | 101,120 | 201,374 |
| `get_users`, 200 users sharing 2,000 organizations | 243 ms | 3,841 | 1,761,396 |
| `get_user_access` per row of those 200 users, bookable's listing | 17,258 ms | 3,488 | 437,835,068 |
| Mixed: 4 writers inserting organizations, 8 readers listing, 20 s | writers 15.8 ms average, 164 ms max; readers 539 ms average, 1,955 ms p99 | | |

12. Per pair, `recompute_user_object` reads the row through `_denormalize_objects` (row, owner organization, location), the creator grant, the direct bindings, the ancestor range with `OFFSET 0` forcing one walk per object (`V2.2:983`), one binding probe per ancestor, and the ancestor range and binding probes again for the composed branch (`V2.2:1035-1077`): `6 + 2a + 2t + 4B` entries per object, against the `6 + 4a + 2t + 4B` printed at `materialization.md:50`.

13. Per area and reader the settle writes an `access_diff_user` row, takes `FOR KEY SHARE` on the area, and runs `_clear_diff` over `_diff_holders`, which enumerates every holder of the area. An area raised by a writer that never reads never clears, and every later read of every user tests it again: the 80,034 descents of the second listing in item 11.

14. `get_user_access` resolved the target through an `OR` over the app's users with no index on `external_id`, settled the actor's share, folded its `user_users_diff` rows and read `_user_users_for_viewer`, the viewer's whole visible set, to answer one target (`V2.4:411-516`).

### E to H

15. E. `_check_claim_composition` judged a binding by the actor's prophet `user_object` pair on the bound object (`V2.3:465-472`, refusal `:510`). The prophet holds no pair and no `object_info` row for a disciple's table, so even the creator's owner claim on `public.bookable_type` was refused with `P0403`. Bookable binds creators with a local `claimius.insert_claim_object` in 10 handlers, which lands on the disciple only.

16. F. The disciple snapshot ships the organizations in `app_object_ids` (`prophet/echo/sync.go:219`), while the disciple's `claim.sa_root_id` and `claim_object.sa_root_id` referenced `claimius.organization` (`disciple/database/V1.1:167`, `:231`). A claim owned by a linked organization under an unlinked root violated `claim_sa_root_id_fkey`. The disciple's `organization` table carries no foreign key on `sa_owner_id` or `sa_root_id`, and `get_sync_events` states that other apps' hierarchy rows never leave the prophet.

17. G. `disciple/sam/main.go:220-223` reset the backoff and the failure count on `ErrSnapshotRequired` before the snapshot applied, and the forced snapshot after `failedPullsBeforeReconcile` failures did the same.

18. H. `get_graph_links_owner` (prophet `V2.4:3619`, disciple `V2.4:2775`) excluded deeper edges with `NOT EXISTS` over the `direct_edges` CTE, planned as a nested loop anti join over a CTE estimated at one row against 99,066: 1.1·10⁹ comparisons and 80 s a call. Its `p_start_id` filter matched `ancestor_id` without `ancestor_type`, which no index serves. `_window_graph` tested a parent and `_window_links` a source with a correlated `EXISTS` over the rows of the same expression.

## 3. The design

### A. No bypass

19. Every `WHEN OTHERS` handler of items 6 to 9 goes. A settle catches only `lock_not_available` and `deadlock_detected`, and only around a unit whose loss the next read repairs: the check's one pair, a batch of a listing's areas, then each of its areas, a queue row, a fold. Everything else raises to the caller. Removing the trigger handlers also removes a subtransaction per row written.

### B. The first listing: not built in v2.0.0, its cost

20. Built in this change: nothing of B. The design below is the measured cost of the alternative, kept here so the decision is made on numbers.

21. Settle by cursor. Each area keeps `raised_txid`, the raising transaction; each reader keeps one cursor per app, the snapshot xmin it last settled at, the `(txid, seq)` rule of the sync feed. A listing reads its cursor and the areas at or above it in one snapshot and settles all of them; `access_diff_user`, `_diff_holders` and `_clear_diff` go, a reader writes one cursor row per listing instead of one row per area, and an area is read once per reader that reads after it. A user's cursor starts at the transaction that creates its `samna_user` row, which is exact because a user reaches nothing before it exists.

22. Recompute from the reader's bindings. `object_info` gains `sa_created_by`, so a pair reads one `object_info` row instead of the row, its owner and its location, and its creator is a lookup in the reader's grant ids; one ancestor range per object serves the direct, tree and composed branches; the bindings on those nodes come from the reader's own bindings, read once and hashed when they are no more than the objects, else one probe per node.

23. The counts it would reach, per object of a share with `a_y + t_y` closure rows:

```
alpha0024   R_y = 6 + 2a_y + 2t_y + 4B_y + 2, a filter of 3 and per area its holders     about 98 entries measured
cursor      R_y = 3 + a_y + t_y                                                       5 entries for a = t = 1
second listing after 16,000 inserts: 80,034 descents at alpha0024, 2 with a cursor
```

24. What it costs to build: a cursor table and a column on `access_diff`, `object_info.sa_created_by` with its backfill and the `samna_app` rows `object_info` lacks, a new body for `recompute_user_object`, `_build_grants_for_objects`, `_settle_diff_share`, `reconcile_if_pending` and `drain_access_diff` in both roles, `access_diff_user` and `reconcile_queue` left unused until a base set drops them, and the suites `drain`, `lock_budget`, `settle_granularity`, `settle_yield`, `listing_writer` and `superseded_functions` rewritten in both roles. About 1,500 changed lines of SQL and tests.

### C. user_users stays stored, the check reads it by key

25. Stored against computed, per user over one turnover of its reach `V_u`, with `U_x` users per object: the fold reads `V_u U_x` entries, the same `Σ_x U_x²` a computed `get_users` reads for every user once. Stored wins when a user lists users or checks another user at least once per turnover: `get_users` reads `S_u ≤ U` entries against `V_u U_x = 2.5·10⁵`, and a check reads 1 row against a semi join of up to `min(V_u, V_v)` pairs. Bookable calls `get_users` per user listing and `get_user_access` per row, so `user_users` stays, at `Σ_u S_u ≤ U (U − 1)` rows.

26. `get_user_access` reads the one `(actor, target)` row by key and the pending rows of the actor and of the target, `m_a + m_t` entries and two pair probes per object they name, the only objects that can move that one count, and folds nothing. The target resolves by three keyed probes: `external_id` over a new partial index `(app_id, external_id)`, `user_id`, `id`.

27. The count is exact over the pairs as settled. A holder that has not read since a change counts as of its last settle until it reads or the drain settles it.

### D. Docs

28. The formula and the statements A, C and E to H change are corrected in `materialization.md`, `reads.md`, `users.md`, `claims.md`, `writes.md`, `graphs.md`, `sync.md`, `comparison.md` and `middleware/`, with the measured rows of section 6.

### E. A binding on a disciple's own table

29. Claimius never resolves a type outside its own schema. A binding on a `claimius.*` object is judged by the actor's pair on the object, as before. A binding on any other object that this database holds no `object_info` row for, from an actor with no pair on it, is judged by the actor's pair on the binding's owning organization, which `_check_app_scope` already requires to be linked to the app. No type check, registry lookup or `table_info` requirement.

30. Consumers stop calling `claimius.insert_claim_object` locally and bind through `NewInsert().Model(&ClaimObject{...}).Access(ac)`, which routes to the prophet. An actor that created the row but holds less than owner on its organization is refused owner on it by the prophet; the disciple is the only party that knows the creator.

### F. Claims rooted outside the app

31. The disciple holds only the organizations linked to its app; a root outside it is an opaque id, as `organization.sa_root_id` already is. `claim.sa_root_id` and `claim_object.sa_root_id` lose their foreign key on the disciple. Shipping the ancestor chain would send other apps' hierarchy rows, which the feed promises never to do; linking ancestors would let the app bind on them.

### G. Snapshot backoff

32. A snapshot that fails to apply backs off like any failed pull; the backoff and the failure count reset only after a snapshot or a pull applies.

### H. Graph links

33. `get_graph_links_owner` keeps, per target and tree, the reached ancestors at the least depth, every depth one edge and otherwise the closest, by a window over one join of the closure to the reader's pair probes; no anti join. `p_start_id` matches `ancestor_type = ANY(registered types)`. `_window_graph` and `_window_links` test against an uncorrelated set, which PostgreSQL hashes once.

## 4. The work, in order

34. This plan and the failing tests of section 5.

35. A in both roles.

36. C: `get_user_access` and the `external_id` index in both roles.

37. E, F, G, H.

38. The docs of D, the changelog rows in `.nimpress/changelog/v2.0.0.md`, the bench of section 6 again.

39. `just test`, `just verify`, `just test-hurl`. No deploy.

## 5. The tests

40. pgTAP per role under `test/sql/<role>/`:
    1. `no_bypass.sql`: a settle forced to fail by a registered table whose `name` column is renamed raises `42703` from `get_access` and from `get_objects`, and the same reads settle once the table is whole.
    2. `user_access_point.sql`: `get_user_access` sees a user sharing objects through rows still pending, resolved by `external_id`, folds nothing, sees no user sharing nothing, and agrees with `get_users`.
    3. `graph_links_scale.sql`: over 3,660 organizations in three levels with one hidden, every organization links to its closest reached ancestor, the children of the hidden one to the root, in fewer than 20 index entries each.
    4. Prophet `consumer_binding.sql`: an owner of the binding's organization binds owner on a row only a disciple holds, a reader is refused with `P0403`, the type is stored untouched, and a binding on a `claimius.*` object is still judged on the object.
    5. Disciple `claim_root_outside.sql`: a claim and a binding rooted at an organization outside the app apply.

41. Go `dbtest`: `prophet/echo/routed_binding_db_test.go` takes a snapshot with claims rooted at an unlinked root and binds through the routed path from a real disciple, the owner accepted and the reader refused with `ErrNoAccess`; `disciple/sam/sync_db_test.go` holds the pull loop to at most three snapshots in 3.5 s while the snapshot fails.

## 6. The bench

42. One kept prophet from `smig test --keep`, helpers in a `bench` schema, each figure an index delta from `pg_stat_get_xact_numscans` and `pg_stat_get_xact_tuples_returned` read twice inside one transaction: the 16,000 children, the 10,000 share after a grant, 200 users over 2,000 organizations for `get_users` and per row `get_user_access`, and `pgbench` with 4 writer and 8 reader clients for 20 seconds. Run at alpha0024 for section 2 and on the result for the changelog.
