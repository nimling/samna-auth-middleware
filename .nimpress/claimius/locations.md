---
layout: doc
title: Locations
description: The location tree, how a row pins into it with sa_location_id, how a location is also a node of its owner's ownership tree, and what a location write rebuilds.
tags: location, sa_location_id, sa_parent_id, location tree, build_location_tree, inheritance_info
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
| `longitude`, `latitude` | Where it is on a map |

## Two trees at once

1. A location is a node of the location tree through `sa_parent_id`. A root sets `sa_parent_id = sa_root_id = id`; a child sets its parent and the before insert trigger copies the root in one probe.

2. A location is also a node of the ownership tree, under its owner. A binding on the owning organization, or on any organization above it, reaches the location.

3. Any registered row pins into the location tree with `sa_location_id`, and so may an organization. A binding on a building reaches every row pinned anywhere inside it, whoever owns that row.

The trees are closed separately. The location rows of `inheritance_info` hold location chains, and the ownership rows hold owner chains, so a row pinned in a building and owned by another organization is reached by a binding on the building and by a binding on its own owner, and not by a binding on the building's owner. A derivation follows one tree from the bound node to the object; see [the model](./index.md#derived-relations).

For a row pinned at depth `d_l`, the location closure holds `d_l + 1` rows. For a location, the ownership closure holds its self edge and its owner's chain, `d(owner) + 2` rows.

## What a location write costs

Every write of a location runs `calc_hierarchical_access`:

1. `build_location_tree` on its root: the advisory lock on the root, then the root's `N_r` location rows deleted and rebuilt with one recursive query over `idx_location_sa_parent_id` for the locations, and one per registered type with `sa_location_id` for the rows pinned in them. `2 N_r` rows written.

2. `_reattach_location_in_ownership`: the location's ownership rows under its old owner deleted, and its self edge and its new owner's chain inserted, `d(owner) + 2` rows.

3. For an insert, the location inherits the app links of its owner through `link_app_object`, as a child organization does.

4. Per app, the actor's pair and `user_users` rows, and an object area with `cascade` on the location.

## Reading locations

1. `get_locations(user, app, required)` returns the locations linked to the app that `get_objects` returns for the user.

2. `get_location_ancestors` returns the location chain of an object from the closure and, for each location on it, the owning organization's chain; `get_ancestors` returns every tree tagged by name. Both read `idx_inheritance_info_descendant`, one range per tree, plus one owner chain range per location on the chain.

3. `get_owner_graph` renders locations under their owners, with `longitude` and `latitude` in the node payload.
