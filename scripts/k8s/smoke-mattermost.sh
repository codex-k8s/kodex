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
if mattercodex_bool "$MATTERCODEX_MATTERMOST_OAUTH2_PROXY_ENABLED"; then
  kubectl -n "$MATTERCODEX_NAMESPACE" get secret "$MATTERCODEX_MATTERMOST_OAUTH2_PROXY_SECRET" >/dev/null
  kubectl -n "$MATTERCODEX_NAMESPACE" get secret "$MATTERCODEX_MATTERMOST_OAUTH2_PROXY_SECRET" -o jsonpath='{.data.OAUTH_CLIENT_ID}' | grep -q .
  kubectl -n "$MATTERCODEX_NAMESPACE" get secret "$MATTERCODEX_MATTERMOST_OAUTH2_PROXY_SECRET" -o jsonpath='{.data.OAUTH_CLIENT_SECRET}' | grep -q .
  kubectl -n "$MATTERCODEX_NAMESPACE" get secret "$MATTERCODEX_MATTERMOST_OAUTH2_PROXY_SECRET" -o jsonpath='{.data.KODEX_OAUTH2_PROXY_COOKIE_SECRET}' | grep -q .
  kubectl -n "$MATTERCODEX_NAMESPACE" get configmap mattermost-oauth2-proxy >/dev/null
  kubectl -n "$MATTERCODEX_NAMESPACE" get deployment mattermost-oauth2-proxy >/dev/null
  kubectl -n "$MATTERCODEX_NAMESPACE" get service mattermost-oauth2-proxy >/dev/null
fi
kubectl -n "$MATTERCODEX_NAMESPACE" get ingress mattermost >/dev/null

POSTGRES_READY="$(kubectl -n "$MATTERCODEX_NAMESPACE" get statefulset mattermost-postgres -o jsonpath='{.status.readyReplicas}' 2>/dev/null || true)"
MATTERMOST_READY="$(kubectl -n "$MATTERCODEX_NAMESPACE" get deployment mattermost -o jsonpath='{.status.readyReplicas}' 2>/dev/null || true)"

mattercodex_log "PostgreSQL готовых реплик: ${POSTGRES_READY:-0}"
mattercodex_log "Mattermost готовых реплик: ${MATTERMOST_READY:-0}"
if mattercodex_bool "$MATTERCODEX_MATTERMOST_OAUTH2_PROXY_ENABLED"; then
  OAUTH2_PROXY_READY="$(kubectl -n "$MATTERCODEX_NAMESPACE" get deployment mattermost-oauth2-proxy -o jsonpath='{.status.readyReplicas}' 2>/dev/null || true)"
  mattercodex_log "Mattermost OAuth2 proxy готовых реплик: ${OAUTH2_PROXY_READY:-0}"
fi
POST_MESSAGE_BYTES="$(kubectl -n "$MATTERCODEX_NAMESPACE" exec -i mattermost-postgres-0 -- sh -lc 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 -P pager=off -At' <<'SQL'
select coalesce(max(character_maximum_length), 0)
from information_schema.columns
where lower(table_name) = 'posts'
  and lower(column_name) = 'message';
SQL
)"
if [ "${POST_MESSAGE_BYTES:-0}" -gt 0 ]; then
  mattercodex_log "Mattermost лимит сообщения, runes: $((POST_MESSAGE_BYTES / 4))"
fi

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

mattercodex_log "read-only проверка завершена"
