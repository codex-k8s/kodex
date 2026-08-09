#!/usr/bin/env bash
set -euo pipefail

version=v4.53.3
expected_sha256=fa52a4e758c63d38299163fbdd1edfb4c4963247918bf9c1c5d31d84789eded4
destination=${1:-/var/run/mattercodex-tools/yq}
temporary_file=$(mktemp)
trap 'rm -f -- "$temporary_file"' EXIT
curl --fail --silent --show-error --location \
  "https://github.com/mikefarah/yq/releases/download/$version/yq_linux_amd64" --output "$temporary_file"
printf '%s  %s\n' "$expected_sha256" "$temporary_file" | sha256sum --check --status || {
  printf 'yq checksum mismatch\n' >&2
  exit 1
}
install -m 0550 "$temporary_file" "$destination"
printf 'Pinned yq installed: %s\n' "$version"
