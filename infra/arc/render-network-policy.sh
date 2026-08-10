#!/usr/bin/env bash
set -euo pipefail

output=${1:-}
[[ -n "$output" ]] || { printf 'Usage: %s <output>\n' "$0" >&2; exit 2; }
script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
source_file="$script_directory/network-policy.yaml"
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
yq eval-all '.' "$source_file" >"$temporary_directory/build-unannotated.yaml"
yq eval-all '
  select(.metadata.name != "build-registry") |
  .metadata.namespace = "mattercodex-ci-deploy" |
  (.. | select(tag == "!!str")) |= sub("mattercodex-ci\\.svc"; "mattercodex-ci-deploy.svc") |
  (.. | select(tag == "!!str")) |= sub("build-runner"; "deploy-runner")
' "$source_file" >"$temporary_directory/deploy-unannotated.yaml"

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
