# Snooze: MongoDB vs PostgreSQL benchmark

Runs done on 2026-05-22 against the local `snoozeweb/snooze:latest` image
(Go rewrite, version 2.0.0, revision `60dbaa55`). Each backend was started
in isolation via the compose files in this directory, the harness was run,
then the stack was torn down with `-v` so the next backend started from a
cold cache. Both backends saw bit-for-bit identical input: the harness
uses a fixed PRNG seed (`0xCAFEBEEF`).

## Setup

| Item | Value |
|---|---|
| Snooze image | `snoozeweb/snooze:latest` (v2.0.0, commit 60dbaa55) |
| Mongo image  | `mongo:7` (single-node replica set `rs0`) |
| Postgres image | `postgres:16-alpine` (single instance) |
| Auth | anonymous-admin (env: `SNOOZE_SERVER_GENERAL_ANONYMOUS_ENABLED=true`, `SNOOZE_SERVER_GENERAL_ANONYMOUS_ADMIN=true`) |
| Seed rules | 200 (100 broad `severity=` + 100 narrow `host=`, each with a SET modification) |
| Seed records | 5,000 (POST /api/v1/alerts via the live pipeline) |
| Pipeline plugins | `rule, aggregaterule, snooze, notification` (defaults) |

The two compose files live next to this report:
[docker-compose.mongo.yaml](docker-compose.mongo.yaml) and
[docker-compose.postgres.yaml](docker-compose.postgres.yaml). The harness
source is [main.go](main.go). To reproduce, run [run.sh](run.sh).

## Phases

1. **`write_burst`** — 5,000 alert POSTs, concurrency 20. Each request goes
   through the full pipeline (rule matching against the cached 200-rule
   tree, then async write).
2. **`read_only`** — 200 iterations of each of four read shapes, concurrency
   10 per shape:
   - `list_record_limit100` (GET `/api/v1/record?limit=100`)
   - `search_by_host` (POST search with `host = srv-web-01`)
   - `search_by_severity` (POST search with `severity = critical`)
   - `search_by_env_and_sev` (POST search with `environment=prod AND severity=error`)
3. **`mixed`** — 15 writer goroutines + 10 reader goroutines running in
   parallel for up to 30s, budgets 3,000 writes and 1,000 reads.

## Headline numbers (all latencies in ms)

### Write burst (5,000 alerts through the pipeline)

|                | MongoDB | PostgreSQL | Mongo / PG |
|----------------|--------:|-----------:|-----------:|
| Throughput     | **290 ops/s** | 172 ops/s | **1.69×** |
| p50 latency    | **64.0** | 112.0 | 0.57× |
| p95 latency    | **119.2** | 168.4 | 0.71× |
| p99 latency    | **159.7** | 213.7 | 0.75× |
| max latency    | 256.3 | 315.7 | 0.81× |
| errors         | 0 | 0 | — |

### Read only

|                          | MongoDB p50 | PG p50 | Mongo p95 | PG p95 |
|--------------------------|------------:|-------:|----------:|-------:|
| `list_record_limit100`   | 68.4 | **50.5** | 115.3 | **85.4** |
| `search_by_host`         | **52.4** | 65.9 | **80.4** | 110.2 |
| `search_by_severity`     | 186.9 | 191.4 | 278.6 | **264.8** |
| `search_by_env_and_sev`  | **67.8** | 92.5 | **111.1** | 158.9 |

Mongo wins on selective predicates that hit indexed-equivalent fields (`host`,
the `AND` shape); Postgres wins on the unindexed list path. Both perform
similarly on the broad `severity` scan (low selectivity dominates).

### Mixed workload

|                            | MongoDB | PostgreSQL |
|----------------------------|--------:|-----------:|
| Total ops in budget        | **4,023** | 3,712 |
| Wall time                  | 23.1 s | 30.1 s (hit duration cap) |
| Effective throughput       | **174 ops/s** | 123 ops/s |
| Writes p50 / p95 / p99     | **107.8** / **191.6** / **280.3** | 159.2 / 304.3 / 353.7 |
| Reads p50 / p95 / p99      | **146.9** / **444.0** / **549.8** | 197.2 / 356.9 / 418.9 |
| Errors                     | 0 | 0 |

