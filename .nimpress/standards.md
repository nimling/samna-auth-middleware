---
title: Standards and compliance
description: Which RFCs, specifications and compliance duties the middleware implements, read from the code, with the anchor that proves each one and the gap that remains.
visibility: hidden
tags: compliance, standards, rfc, middleware, claimius, redaction
lastUpdated: true
---

## Scope

This page covers the Go module `github.com/nimling/samna-auth-middleware/v2` only: `smw`, `shared`, `client`, `dpop`, `disciple`, `prophet`, `bff`, `audit`, `analytics`, `notification`, `uptime` and the two `claimius` SQL sets. Every status is read from code; anchors are repo relative unless they name another repo. The platform wide pages are [Compliance](/ecosystem/compliance) and [Standards](/ecosystem/standards).

## RFCs

### RFC 7519, JSON Web Token

1. Status: `built`

2. Evidence: `smw/token.go:37` parses with `golang-jwt/jwt/v5`, which checks `exp` and `nbf` when present; `smw/token.go:51` checks `iss` against the discovered issuer; `smw/token.go:55` requires the app slug in `aud`; `smw/token.go:78` requires `sub` to be a UUID.

### RFC 8725, JWT best current practices

1. Status: `partial`

2. Evidence: the algorithm is pinned through the JWK, `pkg/encryption/jwks.go:29` publishes `alg: RS256` and `keyfunc` v3 refuses a token whose `alg` differs from the key's; issuer and audience are checked at `smw/token.go:51` and `smw/token.go:55`.

3. Gap: `smw/token.go:37` sets no `jwt.WithValidMethods`, no `jwt.WithExpirationRequired` and no `typ` check, so a token without `exp` validates and the allow list depends on the JWKS carrying `alg`.

### RFC 7515, JSON Web Signature

1. Status: `built`

2. Evidence: signature verification through `jwt.ParseWithClaims` at `smw/token.go:37` and `dpop/dpop.go:76`; signing with RS256 at `getObjectAssetToken`, `prophet/echo/object_asset.go:340-353`.

### RFC 7517, JSON Web Key

1. Status: `built`

2. Evidence: the JWKS is fetched and refreshed by `keyfunc.NewDefault` at `smw/main.go:73` and `disciple/echo/asset.go:127`; the prophet publishes its set with `kid`, `alg` and `use` at `pkg/encryption/jwks.go:13`; a DPoP `jwk` header is parsed at `dpop/dpop.go:60`.

### RFC 7518, JSON Web Algorithms

1. Status: `built`

2. Evidence: RS256 for access tokens (`pkg/encryption/jwks.go:29`); RS256, PS256 and ES256 for DPoP proofs (`dpop/dpop.go:41`).

### RFC 7638, JWK thumbprint

1. Status: `built`

2. Evidence: `dpop/dpop.go:65` computes the SHA256 thumbprint and `dpop/dpop.go:69` encodes it base64url; `smw/token.go:68` compares it to `cnf.jkt`.

### RFC 9068, JWT profile for OAuth 2.0 access tokens

1. Status: `partial`

2. Evidence: `iss`, `aud`, `sub` and `exp` are read at `smw/token.go:35`; `scope` and `client_id` are declared on `TokenClaims` at `smw/models.go:92`.

3. Gap: no `typ: at+jwt` check, `exp`, `iat` and `jti` are not required, and `scope` and `client_id` are parsed but never used.

### RFC 6750, bearer token usage

1. Status: `partial`

2. Evidence: the `Authorization: Bearer` header only, no query or form token, at `smw/token.go:18`; a refused token answers 401 at `smw/main.go:130`.

3. Gap: no `WWW-Authenticate` header with `error="invalid_token"` or `insufficient_scope` on any 401 or 403.

### RFC 6750 section 5.3 and OWASP ASVS V3.1.1, codes in URLs

1. Status: `partial`

