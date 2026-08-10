#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'Direct production bootstrap failed: %s\n' "$*" >&2; exit 1; }
require_denied() {
  local failure_message=$1 output status
  shift
  set +e
  output=$("$@")
  status=$?
  set -e
  [[ $status -eq 1 && "$output" == no ]] || fail "$failure_message"
}
require_no_diff_except_generation() {
  local failure_message=$1 output status meaningful
  shift
  set +e
  output=$("$@" 2>&1)
  status=$?
  set -e
  [[ $status -eq 0 ]] && return
  [[ $status -eq 1 ]] || fail "$failure_message"
  meaningful=$(printf '%s\n' "$output" | awk '
    /^--- / || /^\+\+\+ / || /^@@ / { next }
    /^[+-][[:space:]]+generation:[[:space:]]+[0-9]+$/ { next }
    /^[+-]/ { print }
  ')
  [[ -z "$meaningful" ]] || fail "$failure_message"
}
usage() {
  printf 'Usage: %s --context <exact-context> --mode preflight|apply|readback [--external-material-file <path>]\n' "$0" >&2
}

expected_context=""
mode=""
external_material_file=""
while (($# > 0)); do
  case "$1" in
    --context) expected_context="${2:-}"; shift 2 ;;
    --mode) mode="${2:-}"; shift 2 ;;
    --external-material-file) external_material_file="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$expected_context" ]] || fail "exact Kubernetes context is required"
case "$mode" in preflight|apply|readback) ;; *) fail "mode must be preflight, apply or readback" ;; esac
[[ "$mode" == readback || -r "$external_material_file" ]] || fail "external material file is required"
for command_name in kubectl jq rg yq sha256sum; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$(kubectl config current-context)" == "$expected_context" ]] || fail "Kubernetes context mismatch"

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repository_root=$(cd -- "$script_directory/../.." && pwd -P)
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
umask 077

for permission in \
  'create namespaces' 'create validatingadmissionpolicies.admissionregistration.k8s.io' \
  'create validatingadmissionpolicybindings.admissionregistration.k8s.io' \
  'create clusterroles.rbac.authorization.k8s.io' 'create clusterrolebindings.rbac.authorization.k8s.io'; do
  read -r verb resource <<<"$permission"
  kubectl --context "$expected_context" auth can-i "$verb" "$resource" --all-namespaces | grep -qx yes ||
    fail "bootstrap identity cannot $verb $resource"
done
kubectl --context "$expected_context" api-resources --api-group=cert-manager.io | grep -q '^certificates' ||
  fail "cert-manager API is required before bootstrap"

kubectl kustomize "$repository_root/deploy/k8s/base/direct-production-foundation" |
  yq eval-all 'select(.kind == "ResourceQuota" or .kind == "LimitRange" or .kind == "Issuer" or
    .kind == "Certificate" or .kind == "NetworkPolicy")' >"$temporary_directory/foundation-owner.yaml"
"$repository_root/tools/release/render-direct-production-applications.sh" \
  --scope bootstrap --output "$temporary_directory/application-owner.yaml" >/dev/null
"$repository_root/tools/release/render-direct-production-applications.sh" \
  --scope interfaces --output "$temporary_directory/application-interfaces.yaml" >/dev/null
