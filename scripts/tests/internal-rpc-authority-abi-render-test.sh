#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Internal RPC authority ABI render test failed: %s\n' "$*" >&2
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
render="$temporary_directory/render.yaml"
mutated="$temporary_directory/mutated.yaml"
abi=$(jq -er '.policy.authority_abi_version | tostring' \
  "$repository_root/deploy/k8s/base/internal-rpc-authority-publisher/authority-policy.json")

kubectl kustomize "$repository_root/deploy/k8s/profiles/web-only" >"$render"

validate_render() {
  yq -o=json -I=0 '.' "$1" | jq -s -e --arg abi "$abi" '
    [map(select(.kind == "Deployment"))[] |
      select(any(.spec.template.spec.containers[]?;
        (.name | test("(^internal-rpc-authority-(issuer|verifier)$|platform-worker-grant-agent$)"))))] as $deployments |
    ($deployments | length) > 0 and
    all($deployments[];
      .spec.template.metadata.labels."kodex.dev/internal-rpc-authority-abi" == $abi and
      ([.spec.template.spec.containers[] |
        select(.name | test("(^internal-rpc-authority-(issuer|verifier)$|platform-worker-grant-agent$)")) |
        .image] | unique | length) == 1)
  ' >/dev/null
}

validate_render "$render" || fail 'active workloads do not share the declared authority ABI and image'

yq 'select(.kind == "Deployment" and .metadata.name == "control-plane") |
  .spec.template.spec.containers[] |=
    (select(.name == "control-plane-platform-worker-grant-agent").image =
      "ghcr.io/codex-k8s/kodex/internal-rpc-authority@sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")' \
  "$render" >"$mutated"
if validate_render "$mutated"; then
  fail 'incompatible control-plane grant agent image was accepted'
fi

yq 'select(.kind == "Deployment" and .metadata.name == "secret-broker") |
  .spec.template.metadata.labels."kodex.dev/internal-rpc-authority-abi" = "1"' \
  "$render" >"$mutated"
if validate_render "$mutated"; then
  fail 'incompatible authority ABI label was accepted'
fi

printf 'Internal RPC authority ABI render test passed\n'
