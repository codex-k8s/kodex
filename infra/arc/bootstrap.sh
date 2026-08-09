#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'ARC bootstrap failed: %s\n' "$*" >&2; exit 1; }
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
for command_name in kubectl helm sha256sum awk grep jq yq; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$(kubectl config current-context)" == "$expected_context" ]] || fail "Kubernetes context mismatch"

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repository_root=$(cd -- "$script_directory/../.." && pwd -P)
lock_file="$script_directory/chart.lock"
credential_secret=mattercodex-github-runner-auth
build_namespace=mattercodex-ci
deploy_namespace=mattercodex-ci-deploy
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
    fail "GitHub App credential files are required to create runner auth Secrets"
  grep -Eq '^[0-9]+$' "$github_app_id_file" || fail "GitHub App ID file is invalid"
  grep -Eq '^[0-9]+$' "$github_app_installation_id_file" || fail "GitHub App installation ID file is invalid"
  grep -q '^-----BEGIN .*PRIVATE KEY-----$' "$github_app_private_key_file" || fail "GitHub App private key file is invalid"
}

verify_credential_secret() {
  local namespace=$1 keys
  keys=$(kubectl --context "$expected_context" -n "$namespace" get secret "$credential_secret" -o json |
    jq -c '[.data | keys[]] | sort')
  [[ "$keys" == '["github_app_id","github_app_installation_id","github_app_private_key"]' ]] ||
    fail "runner auth Secret has an unexpected key set in $namespace"
}

materialize_credential_secret() {
  local namespace=$1
  if kubectl --context "$expected_context" -n "$namespace" get secret "$credential_secret" >/dev/null 2>&1; then
    verify_credential_secret "$namespace"
    return
  fi
  validate_github_app_files
  kubectl --context "$expected_context" -n "$namespace" create secret generic "$credential_secret" \
    --from-file=github_app_id="$github_app_id_file" \
    --from-file=github_app_installation_id="$github_app_installation_id_file" \
    --from-file=github_app_private_key="$github_app_private_key_file" >/dev/null
  verify_credential_secret "$namespace"
}

controller_chart=$(pull_chart gha-runner-scale-set-controller)
runner_chart=$(pull_chart gha-runner-scale-set)
rendered_network_policy="$temporary_directory/network-policy.yaml"
"$script_directory/render-network-policy.sh" "$rendered_network_policy"
server_minor=$(kubectl --context "$expected_context" version -o json |
  jq -r '.serverVersion.minor | sub("[^0-9].*$"; "") | tonumber')
((server_minor >= 33)) || fail "Kubernetes 1.33 or newer is required for stable Pod user namespaces"

kubectl --context "$expected_context" apply --dry-run=client -f "$script_directory/namespace-rbac.yaml" >/dev/null
kubectl --context "$expected_context" apply --dry-run=client -f "$rendered_network_policy" >/dev/null
helm template mattercodex-arc-build "$controller_chart" -n "$build_namespace" \
  -f "$script_directory/controller-values.yaml" >/dev/null
helm template mattercodex-build "$runner_chart" -n "$build_namespace" \
  -f "$script_directory/build-runner-values.yaml" >/dev/null
helm template mattercodex-arc-deploy "$controller_chart" -n "$deploy_namespace" \
  -f "$script_directory/controller-deploy-values.yaml" >/dev/null
helm template mattercodex-deploy "$runner_chart" -n "$deploy_namespace" \
  -f "$script_directory/deploy-runner-values.yaml" >/dev/null

if [[ "$mode" == preflight ]]; then
  printf 'ARC preflight completed\n'
  exit 0
fi

if [[ "$mode" == apply ]]; then
  kubectl --context "$expected_context" apply -f "$script_directory/namespace-rbac.yaml" >/dev/null
  kubectl --context "$expected_context" apply -f "$rendered_network_policy" >/dev/null
  for policy_spec in \
    "$build_namespace controller-kubernetes-api app.kubernetes.io/name=gha-rs-controller" \
    "$build_namespace listener-kubernetes-api mattercodex.dev/arc-role=listener" \
    "$deploy_namespace controller-kubernetes-api app.kubernetes.io/name=gha-rs-controller" \
    "$deploy_namespace listener-kubernetes-api mattercodex.dev/arc-role=listener" \
    "$deploy_namespace runner-kubernetes-api mattercodex.dev/arc-role=deploy-runner"; do
    read -r namespace policy selector <<<"$policy_spec"
    "$repository_root/tools/deploy/kubernetes-api-egress.sh" render --context "$expected_context" \
      --namespace "$namespace" --policy "$policy" --pod-selector "$selector" |
      kubectl --context "$expected_context" apply -f - >/dev/null
  done
  kubectl --context "$expected_context" -n "$build_namespace" rollout status \
    deployment/mattercodex-ci-egress-proxy --timeout=3m >/dev/null
  kubectl --context "$expected_context" -n "$deploy_namespace" rollout status \
    deployment/mattercodex-ci-egress-proxy --timeout=3m >/dev/null
  materialize_credential_secret "$build_namespace"
  materialize_credential_secret "$deploy_namespace"

  helm upgrade --install mattercodex-arc-build "$controller_chart" -n "$build_namespace" \
    -f "$script_directory/controller-values.yaml" --wait --timeout 5m >/dev/null
  helm upgrade --install mattercodex-build "$runner_chart" -n "$build_namespace" \
    -f "$script_directory/build-runner-values.yaml" --wait --timeout 5m >/dev/null
  helm upgrade --install mattercodex-arc-deploy "$controller_chart" -n "$deploy_namespace" \
    -f "$script_directory/controller-deploy-values.yaml" --wait --timeout 5m >/dev/null
  helm upgrade --install mattercodex-deploy "$runner_chart" -n "$deploy_namespace" \
    -f "$script_directory/deploy-runner-values.yaml" --wait --timeout 5m >/dev/null
