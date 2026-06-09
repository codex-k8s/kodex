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
mattercodex_require_commands python3 ssh base64
mattercodex_require_env MATTERCODEX_MATTERMOST_BOT_TOKEN

TOKEN_FILE="$(mktemp)"
chmod 600 "$TOKEN_FILE"
trap 'rm -f "$TOKEN_FILE"' EXIT

mattercodex_log "создается Mattermost control surface через API"
MATTERCODEX_SLASH_TOKEN_OUTPUT="$TOKEN_FILE" \
  python3 "$REPO_ROOT/scripts/mattermost/provision-control-surface.py"

SLASH_TOKEN="$(cat "$TOKEN_FILE")"
if [ -z "$SLASH_TOKEN" ]; then
  mattercodex_die "slash command token не был получен"
fi

BOT_TOKEN_B64="$(printf '%s' "$MATTERCODEX_MATTERMOST_BOT_TOKEN" | base64 | tr -d '\n')"
SLASH_TOKEN_B64="$(printf '%s' "$SLASH_TOKEN" | base64 | tr -d '\n')"

NAMESPACE_Q="$(mattercodex_shell_quote "$MATTERCODEX_NAMESPACE")"
REMOTE_KUBECTL="$(mattercodex_remote_kubectl_command)"

mattercodex_log "обновляется bot-service secret на целевом сервере"
cat <<EOF | mattercodex_remote_kubectl_apply_stdin "none"
apiVersion: v1
kind: Secret
metadata:
  name: ${MATTERCODEX_BOT_SERVICE_SECRET}
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: matter-codex-bot-service
    app.kubernetes.io/component: bot-service-secret
type: Opaque
data:
  mattermost-bot-token: ${BOT_TOKEN_B64}
  mattermost-slash-token: ${SLASH_TOKEN_B64}
EOF

if mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q get deployment matter-codex-bot-service >/dev/null 2>&1"; then
  mattercodex_log "перезапускается bot-service для применения secret"
  mattercodex_ssh "set -eu
    $REMOTE_KUBECTL -n $NAMESPACE_Q rollout restart deployment/matter-codex-bot-service >/dev/null
    $REMOTE_KUBECTL -n $NAMESPACE_Q rollout status deployment/matter-codex-bot-service --timeout=180s >/dev/null"
fi

mattercodex_log "Mattermost bot-service provisioning завершен"
