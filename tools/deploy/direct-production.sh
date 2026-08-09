#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'Direct production operation failed: %s\n' "$*" >&2; exit 1; }
usage() {
  printf 'Usage: %s --context <exact-context> --operation preflight|apply|readback --mode dark|cutover|rollback --source-sha <40-hex> --lock <path> --lock-sha256 <64-hex>\n' "$0" >&2
}

expected_context=""
operation=""
mode=""
source_sha=""
lock_file=""
lock_sha256=""
while (($# > 0)); do
  case "$1" in
    --context) expected_context="${2:-}"; shift 2 ;;
    --operation) operation="${2:-}"; shift 2 ;;
    --mode) mode="${2:-}"; shift 2 ;;
    --source-sha) source_sha="${2:-}"; shift 2 ;;
    --lock) lock_file="${2:-}"; shift 2 ;;
    --lock-sha256) lock_sha256="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$expected_context" ]] || fail "exact Kubernetes context is required"
case "$operation" in preflight|apply|readback) ;; *) fail "operation must be preflight, apply or readback" ;; esac
case "$mode" in dark|cutover|rollback) ;; *) fail "mode must be dark, cutover or rollback" ;; esac
[[ "$source_sha" =~ ^[a-f0-9]{40}$ ]] || fail "source SHA must be exact lowercase 40-hex"
for command_name in kubectl jq sha256sum; do command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"; done
[[ "$(kubectl config current-context)" == "$expected_context" ]] || fail "Kubernetes context mismatch"

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repository_root=$(cd -- "$script_directory/../.." && pwd -P)
validator="$repository_root/tools/release/validate-release-lock.sh"
renderer="$repository_root/tools/release/render-direct-production.sh"
"$validator" --lock "$lock_file" --source-sha "$source_sha" --sha256 "$lock_sha256" >/dev/null

if [[ "$mode" == cutover ]]; then
  [[ "${MATTERCODEX_CUTOVER_OWNER_GATE:-}" == approved ]] || fail "cutover owner gate is missing"
  command -v gh >/dev/null 2>&1 || fail "gh is required for cutover blocker verification"
  for issue_number in 241 237 194; do
    state=$(gh issue view "$issue_number" --repo codex-k8s/matter-codex --json state --jq .state)
    [[ "$state" == CLOSED ]] || fail "cutover blocker #$issue_number is not closed"
  done
  fail "cutover manifest is intentionally absent from Wave A; materialize it after blockers #241, #237 and #194"
fi

if [[ "$mode" == rollback ]]; then
  [[ "${MATTERCODEX_ROLLBACK_OWNER_GATE:-}" == approved ]] || fail "rollback owner gate is missing"
fi

temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
render_file="$temporary_directory/direct-production.yaml"
"$renderer" --lock "$lock_file" --source-sha "$source_sha" --sha256 "$lock_sha256" --output "$render_file" >/dev/null
kubectl --context "$expected_context" apply --dry-run=server -f "$render_file" >/dev/null
kubectl --context "$expected_context" auth can-i patch statefulsets.apps -n mattercodex-system | grep -qx yes || fail "deployer cannot patch StatefulSets"

if [[ "$operation" == preflight ]]; then
  kubectl --context "$expected_context" -n matter-kodex-prod get service matter-codex-registry >/dev/null
  printf 'Direct production preflight completed for mode %s\n' "$mode"
  exit 0
fi

if [[ "$operation" == apply ]]; then
  [[ "${MATTERCODEX_PRODUCTION_OWNER_GATE:-}" == approved ]] || fail "production owner gate is missing"
  "$script_directory/bootstrap-direct-production-secrets.sh" --context "$expected_context" --mode apply >/dev/null
  kubectl --context "$expected_context" apply -f "$render_file" >/dev/null
  for workload in mattercodex-postgresql mattercodex-redis mattercodex-nats mattercodex-object-storage; do
    kubectl --context "$expected_context" -n mattercodex-system rollout status "statefulset/$workload" --timeout=5m >/dev/null
  done
  kubectl --context "$expected_context" -n mattercodex-system rollout status deployment/mattercodex-object-store-bootstrap --timeout=3m >/dev/null
fi

stored_source=$(kubectl --context "$expected_context" -n mattercodex-system get configmap mattercodex-release-lock -o jsonpath='{.data.source_sha}')
stored_lock=$(kubectl --context "$expected_context" -n mattercodex-system get configmap mattercodex-release-lock -o jsonpath='{.data.release_lock_sha256}')
[[ "$stored_source" == "$source_sha" && "$stored_lock" == "$lock_sha256" ]] || fail "release readback mismatch"
kubectl --context "$expected_context" -n mattercodex-system get statefulset,deployment,pvc \
  -l mattercodex.dev/profile=direct-production-single-node-prototype \
  -o custom-columns=KIND:.kind,NAME:.metadata.name,READY:.status.readyReplicas --no-headers
if kubectl --context "$expected_context" -n mattercodex-system get ingress -o name | grep -q .; then
  fail "dark namespace contains an Ingress"
fi
printf 'Direct production %s completed for mode %s\n' "$operation" "$mode"
