# v2.0.0: the settle counted, compared and rebuilt so that claimius is the cheapest model on the reads that happen

This plan completes `v2.0.0` after `v2.0.0-alpha0026` (main at `63ae1e8`). It carries the case, the facts verified against the code, the competitor baselines with their sources, the math of every candidate in index entries, the chosen design with its SQL shape, the work in order, the tests, the docs to correct and the half done items. Every change is made in place in the existing functions of both role sets. Costs are index entries read and written, never hardware. The settled model stays: a write records a delta and settles only the actor, a reader settles its own share on read, a check settles one pair and reads it by key, derived structures are deltas folded on read or by the drain, closure tables stay and are kept incrementally, every answer is exact, a settle catches only `lock_not_available` and `deadlock_detected`, and a writer never waits on a reader.

## 1. The case

1. The goal of `v2.0.0` is a system that is cheaper than Zanzibar, SpiceDB, OpenFGA, Auth0 and Entra ID on the operations a workload is made of, with the reason for each figure in the docs. At alpha0026 it is not: the first listing after a broad change reads 20 to 57 entries per pair where an ideal single pass reads 2 to 3 and writes 2 to 4, a check in an app with any open area reads about 38 entries against 9 in a quiet app, every later listing of every user tests the areas a holder who never reads leaves open, and who sees whom is folded for every user whether or not anyone ever asks.

2. The per object formula is wrong in two places and the docs contradict each other: `materialization.md:50` prints `6 + 2a_x + 2t_x + 4B_x`, its own table sums to `2a`, `theory.md:73` prints `4a`, and the measured 20 entries per pair fit neither; section 4 derives the count from the index accesses the code makes.

3. Nothing was ever counted against the competitors on one data set with sources. Section 3 does that, section 4 puts claimius beside them per operation and over a stated workload mix, and the pick is the model with the lowest total, with the operations it still loses named in numbers.

4. The design that wins is the one settled on 2026-09-29 and never written: a settle keyed by a per reader cursor instead of per area bookkeeping, a share recomputed by one descent from each bound node through the closure instead of one ascent per object, pairs that carry everything a read needs so a check and a page probe nothing but the pair and its object, and who sees whom kept only for the users who look.

5. `theory.md` and `scaling.md` go. The first restates the other files, the second contradicts the code (checks from the sources, replicas serving checks) and argues in hardware. Their one correct content, the stratification argument and the cursor proof, already lives in `index.md` and `sync.md`.

## 2. Facts at alpha0026

### The settle as it runs

6. A share is recomputed per object. `_build_grants_for_objects` reads the reader's grants once per call (prophet `V2.2__claimius_functions_internal.sql:916-924`, disciple `:787-795`) and then, per object: one probe of `idx_claim_object_object` for the direct bindings (`:948-953`), one range of `idx_inheritance_info_descendant` forced per object by `OFFSET 0` (`:977-984`, commit `32f6acd`), one binding probe per ancestor (`:985-991`), and for the composed branch the same closure range again (`:1014-1019`), a binding probe on the object and every ancestor (`:1021-1026`), a claim probe per binding (`:1027-1030`) and a link probe per binding (`:1031-1036`). `recompute_user_object` adds per object the row, its organization and its location through `_denormalize_objects` (`:1111-1127`), the creator's grant (`:1233-1238`), a probe of the stored pair (`:1289-1293`), a probe for the delete (`:1267-1272`), the `object_info` upsert probe (`:1275-1282`), and writes the pair (`:1298-1316`), its `user_users_diff` row (`:1326-1335`) and one `claim_graph_cache` probe per pair from the row trigger (`V3.0__claimius_triggers.sql:289-291`). `_settle_object_diff` adds per object of an area one pair probe and one closure range (`:2226-2241`).

7. Per area and reader the listing settle writes an `access_diff_user` row and runs `_clear_diff` over `_diff_holders`, which enumerates every pair holder, binding holder and creator of every object of the batch (`:2343-2450`, `:2455-2481`, `:2529-2535`). An area raised by a writer who never reads never clears, and every later read of every user tests it again (`V2.3__claimius_functions_access.sql:1091-1127`): 80,034 descents for a listing that changes nothing after 16,000 inserts.

8. The check pays the covering test whenever the app holds any area: two existence probes, the object's closure range, the reader's grants and links, one `access_diff` probe per node and per held claim, and the `access_diff_user` probes (`V2.3:997-1080`). Nothing records that the one pair was settled, so the same pair is tested and recomputed on every check until a listing or the drain records the area (`materialization.md:98`).

9. A soft deleted node drops every closure row of its subtree, self edges included (`_splice_subtree` with no parent, `V2.2:259-268`, called from `calc_hierarchical_access` `V2.3:251-252`), and raises one cascade area on the node. The share of that area is the node plus its descendants in the closure, which are gone (`:2174-2188`), so the pairs other users hold on the descendants are never recomputed and stay readable until each descendant is written again.

10. The bypasses are gone: a settle catches only `lock_not_available` and `deadlock_detected` (`V2.2:1491`, `:1610`, `:2537`; `V2.3:1086`, `:1128`, `:1144`, `:1198`).

### The reads

11. `get_access` reads the pair, its `object_info` row, one `user_claim` and one `claim` probe per derivation in `_best_grant` (`V2.2:699-732`), the root organization by subselect (`V2.4__claimius_functions_external.sql:49`) and `check_user_active`: `7 + 2G_x` in a quiet app, as `reads.md:44` says. `get_objects` reads per row the pair, the heap tuple for `grants`, `object_info`, the root subselect and `2G` probes (`V2.4:294-321`).

12. `get_organizations` and `get_locations` call `reconcile_if_pending` and then `get_objects`, which calls it again (`V2.4:337`, `:345`; `:365`, `:373`), and apply `app_has_object` after `get_objects` has applied `LIMIT` (`:345-349`, `:373-377`), so a prophet page comes back short. The disciple functions carry no `app_has_object`.

