#!/usr/bin/env bash

set -euo pipefail

mattercodex_log() {
  printf '[matter-codex] %s\n' "$*"
}

mattercodex_die() {
  printf '[matter-codex] ОШИБКА: %s\n' "$*" >&2
  exit 1
}

mattercodex_repo_root() {
  local script_dir
  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
  printf '%s\n' "$script_dir"
}

mattercodex_load_env_file() {
  local env_file="${1:-.env}"
  if [ ! -f "$env_file" ]; then
    mattercodex_die "env-файл не найден: $env_file"
  fi

  set -a
  # shellcheck disable=SC1090
  . "$env_file"
  set +a
}

mattercodex_require_commands() {
  local missing=0
  local cmd
  for cmd in "$@"; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
      mattercodex_log "не найдена команда: $cmd"
      missing=1
    fi
  done
  [ "$missing" -eq 0 ] || mattercodex_die "не найдены обязательные команды"
}

mattercodex_shell_quote() {
  local value="$1"
  printf "'%s'" "$(printf '%s' "$value" | sed "s/'/'\\\\''/g")"
}

mattercodex_ssh() {
  ssh \
    -i "$TARGET_ROOT_SSH_KEY" \
    -p "$TARGET_PORT" \
    -o BatchMode=yes \
    -o StrictHostKeyChecking=accept-new \
    "$TARGET_ROOT_USER@$TARGET_HOST" \
    "$@"
}

mattercodex_remote_kubectl_command() {
  if [ -n "${MATTERCODEX_REMOTE_KUBECTL:-}" ]; then
    printf '%s\n' "$MATTERCODEX_REMOTE_KUBECTL"
    return
  fi

  mattercodex_ssh 'set -eu
    if kubectl get --raw=/readyz >/dev/null 2>&1; then
      printf "kubectl\n"
    elif command -v sudo >/dev/null 2>&1 && sudo -n k3s kubectl get --raw=/readyz >/dev/null 2>&1; then
      printf "sudo -n k3s kubectl\n"
    elif command -v sudo >/dev/null 2>&1 && sudo -n kubectl get --raw=/readyz >/dev/null 2>&1; then
      printf "sudo -n kubectl\n"
    else
      printf "не удалось подобрать remote kubectl с доступом к Kubernetes API\n" >&2
      exit 1
    fi' </dev/null
}

mattercodex_remote_kubectl_apply_stdin() {
  local dry_run_mode="$1"
  local dry_run_arg
  local remote_kubectl
  dry_run_arg="$(mattercodex_kubectl_dry_run_arg "$dry_run_mode")"
  remote_kubectl="$(mattercodex_remote_kubectl_command)"
  mattercodex_ssh "$remote_kubectl apply ${dry_run_arg:+$dry_run_arg }-f -" >/dev/null
}

mattercodex_require_env() {
  local missing=0
  local name
  for name in "$@"; do
    if [ -z "${!name:-}" ]; then
      mattercodex_log "не задан env: $name"
      missing=1
    fi
  done
  [ "$missing" -eq 0 ] || mattercodex_die "не заданы обязательные env-ключи"
}

mattercodex_bool() {
  case "${1:-}" in
    1|true|TRUE|yes|YES|on|ON) return 0 ;;
    *) return 1 ;;
  esac
}

mattercodex_host_from_url() {
  local url="$1"
  printf '%s\n' "$url" | sed -E 's#^[A-Za-z][A-Za-z0-9+.-]*://##; s#/.*$##; s#:.*$##'
}

mattercodex_parent_domain() {
  local host="$1"
  if [ "$host" != "${host#*.}" ]; then
    printf '%s\n' "${host#*.}"
    return
  fi
  printf '%s\n' "$host"
}

