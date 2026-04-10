# samna_auth_middleware v1 Upgrade: Zanzibar Reverse Index

The complete v1 upgrade plan for the samna_auth_middleware Go library. Companion to the prophet plan at `../../samna_auth/docs/plan/zanzibar.md` and the schema spec at `../../bookable_server/docs/claims_suggestion.md`.

No backwards compatibility, no fallback. Anything that does not match is upgraded inline.

---

## Terminology

**Prophet** — the single authoritative samna_auth deployment. Computes access for samna_auth objects (organization, location, samna_user) and pushes to disciples.

**Disciple** — any consumer service running the same schema locally. Computes access for its own application tables. Receives sync from prophet.

This middleware is the Go library used by **disciples**.

---

## 1. Package Structure

| Package | Responsibility |
|---|---|
| `db` | Database connection, models, query helpers. Exports `GetAccess`, `GetObjectClaims`, `ObjectWhere`, etc. |
| `client` | HTTP wrappers for outbound calls to the prophet. Exports `RegisterSyncCallback`. |
| `echo` | Echo handlers and helpers. Exports `RegisterSyncHandler`, `WaitForRequestSeq`. |
| `middleware` | JWT, session, token validation. Unchanged. |
| `utils` | Env, cookies, response writer. Unchanged. |
| `sync` | Internal. Payload types, apply logic, seq wait. Used by `client` and `echo`. Not exported. |

The `hook` package is deleted entirely.

---

## 2. Naming Rules

No `ByID`. No `ForObject`. Singular = one by id. Plural = list.

| Old | New |
|---|---|
| `ClaimAccess` | `GetAccess` |
| `UserObjectAccess` | deleted |
| `UserObjectHasLevel` | `HasAccess` |
| `GetUserByID` | `GetUser` |
| `GetOrganizationByID` | `GetOrganization` |
| `GetLocationByID` | `GetLocation` |
| `RegisterSyncFunc` | deleted |

---

## 3. File Diff

### 3.1 `db/claims.go`

- `ClaimAccess` → `GetAccess(ctx, userID, objectID, level)`. Calls `samna_auth.get_access(...)`. Returns `*UserObject` or nil.
- `UserObjectAccess` → deleted. Callers use `GetAccess`.
- `UserObjectHasLevel` → `HasAccess(ctx, userID, objectID, level)`.
- **Add** `GetObjectClaims(ctx, userID, objectID)` → wraps `samna_auth.get_claims(user_id, object_id)`.
- `GetClaim`, `GetClaims`, `InitAuthViews` (renamed to `InitAuthTables`) → keep.

### 3.2 `db/query.go`

Keep. `UserWhere`, `ClaimWhere`, `ObjectWhere` unchanged. Underlying SQL reads from `user_object` now.

### 3.3 `db/user.go`

- `GetUserByID` → `GetUser`.
- `GetUsers`, `GetUserClaims`, `GetUserRelations`, `UpdateUser`, `GetUserAudit`, `GetRelationByUserAndObject` → keep.

### 3.4 `db/organization.go`

- `GetOrganizationByID` → `GetOrganization`.
- All other functions → keep.

### 3.5 `db/location.go`

- `GetLocationByID` → `GetLocation`.
- All other functions → keep.

### 3.6 `db/models.go`

**Drop:** `ActiveObject`, `ActiveUser`, `SamnaClient`, `SyncCursor`.

**Add:** `UserObject`, `UserUsers`, `ObjectUsers`, `UserObjectEvent`, `ClientState`.

**Keep:** `SamnaUser`, `Organization`, `Location`, `Claim`, `UserClaim`, `ClaimObject`, `ActiveClaim`, `Asset`, `GraphNode`, `UserRelation`, `Audit`.

### 3.7 `db/main.go`

Rewrite startup flow:

```
db.Init(logger):
  1. Connect to local Postgres
  2. Register Bun models
  3. If not in debug mode:
     a. GET /sam/sync/snapshot on prophet (snapshot pull)
     b. Apply response in replay_mode via sync.ApplyPayload
     c. Run sync.CascadeAccess for the applied payload
        (recomputes local application table access from the synced claim data)
     d. Set client_state.last_applied_seq = response seq
     e. Retry with backoff on failure (SAUTH_INIT_MAX_RETRIES, default 5)
     f. Panic if max retries exceeded
  4. Return
```

`RegisterSyncFunc` deleted. `db.Init` handles the snapshot pull directly via the `sync` internal package. Debug mode (`SAUTH_DEBUG_USER_ID` set) skips steps 3a-3f entirely — everything runs locally.

