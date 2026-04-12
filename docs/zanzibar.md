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
- SAuth-Client-Slug/Secret → SAuth-App-Slug/Secret in all client/ and echo/ files
- UserRelation.ClientID → AppID in db/models.go
- SamnaClient struct updated for M2M, ClientClaim struct added and registered
- Auth refactored: Auth(opts), UserAuthMiddleware(opts), ClientAuthMiddleware(opts) with AuthOptions
- ClientAuthMiddleware sets both client_id and user_id on context
- db/client.go with GetClientByKey, GetClientClaims
- Old SyncCursor type and methods removed
- sync/ package created: ApplyPayload, PullSnapshot, PullSync, UpdateSeq, GetSeq, WaitForSeq
- echo/sync.go created with PostSync handler
- client/sync.go rewritten to use sync.PullSnapshot
- db/main.go Init rewritten with retry and backoff (panics after 5 attempts)

## Client → App Rename In Echo Handlers

`echo/user.go` and `echo/me.go` still have old Client references:
- `GetUserClients` → `GetUserApps`
- `GetUserClient` → `GetUserApp`
- `PutUserClient` → `PutUserApp`
- `GetMeClients` → `GetMeApps`
- `GetMeClient` → `GetMeApp`

## Cleanup

- Remove `RegisterSyncHandler` and `GetSyncHealth` from `echo/sync.go` (consumer mounts PostSync directly)
- Remove `echo/hook.go` entirely (RegisterWebhooks, all PostWebhook* handlers, GetWebhookHealth, verifyWebhook)
- Remove `hook/handler.go` entirely (SyncOrganization, SyncLocation, etc replaced by sync.ApplyPayload)

## Files To Delete

```
rm /Users/slyotis/Projects/nimtech/samna/samna_auth_middleware/echo/hook.go; rm /Users/slyotis/Projects/nimtech/samna/samna_auth_middleware/hook/handler.go
```
