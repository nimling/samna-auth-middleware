---
layout: doc
title: Comparison
description: Claimius against Google Zanzibar, SpiceDB, OpenFGA and Auth0 FGA, Ory Keto and Microsoft Entra ID on check latency, throughput, listing, search, writes, availability, staleness and cost per million checks, each claimius figure a formula in the hardware it runs on, each competitor figure a published one, and for each dimension the condition under which claimius wins.
tags: comparison, zanzibar, spicedb, authzed, openfga, auth0 fga, ory keto, entra id, latency, throughput, availability, sla, consistency, staleness, cost
lastUpdated: true
---

# Comparison

Five systems stand where claimius stands. Every claimius figure below is a formula of [Scaling](./scaling.md), instantiated on the `Standard_D2ds_v5` Azure server claimius runs on today and on the self hosted reference node of that file; every competitor figure is published by its vendor or its authors, and says where. For each dimension the comparison ends in an inequality: the condition under which claimius wins.

The symbols are those of [Scaling](./scaling.md): `c_r = 0.11 ms` a check and `t` a settled pair in calibration time, `σ` the speed of one database thread against the calibration core, `K` the capacity of the database in calibration cores, `ρ` its utilization budget, `r_db` the round trip to it, `L_r` a random read.

## At a glance

| | Claimius | Google Zanzibar | SpiceDB | OpenFGA, Auth0 FGA | Ory Keto | Microsoft Entra ID |
|---|---|---|---|---|---|---|
| Where a check runs | In the service's own Postgres | A central service | A central service | A central service | A central service | In the service, from token claims |
| Check latency | `c_r / σ` in the service's query: 0.2 ms on a `D2ds_v5`, 0.14 ms on the reference node | 3.0 ms p50, 9.46 ms p95, 15.0 ms p99 | 5.76 ms p95 at 1 million a second | p99 under 50 ms for the most complex models | No figure published | Local token validation |
| Check throughput | `(1 + R) ρ K / c_r` per database, summed over services | Over 10 million a second across the fleet | 1 million a second on SpiceDB Dedicated | No figure published | No figure published | Not a per object decision |
| Listing | `V_u × 0.043 u`, index only | No list API published | Lookups advised below about 10,000 reachable resources | `ListObjects` up to 1,000 results within 3 s by default | No figure published | Groups and roles in the token, at most 200 groups |
| Staleness of a read | Current on the prophet; `r_net / 2 + a_e / σ` on a disciple, 0.65 ms in region | Snapshot reads with zookies | `minimize_latency` by default, up to the 5 s quantization window | A 10 s cache in `minimize_latency` | No figure published | The token's lifetime |
| Availability | That of the service's own database | Above 99.999 % over three years | No figure published | 99.99 % SLA | No figure published | 99.99 % SLA |
| Cost per million checks | `10^6 c_r p_h / (3600 K)`: 0.011 USD on a `D2ds_v5` at 0.239 USD an hour | Internal | AuthZed Cloud from 2 USD an hour, resource based | Priced by monthly active users, not published | Plan based | Per user licence |