2. Evidence: the URL carries an access code, never a token, and the code is 256 random bits from `GetCodeValue` (`shared/code.go:178`) stored only as its SHA-256 (`GetCodeHash`, `shared/code.go:186`): an invite code in `claimius.code.hash` (`disciple/database/V1.1__claimius_baseline.sql:1194`, `prophet/database/V1.1__claimius_baseline.sql:1547`), the code of a public grant in `user_claim.options.samk.hash` on the anonymous user's grant (`disciple/database/V1.1__claimius_baseline.sql:326`); a navigation carrying `samk` is answered `303` to the URL without it (`respondCodeStripped`, `smw/code.go:87`) and the code moves to a `__Host-` cookie (`smw/code.go:58`); every answer carrying or following a code sends `Referrer-Policy: no-referrer` (`smw/code.go:100`, `smw/code.go:212`); the guard takes `samk` and `SAuth-Code` off the request before a handler, a logger or a span reads them (`readCode`, `smw/code.go:35-56`), and the span records the path only; API calls send the header `SAuth-Code` instead of the query; a public grant's code is accepted only from the carriers the grant names, query, header or cookie (`shared/claims.go:32-36`), counts only while the request presents it, is valid by the grant's `starts_at` and `ends_at`, spends no count and gives exactly what the grant's claim holds (`get_access`, `disciple/database/V2.4__claimius_functions_external.sql:16`); an invite code resolves only inside its window, below its count and until revoked (`GetCodeLive`, `shared/code.go:191`), and a revoke reaches every disciple through the feed and clears the resolve cache (`shared/apply.go:25`); `claimius.code` is invite only (`shared/code.go:27`, `shared/code.go:326`, `disciple/database/V1.1__claimius_baseline.sql:1218`). This follows the W3C capability URL practice: unguessable, short lived where possible, revocable, moved out of the address bar on first use, and kept out of `Referer`.

3. Gap: the first request still carries the code in its URL, so a proxy or load balancer log in front of the service and the browser history entry of that first request can hold it; a public grant without `ends_at` lives until the grant is deleted or its `options` cleared, and an invite code without `ends_at` until revoked or used up.

### RFC 9449, DPoP

1. Status: `partial`

2. Evidence: `dpop/dpop.go:36` verifies `typ` `dpop+jwt` (47), the embedded `jwk` (52), the signature (76), `htm` (84), `htu` (89), `iat` within 60 s (94), `jti` replay through `claimius.dpop_jti` (101, `dpop/postgres.go:21`, `disciple/database/V1.1__claimius_baseline.sql:1238`) and `ath` (116); `smw/token.go:59` requires a proof whose thumbprint matches `cnf.jkt`.

3. Gap: no server nonce (`DPoP-Nonce`, `use_dpop_nonce`), no `Authorization: DPoP` scheme (only `Bearer ` is read at `smw/token.go:19`), no `WWW-Authenticate: DPoP` challenge, and `htu` is compared with `r.Host` and `r.TLS`, which differ behind a TLS terminating proxy.

### RFC 8252, OAuth for native apps

1. Status: `built`

2. Evidence: a cli signs in through the service it belongs to: `GetLogin` takes the cli's own S256 `cli_challenge` in place of the service's verifier (`disciple/echo/auth.go:39`), `GetCallback` hands the code to the cli's loopback `cli_redirect` with its `cli_state`, or shows it to paste, without exchanging it (`disciple/echo/auth.go:99`), only `http` on `127.0.0.1`, `::1` or `localhost` with a port is accepted (`disciple/echo/cli.go:143`), and `POST /api/auth/cli/token` exchanges the code with the cli's verifier and the service's own secret (`disciple/echo/cli.go:59`). The disciple stores nothing of the login.

3. Gap: the cli is not a registered public client of its own at the prophet; the service exchanges for it as a confidential client.

### RFC 6749, OAuth 2.0 (as client)

1. Status: `partial`

2. Evidence: authorization code at `client/auth.go:201`, refresh at `client/auth.go:210` used by `smw/session.go:122` and `disciple/echo/cli.go:59`, client credentials at `client/auth.go:217`; `state` is generated at `disciple/echo/auth.go:21` and compared with its cookie at `disciple/echo/auth.go:75`.

