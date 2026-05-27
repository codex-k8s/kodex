#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
ENV_FILE="${KODEX_SMOKE_ENV_FILE:-${PROJECT_ROOT}/bootstrap/host/config.env}"
RENDER_DIR="${KODEX_SMOKE_RENDER_DIR:-}"
KEEP_RENDER_DIR="${KODEX_SMOKE_KEEP_RENDER_DIR:-false}"
DRY_RUN="${KODEX_SMOKE_DRY_RUN:-false}"

log() { echo "[$(date -Is)] $*"; }
die() { echo "ERROR: $*" >&2; exit 1; }
require_cmd() { command -v "$1" >/dev/null 2>&1 || die "Missing required command: $1"; }

[ -f "$ENV_FILE" ] || die "Env file not found"
require_cmd go
require_cmd kubectl

set -a
# shellcheck disable=SC1090
source "$ENV_FILE"
set +a

KODEX_PRODUCTION_NAMESPACE="${KODEX_PRODUCTION_NAMESPACE:-kodex-prod}"
KODEX_ROLLOUT_TIMEOUT="${KODEX_ROLLOUT_TIMEOUT:-300s}"

if [ -z "$RENDER_DIR" ]; then
  RENDER_DIR="$(mktemp -d)"
  RENDER_DIR_IS_TEMP="true"
else
  RENDER_DIR_IS_TEMP="false"
  rm -rf "$RENDER_DIR"
fi

cleanup() {
  if [ "${RENDER_DIR_IS_TEMP:-false}" = "true" ] && [ "$KEEP_RENDER_DIR" != "true" ]; then
    rm -rf "$RENDER_DIR"
  fi
}
trap cleanup EXIT

log "Render registry and Kaniko smoke manifests"
go run "${PROJECT_ROOT}/cmd/manifest-render" \
  --env-file "$ENV_FILE" \
  --source "${PROJECT_ROOT}/deploy/base/bootstrap-foundation" \
  --output "${RENDER_DIR}/bootstrap-foundation"
go run "${PROJECT_ROOT}/cmd/manifest-render" \
  --env-file "$ENV_FILE" \
  --source "${PROJECT_ROOT}/deploy/base/bootstrap-builder-smoke" \
  --output "${RENDER_DIR}/bootstrap-builder-smoke"

if [ "$DRY_RUN" = "true" ]; then
  log "Dry-run only: smoke manifests rendered but not applied"
  exit 0
fi

log "Apply registry foundation and wait for readiness"
kubectl create namespace "$KODEX_PRODUCTION_NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -k "${RENDER_DIR}/bootstrap-foundation"
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" rollout status deployment/kodex-registry --timeout="$KODEX_ROLLOUT_TIMEOUT"

log "Run registry mirror and Kaniko smoke jobs"
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" delete job \
  kodex-registry-mirror-smoke \
  kodex-registry-pull-smoke \
  kodex-kaniko-build-smoke \
  --ignore-not-found
kubectl apply -k "${RENDER_DIR}/bootstrap-builder-smoke"
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" wait --for=condition=complete job/kodex-registry-mirror-smoke --timeout="$KODEX_ROLLOUT_TIMEOUT"
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" wait --for=condition=complete job/kodex-registry-pull-smoke --timeout="$KODEX_ROLLOUT_TIMEOUT"
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" wait --for=condition=complete job/kodex-kaniko-build-smoke --timeout="$KODEX_ROLLOUT_TIMEOUT"
log "Registry and Kaniko smoke passed"
