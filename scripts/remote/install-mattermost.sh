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
mattercodex_remote_kubectl_apply_stdin "$APPLY_DRY_RUN_MODE" < "$RENDER_DIR/30-ingress.yaml"

if [ "$DRY_RUN_MODE" = "none" ] && mattercodex_bool "$WAIT"; then
  mattercodex_log "ожидание rollout на целевом сервере"
  mattercodex_ssh "set -eu
    $REMOTE_KUBECTL -n $NAMESPACE_Q rollout status statefulset/mattermost-postgres --timeout=180s >/dev/null
    $REMOTE_KUBECTL -n $NAMESPACE_Q rollout status deployment/mattermost --timeout=300s >/dev/null"
fi

mattercodex_log "remote Mattermost install шаг завершен"
