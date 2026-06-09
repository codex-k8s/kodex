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

mattercodex_log "проверка Kubernetes-объектов bot-service"
kubectl -n "$MATTERCODEX_NAMESPACE" get configmap "$MATTERCODEX_BOT_SERVICE_CODE_CONFIGMAP" >/dev/null
kubectl -n "$MATTERCODEX_NAMESPACE" get configmap "$MATTERCODEX_BOT_SERVICE_CONFIG_CONFIGMAP" >/dev/null
kubectl -n "$MATTERCODEX_NAMESPACE" get deployment matter-codex-bot-service >/dev/null
kubectl -n "$MATTERCODEX_NAMESPACE" get service matter-codex-bot-service >/dev/null
kubectl -n "$MATTERCODEX_NAMESPACE" get ingress matter-codex-bot-service >/dev/null

BOT_READY="$(kubectl -n "$MATTERCODEX_NAMESPACE" get deployment matter-codex-bot-service -o jsonpath='{.status.readyReplicas}' 2>/dev/null || true)"
mattercodex_log "bot-service готовых реплик: ${BOT_READY:-0}"

if mattercodex_bool "$CHECK_URL"; then
  mattercodex_require_commands curl
  mattercodex_log "проверка публичного health endpoint bot-service"
  curl -fsS --max-time 15 "$MATTERCODEX_BOT_SERVICE_SITE_URL/healthz" >/dev/null
fi

mattercodex_log "bot-service read-only проверка завершена"
