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
mattercodex_require_commands ssh envsubst base64 tar

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

if [ "$DRY_RUN_MODE" = "none" ] && mattercodex_bool "${MATTERCODEX_AGENT_RUNNER_BUILD_IMAGE:-true}"; then
  mattercodex_log "сборка agent-runner image на целевом сервере"
  AGENT_RUNNER_ARCHIVE="$(mktemp)"
  trap 'rm -f "${AGENT_RUNNER_ARCHIVE:-}"' EXIT
  tar -C "$REPO_ROOT" -czf "$AGENT_RUNNER_ARCHIVE" \
    services/jobs/agent-runner
  REMOTE_AGENT_RUNNER_DIR="/tmp/matter-codex-agent-runner-build"
  REMOTE_AGENT_RUNNER_DIR_Q="$(mattercodex_shell_quote "$REMOTE_AGENT_RUNNER_DIR")"
  AGENT_RUNNER_IMAGE_Q="$(mattercodex_shell_quote "$MATTERCODEX_AGENT_RUNNER_IMAGE")"
  CODEX_PACKAGE_Q="$(mattercodex_shell_quote "$MATTERCODEX_CODEX_PACKAGE")"
  REMOTE_AGENT_RUNNER_BUILDER="$(mattercodex_ssh 'set -eu
    if command -v docker >/dev/null 2>&1; then
      printf "docker\n"
    elif command -v nerdctl >/dev/null 2>&1; then
      printf "nerdctl\n"
    else
      printf "none\n"
    fi' </dev/null)"
  REMOTE_AGENT_RUNNER_IMPORT="$(mattercodex_ssh 'set -eu
    if command -v sudo >/dev/null 2>&1 && sudo -n k3s ctr images ls >/dev/null 2>&1; then
      printf "sudo -n k3s ctr images import -\n"
    elif command -v sudo >/dev/null 2>&1 && sudo -n ctr -n k8s.io images ls >/dev/null 2>&1; then
      printf "sudo -n ctr -n k8s.io images import -\n"
    else
      printf "none\n"
    fi' </dev/null)"
  if [ "$REMOTE_AGENT_RUNNER_BUILDER" = "docker" ] || [ "$REMOTE_AGENT_RUNNER_BUILDER" = "nerdctl" ]; then
    mattercodex_ssh "rm -rf $REMOTE_AGENT_RUNNER_DIR_Q && mkdir -p $REMOTE_AGENT_RUNNER_DIR_Q && tar -xzf - -C $REMOTE_AGENT_RUNNER_DIR_Q" < "$AGENT_RUNNER_ARCHIVE"
    mattercodex_ssh "set -eu
      cd $REMOTE_AGENT_RUNNER_DIR_Q
      image=$AGENT_RUNNER_IMAGE_Q
      codex_package=$CODEX_PACKAGE_Q
      if [ '$REMOTE_AGENT_RUNNER_BUILDER' = 'docker' ]; then
        docker build --build-arg MATTERCODEX_CODEX_PACKAGE=\"\$codex_package\" -f services/jobs/agent-runner/Dockerfile -t \"\$image\" .
        if [ '$REMOTE_AGENT_RUNNER_IMPORT' != 'none' ]; then
          docker save \"\$image\" | $REMOTE_AGENT_RUNNER_IMPORT
        fi
      else
        nerdctl -n k8s.io build --build-arg MATTERCODEX_CODEX_PACKAGE=\"\$codex_package\" -f services/jobs/agent-runner/Dockerfile -t \"\$image\" .
      fi" </dev/null
  elif [ "$REMOTE_AGENT_RUNNER_IMPORT" != "none" ] && command -v docker >/dev/null 2>&1; then
    mattercodex_log "на сервере нет docker/nerdctl; сборка agent-runner image локально и импорт в Kubernetes runtime"
    docker build \
      --network=host \
      --build-arg "MATTERCODEX_CODEX_PACKAGE=$MATTERCODEX_CODEX_PACKAGE" \
      -f "$REPO_ROOT/services/jobs/agent-runner/Dockerfile" \
      -t "$MATTERCODEX_AGENT_RUNNER_IMAGE" \
      "$REPO_ROOT" >/dev/null
    docker save "$MATTERCODEX_AGENT_RUNNER_IMAGE" | mattercodex_ssh "$REMOTE_AGENT_RUNNER_IMPORT"
  else
    mattercodex_die "не найден способ собрать или импортировать agent-runner image: нужен docker/nerdctl на сервере либо локальный docker и remote k3s/ctr import"
  fi
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
GITHUB_USERNAME_VALUE="${MATTERCODEX_GITHUB_USERNAME:-${GITHUB_USERNAME:-${GITHUB_USER:-}}}"
GITHUB_EMAIL_VALUE="${MATTERCODEX_GITHUB_EMAIL:-${GITHUB_EMAIL:-}}"
if [ -z "$GITHUB_EMAIL_VALUE" ] && [ -n "$GITHUB_USERNAME_VALUE" ]; then
  GITHUB_EMAIL_VALUE="${GITHUB_USERNAME_VALUE}@users.noreply.github.com"
