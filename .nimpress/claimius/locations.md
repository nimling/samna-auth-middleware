---
layout: doc
title: Locations
description: The location tree, how a row pins into it with sa_location_id, and how a location is also a node of its owner's ownership tree.
tags: location, sa_location_id, sa_parent_id, location tree, inheritance_info
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

1. A location is a node of the location tree through `sa_parent_id`. A root sets `sa_parent_id = sa_root_id = id`; a child sets its parent and the before insert trigger copies the root.

2. A location is also a node of the ownership tree through `sa_owner_id`. A claim on the owning organization reaches the location, and through the location tree everything below it.

3. Any registered row pins into the location tree with `sa_location_id`. An organization may pin itself too. A claim on a building reaches every row pinned anywhere inside it, whoever owns that row.

## The closure

The location tree lives in `inheritance_info` with `tree_type = 'location'`, maintained incrementally exactly as the ownership tree is: `D + 1` edges for a new node, `n_S × D` edges each way for a subtree move within its root. A row pinned with `sa_location_id` gets its self edge and one edge per location above its pin. See [Organizations](./organizations.md) for the cost argument.

Ancestors of a pinned row are the union of three sets, each one indexed lookup on `idx_inheritance_info_descendant`: its location chain, the owning organization chain of each location in that chain, and its own ownership chain. `get_location_ancestors` returns the first two, `get_ancestors` all three tagged by tree.

## Linking and reading

1. A location has no `app_id`. Like an organization it reaches an app through `app_object`, and a new location inherits the app links of its owner.

2. `get_locations(user, app, required)` lists the locations a user reaches. `get_owner_graph` renders locations under their owners, with `longitude` and `latitude` in the node payload.
