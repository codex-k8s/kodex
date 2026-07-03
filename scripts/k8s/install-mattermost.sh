#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck disable=SC1091
. "$REPO_ROOT/scripts/lib/env.sh"

ENV_FILE="$REPO_ROOT/.env"
DRY_RUN_MODE="server"
WAIT=false
RENDER_DIR=""
POST_MESSAGE_TARGET_BYTES=200000
POST_MESSAGE_SCHEMA_MIGRATION="$REPO_ROOT/deploy/k8s/mattermost/migrations/000001_post_message_max_length.sql"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --env-file)
      ENV_FILE="$2"
      shift 2
      ;;
    --apply)
      DRY_RUN_MODE="none"
      shift
      ;;
    --wait)
      WAIT=true
      shift
      ;;
    --dry-run=server)
      DRY_RUN_MODE="server"
      shift
      ;;
    --dry-run=client)
      DRY_RUN_MODE="client"
      shift
      ;;
    --render-dir)
      RENDER_DIR="$2"
      shift 2
      ;;
    *)
      mattercodex_die "неизвестный аргумент: $1"
      ;;
  esac
done

mattercodex_load_env_file "$ENV_FILE"
mattercodex_validate_base_env
mattercodex_require_commands kubectl envsubst

if [ -z "$RENDER_DIR" ]; then
  RENDER_DIR="$(mktemp -d)"
fi

"$SCRIPT_DIR/render-mattermost.sh" --env-file "$ENV_FILE" --render-dir "$RENDER_DIR" >/dev/null

DRY_RUN_ARG="$(mattercodex_kubectl_dry_run_arg "$DRY_RUN_MODE")"

enable_mattermost_user_access_tokens() {
  mattercodex_log "проверяется Mattermost user access token config"
  local mattermost_pod current
  mattermost_pod="$(kubectl -n "$MATTERCODEX_NAMESPACE" get pod -l app.kubernetes.io/name=mattermost -o jsonpath='{.items[0].metadata.name}')"
  current="$(kubectl -n "$MATTERCODEX_NAMESPACE" exec "$mattermost_pod" -c mattermost -- mmctl --local --suppress-warnings config get ServiceSettings.EnableUserAccessTokens 2>/dev/null | tail -n 1 | tr -d '\r')"
  if [ "$current" = "true" ]; then
    return
  fi
  kubectl -n "$MATTERCODEX_NAMESPACE" exec "$mattermost_pod" -c mattermost -- mmctl --local --suppress-warnings config set ServiceSettings.EnableUserAccessTokens true >/dev/null
  kubectl -n "$MATTERCODEX_NAMESPACE" exec "$mattermost_pod" -c mattermost -- mmctl --local --suppress-warnings config reload >/dev/null
}

mattercodex_log "применяются манифесты Mattermost"
kubectl apply ${DRY_RUN_ARG:+$DRY_RUN_ARG} -f "$RENDER_DIR/10-postgres.yaml" >/dev/null
kubectl apply ${DRY_RUN_ARG:+$DRY_RUN_ARG} -f "$RENDER_DIR/20-mattermost.yaml" >/dev/null
if mattercodex_bool "$MATTERCODEX_MATTERMOST_OAUTH2_PROXY_ENABLED"; then
  kubectl apply ${DRY_RUN_ARG:+$DRY_RUN_ARG} -f "$RENDER_DIR/25-oauth2-proxy.yaml" >/dev/null
fi
kubectl apply ${DRY_RUN_ARG:+$DRY_RUN_ARG} -f "$RENDER_DIR/30-ingress.yaml" >/dev/null

if [ "$DRY_RUN_MODE" = "none" ]; then
  mattercodex_log "ожидание Mattermost перед schema migration"
  kubectl -n "$MATTERCODEX_NAMESPACE" rollout status statefulset/mattermost-postgres --timeout=180s >/dev/null
  kubectl -n "$MATTERCODEX_NAMESPACE" rollout status deployment/mattermost --timeout=300s >/dev/null
  POST_MESSAGE_BYTES="$(kubectl -n "$MATTERCODEX_NAMESPACE" exec -i mattermost-postgres-0 -- sh -lc 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 -P pager=off -At' <<'SQL'
select coalesce(max(character_maximum_length), 0)
from information_schema.columns
where lower(table_name) = 'posts'
  and lower(column_name) = 'message';
SQL
)"
  if [ "${POST_MESSAGE_BYTES:-0}" -lt "$POST_MESSAGE_TARGET_BYTES" ]; then
    mattercodex_log "применяется Mattermost schema migration для лимита сообщений"
    kubectl -n "$MATTERCODEX_NAMESPACE" exec -i mattermost-postgres-0 -- sh -lc 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 -q' < "$POST_MESSAGE_SCHEMA_MIGRATION"
    kubectl -n "$MATTERCODEX_NAMESPACE" rollout restart deployment/mattermost >/dev/null
    kubectl -n "$MATTERCODEX_NAMESPACE" rollout status deployment/mattermost --timeout=300s >/dev/null
  else
    mattercodex_log "Mattermost schema migration для лимита сообщений уже применена"
  fi

  enable_mattermost_user_access_tokens
else
  mattercodex_log "Mattermost schema migration для лимита сообщений пропущена в dry-run"
fi

if [ "$DRY_RUN_MODE" = "none" ] && mattercodex_bool "$WAIT"; then
  mattercodex_log "ожидание rollout PostgreSQL"
  kubectl -n "$MATTERCODEX_NAMESPACE" rollout status statefulset/mattermost-postgres --timeout=180s >/dev/null
  mattercodex_log "ожидание rollout Mattermost"
  kubectl -n "$MATTERCODEX_NAMESPACE" rollout status deployment/mattermost --timeout=300s >/dev/null
  if mattercodex_bool "$MATTERCODEX_MATTERMOST_OAUTH2_PROXY_ENABLED"; then
    mattercodex_log "ожидание rollout Mattermost OAuth2 proxy"
    kubectl -n "$MATTERCODEX_NAMESPACE" rollout status deployment/mattermost-oauth2-proxy --timeout=180s >/dev/null
  fi
fi

mattercodex_log "Mattermost install шаг завершен"
