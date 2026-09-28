---
layout: doc
title: Apps
description: samna_app, the two scoping mechanisms, app scoped tables and app_object links, and the identity helpers that let SQL know which app it runs as.
tags: samna_app, app_id, app_object, app_object_ids, get_app_id, scoping, tenancy
lastUpdated: true
---

# Apps

An app is one product that claimius governs: Auth itself, Bookable, Nimployer. `claimius.samna_app` holds it.

| Column | Holds |
|---|---|
| `slug` | The unique name a disciple presents as `SAuth-App-Slug` |
| `private_key` | The key the prophet signs the app's tokens with |
| `private_seed` | The seed secrets of the app are encrypted with |
| `app_secret`, `secret_version` | The hashed secret a disciple authenticates with |
| `redirect_uris`, `connection`, `provider_ids` | The login configuration |
| `claim_id` | The default claim `ensure_app_user` grants a new user |
| `sa_owner_id` | The organization that owns the app |

## Two scoping mechanisms

1. Own column. `samna_user`, `claim`, `user_claim`, `claim_object`, `user_object`, `user_users`, `user_relation`, `user_field`, `object_field`, `samna_client`, `samna_secret`, `audit` and `access_diff` carry `app_id`. A row belongs to one app, and every query against them filters on it.

2. Links. `organization` and `location` carry no `app_id`. An object belongs to every app holding a live `app_object` row for it, found with `app_object_ids(app_id, object_type)`, guarded with `app_has_object`, created with `link_app_object`, removed with `unlink_app_object`, never written directly.

Why two: users, grants and access are per app by nature, the same person holds different grants in two products. Organizations and locations are shared structure, the same company and the same building serve every product, and copying them per app would make one move a write per app and let the copies disagree.

## Scoping per role

1. A prophet holds every app. Every read function takes the app as a parameter and the filter is written in the function, never left to a default.

2. A disciple mirrors one app. It carries no `app_id` column and no `app_object`: every row it holds is already in scope, stripped of `app_id` by the pull loop before upsert. The prophet selects what reaches it through `app_id` and `app_object_ids`, so scoping is written on the prophet side only.

## Knowing which app the SQL runs as

| Function | Returns |
|---|---|
| `get_app()` | The system app on a prophet or hybrid, as a row |
| `get_disciple_app()` | The mirrored app on a disciple or hybrid, as a row |
| `get_prophet_app_id()`, `get_disciple_app_id()` | The same as scalars |
| `get_app_id()` | The system app on a prophet or hybrid, the mirrored app on a pure disciple |

The scalar forms exist for `DEFAULT` expressions, which cannot hold a subquery. A system app slug is never written into SQL, because every deployment names its own.
