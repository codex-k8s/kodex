#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Legacy installation reset failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 --context <exact-context> --mode preflight|apply|readback" \
    '  [--confirm DELETE-LEGACY-KODEX-AND-MYQRCONTACT] [--wipe-bootstrap-registry]' >&2
}

expected_context=""
mode=""
confirmation=""
wipe_registry=false
while (($# > 0)); do
  case "$1" in
    --context) expected_context="${2:-}"; shift 2 ;;
    --mode) mode="${2:-}"; shift 2 ;;
    --confirm) confirmation="${2:-}"; shift 2 ;;
    --wipe-bootstrap-registry) wipe_registry=true; shift ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$expected_context" ]] || fail 'exact context is required'
[[ "$mode" == preflight || "$mode" == apply || "$mode" == readback ]] || fail 'mode is invalid'
for command_name in jq kubectl; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$(kubectl config current-context)" == "$expected_context" ]] || fail 'current Kubernetes context mismatch'

legacy_namespace=kodex-system
myqr_namespace=myqrcontact
shared_namespace=matter-kodex-prod
repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
release_profile_selector='kodex.dev/profile in (direct-production-single-node-prototype,web-only,web-with-mattermost)'
release_cluster_resource_kinds=(
  customresourcedefinitions.apiextensions.k8s.io
  validatingadmissionpolicies.admissionregistration.k8s.io
  validatingadmissionpolicybindings.admissionregistration.k8s.io
  clusterroles.rbac.authorization.k8s.io
  clusterrolebindings.rbac.authorization.k8s.io
  bundles.trust.cert-manager.io
)

delete_release_cluster_scope() {
  local resource_kind
  for resource_kind in "${release_cluster_resource_kinds[@]}"; do
    kubectl delete "$resource_kind" -l "$release_profile_selector" \
      --ignore-not-found --wait=true --timeout=5m >/dev/null
  done
}

verify_release_cluster_scope_absent() {
  local resource_kind remaining
  for resource_kind in "${release_cluster_resource_kinds[@]}"; do
    remaining=$(kubectl get "$resource_kind" -l "$release_profile_selector" -o json |
      jq '.items | length')
    [[ "$remaining" == 0 ]] || fail "release-owned cluster scope remains: $resource_kind"
  done
}

detect_mattermost_workload() {
  local deployment_exists=false statefulset_exists=false
  kubectl -n "$shared_namespace" get deployment mattermost >/dev/null 2>&1 && deployment_exists=true
  kubectl -n "$shared_namespace" get statefulset mattermost >/dev/null 2>&1 && statefulset_exists=true
  case "$deployment_exists:$statefulset_exists" in
    true:false) printf 'deployment\n' ;;
    false:true) printf 'statefulset\n' ;;
    false:false) fail 'retained Mattermost workload is absent' ;;
    true:true) fail 'retained Mattermost workload is ambiguous' ;;
  esac
}