Mongo finishes the same 3,000-write + 1,000-read budget ~24 % faster on wall
time, and write tail latency is meaningfully lower. Reader tail latency is
actually *worse* on Mongo under contention (p95/p99 higher than Postgres),
which is consistent with Mongo's WiredTiger taking more locks when both the
pipeline writer and the readers are hammering the same `record` collection.

## Read-on-write fairness, by query (p50/p95 ms)

| Query shape | Mongo (read-only) | PG (read-only) | Mongo (mixed) | PG (mixed) |
|---|---|---|---|---|
| `list_record_limit100`  | 68.4 / 115.3 | 50.5 / 85.4 | (subsumed) | (subsumed) |
| `search_by_host`        | 52.4 / 80.4  | 65.9 / 110.2 | (subsumed) | (subsumed) |
| `search_by_severity`    | 186.9 / 278.6 | 191.4 / 264.8 | (subsumed) | (subsumed) |
| `search_by_env_and_sev` | 67.8 / 111.1 | 92.5 / 158.9 | (subsumed) | (subsumed) |

(The mixed-phase reader randomly samples the three search shapes, so
per-shape breakouts aren't separated in the JSON — overall mixed reads are
in the table above.)

## Interpretation

- **Writes are the headline difference.** Through the full Snooze pipeline
  (rule cache → match → modifications → async insert), Mongo sustains roughly
  **170 % of Postgres's** alert throughput, and write tail latency (p95/p99)
  is consistently lower. This is likely a combination of: the pipeline
  appends to a single hot collection (`record`); Mongo's append-friendly
  storage path; pgx round-trip cost vs the Mongo driver's pooled writes; and
  the `asyncwriter` batch policy interacting differently with each driver.
- **Reads are a wash, with shape-dependent winners.** Mongo wins selective
  equality (`host`, the `AND` shape); Postgres wins the plain list and ties
  on the broad severity scan. Neither was indexed for this benchmark beyond
  whatever the drivers/CRUD plugin set up at boot — production deployments
  that care about a specific query shape should add a covering index.
- **Mongo cluster topology is the operator's choice.** This benchmark used
  a single-node replica set (the minimum to enable change streams, which
  Snooze's syncer needs). Production Mongo deployments will typically run
  3+ nodes, which costs availability headroom but does not directly change
  per-node throughput. Postgres ran as a single instance — the same is true
  for the typical CNPG-fronted production deploy until you add a replica.
- **All runs were error-free.** Both backends handled the workload cleanly;
  this benchmark is a perf comparison, not a stability one.

## Caveats / honest limitations

- **Single host, both backends time-sliced**: the two runs sat on the same
  laptop, ~2 min apart, with the stack torn down (volumes pruned) in
  between. Background noise (other processes, OS page cache state) is
  similar but not identical between runs.
- **Replica set of one** for Mongo: production Mongo deploys use multi-node
  RS for HA; throughput at single-node RS is the best Mongo can do
  resource-equal to single-node Postgres. Adding more Mongo nodes would
  spread read load but raise write costs (oplog).
- **Anonymous-admin auth** bypasses the bootstrap-root-password flow. This
  matters only for the harness setup, not the workload itself — every
  request still hits the same router, middleware chain, and pipeline.
- **No housekeeper interference**: the 1–2 minute run is too short for the
  housekeeper TTL loop to do meaningful work in either backend.
- **No external clients**: the harness ran on the host, talking to the
  container's published port. Container-to-host RTT is in the result.

## Reproducing

```bash
cd bench
./run.sh
ls -l results/   # mongo.json, postgres.json
```

Or step-through:

```bash
go build -o ./bench .
docker compose -f docker-compose.mongo.yaml up -d
./bench -url http://localhost:5200 -backend mongo -out results/mongo.json
docker compose -f docker-compose.mongo.yaml down -v

docker compose -f docker-compose.postgres.yaml up -d
./bench -url http://localhost:5200 -backend postgres -out results/postgres.json
docker compose -f docker-compose.postgres.yaml down -v
```

Raw outputs: [results/mongo.json](results/mongo.json),
[results/postgres.json](results/postgres.json).