`IsDebugMode()` stays. Debug mode = everything local, no prophet calls.

### 3.8 `client/sync.go` — DELETE

The cursor-based polling is gone. `Sync`, `SyncOrganizations`, `SyncLocations`, `SyncSamnaUsers`, `SyncClaims`, `SyncUserClaims`, `SyncClaimObjects`, `syncResource[T]` all deleted.

Replaced by: `db.Init` snapshot pull on startup, push receiver via `echo.RegisterSyncHandler`, and `GET /sam/sync/:seq` pull for incremental catch-up.

### 3.9 `client/callback.go` — NEW

```go
type SyncItem struct {
    ID         uuid.UUID `json:"id"`
    ObjectType string    `json:"type"`
}

type SyncObject struct {
    ID          uuid.UUID `json:"id"`
    ObjectType  string    `json:"type"`
    Name        *string   `json:"name"`
    Description *string   `json:"description"`
    Link        *string   `json:"link"`
}

func RegisterSyncCallback(fn func(items []SyncItem) ([]SyncObject, error)) { ... }
```

Called before the client posts computed `user_object` rows to the prophet. The application receives the batch of `(id, type)` pairs and returns enriched objects with optional display metadata. The application cannot set `id` or `type` — those are read-only inputs. If no callback is registered, `sa_name`/`sa_description`/`sa_link` are null.

### 3.10 `client/http.go`

Every outbound request to the prophet carries:

```
SAuth-Client-Slug: <slug>
SAuth-Client-Secret: <secret>
SAuth-Seq: <last_applied_seq>
```

Every write wrapper returns `(result, seq, error)`. The seq is parsed from the prophet's `SAuth-Seq` response header.

### 3.11 `client/main.go` and HTTP Wrappers

Keep: `client/http.go`, `client/auth.go`, `client/verify.go`, `client/asset.go`, `client/me.go`, `client/user.go`, `client/relation.go`, `client/organization.go`, `client/location.go`, `client/claim.go`.

Update: every write wrapper returns `(result, seq, error)` instead of `(result, error)`.

### 3.12 `sync/` — NEW (internal)

Internal package. Not exported.

**`sync/payload.go`** — shared types:

```go
type SyncMessage struct {
    Type string          `json:"type"`
    Obj  json.RawMessage `json:"obj"`
}
```

The sync payload is always `[]SyncMessage`. Same shape for push, pull, snapshot, and upward sync.

**`sync/apply.go`**:

```go
func ApplyPayload(ctx context.Context, messages []SyncMessage, seq int64) error {
    // BEGIN
    // SET LOCAL samna_auth.replay_mode = 'true'
    // For each message: upsert by table name using bun ON CONFLICT DO UPDATE
    // UPDATE client_state.last_applied_seq = seq
    // COMMIT
    // Then: run post-apply cascade (see sync/cascade.go)
}
```

**`sync/cascade.go`** — post-apply recomputation:

```go
// CascadeAccess inspects the applied payload for changes to org/location/user
// level user_object rows (the types the prophet computes). For each change, it
// finds local application objects whose sa_owner_id or sa_location_id points at
// the changed object. For each affected application object, it calls
// compute_object_claims locally and upserts the resulting user_object rows.
// These local events flow upward to the prophet via the normal upward sync.
func CascadeAccess(ctx context.Context, messages []SyncMessage) error {
    // 1. Filter messages for type = "user_object" where the obj's object_type
    //    is "organization", "location", or "samna_user"
    // 2. For each changed (user_id, object_id, object_type):
    //    a. Query local registered tables for rows where
    //       sa_owner_id = object_id (if object_type = organization)
    //       sa_location_id = object_id (if object_type = location)
    //    b. For each affected app object: call compute_object_claims locally
    //    c. Upsert resulting user_object rows (NOT in replay_mode, triggers fire)
    //    d. Events appended to local user_object_event, flow upward
}
```

This is the mechanism by which a claim change on the prophet cascades to the disciple's application tables. See schema spec Section 9.13.

**`sync/wait.go`**:

```go
func WaitForSeq(ctx context.Context, seq int64) error {
    // Poll client_state.last_applied_seq at 50ms interval
    // Timeout from SAUTH_SEQ_TIMEOUT env var (default 5s)
}
```

**`sync/pull.go`**:

```go
func PullSnapshot(ctx context.Context) error {
    // GET /sam/sync/snapshot
    // ApplyPayload(response)
}

func PullSync(ctx context.Context, seq int64) error {
    // GET /sam/sync/:seq
    // ApplyPayload(response)
}
```