Sources: Zanzibar, [Pang et al. 2019, table 2 and section 4](https://www.usenix.org/system/files/atc19-pang.pdf); SpiceDB, [Google-Scale Authorization: Getting to 1 Million QPS on SpiceDB Dedicated](https://authzed.com/blog/google-scale-authorization), [Consistency](https://authzed.com/docs/spicedb/concepts/consistency), [Commands, `--datastore-revision-quantization-interval`](https://authzed.com/docs/spicedb/concepts/commands), [Protecting a list endpoint](https://authzed.com/docs/spicedb/modeling/protecting-a-list-endpoint), [Pricing](https://authzed.com/pricing); OpenFGA and Auth0 FGA, [Fine Grained Authorization](https://auth0.com/fine-grained-authorization), [Taming P99s in OpenFGA](https://auth0.com/blog/self-tuning-strategy-planner-openfga/), [Consistency modes](https://docs.fga.dev/writing-data/consistency-modes), [Relationship queries](https://openfga.dev/docs/interacting/relationship-queries), [Subscription plans](https://docs.fga.dev/subscription-plans); Ory Keto, [ory/keto](https://github.com/ory/keto); Entra ID, [SLA performance](https://learn.microsoft.com/en-us/entra/identity/monitoring-health/reference-sla-performance), [Access token claims, groups overage](https://learn.microsoft.com/en-us/entra/identity-platform/access-token-claims-reference), [Configurable token lifetimes](https://learn.microsoft.com/en-us/entra/identity-platform/configurable-token-lifetimes); Azure prices from the [Azure retail prices API](https://prices.azure.com/api/retail/prices), Norway East.

## Check latency

A check against a central service costs at least one network round trip `r_s` plus the service's own evaluation `e_s`. A claimius check is a predicate of the query the service sends anyway, so it costs its evaluation and no round trip of its own:

```
central = r_s + e_s        claimius = c_r / σ,  + 2 L_r when the reader's pairs are not cached
c_r = t_s + (2C + L + a + 4) t_d
```

Claimius wins when `c_r / σ < r_s + e_s`. With `e_s` at Zanzibar's published median of 3.0 ms or SpiceDB's p95 of 5.76 ms it holds on any machine and any network. With an ideal central service, `e_s = 0`, it holds whenever the round trip to it exceeds 0.2 ms on a `D2ds_v5` or 0.14 ms on the reference node, which is true of any hop between machines. A central evaluation of a rewrite through `n` levels of usersets costs at least `n` dependent lookups; claimius stores that reachability as the up set on each object, so its evaluation reads one row for it and has no term in the depth of the data, and the theory behind the bound, and behind the prices of 2013 and today, is in [Theory](./theory.md).

The tail is the tail of one Postgres under the M/M/c queue of [Scaling](./scaling.md), which with exponential service times, a pessimistic assumption for a fixed plan, puts the p95 time in system at 5.5 service times on two threads at `ρ = 0.7` and 3.0 on sixty four at `ρ = 0.85`:

| | p50 | p95 | p99 |
|---|---|---|---|
| Claimius on a `D2ds_v5` at 70 % | 0.29 ms | 1.1 ms | 1.6 ms |
| The same with every check missing memory, `+ 2 L_r` | 2.3 ms | 3.1 ms | 3.6 ms |
| Claimius on the reference node at 85 % | 0.10 ms | 0.42 ms | 0.64 ms |
| Zanzibar, published | 3.0 ms | 9.46 ms | 15.0 ms |
| SpiceDB at 1 million a second, published | | 5.76 ms | |

A check issued alone, outside the service's query, adds `r_db`, 1 ms on Azure: still below every published central figure.

## Throughput

A central service carries every check of every client, so its fleet grows with the sum of the load. Claimius has no shared term:

```
X_read = Σ_services min(N_a / c_a, (1 + R) ρ K / c_r)
```

One `D2ds_v5` carries 4,380 checks a second and one reference node 247,000, each times `1 + R` with `R` standbys taking reads. Claimius wins for every service whose own check rate is below `(1 + R) ρ K / c_r`, because below it the checks cost the service nothing it does not already run. SpiceDB's published million a second is one permissions system; a single claimius service reaches it on reference nodes at `1 + R ≥ 10^6 / 247,000`, a primary and four standbys. Zanzibar's ten million a second is a global fleet, and the claimius formula says what that takes: the sum over services, not one larger service.

## Listing

A central service answers "which objects may this user see" by enumeration. OpenFGA's `ListObjects` returns at most 1,000 results within a 3 s deadline by default and is meant for small collections; SpiceDB advises its lookup only below about 10,000 reachable resources. Claimius lists by the reader's own range of the `user_object` primary key, in its own order, keyset paged:

```
ids:        ⌈V_u / 87⌉ leaf pages, index only, 0.03 u a row
rendered:   V_u × 0.043 u, 0.26 µs a row: 20,000 rows in 5.2 ms, 9.4 ms on a D2ds_v5
```

and the implementing side joins that range to its own table in the same query. Claimius wins at every `V_u`: its cost is linear with a constant under a microsecond, where enumeration by a central service is bounded by a deadline.

## Search

No system above searches the text of what a user may reach. A service using one of them searches its own table and filters the `M_q` matches through a batched check, `r_s + M_q e_s`, or lists first and searches the list. Claimius searches inside the reader's reach from whichever side is smaller, `min(V_u c_1, M_q c_2)`, about 5 µs of calibration time a candidate; see [Search](./search.md). It wins whenever `5 µs / σ < e_s` per candidate, which holds against any evaluation that costs more than one index descent.

## Writes

A tuple store writes one tuple per relation: a claim bound to a node and granted to `k` users is `1 + k` tuples in one transaction of its datastore, and a reachability index such as Zanzibar's Leopard is then updated from the changelog. Claimius writes the same source rows and one area row each, constant in users and in reach:

```
write:                 r_db + c_w / σ + L_c, 5.2 ms on a D2ds_v5, 1.65 ms on the reference node
first check of u:      c_r / σ, evaluated from the sources, no settle
first listing of u:    A_u t / σ, the reader's own slice
in all:                U_a A t, by readers and the drain, once per database that serves the app's reads
```

The write is the same order of work as a tuple store's, and a check after it costs its evaluation, whatever the reach of the binding. What claimius pays that a tuple store does not is the materialized listing, `A_u t` once per reader, which a tuple store pays on every enumeration instead. Claimius wins when a listing is read more than once between changes to its slice.

## Availability

A request that needs its data and an access decision succeeds when every component on its path is up. With independent failures:

```
central: A = A_db × A_net × A_authz        claimius: A = A_db
```

The claimius decision adds no factor, because it runs in the database the request already needs, in the same query. A central service at 99.99 % multiplies in 52.6 minutes a year before the network to it; Zanzibar's observed 99.999 % multiplies in 5.3. Claimius wins for any `A_authz < 1`. What it costs is freshness while a disciple's prophet is unreachable; see [Prophets and disciples](./prophets.md).

## Staleness and consistency

| System | A read without a token | A read that must see a write |
|---|---|---|
| Claimius | Current on the prophet; on a disciple `τ_x + r_net / 2 + a_e / σ` behind, 0.65 ms in region and 11 ms from Norway East to West Europe on a `D2ds_v5` | Waits on the write's sequence |
| Zanzibar | Snapshot reads, chosen by the server | Zookie |
| SpiceDB | `minimize_latency`, up to the 5 s quantization window | `at_least_as_fresh` with a ZedToken |
| Auth0 FGA | `MINIMIZE_LATENCY`, a cache of up to 10 s | `HIGHER_CONSISTENCY` |
| Entra ID | The token's lifetime | A new token |

Claimius wins when its disciple staleness is below the window of the default mode of the other system: by a factor of 450 against SpiceDB's 5 s across regions and 7,700 in region, and by twice that against Auth0 FGA's 10 s. The formula has no term in the window a cache chooses, because a disciple is not a cache: it is a replica that applies each change the moment it commits.

## Cost per million checks

A claimius check costs the share `c_r / K` of its database for its duration, so a million of them cost

```
10^6 × c_r × p_h / (3600 K)
```

for a database priced `p_h` an hour: 0.011 USD on a `D2ds_v5` at 0.239 USD an hour and 0.020 USD on a `B2ms` at 0.175 USD, sustained. A central service priced `p_s` an hour and carrying `X` checks a second costs `10^6 p_s / (3600 X)` per million, so claimius is cheaper while

```
X < p_s K / (c_r p_h)
```

52,000 checks a second against AuthZed Cloud's entry price of 2 USD an hour, at `D2ds_v5` prices. Below the knee of [Scaling](./scaling.md) the check runs on capacity the service's database already has, and its marginal cost is zero.

## What rests on published figures only

1. Zanzibar's latency and availability are for a system of more than 10,000 servers holding over two trillion relations; SpiceDB's million a second is a dedicated deployment on CockroachDB holding 100 billion relationships. Claimius has not been run at either scale, and no claim is made that it would match them there. What the formulas claim is per service: every service below its own ceiling is answered faster, fresher, more available and cheaper than by any central service.

2. Auth0 FGA publishes an availability SLA and no latency SLA; its own documentation says latency depends on the model, the data and the cache ratio ([Auth0 FGA, Testing latency and throughput](https://docs.fga.dev/best-practices/latency-tests)). Ory Keto publishes no latency or throughput figure.

3. Entra ID's figures cover sign in and token issuance. It does not decide access to an application's objects; an application that needs that decision builds it, which is what claimius is.

## Ease of use

1. Claimius has four types and two bindings, and every consumer reads through the same functions. An implementing side registers its tables and binds claims to its rows; it writes no model language.

2. Zanzibar and the systems built on it, SpiceDB, OpenFGA and Ory Keto among them, take a schema of relations and rewrites per namespace. That is more expressive, and every question a service asks depends on it being written correctly.

3. Entra ID carries roles and groups in the token, which is the simplest to consume and stops at the granularity of an app role or a group of at most 200 per token.
