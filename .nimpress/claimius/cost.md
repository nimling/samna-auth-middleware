---
gate: samna-developer
layout: doc
title: Cost
description: The hourly cost rows every app reports to the prophet, app_cost, who writes them, how they travel over the sync, how they are read back in buckets, and how the prophet renders them per app and per built in module.
tags: app_cost, get_app_cost, cost, pricing, module, uptime, subscription, storage, spark line, GetAppCost, PutAppCost
lastUpdated: true
---

# Cost

Every app reports what it costs. The report is a row per hour, written by the app itself, and it reaches the prophet like every other routed row: the app writes it through the builder, the prophet stores it as the truth, and the feed carries it back to the app's own replica. Each app decides how it prices itself; claimius carries the number, the unit and the amount, and never a price list.

| Column | Is |
|---|---|
| `id` | The row, kept across the reports of one hour |
| `app_id` | The app the cost belongs to, prophet only, taken from the authenticated app |
| `module` | The app's own slug for its own usage, or the slug of the built in module that did the work for it: `uptime`, `subscription`, `storage` and so on |
| `period_start` | The hour the row covers, truncated to the hour |
| `quantity`, `unit` | What was counted: `request`, `check`, `mail`, `byte`, `row`, or the app's own word |
| `amount`, `currency` | What the hour costs, in an ISO 4217 currency |
| `detail` | The app's own breakdown of the hour, free form |
| `sa_owner_id`, `sa_created_by` | The app's organization and the grant of the app actor that reported the hour |

An app has one live row per module and hour: `(app_id, module, period_start)` is unique among live rows on the prophet, `(module, period_start)` on a disciple. A second report of the same hour updates the row rather than adding one, so an app may report an hour as often as it likes and the last word stands. `app_cost` is a registered table, so the actor that reported a row holds it as its creator, and it is application data: retention never ages it.

## Who writes it

The app's own actor writes its cost. On a disciple the service calls the disciple's `PutAppCost` with the access it holds and the row for the hour; the builder routes the row to the prophet's `/sam/cost`, where the prophet checks that the row's grant is a live grant of that actor in the calling app, stamps the app and the app's organization, and upserts the hour. The answer is copied onto the disciple in the caller's transaction, and the feed brings the same row to every replica of the app. A patch of an hour's measures and a soft delete travel the same way, through the builder's update and delete.

A built in module the prophet hosts, such as uptime or the subscription and newsletter service, carries a cost the prophet reports for itself under the module's slug. An app that uses a built in module reports what that module did for it under the same slug in its own rows, so the module's card on the prophet lists the apps that use it beside the prophet's own figure.

## Who reads it

A reader with READ on the app reads its cost. On the prophet `get_app_cost` takes the app and checks `get_access` on the app row, for a reader in the app itself or in the prophet's own app; READ on the app's organization through an inheriting claim gives it. On a disciple the same function takes no app, since a replica holds one, and checks READ on the organization owning the rows, the app's own organization. A reader without it gets no rows, never an error.

The function answers buckets, not rows: one bucket per module, unit and currency per hour, or per UTC day when asked for days, each carrying the summed quantity and amount and the number of hours it folds. `from` is inclusive and `to` exclusive on the hour, and `module` narrows to one module. In Go the same call is `GetAppCost` on both roles; the browser reads it through `getAppCost` against whichever route the service exposes on its own `/api`, which is the service's own work.

## What the prophet renders

The prophet renders one card per app with a spark line over the buckets, the app's own module first and the built in modules it used beside it, and one card per built in module with the prophet's own cost and the apps that used it listed under it. The rows carry everything the cards need; the cards and the prophet's own `/api` route are Samna Auth's work.
