#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
ENV_FILE="${KODEX_SMOKE_ENV_FILE:-${PROJECT_ROOT}/bootstrap/host/config.env}"
RUN_REGISTRY_SMOKE="${KODEX_BACKEND_SMOKE_RUN_REGISTRY:-true}"
DRY_RUN="${KODEX_BACKEND_SMOKE_DRY_RUN:-false}"

log() { echo "[$(date -Is)] $*"; }
die() { echo "ERROR: $*" >&2; exit 1; }

[ -f "$ENV_FILE" ] || die "Env file not found"

if [ "$DRY_RUN" = "true" ]; then
  log "Dry-run only: render backend deploy plan without applying manifests or running smoke jobs"
  KODEX_BACKEND_PLAN_ENV_FILE="$ENV_FILE" "${PROJECT_ROOT}/bootstrap/host/plan_backend_deploy.sh" --skip-live-kubernetes
  exit 0
fi

if [ "$RUN_REGISTRY_SMOKE" = "true" ]; then
  KODEX_SMOKE_ENV_FILE="$ENV_FILE" "${PROJECT_ROOT}/bootstrap/host/smoke_registry_kaniko.sh"
fi

log "Run first backend ring smoke through idempotent deploy path without rebuilding images"
"${PROJECT_ROOT}/bootstrap/host/deploy_backend_ring.sh" --env-file "$ENV_FILE" --skip-build
log "Backend contour smoke finished"
