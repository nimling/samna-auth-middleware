---
title: Compliance checks
description: The checks the Samna Auth middleware security checkpoint runs against every disciple's token, access, redaction and audit path, each with its method, pass criterion and today's result.
visibility: hidden
tags: compliance, audit, checks, middleware, disciple, claimius, redaction, dpop
lastUpdated: true
---

## Scope

These checks cover the Go module `github.com/nimling/samna-auth-middleware/v2` that every disciple embeds: bearer and DPoP validation in `smw` and `dpop`, the outbound OAuth client in `client`, the browser and cli login in `disciple/echo`, cookies in `utils`, claimius access and the settled field masks in `shared` and the two SQL sets, audit, analytics, notification and telemetry. The inventory of standards, statuses and evidence is [Standards and compliance](./standards.md); the platform map is [Compliance](/ecosystem/compliance); the roadmap checkpoint these checks serve is [Security audit of Samna Auth](/ecosystem/roadmap/security-audit-auth). Go tests run through `just test <package>` or `just test` for the database suites, hurl through `just test-hurl`, and end to end checks on a disciple deployed to tst on the same release. `Today` is read from the code and the inventory; `passes` means the code decides the check, `untested` means only a run decides it.

## Tokens

### C1, issuer and audience

1. Standard: RFC 7519 sections 4.1.1 and 4.1.3; RFC 8725 sections 3.8 and 3.9.

2. Check: a token from another issuer or without the app slug in `aud` is refused.

3. Method:

```bash
sed -n 49,57p smw/token.go
just test smw
```

4. Pass: both comparisons return an error before any claim is used, and the suite passes.

5. Today: `passes`, `smw/token.go:51`, `smw/token.go:55`.

### C2, algorithm allow list

1. Standard: RFC 8725 section 3.1.

2. Check: the parser itself accepts only RS256, independent of whether the JWKS carries `alg`.

3. Method:

```bash
grep -n 'WithValidMethods' smw/token.go
```

4. Pass: `jwt.WithValidMethods([]string{"RS256"})` on `ParseWithClaims`.

5. Today: `fails`, `smw/token.go:37` passes no parser options; the pin rests on `alg` in the prophet's JWKS, `pkg/encryption/jwks.go:29`.

### C3, expiry required

1. Standard: RFC 9068 section 4; RFC 7519 section 4.1.4.

2. Check: a signed token without `exp` is refused.

3. Method:

```bash
grep -n 'WithExpirationRequired' smw/token.go
```

4. Pass: the option is set, or an explicit nil check on `claims.ExpiresAt` follows the parse.

5. Today: `fails`, `smw/token.go:37` checks `exp` only when present.

### C4, access token typ

1. Standard: RFC 9068 section 4; RFC 8725 section 3.11.

2. Check: a token whose header `typ` is not `at+jwt` is refused.

3. Method:

```bash
grep -n '"typ"' smw/token.go
```

4. Pass: a header check on `typ`.

5. Today: `fails`, none in `smw/token.go`; the prophet still signs `JWT`.

### C5, scope and client_id enforced

1. Standard: RFC 9068 section 4; ISO/IEC 27001:2022 A.8.3.

2. Check: a route can require a scope and a machine token's `client_id` is checked against the replicated client.

3. Method:

```bash
grep -rn 'claims.Scope\|claims.ClientID' smw shared disciple
```

4. Pass: a read of each field on the request path.

5. Today: `fails`, both declared on `TokenClaims` at `smw/models.go:92` and never read.

### C6, bearer in the header only

1. Standard: RFC 6750 sections 2.1 and 5.3.

2. Check: the token is read from `Authorization: Bearer` and never from the query or a form field.

3. Method:

```bash
sed -n 17,22p smw/token.go
grep -rn 'QueryParam("access_token")\|FormValue("access_token")' smw disciple
```

4. Pass: the header prefix check and no query or form read.

5. Today: `passes`, `smw/token.go:18`.

### C7, challenge on 401 and 403

1. Standard: RFC 6750 section 3; RFC 9449 section 7.1.

