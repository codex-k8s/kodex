#!/usr/bin/env bash
set -euo pipefail

registry_service_cidr=${1:-}
registry_endpoint_cidr=${2:-}
registry_namespace=${3:-}
output=${4:-}
valid_ipv4_cidr() {
  local cidr=$1 address first second third fourth
  [[ "$cidr" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}/32$ ]] || return 1
  address=${cidr%/32}
  IFS=. read -r first second third fourth <<<"$address"
  ((first <= 255 && second <= 255 && third <= 255 && fourth <= 255))
}
if ! valid_ipv4_cidr "$registry_service_cidr" ||
   ! valid_ipv4_cidr "$registry_endpoint_cidr" ||
   [[ ! "$registry_namespace" =~ ^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$ ]] ||
   [[ -z "$output" ]]; then
  printf 'Usage: %s <registry-service-ipv4/32> <registry-endpoint-ipv4/32> <registry-namespace> <output>\n' "$0" >&2
  exit 2
fi
script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
source_file="$script_directory/network-policy.yaml"
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
REGISTRY_SERVICE_CIDR=$registry_service_cidr REGISTRY_ENDPOINT_CIDR=$registry_endpoint_cidr \
REGISTRY_NAMESPACE=$registry_namespace \
  yq eval-all '
    (.. | select(tag == "!!str")) |=
      sub("__REGISTRY_SERVICE_CIDR__"; strenv(REGISTRY_SERVICE_CIDR)) |
    (.. | select(tag == "!!str")) |=
      sub("__REGISTRY_ENDPOINT_CIDR__"; strenv(REGISTRY_ENDPOINT_CIDR)) |
    (.. | select(tag == "!!str")) |=
      sub("__REGISTRY_NAMESPACE__"; strenv(REGISTRY_NAMESPACE))
  ' "$source_file" >"$temporary_directory/resolved-source.yaml"
if rg -q '__REGISTRY_(SERVICE|ENDPOINT)_CIDR__|__REGISTRY_NAMESPACE__' "$temporary_directory/resolved-source.yaml"; then
  printf 'Registry CIDR placeholder remained after render\n' >&2
  exit 1
fi
yq eval-all '.' "$temporary_directory/resolved-source.yaml" >"$temporary_directory/build-unannotated.yaml"
yq eval-all '
  select(.metadata.name != "build-registry") |
  .metadata.namespace = "mattercodex-ci-deploy" |
  (.. | select(tag == "!!str")) |= sub("mattercodex-ci\\.svc"; "mattercodex-ci-deploy.svc") |
  (.. | select(tag == "!!str")) |= sub("build-runner"; "deploy-runner")
' "$temporary_directory/resolved-source.yaml" >"$temporary_directory/deploy-unannotated.yaml"

annotate_proxy_config() {
  local input=$1 output=$2 config_checksum
  config_checksum=$(yq -r '
    select(.kind == "ConfigMap" and .metadata.name == "mattercodex-ci-egress-proxy") |
    .data."envoy.yaml"
  ' "$input" | sha256sum | awk '{print $1}')
  [[ "$config_checksum" =~ ^[a-f0-9]{64}$ ]] || {
    printf 'Failed to calculate the Envoy config checksum\n' >&2
    exit 1
  }
  CONFIG_CHECKSUM=$config_checksum yq eval-all '
    (. | select(.kind == "Deployment" and
      .metadata.name == "mattercodex-ci-egress-proxy") |
      .spec.template.metadata.annotations."mattercodex.dev/envoy-config-sha256") =
        strenv(CONFIG_CHECKSUM)
  ' "$input" >"$output"
}

annotate_proxy_config "$temporary_directory/build-unannotated.yaml" \
  "$temporary_directory/build.yaml"
annotate_proxy_config "$temporary_directory/deploy-unannotated.yaml" \
  "$temporary_directory/deploy.yaml"
cp "$temporary_directory/build.yaml" "$output"
printf '%s\n' '---' >>"$output"
cat "$temporary_directory/deploy.yaml" >>"$output"
