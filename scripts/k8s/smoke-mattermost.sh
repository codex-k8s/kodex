#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck disable=SC1091
. "$REPO_ROOT/scripts/lib/env.sh"

ENV_FILE="$REPO_ROOT/.env"
CHECK_URL=false

while [ "$#" -gt 0 ]; do
  case "$1" in
    --env-file)
      ENV_FILE="$2"
      shift 2
      ;;
    --check-url)
      CHECK_URL=true
      shift
      ;;
    *)
      mattercodex_die "неизвестный аргумент: $1"
      ;;
  esac
done

mattercodex_load_env_file "$ENV_FILE"
mattercodex_validate_base_env
mattercodex_require_commands kubectl

mattercodex_log "проверка namespace"
kubectl get namespace "$MATTERCODEX_NAMESPACE" >/dev/null

mattercodex_log "проверка Kubernetes-объектов"
kubectl -n "$MATTERCODEX_NAMESPACE" get secret "$MATTERCODEX_POSTGRES_SECRET" >/dev/null
kubectl -n "$MATTERCODEX_NAMESPACE" get statefulset mattermost-postgres >/dev/null
kubectl -n "$MATTERCODEX_NAMESPACE" get service mattermost-postgres >/dev/null
kubectl -n "$MATTERCODEX_NAMESPACE" get pvc mattermost-data >/dev/null
kubectl -n "$MATTERCODEX_NAMESPACE" get deployment mattermost >/dev/null
kubectl -n "$MATTERCODEX_NAMESPACE" get service mattermost >/dev/null
kubectl -n "$MATTERCODEX_NAMESPACE" get ingress mattermost >/dev/null

POSTGRES_READY="$(kubectl -n "$MATTERCODEX_NAMESPACE" get statefulset mattermost-postgres -o jsonpath='{.status.readyReplicas}' 2>/dev/null || true)"
MATTERMOST_READY="$(kubectl -n "$MATTERCODEX_NAMESPACE" get deployment mattermost -o jsonpath='{.status.readyReplicas}' 2>/dev/null || true)"

mattercodex_log "PostgreSQL готовых реплик: ${POSTGRES_READY:-0}"
mattercodex_log "Mattermost готовых реплик: ${MATTERMOST_READY:-0}"

if mattercodex_bool "$CHECK_URL"; then
  mattercodex_require_commands curl
  mattercodex_log "проверка публичного ping endpoint Mattermost"
  curl -fsS --max-time 15 "$MATTERCODEX_MATTERMOST_SITE_URL/api/v4/system/ping" >/dev/null
fi

mattercodex_log "read-only проверка завершена"
