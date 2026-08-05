#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: render-role-image-builder.sh staging|production builder-sha256 authority-sha256 registry-pull-fqdn" >&2
}

if [[ $# -ne 4 ]]; then
  usage
  exit 64
fi
environment_name=$1
builder_digest=$2
authority_digest=$3
registry_pull_host=$4
[[ $environment_name == staging || $environment_name == production ]] || { usage; exit 64; }
for digest in "$builder_digest" "$authority_digest"; do
  [[ $digest =~ ^sha256:[a-f0-9]{64}$ ]] &&
    [[ $digest != sha256:0000000000000000000000000000000000000000000000000000000000000000 ]] ||
    { echo "image digest is invalid" >&2; exit 64; }
done
[[ $registry_pull_host =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] && [[ $registry_pull_host == *.* ]] &&
  [[ $registry_pull_host != *.svc ]] && [[ $registry_pull_host != *.svc.cluster.local ]] ||
  { echo "registry_pull_host must be a node-reachable exact DNS name" >&2; exit 64; }
command -v kubectl >/dev/null 2>&1 || { echo "kubectl is required" >&2; exit 69; }

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
repository_root=$(cd -- "$script_dir/.." && pwd)
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
raw="$temporary_directory/raw.yaml"
kubectl kustomize "$repository_root/deploy/k8s/overlays/$environment_name/role-image-builder" >"$raw"

builder_placeholder='mattercodex-image-registry.mattercodex-system.svc.cluster.local:5000/mattercodex/role-image-builder@sha256:0000000000000000000000000000000000000000000000000000000000000000'
authority_placeholder='ghcr.io/codex-k8s/matter-codex/internal-rpc-authority@sha256:0000000000000000000000000000000000000000000000000000000000000000'
[[ $(grep -F -c "$builder_placeholder" "$raw" || true) -eq 1 ]] ||
  { echo "builder render input is incomplete" >&2; exit 78; }
[[ $(grep -F -c "$authority_placeholder" "$raw" || true) -eq 2 ]] ||
  { echo "authority render inputs are incomplete" >&2; exit 78; }
toolchain_placeholder='ROLE_IMAGE_BUILDER_EXPECTED_TOOLCHAIN_SHA256: "0000000000000000000000000000000000000000000000000000000000000000"'
[[ $(grep -F -c "$toolchain_placeholder" "$raw" || true) -eq 1 ]] ||
  { echo "builder toolchain render input is incomplete" >&2; exit 78; }

sed \
  -e "s|$builder_placeholder|$registry_pull_host/mattercodex/role-image-builder@$builder_digest|g" \
  -e "s|$authority_placeholder|$registry_pull_host/mattercodex/internal-rpc-authority@$authority_digest|g" \
  -e "s|$toolchain_placeholder|ROLE_IMAGE_BUILDER_EXPECTED_TOOLCHAIN_SHA256: \"${builder_digest#sha256:}\"|g" \
  "$raw"
