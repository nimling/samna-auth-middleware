---
layout: doc
title: Security
description: The four roles and their grants per role, read functions that run as their owner, secrets under the app seed and the credential cache, the unlogged token tables, soft delete, and what a compromised disciple can and cannot do.
tags: claimius_admin, claimius_writer, claimius_reader, claimius_disciple_client, _grant_prophet_roles, _grant_disciple_roles, _match_audit_partition_grants, security definer, search_path, samna_secret, private_seed, bcrypt, credential cache, unlogged, rate_limit_gcra, soft delete
lastUpdated: true
---

# Security

## Roles

`V1.0__claimius_roles.sql` creates four roles and the schema, owned by `claimius_admin`, and grants the other three `USAGE` on it. The grants on tables and functions are written by `_grant_prophet_roles` on a prophet and a hybrid and by `_grant_disciple_roles` on a disciple. Both live in `V6.0__claimius_grants.sql`, the last file of each base set, which runs the one that fits at the top level: the disciple set runs the disciple helper, and the prophet set runs the disciple helper on a database initialized through `init_disciple` and the prophet helper otherwise. So every build and every replay of the set leaves the roles holding their grants on every table and function the set carries, a function added in any earlier file included, and no consumer calls a `_grant_*` function. A change to what a role holds is made in that file, which replays it.

| Role | Prophet | Disciple |
|---|---|---|
| `claimius_admin` | Owns every table and function | The same |
| `claimius_writer` | `SELECT` on every table; `INSERT`, `UPDATE` on `samna_app`, `samna_user`, `samna_client`, `samna_secret`, `organization`, `location`, `object_field`, `user_field`, `user_relation`, `notification`, `prophet_state`, `sync_consumer`; `UPDATE` on `prophet_peer`; `INSERT`, `UPDATE`, `DELETE` on `claim_graph_cache`; `EXECUTE` on every function | not granted |
| `claimius_reader` | No table; `EXECUTE` on every function except the claim, grant and binding writers, `merge_user`, `migrate_root`, `init`, `init_disciple`, `init_hybrid`, `create_app`, `offer_handover`, `accept_handover`, `apply_peer_event`, `init_claimius_tables`, `init_claimius_internal` and `recompute_state` | The same |
| `claimius_disciple_client` | not granted | `SELECT` on every table; `INSERT`, `UPDATE`, `DELETE` on the mirrored tables, the derived tables `user_object`, `object_info`, `user_users`, `inheritance_info`, `table_info`, `claim_graph_cache`, and on `disciple_state`, `audit`, `notification`; `EXECUTE` on every function |

1. The reader holds no table privilege. It reaches data only through functions, and it cannot call a function that creates or changes a claim, a grant, a binding or a home, so a connection that only reads cannot change access.

2. The writer holds no `UPDATE` or `DELETE` on `audit` or `sync_event`, so a prophet service connecting as the writer cannot rewrite the trail or the feed. Every partition of `audit` holds exactly the grants of the partitioned table, the owner and `PUBLIC` aside: `_match_audit_partition_grants` revokes every other grant on a partition and gives it each grant of `audit`, when `ensure_audit_partition` attaches a month and on every replay of `V6.0__claimius_grants.sql`. So a partition read or written by name grants nothing the table does not, and the `audit_default` left by an unpartitioned `audit` keeps none of its old grants.

3. The disciple client applies the feed, so it writes the mirrored tables and the rows derived from them. Its authority is the one app the disciple mirrors; see below.

## Reads run as their owner

