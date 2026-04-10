# samna_auth_middleware: Remaining Work

## Completed

- Schema files copied (V1.0 through V1.3)
- Function renames: ClaimAccess → GetAccess, UserObjectAccess deleted, UserObjectHasLevel → HasAccess, GetObjectClaims added
- ByID renames: GetUserByID → GetUser, GetOrganizationByID → GetOrganization, GetLocationByID → GetLocation
- InitAuthViews → InitAuthTables
- GetAccess returns *ActiveClaim with ClaimAccessLevel parameter
- New model structs registered (UserObject, UserUsers, ObjectUsers, UserObjectEvent, ClientState)
- Echo package callers updated
- Builds clean at v1.0.0-alpha30
- samna_client + client_claim tables in schema, copied

## Client → App Rename

- `client/http.go`: `SAuth-Client-Slug` → `SAuth-App-Slug`, `SAuth-Client-Secret` → `SAuth-App-Secret`
- `db/models.go`: `UserRelation.ClientID` → `AppID` (bun tag `client_id` → `app_id`)

## samna_client Model Registration

- `db/models.go`: add `SamnaClient` and `ClientClaim` Bun structs
- `db/main.go`: register new models
- `db/client.go`: `GetClient`, `GetClientClaims` query functions

## M2M Client Auth Middleware

- Check `SAuth-Client-Key` + `SAuth-Client-Secret` headers
- If present AND no `Authorization` header: M2M client path
- Validate against local `samna_auth.samna_client` table
- Check `status = 'active'` and `expires_at > NOW()` (or NULL)
- Set `ctx.Set("client_id", samnaClient.ID)`
- Do NOT set `user_id`

## Sync Package (sync/)

- Payload types, apply logic, wait, pull, cascade
- `sync.ApplyPayload`: apply sync messages in replay_mode transaction
- `sync.CascadeAccess`: post apply recomputation of application table access
- `sync.WaitForSeq`: poll client_state at 50ms, SAUTH_SEQ_TIMEOUT env var
- `sync.PullSnapshot`: GET /sam/sync/snapshot
- `sync.PullSync`: GET /sam/sync/:seq

## Echo Handlers

- `echo.RegisterSyncHandler`: POST handler for receiving sync payloads
- `echo.WaitForRequestSeq`: read your own writes helper

## Client Package Updates

- `client/http.go`: inject SAuth-App-Slug, SAuth-App-Secret, SAuth-Seq headers
- `client/callback.go`: RegisterSyncCallback + SyncItem/SyncObject types
- All write wrappers return (result, seq, error)

## db.Init Rewrite

- Synchronous snapshot pull during init (not in debug mode)
- Retry with backoff, panic after SAUTH_INIT_MAX_RETRIES
- Delete RegisterSyncFunc

## Files To Delete

```
rm hook/handler.go
```

Then remove the `hook/` directory entirely. Replaced by sync/ package and echo.RegisterSyncHandler.
