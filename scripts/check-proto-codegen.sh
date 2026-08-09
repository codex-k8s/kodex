#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "${script_dir}/.." && pwd)"
generated_paths=(
  "libs/go/internalrpcauth/gen/internalrpcauthority/v1"
  "libs/go/controlplaneapi/gen/controlplane/v1"
  "libs/go/interactiongatewayapi/gen/interactiongateway/v1"
)
temporary_root="$(mktemp -d)"

cleanup() {
  rm -rf -- "${temporary_root}"
}
trap cleanup EXIT

(
  cd -- "${repo_root}"
  buf generate --output "${temporary_root}"
)

for generated_path in "${generated_paths[@]}"; do
  if ! diff -ruN \
    "${repo_root}/${generated_path}" \
    "${temporary_root}/${generated_path}"; then
    echo "generated Proto code is stale: ${generated_path}; run make gen-proto" >&2
    exit 1
  fi
done
