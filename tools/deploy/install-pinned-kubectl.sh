#!/usr/bin/env bash
set -euo pipefail

version=v1.35.5
expected_sha256=90f75ea6ecc9ea5633262e1c0b83a40560003b30fc94a04cb099404fcef0c224
destination=${1:-/var/run/mattercodex-tools/kubectl}
temporary_file=$(mktemp)
trap 'rm -f -- "$temporary_file"' EXIT
curl --fail --silent --show-error --location "https://dl.k8s.io/release/$version/bin/linux/amd64/kubectl" --output "$temporary_file"
printf '%s  %s\n' "$expected_sha256" "$temporary_file" | sha256sum --check --status || {
  printf 'kubectl checksum mismatch\n' >&2
  exit 1
}
install -m 0550 "$temporary_file" "$destination"
printf 'Pinned kubectl installed: %s\n' "$version"
