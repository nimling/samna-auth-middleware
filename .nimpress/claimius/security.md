---
gate: auth-middleware
layout: doc
title: Security
description: The four roles and their grants per role, read functions that run as their owner, secrets under the app seed and the credential cache, the unlogged token tables, soft delete, and what a compromised disciple can and cannot do.
tags: claimius_admin, claimius_writer, claimius_reader, claimius_disciple_client, _grant_prophet_roles, _grant_disciple_roles, _match_audit_partition_grants, security definer, search_path, samna_secret, get_secret_value, private_seed, SAUTH_MASTER_KEY, bcrypt, credential cache, unlogged, rate_limit_gcra, soft delete
lastUpdated: true
---

# Security

Claimius guards access in four layers: database roles that decide which connection may touch which table and function, read functions that run as their owner with a pinned `search_path`, secrets that are encrypted or hashed under keys the database never holds, and the app as the boundary of what any one service can reach.

## Roles

`V1.0__claimius_roles.sql` creates four roles and the schema, owned by `claimius_admin`, and grants the other three `USAGE` on it. The grants on tables and functions are written by `_grant_prophet_roles` on a prophet and a hybrid and by `_grant_disciple_roles` on a disciple. Both live in `V6.0__claimius_grants.sql`, the last file of each base set, which runs the one that fits at the top level:

1. The disciple set runs the disciple helper.
2. The prophet set runs the disciple helper on a database initialized through `upsert_disciple_state`, and the prophet helper otherwise.
3. The same file makes every `audit` partition hold the grants of `audit`.

Every build and every replay of the set therefore leaves the roles holding their grants on every table and function the set carries, a function added in any earlier file included. No consumer calls a `_grant_*` function; a change to what a role holds is made in that file.

| Role | Prophet | Disciple |
|---|---|---|
| `claimius_admin` | Owns every table and function | The same |
| `claimius_writer` | `SELECT` on every table; `INSERT`, `UPDATE` on `samna_app`, `samna_user`, `samna_client`, `samna_secret`, `organization`, `location`, `object_field`, `user_field`, `user_relation`, `object_asset`, `notification`, `prophet_state`, `sync_consumer`, `peer_grant`; `UPDATE` on `prophet_peer`; `INSERT`, `UPDATE`, `DELETE` on `claim_graph_cache`; `EXECUTE` on every function | not granted |
| `claimius_reader` | No table; `EXECUTE` on every function except the claim, grant and binding writers, `merge_user`, `migrate_root`, `upsert_system_app`, `upsert_disciple_state`, `upsert_prophet_peer`, `upsert_app`, `insert_app_handover`, `update_app_handover`, `apply_peer_event`, `insert_secret`, `upsert_peer_grant`, `upsert_guest_user`, `update_claim_guest`, `insert_code`, `delete_code`, `update_code_used`, `redeem_code`, `upsert_table_info`, `delete_table_info`, `upsert_internal_table_info` and `recompute_state` | No table; `EXECUTE` on every function except the claim, grant and binding writers, `migrate_root`, `upsert_disciple_state`, `insert_secret`, `update_claim_guest`, `insert_code`, `delete_code`, `redeem_code`, `upsert_table_info`, `delete_table_info`, `upsert_internal_table_info` and `recompute_state` |
| `claimius_disciple_client` | not granted | `SELECT` on every table; `INSERT`, `UPDATE`, `DELETE` on the mirrored tables `samna_user`, `samna_client`, `samna_secret`, `organization`, `location`, `claim`, `user_claim`, `claim_object`, `object_field`, `user_field`, `user_relation`, `object_asset`, on the derived tables `user_object`, `object_info`, `user_users`, `user_users_diff`, `inheritance_info`, `table_info`, `claim_graph_cache`, and on `disciple_state`, `audit`, `notification`; `EXECUTE` on every function |

A prophet set initialized as a disciple grants the disciple client `samna_app` in place of `user_users_diff`.

