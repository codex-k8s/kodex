#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kubernetes API endpoint resolution failed: %s\n' "$*" >&2
  exit 1
}

command -v kubectl >/dev/null 2>&1 || fail "kubectl is required"
command -v jq >/dev/null 2>&1 || fail "jq is required"

addresses="$(
  kubectl get endpointslices.discovery.k8s.io \
    --namespace default \
    --selector kubernetes.io/service-name=kubernetes \
    --output json |
    jq -er '
      [
        .items[].endpoints[]
        | select(.conditions.ready != false)
        | .addresses[]
      ]
      | unique
      | select(length > 0 and length <= 8)
      | .[]
    '
)" || fail "one to eight ready Kubernetes Service EndpointSlice addresses are required"

while IFS= read -r address; do
  [[ -n "$address" ]] || continue
  if [[ "$address" == *:* ]]; then
    printf '%s/128\n' "$address"
  else
    printf '%s/32\n' "$address"
  fi
done <<<"$addresses" | paste -sd, -
