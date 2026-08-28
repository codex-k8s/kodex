#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex local reset failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf 'Usage: %s --context <exact-context> --confirm DELETE-KODEX-LOCAL-DATA\n' "$0" >&2
}

context=""
confirmation=""
while (($# > 0)); do
  case "$1" in
    --context) context=${2:-}; shift 2 ;;
    --confirm) confirmation=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$context" ]] || fail 'exact Kubernetes context is required'
[[ "$confirmation" == DELETE-KODEX-LOCAL-DATA ]] ||
  fail 'confirmation must be DELETE-KODEX-LOCAL-DATA'
for command_name in jq kubectl; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$(kubectl config current-context)" == "$context" ]] || fail 'Kubernetes context mismatch'
kubectl get --raw=/readyz >/dev/null || fail 'Kubernetes API is unavailable'

namespace=kodex-system
namespace_state=$(kubectl get "namespace/$namespace" -o json 2>/dev/null || true)
[[ -n "$namespace_state" ]] || fail 'local Kodex namespace is absent'
jq -e '
  .metadata.name == "kodex-system" and
  .metadata.labels["app.kubernetes.io/part-of"] == "kodex" and
  .metadata.labels["kodex.dev/local-profile"] == "hot-reload" and
  .metadata.labels["kodex.dev/profile"] == "web-only"
' <<<"$namespace_state" >/dev/null || fail 'namespace is not an exact Kodex local profile'

identity_uid=$(kubectl get namespace identity -o jsonpath='{.metadata.uid}' 2>/dev/null || true)
[[ -n "$identity_uid" ]] || fail 'identity namespace is absent'
[[ "$(kubectl get namespace identity -o jsonpath='{.metadata.labels.kodex\.dev/capability}')" == identity ]] ||
  fail 'identity namespace does not have the expected capability label'

radar_uid=$(kubectl get namespace radar-dev -o jsonpath='{.metadata.uid}' 2>/dev/null || true)

kubectl delete "namespace/$namespace" --wait=true --timeout=10m >/dev/null
kubectl get "namespace/$namespace" >/dev/null 2>&1 && fail 'Kodex local namespace still exists'

[[ "$(kubectl get namespace identity -o jsonpath='{.metadata.uid}' 2>/dev/null || true)" == "$identity_uid" ]] ||
  fail 'identity namespace changed during reset'
if [[ -n "$radar_uid" ]]; then
  [[ "$(kubectl get namespace radar-dev -o jsonpath='{.metadata.uid}' 2>/dev/null || true)" == "$radar_uid" ]] ||
    fail 'radar-dev namespace changed during reset'
fi

printf 'Kodex local data reset completed for context %s; identity was preserved\n' "$context"
