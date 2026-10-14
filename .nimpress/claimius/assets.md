---
gate: auth-middleware
layout: doc
title: Assets
description: The shared link from an asset to a claimius object, object_asset, which apps it is published to, how a link is read and written, and how a file is fetched end to end through the prophet without any hop keeping or inventing freshness.
tags: object_asset, get_object_assets, apps_for_object, GetObjectAssets, GetObjectAsset, GetObjectAssetData, GetAssetData, AssetSource, AssetCache, asset link, profile, banner, logo
lastUpdated: true
---

# Assets

An asset is a file a service stores: a profile picture, a banner, a logo, an attachment. The asset row and its bytes stay in the service that stored them, its home. Claimius holds only the link from the asset to a claimius object, so every app that sees the object sees the link, and every reader that may read the object may fetch the file. Bytes never sync; links do.

| Column | Is |
|---|---|
| `id` | The link, the id a reader fetches the file by |
| `app_id` | The home app holding the asset, prophet only |
| `object_type`, `object_id` | The linked object, always a `claimius.*` type; a user by `user_id` |
| `asset_id` | The asset's id in the home's own storage |
| `kind` | `profile`, `banner`, `logo`, `file` and so on |
| `name`, `description`, `mime_type`, `size`, `index` | What a listing shows; `index` orders the links of one object |
| `sa_owner_id`, `sa_created_by` | The object's owner and the grant the writer held WRITE through when it linked, never NULL |

A home app holds one live link per asset and object: `(app_id, asset_id, object_type, object_id)` is unique among live rows on the prophet, `(asset_id, object_type, object_id)` on a disciple, so one asset can be shared on several claimius objects. `object_asset` is not a registered access table. A link has no grants of its own; access to it is access to its object.

## Which apps receive a link

A link follows its object. `apps_for_object(object_type, object_id, app_id)` names the apps a link is published to, and `emit_sync_event` writes the link once per app it names, stamped with that app:

| Object | Apps |
|---|---|
| `organization`, `location` | Every app linked to it through `app_object` |
| `samna_user` | Every app holding a live `samna_user` row with that `user_id` |
| `samna_app` | That app |
| `claim` | The claim's app |
| Any other claimius type | The link's home app |

1. A delete, hard or soft, goes to the same apps as the write it ends. A link moved to another object is deleted from the apps that no longer see it.

2. A `merge_user` moves the links on the source user to the target, and the move publishes them to the apps that see the target. When an object becomes visible to an app later, its live links follow it: an `app_object` link that goes live publishes the object's links to that app after the object itself, and a `samna_user` row inserted or revived for an app publishes the user's links to that app. An `app_object` link that ends deletes the object's links from that app before the object.

3. The snapshot of an app, the disciple's and a peer's, carries every link the app would have been sent, and the snapshot source count counts them, so the backlog check sees them.

4. A disciple applies a link like any source row, upsert by `id` and delete by `id`, with `app_id` stripped. A peer prophet applies it with the home it names, which may be an app the peer does not mirror; `app_id` carries no foreign key for that reason.

## Reading links

`get_object_assets(user, app, object_type, object_id, kind?)` on the prophet and `get_object_assets(user, object_type, object_id, kind?)` on a disciple return the live links on one object the reader may see, ordered by `index`, then `sa_created_at`, of one kind when one is named. They settle the reader's share first, like every `get_*`, and answer nothing for a reader who is not active.

1. On a `samna_user` the reader is the user itself, or a user `get_user_access` resolves with READ: the self, shared visibility and direct claim branches `get_users` decides by.

2. On any other object the reader holds READ through `get_access`.

3. On the prophet the link must also be published to the reading app.

In Go, on `*DB`:

```go
links, err := db.GetObjectAssets(ctx, userID, shared.OBJECT_TYPE_USER, userID, "profile")
link, err := db.GetObjectAsset(ctx, userID, linkID)
```

`GetObjectAsset` answers one link by id under the same rule and `ErrNotFound` when the reader may not see it.

## Writing links

On the prophet a link is a direct insert, update or soft delete, with `app_id` the home app.

On a disciple the builder routes the write to the prophet, `POST`, `PATCH` and `DELETE` on `/sam/object_asset`, and upserts the answer locally:

```go
ac, err := db.MutationAccess(ctx, actor, actor, shared.OBJECT_TYPE_USER, shared.ACCESS_WRITE)
link := &shared.ObjectAsset{ObjectType: shared.OBJECT_TYPE_USER, ObjectID: actor, AssetID: asset.ID, Kind: "profile", MimeType: asset.MimeType}
err = db.NewInsert().Model(link).Access(ac).Scan(ctx)
```

1. The prophet names the actor from `SAuth-Forwarded-User`, which the builder sends with every routed write from the access claim it was given, or from the user of the `created_by` grant in the calling app.

