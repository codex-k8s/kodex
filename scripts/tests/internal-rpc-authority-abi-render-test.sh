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

for profile in web-only web-with-mattermost; do
kubectl kustomize "$repository_root/deploy/k8s/profiles/$profile" >"$render"

validate_render() {
  yq -o=json -I=0 '.' "$1" | jq -s -e --arg abi "$abi" '
    [map(select(.kind == "Deployment"))[] |
      select(any((.spec.template.spec.containers[]?, .spec.template.spec.initContainers[]?);
        (.name | test("(^internal-rpc-authority-(issuer|verifier)$|platform-worker-grant-agent$)"))))] as $deployments |
    ($deployments | length) > 0 and
    all($deployments[];
      .spec.template.metadata.labels."kodex.dev/internal-rpc-authority-abi" == $abi and
      ([ (.spec.template.spec.containers[]?, .spec.template.spec.initContainers[]?) |
        select(.name | test("(^internal-rpc-authority-(issuer|verifier)$|platform-worker-grant-agent$)")) |
        .image] | unique | length) == 1)
  ' >/dev/null
}

validate_render "$render" || fail 'active workloads do not share the declared authority ABI and image'

validate_controller_sidecars() {
  yq -o=json -I=0 '.' "$1" | jq -s -e '
  any(.[];
    .kind == "Deployment" and .metadata.name == "runtime-controller" and
    ([.spec.template.spec.initContainers[].name] ==
      ["internal-rpc-authority-socket-init", "internal-rpc-authority-issuer", "platform-worker-grant-agent"]) and
    all(.spec.template.spec.initContainers[] | select(.name != "internal-rpc-authority-socket-init");
      .restartPolicy == "Always" and .startupProbe.httpGet.path == "/readyz" and
      .readinessProbe.httpGet.path == "/readyz" and
      .resources.requests.cpu != null and .resources.requests.memory != null and
      .resources.limits.cpu != null and .resources.limits.memory != null) and
    ([.spec.template.spec.containers[].name] == ["runtime-controller"]))
  ' >/dev/null
}
validate_controller_sidecars "$render" || fail 'controller authority sidecar startup or termination ordering is invalid'

yq 'select(.kind == "Deployment" and .metadata.name == "runtime-controller") |
  del(.spec.template.spec.initContainers[] | select(.name == "internal-rpc-authority-issuer") | .restartPolicy)' \
  "$render" >"$mutated"
if validate_controller_sidecars "$mutated"; then
  fail 'controller issuer without native shutdown ordering was accepted'
fi

yq 'select(.kind == "Deployment" and .metadata.name == "runtime-controller") |
  .spec.template.spec.initContainers[] |=
    (select(.name == "internal-rpc-authority-issuer").image =
      "ghcr.io/codex-k8s/kodex/internal-rpc-authority@sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")' \
  "$render" >"$mutated"
if validate_render "$mutated"; then
  fail 'incompatible native sidecar authority image was accepted'
fi

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

done
printf 'Internal RPC authority ABI render test passed\n'
