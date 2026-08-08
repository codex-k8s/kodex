#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: render-egress-gateway.sh staging|production egress-gateway-sha256 registry-pull-fqdn" >&2
}

if [[ $# -ne 3 ]]; then
  usage
  exit 2
fi

environment_name=$1
gateway_digest=$2
registry_pull_host=$3

case "$environment_name" in
  staging|production) ;;
  *)
    usage
    exit 2
    ;;
esac

if [[ ! "$gateway_digest" =~ ^sha256:[a-f0-9]{64}$ ]] ||
  [[ "$gateway_digest" == "sha256:0000000000000000000000000000000000000000000000000000000000000000" ]]; then
  echo "gateway_digest is invalid" >&2
  exit 2
fi

valid_registry_pull_host() {
  local host=$1
  local label
  local -a labels=()

  [[ ${#host} -le 253 ]] || return 1
  [[ "$host" == *.* ]] || return 1
  [[ "$host" != .* && "$host" != *. && "$host" != *..* ]] || return 1
  [[ "$host" != *.svc && "$host" != *.svc.cluster.local ]] || return 1

  IFS='.' read -r -a labels <<<"$host"
  ((${#labels[@]} >= 2)) || return 1
  for label in "${labels[@]}"; do
    [[ ${#label} -le 63 ]] || return 1
    [[ "$label" =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?$ ]] || return 1
  done
}

if ! valid_registry_pull_host "$registry_pull_host"; then
  echo "registry_pull_host must be a node-reachable exact DNS name" >&2
  exit 2
fi

if ! command -v kubectl >/dev/null 2>&1; then
  echo "kubectl is required for the canonical render" >&2
  exit 1
fi

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
repository_root=$(cd -- "$script_dir/.." && pwd)
overlay="$repository_root/deploy/k8s/overlays/$environment_name/egress-gateway"
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
raw_render="$temporary_directory/raw.yaml"
final_render="$temporary_directory/final.yaml"

kubectl kustomize "$overlay" >"$raw_render"

registry_host='mattercodex-image-registry.mattercodex-system.svc.cluster.local:5000'
placeholder="$registry_host/mattercodex/egress-gateway@sha256:0000000000000000000000000000000000000000000000000000000000000000"
replacement="$registry_pull_host/mattercodex/egress-gateway@$gateway_digest"
if [[ $(grep -F -c "$placeholder" "$raw_render" || true) -ne 1 ]]; then
  echo "canonical render does not contain exactly one egress-gateway image input" >&2
  exit 1
fi

sed "s|$placeholder|$replacement|g" "$raw_render" >"$final_render"

if grep -F -q '@sha256:0000000000000000000000000000000000000000000000000000000000000000' "$final_render"; then
  echo "unresolved image digest remains in render" >&2
  exit 1
fi

cat "$final_render"