2. It authorizes with the actor's WRITE on the object through `get_access`, or the actor's own `samna_user` when the object is the actor. It stamps `sa_owner_id` and `sa_created_by` from that grant. For a link on the actor's own user the owner is the user and `sa_created_by` the actor's default claim grant in the home app, the grant `upsert_app_user` wrote; without one the write is refused with `403`. `sa_created_by` is never NULL.

3. `app_id` is the calling app. An update or delete reaches only a link the calling app is home for, after the same check on the link's object. An update changes `kind`, `name`, `description`, `mime_type`, `size` and `index`; the object and the asset of a link do not move through the route.

4. A second live link on the same asset of one home is refused with `409`.

## Fetching a file

A reader fetches a file from its own app, the app asks the prophet, and the prophet serves the bytes it holds or relays to the home.

```
reader → app /api/asset/:id/data → prophet /sam/object_asset/:id/data → home <auth base>/asset/:asset_id/data
```

1. **The app.** The consumer's own route calls `GetObjectAssetData(ctx, linkID)` from `disciple/echo`. It checks the reader's READ on the link's object through `GetObjectAsset`, then asks the prophet through the `client` with the app's credentials, forwarding the reader's `If-None-Match` and `If-Modified-Since`. It writes back the status, `Content-Type`, `Content-Length`, `ETag`, `Last-Modified` and `Cache-Control` unchanged, so a `304` reaches the reader as a `304`, and streams the body.

2. **The prophet.** `GET /sam/object_asset/:id/data` answers only a link published to the calling app. When the link's home is the prophet's own system app it streams the prophet's own asset, the lookup and blob `GetAssetData` uses, by the link's `asset_id` and `app_id`, with the conditional headers passed to the blob store. Otherwise it relays a plain `GET` to the home at `<auth base>/asset/<asset_id>/data`, the auth base being the home app's redirect URI ending in `/callback` without `/callback`, where a disciple mounts its `/sam` group. The relay carries a JWT signed with the home app's key, `kid` the home app, issuer the prophet, audience the home app, subject the prophet's system app, `client_id` its slug, valid for a minute.

3. **The home.** `GET /asset/:id/data` in `disciple/sam`'s `RegisterEndpoints` accepts only that token: valid against the app's JWKS, the prophet as issuer, the app in the audience, an expiry, and `client_id` the slug of the prophet the disciple is bound to. A user's token for the same app carries the app's slug there and is refused. The disciple cannot know the host's storage, so it calls the host's source, `AssetSource`, which opens the asset by id and answers status, headers and body. With no source it answers `404`. A `200` whose `ETag` matches the request's `If-None-Match`, or whose `Last-Modified` is not after its `If-Modified-Since`, is answered `304` without a body.

The host registers both callbacks with its configuration:

```go
db, err := sdb.Init(e.Logger, sdb.Options{
	AssetSource: func(ctx context.Context, assetID uuid.UUID) (*sdb.AssetResponse, error) { ... },
	AssetCache:  cache,
})
```

## Freshness passes through

Every hop passes `Content-Type`, `Content-Length`, `ETag`, `Last-Modified` and `Cache-Control` through untouched and never invents freshness. The middleware keeps no cache of its own. A host may register an `AssetCache`:

| Method | Called |
|---|---|
| `Get(ctx, linkID, conditional)` | Before the prophet is asked; answers a stored response or nothing |
| `Put(ctx, linkID, header, body)` | While a `200` streams to the reader, with every response header and the body |

1. A stored response is served only while it is fresh under its own `Cache-Control`: `s-maxage` or `max-age` against its `Date` and `Age`, and never when it says `no-store`, `no-cache` or `private`.

2. A stale one is revalidated with the prophet using its own `ETag` and `Last-Modified`. A `304` serves the stored body with the fresh validators and `Cache-Control` the prophet answered, and puts nothing; a `200` streams the new body and puts it.

3. Only a `200` is put, and never one that says `no-store` or `private`.

A host cache that honours `no-store`, `private`, `max-age` and revalidation by `ETag` and `Last-Modified` is therefore never asked to serve what the home would not have let a cache serve.

## What it costs

With `A` the apps a link is published to and `L` the links of one object:

1. A link write is one row and `A` feed events. A user row that goes live in an app adds the user's `L` live links to that app's feed, and an `app_object` link that goes live or ends adds the object's `L`.

2. `get_object_assets` is the reader's share settle, one access probe on the object, `get_access` or `get_user_access`, and one range of `idx_object_asset_object` of `L` entries; on the prophet each entry also probes `apps_for_object`, one range of `idx_app_object_object` or `idx_samna_user_user_app` or one primary key.

3. A fetch is one `GetObjectAsset`, one prophet request and, for a link homed on a disciple, one relayed request; the bytes cross each hop once and are stored by none of them.
