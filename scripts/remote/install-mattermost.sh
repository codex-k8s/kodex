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
mattercodex_require_commands ssh envsubst base64

if [ -z "$RENDER_DIR" ]; then
  RENDER_DIR="$(mktemp -d)"
fi

"$REPO_ROOT/scripts/k8s/render-mattermost.sh" --env-file "$ENV_FILE" --render-dir "$RENDER_DIR" >/dev/null

APPLY_DRY_RUN_MODE="$DRY_RUN_MODE"
NAMESPACE_Q="$(mattercodex_shell_quote "$MATTERCODEX_NAMESPACE")"
REMOTE_KUBECTL="$(mattercodex_remote_kubectl_command)"

if [ "$DRY_RUN_MODE" = "server" ] && ! mattercodex_ssh "$REMOTE_KUBECTL get namespace $NAMESPACE_Q >/dev/null 2>&1"; then
  mattercodex_log "namespace еще не создан; Mattermost manifests проверяются через remote client dry-run"
  APPLY_DRY_RUN_MODE="client"
fi

mattercodex_log "применяются манифесты Mattermost на целевом сервере"
mattercodex_remote_kubectl_apply_stdin "$APPLY_DRY_RUN_MODE" < "$RENDER_DIR/10-postgres.yaml"
mattercodex_remote_kubectl_apply_stdin "$APPLY_DRY_RUN_MODE" < "$RENDER_DIR/20-mattermost.yaml"

