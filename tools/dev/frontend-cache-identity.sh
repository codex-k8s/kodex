#!/usr/bin/env sh
set -eu

# Один формат receipt для доверенного prime и закрытой проверки в Pod.
test -n "${KODEX_DEV_NODE_IMAGE:-}"
runtime=$(node -p 'JSON.stringify([process.version, process.platform, process.arch, process.versions.modules])')
npm_version=$(npm --version)
alpine_version=$(cat /etc/alpine-release)
manifest_digests=$(sha256sum package.json package-lock.json)
{
  printf '%s\n' 'kodex-frontend-cache-v1' "$KODEX_DEV_NODE_IMAGE"
  printf '%s\n' "$runtime" "$npm_version" "$alpine_version" "$manifest_digests"
} | sha256sum | awk '{print $1}'
