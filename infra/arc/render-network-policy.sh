#!/usr/bin/env bash
set -euo pipefail

output=${1:-}
[[ -n "$output" ]] || { printf 'Usage: %s <output>\n' "$0" >&2; exit 2; }
script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
source_file="$script_directory/network-policy.yaml"
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
yq eval-all '.' "$source_file" >"$temporary_directory/build.yaml"
yq eval-all '
  select(.metadata.name != "build-registry") |
  .metadata.namespace = "mattercodex-ci-deploy" |
  (.. | select(tag == "!!str")) |= sub("mattercodex-ci\\.svc"; "mattercodex-ci-deploy.svc") |
  (.. | select(tag == "!!str")) |= sub("build-runner"; "deploy-runner")
' "$source_file" >"$temporary_directory/deploy.yaml"
cp "$temporary_directory/build.yaml" "$output"
printf '%s\n' '---' >>"$output"
cat "$temporary_directory/deploy.yaml" >>"$output"
