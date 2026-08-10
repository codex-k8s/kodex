#!/usr/bin/env bash
set -euo pipefail

version=v2.15.0
expected_sha256=7d55eda757dc9f233675a3038fcf8779bcda99753b1603f7009fc8537e126b7e
destination=${1:-/var/run/mattercodex-tools/nsc}
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
umask 077

curl --fail --silent --show-error --location \
  "https://github.com/nats-io/nsc/releases/download/$version/nsc-linux-amd64.zip" \
  --output "$temporary_directory/nsc.zip"
printf '%s  %s\n' "$expected_sha256" "$temporary_directory/nsc.zip" |
  sha256sum --check --status || {
    printf 'nsc checksum mismatch\n' >&2
    exit 1
  }
unzip -qq "$temporary_directory/nsc.zip" -d "$temporary_directory/unpacked"
nsc_binary=$(find "$temporary_directory/unpacked" -type f -name nsc -print -quit)
[[ -n "$nsc_binary" ]] || {
  printf 'nsc archive layout mismatch\n' >&2
  exit 1
}
install -m 0550 "$nsc_binary" "$destination"
printf 'Pinned nsc installed: %s\n' "$version"
