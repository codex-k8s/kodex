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
mattercodex_require_commands ssh envsubst base64

if [ -z "$RENDER_DIR" ]; then
  RENDER_DIR="$(mktemp -d)"
fi

"$REPO_ROOT/scripts/k8s/render-bot-service.sh" --env-file "$ENV_FILE" --render-dir "$RENDER_DIR" >/dev/null

APPLY_DRY_RUN_MODE="$DRY_RUN_MODE"
NAMESPACE_Q="$(mattercodex_shell_quote "$MATTERCODEX_NAMESPACE")"
REMOTE_KUBECTL="$(mattercodex_remote_kubectl_command)"

if [ "$DRY_RUN_MODE" = "server" ] && ! mattercodex_ssh "$REMOTE_KUBECTL get namespace $NAMESPACE_Q >/dev/null 2>&1"; then
  mattercodex_log "namespace еще не создан; bot-service manifests проверяются через remote client dry-run"
  APPLY_DRY_RUN_MODE="client"
fi

if [ -n "${MATTERCODEX_MATTERMOST_BOT_TOKEN:-}" ] || [ -n "${MATTERCODEX_MATTERMOST_SLASH_TOKEN:-}" ]; then
  BOT_TOKEN_B64="$(printf '%s' "${MATTERCODEX_MATTERMOST_BOT_TOKEN:-}" | base64 | tr -d '\n')"
  SLASH_TOKEN_B64="$(printf '%s' "${MATTERCODEX_MATTERMOST_SLASH_TOKEN:-}" | base64 | tr -d '\n')"
  mattercodex_log "применяется bot-service secret на целевом сервере"
  cat <<EOF | mattercodex_remote_kubectl_apply_stdin "$APPLY_DRY_RUN_MODE"
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
else
  mattercodex_log "Mattermost bot/slash token не заданы; bot-service secret не создается"
fi

GITHUB_TOKEN_VALUE="${MATTERCODEX_GITHUB_TOKEN:-${GITHUB_PAT:-${GIT_BOT_TOKEN:-}}}"
GITHUB_WEBHOOK_SECRET_VALUE="${MATTERCODEX_GITHUB_WEBHOOK_SECRET:-${GITHUB_WEBHOOK_SECRET:-}}"
if [ -n "$GITHUB_TOKEN_VALUE" ] || [ -n "$GITHUB_WEBHOOK_SECRET_VALUE" ]; then
  GITHUB_TOKEN_B64="$(printf '%s' "$GITHUB_TOKEN_VALUE" | base64 | tr -d '\n')"
  GITHUB_WEBHOOK_SECRET_B64="$(printf '%s' "$GITHUB_WEBHOOK_SECRET_VALUE" | base64 | tr -d '\n')"
  mattercodex_log "применяется GitHub secret на целевом сервере"
  cat <<EOF | mattercodex_remote_kubectl_apply_stdin "$APPLY_DRY_RUN_MODE"
apiVersion: v1
kind: Secret
metadata:
  name: ${MATTERCODEX_GITHUB_SECRET}
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: matter-codex-bot-service
    app.kubernetes.io/component: github-secret
type: Opaque
data:
  github-token: ${GITHUB_TOKEN_B64}
  github-webhook-secret: ${GITHUB_WEBHOOK_SECRET_B64}
EOF
else
  mattercodex_log "GitHub token/webhook secret не заданы; GitHub secret не создается"
fi

CODEX_AUTH_JSON_PATH="${MATTERCODEX_CODEX_AUTH_JSON_PATH:-${CODEX_AUTH_JSON_PATH:-}}"
if [ -n "$CODEX_AUTH_JSON_PATH" ]; then
  [ -f "$CODEX_AUTH_JSON_PATH" ] || mattercodex_die "Codex auth.json не найден: $CODEX_AUTH_JSON_PATH"
  CODEX_AUTH_JSON_B64="$(base64 "$CODEX_AUTH_JSON_PATH" | tr -d '\n')"
  mattercodex_log "применяется Codex auth secret на целевом сервере"
  cat <<EOF | mattercodex_remote_kubectl_apply_stdin "$APPLY_DRY_RUN_MODE"
apiVersion: v1
kind: Secret
metadata:
  name: ${MATTERCODEX_CODEX_AUTH_SECRET}
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: matter-codex-agent-runner
    app.kubernetes.io/component: codex-auth-secret
type: Opaque
data:
  auth.json: ${CODEX_AUTH_JSON_B64}
EOF
else
  mattercodex_log "Codex auth.json path не задан; Codex auth secret не создается"
fi

mattercodex_log "применяются манифесты bot-service на целевом сервере"
cat "$RENDER_DIR/10-code-configmap.yaml" | mattercodex_remote_kubectl_apply_stdin "$APPLY_DRY_RUN_MODE"
cat "$RENDER_DIR/20-configmap.yaml" | mattercodex_remote_kubectl_apply_stdin "$APPLY_DRY_RUN_MODE"
cat "$RENDER_DIR/25-rbac.yaml" | mattercodex_remote_kubectl_apply_stdin "$APPLY_DRY_RUN_MODE"
cat "$RENDER_DIR/30-deployment.yaml" | mattercodex_remote_kubectl_apply_stdin "$APPLY_DRY_RUN_MODE"
cat "$RENDER_DIR/40-service.yaml" | mattercodex_remote_kubectl_apply_stdin "$APPLY_DRY_RUN_MODE"
cat "$RENDER_DIR/50-ingress.yaml" | mattercodex_remote_kubectl_apply_stdin "$APPLY_DRY_RUN_MODE"

if [ "$DRY_RUN_MODE" = "none" ]; then
  mattercodex_log "перезапуск bot-service для применения source ConfigMap на целевом сервере"
  mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q rollout restart deployment/matter-codex-bot-service >/dev/null"
  if mattercodex_bool "$WAIT"; then
    mattercodex_log "ожидание rollout bot-service на целевом сервере"
    mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q rollout status deployment/matter-codex-bot-service --timeout=300s >/dev/null"
  fi
fi

mattercodex_log "remote bot-service install шаг завершен"
