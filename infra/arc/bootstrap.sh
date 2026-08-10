#!/usr/bin/env bash
set -euo pipefail
umask 077

fail() { printf 'ARC bootstrap failed: %s\n' "$*" >&2; exit 1; }
usage() {
  printf 'Usage: %s --context <exact-context> --mode preflight|apply|readback --workflow-sha-file <path> --build-owner-actor-id-file <path> --deploy-owner-actor-id-file <path> [--github-pat-file <path> | --github-app-id-file <path> --github-app-installation-id-file <path> --github-app-private-key-file <path>]\n' "$0" >&2
}

expected_context=""
mode=""
github_app_id_file=""
github_app_installation_id_file=""
github_app_private_key_file=""
github_pat_file=""
workflow_sha_file=""
build_owner_actor_file=""
deploy_owner_actor_file=""
while (($# > 0)); do
  case "$1" in
    --context) expected_context="${2:-}"; shift 2 ;;
    --mode) mode="${2:-}"; shift 2 ;;
    --github-app-id-file) github_app_id_file="${2:-}"; shift 2 ;;
    --github-app-installation-id-file) github_app_installation_id_file="${2:-}"; shift 2 ;;
    --github-app-private-key-file) github_app_private_key_file="${2:-}"; shift 2 ;;
    --github-pat-file) github_pat_file="${2:-}"; shift 2 ;;
    --workflow-sha-file) workflow_sha_file="${2:-}"; shift 2 ;;
    --build-owner-actor-id-file) build_owner_actor_file="${2:-}"; shift 2 ;;
    --deploy-owner-actor-id-file) deploy_owner_actor_file="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

github_app_argument_count=0
for github_app_file in "$github_app_id_file" "$github_app_installation_id_file" "$github_app_private_key_file"; do
  [[ -z "$github_app_file" ]] || github_app_argument_count=$((github_app_argument_count + 1))
done
((github_app_argument_count == 0 || github_app_argument_count == 3)) ||
  fail "GitHub App credential mode requires exactly three files"
if [[ -n "$github_pat_file" ]] && ((github_app_argument_count != 0)); then
  fail "GitHub App and PAT credential modes are mutually exclusive"
fi
credential_mode=none
if [[ -n "$github_pat_file" ]]; then
  credential_mode=pat
elif ((github_app_argument_count == 3)); then
  credential_mode=app
fi

[[ -n "$expected_context" ]] || fail "exact Kubernetes context is required"
case "$mode" in preflight|apply|readback) ;; *) fail "mode must be preflight, apply or readback" ;; esac
[[ -r "$workflow_sha_file" && -r "$build_owner_actor_file" && -r "$deploy_owner_actor_file" ]] ||
  fail "GitHub owner policy files are required before ARC"
for command_name in kubectl helm sha256sum awk grep jq yq rg stat curl; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$(kubectl config current-context)" == "$expected_context" ]] || fail "Kubernetes context mismatch"

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repository_root=$(cd -- "$script_directory/../.." && pwd -P)
"$repository_root/infra/github/bootstrap-actions-policy.sh" --mode readback \
  --workflow-sha-file "$workflow_sha_file" \
  --build-owner-actor-id-file "$build_owner_actor_file" \
  --deploy-owner-actor-id-file "$deploy_owner_actor_file" >/dev/null
lock_file="$script_directory/chart.lock"
credential_secret=mattercodex-github-runner-auth
owner_gate_config=mattercodex-runner-owner-gate
build_namespace=mattercodex-ci
deploy_namespace=mattercodex-ci-deploy
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT

kubernetes_api_service_ip=$(kubectl --context "$expected_context" -n default get service kubernetes \
  -o json | jq -er '.spec.clusterIP | select(type == "string" and length > 0 and . != "None")')
