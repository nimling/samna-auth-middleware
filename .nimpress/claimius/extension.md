---
gate: auth-middleware
layout: doc
title: The extension, future work
description: Future work. Where claimius is going once it matures and hosting allows it, a PostgreSQL extension that scopes every table to the connected user, which choices of today lead there, which extensions already exist for parts of it, and why hosting is the blocker.
tags: future work, extension, row level security, set role, session, connection limit, pg_tle, pg_session_jwt, set_user, azure flexible server
lastUpdated: true
---

# The extension, future work

Everything in this file is future work, not how claimius runs. It states where claimius is going, and it moves only after hosting allows it.

## The goal

Claimius becomes a PostgreSQL extension. It is configured on two things: the access model, and how database connections are handed out. The database itself knows who is connected and what they may reach, so every table is scoped to that user in its natural order: organization, location, the user's grants.

1. A consumer chooses how to use that scoped access: in its own code, as claimius works as a schema, or in the database, behind the claimius guard, by querying its tables directly as the user.

2. The end state is deployment per app or per solution in which direct database access can be handed out: a login with a lease and a maximum connection count, with no safety concern, because authentication and authorization are enforced inside the extension and the database, not in a service in front of it.

## What the schema already provides

1. The per user index. `user_object` is the reverse index keyed by user, primary key `(user_id, app_id, object_type, object_id)`. A row level security policy of the form `EXISTS (SELECT 1 FROM claimius.user_object WHERE user_id = <session user> AND app_id = <session app> AND object_type = <type> AND object_id = <row id> AND sa_access & 4 = 4)` reads one key of it per row. `shared.ObjectWhere` filters a Go query the same way through `get_access` for one row and `get_objects` for a page.

2. The read functions. Every read goes through `get_*` with the user and the app as its first arguments. In the extension those arguments come from the session, not from the caller.

3. The on demand settle and the evaluated check. A policy that reads `user_object` needs the rows it reads to be settled, and the settle runs at the entry of every listing for the reader, which is what a policy over one table targets. A policy on a single row can instead call the check, which settles the one pair it reads.

4. The role grant model. `claimius_reader` sees no table, only functions. A leased login is a reader whose functions and policies take the user from the session.

5. The actor model. `sa_created_by` is a grant. A write made directly by a leased login is stamped with the grant the session carries, as a routed write is.

## The Postgres mechanisms it builds on

| Mechanism | Role in the extension |
|---|---|
| Row level security, `CREATE POLICY ... USING` | Scopes every registered table to the session's user ([docs](https://www.postgresql.org/docs/17/ddl-rowsecurity.html)) |
| `SET ROLE`, `SET SESSION AUTHORIZATION` | Binds a connection to a role that cannot be escaped ([docs](https://www.postgresql.org/docs/17/sql-set-role.html)) |
| Custom settings, `set_config` and `current_setting` | Carries the verified user and app into the session ([docs](https://www.postgresql.org/docs/17/functions-admin.html#FUNCTIONS-ADMIN-SET)) |
| `CONNECTION LIMIT` and `VALID UNTIL` on a role | The maximum connection count and the lease of a login ([docs](https://www.postgresql.org/docs/17/sql-createrole.html)) |
| Extension packaging, control file and scripts | Ships claimius as `CREATE EXTENSION claimius` with versioned upgrades ([docs](https://www.postgresql.org/docs/17/extend-extensions.html)) |

A policy must evaluate its per session values once per statement, not once per row, for example by wrapping them in a scalar subquery.

## Extensions that exist

| Extension | Does | Maturity | License | Postgres |
|---|---|---|---|---|
| [`pg_session_jwt`](https://github.com/neondatabase/pg_session_jwt) | Verifies a JWT against a JWK inside the session and exposes `auth.user_id()` and the claims to policies | In production behind the Neon Data API | Apache 2.0 | Built with pgrx |
| [`set_user`](https://github.com/pgaudit/set_user) | Controlled, logged role switching, blocks `ALTER SYSTEM` and `COPY PROGRAM`, `set_session_auth` for irrevocable switches | Maintained by the pgaudit project, CI on 18 and 19 | PostgreSQL | Current majors |
| [`pg_tle`](https://github.com/aws/pg_tle) | Installs extensions written in trusted languages without filesystem access, with hooks such as a password check | Offered on Amazon RDS, Aurora and Supabase | Apache 2.0 | 12 to 18 |
| [TimescaleDB Community Edition](https://www.tigerdata.com/docs/about/latest/timescaledb-editions) | Compresses and expires old partitions of `audit` and the analytics events by policy | Mature; Azure provides only the Apache 2 edition, without compression | Tiger Data License | Current majors |

Each covers a part: `pg_session_jwt` the verified identity in the session, `set_user` the connection bound to a role, `pg_tle` the delivery of a claimius extension to a managed server, TimescaleDB Community the compression of the time ordered tables of [Storage](./storage.md). They are adopted once hosting allows it.

## Hosting is the blocker

The proving ground and test arena is Azure Database for PostgreSQL Flexible Server.

1. It runs the supported majors 11 to 18, with 18 carrying limits: certain extensions are not supported on it. The Samna servers run 17.

2. An extension can be created only when it is on the allow list of the `azure.extensions` server parameter; any other `CREATE EXTENSION` fails. The list is Microsoft's: a server cannot load an extension that is not on it, so a claimius extension cannot be installed, developed or tested there.

3. `pg_tle`, the one route by which a managed server accepts an extension it does not ship, is not offered; the request stands as a feedback item.

The move to the extension therefore happens after hosting is solved: a host that loads custom extensions, or one that offers `pg_tle`. Until then claimius is a schema delivered by `smig`, and the design in the rest of these files is what runs.

## Cost

1. A policy over `user_object` costs one primary key probe per row it filters, once the reader's share is settled.

2. A policy that evaluates its session values per row pays that evaluation per row; wrapped in a scalar subquery it pays it once per statement.

## Proofs and measurements

Supabase measured a policy falling from 179 ms to 9 ms from wrapping its per session values in a scalar subquery alone.

**Sources.** [PostgreSQL, Row Security Policies](https://www.postgresql.org/docs/17/ddl-rowsecurity.html); [Supabase, RLS performance and best practices](https://supabase.com/docs/guides/troubleshooting/rls-performance-and-best-practices-Z5Jjwv); [Microsoft Learn, Supported versions](https://learn.microsoft.com/en-us/azure/postgresql/configure-maintain/concepts-supported-versions); [Microsoft Learn, Considerations when using extensions](https://learn.microsoft.com/en-us/azure/postgresql/extensions/concepts-extensions-considerations); [Microsoft Learn, Allow extensions](https://learn.microsoft.com/en-us/azure/postgresql/extensions/how-to-allow-extensions); [MicrosoftDocs/azure-docs issue 112210](https://github.com/MicrosoftDocs/azure-docs/issues/112210).
