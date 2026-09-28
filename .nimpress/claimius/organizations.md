---
layout: doc
title: Organizations
description: Root and child organizations, the ownership closure and what an organization write rebuilds, create_root_organization, and how an organization reaches an app through app_object.
tags: organization, ownership, sa_owner_id, sa_root_id, build_ownership_tree, create_root_organization, app_object, migrate_root
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
| `private_seed` | The seed a root generates and its children inherit, keying `app_secret` hashing for apps owned in the tree |
| `sa_created_by` | The `user_claim.id` the creator acted under |

## Roots and children

1. A row is a root when `sa_owner_id = sa_root_id = id`. There is no nullable parent column, so a walk up the tree stops on the row that points at itself.

2. A child sets `sa_owner_id` to its parent and leaves `sa_root_id` empty. The before insert trigger `tg_self_root` copies the parent's `sa_root_id` in one primary key probe, and copies the parent's `private_seed`.

3. A root is a tenant. Every row under it resolves to it, and an update that changes `sa_root_id` is refused by the calc trigger. `migrate_root(object_type, object_id, new_root_id)` is the only path across roots, and every call is reviewed by a person.

4. `create_root_organization(app_id, name, ...)` probes for a live root of that name linked to the app, and otherwise creates the root and links it with `link_app_object`. It is idempotent on app and name, so a bootstrap may run it on every start.

## The ownership closure

The ownership tree is the `ownership` rows of `inheritance_info`: for every organization its chain to the root, and for every row of every registered type with `sa_owner_id` and `sa_deleted_at` its owner followed by the owner's chain. That includes claimius's own tables that carry an owner: locations, claims, grants, bindings, apps, clients, secrets, fields and relations. "Is X under Y" is one probe, "everything under Y" one range; see [Objects](./objects.md#the-closure).

The closure of one root `r` holds

```
N_r = Σ_{organizations o under r} (d(o) + 1)  +  Σ_{owned rows x under r} (d_o(x) + 1)
```

rows, where `d(o)` is the organization's depth and `d_o(x)` the depth of the row counted from its owner.

### What an organization write costs

Every insert, update or delete of an organization runs `calc_hierarchical_access`, which calls `build_ownership_tree` on the root:

1. It takes a transaction scoped advisory lock on the root, so writes inside one tenant serialize and writes in different tenants do not meet.

2. It deletes the root's `N_r` ownership rows.

3. It inserts the organization paths with one recursive query over `idx_organization_sa_owner_id`, `Σ_o (d(o) + 1)` rows.

4. For each registered type with an owner it walks the organization tree again inside `_select_owned_rows` and inserts the self edge and the chain of every live row owned in it, `Σ_x (d_o(x) + 1)` rows.

So one organization write deletes and inserts `N_r` rows each, `2 N_r` rows with three index entries each, whatever changed. The work is linear in the size of the tenant, not of the change: a new leaf organization adds `d + 1` rows to the closure and pays `2 N_r` to get them. Then, per app of the organization, the actor's own pair is recomputed and its `user_users` rows refreshed, and an object area with `cascade` is raised on the organization; see [Materialization](./materialization.md).

A new child also inherits the app links of its owner: for every app the owner is linked to, `link_app_object` inserts the link and recomputes the pair of every user in `_affected_users_for_object(child)`.

### Why the root is the unit

A move of a subtree `S` changes the ancestors of every node of `S` and of every row owned under it, and nothing else. Rebuilding the root rewrites a superset of those rows, the whole tenant, in one set based statement per tree, and needs no case analysis of insert, move and delete: the result is the closure of the live rows, whatever the previous closure held. The advisory lock makes the rebuild and the rows it reads one consistent picture of the tenant. The cost is bounded by the tenant, never by the other tenants of the database.

## Linking to an app

An organization has no `app_id`. It is shared structure, and an app reaches it through a live `claimius.app_object` row.

1. `link_app_object(app_id, object_type, object_id, created_by)` probes for a live link and returns it, or inserts one and recomputes the pair of every user in `_affected_users_for_object`, then refreshes those users' `user_users` rows.

2. `unlink_app_object` soft deletes the link and deletes every `user_object` and `object_users` row of the app on the object, then refreshes the `user_users` rows of those users.

3. `app_object_ids(app_id, object_type)` is the filter a prophet query uses in place of `app_id = ?`, one range of `idx_app_object_unique`. `app_has_object` is the boolean guard, `apps_for_object` the reverse through `idx_app_object_object`.

4. The unique index is `(app_id, object_type, object_id)` where `sa_deleted_at IS NULL`, so a live link is unique per app and object.

One organization serves several apps without being copied, which is why the column is absent rather than repeated: a copy per app would make one move `n_apps` writes and let the copies disagree. A disciple mirrors one app, so every organization it holds is already in scope and it carries no `app_object`. See [Apps](./apps.md).

## Reading organizations

`get_organizations(user, app, required)` returns the organizations linked to the app that `get_objects` returns for the user: one `app_object_ids` range intersected with the user's `V_{u,organization}` pairs. `get_owner_graph` renders the tree; see [Graphs](./graphs.md).
