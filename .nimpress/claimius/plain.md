---
visibility: hidden
layout: doc
title: Claimius in plain words
description: What claimius is, how it decides who may do what, why it is fast, and what it takes to run it inside your own PostgreSQL, without the formulas.
tags: claimius, access control, authorization, plain language, getting started, prophet, disciple
lastUpdated: true
---

# Claimius in plain words

This page says what the rest of the claimius pages say, without the formulas. Every claim here is backed by one of those pages, linked where it applies.

## What it is

Claimius is a set of tables and functions that lives inside the PostgreSQL database your service already has. It answers one question: may this user do this thing to this row. It answers from a stored list of "who can reach what", kept inside the same database as your data, so a check is one indexed lookup and never a call to another server.

You do not send your rows to claimius and you do not run a separate authorization service. You tell claimius which of your tables matter, you hand out permissions, and you ask it before you read or write.

## The three ideas

1. **A claim is a permission template.** It says what may be done: read, write, execute, own. It has a name, like "Kitchen editor".

2. **A grant gives a claim to a user.** A row that says "this user holds Kitchen editor", optionally only between two dates.

3. **A binding attaches a claim to something.** A row that says "Kitchen editor applies to this kitchen". A binding may say "and to everything under it", so one binding on an organization covers every room, desk and booking that organization owns.

A user may do something to a row when they hold a claim that is bound on that row, or on something above it in a tree that the binding cascades through. That is the whole model. A deny is a claim that takes bits away instead of giving them, and a claim can lend another claim's bindings to its holders, one hop, so you can build roles out of roles. See [Claims](./claims.md) and [Denials](./denials.md).

## The trees

Every row sits in up to three trees: who owns it (organizations own organizations, rooms, bookings), where it is (a location inside a location), and its own parent in its own table (a folder inside a folder). Claimius stores the full ancestor list of every row once, so "is this room under that organization" is a lookup, never a walk. See [Organizations](./organizations.md) and [Objects](./objects.md).

## How a check is answered

Every user has a list of pairs: one row per object they can reach, with the bits they hold on it. A check reads one pair. A listing reads the user's range of pairs in order, a page at a time. Search is the same range filtered by text. Nothing is computed while you wait, except the small part described next.

## What happens when something changes

When someone adds a grant, moves an organization or binds a claim, claimius does not rewrite every affected user's list. It updates the list of the person who made the change, and records a note: "this area changed, in this transaction". Every other user's list is brought up to date the next time that user reads, and only for the areas that changed since they last read. A user who never comes back never costs anything. A background job tidies the notes once every active user has passed them. See [Materialization](./materialization.md).

This is the trade claimius makes: writes are cheap and local, the first read after a big change pays for exactly that change, and every read after it is the plain lookup again.

## Prophets and disciples

One database is the prophet: Samna Auth. It is where users log in and where grants are made. Every product service is a disciple: it carries the same claimius schema and pulls the prophet's changes for its own app, as plain source rows, into its own database. A disciple then answers every access question locally. If the prophet is unreachable, a disciple keeps answering from what it has. See [Sync](./sync.md) and [Prophets and disciples](./prophets.md).

## How fast

Measured on the suite in `test/sql`, counted as index entries touched (one entry is one row read from an index):

| Operation | Entries |
|---|---|
| A check on a row nothing has changed under | 7 on the prophet, 4 on a disciple |
| A page of 50 listed rows | about 100 |
| The first listing after one user is granted a node over 1,000 organizations | about 40,000, then back to a page |
| The first listing after a change that reaches 16,000 rows | about 64,000, then back to a page |

The pages [Comparison](./comparison.md) and [Storage](./storage.md) put these beside Zanzibar style systems and Entra ID, and give the cost claimius pays for it: more stored rows per pair, and a grant or binding write that is a few times heavier than an insert.

## What it takes to run it in your PostgreSQL

1. Add the schema. Your `database/migrate.yml` includes `disciple/database` from this repository at a pinned tag; `smig` applies it.

2. Give your tables five columns: `sa_owner_id`, `sa_created_by`, `sa_created_at`, `sa_updated_at`, `sa_deleted_at`, and optionally `sa_location_id` and the pair `sa_parent_id` with `sa_root_id`. Register each table once with `upsert_table_info('public.my_table')`. From then on claimius keeps the trees and the name index for those tables through triggers.

3. When you create a row, bind it: `insert_claim_object` with the claim of the actor who created it. A row with no binding is invisible to everyone but its creator.

4. Before you read, ask `get_objects` for the page a user may see, or `get_access` for one row. Before you write, ask `get_access` and stamp the returned grant id into `sa_created_by`.

5. Never read or write the claimius tables directly. The functions are the surface; the tables change between versions, the functions do not.

In Go the middleware does steps 4 and 5 for you with `MutationAccess`, `GetAccess` and the listing helpers, and runs the pull loop for a disciple. See [The middleware](./middleware/index.md).

## How it grows with you

1. More tables: register them. Nothing in claimius names your tables.
2. More hierarchy: add `sa_location_id` or the parent pair to a table, backfill, register again.
3. More roles: compose claims instead of duplicating bindings.
4. Field level: a binding may carry a scope that names which fields its holder sees, see [Redaction](./redaction.md), and a claim may carry a capability with no object at all, such as "may open this page", see [Capabilities](./capabilities.md).
5. More services: each one is another disciple of the same prophet, with its own app and its own copy.
6. More prophets: a prophet can mirror one app of another prophet, see [Prophets and disciples](./prophets.md).

## Where to look next

[Types](./types.md) for the four kinds of thing claimius knows, [Reads](./reads.md) and [Writes](./writes.md) for every function with what it costs, and [Security](./security.md) for roles and what a compromised disciple can and cannot do.