### 3.13 `hook/` — DELETE ENTIRELY

All files deleted. `handler.go`, `SyncOrganization`, `SyncLocation`, etc. all gone. Replaced by `echo.RegisterSyncHandler` and `sync.ApplyPayload`.

### 3.14 `echo/sync.go` — NEW

```go
// RegisterSyncHandler is the disciple's single sync endpoint handler.
// Mount it at whatever path matches the hook_uri configured on the prophet.
func RegisterSyncHandler(c echo.Context) error {
    // Parse SAuth-Object-Type header
    // Parse body as []SyncMessage
    // Call sync.ApplyPayload
    // Return 204
}
```

One function. Not a group registration. The application calls:

```go
e.POST("/webhooks/sauth", saecho.RegisterSyncHandler)
```

Or wherever the operator configured `hook_uri` on the prophet.

### 3.15 `echo/seq.go` — NEW

```go
func WaitForRequestSeq(c echo.Context) error {
    seqHeader := c.Request().Header.Get("SAuth-Seq")
    if seqHeader == "" { return nil }
    seq, _ := strconv.ParseInt(seqHeader, 10, 64)
    return sync.WaitForSeq(c.Request().Context(), seq)
}
```

Called at the top of disciple application handlers that need read-your-own-writes.

### 3.16 `middleware/`, `utils/`

No changes.

---

## 4. Sync Flows

### 4.1 Receiving Push From Prophet

Prophet POSTs to the disciple's sync endpoint (the path matching `hook_uri`):

```
POST /webhooks/sauth
SAuth-Object-Type: user_object
Body: [{ "type": "user_object", "obj": { "id": "claim_a:bookable_1:bookable", ... } }]
```

The handler calls `sync.ApplyPayload`, which upserts each row in replay_mode.

### 4.2 Snapshot Pull On Startup

`db.Init` calls `sync.PullSnapshot`, which GETs `/sam/sync/snapshot` on the prophet. The response is a `[]SyncMessage` array containing all object types. Applied in replay_mode.

### 4.3 Incremental Pull On Seq Mismatch

If the prophet returns 410 Gone (disciple is behind retention), the disciple calls `sync.PullSnapshot` to refresh. For non-critical catch-up, the disciple can call `sync.PullSync(lastSeq)` which GETs `/sam/sync/:seq`.

### 4.4 Upward Sync (Application Objects → Prophet)

When the disciple's triggers compute `user_object` rows for application tables:

1. Trigger stores locally.
2. Trigger appends to local `user_object_event`.
3. The client package collects pending events.
4. If `RegisterSyncCallback` is set, the callback is called with the batch of items. Application returns enriched `SyncObject` items.
5. Client POSTs to `POST /sam/sync` on the prophet with the `user_object` rows (sa_name/sa_description/sa_link from callback or from the table's sa_ columns).

### 4.5 Trigger Behavior On Disciple

When the disciple's application code writes to a registered table:
- `replay_mode` is NOT set.
- The trigger runs normally.
- Access is computed locally using synced claim data.
- `user_object`/`user_users`/`object_users` are updated locally.
- Events appended to local `user_object_event`.
- Local reads immediately see the new access.

When the disciple receives a sync payload from the prophet:
- `replay_mode` IS set.
- Triggers skip.
- Rows upserted directly.
- No events appended (trigger did not run).
- No upward sync (nothing new in the log).

---

## 5. Summary

| Path | Action |
|---|---|
| `db/claims.go` | Rewrite. GetAccess, HasAccess, GetObjectClaims. |
| `db/query.go` | Keep. |
| `db/user.go` | Rename GetUserByID → GetUser. |
| `db/organization.go` | Rename GetOrganizationByID → GetOrganization. |
| `db/location.go` | Rename GetLocationByID → GetLocation. |
| `db/models.go` | Drop 4, add 5. |
| `db/main.go` | Rewrite startup. Delete RegisterSyncFunc. |
| `client/sync.go` | **Delete**. |
| `client/callback.go` | **New**. RegisterSyncCallback. |
| `client/http.go` | Add SAuth headers, parse SAuth-Seq from responses. |
| `client/*.go` wrappers | Return (result, seq, error). |
| `sync/` | **New** (internal). payload, apply, wait, pull. |
| `hook/` | **Delete entirely**. |
| `echo/sync.go` | **New**. RegisterSyncHandler. |
| `echo/seq.go` | **New**. WaitForRequestSeq. |
| `middleware/` | Keep. |
| `utils/` | Keep. |