fi

kubectl --context "$expected_context" diff -f "$rendered_network_policy" >/dev/null ||
  fail "static CI network policy readback mismatch"
for policy_spec in \
  "$build_namespace controller-kubernetes-api app.kubernetes.io/name=gha-rs-controller" \
  "$build_namespace listener-kubernetes-api mattercodex.dev/arc-role=listener" \
  "$deploy_namespace controller-kubernetes-api app.kubernetes.io/name=gha-rs-controller" \
  "$deploy_namespace listener-kubernetes-api mattercodex.dev/arc-role=listener" \
  "$deploy_namespace runner-kubernetes-api mattercodex.dev/arc-role=deploy-runner"; do
  read -r namespace policy selector <<<"$policy_spec"
  "$repository_root/tools/deploy/kubernetes-api-egress.sh" readback --context "$expected_context" \
    --namespace "$namespace" --policy "$policy" --pod-selector "$selector" >/dev/null
done

verify_credential_secret "$build_namespace"
verify_credential_secret "$deploy_namespace"
for controller_spec in \
  "$build_namespace mattercodex-arc-build-gha-rs-controller" \
  "$deploy_namespace mattercodex-arc-deploy-gha-rs-controller"; do
  read -r namespace controller_name <<<"$controller_spec"
  kubectl --context "$expected_context" -n "$namespace" rollout status \
    "deployment/$controller_name" --timeout=5m >/dev/null
done
for proxy_namespace in "$build_namespace" "$deploy_namespace"; do
  kubectl --context "$expected_context" -n "$proxy_namespace" rollout status \
    deployment/mattercodex-ci-egress-proxy --timeout=3m >/dev/null
done

readback_scale_set() {
  local namespace=$1 name=$2 group=$3 service_account=$4 automount=$5
  kubectl --context "$expected_context" -n "$namespace" get autoscalingrunnerset "$name" -o json |
    jq -e --arg name "$name" --arg group "$group" --arg service_account "$service_account" \
      --argjson automount "$automount" '
      .spec.githubConfigUrl == "https://github.com/codex-k8s/matter-codex" and
      .spec.runnerScaleSetName == $name and .spec.runnerGroup == $group and
      .spec.minRunners == 0 and .spec.maxRunners == 1 and
      .spec.template.spec.serviceAccountName == $service_account and
      .spec.template.spec.automountServiceAccountToken == $automount
    ' >/dev/null || fail "runner scale set readback mismatch: $name"
  kubectl --context "$expected_context" -n "$namespace" get autoscalinglistener -l \
    "actions.github.com/scale-set-name=$name" -o json |
    jq -e '.items | length == 1' >/dev/null || fail "listener resource is absent or non-unique: $name"
  kubectl --context "$expected_context" -n "$namespace" get pod -l \
    "mattercodex.dev/arc-role=listener,actions.github.com/scale-set-name=$name" -o json |
    jq -e '.items | length == 1 and all(.items[];
      .status.phase == "Running" and any(.status.conditions[]?; .type == "Ready" and .status == "True"))' >/dev/null ||
    fail "listener Pod is not uniquely Ready: $name"
  kubectl --context "$expected_context" -n "$namespace" get ephemeralrunnerset -l \
    "actions.github.com/scale-set-name=$name" -o json |
    jq -e '.items | length == 1 and all(.items[];
      .spec.replicas == 0 and .status.currentReplicas == 0 and (.status.failedEphemeralRunners // 0) == 0)' >/dev/null ||
    fail "ephemeral runner set idle readback mismatch: $name"
}

readback_scale_set "$build_namespace" mattercodex-build mattercodex-production-build \
  mattercodex-build-gha-rs-no-permission false
readback_scale_set "$deploy_namespace" mattercodex-deploy mattercodex-production-deploy \
  mattercodex-production-deployer true
kubectl --context "$expected_context" auth can-i get secrets -n "$deploy_namespace" \
  --as=system:serviceaccount:mattercodex-ci-deploy:mattercodex-production-deployer | grep -qx no ||
  fail "deploy runner unexpectedly has Secret read access"

negative_manifest="$temporary_directory/forbidden-pod.yaml"
printf '%s\n' \
  'apiVersion: v1' 'kind: Pod' 'metadata:' '  name: forbidden-ci-pod' \
  '  namespace: mattercodex-ci-deploy' '  labels:' '    mattercodex.dev/arc-role: deploy-runner' \
  'spec:' '  serviceAccountName: mattercodex-production-deployer' '  containers:' \
  '    - name: forbidden' '      image: docker.io/library/alpine:latest' >"$negative_manifest"
if kubectl --context "$expected_context" --as=system:admin --as-group=system:masters \
  apply --dry-run=server -f "$negative_manifest" >/dev/null 2>&1; then
  fail "negative CI admission check unexpectedly succeeded"
fi
printf 'ARC %s and exact operational readback completed\n' "$mode"
