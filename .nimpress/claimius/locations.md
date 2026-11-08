---
gate: samna-developer
layout: doc
title: Locations
description: The location tree, how a row pins into it with sa_location_id, how a location is also a node of its owner's ownership tree, and the closure rows each location write inserts and deletes.
tags: location, sa_location_id, sa_parent_id, location tree, calc_hierarchical_access, _reattach_location_in_ownership, _splice_subtree, inheritance_info
lastUpdated: true
---

# Locations

A location is where something is. A building holds floors, a floor holds rooms, a room holds whatever is placed in it.

| Column | Holds |
|---|---|
| `sa_parent_id` | The parent location. Equals `id` on a root |
| `sa_root_id` | The top location of the tree |
| `sa_owner_id` | The organization that owns the location |
| `sa_level` | Depth below the root |
| `type`, `code_type`, `code` | What it is and how it is labelled |
| `longitude`, `latitude` | Where it is on a map |

## Two trees at once

1. A location is a node of the location tree through `sa_parent_id`. The before insert trigger `tg_self_root` runs `_set_self_root_on_insert`: a row with no parent, or with itself as parent, becomes a root with `sa_parent_id = sa_root_id = id`; a child copies its parent's `sa_root_id`.

2. A location is also a node of the ownership tree, under its owner. A binding on the owning organization, or on any organization above it, reaches the location.

3. Any registered row, an organization included, pins into the location tree with `sa_location_id`. A binding on a building reaches every row pinned anywhere inside it, whoever owns that row.

The trees are closed separately. The `location` rows of `inheritance_info` hold location chains and the rows pinned under them; the `ownership` rows hold owner chains. A row pinned in a building and owned by another organization is reached by a binding on the building and by a binding on its own owner, and not by a binding on the building's owner: a derivation follows one tree from the bound node to the object; see [the model](./index.md#the-tree-relation).

## What a location write does

Every write of a location runs `calc_hierarchical_access` under the advisory lock of its root.

| Write | Location closure | Ownership rows of the location |
|---|---|---|
| Insert | its chain inserted | its chain under the owner inserted |
| Parent change | the subtree spliced under the new parent, through `_splice_subtree` | none |
| Owner change | none | moved to the new owner, through `_reattach_location_in_ownership` |
| Soft delete | every row of the subtree deleted | deleted |
| Restore | the subtree rebuilt by `build_location_tree(root, node)` | inserted |
| Rename | none | none |

A reparent carries every row pinned under the moved location, because those rows are descendants of the node in the location closure. An owner change moves only the location's own ownership rows: rows pinned in the location keep their own owners and are not in the ownership tree through the location.

The rest is the same as for an organization: the `object_info` row is written when a rendering or structural field changed, an insert links the location to every app of its owner, and per app the actor's pair is recomputed and one object area with `cascade` raised, except for a rename; see [Organizations](./organizations.md#what-an-organization-write-does).

## Reading locations

1. `get_locations(user, app, required, after, limit)` returns the locations linked to the app that the user reaches, in `id` order, read through `get_objects` pages, each kept when live and linked.

2. `get_location_ancestors(type, id)` returns the location chain of an object from the closure, the object itself at hop 0, and for each location on the chain its owner's ownership chain. `get_ancestors` returns the three trees tagged by name. `get_direct_access` walks it; see [Claims](./claims.md#the-single-object-gate).

3. `get_owner_graph` renders locations under their parents or owners, with `longitude` and `latitude` in the node payload; see [Graphs](./graphs.md).

## Cost

For a location `l` at depth `d_l(l)` in the location tree, owned by `o`, and a row `x` pinned in `l`:

```
location rows of l   = d_l(l) + 1
ownership rows of l  = d(o) + 2           its self edge, its owner and the owner's ancestors
location rows of x   = d_l(l) + 2 = d_l(x) + 1
```

With `S` the stored location subtree of `l`, the locations below it and every row pinned in them, and `n_S = |S|`:

| Write | Location rows | Ownership rows of `l` |
|---|---|---|
| Insert at depth `d` | `d + 1` inserted | `d(o) + 2` inserted |
| Parent change, depth `d` to `d'` | `n_S · d` deleted, `n_S · d'` inserted | none |
| Owner change | none | `d(o) + 2` deleted, `d(o') + 2` inserted |
| Soft delete | `Σ_{x ∈ S} (d_l(x) + 1)` deleted | `d(o) + 2` deleted |
| Restore | `Σ_{x ∈ S} (d_l(x) + 1)` inserted | `d(o) + 2` inserted |
| Rename | none | none |

The root insert trigger copies the parent's root with one probe. An insert links the location with one `upsert_app_object` per app of the owner and writes one `object_info` row.

`get_location_ancestors` gives each owner chain row the hop `loc_depth + 1 + depth`. `get_ancestors` reads one range of `idx_inheritance_info_descendant` per tree plus one owner chain range per location on the chain.
