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

for command_name in curl kubectl node npm; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$port" =~ ^[0-9]+$ ]] && ((port >= 1024 && port <= 65535)) || fail 'KODEX_E2E_SYNTHETIC_PORT must be between 1024 and 65535'
[[ ${KODEX_E2E_CONFIRM_DISPOSABLE:-} == 'I_UNDERSTAND_THIS_MUTATES_A_DISPOSABLE_INSTALLATION' ]] ||
  fail 'disposable installation confirmation is required'
[[ -n ${KODEX_E2E_BASE_URL:-} ]] || fail 'KODEX_E2E_BASE_URL is required'
[[ -n ${KODEX_E2E_OWNER_USERNAME:-} ]] || fail 'KODEX_E2E_OWNER_USERNAME is required'
[[ -n ${KODEX_E2E_OWNER_PASSWORD:-} ]] || fail 'KODEX_E2E_OWNER_PASSWORD is required'
[[ -n ${KODEX_E2E_RESOURCE_PREFIX:-} ]] || fail 'KODEX_E2E_RESOURCE_PREFIX is required'

if [[ -n ${KODEX_E2E_KUBECONFIG:-} ]]; then
  [[ -f $KODEX_E2E_KUBECONFIG ]] || fail 'KODEX_E2E_KUBECONFIG is not a regular file'
  export KUBECONFIG=$KODEX_E2E_KUBECONFIG
fi

github_enabled=${KODEX_INTEGRATION_E2E_GITHUB:-0}
[[ $github_enabled == 0 || $github_enabled == 1 ]] || fail 'KODEX_INTEGRATION_E2E_GITHUB must be 0 or 1'
if [[ $github_enabled == 1 ]]; then
  if [[ -n ${KODEX_GITHUB_BOT_PAT:-} && -n ${KODEX_GITHUB_BOT_PAT_FILE:-} ]] ||
    [[ -z ${KODEX_GITHUB_BOT_PAT:-} && -z ${KODEX_GITHUB_BOT_PAT_FILE:-} ]]; then
    fail 'exactly one GitHub token source is required'
  fi
fi

[[ -x $control_center/node_modules/.bin/playwright ]] || fail 'run npm ci --ignore-scripts in services/staff/control-center first'

temporary_directory=$(mktemp -d)
port_forward_pid=''
cleanup() {
  if [[ -n $port_forward_pid ]]; then
    kill "$port_forward_pid" >/dev/null 2>&1 || true
    wait "$port_forward_pid" >/dev/null 2>&1 || true
  fi
  rm -rf -- "$temporary_directory"
}
trap cleanup EXIT INT TERM

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
export KODEX_E2E_PRIVATE_OUTPUT_DIR="$temporary_directory/playwright-output"
export KODEX_E2E_SYNTHETIC_READBACK_URL="$readback_origin"

(
  cd -- "$control_center"
  npm run test:e2e:auth
  ./node_modules/.bin/playwright test --config playwright.integration.config.ts
)

printf 'Deployed integration E2E passed\n'
