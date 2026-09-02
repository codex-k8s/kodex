#!/usr/bin/env bash
set -euo pipefail
umask 077

fail() {
  printf 'Deployed integration E2E failed: %s\n' "$*" >&2
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
control_center="$repository_root/services/staff/control-center"
namespace=kodex-system
port=${KODEX_E2E_SYNTHETIC_PORT:-18082}

for command_name in curl date id jq kubectl node npm stat; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
if [[ ! "$port" =~ ^[0-9]+$ ]] || ((port < 1024 || port > 65535)); then
  fail 'KODEX_E2E_SYNTHETIC_PORT must be between 1024 and 65535'
fi
[[ ${KODEX_E2E_CONFIRM_DISPOSABLE:-} == 'I_UNDERSTAND_THIS_MUTATES_A_DISPOSABLE_INSTALLATION' ]] ||
  fail 'disposable installation confirmation is required'
[[ -n ${KODEX_E2E_BASE_URL:-} ]] || fail 'KODEX_E2E_BASE_URL is required'
[[ -n ${KODEX_E2E_OWNER_USERNAME:-} ]] || fail 'KODEX_E2E_OWNER_USERNAME is required'
[[ -n ${KODEX_E2E_OWNER_PASSWORD:-} ]] || fail 'KODEX_E2E_OWNER_PASSWORD is required'
[[ -n ${KODEX_E2E_RESOURCE_PREFIX:-} ]] || fail 'KODEX_E2E_RESOURCE_PREFIX is required'
[[ ${KODEX_E2E_RESOURCE_PREFIX} =~ ^[a-z0-9]([a-z0-9-]{2,38}[a-z0-9])$ ]] ||
  fail 'KODEX_E2E_RESOURCE_PREFIX must be a lowercase 4-40 character slug'
[[ -n ${KODEX_E2E_STATE_DIRECTORY:-} && ${KODEX_E2E_STATE_DIRECTORY} == /* &&
  -d ${KODEX_E2E_STATE_DIRECTORY} && ! -L ${KODEX_E2E_STATE_DIRECTORY} ]] ||
  fail 'KODEX_E2E_STATE_DIRECTORY must be an existing safe absolute path'
[[ $(stat -c '%u' "$KODEX_E2E_STATE_DIRECTORY") == $(id -u) &&
  $((8#$(stat -c '%a' "$KODEX_E2E_STATE_DIRECTORY") & 8#077)) == 0 ]] ||
  fail 'KODEX_E2E_STATE_DIRECTORY must be owner-private'

if [[ -n ${KODEX_E2E_KUBECONFIG:-} ]]; then
  [[ -f $KODEX_E2E_KUBECONFIG ]] || fail 'KODEX_E2E_KUBECONFIG is not a regular file'
  export KUBECONFIG=$KODEX_E2E_KUBECONFIG
fi

github_enabled=${KODEX_INTEGRATION_E2E_GITHUB:-0}
[[ $github_enabled == 0 || $github_enabled == 1 ]] || fail 'KODEX_INTEGRATION_E2E_GITHUB must be 0 or 1'
provider_api_key_enabled=${KODEX_PROVIDER_E2E_API_KEY:-0}
[[ $provider_api_key_enabled == 0 || $provider_api_key_enabled == 1 ]] ||
  fail 'KODEX_PROVIDER_E2E_API_KEY must be 0 or 1'

[[ -x $control_center/node_modules/.bin/playwright ]] || fail 'run npm ci --ignore-scripts in services/staff/control-center first'

integration_status_directory=${KODEX_E2E_STATE_DIRECTORY}/e2e
if [[ -e $integration_status_directory &&
  ( ! -d $integration_status_directory || -L $integration_status_directory ) ]]; then
  fail 'integration status directory is unsafe'
fi
mkdir -p -- "$integration_status_directory"
chmod 0700 -- "$integration_status_directory"
[[ $((8#$(stat -c '%a' "$integration_status_directory") & 8#077)) == 0 ]] ||
  fail 'integration status directory must be private'
integration_status_file="$integration_status_directory/${KODEX_E2E_RESOURCE_PREFIX}-integration-status.json"
[[ ! -L $integration_status_file ]] || fail 'integration status path must not be a symlink'
temporary_directory=$(mktemp -d)
port_forward_pid=''
started_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
core_status='NOT RUN'
github_status='NOT RUN'
provider_api_key_status='NOT RUN'

write_integration_status() {
  local exit_code=$1 overall_status='FAIL' finished_at temporary_status
  [[ $exit_code -ne 0 ]] || overall_status='PASS'
  finished_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  temporary_status=$(mktemp "$integration_status_file.XXXXXX")
  jq -n \
    --arg status "$overall_status" \
    --arg resource_prefix "$KODEX_E2E_RESOURCE_PREFIX" \
    --arg started_at "$started_at" \
    --arg finished_at "$finished_at" \
    --arg core "$core_status" \
    --arg github "$github_status" \
    --arg provider_api_key "$provider_api_key_status" \
    --argjson github_requested "$github_enabled" \
    --argjson provider_api_key_requested "$provider_api_key_enabled" '
      {
        version:1,
        status:$status,
        resourcePrefix:$resource_prefix,
        startedAt:$started_at,
        finishedAt:$finished_at,
        profiles:{
          core:{requested:true,status:$core},
          github:{requested:($github_requested == 1),status:$github},
          providerApiKey:{requested:($provider_api_key_requested == 1),status:$provider_api_key}
        }
      }
    ' >"$temporary_status"
  chmod 0600 -- "$temporary_status"
  mv -- "$temporary_status" "$integration_status_file"
  printf 'Integration profile GitHub: %s\n' "$github_status"
  printf 'Integration profile API key: %s\n' "$provider_api_key_status"
  printf 'Private integration status: %s\n' "$integration_status_file"
}

cleanup() {
  local exit_code=$?
  trap - EXIT INT TERM
  if [[ -n $port_forward_pid ]]; then
    kill "$port_forward_pid" >/dev/null 2>&1 || true
    wait "$port_forward_pid" >/dev/null 2>&1 || true
  fi
  rm -rf -- "$temporary_directory"
  write_integration_status "$exit_code"
  exit "$exit_code"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

if [[ $github_enabled == 1 ]]; then
  if [[ -n ${KODEX_GITHUB_BOT_PAT:-} && -n ${KODEX_GITHUB_BOT_PAT_FILE:-} ]] ||
    [[ -z ${KODEX_GITHUB_BOT_PAT:-} && -z ${KODEX_GITHUB_BOT_PAT_FILE:-} ]]; then
    fail 'exactly one GitHub token source is required'
  fi
fi
if [[ $provider_api_key_enabled == 1 ]]; then
  if [[ -n ${OPENAI_API_KEY:-} && -n ${KODEX_PROVIDER_E2E_API_KEY_FILE:-} ]] ||
    [[ -z ${OPENAI_API_KEY:-} && -z ${KODEX_PROVIDER_E2E_API_KEY_FILE:-} ]]; then
    fail 'exactly one provider API key source is required'
  fi
fi

for deployment in control-plane control-api-gateway runtime-controller integration-gateway integration-synthetic; do
  kubectl -n "$namespace" wait --for=condition=Available "deployment/$deployment" --timeout=180s >/dev/null ||
    fail "$deployment is not available"
done
if [[ $github_enabled == 1 ]]; then
  kubectl -n "$namespace" wait --for=condition=Available deployment/egress-gateway --timeout=180s >/dev/null ||
    fail 'egress-gateway is not available'
fi

port_forward_log="$temporary_directory/port-forward.log"
kubectl -n "$namespace" port-forward --address 127.0.0.1 service/integration-synthetic "$port:8080" >"$port_forward_log" 2>&1 &
port_forward_pid=$!
readback_origin="http://127.0.0.1:$port"
ready=false
for _ in $(seq 1 80); do
  if curl --silent --show-error --fail --max-time 1 "$readback_origin/readyz" >/dev/null 2>&1; then
    ready=true
    break
  fi
  kill -0 "$port_forward_pid" >/dev/null 2>&1 || fail 'synthetic readback port-forward stopped'
  sleep 0.25
done
[[ $ready == true ]] || fail 'synthetic readback port-forward is not ready'

export KODEX_E2E_PROFILE=web-only
export KODEX_E2E_STORAGE_STATE="$temporary_directory/owner.json"
export KODEX_E2E_SYNTHETIC_READBACK_URL="$readback_origin"

overall_exit=0
if (
  cd -- "$control_center"
  npm run test:e2e:auth
); then
  :
else
  core_status='FAIL'
  fail 'owner authentication for integration profiles failed'
fi

if (
  cd -- "$control_center"
  KODEX_E2E_PRIVATE_OUTPUT_DIR="$temporary_directory/playwright-output/core" \
    ./node_modules/.bin/playwright test --config playwright.integration.config.ts \
      --grep-invert 'опциональный GitHub|опциональный API key'
); then
  core_status='PASS'
else
  core_status='FAIL'
  overall_exit=1
fi

if [[ $github_enabled == 1 ]]; then
  if (
    cd -- "$control_center"
    KODEX_E2E_PRIVATE_OUTPUT_DIR="$temporary_directory/playwright-output/github" \
      ./node_modules/.bin/playwright test --config playwright.integration.config.ts \
        --grep 'опциональный GitHub READ и обратимый WRITE проходят через MCP'
  ); then
    github_status='PASS'
  else
    github_status='FAIL'
    overall_exit=1
  fi
fi

if [[ $provider_api_key_enabled == 1 ]]; then
  if (
    cd -- "$control_center"
    KODEX_E2E_PRIVATE_OUTPUT_DIR="$temporary_directory/playwright-output/provider-api-key" \
      ./node_modules/.bin/playwright test --config playwright.integration.config.ts \
        --grep 'опциональный API key account выполняет run с exact affinity'
  ); then
    provider_api_key_status='PASS'
  else
    provider_api_key_status='FAIL'
    overall_exit=1
  fi
fi

[[ $overall_exit -eq 0 ]] || fail 'one or more deployed integration profiles failed'

printf 'Deployed integration E2E passed\n'
