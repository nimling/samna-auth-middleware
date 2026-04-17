# Middleware Model Updates

Align middleware models with samna_auth server V1 changes.

## Renames

- `User` → `SamnaUser` (table: `samna_auth.samna_user`)
- `AppField` → `ObjectField` (table: `ObjectField`, fields: app_id, object_type, name, description, json_schema, required, value)
- `UserField` simplified (fields: field_id, user_id, value)

## Removed from middleware

- `UserApp`/`AppUser` is a prophet side concept. The middleware does not need it. Disciples work with SamnaUser only.

## New models

- `OrganizationModule` (table: `OrganizationModule`, binds Module to Organization)

## Column changes

- `hook_uri` → `sync_uri` on App
- `app_image` text replaces `asset_id`/`asset_url` on App
- Provider: added `slug`, `type` (internal/external), `banner_url`, `logo`, `login_btn`. Removed `client_id`/`client_secret_blob`.
- OrganizationProvider: binding table with `provider_id`, `organization_id`, `app_id`, `oauth_client_id`, `oauth_client_secret`
- All tables have `sa_owner_id` and `sa_created_by`

## Client functions

Update HTTP paths and request/response types to match renamed endpoints on samna_auth server.

## Echo handlers

Update exported handler functions to match renamed types and new endpoints (relation CRUD, access endpoints, search).
