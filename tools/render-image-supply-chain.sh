#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: render-image-supply-chain.sh staging|production control-plane-sha256 authority-sha256 registry-pull-fqdn admission-tools-image@sha256:digest admission-image@sha256:digest policy-revision policy-sha256 pull-credential-generation node-ipv4-cidr node-ipv6-cidr trusted-base-digest frontend-sha256 runtime-contract-revision runtime-contract-sha256" >&2
}

if [[ $# -ne 15 ]]; then
  usage
  exit 2
fi

environment_name=$1
control_plane_digest=$2
authority_image_digest=$3
registry_pull_host=$4
admission_tools_image=$5
admission_image=$6
policy_revision=$7
policy_sha256=$8
pull_credential_generation=$9
node_ipv4_cidr=${10}
node_ipv6_cidr=${11}
trusted_base_digest=${12}
frontend_sha256=${13}
runtime_contract_revision=${14}
runtime_contract_sha256=${15}

case "$environment_name" in
  staging|production) ;;
  *) usage; exit 2 ;;
esac
if [[ ! "$control_plane_digest" =~ ^sha256:[a-f0-9]{64}$ ]] ||
  [[ "$control_plane_digest" == "sha256:0000000000000000000000000000000000000000000000000000000000000000" ]]; then
  echo "control_plane_digest is invalid" >&2
  exit 2
fi
if [[ ! "$authority_image_digest" =~ ^sha256:[a-f0-9]{64}$ ]] ||
  [[ "$authority_image_digest" == "sha256:0000000000000000000000000000000000000000000000000000000000000000" ]]; then
  echo "authority_image_digest is invalid" >&2
  exit 2
fi
if [[ ! "$admission_tools_image" =~ ^[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}$ ]] ||
  [[ "$admission_tools_image" == *@sha256:0000000000000000000000000000000000000000000000000000000000000000 ]]; then
  echo "admission_tools_image is invalid" >&2
  exit 2
fi
if [[ ! "$admission_image" =~ ^[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}$ ]] ||
  [[ "$admission_image" == *@sha256:0000000000000000000000000000000000000000000000000000000000000000 ]]; then
  echo "admission_image is invalid" >&2
  exit 2