2. Check: a refused token answers with `WWW-Authenticate` carrying `error="invalid_token"`, and a refused scope with `insufficient_scope`.

3. Method: against a disciple on tst:

```hurl
GET https://<tst-connect-host>/api/me
Authorization: Bearer invalid
HTTP 401
[Asserts]
header "WWW-Authenticate" contains "error=\"invalid_token\""
```

4. Pass: the header is present.

5. Today: `fails`, `smw/main.go:130` answers the plain string `Unauthorized`.

### C8, authentication context

1. Standard: RFC 9470 section 3; RFC 8176 section 2.

2. Check: `TokenClaims` carries `acr`, `amr` and `auth_time`, and a route can demand a level and answer `insufficient_user_authentication`.

3. Method:

```bash
grep -n 'ACR\|AMR\|AuthTime' smw/models.go
grep -rn 'insufficient_user_authentication' smw
```

4. Pass: the fields and the challenge exist.

5. Today: `fails`, `smw/models.go:92` has none of them.

## Sender constraint

### C9, DPoP proof validation

1. Standard: RFC 9449 section 4.3.

2. Check: a proof is refused on wrong `typ`, missing `jwk`, bad signature, `htm` or `htu` mismatch, `iat` outside 60 seconds, a replayed `jti` or a wrong `ath`, and its thumbprint must equal `cnf.jkt`.

3. Method:

```bash
sed -n 36,120p dpop/dpop.go
sed -n 59,70p smw/token.go
```

4. Pass: one refusal per step, in that order, and the thumbprint comparison in `ValidateToken`; a `dpop` test package covering each step.

5. Today: `passes`, `dpop/dpop.go:47`, `dpop/dpop.go:76`, `dpop/dpop.go:84`, `dpop/dpop.go:89`, `dpop/dpop.go:101`, `dpop/dpop.go:116`, `smw/token.go:68`; no `dpop/*_test.go` exists, so the step list is proven by inspection only.

### C10, DPoP htu behind the ingress

1. Standard: RFC 9449 section 4.3, step 9.

2. Check: a proof whose `htu` is the public HTTPS URL validates on a disciple behind Container Apps ingress.

3. Method: on tst, call a disciple with a DPoP bound token and a proof for `https://<service>.<tst-domain>/api/me`, then read the log line `dpop htu mismatch`.

4. Pass: 200 and no mismatch; `sameURL` reads `X-Forwarded-Proto` and `X-Forwarded-Host` from a trusted proxy.

5. Today: `fails`, `dpop/dpop.go:90` compares against `r.Host` and `r.TLS`, which the ingress rewrites.

### C11, DPoP scheme and nonce

1. Standard: RFC 9449 sections 7.1, 8 and 9.

2. Check: `Authorization: DPoP` is accepted, a `WWW-Authenticate: DPoP` challenge is returned, and the resource can demand a `DPoP-Nonce`.

3. Method:

```bash
grep -rn '"DPoP "\|DPoP-Nonce\|use_dpop_nonce' smw dpop
```

4. Pass: all three appear.

5. Today: `fails`, `smw/token.go:19` reads only the `Bearer ` prefix.

### C12, mTLS binding behind the ingress

1. Standard: RFC 8705 section 3.

2. Check: a token bound with `cnf.x5t#S256` validates when the client certificate is forwarded by the ingress.

3. Method:

```bash
sed -n 150,160p dpop/dpop.go
```

4. Pass: the thumbprint is read from a trusted forwarded certificate header when `r.TLS` is nil.

5. Today: `fails`, `smw/token.go:71` and `dpop/dpop.go:156` read only `r.TLS`, empty behind Container Apps.

## Client authentication

### C13, Basic parsing

1. Standard: RFC 7617 section 2; RFC 6749 section 2.3.1.

2. Check: the `Basic` scheme matches case insensitively and the id and secret are form url decoded after the split.

3. Method:

```bash
sed -n 101,112p smw/client_auth.go
```

4. Pass: `strings.EqualFold` on the scheme and `url.QueryUnescape` on both parts.

5. Today: `fails`, `smw/client_auth.go:104` uses a case sensitive `HasPrefix` and decodes nothing.

