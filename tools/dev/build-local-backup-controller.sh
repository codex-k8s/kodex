#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex local backup-controller build failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf 'Usage: %s --source-root <path> --state-directory <path>\n' "$0" >&2
}

source_root=""
state_directory=""
while (($# > 0)); do
  case "$1" in
    --source-root) source_root=${2:-}; shift 2 ;;
    --state-directory) state_directory=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ "$source_root" == /* && -f "$source_root/services/jobs/backup-controller/Dockerfile" ]] ||
  fail 'source root is invalid'
[[ "$state_directory" == /* && "$state_directory" != / ]] ||
  fail 'state directory is invalid'
for command_name in docker jq k3s sha256sum sudo tar; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
docker buildx version >/dev/null 2>&1 || fail 'docker buildx is required'
[[ -S /run/k3s/containerd/containerd.sock ]] || fail 'local k3s containerd socket is absent'
sudo -n true >/dev/null 2>&1 || fail 'passwordless sudo is required for local k3s image import'

builder=kodex-local-dev
"$source_root/tools/dev/ensure-local-buildx-builder.sh" "$builder"

install -d -m 0700 "$state_directory/cache"
input_digest=$(
  tar --sort=name --mtime='UTC 1970-01-01' --owner=0 --group=0 --numeric-owner \
    -C "$source_root" -cf - services/jobs/backup-controller \
    libs/go/grpcserver libs/go/httpserver libs/go/observability libs/go/securefile \
    libs/go/serviceruntime |
    sha256sum | awk '{print $1}'
)
[[ "$input_digest" =~ ^[a-f0-9]{64}$ ]] || fail 'backup-controller input digest is invalid'

repository=registry.local.kodex/kodex/backup-controller
tag="$repository:local-$input_digest"
archive="$state_directory/cache/backup-controller-$input_digest.oci.tar"
if [[ ! -s "$archive" ]]; then
  next_archive="$archive.next"
  rm -f "$next_archive"
  docker buildx build --builder "$builder" \
    --file "$source_root/services/jobs/backup-controller/Dockerfile" \
    --platform linux/amd64 \
    --provenance=false \
    --sbom=false \
    --tag "$tag" \
    --output "type=oci,dest=$next_archive" \
    "$source_root"
  [[ -s "$next_archive" ]] || fail 'backup-controller OCI archive was not produced'
  mv "$next_archive" "$archive"
fi

manifest_digest=$(tar -xOf "$archive" index.json | jq -er '
  if (.manifests | length) != 1 then error("one image manifest is required")
  else .manifests[0].digest end
') || fail 'backup-controller OCI manifest digest is unavailable'
[[ "$manifest_digest" =~ ^sha256:[a-f0-9]{64}$ ]] ||
  fail 'backup-controller OCI manifest digest is invalid'
exact_reference="$repository@$manifest_digest"

sudo -n k3s ctr -n k8s.io images import \
  --base-name "$repository" "$archive" >/dev/null
sudo -n k3s ctr -n k8s.io images tag --force \
  "$tag" "$exact_reference" >/dev/null

printf '%s\n' "$exact_reference" >"$state_directory/backup-controller-image"
chmod 0600 "$state_directory/backup-controller-image"
printf 'Kodex local backup-controller image ready: %s\n' "$exact_reference"
