---
layout: doc
title: Organizations
description: Root and child organizations, the ownership tree and its up arrays, create_root_organization, and how an organization reaches an app through app_object.
tags: organization, ownership, sa_owner_id, sa_root_id, create_root_organization, app_object, owner_ids, migrate_root
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

The ownership tree is the `owner_ids` array of every object's `object_info` row: the owning organization first and the chain to the root after it. "Is X under Y" is a membership test on X's own row, and "everything under Y" is the posting list of Y in `idx_object_info_ancestors`; see [Objects](./objects.md#the-up-set).

1. Inserting a node under parent `p` writes its row with `p` prepended to `p`'s own `owner_ids`: one row, one posting per ancestor. This is the path `_attach_object_to_trees` takes for every registered row.

2. Moving a subtree `S` within its root rewrites the `owner_ids` of its `n_S` rows: the prefix that was the old parent's chain is replaced by the new parent's.

3. `build_ownership_tree(root)` rewrites the arrays of a whole root from live data. It serves `recompute_state` and registration, never an ordinary write.

### Why arrays

A tree of `N` nodes at average depth `D̄` stores `N` rows whose arrays hold `N D̄` ids, and `N D̄` postings of one to seven bytes each. An insert writes one row, so building a tree node by node is linear in its size and one insert costs the same under ten nodes or a million. A move writes the `n_S` rows that move, never more: those are exactly the objects whose ancestors changed. A closure table writes `D + 1` rows per insert and `2 n_S D` per move for the same answers; the comparison is Result 3 of [Theory](./theory.md).

## Linking to an app

An organization has no `app_id`. It is shared hierarchy, and an app reaches it through a live `claimius.app_object` row.

1. `link_app_object(app_id, object_type, object_id, created_by)` creates the link and materializes access for the pair. It is idempotent.

2. `unlink_app_object` soft deletes the link and purges the access rows it granted.

3. `app_object_ids(app_id, object_type)` is the filter a prophet query uses in place of `app_id = ?`. `app_has_object` is the boolean guard, `apps_for_object` the reverse.

4. The unique index is `(app_id, object_type, object_id)` where `sa_deleted_at IS NULL`, so a live link is unique per app and object.

One organization can serve several apps without being copied, which is why the column is absent rather than repeated. A disciple mirrors one app, so every organization it holds is already in scope and it carries no `app_object` at all. See [Apps](./apps.md).

## Reading organizations

`get_organizations(user, app, required)` lists the organizations a user can reach. `get_owner_graph` renders the tree. `RootAccess` resolves the actor's own root for actions on the root itself. Every read goes through `user_object`; see [Reads](./reads.md).
