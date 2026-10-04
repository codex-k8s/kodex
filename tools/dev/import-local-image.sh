#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex local image import failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    'Usage: import-local-image.sh --context <context> --archive <oci.tar>' \
    '  --repository <registry/repository> --tag <tagged-reference>' \
    '  --exact-reference <digest-reference>' >&2
}

context=""
archive=""
repository=""
tag=""
exact_reference=""
while (($# > 0)); do
  case "$1" in
    --context) context=${2:-}; shift 2 ;;
    --archive) archive=${2:-}; shift 2 ;;
    --repository) repository=${2:-}; shift 2 ;;
    --tag) tag=${2:-}; shift 2 ;;
    --exact-reference) exact_reference=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$context" && "$(kubectl config current-context)" == "$context" ]] ||
  fail 'Kubernetes context mismatch'
[[ "${context,,}" != *prod* && "${context,,}" != *production* ]] ||
  fail 'production context is forbidden'
[[ "$archive" == /* && -s "$archive" && ! -L "$archive" ]] || fail 'OCI archive is invalid'
[[ "$repository" =~ ^[a-z0-9][a-z0-9./_-]+$ ]] || fail 'repository is invalid'
[[ "$tag" == "$repository":* ]] || fail 'tagged reference does not match repository'
[[ "$exact_reference" == "$repository"@sha256:* ]] ||
  fail 'exact reference does not match repository'
manifest_digest=${exact_reference#*@}
[[ "$manifest_digest" =~ ^sha256:[a-f0-9]{64}$ ]] || fail 'manifest digest is invalid'

if [[ "$context" == k3d-* ]]; then
  for command_name in docker jq k3d kubectl sha256sum; do
    command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
  done
  cluster=${context#k3d-}
  k3d image import "$archive" --cluster "$cluster" --mode direct --keep-tarball >/dev/null
  mapfile -t nodes < <(k3d node list -o json | jq -r --arg cluster "$cluster" '
    .[] | select(.name | startswith("k3d-" + $cluster + "-")) |
    select(.role == "server" or .role == "agent") | .name
  ' | sort)
  ((${#nodes[@]} > 0)) || fail 'k3d workload nodes are absent'
  for node in "${nodes[@]}"; do
    docker exec "$node" ctr -n k8s.io images tag --force "$tag" "$exact_reference" >/dev/null
    docker exec "$node" ctr -n k8s.io images list --quiet | grep -Fx "$exact_reference" >/dev/null ||
      fail 'imported immutable image reference is absent from k3d node'
    docker exec "$node" ctr -n k8s.io content get "$manifest_digest" | sha256sum |
      awk -v expected="${manifest_digest#sha256:}" '$1 == expected { found=1 } END { exit !found }' ||
      fail 'imported k3d image manifest digest mismatch'
  done
else
  for command_name in k3s kubectl sha256sum sudo; do
    command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
  done
  [[ -S /run/k3s/containerd/containerd.sock ]] || fail 'local k3s containerd socket is absent'
  sudo -n true >/dev/null 2>&1 || fail 'passwordless sudo is required for local k3s image import'
  sudo -n k3s ctr -n k8s.io images import --base-name "$repository" "$archive" >/dev/null
  sudo -n k3s ctr -n k8s.io images tag --force "$tag" "$exact_reference" >/dev/null
  sudo -n k3s ctr -n k8s.io images list --quiet | grep -Fx "$exact_reference" >/dev/null ||
    fail 'imported immutable image reference is absent'
  sudo -n k3s ctr -n k8s.io content get "$manifest_digest" | sha256sum |
    awk -v expected="${manifest_digest#sha256:}" '$1 == expected { found=1 } END { exit !found }' ||
    fail 'imported image manifest digest mismatch'
fi

printf 'Kodex local image imported: %s\n' "$exact_reference"
