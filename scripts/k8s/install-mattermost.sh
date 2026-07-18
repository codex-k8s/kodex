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
ALLOW_POSTGRES_IMAGE_CHANGE=false
POSTGRES_INDEX_VERIFIED=false
POST_MESSAGE_TARGET_BYTES=200000
POST_MESSAGE_SCHEMA_MIGRATION="$REPO_ROOT/deploy/k8s/mattermost/migrations/000001_post_message_max_length.sql"
POSTGRES_INDEX_VERIFICATION="$REPO_ROOT/deploy/k8s/mattermost/maintenance/verify-btree-indexes.sql"

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
    --allow-postgres-image-change)
      ALLOW_POSTGRES_IMAGE_CHANGE=true
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

if [ "$DRY_RUN_MODE" = "none" ]; then
  CURRENT_POSTGRES_IMAGE="$(kubectl -n "$MATTERCODEX_NAMESPACE" get statefulset mattermost-postgres -o jsonpath='{.spec.template.spec.containers[?(@.name=="postgres")].image}' 2>/dev/null || true)"
  if [ -n "$CURRENT_POSTGRES_IMAGE" ] && [ "$CURRENT_POSTGRES_IMAGE" != "$MATTERCODEX_POSTGRES_IMAGE" ]; then
    if ! mattercodex_bool "$ALLOW_POSTGRES_IMAGE_CHANGE"; then
      mattercodex_die "PostgreSQL image изменился; выполните docs/runbooks/postgres-image-change.md и повторите с --allow-postgres-image-change"
    fi
    mattercodex_log "обнаружена явно разрешенная смена PostgreSQL image"
  fi
  if mattercodex_bool "$ALLOW_POSTGRES_IMAGE_CHANGE" && [ -n "$CURRENT_POSTGRES_IMAGE" ]; then
    MATTERMOST_REPLICAS="$(kubectl -n "$MATTERCODEX_NAMESPACE" get deployment mattermost -o jsonpath='{.spec.replicas}' 2>/dev/null || printf '0')"
    BOT_SERVICE_REPLICAS="$(kubectl -n "$MATTERCODEX_NAMESPACE" get deployment matter-codex-bot-service -o jsonpath='{.spec.replicas}' 2>/dev/null || printf '0')"
    if [ "${MATTERMOST_REPLICAS:-0}" -gt 0 ] || [ "${BOT_SERVICE_REPLICAS:-0}" -gt 0 ]; then
      mattercodex_die "перед разрешенной сменой PostgreSQL image остановите Mattermost и bot-service согласно runbook"
    fi
  fi
fi

enable_mattermost_integrations() {
  mattercodex_log "проверяются настройки Mattermost integration accounts"
  local mattermost_pod current changed
  mattermost_pod="$(kubectl -n "$MATTERCODEX_NAMESPACE" get pod -l app.kubernetes.io/name=mattermost -o jsonpath='{.items[0].metadata.name}')"
  changed=false
  current="$(kubectl -n "$MATTERCODEX_NAMESPACE" exec "$mattermost_pod" -c mattermost -- mmctl --local --suppress-warnings config get ServiceSettings.EnableUserAccessTokens 2>/dev/null | tail -n 1 | tr -d '\r')"
  if [ "$current" != "true" ]; then
    kubectl -n "$MATTERCODEX_NAMESPACE" exec "$mattermost_pod" -c mattermost -- mmctl --local --suppress-warnings config set ServiceSettings.EnableUserAccessTokens true >/dev/null
    changed=true
  fi
  current="$(kubectl -n "$MATTERCODEX_NAMESPACE" exec "$mattermost_pod" -c mattermost -- mmctl --local --suppress-warnings config get ServiceSettings.EnableBotAccountCreation 2>/dev/null | tail -n 1 | tr -d '\r')"
  if [ "$current" != "true" ]; then
    kubectl -n "$MATTERCODEX_NAMESPACE" exec "$mattermost_pod" -c mattermost -- mmctl --local --suppress-warnings config set ServiceSettings.EnableBotAccountCreation true >/dev/null
    changed=true
  fi
  if mattercodex_bool "$changed"; then
    kubectl -n "$MATTERCODEX_NAMESPACE" exec "$mattermost_pod" -c mattermost -- mmctl --local --suppress-warnings config reload >/dev/null
  fi
}

mattercodex_log "применяется манифест PostgreSQL"
kubectl apply ${DRY_RUN_ARG:+$DRY_RUN_ARG} -f "$RENDER_DIR/10-postgres.yaml" >/dev/null
if [ "$DRY_RUN_MODE" = "none" ] && mattercodex_bool "$ALLOW_POSTGRES_IMAGE_CHANGE"; then
  mattercodex_log "ожидание PostgreSQL перед проверкой разрешенной смены image"
  kubectl -n "$MATTERCODEX_NAMESPACE" rollout status statefulset/mattermost-postgres --timeout=180s >/dev/null
  mattercodex_log "проверяется целостность B-tree после разрешенной смены PostgreSQL image"
  kubectl -n "$MATTERCODEX_NAMESPACE" exec -i mattermost-postgres-0 -- sh -lc 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 -q' < "$POSTGRES_INDEX_VERIFICATION"
  POSTGRES_INDEX_VERIFIED=true
fi

mattercodex_log "применяются манифесты Mattermost"
kubectl apply ${DRY_RUN_ARG:+$DRY_RUN_ARG} -f "$RENDER_DIR/20-mattermost.yaml" >/dev/null
if mattercodex_bool "$MATTERCODEX_MATTERMOST_OAUTH2_PROXY_ENABLED"; then
  kubectl apply ${DRY_RUN_ARG:+$DRY_RUN_ARG} -f "$RENDER_DIR/25-oauth2-proxy.yaml" >/dev/null
fi
kubectl apply ${DRY_RUN_ARG:+$DRY_RUN_ARG} -f "$RENDER_DIR/30-ingress.yaml" >/dev/null

if [ "$DRY_RUN_MODE" = "none" ]; then
  mattercodex_log "ожидание Mattermost перед schema migration"
  if ! mattercodex_bool "$POSTGRES_INDEX_VERIFIED"; then
    kubectl -n "$MATTERCODEX_NAMESPACE" rollout status statefulset/mattermost-postgres --timeout=180s >/dev/null
  fi
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

  enable_mattermost_integrations
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
