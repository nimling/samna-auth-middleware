# How a consumer connects

1. A disciple binds to one prophet: `SAUTH_ENDPOINT` is `http://localhost:4212` in dev, `https://<tst-auth-host>` on tst, `https://auth.samna.io` on prd.
2. `SAUTH_APP_SLUG` and `SAUTH_APP_SECRET` name the app; the app and its secret come from the workspace `apps` skill, never a seed. `SAUTH_REDIRECT_URI` and `SAUTH_PAGE_REDIRECT` are the login return.
3. `PG*` is the consumer's own database holding its replicated `claimius`; `SAUTH_REPLICA_DSN` an optional read replica.
4. Sync is pull only: `sam.SyncStart` reads `/sam/sync/:cursor` from the prophet. One puller per database.
5. `SAUTH_DEBUG_USER_ID` and `SAUTH_DEBUG_CLIENTS` turn on debug mode, which skips the sync and acts as the debug user. Set only in a local `.env`.
6. `SAUTH_CLIENT_ID` and `SAUTH_CLIENT_SECRET` are the consumer's machine client for service to service calls.
7. Telemetry: the host calls `nimtel.Init` and mounts `smw.TelemetryMiddleware()` before the auth middleware; the Container Apps env supplies `OTEL_EXPORTER_OTLP_*`.
8. Consumers on v2: samna_auth (prophet), bookable_server_test, nimployer, samna-sync, samna_connect, samna_sign, hvergel, samna-developer. `bookable_server` and `samna.io` stay on v1. `../.claude/scripts/repos.sh` prints every pin.