3. Gap: the code and refresh exchanges authenticate with the custom headers `SAuth-App-Slug` and `SAuth-App-Secret` (`client/auth.go:153`), not `client_secret_basic` or `client_secret_post`; token errors are parsed into the RFC 6749 section 5.2 codes at `client/auth.go:116`.

### RFC 7636, PKCE

1. Status: `built`

2. Evidence: a 32 byte verifier at `client/pkce.go:18`, the S256 challenge at `client/pkce.go:26`, `code_challenge_method=S256` at `client/pkce.go:47`, the verifier sent at `client/auth.go:206`.

### RFC 7009, token revocation

1. Status: `partial`

2. Evidence: the browser logout calls the prophet's own logout at `client/auth.go:56`; a cli signs out through `POST /api/auth/cli/revoke`, which sends RFC 7009 `token` and `token_type_hint` to the prophet's revocation endpoint (`disciple/echo/cli.go:102`, `client/auth.go:225`).

3. Gap: the browser logout still ends every session of the person on the app through the prophet's logout instead of revoking its own refresh family.

### RFC 7662, token introspection

1. Status: `not applicable`

2. Evidence: tokens are validated locally against the JWKS at `smw/token.go:35`; searched `introspect` and `"active"` with no introspection call.

### RFC 7617, Basic authentication

1. Status: `built`

2. Evidence: inbound `Authorization: Basic` decoded at `smw/client_auth.go:102` and checked against the bcrypt hash of the replicated `samna_client` at `smw/client_auth.go:171`; outbound at `client/http.go:274`.

3. Gap: the scheme match is case sensitive and the id and secret are not form decoded as RFC 6749 section 2.3.1 requires.

### RFC 8705, mutual TLS certificate bound tokens

1. Status: `partial`

2. Evidence: `cnf.x5t#S256` is compared with the SHA256 of the peer certificate at `smw/token.go:71` and `dpop/dpop.go:156`.

3. Gap: only `r.TLS` is read, so behind Container Apps ingress the thumbprint is always empty; no mTLS client authentication.

### RFC 8707, resource indicators

1. Status: `missing`

2. Evidence: searched `"resource"` in `client/` and `smw/`; no `resource` parameter is sent and `aud` is matched to the app slug only.

### RFC 9728, OAuth protected resource metadata

1. Status: `missing`

2. Evidence: searched `oauth-protected-resource`, `well-known` and `authorization_servers`; the only well known path is the OIDC discovery read at `shared/main.go:320`.

3. Gap: no `/.well-known/oauth-protected-resource` document and no `resource_metadata` in a 401 challenge, which the MCP authorization spec requires.

### RFC 9470, step up authentication challenge

1. Status: `missing`

2. Evidence: searched `acr`, `acr_values`, `max_age`, `auth_time`, `insufficient_user_authentication`; none read or emitted.

### RFC 8176, authentication method reference values

1. Status: `missing`

2. Evidence: searched `amr`; `TokenClaims` at `smw/models.go:92` carries no `amr`.

### RFC 6265, cookies

1. Status: `partial`

2. Evidence: `utils/cookie.go:57` writes `HttpOnly`, `Secure` and `SameSite` (Lax by default, `SAUTH_COOKIE_SAMESITE`); CSRF double submit at `enforceCSRF`, `smw/session.go:30`; the access, refresh, PKCE and state cookies at `disciple/echo/auth.go:46` and `disciple/echo/auth.go:110`; the access code cookie `__Host-sauth_samk` is `HttpOnly; Secure; SameSite=Lax; Path=/` with no `Domain` and expires with its grant at `smw/code.go:58`.

3. Gap: `Secure` is off on the session cookies when no cookie domain resolves (`utils/cookie.go:43`) unless `SAUTH_COOKIE_SECURE=true` forces it (`utils/cookie.go:48-50`), and only the access code cookie uses the `__Host-` prefix.

### RFC 6797, HSTS

1. Status: `missing`