### C14, client secret verification

1. Standard: ISO/IEC 27001:2022 A.5.17; OWASP ASVS V2.4.1.

2. Check: an inbound client secret is compared against the bcrypt hash of the replicated `claimius.samna_client`, and an unknown or wrong pair answers 401.

3. Method:

```bash
just test
```

4. Pass: `smw/client_auth_db_test.go` passes.

5. Today: `passes`, `smw/client_auth.go:171`.

### C15, outbound client authentication

1. Standard: RFC 6749 section 2.3.1.

2. Check: the code and refresh exchanges with the prophet authenticate with `client_secret_basic` or `client_secret_post`.

3. Method:

```bash
sed -n 118,128p client/auth.go
```

4. Pass: `SetBasicAuth` or `client_id` and `client_secret` in the form.

5. Today: `fails`, `client/auth.go:124` sends `SAuth-App-Slug` and `SAuth-App-Secret`.

### C16, token error codes

1. Standard: RFC 6749 section 5.2.

2. Check: a failed exchange surfaces the prophet's `error` code, so `invalid_grant` is told apart from `invalid_client`.

3. Method:

```bash
grep -n 'invalid_grant\|invalid_client' client/auth.go
```

4. Pass: the client parses `error` into a typed result.

5. Today: `fails`, no match in `client/auth.go`.

## Sessions and cookies

### C17, PKCE S256

1. Standard: RFC 7636 section 4.2; RFC 9700 section 2.1.1.

2. Check: the login sends an S256 challenge from a 32 byte verifier and the verifier at the exchange.

3. Method:

```bash
sed -n 15,50p client/pkce.go
```

4. Pass: `crypto/rand` 32 bytes, SHA256 base64url challenge, `code_challenge_method=S256`.

5. Today: `passes`, `client/pkce.go:18`, `client/pkce.go:26`, `client/pkce.go:47`, `client/auth.go:156`.

### C18, state bound to the browser

1. Standard: RFC 6749 section 10.12; RFC 9700 section 2.1.

2. Check: the callback refuses a missing or mismatching `state` and clears the flow cookies.

3. Method:

```bash
just test disciple
```

4. Pass: `disciple/echo/callback_test.go` passes, with a case for a wrong `state`.

5. Today: `passes`, `disciple/echo/auth.go:21`, `disciple/echo/auth.go:75`.

### C19, Secure cookies

1. Standard: RFC 6265 section 4.1.2.5; OWASP ASVS V3.4.1.

2. Check: every cookie written outside dev carries `Secure`, whether or not a cookie domain resolves.

3. Method:

```bash
sed -n 22,35p utils/cookie.go
curl -sI https://<tst-connect-host>/api/auth/login | grep -i set-cookie
```

4. Pass: `Secure` on every `Set-Cookie`, and the code derives `secure` from the environment, not from the domain.

5. Today: `fails`, `utils/cookie.go:27` sets `secure := domain != ""`.

### C20, cookie prefix

1. Standard: RFC 6265bis section 4.1.3.2; OWASP ASVS V3.4.4.

2. Check: host only session cookies use the `__Host-` prefix.

3. Method:

```bash
grep -rn '__Host-' utils disciple smw client
```

4. Pass: the access, refresh, PKCE and state cookies carry the prefix when no domain is set.

5. Today: `fails`, no match.

### C21, CSRF

1. Standard: OWASP ASVS V4.2.2; ISO/IEC 27001:2022 A.8.26.

2. Check: a cookie authenticated unsafe method without the matching CSRF header is refused, and the cli approval compares in constant time.

3. Method:

```bash
just test smw
sed -n 204,208p disciple/echo/cli.go
```

4. Pass: `smw/session_test.go` passes and the approval uses `subtle.ConstantTimeCompare`.

5. Today: `passes`, `smw/session.go:67`, `disciple/echo/cli.go:206`.

### C22, HSTS

1. Standard: RFC 6797 section 7.1; OWASP ASVS V14.4.5.

2. Check: a disciple's HTTPS responses carry `Strict-Transport-Security`.

3. Method:

