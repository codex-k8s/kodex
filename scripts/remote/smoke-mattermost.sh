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
SECRET_Q="$(mattercodex_shell_quote "$MATTERCODEX_POSTGRES_SECRET")"
REMOTE_KUBECTL="$(mattercodex_remote_kubectl_command)"

mattercodex_log "read-only проверка Mattermost на целевом сервере"
mattercodex_ssh "set -eu
  $REMOTE_KUBECTL get namespace $NAMESPACE_Q >/dev/null
  $REMOTE_KUBECTL -n $NAMESPACE_Q get secret $SECRET_Q >/dev/null
  $REMOTE_KUBECTL -n $NAMESPACE_Q get statefulset mattermost-postgres >/dev/null
  $REMOTE_KUBECTL -n $NAMESPACE_Q get service mattermost-postgres >/dev/null
  $REMOTE_KUBECTL -n $NAMESPACE_Q get pvc mattermost-data >/dev/null
  $REMOTE_KUBECTL -n $NAMESPACE_Q get deployment mattermost >/dev/null
  $REMOTE_KUBECTL -n $NAMESPACE_Q get service mattermost >/dev/null
  $REMOTE_KUBECTL -n $NAMESPACE_Q get ingress mattermost >/dev/null
  printf 'read-only проверка Kubernetes-объектов: успешно\n'"

if mattercodex_bool "$CHECK_URL"; then
  mattercodex_require_commands curl
  mattercodex_log "проверка публичного ping endpoint Mattermost"
  curl -fsS --max-time 15 "$MATTERCODEX_MATTERMOST_SITE_URL/api/v4/system/ping" >/dev/null
fi

mattercodex_log "remote read-only проверка завершена"