1. The reader holds no table privilege. It reaches data only through functions, and it cannot call a function that creates or changes a claim, a grant, a binding or a home.

2. The writer holds no `INSERT`, `UPDATE` or `DELETE` on `audit` or `sync_event`, so a service connecting as the writer cannot rewrite the trail or the feed. Every partition of `audit` holds exactly the grants of the partitioned table, the owner and `PUBLIC` aside: `_match_audit_partition_grants` revokes every other grant on a partition and gives it each grant of `audit`, when `upsert_audit_partition` attaches a month and on every replay of `V6.0__claimius_grants.sql`. A partition read or written by name grants nothing the table does not.

3. The disciple client applies the feed, so it writes the mirrored tables and the rows derived from them. Its authority is the one app the disciple mirrors; see [What a compromised disciple can do](#what-a-compromised-disciple-can-do).

## Reads run as their owner

Every read function of the external surface is `SECURITY DEFINER` with `SET search_path = claimius, pg_temp`: the `get_*` access, listing, scope, audit, secret, asset and graph functions, `search`, `resolve_access`, `mutation_access`, `touch_object_usage`, `drain_access_diff`, the identity helpers, the ancestor and subtree walks and `upsert_audit_partition`.

1. A read settles what it meets: it writes `user_object`, its cursor in `user_activity`, `user_users_diff`, `user_users`, `usage_diff` and `claim_graph_cache`, and never an `access_diff` row. Running as the owner lets a reader that holds no table privilege pay that settle, and the only writes it can cause are those the function body performs.

2. The pinned `search_path` resolves every unqualified name in the body to `claimius` and then the session's temporary schema, so a caller cannot shadow a claimius table or operator with an object of its own. `pg_temp` is named last, which is where the owner graph keeps its transaction temporary tables.

3. The write functions run with the privileges of the caller, so a writer holds the table privileges of every write it performs, and a reader holds none. On a prophet, `apply_peer_event`, which applies a peer prophet's events, also runs as its owner; the reader cannot call it.

## Secrets

1. `samna_secret.value` is AES 256 GCM ciphertext under the app's `private_seed`, with a random 96 bit nonce per value. GCM authenticates as it encrypts, so a changed ciphertext fails to open.

2. `samna_app` carries `private_key` and `private_seed`, both stored wrapped at rest: `enc:v1:` and AES 256 GCM under the prophet's `SAUTH_MASTER_KEY`, 32 random bytes the database never holds (`pkg/encryption/master.go:32`). `ImportMasterKeyPair` opens them where the prophet signs or encrypts (`pkg/encryption/crypto.go:54`) and refuses a value that is not wrapped. Their events carry no `app_id` and no pull serves them, and a disciple skips the event type, so the rows never reach a disciple. They reach a peer prophet only in the app row of `get_peer_snapshot`, still wrapped, so a peer opens them only under the same master key; the home serves `peer=true` only to a pull presenting the peer secret of a live `peer_grant` for that app in `SAuth-Peer-Secret`, so the app credential its disciples hold does not open them.

3. `samna_client.secret_hash` is a bcrypt hash and `samna_app.app_secret` is HMAC SHA256 of the raw secret keyed with the owning organization's `private_seed`: a copy can verify a credential and cannot recover it.

4. The middleware pays bcrypt once per credential. `verifyClientSecret` keeps, per `client_id`, the SHA-256 digest of the secret that passed and the `secret_hash` it passed against. A later request rereads the client row by primary key for its status, expiry and hash, and compares its secret's digest to the kept one in constant time while the hash is unchanged. bcrypt runs again only when the client presents another secret or its `secret_hash` changes, so a revoked client or a rotated secret stops on the next request. `smw.AppMiddleware` keeps the imported key pair of an app per app id and imports it again when the row's `private_key` or `private_seed` differs.

5. `organization.private_seed` keys `app_secret` hashing for the apps owned in the tree. No SQL mints it: the service mints it, wrapped under `SAUTH_MASTER_KEY`, the first time an app of the organization needs a secret, and a child created later takes its owner's (`_set_self_owner_on_insert`, `prophet/database/V2.3__claimius_functions_access.sql:1400`). `smw.AppMiddleware` opens it to verify an app secret (`smw/app.go:100`). It travels to peer prophets only: `get_sync_events` drops it from organization payloads for a disciple, and the disciple snapshot never marshals it, so a disciple cannot verify or forge an `app_secret`.

6. `insert_secret` stores a secret and binds it to the claim of the actor in `sa_created_by` with owner, write and read in one statement, on the prophet for a routed write and on a debug disciple locally. The binding travels with the secret, so every disciple of the app reaches it through `get_secrets` and gates it with `MutationAccess` like any other object.

7. A value opens only in a process that holds the app's `private_seed`, and only for a user who reads the secret. The prophet encrypts every routed write with it (`prophet/echo/secret.go:59`) and the feed carries the ciphertext. A deployed disciple receives its own app's seed from `GET /sam/app` (`prophet/echo/app.go:11`), the route `smw.AppMiddleware` answers only to that app's `SAUTH_APP_SLUG` and `SAUTH_APP_SECRET`, opened from its wrapping and sent `Cache-Control: no-store`. `sam.SyncStart` fetches it at startup (`disciple/sam/main.go:94`) and `shared.GetAppSeed` holds it in process memory (`shared/main.go:133`); it is never written to a table, a file or a log. `SamnaSecret.Resolve(ctx, db, userID)` (`shared/secret.go:141`) first calls `claimius.get_secret_value(user, secret)` (`disciple/database/V2.4__claimius_functions_external.sql:1005`), which answers the ciphertext only to a user `get_access` gives read on the secret, the creator through the claim `insert_secret` bound or any holder of a claim reaching it, and NULL to anyone else, so `Resolve` answers `ErrNoAccess` before any seed is used. It then opens the value with AES 256 GCM; when the held seed cannot open it, it drops the seed, fetches it again once and retries, which is how a changed seed reaches a running disciple. A background job resolves as the user behind the row's `sa_created_by` grant. On a debug disciple the seed is `SAUTH_DEBUG_PRIVATE_SEED` and the path is the same. The key never reaches Postgres. `GET /sam/secret` and `GET /sam/secret/:id` answer the ciphertext, and no route answers plaintext.

## Access codes

A code in a URL is a bearer capability, so it is handled as one; [Users](./users.md), Access codes and The guard, has the mechanics.

1. The value is 256 random bits and is never stored: `claimius.code.hash` is its SHA-256, on the prophet and on every disciple.
2. The URL carries the code, never a token. A navigation that brings `samk` is answered `303` to the same URL without it, and the code moves to a `__Host-` cookie, `HttpOnly; Secure; SameSite=Lax`, that expires with the code. An API call sends `SAuth-Code` instead of the query.
3. Every answer to a request that carries or follows a code sends `Referrer-Policy: no-referrer`, and the guard takes `samk` and `SAuth-Code` off the request before a handler, a request logger or a span reads it. The telemetry span records the path only.
4. A code resolves only inside `starts_at` and `ends_at`, below `count` and until `revoked_at`; a revoke reaches every disciple through the feed and clears the resolve cache. A counted use and an invite redeem are single statements on the prophet, so the last use is taken once.
5. Twenty failed resolves per address and minute answer `429`, and the limit holds for valid codes too until the minute passes; an unanswerable resolve is `503`.
6. A guest reads at most, in the access walk itself, and every guest request writes an audit row with the claim and the code id.
7. A guest code is an identity only on a route group mounted with `UserAuthOptions{Guest: true}`. On every other route it is taken off the request and ignored, so the request answers `401`, or the login redirect on a navigation, exactly as one without a credential.
8. Guest tokens from `POST /sam/guest` always expire, at the access token lifetime or the grant's end.

## Ephemeral tables

`session`, `dpop_jti`, `par_request`, `jwks_cache` and `rate_limit` on a prophet, and `dpop_jti` on a disciple, are `UNLOGGED`: no WAL, not replicated, emptied after a crash. Each holds values that expire, and losing all of them costs a login, a new DPoP proof or a refetched key set, never a security property: a replayed DPoP proof still fails on its `iat`. `rate_limit_gcra` keeps one `rate_limit` row per key, the theoretical arrival time of the generic cell rate algorithm, updated under a transaction advisory lock on the key.

`user_identity` binds an approved outside identity to a `user_id` under an organization. It is written on the prophet, and the feed carries it to every app the organization is linked to without its `data`, the claims of the token it was approved from, which stay on the prophet.

## Soft delete

Rows of registered tables and of the claimius tables that carry `sa_deleted_at` are never hard deleted by application code. A soft delete keeps the row that `sa_created_by`, `audit` and the feed refer to, and removes it from every read: the settle that meets its area deletes the pairs on it. `tg_cascade_user_soft_delete` carries a user's deletion to their grants, relations and field values once, in the deleting write.

## What a compromised disciple can do

A disciple's database and process hold its app's mirrored rows, its own tables, its app slug and secret, and the end user tokens that pass through it.

| It can | It cannot |
|---|---|
| Read everything its app holds, and answer its own service's access questions however it likes | Read another app's rows: the prophet serves only the app its credential names |
| Send routed writes for its own app, stamped with any grant of that app in `sa_created_by` | Write for another app: the app key names the app, and every routed write runs for it alone |
| Pull its own app's feed | Sign a token: it holds no `private_key` |
| Store ciphertext of its app's secrets, and decrypt them in process with its own app's seed, held in memory from `GET /sam/app` | Decrypt another app's secrets: the prophet answers each app only its own seed, and the seed is never stored on the disciple |
| | Hand out more than the stamped grant holds: `_check_claim_composition` refuses a binding whose mask exceeds what that grant's user holds on the object, or for a first binding owner on the organization it opens, and a link to a claim that user cannot read |
| | Replay a DPoP bound token without the client's private key |
| | Write an app another prophet decides: the home refuses it |

The boundary is the app. A disciple is trusted with its own app's data because its service already holds that data; the feed's filter on `app_id` and `app_object_ids` keeps that trust from reaching past it.

## Cost

1. A client credential costs one bcrypt comparison on first use, then per request one primary key probe of `samna_client` and one SHA-256 digest compared in constant time.

2. `rate_limit_gcra` holds one row and one transaction advisory lock per key, whatever the traffic.

3. An audit partition opens with one catalog probe when the month exists, and with one advisory lock, one `CREATE TABLE`, one move of that month's rows out of `audit_default` and one `ATTACH` on the first row of a month.

## Proofs and measurements

### Why a reader cannot change access

1. `claimius_reader` holds no table privilege, so every row it reaches is reached through a function.
2. The functions that create or change a claim, a grant, a binding, an app, a home or a registration are revoked from it in `V6.0__claimius_grants.sql`, which replays on every build.
3. The functions it may call that write run as their owner, and their bodies write only derived state and bookkeeping: pairs recomputed from the model, cursors and marks, usage counts, the directory counts, the claim graph cache, the trim of areas every active cursor has passed, and audit partitions. Each is a function of the model or of reads already made, so no sequence of reader calls changes what any user may do.

**Sources.** [PostgreSQL, CREATE FUNCTION, Writing SECURITY DEFINER Functions Safely](https://www.postgresql.org/docs/17/sql-createfunction.html#SQL-CREATEFUNCTION-SECURITY); [NIST SP 800-38D](https://csrc.nist.gov/pubs/sp/800/38/d/final); [PostgreSQL, CREATE TABLE, UNLOGGED](https://www.postgresql.org/docs/17/sql-createtable.html#SQL-CREATETABLE-UNLOGGED); [RFC 9449, section 11.1](https://www.rfc-editor.org/rfc/rfc9449#section-11.1).