```bash
grep -rn 'Strict-Transport-Security' smw shared disciple
curl -sI https://<tst-connect-host>/ | grep -i strict-transport-security
```

4. Pass: a middleware sets it, or the ingress header is present on every disciple host.

5. Today: `fails`, no match in the module.

## Cli login

### C23, the cli login keeps no state on the disciple

1. Standard: RFC 7636; RFC 8252 sections 7.3 and 8.1; ISO/IEC 27001:2022 A.5.17.

2. Check: a cli signs in through the service's own `/api/auth/login` with its own PKCE challenge, the callback hands the code to the cli without exchanging it, and the disciple stores nothing of the login.

3. Method:

```bash
grep -rn 'cli_login' disciple/database disciple/sdb
just test 'TestGetLoginSendsTheClisChallengeAndKeepsNoVerifier|TestGetCallbackHandsTheCodeToTheLoopbackWithoutExchanging'
```

4. Pass: no table or query in the disciple set; `TestGetLoginSendsTheClisChallengeAndKeepsNoVerifier` and `TestGetCallbackHandsTheCodeToTheLoopbackWithoutExchanging` pass.

5. Today: `passes`, `disciple/echo/auth.go:39`, `disciple/echo/auth.go:99`.

### C24, loopback redirect only

1. Standard: RFC 8252 sections 7.3 and 8.3; RFC 9700 section 4.1.

2. Check: `cli_redirect` is accepted only as `http` on `127.0.0.1`, `::1` or `localhost` with a port and no user info, with a `cli_state` the cli checks; anything else answers 400, and a browser login clears a stale cli target.

3. Method:

```bash
just test 'TestGetLoginSendsTheClisChallengeAndKeepsNoVerifier|TestGetCallbackHandsTheCodeToTheLoopbackWithoutExchanging'
```

4. Pass: `TestGetLoginRefusesANonLoopbackRedirect` and `TestGetLoginWithoutCliClearsAStaleCliTarget` pass.

5. Today: `passes`, `disciple/echo/cli.go:143`.

### C25, code exchanged only with the cli's verifier

1. Standard: RFC 7636 section 4.6; RFC 6749 sections 4.1.3 and 5.2.

2. Check: `POST /api/auth/cli/token` exchanges the code at the prophet with the cli's `code_verifier`, the service's registered `redirect_uri` and its own app secret, relays the prophet's OAuth error, answers `no-store`, and `POST /api/auth/cli/revoke` revokes only the cli's refresh family.

3. Method:

```bash
just test 'TestGetLoginSendsTheClisChallengeAndKeepsNoVerifier|TestGetCallbackHandsTheCodeToTheLoopbackWithoutExchanging'
```

4. Pass: `TestPostCliTokenExchangesTheCodeWithTheClisVerifier`, `TestPostCliTokenRelaysTheProphetsRefusal` and `TestPostCliRevokeRevokesTheClisRefreshFamily` pass.

5. Today: `passes`, `disciple/echo/cli.go:59`, `disciple/echo/cli.go:102`, the paste page refuses framing at `disciple/echo/cli.go:174`.

### C26, rate limits on auth routes

1. Standard: RFC 6585 section 4; ISO/IEC 27001:2022 A.8.5; OWASP ASVS V2.2.1.

2. Check: `/api/auth/login`, `/api/auth/cli/token` and `/api/auth/cli/revoke` are rate limited across replicas, and a 429 carries `Retry-After`.

3. Method:

```bash
grep -rn 'RateLimiter' disciple bff smw
just test smw
```

4. Pass: the limiter is mounted on each route, shared through Postgres, and `smw/rate_limit_test.go` asserts `Retry-After`.

5. Today: `fails`, `smw/rate_limit.go:25` is in memory per process; the cli routes carry it at `disciple/sam/register.go:62` and `smw/rate_limit.go:34` sets `Retry-After`; `/api/auth/login` carries none.

## Access control

### C27, deny by default

1. Standard: ISO/IEC 27001:2022 A.5.15; OWASP ASVS V4.1.3, V4.1.5.

2. Check: an object with no grant for the caller is filtered from reads and refused on writes with 403.

