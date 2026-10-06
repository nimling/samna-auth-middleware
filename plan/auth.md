# Middleware Updates

## Model changes

- `User` → `SamnaUser` (table: `claimius.samna_user`)
- ObjectField added (table: `claimius.object_field`, synced)
- UserField simplified (table: `claimius.user_field`, fields: field_id, user_id, value, synced)
- ClientClaim removed (claims assigned via user_claim)
- ActiveObject and ActiveUser removed (old view models)
- ActiveClaim renamed to AccessClaim (scan target for get_access)

## Sync

- `client_claim` removed from ApplyPayload
- `object_field` and `user_field` added to ApplyPayload

## Search

- `claimius.search` exposed via `db.Search(ctx, userID, query, objectTypes, requiredAccess, limit, offset)`

## Client functions

- `GetClaimSearch` removed (search is now a standalone endpoint)
