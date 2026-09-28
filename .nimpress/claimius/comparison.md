---
layout: doc
title: Comparison
description: Claimius against Google Zanzibar, Auth0 FGA and Microsoft Entra ID on check latency, throughput, availability, consistency, listing and write cost, each claim resting on the cost formulas of Scaling, a measurement, or a published figure, and saying which.
tags: comparison, zanzibar, auth0 fga, openfga, entra id, latency, throughput, availability, sla, consistency
lastUpdated: true
---

# Comparison

Three systems stand where claimius stands. Each figure below is either proved by a formula of [Scaling](./scaling.md), measured in the claimius benchmark, or published by the vendor, and each is labeled as such.

| | Claimius | Google Zanzibar | Auth0 FGA | Microsoft Entra ID |
|---|---|---|---|---|
| Answers | Access of a user to an object, per field, and capabilities | Relations between users and objects | Relations between users and objects, the OpenFGA model | Who the user is; app roles and groups carried in the token |
| Where a check runs | In the service's own Postgres | A central service | A central service | In the service, from token claims; no per object decision |
| Check latency | `t_s + (2C + D + 5) t_d`, 0.11 ms by formula at `C = 5, D = 4`; 0.39 to 0.44 ms measured in the older eight statement form with the round trip | 3.0 ms p50, 9.46 ms p95, 15.0 ms p99, published | No latency figure published; p99 under 50 ms for the most complex models after the planner change, published | Local token validation; no directory call unless the user exceeds 200 groups |
| Check throughput | `0.7 k / c_r` per database, 12,700 a second on 2 vCores by formula, times `1 + R` standbys, times the number of disciples | Over 10 million client requests a second across the fleet, published | No figure published | Not a per object decision |
| Availability | That of the service's own database | Above 99.999 % over three years, published | 99.99 % SLA, published | 99.99 % SLA, 99.998 to 99.999 % most months, published |
| Consistency | Current on the prophet; as of the cursor on a disciple, `τ ≈ τ_rtt + n × 80 µs` behind, or at least as fresh as a sequence | Snapshot reads with zookies | Minimize latency with a 10 s cache, or higher consistency | Token lifetime |

