#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
ENV_FILE="${KODEX_SMOKE_ENV_FILE:-${PROJECT_ROOT}/bootstrap/host/config.env}"
RUN_REGISTRY_SMOKE="${KODEX_BACKEND_SMOKE_RUN_REGISTRY:-true}"
SERVICES="${KODEX_BACKEND_SMOKE_SERVICES:-access-manager project-catalog package-hub provider-hub governance-manager fleet-manager runtime-manager agent-manager codex-hook-ingress integration-gateway}"

log() { echo "[$(date -Is)] $*"; }
die() { echo "ERROR: $*" >&2; exit 1; }

[ -f "$ENV_FILE" ] || die "Env file not found"

if [ "$RUN_REGISTRY_SMOKE" = "true" ]; then
  KODEX_SMOKE_ENV_FILE="$ENV_FILE" "${PROJECT_ROOT}/bootstrap/host/smoke_registry_kaniko.sh"
fi

for service in $SERVICES; do
  script="${PROJECT_ROOT}/scripts/smoke-${service}.sh"
  [ -f "$script" ] || die "Smoke script is missing for ${service}"
  log "Run backend smoke for ${service}"
  KODEX_SMOKE_ENV_FILE="$ENV_FILE" bash "$script"
done

log "Backend contour smoke finished"