rendered_network_policy="$temporary_directory/network-policy.yaml"
"$script_directory/render-network-policy.sh" "$rendered_network_policy"
egress_proxy_config_sha256=$(yq -r '
  select(.kind == "ConfigMap" and .metadata.namespace == "mattercodex-ci" and
    .metadata.name == "mattercodex-ci-egress-proxy") | .data."envoy.yaml"
' "$rendered_network_policy" | sha256sum | awk '{print $1}')
[[ "$egress_proxy_config_sha256" =~ ^[a-f0-9]{64}$ ]] ||
  fail "rendered egress proxy config SHA-256 is invalid"
rendered_values_directory="$temporary_directory/helm-values"
mkdir -p -- "$rendered_values_directory"
"$script_directory/render-helm-values.sh" "$kubernetes_api_service_ip" \
  "$egress_proxy_config_sha256" "$rendered_values_directory" >/dev/null
controller_values="$rendered_values_directory/controller-values.yaml"
controller_deploy_values="$rendered_values_directory/controller-deploy-values.yaml"
build_runner_values="$rendered_values_directory/build-runner-values.yaml"
deploy_runner_values="$rendered_values_directory/deploy-runner-values.yaml"
kubernetes_api_no_proxy=$(yq -er '.env[] | select(.name == "NO_PROXY") | .value' \
  "$controller_values")

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

validate_github_pat_file() {
  local file_mode pat curl_config
  [[ -r "$github_pat_file" && -s "$github_pat_file" ]] ||
    fail "GitHub PAT credential file is required to create runner auth Secrets"
  file_mode=$(stat -c '%a' "$github_pat_file")
  (( (8#$file_mode & 077) == 0 )) || fail "GitHub PAT credential file permissions are too broad"
  awk 'NR != 1 || length($0) == 0 || $0 ~ /[[:space:]]/ {exit 1} END {if (NR != 1) exit 1}' \
    "$github_pat_file" || fail "GitHub PAT credential file is invalid"
  curl_config="$temporary_directory/github-pat-curl.conf"
  pat=$(<"$github_pat_file")
  printf 'header = "Authorization: Bearer %s"\nheader = "Accept: application/vnd.github+json"\nheader = "X-GitHub-Api-Version: 2022-11-28"\n' \
    "$pat" >"$curl_config"
  unset pat
  curl --config "$curl_config" --fail --silent --show-error \
    https://api.github.com/repos/codex-k8s/matter-codex >"$temporary_directory/github-repository.json"
  jq -e '.permissions.admin == true' "$temporary_directory/github-repository.json" >/dev/null ||
    fail "GitHub PAT lacks repository administration"
  curl --config "$curl_config" --fail --silent --show-error \
    'https://api.github.com/repos/codex-k8s/matter-codex/actions/runners?per_page=1' \
    >"$temporary_directory/github-runners.json"
  jq -e '.total_count | type == "number"' "$temporary_directory/github-runners.json" >/dev/null ||
    fail "GitHub PAT cannot read the repository runners API"
}

verify_credential_secret() {
  local namespace=$1 keys
  keys=$(kubectl --context "$expected_context" -n "$namespace" get secret "$credential_secret" -o json |
    jq -c '[.data | keys[]] | sort')
  case "$credential_mode:$keys" in
    pat:'["github_token"]'|app:'["github_app_id","github_app_installation_id","github_app_private_key"]'|none:'["github_token"]'|none:'["github_app_id","github_app_installation_id","github_app_private_key"]') ;;
    *) fail "runner auth Secret has an unexpected credential mode or key set in $namespace" ;;
  esac
  kubectl --context "$expected_context" -n "$namespace" get secret "$credential_secret" -o json |
    jq -e '.data | length > 0 and all(.[]; type == "string" and length > 0)' >/dev/null ||
    fail "runner auth Secret has empty credential data in $namespace"
}

materialize_credential_secret() {
  local namespace=$1
  if kubectl --context "$expected_context" -n "$namespace" get secret "$credential_secret" >/dev/null 2>&1; then
    verify_credential_secret "$namespace"
    return
  fi
  case "$credential_mode" in
    pat)
      validate_github_pat_file
      kubectl --context "$expected_context" -n "$namespace" create secret generic "$credential_secret" \
        --from-file=github_token="$github_pat_file" >/dev/null
      ;;
    app)
      validate_github_app_files
      kubectl --context "$expected_context" -n "$namespace" create secret generic "$credential_secret" \
        --from-file=github_app_id="$github_app_id_file" \
        --from-file=github_app_installation_id="$github_app_installation_id_file" \
        --from-file=github_app_private_key="$github_app_private_key_file" >/dev/null
      ;;
    *) fail "GitHub App or PAT credential files are required when runner auth Secret is absent" ;;
  esac
  verify_credential_secret "$namespace"
}

case "$credential_mode" in
  pat) validate_github_pat_file ;;
  app) validate_github_app_files ;;
esac

