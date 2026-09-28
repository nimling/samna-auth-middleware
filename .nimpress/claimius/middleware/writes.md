---
layout: doc
title: Writes in Go
description: The write builders NewInsert, NewUpdate and NewDelete, how a write to a shared type is routed to the prophet and upserted locally, how the actor is stamped, and debug mode.
tags: NewInsert, NewUpdate, NewDelete, Access, routed write, EndpointFor, debug mode, sa_created_by
lastUpdated: true
---

# Writes in Go

`sdb.Instance().NewInsert()`, `NewUpdate()` and `NewDelete()` take a model and the `AccessClaim` the actor holds.

```go
ac, err := sdb.Instance().MutationAccess(ctx, actor, orgID, sdb.OBJECT_TYPE_ORGANIZATION, sdb.ACCESS_WRITE)
if err != nil {
	return err
}
err = sdb.Instance().NewInsert().Model(&location).Access(ac).Scan(ctx, &location)
```

## Where a write lands

| Table | Deployed disciple | Prophet, and debug mode |
|---|---|---|
| `claim`, `claim_object`, `user_claim` | Sent to the prophet, the answer upserted locally | The local claimius functions |
| `organization`, `location`, `user_relation`, `samna_secret`, `samna_user` | Sent to the prophet, the answer upserted locally | Local, through the triggers |
| The implementing side's own registered tables | Local | Local |

1. `.Access(ac)` stamps `sa_created_by` with `ac.UserClaimID` and `sa_owner_id` from the grant, pointer fields included. Nothing else stamps an actor.

2. A routed write resolves its endpoint from the model with `shared.EndpointFor`, posts over the app key with the end user's forwarded token, and upserts the row the prophet returns inside the same call. The prophet checks that the forwarded token belongs to the actor and evaluates again the actor's access on its own data before it writes.

3. The prophet's answer carries the sequence of the change. A later read on the disciple that must see it waits on `WaitForSeq`, the consistency token of [Sync](../sync.md).

4. The write settles the actor on the prophet and, through the local upsert, on the disciple; every other user's check sees it at once, because a check evaluates from the sources, and every other user's listing settles on its next read. See [Materialization](../materialization.md).

## Debug mode

`SAUTH_DEBUG_USER_ID` or `SAUTH_DEBUG_CLIENTS` turns a disciple into a self contained service: authentication is bypassed, the pull loop does not start, and every write runs through the local claimius functions. It exists for local development and test runs, proves nothing about the prophet path, and is never set in a deployed environment.
