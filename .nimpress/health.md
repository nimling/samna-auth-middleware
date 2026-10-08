---
layout: doc
title: Health
description: The dependency free health route every disciple can mount, and how the Samna Auth status probe reads it.
tags: health, status, probe, monitor, uptime, heartbeat
visibility: hidden
lastUpdated: true
---

# Health

A disciple answers `GET /api/status/health` with `200` while its process is up. The route reads nothing: no database, no prophet, no session. It is the target the Samna Auth status probe calls, and the one a container platform's liveness check should point at, so a slow database or a prophet outage never reads as a dead process.

## Mounting it

`sam.RegisterStatus` registers the route on the group it is given, without auth. Mount it beside the auth group, never inside a group that carries `smw.UserAuthMiddleware`.

```go
sam.RegisterStatus(e.Group("/api/status"))

api := e.Group("/api", smw.UserAuthMiddleware(nil))
sam.RegisterEndpoints(api)
```

The [BFF](./bff.md) mounts it under the parent of its base path, `/api/status` for the default `/api/auth`. The example disciple under `example/disciple` mounts it the same way, and `just test-hurl` covers it with `test/hurl/status_health.hurl`.

## The answer

```json
{
  "data": {
    "status": "ok",
    "app": "samna-developer-dev",
    "version": "v2.0.0"
  },
  "request_id": "6f1c2d5e-4b3a-4c2d-9e8f-0a1b2c3d4e5f",
  "count": 1
}
```

| Field | Holds |
|---|---|
| `status` | `ok` while the process answers; the route has no other value, a process that cannot answer does not answer |
| `app` | `SAUTH_APP_SLUG`, so a probe pointed at the wrong host sees it in the body |
| `version` | `APP_VERSION` from the environment, omitted when unset |

The Go type is `uptime.Health`; the TypeScript package mirrors it as `Health` with `healthUrl(origin)` and `getHealth(origin)`.

## The status probe

Samna Auth keeps a monitor per app. A monitor of kind `http` carries a `target`, an `interval_seconds`, a `timeout_seconds` and an `expected_status`, and the prophet calls the target on that interval and records a check row with the status code and the latency. A run of failed checks opens an incident, passing checks resolve it, and `GET /api/app/<slug>/status` on the prophet lists the monitors with their daily counts and the open incidents; a disciple can proxy that through `GetUptimeStatus`.

Point the monitor at `<app origin>/api/status/health` with `expected_status` 200. Because the route is dependency free, a failing check means the process or the network in front of it, and nothing else. Samna Auth itself answers `GET /api/status/health` the same way, with an empty `data`, and probes it on its own monitor.

The heartbeat is the other direction: `uptime.Init` makes the disciple report a beat to the prophet on an interval, with the checks the service names, such as a database ping. The health route says the process is up; the heartbeat says what it can reach.