13. `mutation_access` runs `reconcile_if_pending` through `resolve_access`, `get_access` and `get_direct_access` with `p_raise`, three times for a miss (`V2.4:737-761`), and `get_direct_access` walks the ancestors on every miss whether or not the settle just ran (`:121-220`).

14. `V2.4:1` and `V2.4:3` of the prophet set carry the same `DROP FUNCTION IF EXISTS claimius.get_access(UUID, UUID, UUID, TEXT, INTEGER)`. The disciple lines differ and are both needed.

### Who sees whom

15. `user_users` is stored and counted, folded from `user_users_diff` by `_settle_user_users_diff` (`V2.2:1339-1497`, commits `c7c1deb`, `d6fcef3`). A pair entering or leaving on an object with `U_x` holders moves `2(U_x − 1)` rows of `user_users`, each a probe and a row version, in whichever fold takes it; the drain folds the oldest objects of every app (`V2.3:1204`), so the count is paid for every user whether or not any user of the app ever lists users. `get_user_access` reads the one row by key (`V2.4:474-492`), after a typeless `reconcile_if_pending` of the actor (`:421`).

### Docs

16. `materialization.md:50` prints `R_x = 6 + 2a_x + 2t_x + 4B_x` against its own table; `theory.md:73` prints `6 + 4a_x + 2t_x + 4B_x`; `reads.md:93` repeats the first. `comparison.md:43` and `:154` give 87 entries for the first check from that formula. `scaling.md:160` says a check evaluates from the sources and `:113` that standbys serve checks, neither of which the code does. Every figure in `comparison.md` is a formula of these files against the competitors' published models, none measured against them.

### Half done

17. `_apply_denies` runs as a PL/pgSQL call per object in the final select of `_build_grants_for_objects` (`V2.2:1075-1078`), including the objects with no deny and no scope that its first branch returns unchanged (`:800-812`).

18. `test/sql/<role>/lock_budget.sql` and `lock_upgrade.sql` test that a listing settles every area in one read and that two folds keep `user_users` exact; the lock budget and the lock upgrade they are named after left in `d6fcef3`.

19. samna_auth runs `drain_access_diff` on a ticker in every process (`samna_auth/internal/service/access.go:16-31`), where the disciple runs it from the one process holding `LeadSync` (`disciple/sam/main.go:65`, `:248-252`).

20. bookable binds a row's creator with a local `claimius.insert_claim_object` in ten handlers (`bookable_server_test/internal/handler/{event,plan,timeslot,bookable,action,code,capability,booking,calendar,bookable_type}.go`), which lands on the disciple only; nimployer's `PostAsset` (`nimployer/internal/handler/asset.go:331`) writes no binding. Since alpha0025 the routed binding on a consumer's own row is accepted by the prophet, judged on the actor's hold on the binding's organization (`V2.3 _check_claim_composition`, `writes.md:97`).

## 3. The competitor baselines

### The data set and the workload

21. One data set for every figure, the one `comparison.md` already uses, with the symbols of `index.md`:

| Symbol | Value | Meaning |
|---|---|---|
| `U`, `O` | `10⁴`, `10⁶` | users, objects |
| `D` | 4 | tree depth, so `a_x = 4`, `t_x = 1` |
| `C`, `n_u` | 5, 25 | grants per user, bindings of those claims |
| `B_x` | 5 | bindings on an object and its ancestors |
| `G_x` | 1 | derivations per pair |
| `V_u`, `U_x` | `5·10³`, 50 | reach of a user, holders of an object |
| `S_u` | 500 | users one user shares an object with |
| `|T|`, `|H|` | `10⁴`, 100 | reach and holders of one broad binding |
| `p` | 50 | rows of a page |
| `n_new` | 6 | areas raised in the app between two reads of one user |
| `Δ_a` | `10³` | areas left open at alpha0026 by holders who never read |

22. The workload, per `10⁶` requests, with the weights assumed: `7·10⁵` checks (one per request), `2.5·10⁵` listing pages (one per page view), `10⁴` directory views each one `get_users` and twenty `get_user_access` rows, `2·10⁴` created objects, `2·10³` grants, revokes and bindings, 10 broad changes each a binding over `|T|` objects held by `|H|` users of whom half list once and each checks 20 pairs, and every write synced to two consumers. A second total is given at 1 broad change, since a binding over `10⁴` objects is a weekly event in a workplace platform and `10⁶` requests is a few days.

### Zanzibar

