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
  export MATTERCODEX_MATTERMOST_INTERNAL_URL="${MATTERCODEX_MATTERMOST_INTERNAL_URL:-http://mattermost.${MATTERCODEX_NAMESPACE:-${PRODUCTION_NAMESPACE:-mattermost}}.svc.cluster.local:8065}"

  export MATTERCODEX_MATTERMOST_IMAGE="${MATTERCODEX_MATTERMOST_IMAGE:-mattermost/mattermost-team-edition:11.6}"
  export MATTERCODEX_POSTGRES_IMAGE="${MATTERCODEX_POSTGRES_IMAGE:-pgvector/pgvector:0.8.5-pg16@sha256:1d533553fefe4f12e5d80c7b80622ba0c382abb5758856f52983d8789179f0fb}"
  export MATTERCODEX_BUSYBOX_IMAGE="${MATTERCODEX_BUSYBOX_IMAGE:-busybox:1.36}"
  export MATTERCODEX_POSTGRES_DB="${MATTERCODEX_POSTGRES_DB:-mattermost}"
  export MATTERCODEX_POSTGRES_USER="${MATTERCODEX_POSTGRES_USER:-mattermost}"
  export MATTERCODEX_POSTGRES_RUNTIME_USER="${MATTERCODEX_POSTGRES_RUNTIME_USER:-mattercodex_runtime}"
  export MATTERCODEX_POSTGRES_SECRET="${MATTERCODEX_POSTGRES_SECRET:-mattermost-postgres}"
  export MATTERCODEX_POSTGRES_STORAGE_SIZE="${MATTERCODEX_POSTGRES_STORAGE_SIZE:-10Gi}"
  export MATTERCODEX_MATTERMOST_STORAGE_SIZE="${MATTERCODEX_MATTERMOST_STORAGE_SIZE:-20Gi}"
  export MATTERCODEX_MATTERMOST_OAUTH2_PROXY_ENABLED="${MATTERCODEX_MATTERMOST_OAUTH2_PROXY_ENABLED:-true}"
  export MATTERCODEX_MATTERMOST_OAUTH2_PROXY_IMAGE="${MATTERCODEX_MATTERMOST_OAUTH2_PROXY_IMAGE:-quay.io/oauth2-proxy/oauth2-proxy:v7.15.0}"
  export MATTERCODEX_MATTERMOST_OAUTH2_PROXY_SECRET="${MATTERCODEX_MATTERMOST_OAUTH2_PROXY_SECRET:-mattermost-oauth2-proxy}"
  export MATTERCODEX_MATTERMOST_OAUTH2_PROXY_CLIENT_ID="${MATTERCODEX_MATTERMOST_OAUTH2_PROXY_CLIENT_ID:-${OAUTH_CLIENT_ID:-}}"
  export MATTERCODEX_MATTERMOST_OAUTH2_PROXY_CLIENT_SECRET="${MATTERCODEX_MATTERMOST_OAUTH2_PROXY_CLIENT_SECRET:-${OAUTH_CLIENT_SECRET:-}}"
  export MATTERCODEX_MATTERMOST_OAUTH2_PROXY_COOKIE_SECRET="${MATTERCODEX_MATTERMOST_OAUTH2_PROXY_COOKIE_SECRET:-${KODEX_OAUTH2_PROXY_COOKIE_SECRET:-}}"
  export MATTERCODEX_MATTERMOST_OAUTH2_PROXY_AUTHENTICATED_EMAILS="${MATTERCODEX_MATTERMOST_OAUTH2_PROXY_AUTHENTICATED_EMAILS:-lepehovsv@gmail.com}"
  export MATTERCODEX_MATTERMOST_OAUTH2_PROXY_REPLICAS="${MATTERCODEX_MATTERMOST_OAUTH2_PROXY_REPLICAS:-1}"
  export MATTERCODEX_MATTERMOST_OAUTH2_PROXY_CPU_REQUEST="${MATTERCODEX_MATTERMOST_OAUTH2_PROXY_CPU_REQUEST:-25m}"
  export MATTERCODEX_MATTERMOST_OAUTH2_PROXY_MEMORY_REQUEST="${MATTERCODEX_MATTERMOST_OAUTH2_PROXY_MEMORY_REQUEST:-64Mi}"
  export MATTERCODEX_MATTERMOST_OAUTH2_PROXY_CPU_LIMIT="${MATTERCODEX_MATTERMOST_OAUTH2_PROXY_CPU_LIMIT:-250m}"
  export MATTERCODEX_MATTERMOST_OAUTH2_PROXY_MEMORY_LIMIT="${MATTERCODEX_MATTERMOST_OAUTH2_PROXY_MEMORY_LIMIT:-128Mi}"
  if mattercodex_bool "$MATTERCODEX_MATTERMOST_OAUTH2_PROXY_ENABLED"; then
    export MATTERCODEX_MATTERMOST_INGRESS_SERVICE_NAME="${MATTERCODEX_MATTERMOST_INGRESS_SERVICE_NAME:-mattermost-oauth2-proxy}"
    export MATTERCODEX_MATTERMOST_INGRESS_SERVICE_PORT_NAME="${MATTERCODEX_MATTERMOST_INGRESS_SERVICE_PORT_NAME:-http}"
  else
    export MATTERCODEX_MATTERMOST_INGRESS_SERVICE_NAME="${MATTERCODEX_MATTERMOST_INGRESS_SERVICE_NAME:-mattermost}"
    export MATTERCODEX_MATTERMOST_INGRESS_SERVICE_PORT_NAME="${MATTERCODEX_MATTERMOST_INGRESS_SERVICE_PORT_NAME:-http}"
  fi
  export MATTERCODEX_INGRESS_CLASS="${MATTERCODEX_INGRESS_CLASS:-kodex-public}"
  export MATTERCODEX_TLS_SECRET="${MATTERCODEX_TLS_SECRET:-mattermost-tls}"
  export MATTERCODEX_CLUSTER_ISSUER="${MATTERCODEX_CLUSTER_ISSUER:-letsencrypt-prod}"
  export MATTERCODEX_ACME_SERVER="${MATTERCODEX_ACME_SERVER:-https://acme-v02.api.letsencrypt.org/directory}"

  export MATTERCODEX_IMAGE_BUILD_STRATEGY="${MATTERCODEX_IMAGE_BUILD_STRATEGY:-kaniko}"
  export MATTERCODEX_IMAGE_TAG="${MATTERCODEX_IMAGE_TAG:-dev}"
  export MATTERCODEX_IMAGE_REPOSITORY_PREFIX="${MATTERCODEX_IMAGE_REPOSITORY_PREFIX:-matter-codex}"
  export MATTERCODEX_IMAGE_REGISTRY_MANAGED="${MATTERCODEX_IMAGE_REGISTRY_MANAGED:-true}"
  export MATTERCODEX_IMAGE_REGISTRY_NAME="${MATTERCODEX_IMAGE_REGISTRY_NAME:-matter-codex-registry}"
  export MATTERCODEX_IMAGE_REGISTRY_IMAGE="${MATTERCODEX_IMAGE_REGISTRY_IMAGE:-registry:2}"
  export MATTERCODEX_IMAGE_REGISTRY_STORAGE_SIZE="${MATTERCODEX_IMAGE_REGISTRY_STORAGE_SIZE:-30Gi}"
  export MATTERCODEX_IMAGE_REGISTRY_STORAGE_CLASS="${MATTERCODEX_IMAGE_REGISTRY_STORAGE_CLASS:-}"
  export MATTERCODEX_IMAGE_REGISTRY_HOST_PORT="${MATTERCODEX_IMAGE_REGISTRY_HOST_PORT:-5001}"
  export MATTERCODEX_IMAGE_REGISTRY_PULL_HOST="${MATTERCODEX_IMAGE_REGISTRY_PULL_HOST:-localhost:${MATTERCODEX_IMAGE_REGISTRY_HOST_PORT}}"
  export MATTERCODEX_IMAGE_REGISTRY_PUSH_HOST="${MATTERCODEX_IMAGE_REGISTRY_PUSH_HOST:-${MATTERCODEX_IMAGE_REGISTRY_NAME}.${MATTERCODEX_NAMESPACE}.svc.cluster.local:5000}"
  export MATTERCODEX_KANIKO_IMAGE="${MATTERCODEX_KANIKO_IMAGE:-gcr.io/kaniko-project/executor:v1.24.0}"
  export MATTERCODEX_KANIKO_CONTEXT_PVC="${MATTERCODEX_KANIKO_CONTEXT_PVC:-matter-codex-kaniko-context}"
  export MATTERCODEX_KANIKO_CONTEXT_STORAGE_SIZE="${MATTERCODEX_KANIKO_CONTEXT_STORAGE_SIZE:-20Gi}"
  export MATTERCODEX_KANIKO_CONTEXT_STORAGE_CLASS="${MATTERCODEX_KANIKO_CONTEXT_STORAGE_CLASS:-}"
  export MATTERCODEX_KANIKO_CPU_REQUEST="${MATTERCODEX_KANIKO_CPU_REQUEST:-2000m}"
  export MATTERCODEX_KANIKO_MEMORY_REQUEST="${MATTERCODEX_KANIKO_MEMORY_REQUEST:-2Gi}"
  export MATTERCODEX_KANIKO_MEMORY_LIMIT="${MATTERCODEX_KANIKO_MEMORY_LIMIT:-24Gi}"
  export MATTERCODEX_KANIKO_JOB_TTL_SECONDS="${MATTERCODEX_KANIKO_JOB_TTL_SECONDS:-120}"
  export MATTERCODEX_KANIKO_ACTIVE_DEADLINE_SECONDS="${MATTERCODEX_KANIKO_ACTIVE_DEADLINE_SECONDS:-7200}"
  export MATTERCODEX_KANIKO_EXTRA_ARGS_YAML="${MATTERCODEX_KANIKO_EXTRA_ARGS_YAML:-}"
  export MATTERCODEX_BOT_SERVICE_IMAGE="${MATTERCODEX_BOT_SERVICE_IMAGE:-${MATTERCODEX_IMAGE_REGISTRY_PULL_HOST}/${MATTERCODEX_IMAGE_REPOSITORY_PREFIX}/bot-service:${MATTERCODEX_IMAGE_TAG}}"
  export MATTERCODEX_BOT_SERVICE_IMAGE_PULL_POLICY="${MATTERCODEX_BOT_SERVICE_IMAGE_PULL_POLICY:-Always}"
  export MATTERCODEX_BOT_SERVICE_BUILD_IMAGE="${MATTERCODEX_BOT_SERVICE_BUILD_IMAGE:-true}"
  export MATTERCODEX_BOT_SERVICE_PORT="${MATTERCODEX_BOT_SERVICE_PORT:-8080}"
  export MATTERCODEX_BOT_SERVICE_HTTP_ADDR="${MATTERCODEX_BOT_SERVICE_HTTP_ADDR:-:${MATTERCODEX_BOT_SERVICE_PORT}}"
  export MATTERCODEX_BOT_SERVICE_CPU_REQUEST="${MATTERCODEX_BOT_SERVICE_CPU_REQUEST:-100m}"
  export MATTERCODEX_BOT_SERVICE_MEMORY_REQUEST="${MATTERCODEX_BOT_SERVICE_MEMORY_REQUEST:-512Mi}"
  export MATTERCODEX_BOT_SERVICE_MEMORY_LIMIT="${MATTERCODEX_BOT_SERVICE_MEMORY_LIMIT:-8Gi}"
  export MATTERCODEX_BOT_SERVICE_SECRET="${MATTERCODEX_BOT_SERVICE_SECRET:-matter-codex-bot-service}"
  export MATTERCODEX_GITHUB_SECRET="${MATTERCODEX_GITHUB_SECRET:-matter-codex-github}"
  export MATTERCODEX_AGENT_GITHUB_SECRET="${MATTERCODEX_AGENT_GITHUB_SECRET:-matter-codex-github-agent}"
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
  export MATTERCODEX_LOCALE="${MATTERCODEX_LOCALE:-ru}"
  export MATTERCODEX_OWNER_MATTERMOST_USERNAME="${MATTERCODEX_OWNER_MATTERMOST_USERNAME:-}"
  export MATTERCODEX_RUNTIME_ENABLED="${MATTERCODEX_RUNTIME_ENABLED:-true}"
  export MATTERCODEX_RUNTIME_NAMESPACE="${MATTERCODEX_RUNTIME_NAMESPACE:-${MATTERCODEX_NAMESPACE}}"
  export MATTERCODEX_RUNTIME_SMOKE_IMAGE="${MATTERCODEX_RUNTIME_SMOKE_IMAGE:-${MATTERCODEX_BUSYBOX_IMAGE}}"
  export MATTERCODEX_AGENT_RUNNER_IMAGE="${MATTERCODEX_AGENT_RUNNER_IMAGE:-${MATTERCODEX_IMAGE_REGISTRY_PULL_HOST}/${MATTERCODEX_IMAGE_REPOSITORY_PREFIX}/agent-runner:${MATTERCODEX_IMAGE_TAG}}"
  export MATTERCODEX_AGENT_RUNNER_BUILD_IMAGE="${MATTERCODEX_AGENT_RUNNER_BUILD_IMAGE:-true}"
  export MATTERCODEX_CODEX_PACKAGE="${MATTERCODEX_CODEX_PACKAGE:-@openai/codex@0.144.1}"
  export MATTERCODEX_RUNTIME_WORKSPACE_STORAGE_SIZE="${MATTERCODEX_RUNTIME_WORKSPACE_STORAGE_SIZE:-1Gi}"
  export MATTERCODEX_AGENT_SESSION_CPU_REQUEST="${MATTERCODEX_AGENT_SESSION_CPU_REQUEST:-500m}"
  export MATTERCODEX_AGENT_SESSION_MEMORY_REQUEST="${MATTERCODEX_AGENT_SESSION_MEMORY_REQUEST:-8Gi}"
  export MATTERCODEX_AGENT_SESSION_MEMORY_LIMIT="${MATTERCODEX_AGENT_SESSION_MEMORY_LIMIT:-8Gi}"
  export MATTERCODEX_AGENT_UTILITY_MEMORY_LIMIT="${MATTERCODEX_AGENT_UTILITY_MEMORY_LIMIT:-4Gi}"
  export MATTERCODEX_AGENT_DEV_SHM_SIZE_LIMIT="${MATTERCODEX_AGENT_DEV_SHM_SIZE_LIMIT:-2Gi}"
  export MATTERCODEX_AGENT_WORKLOAD_PRIORITY_CLASS="${MATTERCODEX_AGENT_WORKLOAD_PRIORITY_CLASS:-matter-codex-agent-workload}"
  export MATTERCODEX_RUNTIME_JOB_TTL_SECONDS="${MATTERCODEX_RUNTIME_JOB_TTL_SECONDS:-86400}"
  export MATTERCODEX_RUNTIME_RETENTION_ENABLED="${MATTERCODEX_RUNTIME_RETENTION_ENABLED:-true}"
  export MATTERCODEX_RUNTIME_RETENTION_INTERVAL="${MATTERCODEX_RUNTIME_RETENTION_INTERVAL:-30m}"
  export MATTERCODEX_RUNTIME_RETENTION_OLDER_THAN="${MATTERCODEX_RUNTIME_RETENTION_OLDER_THAN:-24h}"
  export MATTERCODEX_INTERACTION_CAPABILITY_CLEANUP_ENABLED="${MATTERCODEX_INTERACTION_CAPABILITY_CLEANUP_ENABLED:-true}"
  export MATTERCODEX_INTERACTION_CAPABILITY_CLEANUP_INTERVAL="${MATTERCODEX_INTERACTION_CAPABILITY_CLEANUP_INTERVAL:-30m}"
  export MATTERCODEX_INTERACTION_CAPABILITY_RETENTION="${MATTERCODEX_INTERACTION_CAPABILITY_RETENTION:-168h}"
  export MATTERCODEX_INTERACTION_CAPABILITY_CLEANUP_BATCH="${MATTERCODEX_INTERACTION_CAPABILITY_CLEANUP_BATCH:-500}"
  export MATTERCODEX_AUTOMATION_DELIVERY_INTERVAL="${MATTERCODEX_AUTOMATION_DELIVERY_INTERVAL:-5s}"
  export MATTERCODEX_AUTOMATION_DELIVERY_CONCURRENCY="${MATTERCODEX_AUTOMATION_DELIVERY_CONCURRENCY:-4}"
  export MATTERCODEX_RUNTIME_LOG_TAIL_LINES="${MATTERCODEX_RUNTIME_LOG_TAIL_LINES:-40}"
  export MATTERCODEX_RUNTIME_LIMITS_ENABLED="${MATTERCODEX_RUNTIME_LIMITS_ENABLED:-true}"
  export MATTERCODEX_RUNTIME_NODE_ALLOCATABLE_MEMORY="${MATTERCODEX_RUNTIME_NODE_ALLOCATABLE_MEMORY:-}"
  export MATTERCODEX_RUNTIME_AGENT_MEMORY_BUDGET="${MATTERCODEX_RUNTIME_AGENT_MEMORY_BUDGET:-}"
  export MATTERCODEX_RUNTIME_SYSTEM_MEMORY_RESERVE="${MATTERCODEX_RUNTIME_SYSTEM_MEMORY_RESERVE:-}"
  export MATTERCODEX_RUNTIME_QUOTA_PODS="${MATTERCODEX_RUNTIME_QUOTA_PODS:-80}"
  export MATTERCODEX_RUNTIME_QUOTA_JOBS="${MATTERCODEX_RUNTIME_QUOTA_JOBS:-120}"
  export MATTERCODEX_RUNTIME_QUOTA_PVCS="${MATTERCODEX_RUNTIME_QUOTA_PVCS:-200}"
  export MATTERCODEX_RUNTIME_QUOTA_REQUESTS_STORAGE="${MATTERCODEX_RUNTIME_QUOTA_REQUESTS_STORAGE:-256Gi}"
  export MATTERCODEX_RUNTIME_QUOTA_REQUESTS_CPU="${MATTERCODEX_RUNTIME_QUOTA_REQUESTS_CPU:-28}"
  export MATTERCODEX_RUNTIME_QUOTA_REQUESTS_MEMORY="${MATTERCODEX_RUNTIME_QUOTA_REQUESTS_MEMORY:-96Gi}"
  export MATTERCODEX_RUNTIME_LIMIT_DEFAULT_REQUEST_CPU="${MATTERCODEX_RUNTIME_LIMIT_DEFAULT_REQUEST_CPU:-500m}"
  export MATTERCODEX_RUNTIME_LIMIT_DEFAULT_REQUEST_MEMORY="${MATTERCODEX_RUNTIME_LIMIT_DEFAULT_REQUEST_MEMORY:-1Gi}"
  export MATTERCODEX_AGENT_RUNNER_SERVICE_ACCOUNT="${MATTERCODEX_AGENT_RUNNER_SERVICE_ACCOUNT:-matter-codex-agent-runner}"
  export MATTERCODEX_AGENT_RUNNER_CLUSTER_ADMIN_SERVICE_ACCOUNT="${MATTERCODEX_AGENT_RUNNER_CLUSTER_ADMIN_SERVICE_ACCOUNT:-matter-codex-agent-runner-cluster-admin}"
  export MATTERCODEX_CODEX_AUTH_SECRET="${MATTERCODEX_CODEX_AUTH_SECRET:-matter-codex-codex-auth}"
  export MATTERCODEX_CODEX_AUTH_ACCOUNT="${MATTERCODEX_CODEX_AUTH_ACCOUNT:-primary}"
  export MATTERCODEX_STORAGE_MIGRATIONS_ENABLED="${MATTERCODEX_STORAGE_MIGRATIONS_ENABLED:-true}"
  export MATTERCODEX_BOT_SERVICE_READ_HEADER_TIMEOUT="${MATTERCODEX_BOT_SERVICE_READ_HEADER_TIMEOUT:-5s}"
  export MATTERCODEX_BOT_SERVICE_READ_TIMEOUT="${MATTERCODEX_BOT_SERVICE_READ_TIMEOUT:-10s}"
  export MATTERCODEX_BOT_SERVICE_IDLE_TIMEOUT="${MATTERCODEX_BOT_SERVICE_IDLE_TIMEOUT:-60s}"
  export MATTERCODEX_BOT_SERVICE_MAX_HEADER_BYTES="${MATTERCODEX_BOT_SERVICE_MAX_HEADER_BYTES:-1048576}"
  export MATTERCODEX_BOT_SERVICE_MAX_GITHUB_WEBHOOK_BYTES="${MATTERCODEX_BOT_SERVICE_MAX_GITHUB_WEBHOOK_BYTES:-262144}"
  export MATTERCODEX_BOT_SERVICE_MAX_MCP_REQUEST_BODY_BYTES="${MATTERCODEX_BOT_SERVICE_MAX_MCP_REQUEST_BODY_BYTES:-1048576}"
  export MATTERCODEX_MATTERMOST_HTTP_TIMEOUT="${MATTERCODEX_MATTERMOST_HTTP_TIMEOUT:-5s}"
  export MATTERCODEX_MATTERMOST_HTTP_DIAL_TIMEOUT="${MATTERCODEX_MATTERMOST_HTTP_DIAL_TIMEOUT:-2s}"
  export MATTERCODEX_MATTERMOST_HTTP_TLS_HANDSHAKE_TIMEOUT="${MATTERCODEX_MATTERMOST_HTTP_TLS_HANDSHAKE_TIMEOUT:-2s}"
  export MATTERCODEX_MATTERMOST_HTTP_RESPONSE_HEADER_TIMEOUT="${MATTERCODEX_MATTERMOST_HTTP_RESPONSE_HEADER_TIMEOUT:-3s}"
  export MATTERCODEX_MATTERMOST_HTTP_IDLE_CONN_TIMEOUT="${MATTERCODEX_MATTERMOST_HTTP_IDLE_CONN_TIMEOUT:-30s}"
  export MATTERCODEX_CALLBACK_MAX_BYTES="${MATTERCODEX_CALLBACK_MAX_BYTES:-131072}"
  export MATTERCODEX_CALLBACK_MAX_CHUNKS="${MATTERCODEX_CALLBACK_MAX_CHUNKS:-8}"
  export MATTERCODEX_CALLBACK_MAX_CHUNK_BYTES="${MATTERCODEX_CALLBACK_MAX_CHUNK_BYTES:-49152}"
  export MATTERCODEX_CALLBACK_PUBLISH_CONCURRENCY="${MATTERCODEX_CALLBACK_PUBLISH_CONCURRENCY:-4}"
  export MATTERCODEX_CALLBACK_PUBLISH_DEADLINE="${MATTERCODEX_CALLBACK_PUBLISH_DEADLINE:-5s}"
  export MATTERCODEX_MATTERMOST_ALLOWED_UNTRUSTED_INTERNAL_CONNECTIONS="${MATTERCODEX_MATTERMOST_ALLOWED_UNTRUSTED_INTERNAL_CONNECTIONS:-$(mattercodex_host_from_url "$MATTERCODEX_BOT_SERVICE_INTERNAL_URL")}"
  export MATTERCODEX_MATTERMOST_BOT_USERNAME="${MATTERCODEX_MATTERMOST_BOT_USERNAME:-matter-codex}"
  export MATTERCODEX_MATTERMOST_BOT_EMAIL="${MATTERCODEX_MATTERMOST_BOT_EMAIL:-matter-codex@local.invalid}"
  export MATTERCODEX_MATTERMOST_ADMIN_USERNAME="${MATTERCODEX_MATTERMOST_ADMIN_USERNAME:-matter-codex-lifecycle-admin}"
  export MATTERCODEX_MATTERMOST_ADMIN_EMAIL="${MATTERCODEX_MATTERMOST_ADMIN_EMAIL:-matter-codex-lifecycle-admin@local.invalid}"
  export MATTERCODEX_DEFAULT_TEAM_NAME="${MATTERCODEX_DEFAULT_TEAM_NAME:-agents}"
  export MATTERCODEX_DEFAULT_TEAM_DISPLAY_NAME="${MATTERCODEX_DEFAULT_TEAM_DISPLAY_NAME:-Agents}"
  export MATTERCODEX_DEFAULT_CHANNELS="${MATTERCODEX_DEFAULT_CHANNELS:-agents-control:Agents Control,agents-runs:Agents Runs,agent-alerts:Agent Alerts,agents-audit:Agents Audit}"
}

