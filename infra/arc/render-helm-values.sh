#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'ARC Helm values render failed: %s\n' "$*" >&2; exit 1; }
usage() { printf 'Usage: %s <kubernetes-api-service-ip> <egress-proxy-config-sha256> <registry-host> <output-directory>\n' "$0" >&2; }

kubernetes_api_service_ip=${1:-}
egress_proxy_config_sha256=${2:-}
registry_host=${3:-}
output_directory=${4:-}
[[ -n "$kubernetes_api_service_ip" && -n "$egress_proxy_config_sha256" &&
  -n "$registry_host" && -n "$output_directory" ]] || {
  usage
  exit 2
}

IFS=. read -r octet1 octet2 octet3 octet4 extra <<<"$kubernetes_api_service_ip"
[[ -z "${extra:-}" ]] || fail 'Kubernetes API service IP is not exact IPv4'
for octet in "$octet1" "$octet2" "$octet3" "$octet4"; do
  [[ "$octet" =~ ^(0|[1-9][0-9]{0,2})$ ]] && ((10#$octet <= 255)) ||
    fail 'Kubernetes API service IP is not exact IPv4'
done
[[ "$egress_proxy_config_sha256" =~ ^[a-f0-9]{64}$ ]] ||
  fail 'egress proxy config SHA-256 is invalid'
[[ "$registry_host" =~ ^matter-codex-registry\.[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?\.svc\.cluster\.local$ ]] ||
  fail 'registry host is invalid'
[[ -d "$output_directory" ]] || fail 'output directory does not exist'

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
for values_name in controller-values controller-deploy-values build-runner-values deploy-runner-values; do
  KUBERNETES_API_SERVICE_IP=$kubernetes_api_service_ip \
    EGRESS_PROXY_CONFIG_SHA256=$egress_proxy_config_sha256 \
    REGISTRY_HOST=$registry_host yq '
    (.. | select(tag == "!!str")) |=
      sub("__KUBERNETES_API_SERVICE_IP__"; strenv(KUBERNETES_API_SERVICE_IP)) |
    (.. | select(tag == "!!str")) |=
      sub("__EGRESS_PROXY_CONFIG_SHA256__"; strenv(EGRESS_PROXY_CONFIG_SHA256)) |
    (.. | select(tag == "!!str")) |=
      sub("__REGISTRY_HOST__"; strenv(REGISTRY_HOST))
  ' "$script_directory/$values_name.yaml" >"$output_directory/$values_name.yaml"
  if rg -q '__KUBERNETES_API_SERVICE_IP__|__EGRESS_PROXY_CONFIG_SHA256__|__REGISTRY_HOST__' \
    "$output_directory/$values_name.yaml"; then
    fail "unresolved ARC Helm values placeholder: $values_name"
  fi
done

printf 'ARC Helm values rendered without sensitive values\n'
