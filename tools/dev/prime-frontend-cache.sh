#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex frontend cache prime failed: %s\n' "$*" >&2
  exit 1
}

[[ $# == 2 ]] || fail 'expected source root and cache root'
for command_name in docker flock timeout realpath; do
  command -v "$command_name" >/dev/null || fail "$command_name is required"
done
source_root=$(realpath -- "$1")
cache_root=$(realpath -m -- "$2")
frontend="$source_root/services/staff/control-center"
# Тот же digest применяется renderer к Pod; host Node/npm не используются.
node_image=$(sed -n 's/^FROM \(docker.io\/library\/node:[^ ]*\) AS build$/\1/p' "$frontend/Dockerfile")
[[ "$node_image" =~ @sha256:[a-f0-9]{64}$ ]] || fail 'pinned Node image is absent'
docker image inspect "$node_image" >/dev/null 2>&1 || docker pull "$node_image" >&2
install -d -m 0755 "$cache_root/frontend-v1"
exec 9>"$cache_root/frontend-v1/.prime.lock"
flock -w 600 9 || fail 'cache prime lock timed out'
container_user="$(id -u):$(id -g)"
# В rootless Docker container root отображается в владельца host bind mount.
if docker info --format '{{json .SecurityOptions}}' | grep -q rootless; then
  container_user=0:0
fi
container_args=(--rm --read-only --cap-drop ALL --security-opt no-new-privileges
  --user "$container_user" --tmpfs '/tmp:rw,nosuid,nodev,mode=1777'
  -e HOME=/tmp -e KODEX_DEV_NODE_IMAGE="$node_image"
  -v "$source_root/tools/dev/frontend-cache-identity.sh:/identity.sh:ro")
identity=$(docker run "${container_args[@]}" --network none \
  -v "$frontend:/input:ro" -w /input "$node_image" sh /identity.sh)
[[ "$identity" =~ ^[a-f0-9]{64}$ ]] || fail 'runtime identity is invalid'
destination="$cache_root/frontend-v1/$identity"
if [[ -f "$destination/node_modules/.kodex-cache-identity" ]] &&
  [[ "$(cat "$destination/node_modules/.kodex-cache-identity")" == "$identity" ]]; then
  docker run "${container_args[@]}" --network none \
    -v "$destination:/install:ro" -w /install "$node_image" \
    node --input-type=module -e 'import { transformSync } from "esbuild"; transformSync("const n = 1"); await import("vite")'
  printf '%s\n' "$destination/node_modules"
  exit 0
fi
[[ ! -e "$destination" ]] || fail 'incomplete cache requires explicit removal by trusted host'
staging=$(mktemp -d "$cache_root/frontend-v1/.prime.XXXXXXXX")
cleanup() {
  if [[ -f "$staging/container-id" ]]; then
    docker rm -f "$(cat "$staging/container-id")" >/dev/null 2>&1 || true
  fi
  chmod -R u+rwX "$staging" 2>/dev/null || true
  rm -rf -- "$staging"
}
trap cleanup EXIT
# В контейнер передаются только публичные manifests, без host npmrc/credentials.
cp -- "$frontend/package.json" "$frontend/package-lock.json" "$staging/"
timeout 300s docker run "${container_args[@]}" --cidfile "$staging/container-id" \
  -v "$staging:/install" -w /install \
  "$node_image" sh -ec '
    npm ci --ignore-scripts --include=dev --include=optional --no-audit --no-fund >&2
    node --input-type=module -e '\''import { transformSync } from "esbuild"; transformSync("const n = 1"); await import("vite")'\''
    sh /identity.sh >node_modules/.kodex-cache-identity
  '
rm -- "$staging/container-id"
[[ "$(cat "$staging/node_modules/.kodex-cache-identity")" == "$identity" ]] ||
  fail 'source changed during cache prime'
chmod -R a-w "$staging"
mv -- "$staging" "$destination"
printf '%s\n' "$destination/node_modules"