2. Evidence: searched `Strict-Transport-Security` and `hsts`; no handler or middleware sets it.

### RFC 9457, problem details

1. Status: `missing`

2. Evidence: errors use the envelope `{data, error: {Message}, request_id}` at `pkg/respond/main.go:18` and plain strings at `smw/main.go:130`; searched `application/problem+json`.

### RFC 9110 and RFC 9111, conditional requests and caching

1. Status: `partial`

2. Evidence: on asset data only: `If-None-Match` and `If-Modified-Since` forwarded (`client/http.go:462`), evaluated at `disciple/echo/asset.go:242`, revalidated at `disciple/echo/asset.go:138`, freshness and `no-store`, `private`, `no-cache` at `disciple/echo/asset.go:264`; the prophet answers 304 from blob storage at `prophet/echo/object_asset.go:408`.

3. Gap: no `ETag` or `If-Match` on API reads and writes, so no lost update protection.

### RFC 6585, 429 Too Many Requests

1. Status: `partial`

2. Evidence: a fixed window per address and anonymous cookie at `smw/rate_limit.go:24-30` and `smw/rate_limit.go:44-50`, the limit `SAUTH_RATE_LIMIT` per `SAUTH_RATE_PERIOD`, 60 per minute by default (`smw/rate_limit.go:14-17`), answering 429 with `Retry-After` at `smw/rate_limit.go:52-55`; every anonymous request is counted (`smw/code.go:194`) and a code presented outside the cookie too (`smw/code.go:113`); `smw.RateLimit` is mounted on the cli token and revoke routes (`disciple/sam/register.go:64-65`) and `smw.RateLimiter` on the BFF subscription route (`bff/main.go:76`); an unanswerable resolve is 503, never a pass (`smw/code.go:94-97`).

3. Gap: in memory per process, and `/api/auth/login` carries none.

### RFC 9562, UUID

1. Status: `built`

2. Evidence: version 4 through `gen_random_uuid()` (`shared/models.go:352`) and `uuid.New()`; no version 7.

### RFC 9421, HTTP message signatures, and HMAC webhooks

1. Status: `not applicable`

2. Evidence: searched `Signature-Input`, `webhook`, `X-Hub-Signature`; the middleware sends no webhooks. HMAC SHA256 is used only to hash app secrets (`pkg/encryption/crypto.go:110`).

## Specifications

### OpenID Connect Discovery 1.0 and RP initiated logout 1.0

1. Status: `built`

2. Evidence: issuer, token endpoint and `jwks_uri` read from `/.well-known/openid-configuration` at `shared/main.go:320`; `end_session` with `post_logout_redirect_uri` at `client/pkce.go:57`.

### WHATWG server sent events

1. Status: `partial`

2. Evidence: `notification/stream.go:18` sets `text/event-stream`, writes `data:` frames at `notification/stream.go:46` and comment heartbeats at `notification/stream.go:51`; the prophet negotiates it at `prophet/echo/notification.go:22`.

3. Gap: no `id:` field and no `Last-Event-ID` resume, so a reconnect loses events sent in between.

### W3C Trace Context

1. Status: `partial`

2. Evidence: `smw/telemetry.go:11` mounts `otelecho`, which extracts `traceparent` through the propagator `nimtel.Init` installs (`nimtel@v1.0.0/main.go:89`).

3. Gap: the prophet client at `client/http.go:211` uses a plain `http.Client`, so `traceparent` is not injected on calls to the prophet and a trace breaks at the disciple.

### OpenTelemetry

1. Status: `built`

2. Evidence: `smw/telemetry.go:11` `TelemetryMiddleware`, `smw/telemetry.go:15` stamps the organization on the span context, `shared/access.go:66` stamps it on every granted access check.

### MCP authorization specification

1. Status: `missing`

2. Evidence: no MCP code in this repo (searched `mcp`); it depends on RFC 9728 and RFC 8707, both missing above.

### NIST SP 800-162, ABAC

1. Status: `partial`