render_owner_gate_config() {
  local namespace=$1 workflow_path=$2 job=$3 owner_actor_file=$4 output=$5 gate_directory
  gate_directory="$temporary_directory/owner-gate-$namespace"
  mkdir -p -- "$gate_directory"
  printf 'codex-k8s/matter-codex/%s@refs/heads/main\n' "$workflow_path" \
    >"$gate_directory/expected-workflow-ref"
  cp -- "$workflow_sha_file" "$gate_directory/expected-workflow-sha"
  cp -- "$owner_actor_file" "$gate_directory/expected-owner-actor-id"
  printf '%s\n' "$job" >"$gate_directory/expected-job"
  kubectl --context "$expected_context" -n "$namespace" create configmap "$owner_gate_config" \
    --from-file=job-started.sh="$script_directory/job-started-owner-gate.sh" \
    --from-file=expected-workflow-ref="$gate_directory/expected-workflow-ref" \
    --from-file=expected-workflow-sha="$gate_directory/expected-workflow-sha" \
    --from-file=expected-owner-actor-id="$gate_directory/expected-owner-actor-id" \
    --from-file=expected-job="$gate_directory/expected-job" \
    --dry-run=client -o yaml >"$output"
}

build_owner_gate="$temporary_directory/build-owner-gate.yaml"
deploy_owner_gate="$temporary_directory/deploy-owner-gate.yaml"
render_owner_gate_config "$build_namespace" .github/workflows/build-release.yml build \
  "$build_owner_actor_file" "$build_owner_gate"
render_owner_gate_config "$deploy_namespace" .github/workflows/deploy-production.yml deploy \
  "$deploy_owner_actor_file" "$deploy_owner_gate"

controller_chart=$(pull_chart gha-runner-scale-set-controller)
runner_chart=$(pull_chart gha-runner-scale-set)
server_minor=$(kubectl --context "$expected_context" version -o json |
  jq -r '.serverVersion.minor | sub("[^0-9].*$"; "") | tonumber')
((server_minor >= 33)) || fail "Kubernetes 1.33 or newer is required for stable Pod user namespaces"

kubectl --context "$expected_context" apply --dry-run=client -f "$script_directory/namespace-rbac.yaml" >/dev/null
kubectl --context "$expected_context" apply --dry-run=client -f "$rendered_network_policy" >/dev/null
kubectl --context "$expected_context" apply --dry-run=client -f "$build_owner_gate" >/dev/null
kubectl --context "$expected_context" apply --dry-run=client -f "$deploy_owner_gate" >/dev/null
helm template mattercodex-arc-build "$controller_chart" -n "$build_namespace" \
  -f "$controller_values" >/dev/null
helm template mattercodex-build "$runner_chart" -n "$build_namespace" \
  -f "$build_runner_values" >/dev/null
helm template mattercodex-arc-deploy "$controller_chart" -n "$deploy_namespace" \
  -f "$controller_deploy_values" >/dev/null
helm template mattercodex-deploy "$runner_chart" -n "$deploy_namespace" \
  -f "$deploy_runner_values" >/dev/null

if [[ "$mode" == preflight ]]; then
  printf 'ARC preflight completed\n'
  exit 0
fi

if [[ "$mode" == apply ]]; then
  kubectl --context "$expected_context" apply -f "$script_directory/namespace-rbac.yaml" >/dev/null
  kubectl --context "$expected_context" apply -f "$rendered_network_policy" >/dev/null
  kubectl --context "$expected_context" apply -f "$build_owner_gate" >/dev/null
  kubectl --context "$expected_context" apply -f "$deploy_owner_gate" >/dev/null
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
    -f "$controller_values" --wait --timeout 5m >/dev/null
  helm upgrade --install mattercodex-build "$runner_chart" -n "$build_namespace" \
    -f "$build_runner_values" --wait --timeout 5m >/dev/null
  helm upgrade --install mattercodex-arc-deploy "$controller_chart" -n "$deploy_namespace" \
    -f "$controller_deploy_values" --wait --timeout 5m >/dev/null
  helm upgrade --install mattercodex-deploy "$runner_chart" -n "$deploy_namespace" \
    -f "$deploy_runner_values" --wait --timeout 5m >/dev/null
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
  kubectl --context "$expected_context" -n "$namespace" get deployment "$controller_name" -o json |
    jq -e --arg no_proxy "$kubernetes_api_no_proxy" \
      --arg proxy_sha "$egress_proxy_config_sha256" '
      (.spec.template.metadata.annotations."mattercodex.dev/egress-proxy-config-sha256" ==
        $proxy_sha) and
      ([.spec.template.spec.containers[].env[]? |
        select(.name == "NO_PROXY" and .value == $no_proxy)] | length == 1)
    ' >/dev/null || fail "controller Kubernetes API NO_PROXY readback mismatch: $controller_name"
