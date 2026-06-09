#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck disable=SC1091
. "$REPO_ROOT/scripts/lib/env.sh"

ENV_FILE="$REPO_ROOT/.env"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --env-file)
      ENV_FILE="$2"
      shift 2
      ;;
    *)
      mattercodex_die "неизвестный аргумент: $1"
      ;;
  esac
done

mattercodex_load_env_file "$ENV_FILE"
mattercodex_validate_base_env
mattercodex_require_commands ssh

REMOTE_KUBECTL="$(mattercodex_remote_kubectl_command)"

mattercodex_log "запуск удаленного preflight без изменений"
mattercodex_ssh "set -eu
  $REMOTE_KUBECTL version --client=true >/dev/null
  $REMOTE_KUBECTL get --raw=/readyz >/dev/null
  test -d /etc/rancher || true
  printf 'удаленный preflight: успешно\n'"
