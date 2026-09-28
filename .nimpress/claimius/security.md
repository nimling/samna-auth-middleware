---
layout: doc
title: Security
description: The role grant model, security definer functions, secrets encrypted with the app seed, the ephemeral token tables, soft delete, and what a compromised disciple can and cannot do.
tags: claimius_admin, claimius_writer, claimius_reader, claimius_disciple_client, security definer, samna_secret, private_seed, dpop_jti, session, rate_limit, soft delete
lastUpdated: true
---

# Security

## Roles

Four Postgres roles split what a connection may do. `claimius_admin` owns the schema.

| Role | Tables | Functions |
|---|---|---|
| `claimius_admin` | Owns every table | Everything, including `init`, `init_claimius_tables`, `recompute_state`, `migrate_root` |
| `claimius_writer` | Insert and update on the direct insert tables of [Writes](./writes.md), no delete | Every read and write function |
| `claimius_reader` | None | The `get_*` functions, `resolve_access`, `mutation_access`, `search`, the graphs |
| `claimius_disciple_client` | Insert, update and delete on the mirrored tables, for the pull loop | The apply path |

1. A service's request path connects as `claimius_reader` or `claimius_writer`. A reader that selects from `claimius.user_object` or `claimius.claim_object` is refused by the database, so the rule that access is read through functions is enforced, not reviewed.

2. The read functions are `SECURITY DEFINER`, owned by `claimius_admin`, with `SET search_path = pg_catalog, claimius, pg_temp`. That is how a role with no table privilege reads through them, and the pinned path is what keeps a caller from masking a table or operator the function uses with one of its own ([PostgreSQL, Writing SECURITY DEFINER Functions Safely](https://www.postgresql.org/docs/17/sql-createfunction.html#SQL-CREATEFUNCTION-SECURITY)).

3. No role but `claimius_admin` may update or delete `audit`, `sync_event` or `access_diff`.

## Secrets

1. `samna_secret.value` is AES 256 GCM ciphertext under the app's `private_seed`, a random 96 bit nonce per value, stored hex encoded ([NIST SP 800-38D](https://csrc.nist.gov/pubs/sp/800/38/d/final)). GCM authenticates as it encrypts, so a changed ciphertext fails to open rather than decrypting to garbage.

2. Only the prophet holds `private_seed` and `private_key`. They are never in the feed. A disciple holds ciphertext and resolves a secret through its prophet.

3. `samna_client.secret_hash` and `samna_app.app_secret` are bcrypt hashes. A replica can verify a credential and cannot recover it.

4. `organization.private_seed` keys `app_secret` hashing for the apps owned in its tree, so a secret verified under one tenant's seed is meaningless under another's.

## Ephemeral tables

`session`, `dpop_jti`, `par_request`, `jwks_cache` and `rate_limit` are `UNLOGGED`: no WAL, not replicated, emptied after a crash ([PostgreSQL, CREATE TABLE, UNLOGGED](https://www.postgresql.org/docs/17/sql-createtable.html#SQL-CREATETABLE-UNLOGGED)). Each holds values with an `expires_at` of minutes to hours, and losing all of them after a crash costs a login, a new DPoP proof or a refetched key set, never a security property: a replayed DPoP `jti` still fails, because the proof it came in carries an `iat` outside the window after any restart ([RFC 9449, section 11.1](https://www.rfc-editor.org/rfc/rfc9449#section-11.1)). `rate_limit_gcra` holds one row per key, the theoretical arrival time of the generic cell rate algorithm, updated under a transaction scoped advisory lock on the key, so concurrent calls for one key serialize and different keys never meet.

`user_token` binds an external identity to a `user_id` for debug and integration paths and is never synced.

## Soft delete

Rows in registered tables and in the claimius tables that carry `sa_deleted_at` are never hard deleted by application code. A soft delete keeps the row that `sa_created_by`, `audit` and the feed refer to, and removes it from every read. `tg_cascade_user_soft_delete` carries a user's deletion to their grants, relations and field values once, in the deleting write.

## What a compromised disciple can do

A disciple's database and process hold its app's mirrored rows, its own tables, its app slug and app secret, and the end user tokens that pass through it.

| It can | It cannot |
|---|---|
| Read everything its app holds, and answer its own service's access questions however it likes | Read any other app's rows: the prophet never sends them |
| Send routed writes as the users whose live tokens it sees | Act for a user whose token it has not seen, or replay a DPoP bound token without the client's private key |
| Pull its own feed | Sign a token: it holds no `private_key` |
| Store ciphertext of its app's secrets | Decrypt a secret: it holds no `private_seed` |
| | Change a grant without a user whose own access on the prophet allows it |

The boundary is the app. A disciple is trusted with its own app's data, because its service already holds that data; claimius makes sure that trust does not extend past it.
