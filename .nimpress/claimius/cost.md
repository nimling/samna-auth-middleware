---
gate: samna-developer
layout: doc
title: Cost
description: The cost ledger every app reports to the prophet, app_cost, the three ways a row is reported, the schema that checks its evidence and says how it accumulates, who writes it, how it travels over the sync and how it is read back in buckets.
tags: app_cost, get_app_cost, cost, pricing, ledger, module, kind, usage, fee, period, credit, ref, supersedes, schema, x-samna, accumulate, proration, group key, GetAppCost, PutAppCost, AppCostInput
lastUpdated: true
---

# Cost

Every app reports what it costs. The disciple is the source: it pushes cost rows as they happen, fine or coarse, a request, an hour, a seat month, a one off fee, and the prophet accumulates them. A row reaches the prophet like every other routed row: the app writes it through the builder, the prophet normalises and stores it as the truth, and the feed carries it back to the app's own replica. Each app decides how it prices itself; claimius carries the count, the unit, the amount and the evidence, and never a price list.

## The row

| Column | Is |
|---|---|
| `id` | The row, kept across the reports of one `ref` |
| `app_id` | The app the cost belongs to, prophet only, taken from the authenticated app |
| `module` | The app's own slug for its own usage, or the slug of the built in module that did the work for it: `uptime`, `subscription`, `storage` and so on |
| `kind` | `usage`, metered direct usage over the window; `fee`, a single cost or charge at an instant; `period`, time based usage over the window, a seat month, a device day, GB days, prorated on read; `credit`, a negative correction, discount or refund |
| `period_start`, `period_end` | The window the row covers, end exclusive and never before start. A single cost has start equal to end |
| `quantity`, `unit` | What was counted: `request`, `check`, `mail`, `byte`, `seat`, `event`, `item`, or the app's own word |
| `unit_price` | The price of one unit when the cost was priced per unit; `amount` is then `quantity` times `unit_price`, checked on write to the cent |
| `amount`, `currency` | What the customer bears for the row, in an ISO 4217 currency. Negative only for a credit |
| `ref` | The reporting app's own reference for the window or the event |
| `supersedes_id` | The row this correction supersedes |
| `status` | `estimated` until the app settles the figure, `settled`, or `invoiced` once billed |
| `schema` | Empty, or a JSON Schema document, draft 2020-12, named by `$id`, that `detail` is checked against and that says how the row is presented and accumulated |
| `detail` | The evidence behind the row: any object when `schema` is empty, otherwise an object the schema accepts |
| `sa_owner_id`, `sa_created_by` | The app's organization and the grant of the app actor that reported the row |

An app has one live row per module and ref: `(app_id, module, ref)` is unique among live rows on the prophet, `(module, ref)` on a disciple. A rerun of the same ref replaces that row's values, never adds a second, so an app may report a window as often as it likes and the last word stands. A correction is a new row with its own ref that names the row it corrects in `supersedes_id`; the superseded row stays and is left out of every sum, and deleting the correction brings it back. `app_cost` is a registered table, so the actor that reported a row holds it as its creator, and it is application data: retention never ages it.

## The three forms

`POST /sam/cost` accepts one of three shapes, and so does the disciple's `PutAppCost`, whose builder write routes there. The prophet normalises each into the one row and refuses anything else with a `400` that names the field.

1. The row form: `module`, `kind`, `period_start`, `period_end`, `quantity`, `unit`, `unit_price` or `amount` or both, `currency`, `ref`, and optionally `status`, `supersedes_id`, `schema` and `detail`. The amount is computed as `quantity` times `unit_price` when absent; when both are given they must agree.

2. The items form: `module`, `period_start`, `period_end`, `currency`, `ref`, `items`, and optionally `kind`, `status`, `supersedes_id`, `schema` and a `detail` without items. Every item carries `description`, `quantity`, `unit` and `unit_price` or `amount`. The row's quantity is the sum of the items when they share a unit, else the number of items with unit `item`; the row's amount is the sum of the item amounts; the items land in `detail.items` with every amount filled; `kind` defaults to `usage`, and the schema defaults to the middleware's own cost schema below.

3. The event form: `module`, `at`, `amount`, `currency`, `ref`, and optionally `status`, `supersedes_id`, `schema` and `detail`. The row is a `fee` whose window is the instant `at`, with quantity 1 of unit `event`.

