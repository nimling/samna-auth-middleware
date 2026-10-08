---
visibility: hidden
layout: doc
title: Writes
description: The claimius write surface, which tables take a direct insert, what every trigger does inside the write's own transaction, the rows each write stores as a function of the data, one request worked through, and the locks a write takes.
tags: insert_claim, insert_user_claim, insert_claim_object, delete_claim, calc_object_access, calc_hierarchical_access, calc_claim_access, calc_object_trees, emit_sync_event, enforce_app_home, _check_claim_composition, _raise_user_users_diff, upsert_app, write cost
lastUpdated: true
---

# Writes

A write changes a source row: a registered row, an organization, a location, a claim, a binding, a grant, a user. Triggers on the source table keep the closure, the object's `object_info` row and the writer's own pairs true inside the same transaction, raise the area of what changed, and append the source row to the outbox. Every other user settles when it reads; see [Materialization](./materialization.md).

## The surface

| Table | Written through |
|---|---|
| `claim` | `insert_claim`, `update_claim`, `delete_claim` |
| `user_claim` | `insert_user_claim`, `update_user_claim`, `delete_user_claim` |
| `claim_object` | `insert_claim_object`, `update_claim_object`, `delete_claim_object` |
| `samna_user` | `upsert_app_user`, `merge_user` |
| `samna_secret` | `insert_secret` |
| `organization` roots | `upsert_root_organization` |
| `samna_app` | `upsert_system_app` for the system app, `upsert_app` for every other app; see [Apps](./apps.md#creating-an-app) |
| `app_object` | `upsert_app_object`, `delete_app_object` |
| `organization`, `location`, `samna_app` updates, `samna_client`, `samna_secret` updates, `object_field`, `user_field`, `user_relation`, `prophet_state` | Direct insert and update |
| The implementing side's registered tables | Direct insert and update, by the implementing side |
| Cross root moves | `migrate_root`, reviewed by a person on every call |

`claim`, `user_claim` and `claim_object` also take a direct insert, because their triggers do the same work whatever the path in; the functions are the form for SQL callers and return one jsonb object keyed by the table they wrote. Never written by hand: `user_object`, `object_info`, `user_users`, `user_users_diff`, `inheritance_info`, `table_info`, `sync_event`, `access_diff`, `user_activity`, `claim_graph_cache`.

## The triggers

| Trigger | On | Does |
|---|---|---|
| `tg_enforce_app_home`, `enforce_app_home` | the source tables, before | Refuses a write outside replay to a row of an app another prophet decides, SQLSTATE `P0409` with the home in the hint; an organization or location is refused when any app it is linked to is decided elsewhere. One probe of `samna_app` and of `app_handover`, or of the row's `app_object` links |
| `tg_self_root` | `organization`, `location`, before insert | A root self references; a child copies its parent's `sa_root_id`, one probe |
| `tg_update_txid` | `claim`, `user_claim`, `claim_object`, before | Stamps `txid = pg_current_xact_id()`, which the graph cache compares to its snapshot |
| `tg_check_claim_composition` | `claim_object`, before | Refuses a binding that hands out more than the actor holds, a self link, a reverse link, and a link to a claim the actor cannot read |
| `tg_calc_access`, `calc_object_access` | registered tables without their own tree, `samna_user`, `samna_client`, `samna_secret`, `claim`, `user_relation`, `object_field`, `user_field`, `user_token` | Keeps the row's ownership and location closure rows, its `object_info` row with its creator and owner root, the actor's pair, and raises the object area under the owner root when the write is structural |
| `tg_calc_access`, `calc_hierarchical_access` | `organization`, `location`, registered tables with their own tree | The same, with the table's own tree kept incrementally, the object area raised with `cascade`, and on a soft delete one area per descendant whose closure rows went with it |
| `tg_calc_trees`, `calc_object_trees` | `samna_app`, `user_claim`, `claim_object` | Their ownership closure rows and, on the prophet, the `object_info` row of a `samna_app`, so every bindable object has one |
| `tg_calc_claim_access`, `calc_claim_access` | `claim`, `claim_object`, `user_claim` | Raises the object areas of a claim's bindings and of its claim object, the object area of a binding's object, or the assignee's assignment area, and settles the actor's pairs on the claim and on a binding's object |
| `tg_emit_sync_event`, `emit_sync_event` | the source tables | One outbox row per row written; see [Sync](./sync.md) |
| `tg_raise_user_users_diff_insert`, `tg_raise_user_users_diff_delete` | `user_object`, per statement | One `user_users_diff` row per pair entering or leaving on an object that is not a claim while the app has a viewer, one range of the viewer index read and nothing locked; see [Users](./users.md#who-can-see-whom) |
| `tg_invalidate_claim_graph_cache_insert`, `_update`, `_delete` | `user_object`, `user_users`, per statement | Drops the cached claim graph of the users of the statement's transition tables, an update only where a column the graph shows moved; see [Graphs](./graphs.md) |
| `tg_invalidate_claim_graph_cache` | `object_info`, `samna_user` | Drops the affected users' cached claim graph |
| `tg_cascade_user_soft_delete` | `samna_user`, on the transition of `sa_deleted_at` | Soft deletes the user's grants, relations and field values in the app, once |

### A registered row

`calc_object_access` runs after every insert, update or delete:

1. `_retree_object` detaches the row's ownership and location closure rows when it is deleted or moved, `rows(x)` deletes, and attaches it when it is inserted, restored or moved: the self edge and the owner's ancestor rows copied with `depth + 1`, `d_o + 1` rows, and the same for its location pin, `d_l + 1`. A parent outside the closure leaves the row out, as a full build would. It returns whether the write is structural, anything but an update that keeps the row live, its owner and its location.

2. When the write is not an update, or an update of `name`, `description`, `sa_owner_id`, `sa_location_id`, `sa_root_id` or `sa_deleted_at`, `_write_object_info` upserts the one `object_info` row where a field differs, or deletes it for a row gone or soft deleted.

3. Per app of the row, `_app_ids_for_row`: the actor's pair on the row, `R(actor, {x})`, and on a structural write one object area.

### An organization, a location, or a row with its own tree

`calc_hierarchical_access` keeps the row's own tree incrementally under the transaction advisory lock of its root, so writes inside one tenant serialize and writes in different roots never meet:

| Write | Closure rows |
|---|---|
| Insert at depth `d` | the self edge and `d` rows under the parent, `d + 1` |
| Reparent of a subtree of `n_S` nodes | `n_S × D_old` deleted, `n_S × (D_p + 1)` inserted, by `_splice_subtree` |
| Soft delete of a node with subtree `S` | every row of `S`'s nodes deleted |
| Restore | the restored subtree rebuilt under its live parent by `build_ownership_tree`, `build_location_tree` or `build_parenthood_tree` with the node |
| Update that moves nothing | none |

A location is also a node of its owner's ownership tree: `_reattach_location_in_ownership` drops its ownership rows and inserts its self edge and its owner's chain, `d(owner) + 2` rows, when its owner changes or it is deleted or restored. An organization's own location pin, and a parenthood row's owner and pin, move through `_retree_object`. An update that changes `sa_root_id` outside replay is refused; `migrate_root` is the only path across roots.

A new organization or location inherits the app links of its owner: per app the owner is linked to, `upsert_app_object` inserts the link and raises the object area. Then `object_info` as above, the actor's pair per app, and one object area with `cascade` unless the update is a rename, `name` and `description` alone, which every reader sees through `object_info` without a settle.

### A claim, a binding or a grant

`calc_claim_access` does nothing under replay, where the apply raises the same areas itself; otherwise it raises the areas of the change and settles the actor:

| Write | Raises | Actor's pairs recomputed |
|---|---|---|
| `claim`: name, mask, level, inherit flag or deletion | `object` area on every live binding of the claim and of the claims it links to, `cascade` where the binding inherits and the claim did before or after, and `object` on the claim itself: `n_c + 1` rows | the claim |
| `claim_object` on an object | `object` area on the bound object, with `cascade` when the binding inherits before or after; on an update that moves it, on the old object as well | the claim and the bound object |
| `claim_object` link | the same as a claim change on both claims: `n_A + n_B + 2` rows | the claim and the bound claim |
| `claim_object` scope binding | nothing: no pair carries it | the claim |
| `user_claim` | `assignment` area of the assignee for the claim | the claim |

Every change is recorded on objects: a binding changes only the pairs on its object, and on the object's descendants when it cascades, so its area is the object area of that object; a claim's own change changes the pairs on every object it is bound to, so it raises the object areas of its bindings. A check then finds the change on a node of the object's chain and nothing per held claim, and a holder's next listing settles the objects bound or changed since its cursor, not every live binding of every claim it holds. A holder that lost a binding still holds its pair on the object until its next read recomputes it. A replayed binding raises the object area with `cascade`, since the page does not carry the row it replaced. Measured in the `binding_area` suite: forty bindings of one claim settled, five more bound, the holder's next listing recomputes 5 objects instead of 46; in `claim_change_areas`, a mask change of a claim bound on three objects raises four object areas and none per holder, and a link raises one per binding of both claims and one on each claim object.

The assignee is not settled in the write. A grant changes one user's access and its area names that user, so the assignee's next check settles the pair it asks about and its next listing its share. The other holders of a claim are an unbounded set, `|H_c|`, and none of them is settled in the writer's transaction.

`_check_claim_composition` runs first on every `claim_object` insert or update outside replay:

1. A binding on an object whose effective mask, `eff(c, b)`, carries a bit the actor does not hold there is refused: `reconcile_if_pending` settles the actor's pair on the object, one range reads `max(sa_access)`, and `eff & ~A(actor, y) & 15` must be 0. A deny, a removal and a binding that does not change mask, claim or object are exempt. Two first bindings pass on the actor's owner bit on an organization in place of its access on the object:

    1. On a prophet, the first live binding of an app, while the app carries none, from an actor whose grant belongs to another app, when the actor holds owner on the organization that owns the `samna_app` row, judged in the actor's own app, the app of its `user_claim`, after settling that one pair. A new app has no pairs, so no actor could hold anything in it; the owner of the organization that owns the app is the one with the authority to open it, and `upsert_app` binds under it.

    2. The first live binding on a `samna_user` that holds a claim assigned under the binding's owning organization, when the actor holds owner on that organization in the binding's app. A new user has no pair anyone could hold yet; the owner of the organization the user was just made a member of may bind the user, which is how `insert_organization_user` binds the new user's claim.

    3. A binding on an object outside the `claimius` schema that this database holds no `object_info` row for, a disciple's own row bound on the prophet, from an actor with no pair on it, is judged on the actor's pair on the binding's owning organization. The type passes through unresolved; the disciple computes access on the row when the binding arrives.

2. A composition link must not point at its own claim or reverse a live link, one probe of `idx_claim_object_object`, and the actor must hold read on the target claim, its settled pair on `claimius.claim`.

## What each write stores

A pair entering or leaving writes one `user_users_diff` row while the app has a viewer and reads nothing else; the count it moves is folded by the next read that needs it. `R` is the recompute of [Materialization](./materialization.md#recomputing-a-share).

| Write | Closure rows | `object_info` | Pairs recomputed | Areas | Outbox rows |
|---|---|---|---|---|---|
| Registered row insert | `d_o + d_l + 2` | 1 | the actor's pair | 1 object | 0 |
| Registered row rename | 0 | 1 | the actor's pair | 0 | 0 |
| Registered row move or soft delete | `rows(x)` deleted, `d_o + d_l + 2` inserted on a move | 1 | the actor's pair | 1 object | 0 |
| Organization or location insert at depth `d` | `d + 1`, and `d(owner) + 2` for a location | 1 | the actor's pair, once per app | 1 object with `cascade` | 1 per linked app |
| Organization or location reparent of `n_S` nodes | `n_S (D_old + D_p + 1)` | 1 when a field moved | the actor's pair | 1 object with `cascade` | 1 per linked app |
| Organization or location soft delete of a subtree of `n_S` nodes | `Σ rows(x)` deleted | 1 | the actor's pair | 1 object with `cascade` and `n_S` object, one per dropped descendant | 1 per linked app |
| Organization or location rename | 0 | 1 | the actor's pair | 0 | 1 per linked app |
| `claim` insert | `d_o + 1` for the claim itself | 1 | the actor's pair on the claim, twice | 1 object on the claim | 1 |
| `claim` mask, level, inherit flag, name or deletion | 0 | 1 | the actor's pair on the claim | `n_c + 1` object | 1 |
| `claim_object` insert on an object | `d_o + 1` | 0 | the actor's pairs on the claim and the object | 1 object | 1 |
| `claim_object` link | `d_o + 1` | 0 | the actor's pairs on both claims | `n_A + n_B + 2` object | 1 |
| `user_claim` insert | `d_o + 1` | 0 | the actor's pair on the claim | 1 assignment | 1 |
| `samna_user` soft delete | 0 | 0 | none beyond the cascade | 1 object, and 1 assignment per grant of the user | `1 + C` and the relations and fields |

Every write is constant in the number of users who hold the claims involved: the only user a write settles is its actor, on the rows it names. A pair recompute that finds its pair unchanged writes nothing, so a second recompute of the same pair in one transaction costs its reads and no rows. The implementing side's registered tables write no outbox rows; the source rows of claimius do, one per row written, and an organization or location one per app linked to it.

## One request, one transaction

The write a person makes through the API: create a claim, bind it to a node `y` with `inherits`, grant it to `k` users. As one transaction on the prophet:

| Step | Source rows | Pairs recomputed | Areas |
|---|---|---|---|
| `claim` | 1, and `d_o + 1` closure rows | the actor's pair on the claim, twice | the object area on the claim |
| `claim_object` on `y` | 1, and `d_o + 1` closure rows | the actor's pairs on the claim and on `y` | the object area on `y` with `cascade` |
| `user_claim` × `k` | `k`, and `k (d_o + 1)` closure rows | the actor's pair on the claim, `k` times | `k` assignment areas, one per assignee |

```
source rows  = 2 + k                     outbox rows = 2 + k
recomputes   = 4 + k, each of one pair   pairs written ≤ 2
area rows    = 2 + k: the object area on the claim, the object area on y, one assignment per assignee
```

The request is linear in the users it names and independent of the reach `|T_c| = 1 + |desc(y)|` of the node and of every other user of the app. Each assignee settles its share, about `|T_c|` pairs, on its next listing, and a single pair on its next check. The measured consequence for the feed, 50 events against 240,551 when derived rows travelled, is in [Sync](./sync.md).

### Why only the actor is settled in the write

A writer reads its own write back in the same request: a handler creates a row and returns it, or binds a claim and shows it. The actor's pairs on the rows the write names are what that read back needs, so the write settles them and nothing else. Every other user of the change is an unbounded set, and settling them in the write would put their pairs inside the writer's transaction with every row locked until commit. `_actor_user_for_row` resolves the actor from `sa_created_by`, one probe.

## Removing a claim

`delete_claim` soft deletes, in four statements, the links pointing at the claim, its `|H_c|` grants, its `n_c` bindings, then the claim. Each soft delete is an update that fires the triggers of its table: per row one area raise, which collapses onto the object area of the binding's object, the assignee's assignment area or the claim's object areas, one outbox row, and the actor's pair on the claim, a pair that stops changing after the first. So the removal writes `|H_c| + n_c + 1 + links` source rows and outbox rows, and at most `n_c + 1 + |H_c|` area rows beside those of the linked claims, whatever the claim reaches. Each holder's pairs that reached `T_c` only through the claim are deleted by that holder's next read.

## Locks

1. A hierarchy write takes the transaction advisory lock of its root, `pg_advisory_xact_lock(hashtext(root))`.

2. An area raise takes the area row's lock until commit, so two writers of one object, or of one assignment, serialize on that row and on nothing else. A reader never locks an area: it reads the areas from its cursor or its mark, so a raise never waits on a read. The trim deletes only areas every active cursor has passed, under the drain's own transaction.

3. A pair recompute takes the row locks of the pairs it writes and nothing else, and raises one `user_users_diff` row per pair entering or leaving while the app has a viewer, a new row no other transaction waits on. No write reads or locks another user's pairs, so two writers adding pairs on one object never meet.

4. A write takes no lock per object and no lock that grows with the pairs it moves; the shared lock table holds the root locks of its hierarchy writes and nothing more. Row locks are marks in the rows and take no entry there ([PostgreSQL, Row-Level Locks](https://www.postgresql.org/docs/17/explicit-locking.html#LOCKING-ROWS)).

5. A read takes one transaction advisory lock for the reader's whole share, per app and user, with `pg_try_advisory_xact_lock`, so two reads of one user never settle at once and neither waits on the other. What a read locks and who waits on it is in [Materialization](./materialization.md#locks-a-settle-takes).

## Routed writes

On a disciple a write to a shared type goes to the prophet, and the row the prophet answers with is upserted locally; the implementing side's own tables are written locally. That routing belongs to the middleware; see [Writes in Go](./middleware/writes.md).
