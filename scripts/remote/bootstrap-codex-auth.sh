#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck disable=SC1091
. "$REPO_ROOT/scripts/lib/env.sh"

ENV_FILE="$REPO_ROOT/.env"
AUTH_JSON_PATH=""
TIMEOUT_SECONDS=600
POD_NAME="matter-codex-codex-device-auth"
ACCOUNT_NAME=""

while [ "$#" -gt 0 ]; do
  case "$1" in
    --env-file)
      ENV_FILE="$2"
      shift 2
      ;;
    --auth-json)
      AUTH_JSON_PATH="$2"
      shift 2
      ;;
    --account)
      ACCOUNT_NAME="$2"
      shift 2
      ;;
    --timeout)
      TIMEOUT_SECONDS="$2"
      shift 2
      ;;
    *)
      mattercodex_die "неизвестный аргумент: $1"
      ;;
  esac
done

mattercodex_load_env_file "$ENV_FILE"
mattercodex_validate_base_env
mattercodex_require_commands ssh base64
ACCOUNT_NAME="${ACCOUNT_NAME:-${MATTERCODEX_CODEX_AUTH_ACCOUNT:-primary}}"
CODEX_AUTH_SECRET_NAME="${MATTERCODEX_CODEX_AUTH_SECRET}-${ACCOUNT_NAME}"

NAMESPACE_Q="$(mattercodex_shell_quote "$MATTERCODEX_NAMESPACE")"
REMOTE_KUBECTL="$(mattercodex_remote_kubectl_command)"

mattercodex_apply_codex_auth_secret() {
  local auth_json_b64="$1"
  cat <<EOF | mattercodex_remote_kubectl_apply_stdin none
apiVersion: v1
kind: Secret
metadata:
  name: ${CODEX_AUTH_SECRET_NAME}
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: matter-codex-agent-runner
    app.kubernetes.io/component: codex-auth-secret
    matter-codex.dev/openai-account: ${ACCOUNT_NAME}
type: Opaque
data:
  auth.json: ${auth_json_b64}
EOF
}

if [ -n "$AUTH_JSON_PATH" ]; then
  [ -f "$AUTH_JSON_PATH" ] || mattercodex_die "Codex auth.json не найден: $AUTH_JSON_PATH"
  CODEX_AUTH_JSON_B64="$(base64 "$AUTH_JSON_PATH" | tr -d '\n')"
  mattercodex_log "применяется Codex auth secret из локального auth.json"
  mattercodex_apply_codex_auth_secret "$CODEX_AUTH_JSON_B64"
  mattercodex_log "Codex auth secret сохранен"
  exit 0
fi

POD_NAME_Q="$(mattercodex_shell_quote "$POD_NAME")"

mattercodex_log "создается временный pod для Codex device-code авторизации"
mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q delete pod $POD_NAME_Q --ignore-not-found >/dev/null"

cat <<EOF | mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q apply -f - >/dev/null"
apiVersion: v1
kind: Pod
metadata:
  name: ${POD_NAME}
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: matter-codex-agent-runner
    app.kubernetes.io/component: codex-device-auth
spec:
  restartPolicy: Never
  automountServiceAccountToken: false
  containers:
    - name: codex-device-auth
      image: ${MATTERCODEX_AGENT_RUNNER_IMAGE}
      imagePullPolicy: IfNotPresent
      command: ["matter-codex-agent-runner"]
      args: ["codex-auth"]
EOF

mattercodex_log "ждем запуск pod и выводим device-code инструкции Codex"
mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q wait --for=condition=Ready pod/${POD_NAME} --timeout=120s >/dev/null"

mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q logs -f pod/${POD_NAME}" &
LOG_PID=$!
trap 'kill "$LOG_PID" >/dev/null 2>&1 || true' EXIT

deadline=$((SECONDS + TIMEOUT_SECONDS))
while [ "$SECONDS" -lt "$deadline" ]; do
  if mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q exec pod/${POD_NAME} -- matter-codex-agent-runner auth-ready-check" >/dev/null 2>&1; then
    break
  fi
  sleep 5
done

kill "$LOG_PID" >/dev/null 2>&1 || true
trap - EXIT

if ! mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q exec pod/${POD_NAME} -- matter-codex-agent-runner auth-ready-check" >/dev/null 2>&1; then
  mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q delete pod $POD_NAME_Q --ignore-not-found >/dev/null"
  mattercodex_die "Codex device-code авторизация не завершилась за ${TIMEOUT_SECONDS}s"
fi

CODEX_AUTH_JSON_B64="$(mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q exec pod/${POD_NAME} -- matter-codex-agent-runner print-auth-json")"
mattercodex_log "сохраняется Codex auth secret на целевом сервере"
mattercodex_apply_codex_auth_secret "$CODEX_AUTH_JSON_B64"
mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q delete pod $POD_NAME_Q --ignore-not-found >/dev/null"
mattercodex_log "Codex device-code авторизация сохранена"