done
for proxy_namespace in "$build_namespace" "$deploy_namespace"; do
  kubectl --context "$expected_context" -n "$proxy_namespace" rollout status \
    deployment/mattercodex-ci-egress-proxy --timeout=3m >/dev/null
done

verify_owner_gate_config() {
  local namespace=$1 expected=$2 expected_data actual_data
  expected_data=$(yq -o=json '.data' "$expected" | jq -Sc .)
  actual_data=$(kubectl --context "$expected_context" -n "$namespace" get configmap \
    "$owner_gate_config" -o json | jq -Sc '.data')
  [[ "$actual_data" == "$expected_data" ]] ||
    fail "runner owner gate ConfigMap readback mismatch in $namespace"
}

verify_owner_gate_config "$build_namespace" "$build_owner_gate"
verify_owner_gate_config "$deploy_namespace" "$deploy_owner_gate"

readback_scale_set() {
  local namespace=$1 name=$2 service_account=$3 automount=$4 runner_no_proxy=$5
  kubectl --context "$expected_context" -n "$namespace" get autoscalingrunnerset "$name" -o json |
    jq -e --arg name "$name" --arg service_account "$service_account" \
      --argjson automount "$automount" --arg runner_no_proxy "$runner_no_proxy" '
      .spec.githubConfigUrl == "https://github.com/codex-k8s/matter-codex" and
      .spec.runnerScaleSetName == $name and (.spec | has("runnerGroup") | not) and
      .spec.minRunners == 0 and .spec.maxRunners == 1 and
      .spec.template.spec.serviceAccountName == $service_account and
      .spec.template.spec.automountServiceAccountToken == $automount and
      any(.spec.template.spec.volumes[];
        .name == "owner-gate" and .configMap.name == "mattercodex-runner-owner-gate" and
        .configMap.defaultMode == 365) and
      any(.spec.template.spec.containers[]; .name == "runner" and
        any(.env[]; .name == "NO_PROXY" and .value == $runner_no_proxy) and
        any(.env[]; .name == "ACTIONS_RUNNER_HOOK_JOB_STARTED" and
          .value == "/var/run/mattercodex-owner-gate/job-started.sh") and
        any(.volumeMounts[]; .name == "owner-gate" and
          .mountPath == "/var/run/mattercodex-owner-gate" and .readOnly == true))
    ' >/dev/null || fail "runner scale set readback mismatch: $name"
  kubectl --context "$expected_context" -n "$namespace" get autoscalinglistener -l \
    "actions.github.com/scale-set-name=$name" -o json |
    jq -e '.items | length == 1' >/dev/null || fail "listener resource is absent or non-unique: $name"
  kubectl --context "$expected_context" -n "$namespace" get pod -l \
    "mattercodex.dev/arc-role=listener,actions.github.com/scale-set-name=$name" -o json |
    jq -e --arg no_proxy "$kubernetes_api_no_proxy" '.items as $items |
      ($items | length) == 1 and all($items[];
      .status.phase == "Running" and
      any(.status.conditions[]?; .type == "Ready" and .status == "True") and
      any(.spec.containers[]; any(.env[]?; .name == "NO_PROXY" and .value == $no_proxy)))' \
      >/dev/null ||
    fail "listener Pod is not uniquely Ready: $name"
  kubectl --context "$expected_context" -n "$namespace" get ephemeralrunnerset -l \
    "actions.github.com/scale-set-name=$name" -o json |
    jq -e '.items as $items | ($items | length) == 1 and all($items[];
      .spec.replicas == 0 and .status.currentReplicas == 0 and (.status.failedEphemeralRunners // 0) == 0)' >/dev/null ||
    fail "ephemeral runner set idle readback mismatch: $name"
}

readback_scale_set "$build_namespace" mattercodex-build \
  mattercodex-build-gha-rs-no-permission false \
  matter-codex-registry.matter-kodex-prod.svc.cluster.local,localhost,127.0.0.1
readback_scale_set "$deploy_namespace" mattercodex-deploy \
  mattercodex-production-deployer true "$kubernetes_api_no_proxy"
"$repository_root/infra/github/bootstrap-actions-policy.sh" --mode readback \
  --workflow-sha-file "$workflow_sha_file" \
  --build-owner-actor-id-file "$build_owner_actor_file" \
  --deploy-owner-actor-id-file "$deploy_owner_actor_file" >/dev/null
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