mattercodex_set_defaults() {
  export MATTERCODEX_NAMESPACE="${MATTERCODEX_NAMESPACE:-${PRODUCTION_NAMESPACE:-mattermost}}"
  export MATTERCODEX_MATTERMOST_SITE_URL="${MATTERCODEX_MATTERMOST_SITE_URL:-${PUBLIC_BASE_URL:-}}"
  if [ -z "${MATTERCODEX_MATTERMOST_HOST:-}" ] && [ -n "${MATTERCODEX_MATTERMOST_SITE_URL:-}" ]; then
    MATTERCODEX_MATTERMOST_HOST="$(mattercodex_host_from_url "$MATTERCODEX_MATTERMOST_SITE_URL")"
    export MATTERCODEX_MATTERMOST_HOST
  fi

  export MATTERCODEX_MATTERMOST_IMAGE="${MATTERCODEX_MATTERMOST_IMAGE:-mattermost/mattermost-team-edition:11.6}"
  export MATTERCODEX_POSTGRES_IMAGE="${MATTERCODEX_POSTGRES_IMAGE:-postgres:16-alpine}"
  export MATTERCODEX_BUSYBOX_IMAGE="${MATTERCODEX_BUSYBOX_IMAGE:-busybox:1.36}"
  export MATTERCODEX_POSTGRES_DB="${MATTERCODEX_POSTGRES_DB:-mattermost}"
  export MATTERCODEX_POSTGRES_USER="${MATTERCODEX_POSTGRES_USER:-mattermost}"
  export MATTERCODEX_POSTGRES_SECRET="${MATTERCODEX_POSTGRES_SECRET:-mattermost-postgres}"
  export MATTERCODEX_POSTGRES_STORAGE_SIZE="${MATTERCODEX_POSTGRES_STORAGE_SIZE:-10Gi}"
  export MATTERCODEX_MATTERMOST_STORAGE_SIZE="${MATTERCODEX_MATTERMOST_STORAGE_SIZE:-20Gi}"
  export MATTERCODEX_INGRESS_CLASS="${MATTERCODEX_INGRESS_CLASS:-kodex-public}"
  export MATTERCODEX_TLS_SECRET="${MATTERCODEX_TLS_SECRET:-mattermost-tls}"
  export MATTERCODEX_CLUSTER_ISSUER="${MATTERCODEX_CLUSTER_ISSUER:-letsencrypt-prod}"
  export MATTERCODEX_ACME_SERVER="${MATTERCODEX_ACME_SERVER:-https://acme-v02.api.letsencrypt.org/directory}"

  export MATTERCODEX_BOT_SERVICE_IMAGE="${MATTERCODEX_BOT_SERVICE_IMAGE:-golang:1.26-alpine}"
  export MATTERCODEX_BOT_SERVICE_PORT="${MATTERCODEX_BOT_SERVICE_PORT:-8080}"
  export MATTERCODEX_BOT_SERVICE_HTTP_ADDR="${MATTERCODEX_BOT_SERVICE_HTTP_ADDR:-:${MATTERCODEX_BOT_SERVICE_PORT}}"
  export MATTERCODEX_BOT_SERVICE_SECRET="${MATTERCODEX_BOT_SERVICE_SECRET:-matter-codex-bot-service}"
  export MATTERCODEX_GITHUB_SECRET="${MATTERCODEX_GITHUB_SECRET:-matter-codex-github}"
  export MATTERCODEX_BOT_SERVICE_CODE_CONFIGMAP="${MATTERCODEX_BOT_SERVICE_CODE_CONFIGMAP:-matter-codex-bot-service-code}"
  export MATTERCODEX_BOT_SERVICE_CONFIG_CONFIGMAP="${MATTERCODEX_BOT_SERVICE_CONFIG_CONFIGMAP:-matter-codex-bot-service-config}"
  export MATTERCODEX_BOT_SERVICE_TLS_SECRET="${MATTERCODEX_BOT_SERVICE_TLS_SECRET:-matter-codex-bot-service-tls}"
  if [ -z "${MATTERCODEX_BOT_SERVICE_HOST:-}" ]; then
    local bot_root_domain
    bot_root_domain="$(mattercodex_parent_domain "${MATTERCODEX_MATTERMOST_HOST:-${PRODUCTION_DOMAIN:-mattermost.local}}")"
    MATTERCODEX_BOT_SERVICE_HOST="matter-codex.${bot_root_domain}"
    export MATTERCODEX_BOT_SERVICE_HOST
  fi
  export MATTERCODEX_BOT_SERVICE_SITE_URL="${MATTERCODEX_BOT_SERVICE_SITE_URL:-https://${MATTERCODEX_BOT_SERVICE_HOST}}"
  export MATTERCODEX_BOT_SERVICE_INTERNAL_URL="${MATTERCODEX_BOT_SERVICE_INTERNAL_URL:-http://matter-codex-bot-service.${MATTERCODEX_NAMESPACE}.svc.cluster.local:${MATTERCODEX_BOT_SERVICE_PORT}}"
  export MATTERCODEX_STORAGE_MIGRATIONS_ENABLED="${MATTERCODEX_STORAGE_MIGRATIONS_ENABLED:-true}"
  export MATTERCODEX_BOT_SERVICE_MAX_GITHUB_WEBHOOK_BYTES="${MATTERCODEX_BOT_SERVICE_MAX_GITHUB_WEBHOOK_BYTES:-262144}"
  export MATTERCODEX_MATTERMOST_ALLOWED_UNTRUSTED_INTERNAL_CONNECTIONS="${MATTERCODEX_MATTERMOST_ALLOWED_UNTRUSTED_INTERNAL_CONNECTIONS:-$(mattercodex_host_from_url "$MATTERCODEX_BOT_SERVICE_INTERNAL_URL")}"
  export MATTERCODEX_MATTERMOST_BOT_USERNAME="${MATTERCODEX_MATTERMOST_BOT_USERNAME:-matter-codex}"
  export MATTERCODEX_MATTERMOST_BOT_EMAIL="${MATTERCODEX_MATTERMOST_BOT_EMAIL:-matter-codex@local.invalid}"
  export MATTERCODEX_DEFAULT_TEAM_NAME="${MATTERCODEX_DEFAULT_TEAM_NAME:-agents}"
  export MATTERCODEX_DEFAULT_TEAM_DISPLAY_NAME="${MATTERCODEX_DEFAULT_TEAM_DISPLAY_NAME:-Agents}"
  export MATTERCODEX_DEFAULT_CHANNELS="${MATTERCODEX_DEFAULT_CHANNELS:-agents-control:Agents Control,agents-runs:Agents Runs,agent-alerts:Agent Alerts,agents-audit:Agents Audit}"
}

