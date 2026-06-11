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
mattercodex_require_commands ssh envsubst

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
  mattercodex_log "копируется OAuth2 proxy secret на целевом сервере"
  SOURCE_NAMESPACE_Q="$(mattercodex_shell_quote "$MATTERCODEX_MATTERMOST_OAUTH2_PROXY_SOURCE_NAMESPACE")"
  SOURCE_SECRET_Q="$(mattercodex_shell_quote "$MATTERCODEX_MATTERMOST_OAUTH2_PROXY_SOURCE_SECRET")"
  TARGET_SECRET_Q="$(mattercodex_shell_quote "$MATTERCODEX_MATTERMOST_OAUTH2_PROXY_SECRET")"
  APPLY_ARG="$(mattercodex_kubectl_dry_run_arg "$APPLY_DRY_RUN_MODE")"
  mattercodex_ssh "set -eu
    command -v jq >/dev/null
    $REMOTE_KUBECTL -n $SOURCE_NAMESPACE_Q get secret $SOURCE_SECRET_Q -o json |
      jq --arg name $TARGET_SECRET_Q --arg namespace $NAMESPACE_Q '
        .data.KODEX_GITHUB_OAUTH_CLIENT_ID as \$client_id |
        .data.KODEX_GITHUB_OAUTH_CLIENT_SECRET as \$client_secret |
        .data.KODEX_OAUTH2_PROXY_COOKIE_SECRET as \$cookie_secret |
        if (\$client_id == null or \$client_secret == null or \$cookie_secret == null) then
          error(\"source OAuth2 secret misses required keys\")
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
              KODEX_GITHUB_OAUTH_CLIENT_ID: \$client_id,
              KODEX_GITHUB_OAUTH_CLIENT_SECRET: \$client_secret,
              KODEX_OAUTH2_PROXY_COOKIE_SECRET: \$cookie_secret
            }
          }
        end' |
      $REMOTE_KUBECTL apply ${APPLY_ARG:+$APPLY_ARG }-f - >/dev/null"
  mattercodex_remote_kubectl_apply_stdin "$APPLY_DRY_RUN_MODE" < "$RENDER_DIR/25-oauth2-proxy.yaml"
fi

mattercodex_remote_kubectl_apply_stdin "$APPLY_DRY_RUN_MODE" < "$RENDER_DIR/30-ingress.yaml"

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