3. Method:

```bash
just test
just test-hurl
```

4. Pass: `shared/access_db_test.go` and `test/hurl/access_graph.hurl` pass.

5. Today: `passes`, `shared/access.go:17`.

### C28, every write checked

1. Standard: ISO/IEC 27001:2022 A.5.15; OWASP ASVS V4.1.1.

2. Check: every insert, update and delete under `prophet/echo` and `disciple/echo` goes through `MutationAccess` or the batch gate first.

3. Method:

```bash
grep -rln 'NewInsert\|NewUpdate\|NewDelete' prophet/echo disciple/echo | xargs grep -L 'MutationAccess\|GetAccess'
```

4. Pass: no file listed, or each listed file writes only rows owned by the caller's own session.

5. Today: `passes`, `shared/access.go:37`; standards entry OWASP ASVS V4.

### C29, time bounded grants

1. Standard: ISO/IEC 27001:2022 A.5.18.

2. Check: a grant with `p_ends_at` in the past grants nothing.

3. Method:

```bash
grep -n 'p_starts_at\|p_ends_at' prophet/database/V2.4__claimius_functions_external.sql
just test
```

4. Pass: the bounds are stored and honoured by `get_access` in the prophet suite.

5. Today: `passes`, `prophet/database/V2.4__claimius_functions_external.sql:2296`.

### C30, access review export

1. Standard: ISO/IEC 27001:2022 A.5.18.

2. Check: a function or route lists every grant under an organization with holder, claim, level, start and end.

3. Method:

```bash
grep -rn -i 'review\|recertif' prophet shared
```

4. Pass: an export exists.

5. Today: `fails`, none; standards duty 2.

### C31, separation of duty

1. Standard: ISO/IEC 27001:2022 A.5.3; NIST RBAC (ANSI INCITS 359) sections 6.2 and 6.3.

2. Check: two claims can be declared mutually exclusive and the grant of the second is refused.

3. Method:

```bash
grep -rn -i 'exclusive\|separation' prophet/database disciple/database
```

4. Pass: a constraint table and a check in `insert_user_claim`.

5. Today: `fails`, `disciple/database/V2.3__claimius_functions_access.sql:423` checks composition only.

## Redaction

### C32, masks settled in SQL

1. Standard: ISO/IEC 27001:2022 A.8.11; NIST SP 800-162 section 3.

2. Check: `_apply_denies` returns a per field mask `{field: mask, "*": mask}` for one pair in both schema sets.

3. Method:

```bash
grep -n '_apply_denies' prophet/database/V2.2__claimius_functions_internal.sql disciple/database/V2.2__claimius_functions_internal.sql
just test
```

4. Pass: both definitions exist and the access suites pass with a deny by field case.

5. Today: `passes`, `prophet/database/V2.2__claimius_functions_internal.sql:823`, `disciple/database/V2.2__claimius_functions_internal.sql:725`.

### C33, settled scope typed in Go

1. Standard: ISO/IEC 27001:2022 A.8.11; GDPR Art. 25(2).

2. Check: `AccessClaim.Scope` is parsed into one typed scope that every disciple shares, instead of each consumer carrying its own engine.

3. Method:

```bash
sed -n 608,614p shared/models.go
grep -rn 'func.*Redact\|func.*ParseScope' shared smw disciple
```

4. Pass: a typed field and one parse function in `shared`.

5. Today: `fails`, `shared/models.go:612` is a raw `*json.RawMessage`; engines live in `bookable_server/internal/db/redaction.go:36` and `nimflow/redact/scope.go:18`.

### C34, reads withhold masked fields

1. Standard: ISO/IEC 27001:2022 A.8.11; GDPR Art. 5(1)(c).

2. Check: a field whose mask lacks read is absent from the response.

3. Method:

```bash
sed -n 88,100p shared/access.go
```

4. Pass: an apply step after the gate that removes withheld fields by `bun` column and dotted path.

5. Today: `fails`, `shared/access.go:90` `Has` tests `Access` only and nothing reads `Scope`.

### C35, writes refuse masked fields

1. Standard: ISO/IEC 27001:2022 A.8.11; OWASP ASVS V4.2.1.