if mattercodex_bool "$MATTERCODEX_MATTERMOST_OAUTH2_PROXY_ENABLED"; then
  mattercodex_log "синхронизируется OAuth2 proxy secret на целевом сервере"
  TARGET_SECRET_Q="$(mattercodex_shell_quote "$MATTERCODEX_MATTERMOST_OAUTH2_PROXY_SECRET")"
  APPLY_ARG="$(mattercodex_kubectl_dry_run_arg "$APPLY_DRY_RUN_MODE")"
  SECRET_APPLY_EXTRA=""
  if [ "$APPLY_DRY_RUN_MODE" != "client" ]; then
    SECRET_APPLY_EXTRA="--server-side"
  fi
  CLIENT_ID_B64=""
  CLIENT_SECRET_B64=""
  COOKIE_SECRET_B64=""
  if [ -n "${MATTERCODEX_MATTERMOST_OAUTH2_PROXY_CLIENT_ID:-}" ]; then
    CLIENT_ID_B64="$(printf '%s' "$MATTERCODEX_MATTERMOST_OAUTH2_PROXY_CLIENT_ID" | base64 | tr -d '\n')"
  fi
  if [ -n "${MATTERCODEX_MATTERMOST_OAUTH2_PROXY_CLIENT_SECRET:-}" ]; then
    CLIENT_SECRET_B64="$(printf '%s' "$MATTERCODEX_MATTERMOST_OAUTH2_PROXY_CLIENT_SECRET" | base64 | tr -d '\n')"
  fi
  if [ -z "${MATTERCODEX_MATTERMOST_OAUTH2_PROXY_COOKIE_SECRET:-}" ]; then
    MATTERCODEX_MATTERMOST_OAUTH2_PROXY_COOKIE_SECRET="$(mattercodex_generate_oauth2_cookie_secret)"
  fi
  COOKIE_SECRET_B64="$(printf '%s' "$MATTERCODEX_MATTERMOST_OAUTH2_PROXY_COOKIE_SECRET" | base64 | tr -d '\n')"
  CLIENT_ID_B64_Q="$(mattercodex_shell_quote "$CLIENT_ID_B64")"
  CLIENT_SECRET_B64_Q="$(mattercodex_shell_quote "$CLIENT_SECRET_B64")"
  COOKIE_SECRET_B64_Q="$(mattercodex_shell_quote "$COOKIE_SECRET_B64")"
  mattercodex_ssh "set -eu
    command -v jq >/dev/null
    EXISTING_JSON=\"\$($REMOTE_KUBECTL -n $NAMESPACE_Q get secret $TARGET_SECRET_Q -o json 2>/dev/null || printf '{\"data\":{}}')\"
    printf '%s' \"\$EXISTING_JSON\" |
      jq \
        --arg name $TARGET_SECRET_Q \
        --arg namespace $NAMESPACE_Q \
        --arg client_id_b64 $CLIENT_ID_B64_Q \
        --arg client_secret_b64 $CLIENT_SECRET_B64_Q \
        --arg cookie_secret_b64 $COOKIE_SECRET_B64_Q '
        def provided_or_existing(\$provided; \$existing):
          if \$provided != \"\" then \$provided
          elif (\$existing != null and \$existing != \"\") then \$existing
          else null end;
        def existing_or_provided(\$provided; \$existing):
          if (\$existing != null and \$existing != \"\") then \$existing
          elif \$provided != \"\" then \$provided
          else null end;
        .data.OAUTH_CLIENT_ID as \$existing_client_id |
        .data.OAUTH_CLIENT_SECRET as \$existing_client_secret |
        .data.KODEX_OAUTH2_PROXY_COOKIE_SECRET as \$existing_cookie_secret |
        provided_or_existing(\$client_id_b64; \$existing_client_id) as \$client_id |
        provided_or_existing(\$client_secret_b64; \$existing_client_secret) as \$client_secret |
        existing_or_provided(\$cookie_secret_b64; \$existing_cookie_secret) as \$cookie_secret |
        if (\$client_id == null or \$client_secret == null or \$cookie_secret == null) then
          error(\"OAuth2 secret misses required keys\")
        else
          {
            apiVersion: \"v1\",
            kind: \"Secret\",
            type: \"Opaque\",
            metadata: {
              name: \$name,
              namespace: \$namespace,
              labels: {
                \"app.kubernetes.io/name\": \"mattermost-oauth2-proxy\",
                \"app.kubernetes.io/component\": \"oauth2-proxy\"
              }
            },
            data: {
              OAUTH_CLIENT_ID: \$client_id,
              OAUTH_CLIENT_SECRET: \$client_secret,
              KODEX_OAUTH2_PROXY_COOKIE_SECRET: \$cookie_secret
            }
          }
        end' |
      $REMOTE_KUBECTL apply ${SECRET_APPLY_EXTRA:+$SECRET_APPLY_EXTRA }${APPLY_ARG:+$APPLY_ARG }-f - >/dev/null
    if [ '$APPLY_DRY_RUN_MODE' = 'none' ]; then
      $REMOTE_KUBECTL -n $NAMESPACE_Q annotate secret $TARGET_SECRET_Q kubectl.kubernetes.io/last-applied-configuration- --overwrite >/dev/null 2>&1 || true
    fi"
  mattercodex_remote_kubectl_apply_stdin "$APPLY_DRY_RUN_MODE" < "$RENDER_DIR/25-oauth2-proxy.yaml"
fi

mattercodex_remote_kubectl_apply_stdin "$APPLY_DRY_RUN_MODE" < "$RENDER_DIR/30-ingress.yaml"

if [ "$DRY_RUN_MODE" = "none" ]; then
  mattercodex_log "ожидание Mattermost перед schema migration"
  mattercodex_ssh "set -eu
    $REMOTE_KUBECTL -n $NAMESPACE_Q rollout status statefulset/mattermost-postgres --timeout=180s >/dev/null
    $REMOTE_KUBECTL -n $NAMESPACE_Q rollout status deployment/mattermost --timeout=300s >/dev/null"

  POST_MESSAGE_BYTES="$(mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q exec -i mattermost-postgres-0 -- sh -lc 'psql -U \"\$POSTGRES_USER\" -d \"\$POSTGRES_DB\" -v ON_ERROR_STOP=1 -P pager=off -At'" <<'SQL'
select coalesce(max(character_maximum_length), 0)
from information_schema.columns
where lower(table_name) = 'posts'
  and lower(column_name) = 'message';
SQL
)"
  if [ "${POST_MESSAGE_BYTES:-0}" -lt "$POST_MESSAGE_TARGET_BYTES" ]; then
    mattercodex_log "применяется Mattermost schema migration для лимита сообщений"
    mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q exec -i mattermost-postgres-0 -- sh -lc 'psql -U \"\$POSTGRES_USER\" -d \"\$POSTGRES_DB\" -v ON_ERROR_STOP=1 -q'" < "$POST_MESSAGE_SCHEMA_MIGRATION"
    mattercodex_ssh "set -eu
      $REMOTE_KUBECTL -n $NAMESPACE_Q rollout restart deployment/mattermost >/dev/null
      $REMOTE_KUBECTL -n $NAMESPACE_Q rollout status deployment/mattermost --timeout=300s >/dev/null"
  else
    mattercodex_log "Mattermost schema migration для лимита сообщений уже применена"
  fi
elif [ "$DRY_RUN_MODE" = "server" ] || [ "$DRY_RUN_MODE" = "client" ]; then
  mattercodex_log "Mattermost schema migration для лимита сообщений пропущена в dry-run"
fi

if [ "$DRY_RUN_MODE" = "none" ] && mattercodex_bool "$WAIT"; then
  mattercodex_log "ожидание rollout на целевом сервере"
  mattercodex_ssh "set -eu
    $REMOTE_KUBECTL -n $NAMESPACE_Q rollout status statefulset/mattermost-postgres --timeout=180s >/dev/null
    $REMOTE_KUBECTL -n $NAMESPACE_Q rollout status deployment/mattermost --timeout=300s >/dev/null"
  if mattercodex_bool "$MATTERCODEX_MATTERMOST_OAUTH2_PROXY_ENABLED"; then
    mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q rollout status deployment/mattermost-oauth2-proxy --timeout=180s >/dev/null"
  fi
fi

mattercodex_log "remote Mattermost install шаг завершен"
