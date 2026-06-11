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
mattercodex_require_commands kubectl envsubst

if [ -z "$RENDER_DIR" ]; then
  RENDER_DIR="$(mktemp -d)"
fi

"$SCRIPT_DIR/render-mattermost.sh" --env-file "$ENV_FILE" --render-dir "$RENDER_DIR" >/dev/null

DRY_RUN_ARG="$(mattercodex_kubectl_dry_run_arg "$DRY_RUN_MODE")"

mattercodex_log "применяются манифесты Mattermost"
kubectl apply ${DRY_RUN_ARG:+$DRY_RUN_ARG} -f "$RENDER_DIR/10-postgres.yaml" >/dev/null
kubectl apply ${DRY_RUN_ARG:+$DRY_RUN_ARG} -f "$RENDER_DIR/20-mattermost.yaml" >/dev/null
if mattercodex_bool "$MATTERCODEX_MATTERMOST_OAUTH2_PROXY_ENABLED"; then
  kubectl apply ${DRY_RUN_ARG:+$DRY_RUN_ARG} -f "$RENDER_DIR/25-oauth2-proxy.yaml" >/dev/null
fi
kubectl apply ${DRY_RUN_ARG:+$DRY_RUN_ARG} -f "$RENDER_DIR/30-ingress.yaml" >/dev/null

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