2. Check: a body that names a field whose mask lacks write is refused with 403.

3. Method:

```bash
grep -rn 'Scope' shared/access.go smw disciple/echo | grep -v json
```

4. Pass: a write path compares body fields with the mask.

5. Today: `fails`, no write path reads the mask; `.nimpress/claimius/redaction.md:65` states the rule.

## Audit and logging

### C36, audit in the transaction

1. Standard: ISO/IEC 27001:2022 A.8.15.

2. Check: `WriteAudit` runs on the caller's `bun.IDB`, so a rolled back change leaves no row and a committed one always has one.

3. Method:

```bash
just test
```

4. Pass: `shared/audit_db_test.go` and `prophet/echo/audit_db_test.go` pass.

5. Today: `passes`, `shared/audit.go:10`.

### C37, no dropped audit rows

1. Standard: ISO/IEC 27001:2022 A.8.15; OWASP ASVS V7.1.3.

2. Check: no audit path writes outside the change's transaction or drops a row after retries.

3. Method:

```bash
grep -rn 'audit.Write(' --include='*.go' .
sed -n 11,35p audit/audit.go
```

4. Pass: no caller of the goroutine writer, or the writer fails the request.

5. Today: `fails`, `audit/audit.go:11` writes in a goroutine and logs and drops the row after three attempts.

### C38, audit retention and tamper evidence

1. Standard: ISO/IEC 27001:2022 A.8.15, A.5.33; GDPR Art. 5(1)(e).

2. Check: monthly partitions are kept for a stated period and dropped after, and rows are hash chained or written to an append only sink.

3. Method:

```bash
grep -n 'PARTITION' disciple/database/V1.1__claimius_baseline.sql
grep -rn -i 'retention\|prev_hash' disciple/database prophet/database audit
```

4. Pass: a retention sweep and a chain column or sink.

5. Today: `fails`, partitions at `disciple/database/V1.1__claimius_baseline.sql:849` with neither.

### C39, trace context to the prophet

1. Standard: W3C Trace Context section 3; ISO/IEC 27001:2022 A.8.16.

2. Check: calls from a disciple to the prophet carry `traceparent`.

3. Method:

```bash
sed -n 211,216p client/http.go
```

4. Pass: the client's `Transport` is `nimtel.Transport` or `otelhttp.NewTransport`.

5. Today: `fails`, `client/http.go:215` builds a plain `http.Client`.

### C40, telemetry on every request

1. Standard: ISO/IEC 27001:2022 A.8.16.

2. Check: `TelemetryMiddleware` opens a span per request and stamps the organization.

3. Method:

```bash
just test smw
```

4. Pass: `smw/telemetry_test.go` passes.

5. Today: `passes`, `smw/telemetry.go:11`, `smw/telemetry.go:15`.

## Cryptography

### C41, secrets sealed at rest

1. Standard: ISO/IEC 27001:2022 A.8.24; OWASP ASVS V6.2.1.

2. Check: secrets are AES GCM with a fresh nonce per write; the app and organization keys that seal them are stored wrapped under a master key the database never holds; a value opens only in process, for a user with read on it.

3. Method:

```bash
sed -n 10,30p shared/cipher.go
sed -n 20,75p pkg/encryption/master.go
grep -n -A20 'FUNCTION claimius.get_secret_value' disciple/database/V2.4__claimius_functions_external.sql
just test 'MasterKey|WrapAtRest|UnwrapAtRest|ImportMasterKeyPair|SamApp|GetSecretValue|Resolve'
```

4. Pass: `cipher.NewGCM` with a nonce from `crypto/rand`; `WrapAtRest` refuses without a 32 byte `SAUTH_MASTER_KEY` and `UnwrapAtRest` refuses a plain value; `get_secret_value` answers NULL to a user without read; the named tests pass.

