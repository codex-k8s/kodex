#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: render-control-plane.sh staging|production control-plane-sha256 authority-sha256" >&2
}

if [[ $# -ne 3 ]]; then
  usage
  exit 2
fi

environment_name=$1
image_digest=$2
authority_image_digest=$3

case "$environment_name" in
  staging|production) ;;
  *)
    usage
    exit 2
    ;;
esac

for digest_name in image_digest authority_image_digest; do
  digest=${!digest_name}
  if [[ ! "$digest" =~ ^sha256:[a-f0-9]{64}$ ]] ||
    [[ "$digest" == "sha256:0000000000000000000000000000000000000000000000000000000000000000" ]]; then
    echo "$digest_name is invalid" >&2
    exit 2
  fi
done

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

placeholder='ghcr.io/codex-k8s/matter-codex/control-plane@sha256:0000000000000000000000000000000000000000000000000000000000000000'
replacement="ghcr.io/codex-k8s/matter-codex/control-plane@$image_digest"
placeholder_count=$(grep -F -c "$placeholder" "$raw_render" || true)
if [[ "$placeholder_count" -ne 2 ]]; then
  echo "canonical render does not contain exactly two image inputs" >&2
  exit 1
fi

authority_placeholder='ghcr.io/codex-k8s/matter-codex/internal-rpc-authority@sha256:0000000000000000000000000000000000000000000000000000000000000000'
authority_replacement="ghcr.io/codex-k8s/matter-codex/internal-rpc-authority@$authority_image_digest"
authority_placeholder_count=$(grep -F -c "$authority_placeholder" "$raw_render" || true)
if [[ "$authority_placeholder_count" -ne 2 ]]; then
  echo "canonical render does not contain exactly two authority image inputs" >&2
  exit 1
fi

sed \
  -e "s|$placeholder|$replacement|g" \
  -e "s|$authority_placeholder|$authority_replacement|g" \
  "$raw_render" >"$final_render"

if grep -F -q '@sha256:0000000000000000000000000000000000000000000000000000000000000000' "$final_render"; then
  echo "unresolved image digest remains in render" >&2
  exit 1
fi

cat "$final_render"
