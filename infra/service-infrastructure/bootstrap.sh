#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Service infrastructure bootstrap failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 --context <exact-context> --mode preflight|apply-controllers|readback" >&2
}

expected_context=""
mode=""
while (($# > 0)); do
  case "$1" in
    --context) expected_context="${2:-}"; shift 2 ;;
    --mode) mode="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$expected_context" ]] || fail 'exact context is required'
[[ "$mode" == preflight || "$mode" == apply-controllers || "$mode" == readback ]] ||
  fail 'mode is invalid'

for command_name in helm jq kubectl sha256sum; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$(kubectl config current-context)" == "$expected_context" ]] || fail 'current Kubernetes context mismatch'

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
lock_file="$script_directory/charts.lock.json"
jq -e '
  .schemaVersion == 1 and (.charts | length) == 1 and
  ([.charts[].name] | unique | length) == 1 and
  all(.charts[];
    (.name | test("^[a-z0-9-]+$")) and
    (.chart | test("^[a-z0-9-]+$")) and
    (.version | test("^v?[0-9]+\\.[0-9]+\\.[0-9]+$")) and
    (.sha256 | test("^[a-f0-9]{64}$")))
' "$lock_file" >/dev/null || fail 'chart lock is invalid'

kubectl get namespace cert-manager >/dev/null 2>&1 || fail 'cert-manager namespace is absent'
kubectl -n cert-manager wait --for=condition=Available deployment/cert-manager --timeout=180s >/dev/null ||
  fail 'cert-manager is unavailable'

if [[ "$mode" == preflight ]]; then
  printf 'Service infrastructure preflight completed\n'
  exit 0
fi

download_directory=$(mktemp -d)
trap 'rm -rf -- "$download_directory"' EXIT

download_chart() {
  local name=$1
  local chart repository version expected_sha chart_directory archive
  chart=$(jq -er --arg name "$name" '.charts[] | select(.name == $name) | .chart' "$lock_file")
  repository=$(jq -er --arg name "$name" '.charts[] | select(.name == $name) | .repository' "$lock_file")
  version=$(jq -er --arg name "$name" '.charts[] | select(.name == $name) | .version' "$lock_file")
  expected_sha=$(jq -er --arg name "$name" '.charts[] | select(.name == $name) | .sha256' "$lock_file")
  chart_directory="$download_directory/$name"
  mkdir -p "$chart_directory"
  if [[ "$repository" == oci://* ]]; then
    helm pull "$repository" --version "$version" --destination "$chart_directory" >/dev/null
  else
    helm repo add "kodex-$name" "$repository" --force-update >/dev/null
    helm pull "kodex-$name/$chart" --version "$version" --destination "$chart_directory"
  fi
  archive=$(find "$chart_directory" -maxdepth 1 -type f -name '*.tgz' -print -quit)
  [[ -n "$archive" ]] || fail "chart archive is absent: $name"
  printf '%s  %s\n' "$expected_sha" "$archive" | sha256sum --check --status ||
    fail "chart digest mismatch: $name"
  printf '%s\n' "$archive"
}

require_ready_deployment_by_selector() {
  local namespace=$1
  local selector=$2
  local description=$3
  local deployment_list deployment_name

  deployment_list=$(kubectl -n "$namespace" get deployment -l "$selector" -o json) ||
    fail "$description deployment query failed"
  deployment_name=$(jq -er '
    if (.items | length) == 1 then
      .items[0].metadata.name
    else
      error("expected exactly one deployment")
    end
  ' <<<"$deployment_list") || fail "$description deployment is not unique"

  kubectl -n "$namespace" rollout status "deployment/$deployment_name" --timeout=180s >/dev/null ||
    fail "$description deployment rollout is incomplete"
  kubectl -n "$namespace" get deployment "$deployment_name" -o json | jq -e '
    (.spec.replicas // 0) > 0 and
    .status.observedGeneration == .metadata.generation and
    (.status.updatedReplicas // 0) == .spec.replicas and
    (.status.readyReplicas // 0) == .spec.replicas and
    (.status.availableReplicas // 0) == .spec.replicas
  ' >/dev/null || fail "$description deployment is not fully ready"
}

if [[ "$mode" == apply-controllers ]]; then
  kubectl create namespace kodex-trust --dry-run=client -o yaml | kubectl apply -f - >/dev/null

  trust_chart=$(download_chart trust-manager)
  helm upgrade --install kodex-trust-manager "$trust_chart" \
    --namespace cert-manager --values "$script_directory/trust-manager-values.yaml" \
    --atomic --wait --timeout 10m
fi

if [[ "$mode" == readback ]]; then
  for resource in bundles.trust.cert-manager.io; do
    kubectl get customresourcedefinition "$resource" >/dev/null 2>&1 ||
      fail "required CRD is absent: $resource"
  done
  require_ready_deployment_by_selector cert-manager \
    'app.kubernetes.io/instance=kodex-trust-manager,app.kubernetes.io/name=trust-manager' \
    'trust-manager'
fi

printf 'Service infrastructure bootstrap completed: %s\n' "$mode"