2. Evidence: object attributes through bindings and scopes: `ScopeAccess` over a jsonpath at `shared/access.go:174`, per field masks in `_apply_denies` (`prophet/database/V2.2__claimius_functions_internal.sql:911`), time bounded grants `p_starts_at` and `p_ends_at` (`insert_user_claim`, `prophet/database/V2.4__claimius_functions_external.sql:2692`).

3. Gap: no environment or subject attributes beyond time, and field masks are not enforced in Go (see Redaction).

### NIST RBAC (ANSI INCITS 359)

1. Status: `partial`

2. Evidence: claims are roles assigned through `user_claim`, permissions through `claim_object`, hierarchy through `inherits` and composition with `_check_claim_composition` (`disciple/database/V2.3__claimius_functions_access.sql:502`, `prophet/database/V2.3__claimius_functions_access.sql:523`), levels, the higher of which wins.

3. Gap: no static or dynamic separation of duty constraints.

### OWASP ASVS V4, access control

1. Status: `built`

2. Evidence: deny by default, `ErrNoAccess` at `shared/access.go:17` maps to 403 on writes and a filter on reads; every write checks through `MutationAccess` at `shared/access.go:41`; CSRF at `enforceCSRF`, `smw/session.go:30`.

### OWASP ASVS V8, data protection

1. Status: `partial`

2. Evidence: secrets AES GCM at rest (`shared/cipher.go:12`, `aesEncrypt` at `shared/secret.go:65`), the app and organization keys that seal them wrapped under `SAUTH_MASTER_KEY` (`pkg/encryption/master.go:32`, opened at `pkg/encryption/crypto.go:54`), a secret opened only in process for a user with read through `get_secret_value` (`shared/secret.go:144`, `disciple/database/V2.4__claimius_functions_external.sql:1095`) with the app's own seed from `GET /sam/app` held in memory (`GetSamApp`, `prophet/echo/app.go:12`; `client.GetAppSeed`, `client/main.go:50`), client secrets bcrypt (`smw/client_auth.go:171`), Postgres `sslmode` defaults to `verify-full` (`shared/main.go:232`).

3. Gap: field level masks are not applied in Go.

## Redaction

Status: `partial`.

1. The SQL settles one pair into a mask per field. `claimius._apply_denies` (`prophet/database/V2.2__claimius_functions_internal.sql:911` to `1040`, `disciple/database/V2.2__claimius_functions_internal.sql:842` to `971`) subtracts denies by level and by scope, closes each remainder under implication and returns `scope` as `{field: mask, "*": mask}`, NULL meaning the whole object. The rule that a field lacking read is withheld and a field lacking write is refused is stated in `.nimpress/claimius/redaction.md:65`.

2. Go carries the settled scope but never reads it. `AccessClaim.Scope` is a raw `*json.RawMessage` (`shared/models.go:624`), filled by the batch gate (`MutationAccessMany`, `shared/access.go:106-172`) and `ScopeAccess` (`shared/access.go:174`); `AccessClaimSet.Has` (`shared/access.go:94`) tests only `Access`. There is no parse, apply or withhold function in `shared/`, `smw/` or `disciple/`, and no write path refuses a body field whose mask lacks the write bit.

3. Consumers carry their own engines: `bookable_server/internal/db/redaction.go:36` `ParseRedactionScope` and `:120` `ResolveRedaction` with resolver directives, copied into `bookable_server_test`, `bookable_server_assets` and `bookable_server_reporting`; `nimflow/redact/scope.go:18` `ParseScope`, `:67` `Redact` over maps with dotted paths and `:134` `RedactRows` over structs.

4. The gap to a generic middleware package: parse the settled `{field: mask, "*": mask}` into one typed scope on `AccessClaim`; apply it to structs by `bun` column name and to maps by dotted path; refuse a write whose body names a field without the write bit, with a 403; and carry the directives bookable resolves today, so every disciple withholds the same fields the SQL computed.

## Compliance duties

1. ISO/IEC 27001:2022 A.5.15, access control: `built`. Claimius masks, bindings and denies answered by `get_access` and `mutation_access` through `shared/claims.go:98` and `shared/access.go:41`.