fi
if [ -n "$GITHUB_TOKEN_VALUE" ] || [ -n "$GITHUB_WEBHOOK_SECRET_VALUE" ]; then
  if [ -n "$GITHUB_TOKEN_VALUE" ]; then
    [ -n "$GITHUB_USERNAME_VALUE" ] || mattercodex_die "GitHub username не задан: укажи MATTERCODEX_GITHUB_USERNAME или GITHUB_USERNAME/GITHUB_USER"
    [ -n "$GITHUB_EMAIL_VALUE" ] || mattercodex_die "GitHub email не задан: укажи MATTERCODEX_GITHUB_EMAIL или GITHUB_EMAIL"
  fi
  GITHUB_TOKEN_B64="$(printf '%s' "$GITHUB_TOKEN_VALUE" | base64 | tr -d '\n')"
  GITHUB_WEBHOOK_SECRET_B64="$(printf '%s' "$GITHUB_WEBHOOK_SECRET_VALUE" | base64 | tr -d '\n')"
  GITHUB_USERNAME_B64="$(printf '%s' "$GITHUB_USERNAME_VALUE" | base64 | tr -d '\n')"
  GITHUB_EMAIL_B64="$(printf '%s' "$GITHUB_EMAIL_VALUE" | base64 | tr -d '\n')"
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
  github-username: ${GITHUB_USERNAME_B64}
  github-email: ${GITHUB_EMAIL_B64}
EOF
else
  mattercodex_log "GitHub token/webhook secret не заданы; GitHub secret не создается"
fi

AGENT_GITHUB_TOKEN_VALUE="${MATTERCODEX_AGENT_GITHUB_TOKEN:-${GIT_BOT_TOKEN:-}}"
AGENT_GITHUB_USERNAME_VALUE="${MATTERCODEX_AGENT_GITHUB_USERNAME:-${GIT_BOT_USERNAME:-}}"
AGENT_GITHUB_EMAIL_VALUE="${MATTERCODEX_AGENT_GITHUB_EMAIL:-${GIT_BOT_MAIL:-${GIT_BOT_EMAIL:-}}}"
if [ -z "$AGENT_GITHUB_EMAIL_VALUE" ] && [ -n "$AGENT_GITHUB_USERNAME_VALUE" ]; then
  AGENT_GITHUB_EMAIL_VALUE="${AGENT_GITHUB_USERNAME_VALUE}@users.noreply.github.com"
fi
if [ -n "$AGENT_GITHUB_TOKEN_VALUE" ]; then
  [ -n "$AGENT_GITHUB_USERNAME_VALUE" ] || mattercodex_die "agent GitHub username не задан: укажи MATTERCODEX_AGENT_GITHUB_USERNAME или GIT_BOT_USERNAME"
  [ -n "$AGENT_GITHUB_EMAIL_VALUE" ] || mattercodex_die "agent GitHub email не задан: укажи MATTERCODEX_AGENT_GITHUB_EMAIL или GIT_BOT_MAIL/GIT_BOT_EMAIL"
  AGENT_GITHUB_TOKEN_B64="$(printf '%s' "$AGENT_GITHUB_TOKEN_VALUE" | base64 | tr -d '\n')"
  AGENT_GITHUB_USERNAME_B64="$(printf '%s' "$AGENT_GITHUB_USERNAME_VALUE" | base64 | tr -d '\n')"
  AGENT_GITHUB_EMAIL_B64="$(printf '%s' "$AGENT_GITHUB_EMAIL_VALUE" | base64 | tr -d '\n')"
  mattercodex_log "применяется agent GitHub secret на целевом сервере"
  cat <<EOF | mattercodex_remote_kubectl_apply_stdin "$APPLY_DRY_RUN_MODE"
apiVersion: v1
kind: Secret
metadata:
  name: ${MATTERCODEX_AGENT_GITHUB_SECRET}
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: matter-codex-agent-runner
    app.kubernetes.io/component: github-agent-secret
type: Opaque
data:
  github-token: ${AGENT_GITHUB_TOKEN_B64}
  github-username: ${AGENT_GITHUB_USERNAME_B64}
  github-email: ${AGENT_GITHUB_EMAIL_B64}
EOF
else
  mattercodex_log "agent GitHub token не задан; agent GitHub secret не создается"
fi

CODEX_AUTH_JSON_PATH="${MATTERCODEX_CODEX_AUTH_JSON_PATH:-${CODEX_AUTH_JSON_PATH:-}}"
if [ -n "$CODEX_AUTH_JSON_PATH" ]; then
  [ -f "$CODEX_AUTH_JSON_PATH" ] || mattercodex_die "Codex auth.json не найден: $CODEX_AUTH_JSON_PATH"
  CODEX_AUTH_ACCOUNT="${MATTERCODEX_CODEX_AUTH_ACCOUNT:-primary}"
  CODEX_AUTH_SECRET_NAME="${MATTERCODEX_CODEX_AUTH_SECRET}-${CODEX_AUTH_ACCOUNT}"
  CODEX_AUTH_JSON_B64="$(base64 "$CODEX_AUTH_JSON_PATH" | tr -d '\n')"
  mattercodex_log "применяется Codex auth secret на целевом сервере"
  cat <<EOF | mattercodex_remote_kubectl_apply_stdin "$APPLY_DRY_RUN_MODE"
apiVersion: v1
kind: Secret
metadata:
  name: ${CODEX_AUTH_SECRET_NAME}
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: matter-codex-agent-runner
    app.kubernetes.io/component: codex-auth-secret
    matter-codex.dev/openai-account: ${CODEX_AUTH_ACCOUNT}
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
