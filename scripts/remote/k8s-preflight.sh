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

NAMESPACE_Q="$(mattercodex_shell_quote "$MATTERCODEX_NAMESPACE")"
REMOTE_KUBECTL="$(mattercodex_remote_kubectl_command)"

mattercodex_log "проверка Kubernetes на целевом сервере"
mattercodex_ssh "set -eu
  $REMOTE_KUBECTL version --client=true >/dev/null
  $REMOTE_KUBECTL get --raw=/readyz >/dev/null
  $REMOTE_KUBECTL auth can-i create namespaces >/dev/null 2>&1
  $REMOTE_KUBECTL auth can-i create secrets -n $NAMESPACE_Q >/dev/null 2>&1
  $REMOTE_KUBECTL auth can-i create statefulsets.apps -n $NAMESPACE_Q >/dev/null 2>&1
  $REMOTE_KUBECTL auth can-i create deployments.apps -n $NAMESPACE_Q >/dev/null 2>&1
  $REMOTE_KUBECTL auth can-i create ingresses.networking.k8s.io -n $NAMESPACE_Q >/dev/null 2>&1
  printf 'удаленный Kubernetes preflight: успешно\n'"