mattercodex_memory_quantity_kib() {
  local raw="${1:-}"
  if [[ ! "$raw" =~ ^([1-9][0-9]{0,8})(Ki|Mi|Gi|Ti)$ ]]; then
    return 1
  fi

  local amount="${BASH_REMATCH[1]}"
  local unit="${BASH_REMATCH[2]}"
  local factor
  case "$unit" in
    Ki) factor=1 ;;
    Mi) factor=1024 ;;
    Gi) factor=1048576 ;;
    Ti) factor=1073741824 ;;
    *) return 1 ;;
  esac
  local amount_decimal value_kib
  amount_decimal="$((10#$amount))"
  [ "$amount_decimal" -le "$((9007199254740991 / factor))" ] || return 1
  value_kib="$((amount_decimal * factor))"
  printf '%s\n' "$value_kib"
}

mattercodex_validate_agent_memory_guard() {
  if ! mattercodex_bool "${MATTERCODEX_RUNTIME_ENABLED:-}"; then
    return
  fi
  mattercodex_bool "${MATTERCODEX_RUNTIME_LIMITS_ENABLED:-}" || mattercodex_die "runtime memory guard нельзя выключать при включённом agent runtime"
  mattercodex_require_env \
    MATTERCODEX_RUNTIME_NODE_ALLOCATABLE_MEMORY \
    MATTERCODEX_RUNTIME_AGENT_MEMORY_BUDGET \
    MATTERCODEX_RUNTIME_SYSTEM_MEMORY_RESERVE \
    MATTERCODEX_AGENT_WORKLOAD_PRIORITY_CLASS

  local node_memory agent_budget system_reserve session_request session_limit utility_limit dev_shm_limit
  node_memory="$(mattercodex_memory_quantity_kib "$MATTERCODEX_RUNTIME_NODE_ALLOCATABLE_MEMORY")" || mattercodex_die "MATTERCODEX_RUNTIME_NODE_ALLOCATABLE_MEMORY должен быть положительным Kubernetes memory quantity с суффиксом Ki, Mi, Gi или Ti"
  agent_budget="$(mattercodex_memory_quantity_kib "$MATTERCODEX_RUNTIME_AGENT_MEMORY_BUDGET")" || mattercodex_die "MATTERCODEX_RUNTIME_AGENT_MEMORY_BUDGET должен быть положительным Kubernetes memory quantity с суффиксом Ki, Mi, Gi или Ti"
  system_reserve="$(mattercodex_memory_quantity_kib "$MATTERCODEX_RUNTIME_SYSTEM_MEMORY_RESERVE")" || mattercodex_die "MATTERCODEX_RUNTIME_SYSTEM_MEMORY_RESERVE должен быть положительным Kubernetes memory quantity с суффиксом Ki, Mi, Gi или Ti"
  session_request="$(mattercodex_memory_quantity_kib "$MATTERCODEX_AGENT_SESSION_MEMORY_REQUEST")" || mattercodex_die "MATTERCODEX_AGENT_SESSION_MEMORY_REQUEST должен быть положительным Kubernetes memory quantity с суффиксом Ki, Mi, Gi или Ti"
  session_limit="$(mattercodex_memory_quantity_kib "$MATTERCODEX_AGENT_SESSION_MEMORY_LIMIT")" || mattercodex_die "MATTERCODEX_AGENT_SESSION_MEMORY_LIMIT должен быть положительным Kubernetes memory quantity с суффиксом Ki, Mi, Gi или Ti"
  utility_limit="$(mattercodex_memory_quantity_kib "$MATTERCODEX_AGENT_UTILITY_MEMORY_LIMIT")" || mattercodex_die "MATTERCODEX_AGENT_UTILITY_MEMORY_LIMIT должен быть положительным Kubernetes memory quantity с суффиксом Ki, Mi, Gi или Ti"
  dev_shm_limit="$(mattercodex_memory_quantity_kib "$MATTERCODEX_AGENT_DEV_SHM_SIZE_LIMIT")" || mattercodex_die "MATTERCODEX_AGENT_DEV_SHM_SIZE_LIMIT должен быть положительным Kubernetes memory quantity с суффиксом Ki, Mi, Gi или Ti"

  [ "$session_request" -eq "$session_limit" ] || mattercodex_die "session memory request должен совпадать с memory limit для безопасного scheduler budget"
  [ "$session_limit" -le "$agent_budget" ] || mattercodex_die "session memory limit превышает aggregate agent memory budget"
  [ "$utility_limit" -le "$agent_budget" ] || mattercodex_die "utility memory limit превышает aggregate agent memory budget"
  [ "$dev_shm_limit" -le "$session_limit" ] || mattercodex_die "agent dev shm size limit превышает session memory limit"
  [ $((agent_budget + system_reserve)) -le "$node_memory" ] || mattercodex_die "aggregate agent memory budget вместе с системным резервом превышает allocatable memory узла"

  if [ "${#MATTERCODEX_AGENT_WORKLOAD_PRIORITY_CLASS}" -gt 63 ] || [[ ! "$MATTERCODEX_AGENT_WORKLOAD_PRIORITY_CLASS" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]]; then
    mattercodex_die "MATTERCODEX_AGENT_WORKLOAD_PRIORITY_CLASS должен быть корректным DNS label"
  fi
}