5. Today: `passes`. Secrets: `shared/cipher.go:12`, `shared/secret.go:62` on a debug disciple, `utils/aes.go:12` from `prophet/echo/secret.go:59` on the prophet, which encrypts every routed write. Keys at rest: `pkg/encryption/master.go:20`, `pkg/encryption/master.go:49`, opened by `ImportMasterKeyPair` at `pkg/encryption/crypto.go:54` and for the organization seed at `smw/app.go:100`. Delivery and opening: `GET /sam/app` answers the calling app its own seed (`prophet/echo/app.go:11`), held in memory (`shared/main.go:133`), and `Resolve` (`shared/secret.go:141`) reads through `get_secret_value` (`disciple/database/V2.4__claimius_functions_external.sql:1005`).

### C42, TLS to Postgres

1. Standard: ISO/IEC 27001:2022 A.8.24; GDPR Art. 32(1)(a).

2. Check: the default `sslmode` is `verify-full` and no deployed disciple overrides it.

3. Method:

```bash
sed -n 143,147p shared/main.go
az containerapp show -g <resource-group> -n <container-app> --query "properties.template.containers[0].env[?name=='PGSSLMODE']"
```

4. Pass: the default holds and no deployed env sets a weaker mode.

5. Today: `untested`, the default passes at `shared/main.go:189`; the deployed env values are unread.

## Personal data

### C43, minimal replication

1. Standard: GDPR Art. 5(1)(c) and Art. 25(2).

2. Check: a disciple receives only the profile fields its app declares it needs.

3. Method:

```bash
sed -n 65,90p disciple/database/V1.1__claimius_baseline.sql
```

4. Pass: the sync filters `samna_user` columns per app.

5. Today: `fails`, every disciple replicates the full profile, email and phone included; only `user_identity.data` is held back, `disciple/database/V1.1__claimius_baseline.sql:325`.

### C44, analytics behind consent

1. Standard: GDPR Art. 6(1)(a), Art. 7 and Art. 25(2).

2. Check: no analytics session or event is written for an anonymous id without a granted consent.

3. Method:

```bash
sed -n 55,80p analytics/consent.go
grep -n 'GetConsent' analytics/*.go
```

4. Pass: every writer path calls `GetConsent` and stops when it is not granted.

5. Today: `passes`, `analytics/consent.go:61`.

### C45, raw analytics expiry

1. Standard: GDPR Art. 5(1)(e).

2. Check: raw analytics events older than 30 days are deleted on a schedule.

3. Method:

```bash
sed -n 100,110p analytics/writer.go
```

4. Pass: `delete_analytics_event` runs with `rawRetentionDays` 30.

5. Today: `passes`, `analytics/analytics.go:18`, `analytics/writer.go:106`.

### C46, retention for every other personal table

1. Standard: GDPR Art. 5(1)(e); ISO/IEC 27001:2022 A.8.10.

2. Check: `audit`, `notification`, `consent`, `analytics_session`, `analytics_capture`, `user_activity` and `usage_diff` each have a stated period and a sweep.

3. Method:

```bash
grep -rn -i 'delete_.*older\|retention' analytics notification audit shared prophet/database disciple/database
```

4. Pass: one sweep per table.

5. Today: `fails`, only `analytics/writer.go:106`; platform control Retention is `missing`.

### C47, export contract

1. Standard: GDPR Art. 15 and Art. 20.

2. Check: the middleware defines an export a disciple implements per object type, gathering `samna_user`, `user_field`, `user_relation`, `notification`, `consent`, `analytics_*` and the host's own rows.

3. Method:

```bash
grep -rn -i 'export' shared/me.go disciple/sam prophet/sam
```

4. Pass: an interface the host registers and a route that fans it out.

5. Today: `fails`, only the `/me` reads at `disciple/sam/register.go:14` and `shared/me.go:19`.

### C48, erasure contract

1. Standard: GDPR Art. 17; ISO/IEC 27001:2022 A.8.10.

2. Check: deleting a user hard deletes or anonymizes the profile and every row that names the user, and asks each host to erase its own rows.

3. Method: delete a user on the prophet in the prophet suite, then on the disciple database:

```bash
psql -c "SELECT count(*) FROM claimius.samna_user WHERE id = '$ID' AND email IS NOT NULL"
psql -c "SELECT count(*) FROM claimius.user_field WHERE user_id = '$ID'"
psql -c "SELECT count(*) FROM claimius.notification WHERE user_id = '$ID'"
```

