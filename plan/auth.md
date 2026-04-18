# Middleware Updates

## Model changes

- `User` → `SamnaUser` (table: `samna_auth.samna_user`)
- ObjectField added (table: `samna_auth.object_field`, synced)
- UserField simplified (table: `samna_auth.user_field`, fields: field_id, user_id, value, synced)
- ClientClaim removed (claims assigned via user_claim)
- ActiveObject and ActiveUser removed (old view models)
- ActiveClaim renamed to UserClaimAccess (scan target for get_access and GetUserClaims)

## Sync

- `client_claim` removed from ApplyPayload
- `object_field` and `user_field` added to ApplyPayload

## Search

- `samna_auth.search` exposed via `db.Search(ctx, userID, query, objectTypes, accessLevel, limit, offset)`

## Client functions

- `GetClaimSearch` removed (search is now a standalone endpoint)