mattercodex_validate_agent_workload_inventory() {
  mattercodex_require_commands jq
  local inventory agent_budget legacy_count
  inventory="$(cat)"
  jq -e '.items | type == "array"' >/dev/null <<<"$inventory" || mattercodex_die "Kubernetes inventory agent workloads имеет неверный формат"
  agent_budget="$(mattercodex_memory_quantity_kib "$MATTERCODEX_RUNTIME_AGENT_MEMORY_BUDGET")" || mattercodex_die "aggregate agent memory budget не прошёл проверку диапазона"
  legacy_count="$(jq -r --arg service_account "$MATTERCODEX_AGENT_RUNNER_CLUSTER_ADMIN_SERVICE_ACCOUNT" '[.items[] | select((.spec.serviceAccountName // "default") == $service_account)] | length' <<<"$inventory")"
  [ "$legacy_count" -eq 0 ] || mattercodex_die "найдены pod с отозванным cluster-admin ServiceAccount; останови связанные workload и удали только их Pod/Job перед повтором"

  local pod_name priority_class service_account init_count container_count request_memory limit_memory request_kib limit_kib
  while IFS=$'\t' read -r pod_name priority_class service_account init_count container_count request_memory limit_memory; do
    [ -n "$pod_name" ] || continue
    [ "$priority_class" = "$MATTERCODEX_AGENT_WORKLOAD_PRIORITY_CLASS" ] || mattercodex_die "agent workload находится вне выбранного PriorityClass; выполни fail-closed reconciliation Pod/Job перед rollout"
    [ "$service_account" = "$MATTERCODEX_AGENT_RUNNER_SERVICE_ACCOUNT" ] || mattercodex_die "agent workload использует неподдерживаемый ServiceAccount; выполни fail-closed reconciliation Pod/Job перед rollout"
    [ "$init_count" -eq 0 ] && [ "$container_count" -eq 1 ] || mattercodex_die "agent workload имеет неподдерживаемый состав контейнеров; выполни reconciliation перед rollout"
    request_kib="$(mattercodex_memory_quantity_kib "$request_memory")" || mattercodex_die "agent workload не имеет проверяемого memory request"
    limit_kib="$(mattercodex_memory_quantity_kib "$limit_memory")" || mattercodex_die "agent workload не имеет проверяемого memory limit"
    [ "$request_kib" -eq "$limit_kib" ] || mattercodex_die "agent workload имеет legacy memory request/limit; выполни reconciliation Pod/Job перед rollout"
    [ "$limit_kib" -le "$agent_budget" ] || mattercodex_die "agent workload превышает aggregate memory budget; выполни reconciliation перед rollout"
  done < <(jq -r '.items[] | select(.metadata.labels["app.kubernetes.io/name"] == "matter-codex-agent-runner") | [
    .metadata.name,
    (.spec.priorityClassName // ""),
    (.spec.serviceAccountName // "default"),
    ((.spec.initContainers // []) | length),
    ((.spec.containers // []) | length),
    (.spec.containers[0].resources.requests.memory // ""),
    (.spec.containers[0].resources.limits.memory // "")
  ] | @tsv' <<<"$inventory")
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

mattercodex_pod_input_revision() {
  [ "$#" -gt 0 ] || mattercodex_die "для pod input revision нужен хотя бы один объект"
  mattercodex_require_commands sha256sum
  printf '%s\0' "$@" | sha256sum | awk '{print $1}'
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

mattercodex_validate_postgres_dsn() {
  local dsn="${1:-}"
  local postgres_dsn_pattern='^postgres://([^:/@?#]+):([^/@?#]+)@([[:alnum:].-]+):([0-9]+)/([^/?#]+)\?sslmode=disable&connect_timeout=10$'
  if [[ ! "$dsn" =~ $postgres_dsn_pattern ]]; then
    mattercodex_die "PostgreSQL DSN не прошёл безопасную синтаксическую проверку"
  fi
}

mattercodex_postgres_dsn() {
  local username="${1:-}"
  local password="${2:-}"
  local hostname="${3:-}"
  local database="${4:-}"
  if [ -z "$username" ] || [ -z "$password" ] || [ -z "$hostname" ] || [ -z "$database" ]; then
    mattercodex_die "для PostgreSQL DSN обязательны user, password, host и database"
  fi
  local encoded_username encoded_password encoded_database dsn
  encoded_username="$(jq -nr --arg value "$username" '$value | @uri')"
  encoded_password="$(jq -nr --arg value "$password" '$value | @uri')"
  encoded_database="$(jq -nr --arg value "$database" '$value | @uri')"
  dsn="postgres://${encoded_username}:${encoded_password}@${hostname}:5432/${encoded_database}?sslmode=disable&connect_timeout=10"
  mattercodex_validate_postgres_dsn "$dsn"
  printf '%s\n' "$dsn"
}

mattercodex_generate_oauth2_cookie_secret() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -base64 24 | tr -d '\n'
    return
  fi
  mattercodex_die "openssl нужен для генерации OAuth2 cookie secret, если он еще не задан"
}