Sources: Zanzibar, [Pang et al. 2019, table 2 and section 4](https://www.usenix.org/system/files/atc19-pang.pdf); Auth0 FGA, [Fine Grained Authorization](https://auth0.com/fine-grained-authorization), [Taming P99s in OpenFGA](https://auth0.com/blog/self-tuning-strategy-planner-openfga/), [Consistency modes](https://docs.fga.dev/writing-data/consistency-modes); Entra ID, [SLA performance](https://learn.microsoft.com/en-us/entra/identity/monitoring-health/reference-sla-performance), [Access token claims, groups overage](https://learn.microsoft.com/en-us/entra/identity-platform/access-token-claims-reference).

## What the formulas prove

### Latency

A check against a central service costs at least one network round trip `r_net` plus the service's own evaluation `e_s`. A check in the service's own database costs its evaluation `e_c`, plus one round trip to that database `r_db` when it is issued alone rather than folded into the service's own query.

```
central = r_net + e_s        local = e_c, or r_db + e_c when issued alone
e_c = t_s + (2C + D + 5) × t_d
```

`e_c` is one statement and `2C + D + 5` B-tree descents of height at most four up to twenty million pairs: 0.11 ms at `C = 5`, `D = 4` with the constants of [Scaling](./scaling.md), and independent of `U` and `O`. The inequality `local < central` holds whenever `e_c < r_net + e_s`. With `e_s` at Zanzibar's published median of 3.0 ms it holds for any network. With an ideal central service, `e_s = 0`, it holds whenever the round trip to it exceeds 0.11 ms, which is true of any network hop between machines. A central service's own evaluation of a rewrite through `n` levels of usersets costs at least `n` dependent lookups; claimius precomputed that reachability in the closure, so its evaluation has no term in the depth of the data.

What the formula does not prove is tail behaviour at Zanzibar's scale. Claimius's tail is the tail of one Postgres under the Erlang C queue of [Scaling](./scaling.md): at 70 % utilization on 2 vCores the mean time in system is 1.96 times the service time, and the database is kept below that by standbys and by the one statement check.

### Throughput

A central service carries every check of every client, so its fleet grows with the sum of the load. Claimius has no shared term on the read path:

```
X_read = Σ_disciples min(N_c / c_a, (1 + R) × 0.7 k / c_r)
```

Each service's reads are bounded by its own API cores and its own database, and adding a service adds its capacity. What a single database of one service carries is 12,700 checks a second on 2 vCores cached, and 1,875 when the per user index has outgrown memory and every check misses twice; beyond that a service adds standbys. Zanzibar's 10 million a second is the published figure of a global fleet; claimius makes no claim at that scale, and the formula says what it takes: the sum over services, not one larger service.

### Availability

A request that needs its data and an access decision succeeds when every component on its path is up. With independent failures:

```
central: A = A_db × A_net × A_authz        local: A = A_db
```

The local decision adds no factor, because it runs in the database the request already needs. A central service at 99.99 % multiplies in `1 − 0.9999`, 52.6 minutes a year, before the network between them. This holds for any `A_authz < 1` and is independent of vendor. What it costs is freshness: while a disciple's prophet is unreachable the disciple answers from its replica, so a revoke made before the partition reaches it only after the partition heals. See [Prophets and disciples](./prophets.md).

### Listing

A central service answers "which objects may this user see" by enumeration: OpenFGA's `ListObjects` returns at most 1,000 results within a 3 s deadline by default and is meant for small collections ([OpenFGA, Relationship queries](https://openfga.dev/docs/interacting/relationship-queries)); SpiceDB advises its lookup only below about 10,000 reachable resources ([SpiceDB, Protecting a list endpoint](https://authzed.com/docs/spicedb/modeling/protecting-a-list-endpoint)). Claimius lists by the reader's own range of the `user_object` primary key:

```
ids:        ⌈V_u / 87⌉ leaf pages, index only
rendered:   V_u × 0.114 cost units, 0.7 µs a row, measured 20,000 rows in 14.4 ms
```

and the implementing side joins that range to its own table in the same query. The difference is structural: the per user index lives next to the data it filters, so a listing is a range, not a search.

### Writes

A tuple store writes one tuple per relation and pays for reachability at read time, or maintains a precomputed index at write time: Zanzibar's Leopard index is updated incrementally from the tuple changelog ([Pang et al. 2019, section 3.2.4](https://www.usenix.org/system/files/atc19-pang.pdf)). Claimius writes one source row and one area row per write, constant in users and in the reach of the claim, and pays reachability per reader:

```
write:            ≈ 7 + 4k rows and 31 + 16k index entries for a claim, a binding and k grants, 1.2 ms
first read of u:  A_u × t, the reader's own slice
in all:           U_a × A × t, by readers and the drain
```

The cost that grows with the data is paid once per reader and bounded by what that reader can see.

## What rests on published figures only

1. Zanzibar's latency and availability are for a system of more than 10,000 servers holding over two trillion relations. Claimius has not been run at that scale and no claim is made that it would match it there.

2. Auth0 FGA publishes an availability SLA and no latency SLA; its own documentation says latency depends on the model, the data and the cache ratio ([Auth0 FGA, Testing latency and throughput](https://docs.fga.dev/best-practices/latency-tests)).

3. Entra ID's figures cover sign in and token issuance. It does not decide access to an application's objects; an application that needs that decision builds it, which is what claimius is.

## Ease of use

1. Claimius has four types and two bindings, and every consumer reads through the same functions. An implementing side registers its tables and binds claims to its rows; it writes no model language.

2. Zanzibar and the systems built on it, Auth0 FGA among them, take a schema of relations and rewrites per namespace. That is more expressive, and every question a service asks depends on it being written correctly.

3. Entra ID carries roles and groups in the token, which is the simplest to consume and stops at the granularity of an app role or a group of at most 200 per token.