23. Model: relation tuples `object#relation@user` or `@object#relation`, userset rewrites per relation (`this`, `computed_userset`, `tuple_to_userset`), stored in Spanner, read at a snapshot named by a zookie, checks cached, group membership precomputed by Leopard ([Pang et al. 2019](https://www.usenix.org/system/files/atc19-pang.pdf), sections 2, 3.1, 3.2.4). A folder tree of depth `D` is `viewer = this ∪ tuple_to_userset(parent, viewer)`.

24. Counts, one tuple read as one entry, uncached:

| Operation | Zanzibar | Assumption |
|---|---|---|
| Check | `2D + 1 = 9` reads and 1 round trip | at each of `D + 1` nodes one read of the user's direct membership and one of the parent tuple; a grant through a group adds one read per level |
| Grant, binding, tree insert, revoke | 1 tuple, about 3 entries, replicated to every Spanner replica | a group tuple change yields "potentially tens of thousands of discrete Leopard tuple events" (3.2.4) |
| First check after a grant | 9, with a zookie at least as fresh as the write | no settle exists |
| First listing after a grant, later page | no API: `Expand` lists the members of one object, and clients build their own search index from `Watch` (2.4.6) | a client index is an eager materialization: `|H| · |T|` pairs per broad change, `U_x` per insert |
| Who sees whom | none | |
| Storage | the tuples: `U · C + O ≈ 1.05·10⁶` | "Zanzibar avoids storage denormalization" (3) |
| Sync per change | one tuple to every replica of its Spanner group, 5 by default | |

### SpiceDB and OpenFGA, Auth0 FGA

25. Model: the Zanzibar schema in Postgres, CockroachDB or MySQL with one row per tuple and indexes on both ends, checks dispatched per rewrite step and cached per revision, consistency per request ([SpiceDB, Consistency](https://authzed.com/docs/spicedb/concepts/consistency); [OpenFGA, Relationship queries](https://openfga.dev/docs/interacting/relationship-queries); [Auth0 FGA, Consistency modes](https://docs.fga.dev/writing-data/consistency-modes)). Listing is a reverse walk from the subject: `LookupResources` in SpiceDB, with a cursor and a limit ([Protecting a list endpoint](https://authzed.com/docs/spicedb/modeling/protecting-a-list-endpoint)); `ListObjects` in OpenFGA, which returns up to `listObjectsMaxResults`, 1,000 by default, with no cursor.

| Operation | SpiceDB | OpenFGA, Auth0 FGA |
|---|---|---|
| Check | `2D + 1 = 9` reads, one dispatch per step, 1 round trip | the same |
| Grant, binding, tree insert, revoke | 1 row, about 5 entries across its indexes; a revoke is a row version | the same |
| First listing after a grant | nothing to settle; a listing reads the reach: `C + n_u` subject reads and one reverse read per reached node, `≈ 1.1 V_u` for a full listing | the same, capped at 1,000 results per call |
| Later page of `p` | `C + n_u + p + D ≈ 85` with a cursor | `≈ 1.1 V_u`, capped: `1,000` per call, every call |
| Who sees whom | none: `LookupSubjects` lists the subjects of one object | none |
| Storage | the tuples, `≈ 1.05·10⁶` rows | the same |
| Sync per change | one row to every datastore replica | the same |

### Entra ID and Auth0

26. Model: roles and groups in a directory, carried in the token at issuance; no object and no per object decision ([Access token claims reference](https://learn.microsoft.com/en-us/entra/identity-platform/access-token-claims-reference#groups-overage-claim); [Token lifetime](https://learn.microsoft.com/en-us/entra/identity-platform/access-tokens#token-lifetime)). Auth0's RBAC is the same shape, permissions in the token.

| Operation | Entra ID, Auth0 |
|---|---|
| Check | 0 reads: the token's `groups` or `roles`; above 200 groups one Graph call per request. No per object answer |
| Grant, revoke | one directory write; takes effect at the next token, up to 60 to 90 minutes after a revoke |
| Tree insert, listing, first listing | none: the application stores and filters its own objects |
| Who sees whom | `memberOf` then `members` per group through Graph, `g · n` reads per view |
| Storage | groups and memberships |
| Sync | none; the token carries the claims |

## 4. The math

### Claimius as it is, derived from the index accesses of fact 6

27. Per object `x` of a share, counting a probe that finds nothing as one entry and a probe that finds rows as those rows:

```
direct bindings           1 + b_x
closure range, tree branch   a_x + t_x
binding probe per ancestor   Σ_{y ∈ anc(x)} max(1, b_y)  ≤ a_x + B_x − b_x
closure range, composed      a_x + t_x
binding probe per node       a_x + 1 + B_x
claim and link per binding   2 B_x
row, organization, location  3
creator grant, stored pair, delete probe, object_info probe   4
share test: pair probe and closure range                     1 + a_x + t_x

R_x ≤ 10 + 5 a_x + 3 t_x + 4 B_x            writes: 2 pair, 2 user_users_diff, 1 cache probe
```

At `a_x = t_x = b_x = B_x = 1`, organizations one level under a bound root, `R_x = 22`, against 20 measured per pair for the grant on a node over 10,000 organizations (201,374 entries). At the data set's `a_x = 4`, `t_x = 1`, `B_x = 5` it is 53 read and 5 written per pair, `5.8·10⁵` for `|T| = 10⁴`. `materialization.md:50` omits the `a_x` empty probes of the tree branch, the `a_x + 1` probes of the composed branch and the four per object probes of `recompute_user_object`; `theory.md:73` omits the last two.

28. Every other figure of alpha0026 used below: check `9` in a quiet app and `≈ 38` with any area open (fact 8); page `3 + p(3 + 2G) + 6 Δ_a`, the last term measured as 6 entries per open area on the second listing after 16,000 inserts; first check after a grant 87; tree insert about 70, grant 30, binding 120, revoke 30 (`writes.md:105-116` with `R` from item 27); `get_users` `6 Δ_a + 2 S_u`, `get_user_access` `6 Δ_a + 20`; the fold `U_x + 2(U_x − 1)` probes and row versions per pair change, 150 to 350; sync 11 entries per change per consumer.

### The candidates

29. A, alpha0026 as it is.

30. B, the cursor settle with a set based share. One cursor per reader and app, the snapshot `xmin` of its last share settle, replaces `access_diff_user`, `_diff_holders`, `_clear_diff` and `reconcile_queue`; a listing reads the areas raised since its cursor as one range and settles the reader's share of all of them in one pass that descends from the bound nodes through the closure; a check compares its pair's own mark to the areas raised since it; every change to a claim or a link raises object areas on the nodes the claim is bound to, so a check's covering test is a probe per node of the object's chain and nothing per held claim; `object_info` carries the creator and the owner's root, so a share reads one row per object and nothing from the implementing side's table; a pair's `grants` carry the grant id, its window, the claim name and its creation time, so `_best_grant` probes nothing; `user_users` is kept for the users who have looked at the directory and built from the pairs when one first looks.

31. C, direct pairs only. `user_object` holds the pairs of direct bindings and creators; a cascaded pair is derived at read from the closure with a descendant index ordered by descendant key so that a page merges the reader's `n_u` ranges. No areas, no settle.

32. Sub options costed inside B:
    1. `user_users` stored and counted for every user (A), stored for viewers only (B), or computed per read (`Σ_{x ∈ V_u} U_x` per `get_users`, `V_u + V_v` per `get_user_access`).
    2. A per pair mark for the check against the reader's cursor alone: with the cursor alone every check of a pair under an open area recomputes it until the reader lists; with the mark it recomputes once.
    3. Per node cascade rows, one stored row per (user, bound node) with membership resolved on read, is C with the bound nodes stored, which `user_claim ⋈ claim_object` already is; it costs what C costs.
    4. Ancestor arrays on `object_info` under a GIN index in place of the closure: the same entries per check and per descent, `n_S` row rewrites per move like the closure's `n_S (D + D')` rows, and the closure dropped, which the ruling keeps. Not taken.

### Per operation, in entries

33. The counts of B are derived in section 5 where each function is shaped and, where a row says measured, replaced by the figure measured on the release in the pgTAP suites and the bench; `n_new` is the areas raised in the reader's root since its last read.

| Operation | A, alpha0026 | B, chosen | C, direct pairs | Zanzibar | SpiceDB | OpenFGA | Entra ID |
|---|---|---|---|---|---|---|---|
| Check, nothing raised since the pair's mark | 9 | 7 on the prophet and 4 on a disciple, measured | 28 | 9 + 1 round trip | 9 + 1 round trip | 9 + 1 round trip | 0, no object |
| Check, `n_new = 6` raised since | 38 | 15 on the prophet and 10 on a disciple after an unrelated raise in the root, measured, one heap only write | 28 | 9 | 9 | 9 | 0 |
| First check after a grant on the pair | 87, again on every check until a listing | the one pair recomputed once, measured in `check_mark` | 28 | 9 | 9 | 9 | a new token |
| Page of `p = 50` ids, nothing raised | `253 + 6 Δ_a = 6,253` | 101 with `object_info`, measured in the bench | `n_u + p + D = 79` | no listing | 85 | 1,000, the cap, every call | none |
| Page of 50, `n_new = 6` | 6,253 | `55 + 7 n_new = 97` | 79 | | 85 | 1,000 | |
| Page of 50 with name, owner and root | `+ 0` | included above: `2p + 1` | `+ p` | | `+ p` application rows | `+ p` | |
| First listing after a grant over `|T| = 10⁴` | `2.0·10⁵` measured, `5.8·10⁵` at depth 4 | 40,035 read, `4|T|` with the page, `2|T|` written, measured in the bench; 64,048 over 16,000 | `1.1 (V_u + |T|) = 1.65·10⁴` on that listing and every later full listing | the client's index | 85 per page, `1.1 V_u` unpaged | 1,000 | none |
| Grant | 30 | 31 | 7 | 3 | 5 | 5 | 1 write |
| Binding on a node | 120 | 90 | 36 | 3 | 5 | 5 | |
| Revoke | 30, then the holder's next listing settles `|T|` | 31, then `4|T|` at the holder's next listing, 64,038 over 16,000 measured | 7 | 3 | 5 | 5 | up to 90 minutes |
| Tree insert at depth 4 | 70 | 51 | 23 | 3 | 5 | 5 | none |
| `get_users` | `6 Δ_a + 2 S_u = 7,000` | 6,212 and the build of the viewer's rows once, then 7, measured in the bench | `Σ_{x ∈ V_u} (a_x + 1 + U_x) = 2.75·10⁵` | none | none | none | `g · n` Graph reads |
| `get_user_access` per row | `6 Δ_a + 20 = 6,020` | 23, of which 20 the resolution of the target, measured in the bench | `V_u + V_v = 10⁴` | none | none | none | none |
| `user_users` per pair change | 150 to 350, every app | `U_x + 1 + U_x^r ≈ 56` at a tenth of the users viewing, 0 in an app with no viewer | 0 | | | | |
| Sync per change per consumer | 11 | 13 | 6 | one tuple per replica | one row per replica | the same | none |
| Storage | `P = 5·10⁷` pairs, `Σ_u S_u`, closure `5·10⁶`, `object_info 10⁶` | the same with `Σ_{u viewing} S_u` | closure and `object_info` only | `1.05·10⁶` tuples | `1.05·10⁶` rows | the same | groups |

The measured check reads 7 where the derivation said 4: the extra entries are the cursor and trim probes the mark rule needs, the trimmed position and the two area ranges of the root and of the reader's assignments, which are what make a stale mark safe.

34. Where each figure of B comes from: a check reads the pair, one range of `idx_access_diff_root_txid` from the pair's mark, the object's `object_info` row and `samna_user`; when the range finds areas it adds the object's closure range and either the areas themselves, when fewer than `2a_x + 3`, or one `access_diff` probe per node and one for the reader's assignments; a page reads one index entry per row from the reader's primary key range and one `object_info` row when rendering; a first listing reads the cursor, the areas, the bindings of the raised claim, the node's ancestors, one closure entry per object of the subtree, one `object_info` row per object and the reader's own range once for the comparison, and writes two index entries per pair; a revoke's settle reads the same share and deletes the pairs. Zanzibar and the stores pay nothing on a change and the full walk on every read; claimius pays the walk once per holder who reads and then two entries per row.

### The totals over the workload

35. Entries in millions per `10⁶` requests at the weights of item 22, the fold at a tenth of the users viewing the directory:

| Term | A | B | C | SpiceDB | OpenFGA |
|---|---|---|---|---|---|
| Checks | 26.6 | 10.5, quiet 2.8 | 19.6 | 6.3 | 6.3 |
| Pages | 1,563 | 24.3, quiet 13.8 | 19.8 | 21.3 | 250 |
| First listings after the 10 broad changes | 100 | 32.5 | 0.04 | 0.04 | 0.5 |
| First checks | 1.7 | 0.8 | 0.6 | 0.2 | 0.2 |
| Created objects | 1.4 | 1.0 | 0.5 | 0.1 | 0.1 |
| Grants, bindings, revokes | 0.1 | 0.1 | 0.05 | 0.01 | 0.01 |
| Sync | 0.5 | 0.6 | 0.3 | 0.3 | 0.3 |
| Total without a directory | 1,693 | 70, at 1 broad change 41, quiet 20 | 41 | 28 and `9.5·10⁵` round trips | 257 and `9.5·10⁵` round trips |
| `get_users` | 70 | 10.1 | 2,750 | none | none |
| `get_user_access` | 1,204 | 3.8 | 2,000 | none | none |
| `user_users` folds, `6·10⁶` pair changes | 1,500 | 336 | 0 | none | none |
| Total with a directory | 4,467 | 420 | 4,791 | no such feature | no such feature |

36. What the totals say:
    1. B is the only model that carries who sees whom at a cost that grows with changes, `U_x` per pair change, and not with reads, `V_u U_x` per view. A and C pay ten times more for it; the stores and Entra do not have it.
    2. Without a directory, B reads `70·10⁶` at 10 broad changes and `41·10⁶` at 1, against SpiceDB's `28·10⁶` and C's `41·10⁶`. B's extra is the materialization: `32·10⁶` of first listings at 10 broad changes, and `9` entries a page for the areas it meets when the app is busy. In a quiet app B reads `20·10⁶`, below every other model.
    3. On the two operations that are `95 %` of the requests B beats every store when nothing was raised since the reader's last read, 4 against 9 and 55 against 85, and stays within `1.7×` and `1.15×` when something was, with no round trip where every store pays one per request. OpenFGA's uncursored listing loses by `10×` on every page.
    4. B loses, in every case, the first listing after a broad change by `|T|` entries read and `2|T|` to `4|T|` written against a store's 85 per page, the storage by `50×`, the write of a grant or a binding by `6×` to `18×` against one tuple, and the revoke's settle at the holder's next listing. Every one of these is paid on a change, once, never on a request.
    5. C wins the writes and the first listing and has no settle, and loses every check by `3×` to `7×`, every page in a quiet app, and who sees whom by `270×`. Its page figure needs a descendant index ordered by descendant key, a fourth closure index, which a store does not need either.

37. The pick is B. It has the lowest total of the three claimius models with or without a directory, beats the stores on the hot reads in a quiet root and ties them in a busy one, and keeps every guarantee of the settled model. The losses are named in item 36.4 and stay in the docs as losses.

## 5. The design

### Tables and indexes, both roles

38. `access_diff`: `txid XID8 NOT NULL DEFAULT pg_current_xact_id()`, set again on every raise; `root_id UUID`, the owner root of the node, from `object_info.sa_owner_root_id` of the node when it has one; `kind` is `object` or `assignment`; the `claim` kind, `raise_count` and the claim area index go; `idx_access_diff_root_txid (app_id, root_id, txid)` and `idx_access_diff_user (app_id, user_id, txid) WHERE kind = 'assignment'`; the object index stays. A disciple drops `app_id` as everywhere.

39. `access_diff_user` and `reconcile_queue` are dropped, with the DROP in `V1.1` and `superseded_functions.sql` asserting the four functions of fact 7 are gone.

40. `user_activity`: `settled_txid XID8 NOT NULL DEFAULT '0'`, `settled_at TIMESTAMPTZ`, `directory_at TIMESTAMPTZ`. The row is the reader's cursor, its time and whether it views the directory; `touch_object_usage` keeps writing `last_used_at`.

41. `user_object`: `settled_txid XID8 NOT NULL DEFAULT '0'`, the pair's mark, not indexed, updated heap only under `fillfactor = 90`.

42. `object_info`: `sa_created_by UUID` and `sa_owner_root_id UUID`, written by `_write_object_info` from `_denormalize_objects`, backfilled once in `V1.1` by `_write_object_info` over every registered table; `idx_object_info_created_by (sa_created_by)`. `calc_object_trees` writes the `object_info` row of `samna_app` on the prophet, so every bindable object has one.

43. `prophet_state` per app and `disciple_state`: `access_trimmed_txid XID8 NOT NULL DEFAULT '0'`.

44. `grants` elements gain `user_claim_id`, `starts_at`, `ends_at`, `claim_name`, `claim_created_at`. Every one changes only through a write that raises an area covering the pair: a grant's window through the assignment area, a claim's name through the object areas of its bindings.

45. `tg_invalidate_claim_graph_cache` on `user_object` and `user_users` becomes a statement trigger over its transition table, one delete per statement.

46. `_splice_subtree` returns the self edges it drops when it is called with no parent, and `calc_hierarchical_access` raises one object area per dropped descendant per app, so a soft deleted subtree of `n_S` nodes costs `n_S` area rows beside the `Σ rows(x)` closure rows it already deletes, and every holder's next read recomputes its pairs on them. This closes fact 9.

### What a write raises

47. `_raise_object_diff(app, type, id, cascade, root)` upserts the area by its composite id, setting `txid`, `cascade` and `root_id`. `calc_object_access` and `calc_hierarchical_access` pass the row's owner root.

48. `_raise_claim_diff(app, claim)` raises one object area per live binding of the claim on an object, with `cascade` when the binding and the claim inherit, and one on the claim object itself; `_raise_claim_diff(app, claim, user)` raises the assignment area. `calc_claim_access` calls the first for a `claim` row and for a link, on both claims of the link, the second for a `user_claim` row, and `_raise_object_diff` for a binding on an object as today. A claim bound on `n_c` objects raises `n_c + 1` rows when its mask, level, inherit flag or deletion changes; a link raises `n_A + n_B + 2`. The disciple apply and `apply_peer_event` call the same functions.

### The share settle

49. `reconcile_if_pending(app, u)` without a target, under the reader's advisory lock as today:

```
xmin     := pg_snapshot_xmin(pg_current_snapshot())
cursor   := settled_txid of user_activity, '0' when absent
trimmed  := access_trimmed_txid of the app
roots    := areas with txid >= cursor, kind = object, kept when
              (u holds a pair on the node)
           or (the node is bound by a claim u holds or links to, live or changed since settled_at)
           or (an ancestor of the node is bound with inherits by such a claim)
           or (object_info.sa_created_by of the node is one of u's grants)
         ∪ for each assignment area of u: the claim's bindings live or changed since settled_at, cascade as the binding and claim inherit or changed since
when cursor < trimmed: roots := u's bindings as above over every claim u holds or links to ∪ u's own pairs ∪ the objects of idx_object_info_created_by under u's grants
recompute_user_object(app, u, roots)
user_activity.settled_txid := xmin, settled_at := now()
```

The relevance test reads per area one `object_info` row, one pair probe and, only when both find nothing and the node is not bound, the node's closure range: 2 to 7 entries; areas whose `root_id` is not among the roots of the reader's bindings are skipped from the index range with no probe. The full resettle after a trim reads the reader's reach once, which is what any listing of the reader would read, and happens only for a reader absent longer than the trim window.

50. `recompute_user_object(app, u, roots)` is one statement over `object_info` for every root at once, all types together:

```
Y        := roots ∪ descendants of the cascading roots, one range of idx_inheritance_info_ancestor each, carrying tree_type
above    := bindings of u's claims and linked claims on the ancestors of each cascading root, one range of idx_inheritance_info_descendant per root, joined by hash
inside   := bindings of those claims on nodes of Y, by hash; each descends once, |desc(y)| entries
direct   := bindings of those claims on objects of Y, by hash
creator  := object_info.sa_created_by ∈ u's grant ids
derivations := direct, above restricted to the root's tree, inside per tree, composed through the links with comp(a, l, b, binding)
per object: no deny and no scope → bit_or in SQL; otherwise _apply_denies
live     := the object_info row exists
stored   := u's pairs on Y, one probe per object when |Y| < V_u, else u's range once
written  := pairs that differ, settled_txid = xmin; deleted := stored pairs no derivation reaches or whose object is gone
```

Per object of a share: one closure entry, one `object_info` row, one pair entry for the comparison, and the descents of bound nodes inside the subtree, `δ − 1` more, so `3 + δ` read and 2 written, 4 with the directory diff. The grants and bindings of the reader are read once per settle, `1 + 2C + C + n_u + L + n_L`.

51. For one root without cascade, the point settle, the bindings above are found by one probe per node of the chain instead of the reader's bindings when the roots are at most `C`: `a_x + 1` probes returning `B_x` rows, joined to the reader's claims and links read once. Same output, `1 + 2C + C + (a_x + t_x) + (a_x + 1) + B_x + 1 + 1` read and 4 written, 36 and 5 on the data set.

52. `_bulk_seed_user_object`, `recompute_state` and `rebuild_user_object` call `recompute_user_object` with their objects as roots without cascade, grouped per user as today.

### The point settle

53. `reconcile_if_pending(app, u, τ, x)`:

```
pair := u's pair on x, mark := pair.settled_txid, or the cursor when no pair, or '0'
root := object_info(x).sa_owner_root_id
since := areas with (app, root, txid >= mark), at most 2a_x + 3 of them, and u's assignment areas with txid >= mark
when none: return
nodes := x and its ancestors, one closure range
covered := an area of since on a node, self or cascade, or an assignment area of u, or an area on a claim object x itself
when since reached its limit: covered := one access_diff probe per node and one of idx_access_diff_user
when covered: recompute_user_object(app, u, [(τ, x, false)]) with settled_txid = xmin
else when pair exists: pair.settled_txid := xmin
```

A check in a root where nothing was raised since the pair's mark reads the pair and one empty range; the mark proves the pair current because every change that can reach the pair raises an area on one of its nodes or on the reader's assignment, by item 48 and by the support argument of `index.md`, and an area's `txid` is below the mark only when its transaction had ended before the snapshot that set the mark, the rule of `sync.md`.

### The grant on a pair

54. `_best_grant(grants, required)` keeps the derivations whose mask covers `required` and whose stored window holds at `now()`, ordered as today by bits, mask, level and `claim_created_at`, and reads nothing. `get_access`, `get_objects`, `search` and `get_claims` for one object take the token from it. The second argument pair `(user, app)` leaves the signature.

### The drain and the trim

55. `drain_access_diff(p_limit_pairs)` takes the users of `user_activity` whose cursor is below the newest area of their app, most recently used first, and for each applies the rule of `materialization.md` with `λ̂ = n / (now − settled_at)` for the `n` areas since its cursor, counted up to 1,000, and `r̂ = 1 / (now − last_used_at)`; a user taken gets its share settle under its own advisory lock. It then trims: per app, every area with `txid` below the lowest cursor among users used within 7 days is deleted and `access_trimmed_txid` set to that cursor. On the prophet it drains and trims only its own apps and apps with no consumer in contact, as today. Then the usage fold and the directory fold as today.

### Who sees whom for viewers

56. `user_users` holds rows for viewers: users with `directory_at` set. `get_users`, `get_user_access` as the actor and `get_claim_graph` set it on first use and build the user's rows from its pairs, `Σ_{x ∈ V_u} U_x` once, subtracting the pending `user_users_diff` rows on its objects so the invariant `user_users = C(P ⊖ D)` of `users.md` holds for every viewer row. `_settle_user_users_diff` folds as today and writes only rows whose viewer has `directory_at`; the statement trigger raises diff rows only while the app has a viewer used within 30 days, read from one `user_activity` probe per statement, and `prophet_state` or `disciple_state` records `directory_since`, the txid raising resumed at, so a viewer whose `directory_at` is older rebuilds. The count semantics, the fold and its proof stay.

### Reads

57. `get_access`: the point settle, `check_user_active`, the pair joined to `object_info` for owner, location and owner root, the grant from `_best_grant`. 4 entries when nothing was raised since the mark. A new parameter `p_settle BOOLEAN DEFAULT TRUE` lets `mutation_access` run the settle once.

58. `get_direct_access`: the same parameter; `mutation_access` passes `FALSE` after its own settle and walks only when the settle was skipped on a lock, which `reconcile_if_pending` reports through a boolean return. A miss after a settle that ran answers `P0403` or `P0404` from the existence probe alone.

59. `get_objects`: per row the pair's index entry, the heap tuple for `grants`, and `object_info` for name, description, link, owner, location and owner root; no root subselect. On the prophet, `claimius.organization` and `claimius.location` rows are kept only when `app_has_object` holds, inside the `WHERE` before `LIMIT`. `get_organizations` and `get_locations` call `get_objects` and nothing else, so one settle runs.

60. `get_user_access`: the actor's share settle, the target resolved by its three probes, the one `user_users` row and the pending rows of the two users, the actor's bindings on `samna_user`: `2 + 3 + 1 + m_a + m_t + 2C`.

### Sync

61. Nothing new travels. The feed carries source rows; each consumer raises its own areas under replay with its own `txid`, keeps its own cursors and marks, and folds its own directory. The disciple apply at `disciple/sam/main.go:436-458` and `apply_peer_event` call the functions of item 48.

### Go

62. `shared/access.go`: `GetAccess`, `MutationAccess` and the listing helpers unchanged in signature; `AccessClaim.Level` and `ClaimName` now come from the pair. `LeadSync` moves from `disciple/sam/main.go:113` to `shared`, with the disciple call sites following, so samna_auth can run its drain under `claimius.prophet_drain:<slug>` on one process. `ts/src/types.ts` follows no change: no wire type moves.

## 6. The work, in order

63. This plan, the `rm` of `theory.md` and `scaling.md`, and the failing tests of section 7.

64. `V1.1`, both roles: items 38 to 43, the `object_info` backfill, the drops.

65. `V2.2`, both roles: `_build_grants_for_objects` and `recompute_user_object` to roots over `object_info` with the two binding shapes, the stored grant fields and the SQL fast path for pairs without a deny or a scope; `_raise_object_diff` and `_raise_claim_diff`; `_settle_diff_share(app, user)` as the share settle of item 49; `_splice_subtree` returning dropped self edges; `_best_grant` pure; `_settle_user_users_diff` and `_raise_user_users_diff` for viewers; `_settle_object_diff`, `_settle_claim_diff`, `_diff_holders` and `_clear_diff` dropped.

66. `V2.3`, both roles: `calc_claim_access`, `calc_hierarchical_access` for the dropped subtree, `calc_object_trees` for `samna_app` on the prophet, `reconcile_if_pending` as items 49 and 53 returning whether it settled, `drain_access_diff` as item 55. `V3.0`: the statement triggers of item 45.

67. `V2.4`, both roles: `get_access`, `get_direct_access`, `mutation_access`, `get_objects`, `get_organizations`, `get_locations`, `get_user_access`, `get_users`, `get_claim_graph`, `search`, `get_claims`, `_bulk_seed_user_object`, `recompute_state`, `rebuild_user_object`; the duplicate `DROP` at prophet `V2.4:1`.

68. Go: `shared/access.go`, `shared/main.go` for `LeadSync`, `disciple/sam/main.go`.

69. The tests of section 7 green in both roles, fresh and `--from v2.0.0-alpha0026`; `just test`, `just verify`, `just test-hurl`.

70. The docs of section 8, the changelog rows in `.nimpress/changelog/v2.0.0.md`, the bench of item 76 run on the result and its figures written into `comparison.md`.

71. No deploy. The consumer items of item 86 follow the release.

## 7. The tests

72. pgTAP per role under `test/sql/<role>/`, each with the counting helper of `graph_links_scale.sql`, the delta of `pg_stat_get_xact_tuples_returned` over the claimius indexes inside one transaction:
    1. `settle_cursor.sql`: a listing after a grant on a node over 1,000 organizations settles all 1,000, moves the cursor to the snapshot `xmin`, and a second listing reads at most 3 entries; an area raised by a transaction still open at the first listing is settled by the second.
    2. `settle_share.sql`: the first listing after that grant reads at most `2|T| + V_u + 40` entries and writes `2|T|` pair entries; the same share at depth 4 with five bindings on the chain costs the same per object, which is the claim of item 50.
    3. `check_mark.sql`: a check with nothing raised since the mark reads 4 entries; after an unrelated raise in the root it reads at most `2a_x + 7` and moves the mark; after a raise on an ancestor it recomputes once and the next check reads 4.
    4. `claim_change_areas.sql`: a mask change, a level change and a link raise one object area per binding and none per holder; a holder's check under the binding is covered.
    5. `subtree_delete.sql`: soft deleting an organization with ten descendants raises ten areas and a holder's next listing no longer returns them; restoring it returns them.
    6. `trim.sql`: areas below the oldest active cursor are trimmed; a reader absent longer than the window resettles its reach and sees a revoke that happened while it was away.
    7. `directory_viewers.sql`: a user who never lists users causes no `user_users` row; its first `get_users` builds its rows equal to the self join; the fold afterwards keeps them equal under two concurrent folds, the test of today's `lock_upgrade.sql` moved by `mv` to `user_users_fold.sql`.
    8. `listing_pages.sql` extended: `get_organizations` on the prophet returns `limit` rows when the reader's range holds unlinked organizations.
    9. `grant_fields.sql`: a grant whose window closes stops `get_access` and `get_objects` with no write; a claim renamed shows its new name after the holder's next read.
    10. `superseded_functions.sql` extended with the four functions of fact 7 and the two tables of item 39.
    11. `lock_budget.sql` moved by `mv` to `listing_settles_all.sql`, its assertions kept.

73. The suites that read `access_diff_user`, `_diff_holders`, `_clear_diff` or `reconcile_queue`: `drain`, `listing_writer`, `settle_granularity`, `settle_yield`, `superseded_functions`, `role_grants` on the prophet, `lock_budget` and `lock_upgrade`, rewritten against the cursor and the mark with the same scenarios.

74. Go `dbtest`: `prophet/echo/routed_binding_db_test.go` and `disciple/sam/sync_db_test.go` stay; `shared/access_db_test.go` gains the check count of item 72.3 through `GetAccess`; samna_auth's drain under `LeadSync` is tested in samna_auth.

75. `just test-hurl` for `get_organizations` and `get_locations` pages on the example disciple.

76. The bench, in `test/sql/<role>/bench.sql`, skipped by default and run with `smig test --keep` on a kept database: the 16,000 children under one root, the grant on a node over 10,000 organizations, 200 users over 2,000 organizations for `get_users` and per row `get_user_access`, each figure the index delta of item 72 and `clock_timestamp()`, written as the measured column of `comparison.md`. The figures to beat are fact 6 and `materialization.md:114`: 1,577,903 and 201,374 entries for the two listings, 96,082 for the second, 437,835,068 for the per row calls.

## 8. The docs to correct

77. Removed, with the `rm` printed for the user: `.nimpress/claimius/theory.md`; `.nimpress/claimius/scaling.md`. No other file links to either.

78. `index.md`: `:35` the Materialization row names the cursor and the mark in place of `access_diff_user`; `:165-166` the stored tables: `access_diff` as the open areas since the oldest cursor, `user_activity` as the cursors, no `access_diff_user`; `:181` and `:183` the same, no `reconcile_queue`; `:191` the check reads the pair after its mark proves it; `:230` principle 3 reads "a listing settles the reader's share of every area raised since its cursor, a check the one pair when an area on its nodes is newer than the pair's mark, and an area leaves when every active cursor has passed it".

79. `materialization.md`: `:5` tags; `:19` `user_object` gains `settled_txid`; `:21` `access_diff` columns, `txid`, `root_id`, kinds `object` and `assignment`; `:22` and `:26` removed; `:25` `user_activity` columns; `:34-53` the recompute as item 50 with `3 + δ` read and 2 written per object and the derivation of item 27 as the figure it replaces; `:55-68` the writes table with the statement trigger; `:70-84` the areas table as item 48; `:86-98` the check as item 53; `:100-116` the listing as item 49; `:118-143` the locks table without `access_diff_user` and `_clear_diff`; `:145-177` the share as items 49 and 50, holders gone; `:179-193` the proof restated on the mark and the cursor; `:197-217` the fold for viewers as item 56; `:219-239` the drain and the trim as item 55; `:241-244` rebuilding.

80. `reads.md`: `:11` the opening; `:34-47` `get_access` as 4 entries and the mark; `:51-71` `get_direct_access` with `p_settle`; `:77` `mutation_access` one settle; `:81-89` the page as `h + 2p`; `:93` the per object figure; `:117` `get_organizations` filters before the page; `:119` `get_users` for viewers; `:126-134` which reads write.

81. `writes.md`: `:29` the never written list; `:42` `calc_claim_access`; `:74-87` the areas table; `:105-116` the areas column; `:124-134` the request's area rows `n_c + 2 + k`; `:142-144` removing a claim; `:146-156` locks, the area row lock and the trim.

82. `comparison.md`: rewritten whole on section 3 and items 33 to 36, with the measured column from the bench, the workload weights and the losses of item 36.4 kept as losses.

83. `claims.md:50-56` the composition check settles one pair by the mark; `:113` the window read from the pair; `:129` `_best_grant` reads nothing; `:143-157` the gate counts. `users.md:50-64` `get_user_access` and `get_users` for viewers; `:77-91` the fold for viewers; `:115-117`. `objects.md:37` no `reconcile_queue`; `:88-107` the two columns and the index of item 42; `:99` `recompute_user_object` reads `object_info` and inserts nothing. `organizations.md:109` the page filter. `storage.md:13-27`, `:47-62`, `:95-109` the rows, indexes and dead rows of the new tables. `graphs.md:19`, `:72-78` the statement triggers. `usage.md:41`, `:59`. `security.md:32`. `sync.md:67` the areas a page raises. `types.md:63` the derivation of one pair as item 51. `denials.md:54` `_apply_denies` runs only for a pair with a deny or a scope.

84. `.claude/rules/claimius.md:7` and `:32`, `.claude/skills/claimius/SKILL.md:90`, `:95`, `:310`: the never written list and the settle sentence.

## 9. The half done items

85. In the middleware:
    1. `_apply_denies` separate: item 50 evaluates a pair with no deny and no scope as `bit_or` in the set statement and calls `_apply_denies` only for the others; the function stays as it is.
    2. `get_organizations` and `get_locations` short page and double settle: item 59.
    3. The duplicate `DROP` at prophet `V2.4:1`: removed, item 67.
    4. `lock_budget.sql` and `lock_upgrade.sql`: moved by `mv` in both roles, items 72.7 and 72.11.
    5. The check's error handling: kept as fact 10; `reconcile_if_pending` returns whether it settled so the gate knows when to walk, item 58.

86. In the consumers, after the release and in their own repos, named here because the goal needs them:
    1. samna_auth: `InitAccessDiffDrain` runs under `LeadSync` with the key of item 62.
    2. bookable: the ten local `claimius.insert_claim_object` calls become `NewInsert().Model(&sdb.ClaimObject{...}).Access(ac)`, the routed binding, with the claim bound by the creator's owner claim on the row.
    3. nimployer: `PostAsset` binds the creator the same way, the shape of `nimployer/internal/handler/claim.go:289`.

## 10. What stays out of v2.0.0

87. Ancestor arrays on `object_info` in place of the closure (`v2.md` item 95), and any change to how the closure is kept beyond item 46.

88. A check or a listing that evaluates from the sources instead of the pair (`v2.md` items 96 and 97); the ruling keeps the pair.

89. Read routing to standbys beyond what `shared` does today (`v2.md` item 79), pool sizing (`v2.md` item 94), the check as a predicate of the service's own query (`v2.md` item 90).

90. A witness based `user_users` without counts: it saves on grants and loses on revokes by the partners' rebuilds, so the counted fold stays.

91. Analytics, federation, guests, the clis and the consumer repos beyond item 86.