4. Pass: every count 0 after the sync, and a host hook is called.

5. Today: `fails`, `shared/user.go:271` sets `sa_deleted_at` only; no host hook.

### C49, records of processing

1. Standard: GDPR Art. 30.

2. Check: each personal data table carries a purpose and legal basis in the type metadata, so a register can be generated.

3. Method:

```bash
grep -rn -i 'purpose\|legal_basis' prophet/database disciple/database
```

4. Pass: the columns exist and every inventory table has a value.

5. Today: `fails`, none; standards duty 10.

## Interfaces

### C50, problem details

1. Standard: RFC 9457 section 3.

2. Check: error responses use `application/problem+json`.

3. Method:

```bash
grep -rn 'problem+json' pkg smw shared
```

4. Pass: the error writer sets the media type and the members.

5. Today: `fails`, `pkg/respond/main.go:18` writes the envelope and `smw/main.go:130` a plain string.

### C51, lost update protection

1. Standard: RFC 9110 sections 13.1.1 and 8.8.3.

2. Check: API reads return `ETag` and writes honour `If-Match` with 412.

3. Method:

```bash
grep -rn 'If-Match\|HeaderETag' shared smw disciple prophet/echo
```

4. Pass: both on the object routes.

5. Today: `fails`, only asset data, `disciple/echo/asset.go:242`.

### C52, protected resource metadata

1. Standard: RFC 9728 section 3; MCP authorization specification.

2. Check: a disciple serves `/.well-known/oauth-protected-resource` and names it in `resource_metadata` on a 401.

3. Method:

```bash
curl -s -o /dev/null -w '%{http_code}\n' https://<tst-connect-host>/.well-known/oauth-protected-resource
```

4. Pass: 200 with `resource` and `authorization_servers`.

5. Today: `fails`, the only well known path is the discovery read at `shared/main.go:266`.

### C53, resource indicators

1. Standard: RFC 8707 section 2.

2. Check: the client sends `resource` and the validator matches `aud` to the resource URL.

3. Method:

```bash
grep -rn '"resource"' client smw
```

4. Pass: both appear.

5. Today: `fails`, none; `smw/token.go:55` matches the app slug.

### C54, event stream resume

1. Standard: WHATWG HTML server sent events, `id` field and `Last-Event-ID`.

2. Check: notification frames carry `id:` and a reconnect with `Last-Event-ID` replays missed events.

3. Method:

```bash
grep -n 'id: \|Last-Event-ID' notification/stream.go
```

4. Pass: both appear.

5. Today: `fails`, `notification/stream.go:46` writes `data:` only.

## Supply chain

### C55, SBOM and provenance

1. Standard: CRA Annex I Part II point 1; ISO/IEC 27001:2022 A.8.25.

2. Check: each release tag publishes an SBOM of the Go module and the `@nimling` package.

3. Method:

```bash
grep -n -i 'sbom\|attest\|provenance' .github/workflows/release.yml .github/workflows/typescript-publish.yml
```

4. Pass: an SBOM step in each.

5. Today: `fails`, no match.

### C56, known vulnerabilities

1. Standard: CRA Annex I Part I point 2(a); ISO/IEC 27001:2022 A.8.8.

2. Check: no reachable known vulnerability in the module or the `ts/` package.

3. Method:

```bash
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
cd ts && pnpm audit --prod
```

4. Pass: no called vulnerability and no high or critical advisory.

5. Today: `untested`, no workflow in `.github/workflows/` runs either.

## Summary

1. 56 checks.

2. 17 pass: C1, C6, C9, C14, C17, C18, C21, C23, C27, C28, C29, C32, C36, C40, C41, C44, C45.

3. 37 fail: C2, C3, C4, C5, C7, C8, C10, C11, C12, C13, C15, C16, C19, C20, C22, C24, C25, C26, C30, C31, C33, C34, C35, C37, C38, C39, C43, C46, C47, C48, C49, C50, C51, C52, C53, C54, C55.

4. 2 untested: C42, C56.
