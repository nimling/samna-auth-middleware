---
layout: doc
title: Organizations
description: Root and child organizations, the ownership closure, the closure rows each organization write inserts and deletes, create_root_organization, migrate_root, and how an organization reaches an app through app_object.
tags: organization, ownership, sa_owner_id, sa_root_id, calc_hierarchical_access, _splice_subtree, build_ownership_tree, create_root_organization, link_app_organization, app_object, migrate_root
lastUpdated: true
---

# Organizations

An organization is who owns something. Every registered row carries `sa_owner_id`, an organization, and organizations nest through the same column.

| Column | Holds |
|---|---|
| `sa_owner_id` | The parent organization. Equals `id` on a root |
| `sa_root_id` | The top of the tree. Equals `id` on a root |
| `sa_level` | Depth below the root, 0 at the root |
| `sa_location_id` | An optional location pin for the organization itself |
| `private_seed` | The seed a root generates and its children inherit |
| `type`, `external_id`, `provider_id` | What it is and where it came from |
| `sa_created_by` | The `user_claim.id` the creator acted under |

## Roots and children

1. A row is a root when `sa_owner_id = sa_root_id = id`. There is no nullable parent column, so a walk up the tree stops on the row that points at itself.

2. The before insert trigger `tg_self_root` runs `_set_self_owner_on_insert`. A row with no owner, or with itself as owner, becomes a root and generates a `private_seed` of 32 random bytes. A child with no `sa_root_id` copies its parent's `sa_root_id` and `private_seed`, one primary key probe.

3. A root is a tenant. Every row under it resolves to it, and an update that changes `sa_root_id` outside replay and outside `migrate_root` is refused by `calc_hierarchical_access`. `migrate_root(object_type, object_id, new_root_id)` is the only path across roots, and every call is reviewed by a person.

4. `create_root_organization(app_id, name, actor, ...)` probes for a live root of that name linked to the app and returns it, and otherwise inserts the root and links it with `link_app_object`. It is idempotent on app and name, so a bootstrap may run it on every start.

## The ownership closure

The ownership tree is the `ownership` rows of `inheritance_info`: every organization with its chain to the root, every location under its owner, and every row of a registered type with `sa_owner_id` under its owner's chain. That includes claimius's own owned tables: claims, grants, bindings, apps, users' relations and fields, clients and secrets. "Is `x` under `y`" is one probe of the primary key, "everything under `y`" one range of `idx_inheritance_info_ancestor`; see [Objects](./objects.md).

For an organization `o` at depth `d(o)` and a row `x` owned by `o`:

```
rows(o) = d(o) + 1                       its self edge and one row per ancestor
rows(x) = d(o) + 2 = d_o(x) + 1          its self edge, its owner and the owner's ancestors
N_r     = Σ_{o under r} (d(o) + 1) + Σ_{x owned under r} (d_o(x) + 1)
```

## What an organization write costs

Every insert, update or delete of an organization runs `calc_hierarchical_access`. It takes the transaction advisory lock of the root, `hashtext(sa_root_id)`, so closure writes inside one tenant serialize and writes in different tenants never meet, and then changes only the rows the write moves. With `S` the stored ownership subtree of the organization, itself, its descendant organizations, their locations and every row owned in them, and `n_S = |S|`:

| Write | Closure rows | Through |
|---|---|---|
| Insert at depth `d` | `d + 1` inserted: the self edge and one row per ancestor of the parent | a direct insert, then `_splice_subtree` with `n_S = 1` |
| Owner change inside the root, from depth `d` to `d'` | `n_S · d` deleted, `n_S · d'` inserted | `_splice_subtree` |
| Soft delete | `Σ_{x ∈ S} (d(x) + 1)` deleted | `_splice_subtree` with no parent |
| Restore, or a node missing from the closure | `Σ_{x ∈ S} (d(x) + 1)` inserted | `build_ownership_tree(root, node)`, the subtree only, hung under the stored ancestors of its parent |
| Rename, or any write that moves no owner, root or deletion | none | |

`_splice_subtree` deletes the rows linking `S` to its former ancestors and inserts the rows linking it to the new parent's ancestors, `n_S (D + 1)` rows for a parent at depth `D`. When the new parent is outside the closure or inside `S` itself, it drops every row of `S`, as a rebuild of the root would drop a subtree its root no longer reaches. Each closure row carries three index entries, the primary key and the two covering indexes.

