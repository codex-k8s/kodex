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
OAUTH2_PROXY_SECRET_Q="$(mattercodex_shell_quote "$MATTERCODEX_MATTERMOST_OAUTH2_PROXY_SECRET")"
OAUTH2_PROXY_ENABLED=false
if mattercodex_bool "$MATTERCODEX_MATTERMOST_OAUTH2_PROXY_ENABLED"; then
  OAUTH2_PROXY_ENABLED=true
fi
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
  if [ '$OAUTH2_PROXY_ENABLED' = 'true' ]; then
    $REMOTE_KUBECTL -n $NAMESPACE_Q get secret $OAUTH2_PROXY_SECRET_Q >/dev/null
    $REMOTE_KUBECTL -n $NAMESPACE_Q get secret $OAUTH2_PROXY_SECRET_Q -o jsonpath='{.data.OAUTH_CLIENT_ID}' | grep -q .
    $REMOTE_KUBECTL -n $NAMESPACE_Q get secret $OAUTH2_PROXY_SECRET_Q -o jsonpath='{.data.OAUTH_CLIENT_SECRET}' | grep -q .
    $REMOTE_KUBECTL -n $NAMESPACE_Q get secret $OAUTH2_PROXY_SECRET_Q -o jsonpath='{.data.KODEX_OAUTH2_PROXY_COOKIE_SECRET}' | grep -q .
    $REMOTE_KUBECTL -n $NAMESPACE_Q get configmap mattermost-oauth2-proxy >/dev/null
    $REMOTE_KUBECTL -n $NAMESPACE_Q get deployment mattermost-oauth2-proxy >/dev/null
    $REMOTE_KUBECTL -n $NAMESPACE_Q get service mattermost-oauth2-proxy >/dev/null
  fi
  $REMOTE_KUBECTL -n $NAMESPACE_Q get ingress mattermost >/dev/null
  printf 'read-only проверка Kubernetes-объектов: успешно\n'
  POSTGRES_READY=\"\$($REMOTE_KUBECTL -n $NAMESPACE_Q get statefulset mattermost-postgres -o jsonpath='{.status.readyReplicas}' 2>/dev/null || true)\"
  MATTERMOST_READY=\"\$($REMOTE_KUBECTL -n $NAMESPACE_Q get deployment mattermost -o jsonpath='{.status.readyReplicas}' 2>/dev/null || true)\"
  printf 'PostgreSQL готовых реплик: %s\n' \"\${POSTGRES_READY:-0}\"
  printf 'Mattermost готовых реплик: %s\n' \"\${MATTERMOST_READY:-0}\"
  if [ '$OAUTH2_PROXY_ENABLED' = 'true' ]; then
    OAUTH2_PROXY_READY=\"\$($REMOTE_KUBECTL -n $NAMESPACE_Q get deployment mattermost-oauth2-proxy -o jsonpath='{.status.readyReplicas}' 2>/dev/null || true)\"
    printf 'Mattermost OAuth2 proxy готовых реплик: %s\n' \"\${OAUTH2_PROXY_READY:-0}\"
  fi
  POST_MESSAGE_BYTES=\"\$($REMOTE_KUBECTL -n $NAMESPACE_Q exec -i mattermost-postgres-0 -- sh -lc 'psql -U \"\$POSTGRES_USER\" -d \"\$POSTGRES_DB\" -v ON_ERROR_STOP=1 -P pager=off -At' <<'SQL'
select coalesce(max(character_maximum_length), 0)
from information_schema.columns
where lower(table_name) = 'posts'
  and lower(column_name) = 'message';
SQL
)\"
  if [ \"\${POST_MESSAGE_BYTES:-0}\" -gt 0 ]; then
    printf 'Mattermost лимит сообщения, runes: %s\n' \"\$((POST_MESSAGE_BYTES / 4))\"
  fi"

if mattercodex_bool "$CHECK_URL"; then
  mattercodex_require_commands curl
  if mattercodex_bool "$MATTERCODEX_MATTERMOST_OAUTH2_PROXY_ENABLED"; then
    mattercodex_log "проверка публичного OAuth gate Mattermost"
    HTTP_STATUS="$(curl -sS --max-time 15 -o /dev/null -w '%{http_code}' "$MATTERCODEX_MATTERMOST_SITE_URL/api/v4/system/ping")"
    case "$HTTP_STATUS" in
      200) mattercodex_die "Mattermost endpoint доступен без OAuth, ожидался redirect/auth status" ;;
      30*|401|403) ;;
      *) mattercodex_die "неожиданный HTTP status OAuth gate: $HTTP_STATUS" ;;
    esac
  else
    mattercodex_log "проверка публичного ping endpoint Mattermost"
    curl -fsS --max-time 15 "$MATTERCODEX_MATTERMOST_SITE_URL/api/v4/system/ping" >/dev/null
  fi
fi

mattercodex_log "remote read-only проверка завершена"
