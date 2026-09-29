---
layout: doc
title: Writes
description: The write surface, which tables take a direct insert, what every write does inside its own transaction, and the rows each writes as a function of the data.
tags: create_claim, assign_claim_user, assign_claim_object, remove_claim, calc_object_access, calc_hierarchical_access, calc_claim_access, _check_claim_composition, write cost
lastUpdated: true
---

# Writes

A write changes a source row: a registered row, an organization, a location, a claim, a binding, a grant, a user. Triggers on the source table keep the closure, the writer's own pairs and the outstanding areas true inside the same transaction; the rest waits for the first read, see [Materialization](./materialization.md).

## The surface

| Table | Written through |
|---|---|
| `claim` | `create_claim`, `update_claim`, `remove_claim` |
| `user_claim` | `assign_claim_user`, `update_claim_user`, `remove_user_claim` |
| `claim_object` | `assign_claim_object`, `update_claim_object`, `remove_claim_object` |
| `samna_user` | `ensure_app_user`, `merge_user` |
| `organization` roots | `create_root_organization` |
| `app_object` | `link_app_object`, `unlink_app_object` |
| `organization`, `location`, `samna_app`, `samna_client`, `samna_secret`, `object_field`, `user_field`, `user_relation`, `prophet_state` | Direct insert and update |
| The implementing side's registered tables | Direct insert and update, by the implementing side |

`claim`, `user_claim` and `claim_object` also accept a direct insert, because their triggers do the same work whatever the path in; the functions are the form for SQL callers and return one jsonb object keyed by the table they wrote. Never written by hand: `user_object`, `user_users`, `inheritance_info`, `table_info`, `sync_event`, `access_diff`, `reconcile_queue`, `claim_graph_cache`.

## What each write costs

`F_u` is the `user_users` counting for the pairs of `u` that entered or left, `U_x` entries read and at most `2 (U_x − 1)` rows written per pair on `x`, and `R_read` the reads of one pair recompute; see [Users](./users.md) and [Materialization](./materialization.md#recomputing-one-pair).

| Write | Closure rows | Pairs recomputed in the write | Area rows |
|---|---|---|---|
| Registered row insert | `d_o + d_l + 2` inserted | the actor's pair on the row, then `F_actor` | 1 object |
| Registered row rename | none | the actor's pair, then `F_actor` | none |
| Registered row owner or location change, soft delete | `rows(x)` deleted, `d_o + d_l + 2` inserted on a change | the actor's pair, then `F_actor` | 1 object |
| Row of a table with its own parenthood tree | `2 N_r` of its root, plus the attach above | the actor's pair, then `F_actor` | 1 object with `cascade` |
| Organization, any write | `2 N_r` of its root's ownership closure | the actor's pair; on insert, every affected user per app link | 1 object with `cascade` |
| Location, any write | `2 N_r` of its root's location closure, `d(owner) + 2` in ownership | as above | 1 object with `cascade` |
| `claim` insert or update | `d_o + 1` for the claim itself | the actor's pairs on `T_c` and on the claim | 1 claim, 1 object |
| `claim_object` insert or update | none | the actor's pairs on `T_c` | 1 claim, 2 for a link |
| `user_claim` insert or update | none | the actor's and the assignee's pairs on `T_c`, then `F` for each | 1 claim |
| `samna_user` soft delete | none | per grant of the user, as a `user_claim` update | 1 object, 1 claim per grant |

Every write is constant in the number of users who hold the claims involved: the only users a write settles are its actor and, for a grant, its assignee. A write on a claim is linear in the claim's reach `|T_c|`, and a hierarchy write in the size of its root's closure `N_r`. Each source row written emits one `sync_event` row, and each pair and `user_users` row written one more; see [Sync](./sync.md).

A pair recompute that finds its pair unchanged writes nothing, so the actor's second and later recomputes of the same `T_c` in one transaction cost `R_read` each and no rows.

## Checks before a binding is written

`_check_claim_composition` runs before every `claim_object` insert or update:

1. A binding on an object with a mask of its own and no deny bit must not carry a bit the writer does not hold there: one range of the writer's pairs on the object, `max(sa_access)`, and `(b & ~max & 15) = 0`.

2. A composition link must not point at its own claim or reverse a live link, one probe of `idx_claim_object_lookup`, and the writer must hold read on the target claim in `user_object`, one range.

## One request, one transaction

The write a person makes through the API is one request: create a claim, bind it to a node `y` with `inherits`, grant it to `k` users. As one transaction on the prophet:

| Step | Source rows | Pairs recomputed | Pairs written at most |
|---|---|---|---|
| `claim` | 1, and `d_o + 1` closure rows | the actor's pair on the claim | 1 |
| `claim_object` on `y` | 1 | the actor's `|T_c|` pairs | `|T_c|` |
| `user_claim` × `k` | `k` | `k` times the actor's `|T_c|`, and each assignee's `|T_c|` | `k |T_c|` |

```
source rows   = 2 + k
recomputes    = 1 + (1 + 2k) |T_c|
pairs written ≤ 1 + (1 + k) |T_c|
area rows     = 2, the claim area raised 1 + k times and stored once
```

with `|T_c| = 1 + |desc(y)|`. The request is linear in the reach of the node and in the users it names, and independent of every other user of the app. The other holders of the claim, and every user reaching `y` through another claim, settle on the first read that meets the area.

### Why the actor and the assignee are settled in the write

A writer reads its own write back in the same request: a handler creates a row and returns it, or binds a claim and lists what it now covers. The actor's pairs are those the read back needs, so the write settles them and the read back finds nothing outstanding for them. A grant is settled for its assignee because the grant exists to change that one user's access: after commit the assignee's pairs are correct before any read. Every other user of the change is an unbounded set, `|H_c|` for a claim and `|affected(y)|` summed over a subtree for a node, and settling them in the write would put `|H_c| × |T_c|` pairs inside the writer's transaction with every row locked until commit. Claimius is always entered in the context of a user or a client, so the actor is always known: `_actor_user_for_row` resolves it from `sa_created_by`, one probe.

## Removing a claim

`remove_claim` soft deletes the links pointing at the claim, then its `|H_c|` grants, then its `n_c` bindings, then the claim. Each soft delete is an update that fires the triggers of its table, so the removal recomputes at most

```
(2 |H_c| + n_c) × |T_c|
```

pairs in its transaction: the actor and the assignee per grant, the actor per binding, `T_c` shrinking as the bindings go. The pairs of users who reached `T_c` only through this claim are deleted by those recomputes or by the settle of the area.

## Locks

1. A hierarchy write takes the advisory lock of its root, so writes inside one tenant tree serialize and writes in different roots do not meet.

2. An area raise takes the area row's lock until commit, so two writers of one claim, or of one object, serialize on that row and on nothing else.

3. A pair recompute locks the pair it writes. The write's own pairs are those of its actor and assignee, so two writers acting for different users meet only on an area row.

## Routed writes

On a disciple, a write to a shared type goes to the prophet, and the row the prophet answers with is upserted locally. The implementing side's own tables are written locally. That routing belongs to the middleware; see [Writes in Go](./middleware/writes.md).
