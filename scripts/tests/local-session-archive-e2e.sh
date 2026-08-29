#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Local session archive E2E failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    'Usage: local-session-archive-e2e.sh --context <exact-context>' \
    '  --kubeconfig <path> --state-directory <path>' >&2
}

context=""
kubeconfig=""
state_directory=""
while (($# > 0)); do
  case "$1" in
    --context) context=${2:-}; shift 2 ;;
    --kubeconfig) kubeconfig=${2:-}; shift 2 ;;
    --state-directory) state_directory=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

confirmation=I_UNDERSTAND_THIS_MUTATES_A_DISPOSABLE_INSTALLATION
[[ ${KODEX_E2E_CONFIRM_DISPOSABLE:-} == "$confirmation" ]] ||
  fail 'explicit disposable installation confirmation is required'
[[ -n "$context" && "${context,,}" != *prod* && "${context,,}" != *production* ]] ||
  fail 'exact non-production context is required'
[[ "$kubeconfig" == /* && -f "$kubeconfig" && -r "$kubeconfig" && ! -L "$kubeconfig" ]] ||
  fail 'Kubernetes configuration is absent or unsafe'
[[ "$state_directory" == /* && -d "$state_directory" && ! -L "$state_directory" ]] ||
  fail 'state directory is absent or unsafe'
for command_name in head jq kubectl sed seq sleep; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done

export KUBECONFIG="$kubeconfig"
[[ $(kubectl config current-context) == "$context" ]] || fail 'Kubernetes context mismatch'
kubectl get --raw=/readyz >/dev/null || fail 'Kubernetes API is unavailable'
kubectl get namespace/kodex-system -o json | jq -e '
  .metadata.labels["app.kubernetes.io/part-of"] == "kodex" and
  .metadata.labels["kodex.dev/environment"] == "staging" and
  .metadata.labels["kodex.dev/local-profile"] == "hot-reload"
' >/dev/null || fail 'Kodex namespace is not the exact disposable local profile'

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
temporary_directory=$(mktemp -d "$state_directory/e2e/session-archive.XXXXXX")
port_forward_pid=""
cleanup() {
  if [[ -n "$port_forward_pid" ]]; then
    kill "$port_forward_pid" >/dev/null 2>&1 || true
    wait "$port_forward_pid" >/dev/null 2>&1 || true
  fi
  rm -rf -- "$temporary_directory"
}
trap cleanup EXIT

secret_state="$temporary_directory/object-storage.json"
kubectl -n kodex-system get secrets -l kodex.dev/local-credential=object-storage -o json >"$secret_state"
chmod 0600 "$secret_state"
[[ $(jq '.items | length' "$secret_state") == 1 ]] || fail 'exactly one local object storage credential is required'
jq -er '.items[0].data["access-key"] | @base64d' "$secret_state" >"$temporary_directory/access-key"
jq -er '.items[0].data["secret-key"] | @base64d' "$secret_state" >"$temporary_directory/secret-key"
chmod 0600 "$temporary_directory/access-key" "$temporary_directory/secret-key"

port_forward_log="$temporary_directory/port-forward.log"
kubectl -n kodex-system port-forward --address=127.0.0.1 service/seaweedfs-s3 :8333 \
  >"$port_forward_log" 2>&1 &
port_forward_pid=$!
endpoint=""
for _ in $(seq 1 100); do
  endpoint=$(sed -nE 's/^Forwarding from 127\.0\.0\.1:([0-9]+) -> 8333$/http:\/\/127.0.0.1:\1/p' "$port_forward_log" | head -n 1)
  [[ -n "$endpoint" ]] && break
  kill -0 "$port_forward_pid" >/dev/null 2>&1 || fail 'SeaweedFS port-forward failed'
  sleep 0.1
done
[[ -n "$endpoint" ]] || fail 'SeaweedFS loopback endpoint was not established'

SESSION_ARCHIVE_E2E_ENDPOINT="$endpoint" \
SESSION_ARCHIVE_E2E_ACCESS_KEY_FILE="$temporary_directory/access-key" \
SESSION_ARCHIVE_E2E_SECRET_KEY_FILE="$temporary_directory/secret-key" \
  "$repository_root/scripts/tests/session-archive-seaweedfs-e2e-test.sh"
