#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: render-automation-scheduler.sh staging|production scheduler-sha256 authority-sha256 registry-pull-fqdn" >&2
}

if [[ $# -ne 4 ]]; then
  usage
  exit 2
fi

environment_name=$1
scheduler_digest=$2
authority_digest=$3
registry_pull_host=$4

case "$environment_name" in
  staging|production) ;;
  *)
    usage
    exit 2
    ;;
esac

for digest_name in scheduler_digest authority_digest; do
  digest=${!digest_name}
  if [[ ! "$digest" =~ ^sha256:[a-f0-9]{64}$ ]] ||
    [[ "$digest" == "sha256:0000000000000000000000000000000000000000000000000000000000000000" ]]; then
    echo "$digest_name is invalid" >&2
    exit 2
  fi
done

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
overlay="$repository_root/deploy/k8s/overlays/$environment_name/automation-scheduler"
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
raw_render="$temporary_directory/raw.yaml"
final_render="$temporary_directory/final.yaml"

kubectl kustomize "$overlay" >"$raw_render"

registry_host='mattercodex-image-registry.mattercodex-system.svc.cluster.local:5000'
scheduler_placeholder="$registry_host/mattercodex/automation-scheduler@sha256:0000000000000000000000000000000000000000000000000000000000000000"
scheduler_replacement="$registry_pull_host/mattercodex/automation-scheduler@$scheduler_digest"
if [[ $(grep -F -c "$scheduler_placeholder" "$raw_render" || true) -ne 1 ]]; then
  echo "canonical render does not contain exactly one automation-scheduler image input" >&2
  exit 1
fi

authority_placeholder='ghcr.io/codex-k8s/matter-codex/internal-rpc-authority@sha256:0000000000000000000000000000000000000000000000000000000000000000'
authority_replacement="$registry_pull_host/mattercodex/internal-rpc-authority@$authority_digest"
if [[ $(grep -F -c "$authority_placeholder" "$raw_render" || true) -ne 2 ]]; then
  echo "canonical render does not contain exactly two authority image inputs" >&2
  exit 1
fi

sed \
  -e "s|$scheduler_placeholder|$scheduler_replacement|g" \
  -e "s|$authority_placeholder|$authority_replacement|g" \
  "$raw_render" >"$final_render"

if grep -F -q '@sha256:0000000000000000000000000000000000000000000000000000000000000000' "$final_render"; then
  echo "unresolved image digest remains in render" >&2
  exit 1
fi

cat "$final_render"
