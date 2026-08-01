#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: render-control-plane.sh staging|production control-plane-sha256 authority-sha256 agent-runtime-sha256 registry-pull-fqdn admission-tools-image@sha256:digest policy-revision" >&2
}

if [[ $# -ne 7 ]]; then
  usage
  exit 2
fi

environment_name=$1
image_digest=$2
authority_image_digest=$3
agent_runtime_image_digest=$4
registry_pull_host=$5
admission_tools_image=$6
policy_revision=$7

case "$environment_name" in
  staging|production) ;;
  *)
    usage
    exit 2
    ;;
esac

for digest_name in image_digest authority_image_digest agent_runtime_image_digest; do
  digest=${!digest_name}
  if [[ ! "$digest" =~ ^sha256:[a-f0-9]{64}$ ]] ||
    [[ "$digest" == "sha256:0000000000000000000000000000000000000000000000000000000000000000" ]]; then
    echo "$digest_name is invalid" >&2
    exit 2
  fi
done
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
overlay="$repository_root/deploy/k8s/overlays/$environment_name/control-plane"
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
raw_render="$temporary_directory/raw.yaml"
final_render="$temporary_directory/final.yaml"

kubectl kustomize "$overlay" >"$raw_render"

registry_host='mattercodex-image-registry.mattercodex-system.svc.cluster.local:5000'
placeholder="$registry_host/mattercodex/control-plane@sha256:0000000000000000000000000000000000000000000000000000000000000000"
replacement="$registry_pull_host/mattercodex/control-plane@$image_digest"
placeholder_count=$(grep -F -c "$placeholder" "$raw_render" || true)
if [[ "$placeholder_count" -ne 3 ]]; then
  echo "canonical render does not contain exactly three control-plane image inputs" >&2
  exit 1
fi

authority_placeholder='ghcr.io/codex-k8s/matter-codex/internal-rpc-authority@sha256:0000000000000000000000000000000000000000000000000000000000000000'
authority_replacement="$registry_pull_host/mattercodex/internal-rpc-authority@$authority_image_digest"
authority_placeholder_count=$(grep -F -c "$authority_placeholder" "$raw_render" || true)
if [[ "$authority_placeholder_count" -ne 2 ]]; then
  echo "canonical render does not contain exactly two authority image inputs" >&2
  exit 1
fi

runtime_digest_placeholder='mattercodex.dev/agent-runtime-image-digest: sha256:0000000000000000000000000000000000000000000000000000000000000000'
runtime_digest_replacement="mattercodex.dev/agent-runtime-image-digest: $agent_runtime_image_digest"
runtime_digest_placeholder_count=$(grep -F -c "$runtime_digest_placeholder" "$raw_render" || true)
if [[ "$runtime_digest_placeholder_count" -ne 1 ]]; then
  echo "canonical render does not contain exactly one agent runtime digest input" >&2
  exit 1
fi

registry_host_placeholder='registry-pull.invalid'
registry_host_placeholder_count=$(grep -F -c "$registry_host_placeholder" "$raw_render" || true)
if [[ "$registry_host_placeholder_count" -lt 2 ]]; then
  echo "canonical render does not materialize the node registry endpoint" >&2
  exit 1
fi
tools_placeholder='admission-tools.invalid/mattercodex/image-admission-tools@sha256:0000000000000000000000000000000000000000000000000000000000000000'
tools_digest=${admission_tools_image##*@}
if [[ $(grep -F -c "$tools_placeholder" "$raw_render" || true) -lt 5 ]] ||
  [[ $(grep -F -c 'mattercodex.dev/admission-tools-sha256: sha256:0000000000000000000000000000000000000000000000000000000000000000' "$raw_render" || true) -ne 1 ]] ||
  [[ $(grep -F -c 'policyRevision: "0"' "$raw_render" || true) -ne 1 ]]; then
  echo "canonical render does not contain the owner admission intent" >&2
  exit 1
fi

sed \
  -e "s|$placeholder|$replacement|g" \
  -e "s|$authority_placeholder|$authority_replacement|g" \
  -e "s|$runtime_digest_placeholder|$runtime_digest_replacement|g" \
  -e "s|$tools_placeholder|$admission_tools_image|g" \
  -e "s|mattercodex.dev/admission-tools-sha256: sha256:0000000000000000000000000000000000000000000000000000000000000000|mattercodex.dev/admission-tools-sha256: $tools_digest|g" \
  -e "s|policyRevision: \"0\"|policyRevision: \"$policy_revision\"|g" \
  -e "s|$registry_host_placeholder|$registry_pull_host|g" \
  "$raw_render" >"$final_render"

if grep -F -q '@sha256:0000000000000000000000000000000000000000000000000000000000000000' "$final_render"; then
  echo "unresolved image digest remains in render" >&2
  exit 1
fi

if grep -F -q "$registry_host_placeholder" "$final_render"; then
  echo "unresolved registry pull host remains in render" >&2
  exit 1
fi

cat "$final_render"