2. ISO/IEC 27001:2022 A.5.18, access rights: `partial`. Grants are created through `insert_user_claim` with optional expiry (`prophet/database/V2.4__claimius_functions_external.sql:2692`) and soft deleted by `delete_user_claim`; no access review export.

3. ISO/IEC 27001:2022 A.8.3, information access restriction: `partial`. Object level restriction is built; field level restriction is computed in SQL and not applied in Go (Redaction 2).

4. ISO/IEC 27001:2022 A.8.5, secure authentication: `partial`. JWT against the JWKS (`smw/token.go:35`), PKCE (`client/pkce.go:26`), DPoP (`dpop/dpop.go:36`), bcrypt client secrets (`smw/client_auth.go:171`), CSRF (`enforceCSRF`, `smw/session.go:30`); gaps are `exp` not required (`smw/token.go:37`) and `Secure` off without a cookie domain (`utils/cookie.go:43`) unless `SAUTH_COOKIE_SECURE=true` forces it (`utils/cookie.go:48-50`).

5. ISO/IEC 27001:2022 A.8.11, data masking: `partial`. See Redaction.

6. ISO/IEC 27001:2022 A.8.10, information deletion: `partial`. `claimius.prune(JSONB)` at the end of `prophet/database/V2.4__claimius_functions_external.sql` and `disciple/database/V2.4__claimius_functions_external.sql` deletes, from a runtime table over `size_mb`, every row older than its group's days, `audit` by `sa_created_at`, called once an hour by one leader from `prophet/sam/main.go` and `disciple/sam/main.go` through `shared/retention.go` `PruneLoop`; a host adds its own tables in `public.prune(JSONB)`. `user_activity` has no sweep, and application data is never aged by design.

7. ISO/IEC 27001:2022 A.8.15, logging: `partial`. `claimius.audit` partitioned by month (`disciple/database/V1.1__claimius_baseline.sql:904`, `:916`), written in the caller's transaction by `shared/audit.go:10`, and rows past the `audit` retention are deleted by `sa_created_at` in `claimius.prune` (`disciple/database/V2.4__claimius_functions_external.sql:4994`); no partition is dropped. `audit/audit.go:11` writes in a goroutine with three retries and drops the row on failure, and live rows carry no hash chain.

8. ISO/IEC 27001:2022 A.5.33, protection of records: `missing`. `audit` is deleted by age like every other runtime table (`claimius.prune`, `prophet/database/V2.4__claimius_functions_external.sql`), and no record is exported before deletion or written to an append only store.

9. ISO/IEC 27001:2022 A.8.16, monitoring: `built`. OpenTelemetry at `smw/telemetry.go:11`, uptime heartbeat at `uptime/uptime.go:42`, health route at `disciple/sam/register.go:40`.

10. GDPR Art. 5(1)(c), data minimisation: `partial`. `user_identity.data` is not carried on the feed (`disciple/database/V1.1__claimius_baseline.sql:383`, `prophet/echo/sync.go:121`, `:140`) and raw analytics events age through `claimius.prune`, 365 days by default (`prophet/database/V2.4__claimius_functions_external.sql:5960`); every disciple still replicates the full `samna_user` profile, email and phone included, whether it needs them or not.

11. GDPR Art. 5(1)(e), storage limitation: `partial`. Each personal runtime table of claimius has a period and an hourly sweep in `claimius.prune` (`prophet/database/V2.4__claimius_functions_external.sql`): `notification` closed or soft deleted, `analytics_session`, `analytics_event`, `analytics_heat`, `analytics_capture` and `audit` by age, `consent` once revoked, the days per table or kind from the app's settings. `user_activity` has no sweep.

12. GDPR Art. 25, data protection by design and by default: `partial`. Deny by default (`shared/access.go:17`), analytics behind consent (`analytics/consent.go:61`); field masks are not enforced by default in Go.

13. GDPR Art. 30, records of processing: `missing`. No register of processing in code; the audit trail records claimius writes, not processing purposes.

