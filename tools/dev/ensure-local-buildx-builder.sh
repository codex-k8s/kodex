#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex local Buildx bootstrap failed: %s\n' "$*" >&2
  exit 1
}

builder=${1:-}
[[ "$builder" =~ ^[a-z0-9][a-z0-9._-]{0,63}$ ]] || fail 'builder name is invalid'
command -v docker >/dev/null 2>&1 || fail 'docker is required'
docker buildx version >/dev/null 2>&1 || fail 'docker buildx is required'

inspect_output=$(docker buildx inspect "$builder" --bootstrap 2>&1 || true)
if grep -Eq '^Status:[[:space:]]+running$' <<<"$inspect_output"; then
  exit 0
fi

context=$(docker context show 2>/dev/null) || fail 'current Docker context is unavailable'
[[ "$context" =~ ^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$ ]] || fail 'current Docker context is invalid'
docker context inspect "$context" >/dev/null 2>&1 || fail 'current Docker context is unavailable'

# A system reinstall or a rootful/rootless switch can leave Buildx metadata
# pointing at a socket that no longer exists. Local builder state is disposable.
docker buildx rm --force "$builder" >/dev/null 2>&1 || true
docker buildx create --name "$builder" --driver docker-container "$context" >/dev/null ||
  fail 'create builder for the current Docker context'
inspect_output=$(docker buildx inspect "$builder" --bootstrap 2>&1 || true)
grep -Eq '^Status:[[:space:]]+running$' <<<"$inspect_output" || fail 'builder bootstrap failed'