The answer is `201` on a new row and `200` on a replace by ref. `PATCH /sam/cost/:id` takes `status` and `detail` only, the detail checked against the row's schema; `DELETE` soft deletes.

## The schema

A row with an empty `schema` is just a cost over a window, and its `detail` may be any object, including empty. A row with a schema carries a JSON Schema document, draft 2020-12, named by `$id`: the prophet checks `detail` against it on write and refuses a mismatch with a `400` naming the failing path, `detail at '/subject/id'`. The document may carry an `x-samna` object with the presentation hints Samna Auth reads: `title`, `columns`, paths into `detail` to show, `unit_label` and `amount_label`. Claimius checks only that `x-samna` is an object; Samna Auth owns the rules per `$id`.

The schema may describe the accumulation too, under `x-samna.accumulate`: `method` is `sum`, the default, `max`, `last` or `count`; `proration` is `overlap`, the default, or `start`; `group` is an array of paths into `detail`, for example `["subject.id"]`, whose values become an extra bucket key. Claimius checks the shape and refuses an unknown method or proration.

The middleware ships one schema document, `shared.CostSchema` in Go and `COST_SCHEMA` in TypeScript, with `$id` `https://samna.io/schema/cost/1`: an object of `subject` (`type`, `id`, `name`), `source` (`vendor`, `ids`, `refs`, `run_id`), `items` (lines of one shape: `description`, `quantity`, `unit`, `unit_price`, `amount`, `ref`, `at`), `pricing` (`list`, `tier`, `prorated`) and `notes`, every key optional, nothing else allowed, accumulating by `sum` over `overlap` grouped by `subject.id`. Under that schema the amounts of `detail.items` must sum to the row's amount. An app sends it as its `schema` to take it as is; Samna Auth uses it for the built in modules.

## Who writes it

The app's own actor writes its cost. On a disciple the service calls the disciple's `PutAppCost` with the access it holds and the input in one of the three forms; the builder routes it to the prophet's `/sam/cost`, where the prophet checks that the input's grant is a live grant of that actor in the calling app, stamps the app and the app's organization, checks `supersedes_id` against the app's own live rows, normalises the input and replaces the ref. The answer is copied onto the disciple in the caller's transaction, and the feed brings the same row to every replica of the app. A patch of a row's status or detail and a soft delete travel the same way, through the builder's update and delete. A debug disciple, with no prophet to route to, normalises and replaces locally under the same rules.

A built in module the prophet hosts, such as uptime or the subscription and newsletter service, carries a cost the prophet reports for itself under the module's slug. An app that uses a built in module reports what that module did for it under the same slug in its own rows, so the module's card on the prophet lists the apps that use it beside the prophet's own figure.

## Who reads it

A reader with READ on the app reads its cost. On the prophet `get_app_cost` takes the app and checks `get_access` on the app row, for a reader in the app itself or in the prophet's own app; READ on the app's organization through an inheriting claim gives it. On a disciple the same function takes no app, since a replica holds one, and checks READ on the organization owning the rows, the app's own organization. A reader without it gets no rows, never an error.

The function answers buckets, not rows: one per module, kind, schema `$id`, group key, unit and currency per UTC hour, day or month, each carrying the accumulated quantity and amount, the hours of window it folds and the number of rows it accumulated, so a stream of fine rows is visible as such. A fee and a credit fall whole into the bucket of their `period_start`, as does a row whose window is empty or whose schema prorates by `start`; usage and period rows are prorated by the overlap of their window with the bucket and with the billing window, `from` inclusive and `to` exclusive. The row's schema decides the rest: the method per bucket and group, `sum`, `max`, `last` by `period_start`, or `count` on the quantity with the amount still summed, and the group key, a JSON object of the `group` paths and their values from `detail`, null without one. `module`, `status`, and a subject's `type` and `id` read from `detail.subject` narrow the rows. In Go the same call is `GetAppCost` with an `AppCostQuery` on both roles; the browser reads it through `getAppCost` against whichever route the service exposes on its own `/api`, which is the service's own work.

## What the prophet renders

The prophet renders one card per app with a spark line over the buckets, the app's own module first and the built in modules it used beside it, and one card per built in module with the prophet's own cost and the apps that used it listed under it. Each schema `$id` is presented by its own rule from the hints in its `x-samna`. The rows carry everything the cards need; the cards and the prophet's own `/api` route are Samna Auth's work.
