#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: render-image-supply-chain.sh staging|production control-plane-sha256 registry-pull-fqdn admission-tools-image@sha256:digest policy-revision" >&2
}

if [[ $# -ne 5 ]]; then
  usage
  exit 2
fi

environment_name=$1
control_plane_digest=$2
registry_pull_host=$3
admission_tools_image=$4
policy_revision=$5

case "$environment_name" in
  staging|production) ;;
  *) usage; exit 2 ;;
esac
if [[ ! "$control_plane_digest" =~ ^sha256:[a-f0-9]{64}$ ]] ||
  [[ "$control_plane_digest" == "sha256:0000000000000000000000000000000000000000000000000000000000000000" ]]; then
  echo "control_plane_digest is invalid" >&2
  exit 2
fi
if [[ ! "$admission_tools_image" =~ ^[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}$ ]] ||
  [[ "$admission_tools_image" == *@sha256:0000000000000000000000000000000000000000000000000000000000000000 ]]; then
  echo "admission_tools_image is invalid" >&2
  exit 2
fi
[[ $policy_revision =~ ^[1-9][0-9]*$ ]] || {
  echo "policy_revision is invalid" >&2
  exit 2
}
if [[ ! "$registry_pull_host" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] ||
  [[ "$registry_pull_host" != *.* ]] ||
  [[ "$registry_pull_host" == *.svc ]] ||
  [[ "$registry_pull_host" == *.svc.cluster.local ]] ||
  [[ ${#registry_pull_host} -gt 253 ]]; then
  echo "registry_pull_host must be a node-reachable exact DNS name" >&2
  exit 2
fi
if ! command -v kubectl >/dev/null 2>&1; then
  echo "kubectl is required for the canonical render" >&2
  exit 1
fi

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
repository_root=$(cd -- "$script_dir/.." && pwd)
overlay="$repository_root/deploy/k8s/overlays/$environment_name/image-supply-chain"
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
raw_render="$temporary_directory/raw.yaml"
final_render="$temporary_directory/final.yaml"

kubectl kustomize "$overlay" >"$raw_render"

digest_placeholder='mattercodex-image-registry.mattercodex-system.svc.cluster.local:5000/mattercodex/control-plane@sha256:0000000000000000000000000000000000000000000000000000000000000000'
digest_replacement="$registry_pull_host/mattercodex/control-plane@$control_plane_digest"
if [[ $(grep -F -c "$digest_placeholder" "$raw_render" || true) -ne 1 ]]; then
  echo "supply-chain render must contain one node readback image input" >&2
  exit 1
fi
tools_placeholder='admission-tools.invalid/mattercodex/image-admission-tools@sha256:0000000000000000000000000000000000000000000000000000000000000000'
tools_digest=${admission_tools_image##*@}
if [[ $(grep -F -c "$tools_placeholder" "$raw_render" || true) -lt 1 ]] ||
  [[ $(grep -F -c 'mattercodex.dev/admission-tools-sha256: sha256:0000000000000000000000000000000000000000000000000000000000000000' "$raw_render" || true) -ne 1 ]] ||
  [[ $(grep -F -c 'policyRevision: "0"' "$raw_render" || true) -ne 1 ]]; then
  echo "supply-chain render does not contain the owner admission intent" >&2
  exit 1
fi
if [[ $(grep -F -c 'registry-pull.invalid' "$raw_render" || true) -lt 3 ]]; then
  echo "supply-chain render does not bind the pull endpoint consistently" >&2
  exit 1
fi

sed \
  -e "s|$digest_placeholder|$digest_replacement|g" \
  -e "s|$tools_placeholder|$admission_tools_image|g" \
  -e "s|mattercodex.dev/admission-tools-sha256: sha256:0000000000000000000000000000000000000000000000000000000000000000|mattercodex.dev/admission-tools-sha256: $tools_digest|g" \
  -e "s|policyRevision: \"0\"|policyRevision: \"$policy_revision\"|g" \
  -e "s|registry-pull.invalid|$registry_pull_host|g" \
  "$raw_render" >"$final_render"

if grep -F -q 'sha256:0000000000000000000000000000000000000000000000000000000000000000' "$final_render" ||
  grep -F -q 'registry-pull.invalid' "$final_render"; then
  echo "unresolved supply-chain input remains in render" >&2
  exit 1
fi
cat "$final_render"
