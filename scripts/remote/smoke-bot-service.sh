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
mattercodex_require_commands ssh

NAMESPACE_Q="$(mattercodex_shell_quote "$MATTERCODEX_NAMESPACE")"
CONFIG_Q="$(mattercodex_shell_quote "$MATTERCODEX_BOT_SERVICE_CONFIG_CONFIGMAP")"
REMOTE_KUBECTL="$(mattercodex_remote_kubectl_command)"

mattercodex_log "read-only проверка bot-service на целевом сервере"
mattercodex_ssh "set -eu
  $REMOTE_KUBECTL -n $NAMESPACE_Q get configmap $CONFIG_Q >/dev/null
  $REMOTE_KUBECTL -n $NAMESPACE_Q get deployment matter-codex-bot-service >/dev/null
  $REMOTE_KUBECTL -n $NAMESPACE_Q get service matter-codex-bot-service >/dev/null
  $REMOTE_KUBECTL -n $NAMESPACE_Q get ingress matter-codex-bot-service >/dev/null
  ready=\"\$($REMOTE_KUBECTL -n $NAMESPACE_Q get deployment matter-codex-bot-service -o jsonpath='{.status.readyReplicas}' 2>/dev/null || true)\"
  printf 'bot-service готовых реплик: %s\n' \"\${ready:-0}\""

if mattercodex_bool "$CHECK_URL"; then
  mattercodex_require_commands curl
  mattercodex_log "проверка полной публичной матрицы bot-service без передачи секретов и рабочих payload"
  slash_status="$(curl -sS --max-time 15 -o /dev/null -w '%{http_code}' -X POST "$MATTERCODEX_BOT_SERVICE_SITE_URL/mattermost/slash/agents")"
  github_status="$(curl -sS --max-time 15 -o /dev/null -w '%{http_code}' -X POST "$MATTERCODEX_BOT_SERVICE_SITE_URL/github/webhook")"
  [ "$slash_status" = "401" ] || mattercodex_die "публичный slash endpoint не подтвердил настроенный fail-closed контур"
  [ "$github_status" = "401" ] || mattercodex_die "публичный GitHub webhook не подтвердил настроенный fail-closed контур"
  internal_paths=(
    "/"
    "/healthz"
    "/health/livez"
    "/health/readyz"
    "/readyz"
    "/metrics"
    "/mattermost/actions/agents"
    "/mattermost/dialogs/agents"
    "/mcp/sessions/synthetic-smoke"
    "/internal/synthetic-smoke"
  )
  for path in "${internal_paths[@]}"; do
    status="$(curl -sS --max-time 15 -o /dev/null -w '%{http_code}' "$MATTERCODEX_BOT_SERVICE_SITE_URL$path")"
    [ "$status" = "404" ] || mattercodex_die "кластерный маршрут опубликован через Ingress: $path"
  done
fi

mattercodex_log "remote bot-service read-only проверка завершена"
