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
RENDER_DIR_CREATED=false

cleanup() {
  if mattercodex_bool "$RENDER_DIR_CREATED"; then
    rm -rf "$RENDER_DIR"
  fi
}

trap cleanup EXIT

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
mattercodex_require_commands kubectl envsubst base64

if [ -z "$RENDER_DIR" ]; then
  RENDER_DIR="$(mktemp -d)"
  RENDER_DIR_CREATED=true
fi

"$SCRIPT_DIR/render-bot-service.sh" --env-file "$ENV_FILE" --render-dir "$RENDER_DIR" >/dev/null

DRY_RUN_ARG="$(mattercodex_kubectl_dry_run_arg "$DRY_RUN_MODE")"

apply_rendered_manifest() {
  local template="$1"
  local output="$2"
  mattercodex_render_template "$template" "$output"
  kubectl apply ${DRY_RUN_ARG:+$DRY_RUN_ARG} -f "$output" >/dev/null
}

if [ "$DRY_RUN_MODE" = "none" ] && mattercodex_bool "${MATTERCODEX_AGENT_RUNNER_BUILD_IMAGE:-true}"; then
  mattercodex_require_commands docker
  mattercodex_log "сборка agent-runner image"
  docker build \
    --network=host \
    --build-arg "MATTERCODEX_CODEX_PACKAGE=$MATTERCODEX_CODEX_PACKAGE" \
    -f "$REPO_ROOT/services/jobs/agent-runner/Dockerfile" \
    -t "$MATTERCODEX_AGENT_RUNNER_IMAGE" \
    "$REPO_ROOT"
fi

if [ "$DRY_RUN_MODE" = "none" ] && mattercodex_bool "${MATTERCODEX_BOT_SERVICE_BUILD_IMAGE:-true}"; then
  mattercodex_require_commands docker
  mattercodex_log "сборка bot-service image"
  docker build \
    --network=host \
    --target prod \
    -f "$REPO_ROOT/services/external/bot-service/Dockerfile" \
    -t "$MATTERCODEX_BOT_SERVICE_IMAGE" \
    "$REPO_ROOT"
fi

if [ -n "${MATTERCODEX_MATTERMOST_BOT_TOKEN:-}" ] || [ -n "${MATTERCODEX_MATTERMOST_SLASH_TOKEN:-}" ]; then
  export BOT_TOKEN_B64
  export SLASH_TOKEN_B64
  export ADMIN_TOKEN_B64
  BOT_TOKEN_B64="$(printf '%s' "${MATTERCODEX_MATTERMOST_BOT_TOKEN:-}" | base64 | tr -d '\n')"
  SLASH_TOKEN_B64="$(printf '%s' "${MATTERCODEX_MATTERMOST_SLASH_TOKEN:-}" | base64 | tr -d '\n')"
  ADMIN_TOKEN_B64="$(printf '%s' "${MATTERCODEX_MATTERMOST_ADMIN_TOKEN:-}" | base64 | tr -d '\n')"
  mattercodex_log "применяется bot-service secret"
  apply_rendered_manifest "$REPO_ROOT/deploy/k8s/bot-service/bot-service-secret.yaml.tpl" "$RENDER_DIR/05-bot-service-secret.yaml"
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
  export GITHUB_TOKEN_B64
  export GITHUB_WEBHOOK_SECRET_B64
  export GITHUB_USERNAME_B64
  export GITHUB_EMAIL_B64
  GITHUB_TOKEN_B64="$(printf '%s' "$GITHUB_TOKEN_VALUE" | base64 | tr -d '\n')"
  GITHUB_WEBHOOK_SECRET_B64="$(printf '%s' "$GITHUB_WEBHOOK_SECRET_VALUE" | base64 | tr -d '\n')"
  GITHUB_USERNAME_B64="$(printf '%s' "$GITHUB_USERNAME_VALUE" | base64 | tr -d '\n')"
  GITHUB_EMAIL_B64="$(printf '%s' "$GITHUB_EMAIL_VALUE" | base64 | tr -d '\n')"
  mattercodex_log "применяется GitHub secret"
  apply_rendered_manifest "$REPO_ROOT/deploy/k8s/bot-service/github-secret.yaml.tpl" "$RENDER_DIR/06-github-secret.yaml"
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
  export AGENT_GITHUB_TOKEN_B64
  export AGENT_GITHUB_USERNAME_B64
  export AGENT_GITHUB_EMAIL_B64
  AGENT_GITHUB_TOKEN_B64="$(printf '%s' "$AGENT_GITHUB_TOKEN_VALUE" | base64 | tr -d '\n')"
  AGENT_GITHUB_USERNAME_B64="$(printf '%s' "$AGENT_GITHUB_USERNAME_VALUE" | base64 | tr -d '\n')"
  AGENT_GITHUB_EMAIL_B64="$(printf '%s' "$AGENT_GITHUB_EMAIL_VALUE" | base64 | tr -d '\n')"
  mattercodex_log "применяется agent GitHub secret"
  apply_rendered_manifest "$REPO_ROOT/deploy/k8s/bot-service/agent-github-secret.yaml.tpl" "$RENDER_DIR/07-agent-github-secret.yaml"
