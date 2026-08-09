#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'ARC bootstrap failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf 'Usage: %s --context <exact-context> --mode preflight|apply|readback [--github-app-id-file <path> --github-app-installation-id-file <path> --github-app-private-key-file <path>]\n' "$0" >&2
}

expected_context=""
mode=""
github_app_id_file=""
github_app_installation_id_file=""
github_app_private_key_file=""
while (($# > 0)); do
  case "$1" in
    --context) expected_context="${2:-}"; shift 2 ;;
    --mode) mode="${2:-}"; shift 2 ;;
    --github-app-id-file) github_app_id_file="${2:-}"; shift 2 ;;
    --github-app-installation-id-file) github_app_installation_id_file="${2:-}"; shift 2 ;;
    --github-app-private-key-file) github_app_private_key_file="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$expected_context" ]] || fail "exact Kubernetes context is required"
case "$mode" in preflight|apply|readback) ;; *) fail "mode must be preflight, apply or readback" ;; esac
for command_name in kubectl helm sha256sum awk grep jq; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
actual_context=$(kubectl config current-context)
[[ "$actual_context" == "$expected_context" ]] || fail "Kubernetes context mismatch"

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repository_root=$(cd -- "$script_directory/../.." && pwd -P)
lock_file="$script_directory/chart.lock"
namespace=mattercodex-ci
credential_secret=mattercodex-github-runner-auth
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
umask 077

pull_chart() {
  local chart=$1 version expected_sha archive actual_sha
  version=$(awk -v name="$chart" '$1 == name {print $2}' "$lock_file")
  expected_sha=$(awk -v name="$chart" '$1 == name {print $3}' "$lock_file")
  [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail "invalid chart version for $chart"
  [[ "$expected_sha" =~ ^[a-f0-9]{64}$ ]] || fail "invalid chart checksum for $chart"
  helm pull "oci://ghcr.io/actions/actions-runner-controller-charts/$chart" \
    --version "$version" --destination "$temporary_directory" >/dev/null 2>&1
  archive="$temporary_directory/$chart-$version.tgz"
  actual_sha=$(sha256sum "$archive" | awk '{print $1}')
  [[ "$actual_sha" == "$expected_sha" ]] || fail "chart checksum mismatch for $chart"
  printf '%s' "$archive"
}

validate_github_app_files() {
  [[ -r "$github_app_id_file" && -r "$github_app_installation_id_file" && -r "$github_app_private_key_file" ]] ||
    fail "GitHub App credential files are required to create the runner auth Secret"
  grep -Eq '^[0-9]+$' "$github_app_id_file" || fail "GitHub App ID file is invalid"
  grep -Eq '^[0-9]+$' "$github_app_installation_id_file" || fail "GitHub App installation ID file is invalid"
  grep -q '^-----BEGIN .*PRIVATE KEY-----$' "$github_app_private_key_file" || fail "GitHub App private key file is invalid"
}

verify_credential_secret() {
  local keys
  keys=$(kubectl --context "$expected_context" -n "$namespace" get secret "$credential_secret" -o json |
    jq -c '[.data | keys[]] | sort')
  [[ "$keys" == '["github_app_id","github_app_installation_id","github_app_private_key"]' ]] ||
    fail "runner auth Secret has an unexpected key set"
}

if [[ "$mode" == readback ]]; then
  kubectl --context "$expected_context" get namespace "$namespace" >/dev/null
  verify_credential_secret
  kubectl --context "$expected_context" -n "$namespace" get deployment,autoscalingrunnerset,ephemeralrunnerset \
    -o custom-columns=KIND:.kind,NAME:.metadata.name,READY:.status.readyReplicas --no-headers
  printf 'ARC readback completed\n'
  exit 0
fi

controller_chart=$(pull_chart gha-runner-scale-set-controller)
runner_chart=$(pull_chart gha-runner-scale-set)

server_minor=$(kubectl --context "$expected_context" version -o json | jq -r '.serverVersion.minor | sub("[^0-9].*$"; "") | tonumber')
((server_minor >= 33)) || fail "Kubernetes 1.33 or newer is required for stable Pod user namespaces"
kubectl --context "$expected_context" apply --dry-run=client -f "$script_directory/namespace-rbac.yaml" >/dev/null
kubectl --context "$expected_context" api-resources --api-group=rbac.authorization.k8s.io >/dev/null
helm template mattercodex-arc "$controller_chart" -n "$namespace" -f "$script_directory/controller-values.yaml" >/dev/null
helm template mattercodex-build "$runner_chart" -n "$namespace" -f "$script_directory/build-runner-values.yaml" >/dev/null
helm template mattercodex-deploy "$runner_chart" -n "$namespace" -f "$script_directory/deploy-runner-values.yaml" >/dev/null

if [[ "$mode" == preflight ]]; then
  printf 'ARC preflight completed\n'
  exit 0
fi

kubectl --context "$expected_context" apply -f "$script_directory/namespace-rbac.yaml" >/dev/null
if kubectl --context "$expected_context" -n "$namespace" get secret "$credential_secret" >/dev/null 2>&1; then
  verify_credential_secret
else
  validate_github_app_files
  kubectl --context "$expected_context" -n "$namespace" create secret generic "$credential_secret" \
    --from-file=github_app_id="$github_app_id_file" \
    --from-file=github_app_installation_id="$github_app_installation_id_file" \
    --from-file=github_app_private_key="$github_app_private_key_file" >/dev/null
  verify_credential_secret
fi

helm upgrade --install mattercodex-arc "$controller_chart" -n "$namespace" \
  -f "$script_directory/controller-values.yaml" --wait --timeout 5m >/dev/null
helm upgrade --install mattercodex-build "$runner_chart" -n "$namespace" \
  -f "$script_directory/build-runner-values.yaml" --wait --timeout 5m >/dev/null
helm upgrade --install mattercodex-deploy "$runner_chart" -n "$namespace" \
  -f "$script_directory/deploy-runner-values.yaml" --wait --timeout 5m >/dev/null

kubectl --context "$expected_context" -n "$namespace" rollout status deployment/mattercodex-arc-gha-rs-controller --timeout=5m >/dev/null
verify_credential_secret
printf 'ARC apply completed\n'