mattercodex_validate_base_env() {
  mattercodex_require_env \
    TARGET_HOST \
    TARGET_PORT \
    TARGET_ROOT_USER \
    TARGET_ROOT_SSH_KEY \
    OPERATOR_USER \
    OPERATOR_SSH_PUBKEY_PATH \
    PRODUCTION_NAMESPACE \
    PRODUCTION_DOMAIN \
    PUBLIC_BASE_URL \
    LETSENCRYPT_EMAIL

  mattercodex_set_defaults

  mattercodex_require_env \
    MATTERCODEX_NAMESPACE \
    MATTERCODEX_MATTERMOST_SITE_URL \
    MATTERCODEX_MATTERMOST_HOST
}

mattercodex_render_template() {
  local template="$1"
  local output="$2"
  if [ "$output" = "-" ]; then
    envsubst < "$template"
    return
  fi
  mkdir -p "$(dirname "$output")"
  envsubst < "$template" > "$output"
}

mattercodex_kubectl_dry_run_arg() {
  local mode="${1:-server}"
  case "$mode" in
    none) printf '%s\n' '' ;;
    server) printf '%s\n' '--dry-run=server' ;;
    client) printf '%s\n' '--dry-run=client' ;;
    *) mattercodex_die "неподдерживаемый dry-run режим: $mode" ;;
  esac
}

mattercodex_generate_password() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -base64 32 | tr -d '\n'
    return
  fi
  mattercodex_die "openssl нужен для генерации PostgreSQL password, если MATTERCODEX_POSTGRES_PASSWORD не задан"
}