remove_legacy_session_token_finalizer() {
  local secret_name=$1 patch
  if patch=$(kubectl -n "$shared_namespace" get secret "$secret_name" -o json | jq -ce '
    select((.metadata.finalizers // []) | index("kodex.dev/session-token-protection")) |
    {metadata:{finalizers:[.metadata.finalizers[] | select(. != "kodex.dev/session-token-protection")]}}
  '); then
    kubectl -n "$shared_namespace" patch secret "$secret_name" --type=merge -p "$patch" >/dev/null
  fi
}

delete_stray_default_registry_resource() {
  local resource_kind=$1 resource_name=kodex-registry actual_owner
  if ! kubectl -n default get "$resource_kind" "$resource_name" >/dev/null 2>&1; then
    return
  fi
  actual_owner=$(kubectl -n default get "$resource_kind" "$resource_name" \
    -o jsonpath='{.metadata.labels.app\.kubernetes\.io/part-of}')
  [[ "$actual_owner" == kodex-release-bootstrap ]] ||
    fail "refusing to delete non-Kodex default resource: $resource_kind/$resource_name"
  kubectl -n default delete "$resource_kind" "$resource_name" --wait=true --timeout=5m
}

for retained_namespace in "$shared_namespace" kodex-ci kodex-ci-deploy identity cert-manager; do
  kubectl get namespace "$retained_namespace" >/dev/null 2>&1 || fail "retained namespace is absent: $retained_namespace"
done
mattermost_workload_kind=$(detect_mattermost_workload)
kubectl -n "$shared_namespace" get deployment kodex-registry >/dev/null 2>&1 || fail 'retained registry is absent'

if [[ "$mode" == preflight ]]; then
  legacy_pvcs=$(kubectl -n "$shared_namespace" get persistentvolumeclaims -o json |
    jq '[.items[] | select(.metadata.name == "kodex-kaniko-context" or (.metadata.name | startswith("mc-session-")))] | length')
  legacy_secrets=$(kubectl -n "$shared_namespace" get secrets -o json |
    jq '[.items[] | select(
      (.metadata.name | startswith("kodex-")) or
      (.metadata.name | startswith("mc-session-")) or
      (.metadata.name | startswith("mc-var-")) or
      .metadata.name == "legacy-data-migration-source-postgresql-g1"
    )] | length')
  printf 'Legacy reset preflight completed: mattermost=%s shared PVC=%s shared secrets=%s\n' \
    "$mattermost_workload_kind" "$legacy_pvcs" "$legacy_secrets"
  exit 0
fi

if [[ "$mode" == apply ]]; then
  [[ "$confirmation" == DELETE-LEGACY-KODEX-AND-MYQRCONTACT ]] || fail 'destructive confirmation mismatch'

  delete_release_cluster_scope
  kubectl delete namespace "$legacy_namespace" "$myqr_namespace" --ignore-not-found --wait=true --timeout=20m

  kubectl -n "$shared_namespace" delete deployment,service,ingress kodex-bot-service \
    --ignore-not-found --wait=true --timeout=5m

  kubectl delete clusterrole kodex-agent-runner-cluster-readonly --ignore-not-found
  kubectl delete clusterrolebinding \
    kodex-agent-runner-cluster-admin \
    kodex-agent-runner-cluster-readonly \
    --ignore-not-found
  kubectl delete validatingadmissionpolicy,validatingadmissionpolicybinding \
    kodex-production-workload-contracts \
    kodex-runtime-archive-restore-disabled \
    --ignore-not-found

  while IFS= read -r resource_name; do
    [[ -n "$resource_name" ]] || continue
    kubectl -n "$shared_namespace" delete persistentvolumeclaim "$resource_name" --wait=true --timeout=5m
  done < <(kubectl -n "$shared_namespace" get persistentvolumeclaims -o json |
    jq -r '.items[] | select(.metadata.name == "kodex-kaniko-context" or (.metadata.name | startswith("mc-session-"))) | .metadata.name')

  while IFS= read -r resource_name; do
    [[ -n "$resource_name" ]] || continue
    remove_legacy_session_token_finalizer "$resource_name"
    kubectl -n "$shared_namespace" delete secret "$resource_name" \
      --ignore-not-found --wait=true --timeout=2m
  done < <(kubectl -n "$shared_namespace" get secrets -o json |
    jq -r '.items[] | select(
      (.metadata.name | startswith("kodex-")) or
      (.metadata.name | startswith("mc-session-")) or
      (.metadata.name | startswith("mc-var-")) or
      .metadata.name == "legacy-data-migration-source-postgresql-g1"
    ) | .metadata.name')

  for resource_kind in serviceaccount role rolebinding; do
    while IFS= read -r resource_name; do
      [[ -n "$resource_name" ]] || continue
      kubectl -n "$shared_namespace" delete "$resource_kind" "$resource_name" --wait=true --timeout=2m
    done < <(kubectl -n "$shared_namespace" get "$resource_kind" -o json |
      jq -r '.items[] | select(
        (.metadata.name | startswith("kodex-")) or
        (.metadata.name | startswith("mc-session-"))
      ) | .metadata.name')
  done

  if [[ "$wipe_registry" == true ]]; then
    for resource_kind in deployment service persistentvolumeclaim; do
      delete_stray_default_registry_resource "$resource_kind"
    done
    kubectl -n "$shared_namespace" scale deployment/kodex-registry --replicas=0 >/dev/null
    kubectl -n "$shared_namespace" rollout status deployment/kodex-registry --timeout=180s >/dev/null
    kubectl -n "$shared_namespace" delete persistentvolumeclaim kodex-registry --wait=true --timeout=5m
    kubectl -n "$shared_namespace" apply -f "$repository_root/infra/bootstrap-registry/registry.yaml" >/dev/null
    kubectl -n "$shared_namespace" rollout status deployment/kodex-registry --timeout=300s >/dev/null
  fi
fi

if [[ "$mode" == readback ]]; then
  for removed_namespace in "$legacy_namespace" "$myqr_namespace"; do
    if kubectl get namespace "$removed_namespace" >/dev/null 2>&1; then
      fail "removed namespace still exists: $removed_namespace"
    fi
  done
  verify_release_cluster_scope_absent
  [[ $(detect_mattermost_workload) == "$mattermost_workload_kind" ]] ||
    fail 'retained Mattermost workload kind changed during reset'
  kubectl -n "$shared_namespace" get deployment kodex-registry >/dev/null
  kubectl -n kodex-ci get deployment kodex-arc-build-gha-rs-controller >/dev/null
  kubectl -n kodex-ci-deploy get deployment kodex-arc-deploy-gha-rs-controller >/dev/null
  kubectl -n identity get ingress sso >/dev/null
fi

printf 'Legacy installation reset completed: %s\n' "$mode"
