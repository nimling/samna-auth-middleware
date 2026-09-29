# Machine clients

A machine client is a service account that lets one service call another service's API with no human in the loop. It is a row in `claimius.samna_client`, created on the prophet, replicated to the disciple by sync, and presented on every call as an id and a secret.

The defining property: `samna_client.client_id` doubles as the `user_id` in `user_claim`. A client is an actor like any other, so every claim, every scope grant and every access check works on it unchanged. There is no second authorization path for machines.

## Roles in the flow

| Side | Runs | Does |
|------|------|------|
| Prophet | `smw.AppMiddleware` over `sam.RegisterEndpoints` | Creates the client, returns the secret once, binds claims to the client id |
| Disciple that serves data | `smw.UserAuthMiddleware(nil)` or `smw.ClientAuthMiddleware()` | Verifies the credentials against its local replica and sets the actor |
| Disciple that calls | its own HTTP client | Sends the id and secret as basic auth |

## Creating the client

The routes live under the group `sam.RegisterEndpoints` mounts, guarded by `smw.AppMiddleware`, so the caller authenticates as an app with `SAuth-App-Slug` and `SAuth-App-Secret`, not as a user.

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/samna_client` | List the app's clients |
| `GET` | `/samna_client/:id` | One client |
| `POST` | `/samna_client` | Create. Returns the raw secret once |
| `PUT` `PATCH` | `/samna_client/:id` | Update name, description, status, expiry |
| `DELETE` | `/samna_client/:id` | Soft delete |
| `POST` | `/samna_client/:id/rotate` | New secret, returned once |
| `POST` | `/samna_client/:id/claim` | Bind a claim to the client |
| `DELETE` | `/samna_client/:id/claim/:claim_id` | Remove a binding |

`POST` generates 32 random bytes, base64 raw url encodes them, stores only the bcrypt hash in `secret_hash`, and returns the plaintext in the `secret` field of the response. It is never retrievable again. `rotate` does the same against an existing row. Losing the value means rotating.

`app_id` is stamped from the app context, never from the body. Create the client under **the app whose API it will call**, because the sync feed is filtered by `app_id`; a client created under the calling app never reaches the serving app's replica.

## Granting it access

A client with no claim authenticates and sees nothing. Bind a claim the same way you would for a user:

- Object access. `assign_claim_object` binds the claim to organizations, locations or registered rows; the client's `user_object` pairs settle on its first check or listing, and `ObjectWhere(q, clientID, objectType, level, nil, after, limit)` filters a listing by them.
- Capability access. A `claim_object` with a NULL `object_id` and a `scope` payload, read back with `ScopeAccess(clientID, jsonpath, level)`. This is how a client is told it may read only the public subset of a resource. See [Capabilities](../.nimpress/claimius/capabilities.md).

`POST /samna_client/:id/claim` takes a `user_claim` body and stamps `app_id` from the app context. Time bounds through `starts_at` and `ends_at` apply to clients exactly as to users.

## Reaching the disciple

The feed carries `claimius.samna_client`, `claimius.samna_secret`, `claim`, `user_claim` and `claim_object` as source rows, filtered by `app_id`, in the snapshot `GetSyncSnapshot` and in every page after it. The disciple's `sam.SyncStart(ctx)` loop applies them and derives the client's `user_object` pairs itself on the client's next read, so a client created on the prophet becomes usable on the disciple within one pull.

Nothing on the serving side calls the prophet per request. `authenticateClient` reads the local copy only: it runs bcrypt once per credential and after that compares the digest of the presented secret with the one it verified, rereading the client row by `client_id` on every request, so a revoked client or a rotated secret stops on the next call.

## Calling with the credentials

The credentials go on the wire in one of two shapes, checked in this order:

1. HTTP basic auth, `Authorization: Basic base64(client_id:client_secret)`.
2. Form values `client_id` and `client_secret`.

The client id must be the `client_id` uuid, not the row `id`.

```go
req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/article", nil)
req.SetBasicAuth(clientID, clientSecret)
```

Two optional headers ride along when the call is made on behalf of a human:

- `SAuth-User-Token` is validated with `ValidateToken`, and the resolved owner is checked with `HasAccess(clientID, ownerID, OBJECT_TYPE_ORGANIZATION, ACCESS_READ)` before the user replaces the client as the actor.
- `SAuth-User-ID` carries an external user reference, resolved and checked against the client's read on the owner the same way.

A call carries at most one of the two: both together are refused with 403.

Omit both for anonymous traffic such as a public website; the client itself is then the actor and its own claims decide the answer.

## Serving the call

`smw.ClientAuthMiddleware()` accepts client credentials and nothing else. Use it on a group that is only ever reached by another service.

`smw.UserAuthMiddleware(opts)` tries a bearer token, then a session cookie, then client credentials. Use it on a group that serves browsers and services from the same paths, which is the common case for an API a website and a machine both read.

Either way the actor lands in the context:

```go
clientID := ctx.Get(smw.UserIDKey).(uuid.UUID)
```

From there the handler runs the same access lookups it runs for a human.

## Verifying

`GET /status/...` style unauthenticated routes answering while `/api/...` answers 401 means the credentials are being rejected, not the service being down. Check, in this order: the client row exists on the serving side's replica, its `status` is `active`, `expires_at` is null or in the future, it was created under the serving app's `app_id`, and the secret is the plaintext from create or rotate rather than the hash.

## Debug

`SAUTH_DEBUG_CLIENTS` holds a JSON map of static client credentials and bypasses the database lookup. `SAUTH_DEBUG_USER_ID` short circuits authentication entirely and pins one user id. Both also stop `SyncStart` from pulling, so a deployment with either variable set holds no replicated clients at all.
