#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck disable=SC1091
. "$REPO_ROOT/scripts/lib/env.sh"

ENV_FILE="$REPO_ROOT/.env"
CHECK_KUBECTL=false

while [ "$#" -gt 0 ]; do
  case "$1" in
    --env-file)
      ENV_FILE="$2"
      shift 2
      ;;
    --require-kubernetes)
      CHECK_KUBECTL=true
      shift
      ;;
    *)
      mattercodex_die "неизвестный аргумент: $1"
      ;;
  esac
done

mattercodex_load_env_file "$ENV_FILE"
mattercodex_validate_base_env
mattercodex_require_commands envsubst sed

if mattercodex_bool "$CHECK_KUBECTL"; then
  mattercodex_require_commands kubectl
  kubectl version --client=true >/dev/null
  mattercodex_log "kubectl client: доступен"
  CURRENT_CONTEXT="$(kubectl config current-context 2>/dev/null || true)"
  if [ -n "$CURRENT_CONTEXT" ]; then
    if ! kubectl config get-contexts "$CURRENT_CONTEXT" --no-headers >/dev/null 2>&1; then
      mattercodex_die "kubectl current context выбран, но не описан в kubeconfig"
    fi
    mattercodex_log "kubectl context: настроен"
  else
    mattercodex_log "kubectl context: не выбран"
  fi
fi

mattercodex_log "env-файл: загружен"
mattercodex_log "обязательные env-ключи: заданы"
mattercodex_log "namespace: настроен"
mattercodex_log "Mattermost URL: настроен"
