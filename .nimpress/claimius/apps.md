---
layout: doc
title: Apps
description: samna_app, the two scoping mechanisms, app scoped tables and app_object links, what each costs a query, and the identity helpers that tell SQL which app it runs as.
tags: samna_app, app_id, app_object, app_object_ids, get_app_id, scoping, tenancy
lastUpdated: true
---

# Apps

An app is one product that claimius governs: Samna Auth itself, Bookable, Nimployer. `claimius.samna_app` holds it.

| Column | Holds |
|---|---|
| `slug` | The unique name a disciple presents to its prophet |
| `private_key` | The key the prophet signs the app's tokens with |
| `private_seed` | The seed the app's secrets are encrypted with |
| `app_secret`, `secret_version` | The hashed secret a disciple authenticates with |
| `redirect_uris`, `connection`, `provider_ids` | The login configuration |
| `claim_id` | The default claim `ensure_app_user` grants a new user |
| `sa_owner_id` | The organization that owns the app |

## Two scoping mechanisms

1. Own column. `samna_user`, `claim`, `user_claim`, `claim_object`, `user_object`, `object_users`, `user_users`, `user_relation`, `user_field`, `object_field`, `samna_client`, `samna_secret`, `audit` and `access_diff` carry `app_id` on a prophet. A row belongs to one app, and every read function filters on it: `app_id` is the second column of `idx_user_claim_user_app` and `idx_user_object_user_type` and the first of `idx_user_users_viewer`, so the filter narrows the same descent that finds the user.

2. Links. `organization` and `location` carry no `app_id`. An object belongs to every app holding a live `app_object` row for it, found with `app_object_ids(app_id, object_type)`, guarded with `app_has_object`, created with `link_app_object`, removed with `unlink_app_object`.

Why two: users, grants and access are per app by nature, the same person holds different grants in two products, so a per app row costs nothing extra. Organizations and locations are shared structure, the same company and the same building serve every product. With `n_a` apps reaching an organization, a copy per app stores `n_a` rows and makes every move `n_a` rebuilds that can disagree; the link stores one row and `n_a` links of four columns, and a move rebuilds once.

## Scoping per role

1. A prophet holds every app. Every read function takes the app as a parameter and filters on it in its body.

2. A disciple mirrors one app. Its tables carry no `app_id` column and it has no `app_object`: every row it holds is already in scope, and its read functions accept the app argument and ignore it. The prophet selects what reaches a disciple by `app_id` and `app_object_ids`, so scoping is written on the prophet side only; see [Sync](./sync.md).

3. A row with no `app_id` of its own is scoped by `_app_ids_for_row`: its `app_id` column when it has one, otherwise its `app_object` links, otherwise, for an implementing side's table, the deployment's own app. The calc trigger runs once per app the row resolves to.

## Knowing which app the SQL runs as

| Function | Returns |
|---|---|
| `get_app()` | The system app on a prophet or hybrid, as a row, by the slug marked in `prophet_state` |
| `get_disciple_app()` | The mirrored app on a disciple or hybrid, as a row, by the slug in `disciple_state` |
| `get_prophet_app_id()`, `get_disciple_app_id()` | The same as scalars |
| `get_app_id()` | The system app on a prophet or hybrid, the mirrored app on a pure disciple |

Each is two primary key or unique probes. The scalar forms exist for `DEFAULT` expressions, which cannot hold a subquery. A system app slug is never written into SQL, because every deployment names its own.
