#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'ARC Helm values render failed: %s\n' "$*" >&2; exit 1; }
usage() { printf 'Usage: %s <kubernetes-api-service-ip> <output-directory>\n' "$0" >&2; }

kubernetes_api_service_ip=${1:-}
output_directory=${2:-}
[[ -n "$kubernetes_api_service_ip" && -n "$output_directory" ]] || {
  usage
  exit 2
}

IFS=. read -r octet1 octet2 octet3 octet4 extra <<<"$kubernetes_api_service_ip"
[[ -z "${extra:-}" ]] || fail 'Kubernetes API service IP is not exact IPv4'
for octet in "$octet1" "$octet2" "$octet3" "$octet4"; do
  [[ "$octet" =~ ^(0|[1-9][0-9]{0,2})$ ]] && ((10#$octet <= 255)) ||
    fail 'Kubernetes API service IP is not exact IPv4'
done
[[ -d "$output_directory" ]] || fail 'output directory does not exist'

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
for values_name in controller-values controller-deploy-values build-runner-values deploy-runner-values; do
  KUBERNETES_API_SERVICE_IP=$kubernetes_api_service_ip yq '
    (.. | select(tag == "!!str")) |=
      sub("__KUBERNETES_API_SERVICE_IP__"; strenv(KUBERNETES_API_SERVICE_IP))
  ' "$script_directory/$values_name.yaml" >"$output_directory/$values_name.yaml"
  if rg -q '__KUBERNETES_API_SERVICE_IP__' "$output_directory/$values_name.yaml"; then
    fail "unresolved Kubernetes API service IP placeholder: $values_name"
  fi
done

printf 'ARC Helm values rendered without sensitive values\n'
