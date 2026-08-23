#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Legacy installation reset failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 --context <exact-context> --mode preflight|apply|readback" \
    '  [--confirm DELETE-LEGACY-MATTERCODEX-AND-MYQRCONTACT] [--wipe-bootstrap-registry]' >&2
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

legacy_namespace=mattercodex-system
myqr_namespace=myqrcontact
shared_namespace=matter-kodex-prod
repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)

for retained_namespace in "$shared_namespace" mattercodex-ci mattercodex-ci-deploy identity cert-manager; do
  kubectl get namespace "$retained_namespace" >/dev/null 2>&1 || fail "retained namespace is absent: $retained_namespace"
done
kubectl -n "$shared_namespace" get statefulset mattermost >/dev/null 2>&1 || fail 'retained Mattermost is absent'
kubectl -n "$shared_namespace" get deployment matter-codex-registry >/dev/null 2>&1 || fail 'retained registry is absent'

if [[ "$mode" == preflight ]]; then
  legacy_pvcs=$(kubectl -n "$shared_namespace" get persistentvolumeclaims -o json |
    jq '[.items[] | select(.metadata.name == "matter-codex-kaniko-context" or (.metadata.name | startswith("mc-session-")))] | length')
  legacy_secrets=$(kubectl -n "$shared_namespace" get secrets -o json |
    jq '[.items[] | select(
      (.metadata.name | startswith("matter-codex-")) or
      (.metadata.name | startswith("mc-session-")) or
      (.metadata.name | startswith("mc-var-")) or
      .metadata.name == "legacy-data-migration-source-postgresql-g1"
    )] | length')
  printf 'Legacy reset preflight completed: shared PVC=%s shared secrets=%s\n' "$legacy_pvcs" "$legacy_secrets"
  exit 0
fi

if [[ "$mode" == apply ]]; then
  [[ "$confirmation" == DELETE-LEGACY-MATTERCODEX-AND-MYQRCONTACT ]] || fail 'destructive confirmation mismatch'

  kubectl delete namespace "$legacy_namespace" "$myqr_namespace" --ignore-not-found --wait=true --timeout=20m

  kubectl -n "$shared_namespace" delete deployment,service,ingress matter-codex-bot-service \
    --ignore-not-found --wait=true --timeout=5m

  kubectl delete clusterrole matter-codex-agent-runner-cluster-readonly --ignore-not-found
  kubectl delete clusterrolebinding \
    matter-codex-agent-runner-cluster-admin \
    matter-codex-agent-runner-cluster-readonly \
    --ignore-not-found
  kubectl delete validatingadmissionpolicy,validatingadmissionpolicybinding \
    mattercodex-production-workload-contracts \
    mattercodex-runtime-archive-restore-disabled \
    --ignore-not-found

  while IFS= read -r resource_name; do
    [[ -n "$resource_name" ]] || continue
    kubectl -n "$shared_namespace" delete persistentvolumeclaim "$resource_name" --wait=true --timeout=5m
  done < <(kubectl -n "$shared_namespace" get persistentvolumeclaims -o json |
    jq -r '.items[] | select(.metadata.name == "matter-codex-kaniko-context" or (.metadata.name | startswith("mc-session-"))) | .metadata.name')

  while IFS= read -r resource_name; do
    [[ -n "$resource_name" ]] || continue
    kubectl -n "$shared_namespace" delete secret "$resource_name" --wait=true --timeout=2m
  done < <(kubectl -n "$shared_namespace" get secrets -o json |
    jq -r '.items[] | select(
      (.metadata.name | startswith("matter-codex-")) or
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
        (.metadata.name | startswith("matter-codex-")) or
        (.metadata.name | startswith("mc-session-"))
      ) | .metadata.name')
  done

  if [[ "$wipe_registry" == true ]]; then
    kubectl -n "$shared_namespace" scale deployment/matter-codex-registry --replicas=0 >/dev/null
    kubectl -n "$shared_namespace" rollout status deployment/matter-codex-registry --timeout=180s >/dev/null
    kubectl -n "$shared_namespace" delete persistentvolumeclaim matter-codex-registry --wait=true --timeout=5m
    kubectl apply -f "$repository_root/infra/bootstrap-registry/registry.yaml" >/dev/null
    kubectl -n "$shared_namespace" rollout status deployment/matter-codex-registry --timeout=300s >/dev/null
  fi
fi

if [[ "$mode" == readback ]]; then
  for removed_namespace in "$legacy_namespace" "$myqr_namespace"; do
    if kubectl get namespace "$removed_namespace" >/dev/null 2>&1; then
      fail "removed namespace still exists: $removed_namespace"
    fi
  done
  kubectl -n "$shared_namespace" get statefulset mattermost >/dev/null
  kubectl -n "$shared_namespace" get deployment matter-codex-registry >/dev/null
  kubectl -n mattercodex-ci get deployment mattercodex-arc-build-gha-rs-controller >/dev/null
  kubectl -n mattercodex-ci-deploy get deployment mattercodex-arc-deploy-gha-rs-controller >/dev/null
  kubectl -n identity get ingress sso >/dev/null
fi

printf 'Legacy installation reset completed: %s\n' "$mode"
