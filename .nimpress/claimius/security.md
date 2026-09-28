---
layout: doc
title: Security
description: The role grants, secrets under the app seed, the unlogged token tables, soft delete, and what a compromised disciple can and cannot do.
tags: claimius_admin, claimius_writer, claimius_reader, claimius_disciple_client, grants, samna_secret, private_seed, unlogged, rate_limit_gcra, soft delete
lastUpdated: true
---

# Security

## Roles

Four Postgres roles split what a connection may do. `claimius_admin` owns the schema; the other three hold `USAGE` on it and the grants below, set by `_grant_prophet_roles`, `_grant_disciple_roles` or both on a hybrid.

| Role | Tables | Functions |
|---|---|---|
| `claimius_admin` | Owns every table | Everything |
| `claimius_writer` | `SELECT` on every table; `INSERT`, `UPDATE` on `samna_app`, `samna_user`, `samna_client`, `samna_secret`, `organization`, `location`, `object_field`, `user_field`, `user_relation`, `notification`, `prophet_state`; `INSERT`, `UPDATE`, `DELETE` on `claim_graph_cache` | Every function |
| `claimius_reader` | `INSERT`, `UPDATE`, `DELETE` on `claim_graph_cache`, nothing else | Every function except the claim, grant and binding writers, `merge_user`, `migrate_root`, the `init` functions and `recompute_state` |
| `claimius_disciple_client` | `SELECT` on every table; `INSERT`, `UPDATE`, `DELETE` on the mirrored tables, the derived tables and `audit` | Every function |

1. The claimius functions run with the privileges of their caller, the PostgreSQL default ([CREATE FUNCTION, SECURITY INVOKER](https://www.postgresql.org/docs/17/sql-createfunction.html)), so a connection holds the table privileges of every function it calls, and the grants above are the whole of what a role can do.

2. No grant gives the writer or the reader `UPDATE` or `DELETE` on `audit` or `sync_event`, so a service connecting as either cannot rewrite the trail or the feed.

3. The reader cannot call the functions that create or change claims, grants and bindings, so a connection that only reads cannot change access.

## Secrets

1. `samna_secret.value` is AES 256 GCM ciphertext under the app's `private_seed`, with a random 96 bit nonce per value ([NIST SP 800-38D](https://csrc.nist.gov/pubs/sp/800/38/d/final)). GCM authenticates as it encrypts, so a changed ciphertext fails to open rather than decrypting to garbage.

2. A `samna_app` row has no `app_id`, so its events never match a pull: `private_key` and `private_seed` never leave the prophet. A disciple holds ciphertext and resolves a secret through its prophet.

3. `samna_client.secret_hash` and `samna_app.app_secret` are bcrypt hashes. A copy can verify a credential and cannot recover it.

4. `organization.private_seed` is generated at a root and inherited by its children, and keys `app_secret` hashing for the apps owned in the tree, so a secret verified under one tenant's seed is meaningless under another's.

## Ephemeral tables

`session`, `dpop_jti`, `par_request`, `jwks_cache` and `rate_limit` are `UNLOGGED`: no WAL, not replicated, emptied after a crash ([PostgreSQL, CREATE TABLE, UNLOGGED](https://www.postgresql.org/docs/17/sql-createtable.html#SQL-CREATETABLE-UNLOGGED)). Each holds values that expire, and losing all of them after a crash costs a login, a new DPoP proof or a refetched key set, never a security property: a replayed DPoP proof still fails on its `iat` ([RFC 9449, section 11.1](https://www.rfc-editor.org/rfc/rfc9449#section-11.1)). `rate_limit_gcra` holds one row per key, the theoretical arrival time of the generic cell rate algorithm, updated under a transaction scoped advisory lock on the key, so concurrent calls for one key serialize and different keys never meet: one row and one lock per key, whatever the traffic.

`user_token` binds an external identity to a `user_id` for debug and integration paths and is never carried on the feed.

## Soft delete

Rows of registered tables and of the claimius tables that carry `sa_deleted_at` are never hard deleted by application code. A soft delete keeps the row that `sa_created_by`, `audit` and the feed refer to, and removes it from every read. `tg_cascade_user_soft_delete` carries a user's deletion to their grants, relations and field values once, in the deleting write.

## What a compromised disciple can do

A disciple's database and process hold its app's mirrored rows, its own tables, its app slug and app secret, and the end user tokens that pass through it.

| It can | It cannot |
|---|---|
| Read everything its app holds, and answer its own service's access questions however it likes | Read any other app's rows: the prophet never sends them |
| Send routed writes as the users whose live tokens it sees | Act for a user whose token it has not seen, or replay a DPoP bound token without the client's private key |
| Pull its own feed | Sign a token: it holds no `private_key` |
| Store ciphertext of its app's secrets | Decrypt a secret: it holds no `private_seed` |
| | Change a grant without a user whose own access on the prophet allows it |

The boundary is the app. A disciple is trusted with its own app's data, because its service already holds that data; the feed's filter on `app_id` and `app_object_ids` makes sure that trust does not extend past it.