fi
[[ $policy_revision =~ ^[1-9][0-9]*$ ]] || {
  echo "policy_revision is invalid" >&2
  exit 2
}
[[ $policy_sha256 =~ ^[a-f0-9]{64}$ ]] && [[ $policy_sha256 != 0000000000000000000000000000000000000000000000000000000000000000 ]] || {
  echo "policy_sha256 is invalid" >&2
  exit 2
}
[[ $pull_credential_generation =~ ^[1-9][0-9]*$ ]] || {
  echo "pull_credential_generation is invalid" >&2
  exit 2
}
if [[ ! $node_ipv4_cidr =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}/([1-9]|[12][0-9]|3[0-2])$ ]] ||
  [[ ! $node_ipv6_cidr =~ : ]] || [[ ! $node_ipv6_cidr =~ /([1-9][0-9]{0,2})$ ]] ||
  (( ${node_ipv6_cidr##*/} > 128 )); then
  echo "exact node pull CIDRs are invalid" >&2
  exit 2
fi
[[ $trusted_base_digest =~ ^sha256:[a-f0-9]{64}$ ]] &&
  [[ $trusted_base_digest != sha256:0000000000000000000000000000000000000000000000000000000000000000 ]] ||
  { echo "trusted_base_digest is invalid" >&2; exit 2; }
for digest_name in frontend_sha256 runtime_contract_sha256; do
  digest=${!digest_name}
  [[ $digest =~ ^[a-f0-9]{64}$ ]] && [[ $digest != 0000000000000000000000000000000000000000000000000000000000000000 ]] ||
    { echo "$digest_name is invalid" >&2; exit 2; }
done
[[ $runtime_contract_revision =~ ^[1-9][0-9]*$ ]] || { echo "runtime_contract_revision is invalid" >&2; exit 2; }
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
pull_readback_placeholder='registry-pull.invalid/mattercodex/control-plane@sha256:0000000000000000000000000000000000000000000000000000000000000000'
if [[ $(grep -F -c "$pull_readback_placeholder" "$raw_render" || true) -ne 1 ]]; then
  echo "supply-chain render must contain one authenticated pull readback input" >&2
  exit 1
fi
tools_placeholder='admission-tools.invalid/mattercodex/image-admission-tools@sha256:0000000000000000000000000000000000000000000000000000000000000000'
admission_placeholder='mattercodex-image-registry.mattercodex-system.svc.cluster.local:5000/mattercodex/image-admission@sha256:0000000000000000000000000000000000000000000000000000000000000000'
authority_placeholder='ghcr.io/codex-k8s/matter-codex/internal-rpc-authority@sha256:0000000000000000000000000000000000000000000000000000000000000000'
authority_replacement="$registry_pull_host/mattercodex/internal-rpc-authority@$authority_image_digest"
tools_digest=${admission_tools_image##*@}
if [[ $(grep -F -c "$tools_placeholder" "$raw_render" || true) -lt 1 ]] ||
  [[ $(grep -F -c "$admission_placeholder" "$raw_render" || true) -ne 1 ]] ||
  [[ $(grep -F -c "$authority_placeholder" "$raw_render" || true) -ne 1 ]] ||
  [[ $(grep -F -c 'mattercodex.dev/admission-tools-sha256: sha256:0000000000000000000000000000000000000000000000000000000000000000' "$raw_render" || true) -ne 1 ]] ||
  [[ $(grep -F -c 'policyRevision: "0"' "$raw_render" || true) -ne 1 ]] ||
  [[ $(grep -F -c 'policySHA256: "0000000000000000000000000000000000000000000000000000000000000000"' "$raw_render" || true) -ne 1 ]]; then
  echo "supply-chain render does not contain the owner admission intent" >&2
  exit 1
fi
if [[ $(grep -F -c 'mattercodex.dev/pull-credential-generation: "0"' "$raw_render" || true) -ne 2 ]] ||
  [[ $(grep -F -c 'name: PULL_CREDENTIAL_GENERATION' "$raw_render" || true) -ne 1 ]]; then
  echo "supply-chain render does not contain the pull credential generation fence" >&2
  exit 1
fi
if [[ $(grep -F -c 'registry-pull.invalid' "$raw_render" || true) -lt 3 ]]; then
  echo "supply-chain render does not bind the pull endpoint consistently" >&2
  exit 1
fi
if [[ $(grep -F -c '192.0.2.0/32' "$raw_render" || true) -ne 2 ]] ||
  [[ $(grep -F -c '2001:db8::/128' "$raw_render" || true) -ne 2 ]]; then
  echo "supply-chain render does not contain the exact node ingress placeholders" >&2
  exit 1
fi

sed \
  -e "s|$digest_placeholder|$digest_replacement|g" \
  -e "s|$pull_readback_placeholder|$digest_replacement|g" \
  -e "s|$tools_placeholder|$admission_tools_image|g" \
  -e "s|$admission_placeholder|$admission_image|g" \
  -e "s|$authority_placeholder|$authority_replacement|g" \
  -e "s|mattercodex.dev/admission-tools-sha256: sha256:0000000000000000000000000000000000000000000000000000000000000000|mattercodex.dev/admission-tools-sha256: $tools_digest|g" \
  -e "s|policyRevision: \"0\"|policyRevision: \"$policy_revision\"|g" \
  -e "s|policySHA256: \"0000000000000000000000000000000000000000000000000000000000000000\"|policySHA256: \"$policy_sha256\"|g" \
  -e "s|mattercodex.dev/pull-credential-generation: \"0\"|mattercodex.dev/pull-credential-generation: \"$pull_credential_generation\"|g" \
  -e "/name: PULL_CREDENTIAL_GENERATION/{n;s|value: \"0\"|value: \"$pull_credential_generation\"|;}" \
  -e "s|registry-pull.invalid|$registry_pull_host|g" \
  -e "s|192.0.2.0/32|$node_ipv4_cidr|g" \
  -e "s|2001:db8::/128|$node_ipv6_cidr|g" \
  -e "s|trustedRoleBaseDigest: sha256:0000000000000000000000000000000000000000000000000000000000000000|trustedRoleBaseDigest: $trusted_base_digest|g" \
  -e "s|frontendSHA256: \"0000000000000000000000000000000000000000000000000000000000000000\"|frontendSHA256: \"$frontend_sha256\"|g" \
  -e "s|roleRuntimeContractRevision: \"1\"|roleRuntimeContractRevision: \"$runtime_contract_revision\"|g" \
  -e "s|roleRuntimeContractSHA256: \"0000000000000000000000000000000000000000000000000000000000000000\"|roleRuntimeContractSHA256: \"$runtime_contract_sha256\"|g" \
  "$raw_render" >"$final_render"

if grep -F -q 'sha256:0000000000000000000000000000000000000000000000000000000000000000' "$final_render" ||
  grep -F -q 'registry-pull.invalid' "$final_render" ||
  grep -F -q '192.0.2.0/32' "$final_render" || grep -F -q '2001:db8::/128' "$final_render"; then
  echo "unresolved supply-chain input remains in render" >&2
  exit 1
fi
cat "$final_render"
