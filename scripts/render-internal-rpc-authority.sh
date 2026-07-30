#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'internal-rpc-authority render failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf 'Usage: %s --environment staging|production --image-ref <repository@sha256:digest>\n' "$0" >&2
}

environment_name=""
image_ref=""
while (($# > 0)); do
  case "$1" in
    --environment)
      (($# >= 2)) || fail "--environment requires a value"
      environment_name="$2"
      shift 2
      ;;
    --image-ref)
      (($# >= 2)) || fail "--image-ref requires a value"
      image_ref="$2"
      shift 2
      ;;
    --help)
      usage
      exit 0
      ;;
    *)
      fail "unsupported argument: $1"
      ;;
  esac
done

case "$environment_name" in
  staging | production) ;;
  *)
    usage
    fail "environment must be staging or production"
    ;;
esac

expected_repository="ghcr.io/codex-k8s/matter-codex/internal-rpc-authority"
case "$image_ref" in
  "${expected_repository}@sha256:"????????????????????????????????????????????????????????????????) ;;
  *) fail "image reference must use the registered repository and exact sha256 digest" ;;
esac
digest="${image_ref##*@sha256:}"
[[ "$digest" =~ ^[a-f0-9]{64}$ ]] || fail "image digest must be 64 lowercase hexadecimal characters"
[[ "$digest" != "0000000000000000000000000000000000000000000000000000000000000000" ]] ||
  fail "zero image digest is a fail-closed source placeholder"

command -v kubectl >/dev/null 2>&1 || fail "kubectl is required"

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
repo_root="$(cd -- "$script_dir/.." && pwd -P)"
overlay="$repo_root/deploy/k8s/overlays/$environment_name/internal-rpc-authority"
placeholder="${expected_repository}@sha256:0000000000000000000000000000000000000000000000000000000000000000"

rendered="$(kubectl kustomize "$overlay")" || fail "kustomize render failed"
occurrences="$(grep -Fc "image: $placeholder" <<<"$rendered" || true)"
[[ "$occurrences" == "7" ]] ||
  fail "render must contain exactly seven registered deployable image placeholders"

awk -v placeholder="$placeholder" -v replacement="$image_ref" '
  {
    sub("image: " placeholder "$", "image: " replacement)
    print
  }
' <<<"$rendered"
