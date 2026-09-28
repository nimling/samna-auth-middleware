---
layout: doc
title: Organizations
description: Root and child organizations, the ownership tree and its closure, create_root_organization, and how an organization reaches an app through app_object.
tags: organization, ownership, sa_owner_id, sa_root_id, create_root_organization, app_object, inheritance_info, migrate_root
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
| `private_seed` | The HMAC seed a root generates and its children inherit, keying `app_secret` hashing for apps owned in the tree |
| `sa_created_by` | The `user_claim.id` the creator acted under |

## Roots

1. A row is a root when `sa_owner_id = sa_root_id = id`. There is no nullable parent column, so a walk up the tree stops on the row that points at itself.

2. A root is a tenant. Every row under it resolves to it, and a move to another root is refused by the calc trigger. `migrate_root(object_type, object_id, new_root_id)` is the only path across roots, and every call is reviewed by a person.

3. `create_root_organization(app_id, name, ...)` creates a root and links it to the app in one call. It is idempotent on app and name, so a bootstrap may run it on every start.

## Children

1. A child sets `sa_owner_id` to its parent and leaves `sa_root_id` empty. The before insert trigger `tg_self_root` copies the parent's `sa_root_id`.

2. A new child inherits the app links of its owner. The calc trigger calls `link_app_object` for every app the parent is linked to, so an app that reaches a tree reaches rows added under it.

## The ownership tree

The ownership tree lives in `inheritance_info` as a closure table: one row per ancestor and descendant pair, including the depth zero self edge. A closure answers "is X under Y" and "everything under Y" with one indexed lookup each, where a recursive walk costs one lookup per level ([Karwin, Rendering Trees with Closure Tables](https://karwin.com/blog/index.php/2010/03/24/rendering-trees-with-closure-tables/)).

Claimius maintains the closure incrementally.

1. Inserting a node under parent `p` inserts its self edge and one edge per ancestor of `p`: `D + 1` rows for depth `D`, read from `p`'s own edges. This is the path `_attach_object_to_trees` takes for every registered row.

2. Moving a subtree `S` within its root deletes the edges from ancestors outside `S` to nodes in `S` and inserts the cross product of the new parent's ancestors with `S`: `n_S × D` rows each way for `n_S` nodes in `S`, the edges inside `S` untouched ([Karwin, Moving Subtrees in Closure Table Hierarchies](https://www.percona.com/blog/moving-subtrees-in-closure-table/)).

3. Soft deleting a node detaches it from the closure the same way.

4. `build_ownership_tree(root)` rewrites a whole root from live data. It serves `recompute_state` and registration, never an ordinary write.

### Why incremental

The closure of a tree with `N` nodes of average depth `D̄` holds `N(D̄ + 1)` edges. An insert writes `D + 1` of them and reads `D` from the parent's own edges through the covering index, so building a tree node by node costs `Σ (D_i + 1)`, linear in the size of the tree, and one insert costs the same under a tree of ten nodes or a million. A move costs in proportion to what moves, `n_S × D`, never to the size of the tree. In the benchmark tree of depth four a new organization writes 5 edges; the tree with its 20,000 registered rows holds 142,743.

## Linking to an app

An organization has no `app_id`. It is shared hierarchy, and an app reaches it through a live `claimius.app_object` row.

1. `link_app_object(app_id, object_type, object_id, created_by)` creates the link and materializes access for the pair. It is idempotent.

2. `unlink_app_object` soft deletes the link and purges the access rows it granted.

3. `app_object_ids(app_id, object_type)` is the filter a prophet query uses in place of `app_id = ?`. `app_has_object` is the boolean guard, `apps_for_object` the reverse.

4. The unique index is `(app_id, object_type, object_id)` where `sa_deleted_at IS NULL`, so a live link is unique per app and object.

One organization can serve several apps without being copied, which is why the column is absent rather than repeated. A disciple mirrors one app, so every organization it holds is already in scope and it carries no `app_object` at all. See [Apps](./apps.md).

## Reading organizations

`get_organizations(user, app, required)` lists the organizations a user can reach. `get_owner_graph` renders the tree. `RootAccess` resolves the actor's own root for actions on the root itself. Every read goes through `user_object`; see [Reads](./reads.md).
