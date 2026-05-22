#!/usr/bin/env bash
# Run the Snooze MongoDB vs PostgreSQL benchmark, sequentially.
#
# Each stack is brought up in isolation: down + volume-prune happens between
# the two so the second backend starts from a cold cache, just like the first.
set -euo pipefail
cd "$(dirname "$0")"

build_bench() {
  if [[ ! -x ./bench ]]; then
    echo "[run] compiling harness"
    go build -o ./bench .
  fi
}

stop_all() {
  echo "[run] stopping any leftover stack"
  docker compose -f docker-compose.mongo.yaml    down -v --remove-orphans 2>/dev/null || true
  docker compose -f docker-compose.postgres.yaml down -v --remove-orphans 2>/dev/null || true
}

run_backend() {
  local backend="$1"
  local compose="$2"
  echo "================================================================"
  echo "[run] BACKEND: $backend"
  echo "================================================================"
  docker compose -f "$compose" up -d
  trap 'docker compose -f "$compose" logs --tail=200 || true; docker compose -f "$compose" down -v --remove-orphans || true' ERR
  ./bench -url http://localhost:5200 -backend "$backend" -out "results/${backend}.json"
  echo "[run] ---- snooze logs (tail 60) for $backend ----"
  docker compose -f "$compose" logs --tail=60 snooze || true
  docker compose -f "$compose" down -v --remove-orphans
  trap - ERR
}

stop_all
build_bench

run_backend mongo    docker-compose.mongo.yaml
run_backend postgres docker-compose.postgres.yaml

echo "[run] both runs done. results/ has the two JSON reports."
ls -l results/