else
  mattercodex_log "agent GitHub token не задан; agent GitHub secret не создается"
fi

CODEX_AUTH_JSON_PATH="${MATTERCODEX_CODEX_AUTH_JSON_PATH:-${CODEX_AUTH_JSON_PATH:-}}"
if [ -n "$CODEX_AUTH_JSON_PATH" ]; then
  [ -f "$CODEX_AUTH_JSON_PATH" ] || mattercodex_die "Codex auth.json не найден: $CODEX_AUTH_JSON_PATH"
  CODEX_AUTH_ACCOUNT="${MATTERCODEX_CODEX_AUTH_ACCOUNT:-primary}"
  CODEX_AUTH_SECRET_NAME="${MATTERCODEX_CODEX_AUTH_SECRET}-${CODEX_AUTH_ACCOUNT}"
  export CODEX_AUTH_ACCOUNT
  export CODEX_AUTH_SECRET_NAME
  export CODEX_AUTH_JSON_B64
  CODEX_AUTH_JSON_B64="$(base64 "$CODEX_AUTH_JSON_PATH" | tr -d '\n')"
  mattercodex_log "применяется Codex auth secret"
  apply_rendered_manifest "$REPO_ROOT/deploy/k8s/bot-service/codex-auth-secret.yaml.tpl" "$RENDER_DIR/08-codex-auth-secret.yaml"
else
  mattercodex_log "Codex auth.json path не задан; Codex auth secret не создается"
fi

mattercodex_log "применяются манифесты bot-service"
for manifest in \
  "$RENDER_DIR/10-configmap.yaml" \
  "$RENDER_DIR/15-runtime-limits.yaml" \
  "$RENDER_DIR/20-rbac.yaml" \
  "$RENDER_DIR/30-deployment.yaml" \
  "$RENDER_DIR/40-service.yaml" \
  "$RENDER_DIR/50-ingress.yaml"; do
  if [ -f "$manifest" ]; then
    kubectl apply ${DRY_RUN_ARG:+$DRY_RUN_ARG} -f "$manifest" >/dev/null
  fi
done

if [ "$DRY_RUN_MODE" = "none" ]; then
  LEGACY_CODE_CONFIGMAP="${MATTERCODEX_BOT_SERVICE_CODE_CONFIGMAP:-matter-codex-bot-service-code}"
  mattercodex_log "удаляется legacy bot-service source ConfigMap, если он остался"
  kubectl -n "$MATTERCODEX_NAMESPACE" delete configmap "$LEGACY_CODE_CONFIGMAP" --ignore-not-found >/dev/null
  if mattercodex_bool "$WAIT"; then
    mattercodex_log "ожидание rollout bot-service"
    kubectl -n "$MATTERCODEX_NAMESPACE" rollout status deployment/matter-codex-bot-service --timeout=300s >/dev/null
  fi
fi

mattercodex_log "bot-service install шаг завершен"
