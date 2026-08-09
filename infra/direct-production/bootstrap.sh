#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'Direct production bootstrap failed: %s\n' "$*" >&2; exit 1; }
usage() {
  printf 'Usage: %s --context <exact-context> --mode preflight|apply|readback [--application-material-file <path>]\n' "$0" >&2
}

expected_context=""
mode=""
application_material_file=""
while (($# > 0)); do
  case "$1" in
    --context) expected_context="${2:-}"; shift 2 ;;
    --mode) mode="${2:-}"; shift 2 ;;
    --application-material-file) application_material_file="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$expected_context" ]] || fail "exact Kubernetes context is required"
case "$mode" in preflight|apply|readback) ;; *) fail "mode must be preflight, apply or readback" ;; esac
for command_name in kubectl jq yq sha256sum; do
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

for manifest in "$script_directory/bootstrap.yaml" "$temporary_directory/foundation-owner.yaml" \
  "$temporary_directory/application-owner.yaml" "$workload_contracts" "$workload_policy"; do
  kubectl --context "$expected_context" apply --dry-run=client -f "$manifest" >/dev/null
done

expected_secrets=$( {
  yq eval-all -r '.. | select(has("secretKeyRef")) | .secretKeyRef.name' "$temporary_directory/application-interfaces.yaml"
  yq eval-all -r '.. | select(has("secret")) | select(.secret.secretName != null) | .secret.secretName' "$temporary_directory/application-interfaces.yaml"
} | sed '/^---$/d;/^null$/d;/^$/d' | LC_ALL=C sort -u )
expected_secret_keys=$(yq eval-all -r '.. | select(has("secretKeyRef")) |
  [.secretKeyRef.name,.secretKeyRef.key] | @tsv' "$temporary_directory/application-interfaces.yaml" |
  sed '/^---$/d;/^null$/d;/^$/d' | LC_ALL=C sort -u)
defined_configmaps="$temporary_directory/defined-configmaps"
referenced_configmaps="$temporary_directory/referenced-configmaps"
yq eval-all -r 'select(.kind == "ConfigMap") | .metadata.name' "$temporary_directory/application-interfaces.yaml" |
  sed '/^---$/d;/^null$/d;/^$/d' | LC_ALL=C sort -u >"$defined_configmaps"
{
  yq eval-all -r '.. | select(has("configMapRef")) | .configMapRef.name' "$temporary_directory/application-interfaces.yaml"
  yq eval-all -r '.. | select(has("configMapKeyRef")) | .configMapKeyRef.name' "$temporary_directory/application-interfaces.yaml"
  yq eval-all -r '.. | select(has("configMap")) | select(.configMap.name != null) | .configMap.name' "$temporary_directory/application-interfaces.yaml"
} | sed '/^---$/d;/^null$/d;/^$/d' | LC_ALL=C sort -u >"$referenced_configmaps"
expected_material_configmaps=$(comm -13 "$defined_configmaps" "$referenced_configmaps" |
  sed '/^kube-root-ca\.crt$/d;/^mattercodex-image-admission-policy$/d')
[[ -n "$expected_secrets" ]] || fail "application Secret interface set is empty"

if [[ -n "$application_material_file" ]]; then
  [[ -r "$application_material_file" ]] || fail "application material manifest is not readable"
  yq -o=json eval-all '.' "$application_material_file" | jq -es '
    length > 0 and all(.[]; type == "object" and .apiVersion == "v1" and
      .metadata.namespace == "mattercodex-system" and
      ((.kind == "Secret" and (.data | type == "object" and length > 0) and (.stringData == null)) or
       (.kind == "ConfigMap" and
        (((.data // {}) | type == "object") and ((.binaryData // {}) | type == "object") and
         ((((.data // {}) | length) + ((.binaryData // {}) | length)) > 0)))))
  ' >/dev/null || fail "application material manifest has an invalid resource"
  supplied_secrets=$(yq eval-all -r 'select(.kind == "Secret") | .metadata.name' "$application_material_file" | LC_ALL=C sort -u)
  supplied_configmaps=$(yq eval-all -r 'select(.kind == "ConfigMap") | .metadata.name' "$application_material_file" | LC_ALL=C sort -u)
  [[ "$supplied_secrets" == "$expected_secrets" ]] || fail "application material Secret name set mismatch"
  [[ "$supplied_configmaps" == "$expected_material_configmaps" ]] || fail "application material ConfigMap name set mismatch"
  while IFS=$'\t' read -r secret_name secret_key; do
    [[ -n "$secret_name" && -n "$secret_key" ]] || continue
    SECRET_NAME="$secret_name" SECRET_KEY="$secret_key" yq eval-all -e '
      select(.kind == "Secret" and .metadata.name == strenv(SECRET_NAME)) |
      .data[strenv(SECRET_KEY)] != null and .data[strenv(SECRET_KEY)] != ""
    ' "$application_material_file" >/dev/null || fail "application Secret key interface is absent"
  done <<<"$expected_secret_keys"
elif [[ "$mode" == apply ]]; then
  fail "application material manifest is required for owner-controlled apply"
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
  "$repository_root/tools/deploy/bootstrap-direct-production-secrets.sh" --context "$expected_context" --mode apply >/dev/null
  kubectl --context "$expected_context" apply -f "$application_material_file" >/dev/null
fi

kubectl --context "$expected_context" diff -f "$workload_contracts" >/dev/null ||
  fail "production workload contract readback mismatch"
kubectl --context "$expected_context" diff -f "$workload_policy" >/dev/null ||
  fail "production workload admission policy readback mismatch"

"$repository_root/tools/deploy/bootstrap-direct-production-secrets.sh" --context "$expected_context" --mode readback >/dev/null
while IFS= read -r secret_name; do
  [[ -n "$secret_name" ]] || continue
  kubectl --context "$expected_context" -n mattercodex-system get secret "$secret_name" -o json |
    jq -e '.data != null and (.data | length) > 0 and all(.data[]; length > 0)' >/dev/null ||
    fail "application Secret interface is absent or empty: $secret_name"
done <<<"$expected_secrets"
while IFS=$'\t' read -r secret_name secret_key; do
  [[ -n "$secret_name" && -n "$secret_key" ]] || continue
  kubectl --context "$expected_context" -n mattercodex-system get secret "$secret_name" -o json |
    jq -e --arg key "$secret_key" '.data[$key] != null and (.data[$key] | length) > 0' >/dev/null ||
    fail "application Secret key interface is absent"
done <<<"$expected_secret_keys"
while IFS= read -r configmap_name; do
  [[ -n "$configmap_name" ]] || continue
  kubectl --context "$expected_context" -n mattercodex-system get configmap "$configmap_name" -o json |
    jq -e '((((.data // {}) | length) + ((.binaryData // {}) | length)) > 0) and
      all((.data // {})[]; length > 0) and all((.binaryData // {})[]; length > 0)' >/dev/null ||
    fail "application ConfigMap material interface is absent or empty"
done <<<"$expected_material_configmaps"

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
kubectl --context "$expected_context" auth can-i get secrets -n mattercodex-system \
  --as=system:serviceaccount:mattercodex-ci-deploy:mattercodex-production-deployer | grep -qx no ||
  fail "routine deployer unexpectedly has Secret read access"

bootstrap_digest=$(sha256sum "$script_directory/bootstrap.yaml" "$temporary_directory/foundation-owner.yaml" \
  "$temporary_directory/application-owner.yaml" "$workload_contracts" "$workload_policy" |
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
