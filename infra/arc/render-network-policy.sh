#!/usr/bin/env bash
set -euo pipefail

registry_service_cidr=${1:-}
registry_endpoint_cidr=${2:-}
registry_namespace=${3:-}
release_registry_host=${4:-}
ingress_service_cidr=${5:-}
ingress_endpoint_port=${6:-}
ingress_namespace=${7:-}
ingress_pod_name=${8:-}
ingress_service_host=${9:-}
output=${10:-}
valid_ipv4_cidr() {
  local cidr=$1 address first second third fourth
  [[ "$cidr" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}/32$ ]] || return 1
  address=${cidr%/32}
  IFS=. read -r first second third fourth <<<"$address"
  ((first <= 255 && second <= 255 && third <= 255 && fourth <= 255))
}
valid_dns_host() {
  local host=$1 label
  local -a labels=()

  [[ ${#host} -le 253 && "$host" == *.* ]] || return 1
  [[ "$host" != .* && "$host" != *. && "$host" != *..* ]] || return 1
  [[ "$host" != *.svc && "$host" != *.svc.cluster.local ]] || return 1
  IFS='.' read -r -a labels <<<"$host"
  ((${#labels[@]} >= 2)) || return 1
  for label in "${labels[@]}"; do
    [[ ${#label} -le 63 ]] || return 1
    [[ "$label" =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?$ ]] || return 1
  done
}
if ! valid_ipv4_cidr "$registry_service_cidr" ||
   ! valid_ipv4_cidr "$registry_endpoint_cidr" ||
   ! valid_ipv4_cidr "$ingress_service_cidr" ||
   [[ ! "$registry_namespace" =~ ^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$ ]] ||
   [[ ! "$ingress_namespace" =~ ^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$ ]] ||
   [[ ! "$ingress_pod_name" =~ ^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$ ]] ||
   [[ ! "$ingress_endpoint_port" =~ ^[1-9][0-9]{0,4}$ ]] ||
   ((ingress_endpoint_port > 65535)) ||
   ! valid_dns_host "$release_registry_host" ||
   [[ ! "$ingress_service_host" =~ ^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?\.[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?\.svc\.cluster\.local$ ]] ||
   [[ -z "$output" ]]; then
  printf 'Usage: %s <registry-service-ipv4/32> <registry-endpoint-ipv4/32> <registry-namespace> <release-registry-host> <ingress-service-ipv4/32> <ingress-endpoint-port> <ingress-namespace> <ingress-pod-name> <ingress-service-host> <output>\n' "$0" >&2
  exit 2
fi
script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
source_file="$script_directory/network-policy.yaml"
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
REGISTRY_SERVICE_CIDR=$registry_service_cidr REGISTRY_ENDPOINT_CIDR=$registry_endpoint_cidr \
REGISTRY_NAMESPACE=$registry_namespace RELEASE_REGISTRY_HOST=$release_registry_host \
INGRESS_SERVICE_CIDR=$ingress_service_cidr INGRESS_ENDPOINT_PORT=$ingress_endpoint_port \
INGRESS_NAMESPACE=$ingress_namespace INGRESS_POD_NAME=$ingress_pod_name \
INGRESS_SERVICE_HOST=$ingress_service_host \
  yq eval-all '
    (.. | select(tag == "!!str")) |=
      sub("__REGISTRY_SERVICE_CIDR__"; strenv(REGISTRY_SERVICE_CIDR)) |
    (.. | select(tag == "!!str")) |=
      sub("__REGISTRY_ENDPOINT_CIDR__"; strenv(REGISTRY_ENDPOINT_CIDR)) |
    (.. | select(tag == "!!str")) |=
      sub("__REGISTRY_NAMESPACE__"; strenv(REGISTRY_NAMESPACE)) |
    (.. | select(tag == "!!str")) |=
      sub("__RELEASE_REGISTRY_HOST__"; strenv(RELEASE_REGISTRY_HOST)) |
    (.. | select(tag == "!!str")) |=
      sub("__INGRESS_SERVICE_CIDR__"; strenv(INGRESS_SERVICE_CIDR)) |
    (.. | select(tag == "!!str")) |=
      sub("__INGRESS_NAMESPACE__"; strenv(INGRESS_NAMESPACE)) |
    (.. | select(tag == "!!str")) |=
      sub("__INGRESS_POD_NAME__"; strenv(INGRESS_POD_NAME)) |
    (.. | select(tag == "!!str")) |=
      sub("__INGRESS_SERVICE_HOST__"; strenv(INGRESS_SERVICE_HOST)) |
    (. | select(.kind == "NetworkPolicy" and
      .metadata.name == "github-proxy-egress") |
      .spec.egress[1].ports[1].port) = (strenv(INGRESS_ENDPOINT_PORT) | tonumber)
  ' "$source_file" >"$temporary_directory/resolved-source.yaml"
if rg -q '__REGISTRY_(SERVICE|ENDPOINT)_CIDR__|__REGISTRY_NAMESPACE__|__RELEASE_REGISTRY_HOST__|__INGRESS_' "$temporary_directory/resolved-source.yaml"; then
  printf 'Registry network policy placeholder remained after render\n' >&2
  exit 1
fi
yq eval-all '.' "$temporary_directory/resolved-source.yaml" >"$temporary_directory/build-unannotated.yaml"
yq eval-all '
  select(.metadata.name != "build-registry") |
  .metadata.namespace = "kodex-ci-deploy" |
  (.. | select(tag == "!!str")) |= sub("kodex-ci\\.svc"; "kodex-ci-deploy.svc") |
  (.. | select(tag == "!!str")) |= sub("build-runner"; "deploy-runner")
' "$temporary_directory/resolved-source.yaml" >"$temporary_directory/deploy-unannotated.yaml"

annotate_proxy_config() {
  local input=$1 output=$2 config_checksum
  config_checksum=$(yq -r '
    select(.kind == "ConfigMap" and .metadata.name == "kodex-ci-egress-proxy") |
    .data."envoy.yaml"
  ' "$input" | sha256sum | awk '{print $1}')
  [[ "$config_checksum" =~ ^[a-f0-9]{64}$ ]] || {
    printf 'Failed to calculate the Envoy config checksum\n' >&2
    exit 1
  }
  CONFIG_CHECKSUM=$config_checksum yq eval-all '
    (. | select(.kind == "Deployment" and
      .metadata.name == "kodex-ci-egress-proxy") |
      .spec.template.metadata.annotations."kodex.dev/envoy-config-sha256") =
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