Every read function of the external surface, the `get_*` access, listing, scope, audit, secret and graph functions, `search`, `resolve_access`, `mutation_access`, `touch_object_usage`, `drain_access_diff`, the identity helpers, the ancestor and subtree walks and `ensure_audit_partition`, is `SECURITY DEFINER` with `SET search_path = claimius, pg_temp` ([CREATE FUNCTION, Writing SECURITY DEFINER Functions Safely](https://www.postgresql.org/docs/17/sql-createfunction.html#SQL-CREATEFUNCTION-SECURITY)).

1. A read settles what it meets: it writes `user_object`, `user_users`, `access_diff_user`, `claim_graph_cache` and clears `access_diff` rows. Running as the owner lets a reader that holds no table privilege pay that settle, and nothing else: the only writes it can cause are those the function body performs.

2. The pinned `search_path` resolves every unqualified name in the body to `claimius` and then the session's temporary schema, so a caller cannot shadow a claimius table or operator with an object of its own. `pg_temp` is named last, which is where the owner graph keeps its transaction temporary tables.

3. The functions a write triggers run with the privileges of the caller, so a writer holds the table privileges of every write it performs, and a reader holds none.

## Secrets

1. `samna_secret.value` is AES 256 GCM ciphertext under the app's `private_seed`, with a random 96 bit nonce per value ([NIST SP 800-38D](https://csrc.nist.gov/pubs/sp/800/38/d/final)). GCM authenticates as it encrypts, so a changed ciphertext fails to open.

2. `samna_app` carries `private_key` and `private_seed`. Its events carry no `app_id` and no pull serves them, and a disciple skips the event type, so the keys never reach a disciple. They reach a peer prophet only in the app row of `get_peer_snapshot`, served over the mirrored app's own credential to a prophet that mirrors that app.

3. `samna_client.secret_hash` and `samna_app.app_secret` are bcrypt hashes: a copy can verify a credential and cannot recover it.

4. The middleware pays bcrypt once per credential. `verifyClientSecret` keeps, per `client_id`, the SHA-256 digest of the secret that passed and the `secret_hash` it passed against. A later request rereads the client row by primary key for its status, expiry and hash, and compares its secret's digest to the kept one in constant time while the hash is unchanged. Per request that is one primary key probe and one digest, and bcrypt runs again only when the client presents another secret or its `secret_hash` changes, so a revoked client or a rotated secret stops on the next request. `smw.AppMiddleware` keeps the imported key pair of an app per app id and imports it again when the row's `private_key` or `private_seed` differs.

5. `organization.private_seed` is generated at a root and inherited by its children, and keys `app_secret` hashing for the apps owned in the tree.

## Ephemeral tables

`session`, `dpop_jti`, `par_request`, `jwks_cache` and `rate_limit` on a prophet, and `dpop_jti` on a disciple, are `UNLOGGED`: no WAL, not replicated, emptied after a crash ([PostgreSQL, CREATE TABLE, UNLOGGED](https://www.postgresql.org/docs/17/sql-createtable.html#SQL-CREATETABLE-UNLOGGED)). Each holds values that expire, and losing all of them costs a login, a new DPoP proof or a refetched key set, never a security property: a replayed DPoP proof still fails on its `iat` ([RFC 9449, section 11.1](https://www.rfc-editor.org/rfc/rfc9449#section-11.1)). `rate_limit_gcra` keeps one `rate_limit` row per key, the theoretical arrival time of the generic cell rate algorithm, updated under a transaction advisory lock on the key: one row and one lock per key, whatever the traffic.

`user_token` binds an external identity to a `user_id` for debug and integration paths and never enters the feed.

## Soft delete

Rows of registered tables and of the claimius tables that carry `sa_deleted_at` are never hard deleted by application code. A soft delete keeps the row that `sa_created_by`, `audit` and the feed refer to, and removes it from every read: the settle that meets its area deletes the pairs on it. `tg_cascade_user_soft_delete` carries a user's deletion to their grants, relations and field values once, in the deleting write.

## What a compromised disciple can do

A disciple's database and process hold its app's mirrored rows, its own tables, its app slug and secret, and the end user tokens that pass through it.

| It can | It cannot |
|---|---|
| Read everything its app holds, and answer its own service's access questions however it likes | Read another app's rows: the prophet serves only the app its credential names |
| Send routed writes for its own app, stamped with any grant of that app in `sa_created_by` | Write for another app: the app key names the app, and every routed write runs for it alone |
| Pull its own app's feed | Sign a token: it holds no `private_key` |
| Store ciphertext of its app's secrets | Decrypt a secret: it holds no `private_seed` |
| | Hand out more than the stamped grant holds: `_check_claim_composition` refuses a binding whose mask exceeds what that grant's user holds on the object, or for a first binding owner on the organization it opens, and a link to a claim that user cannot read |
| | Replay a DPoP bound token without the client's private key |
| | Write an app another prophet decides: the home refuses it |

The boundary is the app. A disciple is trusted with its own app's data because its service already holds that data; the feed's filter on `app_id` and `app_object_ids` keeps that trust from reaching past it.
