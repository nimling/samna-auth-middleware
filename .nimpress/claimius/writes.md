---
layout: doc
title: Writes
description: The write surface, which tables take a direct insert, what every write does inside its own transaction, and what each costs.
tags: create_claim, assign_claim_user, assign_claim_object, calc_object_access, calc_hierarchical_access, calc_claim_access, write cost
lastUpdated: true
---

# Writes

A write changes a source row: a registered row, an organization, a location, a claim, a binding, a grant, a user. Triggers on the source table keep every derived table true, inside the same transaction.

## The surface

| Table | Written through |
|---|---|
| `claim` | `create_claim`, `update_claim`, `remove_claim` |
| `user_claim` | `assign_claim_user`, `update_claim_user`, `remove_user_claim` |
| `claim_object` | `assign_claim_object`, `update_claim_object`, `remove_claim_object` |
| `samna_user` | `ensure_app_user`, `merge_user` |
| `organization` roots | `create_root_organization` |
| `app_object` | `link_app_object`, `unlink_app_object` |
| `organization`, `location`, `samna_app`, `samna_client`, `samna_secret`, `object_field`, `user_field`, `user_relation`, `prophet_state`, `disciple_state` | Direct insert and update |
| The implementing side's registered tables | Direct insert and update, by the implementing side |

`claim`, `user_claim` and `claim_object` also accept a direct insert from a handler, because their before triggers enforce every invariant whatever the path in; the functions are the ergonomic form for SQL callers. Every write function returns one jsonb object keyed by the table it wrote, unpacked with `->` and `->>`.

Never written by hand: `user_object`, `object_info`, `user_users`, `table_info`, `sync_event`, `access_diff`, `reconcile_queue`, `claim_graph_cache`, `user_activity`. `audit` is written through `write_audit`.

## What a write does

| Write | Up set | Synchronous settle | Area |
|---|---|---|---|
| Registered row insert | The row's arrays, its parent's with the parent prepended | The actor's pair; the `object_info` row | Object |
| Registered row rename | none | The `object_info` row | none |
| Registered row owner or location move | The row's arrays rewritten | The actor's pair; the `object_info` row | Object |
| Registered row soft delete | Out of every posting list with the row | The actor's pair removed; the `object_info` row | Object |
| Organization or location insert | The row's arrays, its parent's with the parent prepended | The actor's pair; the `object_info` row | Object with cascade |
| Subtree move within its root | The arrays of the `n_S` rows that move | The actor's pair | Object with cascade |
| `claim`, `claim_object` | none | none | Claim |
| `user_claim` | none | none | The assignee's slice of the claim |
| `samna_user` soft delete | none | Grants, relations and fields soft deleted by `tg_cascade_user_soft_delete` | The user's claims |

Every write is constant in the number of users and in the reach of the claim it touches. So:

| Write | Cost |
|---|---|
| A registered row | One `object_info` row with its up set, one settled pair, one area row |
| A grant or revoke | One `user_claim` row, one area row, its `sync_event` and `audit` rows; measured with the older synchronous settle at 3.5 ms for 33 objects and 6.6 ms for 166, the difference being the settle it no longer does |
| A binding or claim change | One row, one area row; `A × 0.019 ms` for each holder who lists, the drain for the holders likely to list before the next change |
| A move of `n_S` nodes | `n_S` `object_info` rows, the rest later |

## One request, one transaction

The write a person makes through the API is one request: create a claim, bind it to an organization or location with `inherits`, grant it to `k` users. It is one transaction on the prophet, and its statements travel as one batch, so it pays one round trip and one commit.

| | Rows | Index entries | Locks held to commit |
|---|---|---|---|
| `claim` | 1, its `sync_event` and `audit` | about 13 | the new row; `FOR KEY SHARE` on the owning organization and the app |
| `claim_object` | 1, its `sync_event`, `audit` and claim area | about 18 | the new row; the claim's area row |
| `user_claim` × `k` | `k`, their `sync_event`, `audit` and slice areas | about `16k` | the new rows; `FOR KEY SHARE` on the claim |

That is about `7 + 4k` heap rows and `31 + 16k` index entries, 1.2 ms of database CPU at `k = 3`, and about 10 kB of WAL plus a page image for each index leaf first touched since the last checkpoint. The areas are raised last in the transaction, so the only row another writer can wait on, an area row, is held for the commit and nothing more: writers of one claim queue only above `1 / h`, about 300 a second at a 3 ms commit. Everyone under the node settles on their own next read, `A × t` each; see [Materialization](./materialization.md).

Why not settle in the write: settling the `k` grantees inside the write costs `k × A × t` and holds every one of those `user_object` rows locked until commit, 17 ms at `k = 3` over 300 objects and 1.1 s per grantee over 20,000. The on demand shape makes the write constant and hands each grantee exactly its own slice. The inequality is `k × A × t > 0`, which holds for any grant of a claim bound to anything.

## Why the actor's own pair is settled in the write

A writer reads its own row back in the same request: a handler creates a row and returns it. The actor's pair for a row it wrote is one pair, constant, so the write settles it and the read back needs no settle. A claim, binding or grant change has no single pair; the actor who lists what a binding now covers settles its own slice on that read, in the same request, at the same cost it would have paid inside the write. Claimius is always entered in the context of a user or a client, so the actor is always known: `_actor_user_for_row` resolves it from `sa_created_by`.

## Routed writes

On a disciple, a write to a shared type goes to the prophet, and the row the prophet answers with is upserted locally in the same call. The implementing side's own tables are written locally. That routing belongs to the middleware; see [Middleware](./middleware/index.md).