14. GDPR Art. 15, right of access: `partial`. A user reads its own profile, claims, relations, organizations, locations, assets and audit through `/me` (`disciple/sam/register.go:17-26`); no export contract gathers `user_field`, `notification`, `consent`, `analytics_*` and the host's own tables into one answer.

15. GDPR Art. 17, right to erasure: `partial`. `DeleteUser` soft deletes `samna_user` and `user_identity` (`shared/user.go:262-321`, `prophet/echo/user.go:307`); no hard delete or anonymisation of the row, `user_field`, `user_relation`, `notification`, `audit.sa_created_by`, `consent` or `analytics_*`, and no hook asks the host to erase its own rows.

16. GDPR Art. 32, security of processing: `partial`. TLS to Postgres by default (`shared/main.go:232`), secrets AES GCM (`shared/cipher.go:12`, `utils/aes.go:12`) under keys wrapped at rest (`pkg/encryption/master.go:32`) and opened only for a reader (`shared/secret.go:144`), DPoP and mTLS binding (`smw/token.go:59`).

## Personal data

1. `claimius.samna_user` (`disciple/database/V1.1__claimius_baseline.sql:69`, `prophet/database/V1.1__claimius_baseline.sql:147`): `first_name`, `last_name`, `user_name`, `user_image`, `phone`, `email`, `external_id`, `config` and `profile`. Replicated to every disciple.

2. `claimius.user_identity` (`disciple/database/V1.1__claimius_baseline.sql:383`): `external_id`, `id_claim`, `data`; `data` stays on the prophet.

3. `claimius.user_field` (`disciple/database/V1.1__claimius_baseline.sql:1035`): `value` per user and field.

4. `claimius.user_relation` (`disciple/database/V1.1__claimius_baseline.sql:1053`): `description`, `pinned`, `priority`, `used_at` per user and object.

5. `claimius.user_object`, `claimius.user_activity` and `claimius.usage_diff` (`disciple/database/V1.1__claimius_baseline.sql:495`, `:829`, `:845`): what a user may reach, `last_used_at`, `used_count`; behavioural data.

6. `claimius.audit` (`disciple/database/V1.1__claimius_baseline.sql:904`): `sa_created_by` and free text `message`.

7. `claimius.notification` (`disciple/database/V1.1__claimius_baseline.sql:1246`): `user_id`, `title`, `description`, `link`.

8. Prophet only: `claimius.session.data` (`prophet/database/V1.1__claimius_baseline.sql`); `claimius.consent` with `ip`, `user_agent`, `anonymous_id` (`prophet/database/V1.1__claimius_baseline.sql`); `claimius.analytics_session` with `ip`, `user_agent`, `referrer`, `language`, `timezone` (`prophet/database/V1.1__claimius_baseline.sql`); `claimius.analytics_event.payload` with click text and input signals (`prophet/database/V1.1__claimius_baseline.sql`); `claimius.analytics_capture.image`, a rendered page (`prophet/database/V1.1__claimius_baseline.sql:1769`).

9. `claimius.guest_identity` (`disciple/database/V1.1__claimius_baseline.sql:422`, `prophet/database/V1.1__claimius_baseline.sql:537`): `email`, `phone` and `ends_at` of a guest user, on both roles; never synced, soft deleted by `claimius.prune` once `ends_at` has passed and deleted as kind `log` after its days.

10. Export: no contract. The nearest are the `/me` reads at `disciple/sam/register.go:17-26` and `GetMe` at `shared/me.go:19`.

11. Erase: soft delete only, `DeleteUser` at `shared/user.go:262-321` sets `sa_deleted_at` on `samna_user` and `user_identity`; `merge_user` (`prophet/database/V2.4__claimius_functions_external.sql:2860`) is the only path that removes a `samna_user` row of a signed in user, and `claimius.prune` removes soft deleted guest rows after their days. The runtime tables expire by age under the retention job (`PruneLoop`, `shared/retention.go:32`); `consent` is kept.