kubernetes_api_policies="$temporary_directory/runtime-adapter-kubernetes-api-egress.yaml"
: >"$kubernetes_api_policies"
for binding in \
  integration-gateway-kubernetes-api-exact:integration-gateway \
  interaction-gateway-kubernetes-api-exact:interaction-gateway; do
  policy_name=${binding%%:*}
  selector_value=${binding#*:}
  "$repository_root/tools/deploy/kubernetes-api-egress.sh" render \
    --context "$expected_context" --namespace mattercodex-system \
    --policy "$policy_name" --pod-selector "mattercodex.dev/runtime-secret-api=$selector_value" \
    >>"$kubernetes_api_policies"
  printf '%s\n' '---' >>"$kubernetes_api_policies"
done
contract_source="$temporary_directory/workload-contract-source.yaml"
contract_lock="$temporary_directory/workload-contract-release-lock.json"
contract_source_sha=1111111111111111111111111111111111111111
contract_digest=sha256:2222222222222222222222222222222222222222222222222222222222222222
jq -S --arg source_sha "$contract_source_sha" --arg digest "$contract_digest" '
  {schema_version:1,profile:"direct-production single-node prototype",source_sha:$source_sha,
   build_run_id:"local",registry_push:"matter-codex-registry.matter-kodex-prod.svc.cluster.local:5000",
   node_pull:"localhost:5001",images:[.images[]|{component,repository:("mattercodex/"+.component),
     digest:$digest,pull_ref:("localhost:5001/mattercodex/"+.component+"@"+$digest)}]}
' "$repository_root/tools/release/images.json" >"$contract_lock"
contract_lock_sha256=$(sha256sum "$contract_lock" | awk '{print $1}')
"$repository_root/tools/release/render-direct-production.sh" \
  --lock "$contract_lock" --source-sha "$contract_source_sha" \
  --sha256 "$contract_lock_sha256" --output "$contract_source" >/dev/null
workload_contracts="$temporary_directory/workload-contracts.yaml"
"$repository_root/tools/release/render-production-workload-contracts.sh" \
  --manifest "$contract_source" --output "$workload_contracts" >/dev/null
workload_policy="$script_directory/workload-policy.yaml"

yq -o=json eval-all '.' "$temporary_directory/application-interfaces.yaml" | jq -s -e '
  map(select(.kind != null)) as $resources |
  first($resources[] | select(.kind == "ConfigMap" and .metadata.name == "control-plane-runtime")) as $control |
  first($resources[] | select(.kind == "ConfigMap" and .metadata.name == "runtime-controller-runtime")) as $runtime |
  ($control.data.CONTROL_PLANE_RUNTIME_ARCHIVE_RESTORE_CAPABILITY == "disabled") and
  ($control.data.CONTROL_PLANE_RUNTIME_ARCHIVE_SIGNING_KEY_FILE == null) and
  ($control.data.CONTROL_PLANE_RUNTIME_RESTORE_SIGNING_KEY_FILE == null) and
  ($runtime.data.RUNTIME_ARCHIVE_RESTORE_CAPABILITY == "disabled") and
  ($runtime.data.RUNTIME_ARCHIVE_RESTORE_FOLLOW_UP_ISSUE == "https://github.com/codex-k8s/matter-codex/issues/310") and
  ([$resources[] | select(.metadata.name | test("^(runtime-(archive|restore-verifier|rehydrate)(-|$)|runtime-s3-(archive|restore)(-|$)|runtime-s3-(exchanger|readback)-|runtime-controller-(archive-workers-s3|s3-security-policy)$)"))] | length) == 0
' >/dev/null || fail "runtime archive/restore capability is not disabled in the exact application render"
yq -o=json eval-all '.' "$temporary_directory/application-owner.yaml" | jq -s -e '
  map(select(.kind != null)) as $resources |
  ([$resources[] | select(.metadata.name | test("^(runtime-(archive|restore-verifier|rehydrate)(-|$)|runtime-s3-(archive|restore)(-|$)|runtime-s3-(exchanger|readback)-|runtime-controller-(archive-workers-s3|s3-security-policy)$)"))] | length) == 0 and
  (first($resources[] | select(.kind == "NetworkPolicy" and .metadata.name == "runtime-controller-workers-exact-paths")) |
    .spec.podSelector.matchExpressions[0].values == ["runtime-cleanup-authorizer"])
' >/dev/null || fail "runtime archive/restore owner resources remain in the exact bootstrap render"

for manifest in "$script_directory/bootstrap.yaml" "$temporary_directory/foundation-owner.yaml" \
  "$temporary_directory/application-owner.yaml" "$kubernetes_api_policies" "$workload_contracts" "$workload_policy"; do
  kubectl --context "$expected_context" apply --dry-run=client -f "$manifest" >/dev/null
done

admission_policies="$temporary_directory/validating-admission-policies.yaml"
yq eval-all 'select(.kind == "ValidatingAdmissionPolicy")' \
  "$script_directory/bootstrap.yaml" "$temporary_directory/foundation-owner.yaml" \
  "$temporary_directory/application-owner.yaml" "$kubernetes_api_policies" \
  "$workload_contracts" "$workload_policy" >"$admission_policies"
kubectl --context "$expected_context" apply --dry-run=server -f "$admission_policies" >/dev/null ||
  fail "production validating admission policies do not compile"

materializer="$repository_root/tools/deploy/materialize-direct-production-application.sh"
if [[ "$mode" != readback ]]; then
  "$materializer" --mode render --external-material-file "$external_material_file" \
    --output "$temporary_directory/application-material.yaml" >/dev/null
  kubectl --context "$expected_context" apply --dry-run=client \
    -f "$temporary_directory/application-material.yaml" >/dev/null
  yq -o=json eval-all '.' "$temporary_directory/application-interfaces.yaml" | jq -s -e '
    [ .[] | select(.kind == "Deployment" or .kind == "DaemonSet" or .kind == "Job") |
      ((.spec.template.spec.containers // []) + (.spec.template.spec.initContainers // []))[] |
      select(.name == "publisher" or .name == "reconciler" or
        .name == "internal-rpc-authority-issuer" or
        .name == "internal-rpc-authority-verifier")
    ] as $authority |
    ($authority | length) > 0 and all($authority[];
      ([.env[]? | select(.name == "INTERNAL_RPC_AUTHORITY_SECRET_BACKEND")] | length) == 1 and
      ([.env[]? | select(.name == "INTERNAL_RPC_AUTHORITY_SECRET_BACKEND")][0] |
        .value == "direct-production-kubernetes-file" and .valueFrom == null) and
      ([.env[]? | select(.name == "INTERNAL_RPC_AUTHORITY_DEPLOYMENT_PROFILE")] | length) == 1 and
      ([.env[]? | select(.name == "INTERNAL_RPC_AUTHORITY_DEPLOYMENT_PROFILE")][0] |
        .value == null and
        .valueFrom.fieldRef.fieldPath == "metadata.labels['"'"'mattercodex.dev/profile'"'"']"))
  ' >/dev/null || fail "internal-rpc-authority prototype backend binding is invalid"
  if yq -o=json eval-all '.' "$temporary_directory/application-interfaces.yaml" | jq -s -e '
    any(.[];
      ((.kind == "Deployment" or .kind == "DaemonSet" or .kind == "Job") and
       any(((.spec.template.spec.containers // []) +
            (.spec.template.spec.initContainers // []))[]?.env[]?;
         .name == "INTEGRATION_GATEWAY_VAULT_ADDRESS" or
         .name == "INTERACTION_GATEWAY_BOT_CREDENTIAL_VAULT_ADDRESS" or
         .name == "RUNTIME_VAULT_ADDRESS")) or
      (.kind == "ConfigMap" and any((.data // {}) | keys[];
        . == "INTEGRATION_GATEWAY_VAULT_ADDRESS" or
        . == "INTERACTION_GATEWAY_BOT_CREDENTIAL_VAULT_ADDRESS" or
        . == "RUNTIME_VAULT_ADDRESS")))
  ' >/dev/null; then
    kubectl --context "$expected_context" -n mattercodex-system get service vault -o json |
      jq -e '.spec.ports == [{"name":"https","port":8200,"protocol":"TCP","targetPort":8200}]' >/dev/null ||
      fail "required exact Vault Service binding is absent"
    kubectl --context "$expected_context" -n mattercodex-system get endpointslice \
      -l kubernetes.io/service-name=vault -o json |
      jq -e '(.items | length) > 0 and any(.items[];
        any(.ports[]?; .name == "https" and .port == 8200 and .protocol == "TCP") and
        any(.endpoints[]?; .conditions.ready == true))' >/dev/null ||
      fail "required exact Vault endpoint binding is absent"
  fi
fi

if [[ "$mode" == preflight ]]; then
  printf 'Direct production bootstrap preflight completed\n'
  exit 0
fi

if [[ "$mode" == apply ]]; then
  kubectl --context "$expected_context" apply -f "$script_directory/bootstrap.yaml" >/dev/null
  kubectl --context "$expected_context" apply -f "$workload_contracts" >/dev/null
  kubectl --context "$expected_context" apply -f "$workload_policy" >/dev/null
  kubectl --context "$expected_context" apply -f "$temporary_directory/foundation-owner.yaml" >/dev/null
  kubectl --context "$expected_context" apply -f "$temporary_directory/application-owner.yaml" >/dev/null
  kubectl --context "$expected_context" apply -f "$kubernetes_api_policies" >/dev/null
  "$repository_root/tools/deploy/bootstrap-direct-production-secrets.sh" --context "$expected_context" --mode apply >/dev/null
  for certificate_name in mattercodex-prototype-ca mattercodex-legacy-mattermost-bridge mattercodex-legacy-bot-service-bridge; do
    kubectl --context "$expected_context" -n mattercodex-system wait --for=condition=Ready \
      "certificate/$certificate_name" --timeout=3m >/dev/null
  done
  "$materializer" --mode apply --context "$expected_context" \
    --external-material-file "$external_material_file" >/dev/null
fi

kubectl --context "$expected_context" diff -f "$workload_contracts" >/dev/null ||
  fail "production workload contract readback mismatch"
kubectl --context "$expected_context" diff -f "$script_directory/bootstrap.yaml" >/dev/null ||
  fail "production owner admission policy readback mismatch"
require_no_diff_except_generation "production application owner policy readback mismatch" \
  kubectl --context "$expected_context" diff -f "$temporary_directory/application-owner.yaml"
kubectl --context "$expected_context" diff -f "$workload_policy" >/dev/null ||
  fail "production workload admission policy readback mismatch"
kubectl --context "$expected_context" diff -f "$kubernetes_api_policies" >/dev/null ||
  fail "runtime adapter Kubernetes API egress readback mismatch"

"$repository_root/tools/deploy/bootstrap-direct-production-secrets.sh" --context "$expected_context" --mode readback >/dev/null
"$materializer" --mode readback --context "$expected_context" >/dev/null

while IFS= read -r certificate_name; do
  [[ -n "$certificate_name" && "$certificate_name" != '---' ]] || continue
  kubectl --context "$expected_context" -n mattercodex-system wait --for=condition=Ready \
    "certificate/$certificate_name" --timeout=3m >/dev/null
done < <(yq eval-all -r 'select(.kind == "Certificate") | .metadata.name' "$temporary_directory/foundation-owner.yaml")

positive_manifest="$temporary_directory/positive.yaml"
printf '%s\n' \
  'apiVersion: v1' 'kind: ConfigMap' 'metadata:' '  name: mattercodex-release-lock' \
  '  namespace: mattercodex-system' '  labels:' \
  '    mattercodex.dev/profile: direct-production-single-node-prototype' \
  '    mattercodex.dev/release-managed: "true"' >"$positive_manifest"
kubectl --context "$expected_context" --as=system:serviceaccount:mattercodex-ci-deploy:mattercodex-production-deployer \
  apply --dry-run=server -f "$positive_manifest" >/dev/null || fail "positive deploy admission check failed"
negative_manifest="$temporary_directory/negative.yaml"
sed 's/name: mattercodex-release-lock/name: forbidden-production-resource/' "$positive_manifest" >"$negative_manifest"
if kubectl --context "$expected_context" --as=system:serviceaccount:mattercodex-ci-deploy:mattercodex-production-deployer \
  apply --dry-run=server -f "$negative_manifest" >/dev/null 2>&1; then
  fail "negative deploy admission check unexpectedly succeeded"
fi
require_denied "routine deployer unexpectedly has Secret read access" \
  kubectl --context "$expected_context" auth can-i get secrets -n mattercodex-system \
  --as=system:serviceaccount:mattercodex-ci-deploy:mattercodex-production-deployer

bootstrap_digest=$(sha256sum "$script_directory/bootstrap.yaml" "$temporary_directory/foundation-owner.yaml" \
  "$temporary_directory/application-owner.yaml" "$kubernetes_api_policies" "$workload_contracts" "$workload_policy" |
  sha256sum | awk '{print $1}')
readiness_manifest="$temporary_directory/readiness.yaml"
printf '%s\n' \
  'apiVersion: v1' 'kind: ConfigMap' 'metadata:' '  name: mattercodex-bootstrap-readiness' \
  '  namespace: mattercodex-system' '  labels:' \
  '    mattercodex.dev/profile: direct-production-single-node-prototype' \
  'data:' '  status: ready' \
  '  profile: "direct-production single-node prototype"' \
  "  bootstrap_sha256: \"$bootstrap_digest\"" >"$readiness_manifest"
kubectl --context "$expected_context" apply -f "$readiness_manifest" >/dev/null
kubectl --context "$expected_context" -n mattercodex-system get configmap mattercodex-bootstrap-readiness -o json |
  jq -e --arg digest "$bootstrap_digest" '.data.status == "ready" and .data.bootstrap_sha256 == $digest' >/dev/null ||
  fail "bootstrap readiness readback mismatch"
printf 'Direct production bootstrap %s completed\n' "$mode"