Beside the closure, the same trigger:

1. Keeps the organization's own location pin with `_retree_object`: on insert, restore or a pin change its `d_l + 1` location rows; see [Locations](./locations.md).

2. Writes the organization's `object_info` row through `_write_object_info` when the name, description, owner, location, root or deletion changed, one row; see [Objects](./objects.md#objectinfo).

3. On insert of a child, links it to every app its owner is linked to, one `link_app_object` per app.

4. Per app the row resolves to, recomputes the actor's pair on the organization and raises one object area with `cascade` on it, unless the write only renamed it: a rename raises no area, because no access follows a name. See [Materialization](./materialization.md).

Under `replay_mode`, on a disciple and for a row a peer's feed carries, the same trigger keeps the closure and raises the area, and settles no actor, because a replayed row has no local actor.

```
closure rows written (insert)  = d + 1 (+ d_l + 1 for a pin)       index entries = 3 per row
closure rows written (move)    = n_S (d + d')
area rows raised               = one per app, collapsing onto the open area of the organization
```

The work of a write is linear in the subtree it moves and constant in the size of the tenant: a new leaf organization writes `d + 1` rows whatever `N_r` is.

## Crossing roots

`migrate_root(object_type, object_id, new_root_id)` is the one path that changes `sa_root_id`. It hangs the object, with its subtree, directly under `new_root_id`:

1. It refuses, SQLSTATE `22023`, an object that is not live, a `new_root_id` that is not a live root of the same type, and a `new_root_id` inside the object's own subtree.

2. It walks the subtree through the object's own parent column, `sa_owner_id` for an organization and `sa_parent_id` for a location or a parenthood row, so the walk of an organization follows the ownership tree it is moving.

3. One update gives every node of the subtree the new root and the object the new root as its parent, under the transaction setting `claimius.migrating_root`, which is the one case `calc_hierarchical_access` lets a root change through outside replay. The trigger runs as for any reparent: it splices the moved subtree into the new tree incrementally and raises the object area, with `cascade`, of every node it moves, so every holder on either side settles on its next read.

4. For an organization, the rows of registered tables owned in the subtree that carry `sa_root_id` and no parenthood of their own, and whose `sa_root_id` names the old root, take the new one, one update per such table. On a prophet the moved organization then joins every app of the new root, `link_app_organization` with `cascade`.

With `n_S` the moved subtree, the move writes `n_S` rows, the closure rows the splice moves and `n_S` areas, and nothing in the rest of either tenant. It still crosses the tenant boundary every other write keeps, which is why it is kept off the normal write path and every call is reviewed.

## Linking to an app

An organization has no `app_id`. It is shared structure, and an app reaches it through a live `claimius.app_object` row.

1. `link_app_object(app_id, object_type, object_id, created_by)` probes `idx_app_object_unique` for a live link and returns it, or inserts one and, outside replay, recomputes the pair of every user of `_affected_users_for_object`: the creator, the pair holders of the object and the holders of claims bound on it or, inheriting, on an ancestor. One link row with its four index entries, and `|affected(o)|` pair recomputes.

2. `unlink_app_object` soft deletes the live link and deletes every `user_object` row of the app on the object; `user_users` counts those pairs down.

3. `link_app_organization(app_id, org_id, cascade)` links an existing organization and, with `cascade`, every organization and location under it in the ownership closure: one `link_app_object` per node, additive and idempotent.

4. `app_object_ids(app_id, object_type)` is the filter a prophet query uses in place of `app_id = ?`, one range of `idx_app_object_unique`. `app_has_object` is the boolean guard, one probe. `apps_for_object` is the reverse through `idx_app_object_object`.

One organization serves several apps without being copied. With `n_a` apps reaching an organization, the link stores `n_a` link rows beside one organization row, and a move changes the closure once. A disciple mirrors one app, so every organization it holds is already in scope and it carries no `app_object`; see [Apps](./apps.md).

## Reading organizations

`get_organizations(user, app, required, after, limit)` returns the organizations linked to the app that one `get_objects` page returns for the user, in `id` order: at most `limit` of the user's `V_{u,organization}` pairs past `after`, each joined to its row by key and kept when live and linked, one `app_has_object` probe per row. `get_owner_graph` renders the tree; see [Graphs](./graphs.md).
