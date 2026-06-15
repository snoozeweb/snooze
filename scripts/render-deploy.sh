#!/usr/bin/env bash
# scripts/render-deploy.sh
#
# Build + push the snoozeweb/snooze image, then redeploy the Render service
# that backs try.snoozeweb.net.
#
# The Render service is configured with runtime: image — it pulls
# docker.io/snoozeweb/snooze:<tag> directly, so a redeploy after the push is
# all that's needed.
#
# Usage:
#   RENDER_API_KEY=... scripts/render-deploy.sh                       # uses defaults
#   RENDER_API_KEY=... scripts/render-deploy.sh 2.0.1                 # tag override
#   RENDER_API_KEY=... RENDER_SERVICE_ID=srv-xxx \
#     scripts/render-deploy.sh 2.0.1                                  # target a different service
#
#   SKIP_BUILD=1 scripts/render-deploy.sh        # already-built image, just redeploy
#   SKIP_PUSH=1  scripts/render-deploy.sh        # local-only rebuild for inspection
#
# Required env:
#   RENDER_API_KEY        Render personal API token (see https://dashboard.render.com/u/settings#api-keys)
#
# Optional env:
#   RENDER_SERVICE_ID     Service to redeploy. Default: try.snoozeweb.net (snoozeweb-go).
#   IMAGE_REPO            Docker repo. Default: snoozeweb/snooze.
#   SKIP_BUILD=1                        Skip the `task docker:build` step.
#   SKIP_PUSH=1                         Skip the `docker push` step (implies no Render redeploy).
#   SKIP_LATEST=1                       Skip tagging+pushing :latest.
#   DEPLOY_TIMEOUT_SECS                 Polling budget (default 600).
#
# Demo data (set these in the Render service's Environment settings):
#   SNOOZE_SERVER_CORE_SEED_DEMO=true   Seed rich demo data on first boot (environments,
#                                       users, rules, actions, notifications, snooze
#                                       filters, 17 alerts, comments). Idempotent —
#                                       safe to leave enabled; re-runs are no-ops.
set -Eeuo pipefail

readonly DEFAULT_TAG="2.0.0"
readonly DEFAULT_SERVICE_ID="srv-d85f3h6gvqtc73bq0cv0"     # try.snoozeweb.net
readonly DEFAULT_IMAGE_REPO="snoozeweb/snooze"
readonly RENDER_API="https://api.render.com/v1"
readonly HEALTH_URL="https://try.snoozeweb.net/healthz"

TAG="${1:-$DEFAULT_TAG}"
SERVICE_ID="${RENDER_SERVICE_ID:-$DEFAULT_SERVICE_ID}"
IMAGE_REPO="${IMAGE_REPO:-$DEFAULT_IMAGE_REPO}"
TIMEOUT="${DEPLOY_TIMEOUT_SECS:-600}"

die() { printf '\033[31merror:\033[0m %s\n' "$*" >&2; exit 1; }
note() { printf '\033[36m==>\033[0m %s\n' "$*"; }

[[ -n "${RENDER_API_KEY:-}" ]] || die "RENDER_API_KEY is not set"
command -v docker >/dev/null   || die "docker not found in PATH"
command -v task >/dev/null     || die "task not found in PATH (https://taskfile.dev)"
command -v curl >/dev/null     || die "curl not found in PATH"
command -v python3 >/dev/null  || die "python3 not found in PATH"

# Repo root — script may be invoked from anywhere.
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

note "Image:   $IMAGE_REPO:$TAG"
note "Service: $SERVICE_ID"

# ----- 1. Build ----------------------------------------------------------------
if [[ "${SKIP_BUILD:-0}" != "1" ]]; then
  note "Building $IMAGE_REPO:$TAG via task docker:build..."
  task docker:build BINARY=snooze-server TAG="$TAG" VERSION="$TAG"
else
  note "SKIP_BUILD=1 — using existing local image"
  docker image inspect "$IMAGE_REPO:$TAG" >/dev/null \
    || die "image $IMAGE_REPO:$TAG not found locally"
fi

# ----- 2. Push (with :latest alias) -------------------------------------------
if [[ "${SKIP_PUSH:-0}" != "1" ]]; then
  note "Pushing $IMAGE_REPO:$TAG..."
  docker push "$IMAGE_REPO:$TAG"
  if [[ "${SKIP_LATEST:-0}" != "1" ]]; then
    note "Tagging + pushing $IMAGE_REPO:latest..."
    docker tag "$IMAGE_REPO:$TAG" "$IMAGE_REPO:latest"
    docker push "$IMAGE_REPO:latest"
  fi
else
  note "SKIP_PUSH=1 — stopping after local build (no Render redeploy)"
  exit 0
fi

# ----- 3. Trigger Render redeploy ---------------------------------------------
note "Triggering redeploy on $SERVICE_ID..."
deploy_resp=$(curl -fsS -X POST \
  -H "Authorization: Bearer $RENDER_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{}' \
  "$RENDER_API/services/$SERVICE_ID/deploys")
deploy_id=$(printf '%s' "$deploy_resp" | python3 -c 'import sys,json; print(json.load(sys.stdin)["id"])')
note "Deploy ID: $deploy_id"

# ----- 4. Poll until terminal -------------------------------------------------
start=$(date +%s)
while :; do
  status=$(curl -fsS \
    -H "Authorization: Bearer $RENDER_API_KEY" \
    -H "Accept: application/json" \
    "$RENDER_API/services/$SERVICE_ID/deploys/$deploy_id" \
    | python3 -c 'import sys,json; print(json.load(sys.stdin).get("status",""))')
  case "$status" in
    live)              note "Deploy is live ($status)"; break ;;
    build_failed|update_failed|canceled|deactivated)
                       die "Deploy ended with status=$status — check the Render dashboard" ;;
    *)                 ;;
  esac
  elapsed=$(( $(date +%s) - start ))
  if (( elapsed > TIMEOUT )); then
    die "Deploy still status=$status after ${TIMEOUT}s — giving up"
  fi
  printf '   [%3ds] status=%s\n' "$elapsed" "$status"
  sleep 8
done

# ----- 5. Smoke check ---------------------------------------------------------
note "Probing $HEALTH_URL..."
http_code=$(curl -s -o /tmp/render-deploy-healthz.out -w '%{http_code}' "$HEALTH_URL" || true)
if [[ "$http_code" == "200" ]]; then
  body=$(cat /tmp/render-deploy-healthz.out)
  note "OK: $http_code $body"
else
  die "$HEALTH_URL returned HTTP $http_code (deploy is live but the app didn't answer)"
fi
