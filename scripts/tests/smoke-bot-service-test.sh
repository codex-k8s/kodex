#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
TEST_DIR="$(mktemp -d)"
trap 'rm -rf "$TEST_DIR"' EXIT

ENV_FILE="$TEST_DIR/synthetic.env"
{
  printf '%s\n' 'TARGET_HOST=synthetic.invalid'
  printf '%s\n' 'TARGET_PORT=22'
  printf '%s\n' 'TARGET_ROOT_USER=synthetic'
  printf '%s\n' 'TARGET_ROOT_SSH_KEY=/tmp/synthetic-key'
  printf '%s\n' 'OPERATOR_USER=synthetic'
  printf '%s\n' 'OPERATOR_SSH_PUBKEY_PATH=/tmp/synthetic.pub'
  printf '%s\n' 'PRODUCTION_NAMESPACE=mattermost'
  printf '%s\n' 'PRODUCTION_DOMAIN=example.invalid'
  printf '%s\n' 'PUBLIC_BASE_URL=https://mattermost.example.invalid'
  printf '%s\n' 'LETSENCRYPT_EMAIL=synthetic@example.invalid'
  printf '%s\n' 'MATTERCODEX_BOT_SERVICE_SITE_URL=https://bot.example.invalid'
  printf '%s\n' 'MATTERCODEX_REMOTE_KUBECTL=kubectl'
} >"$ENV_FILE"

kubectl() {
  case "$*" in
    *jsonpath*) printf '1' ;;
  esac
}

ssh() {
  return 0
}

curl() {
  local method="GET"
  local url="${!#}"
  local previous=""
  local argument
  for argument in "$@"; do
    if [ "$previous" = "-X" ]; then
      method="$argument"
    fi
    previous="$argument"
  done
  printf '%s %s\n' "$method" "$url" >>"$SMOKE_CALLS_FILE"
  case "$url" in
    */mattermost/slash/agents) printf '%s' "${SMOKE_SLASH_STATUS:-401}" ;;
    */github/webhook) printf '%s' "${SMOKE_GITHUB_STATUS:-503}" ;;
    *) printf '%s' "${SMOKE_INTERNAL_STATUS:-404}" ;;
  esac
}

export -f kubectl ssh curl

expected_matrix() {
  local origin="$1"
  printf '%s\n' \
    "POST $origin/mattermost/slash/agents" \
    "POST $origin/github/webhook" \
    "GET $origin/" \
    "GET $origin/healthz" \
    "GET $origin/health/livez" \
    "GET $origin/health/readyz" \
    "GET $origin/readyz" \
    "GET $origin/metrics" \
    "GET $origin/mattermost/actions/agents" \
    "GET $origin/mattermost/dialogs/agents" \
    "GET $origin/internal/agent-sessions/synthetic-smoke" \
    "GET $origin/mcp/sessions/synthetic-smoke" \
    "GET $origin/internal/synthetic-smoke"
}

for script in scripts/k8s/smoke-bot-service.sh scripts/remote/smoke-bot-service.sh; do
  SMOKE_CALLS_FILE="$TEST_DIR/$(basename "$(dirname "$script")").calls"
  export SMOKE_CALLS_FILE
  : >"$SMOKE_CALLS_FILE"
  bash "$REPO_ROOT/$script" --env-file "$ENV_FILE" --check-url >/dev/null
  expected_matrix 'https://bot.example.invalid' >"$TEST_DIR/expected.calls"
  diff -u "$TEST_DIR/expected.calls" "$SMOKE_CALLS_FILE"
done

SMOKE_CALLS_FILE="$TEST_DIR/negative.calls"
SMOKE_INTERNAL_STATUS=200
export SMOKE_CALLS_FILE SMOKE_INTERNAL_STATUS
if bash "$REPO_ROOT/scripts/k8s/smoke-bot-service.sh" --env-file "$ENV_FILE" --check-url >/dev/null 2>&1; then
  printf 'smoke принял опубликованный внутренний маршрут\n' >&2
  exit 1
fi

printf 'матрица smoke-bot-service: PASS\n'
