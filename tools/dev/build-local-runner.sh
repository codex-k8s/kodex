#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex local runner build failed: %s\n' "$*" >&2
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

[[ "$source_root" == /* && -f "$source_root/services/jobs/agent-runner/Dockerfile" ]] ||
  fail 'source root is invalid'
[[ "$state_directory" == /* && "$state_directory" != / ]] || fail 'state directory is invalid'
for command_name in docker jq sha256sum tar; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
docker buildx version >/dev/null 2>&1 || fail 'docker buildx is required'
[[ -S /run/k3s/containerd/containerd.sock ]] || fail 'local k3s containerd socket is absent'

builder=kodex-local-dev
if ! docker buildx inspect "$builder" >/dev/null 2>&1; then
  docker buildx create --name "$builder" --driver docker-container >/dev/null
fi
docker buildx inspect "$builder" --bootstrap >/dev/null

install -d -m 0700 "$state_directory/cache"
input_digest=$(
  tar --sort=name --mtime='UTC 1970-01-01' --owner=0 --group=0 --numeric-owner \
    -C "$source_root" -cf - services/jobs/agent-runner libs/go |
    sha256sum | awk '{print $1}'
)
[[ "$input_digest" =~ ^[a-f0-9]{64}$ ]] || fail 'runner input digest is invalid'

repository=registry.local.kodex/kodex/agent-runner
tag="$repository:local-$input_digest"
archive="$state_directory/cache/agent-runner-$input_digest.oci.tar"
if [[ ! -s "$archive" ]]; then
  next_archive="$archive.next"
  rm -f "$next_archive"
  docker buildx build --builder "$builder" \
    --file "$source_root/services/jobs/agent-runner/Dockerfile" \
    --target local-runtime \
    --platform linux/amd64 \
    --provenance=false \
    --sbom=false \
    --tag "$tag" \
    --output "type=oci,dest=$next_archive" \
    "$source_root"
  [[ -s "$next_archive" ]] || fail 'runner OCI archive was not produced'
  mv "$next_archive" "$archive"
fi

manifest_digest=$(tar -xOf "$archive" index.json | jq -er '
  if (.manifests | length) != 1 then error("one image manifest is required")
  else .manifests[0].digest end
') || fail 'runner OCI manifest digest is unavailable'
[[ "$manifest_digest" =~ ^sha256:[a-f0-9]{64}$ ]] || fail 'runner OCI manifest digest is invalid'
exact_reference="$repository@$manifest_digest"

docker run --rm --user 0 \
  -v /run/k3s/containerd/containerd.sock:/run/k3s/containerd/containerd.sock \
  -v "$archive:/image.oci.tar:ro" \
  --entrypoint /bin/ctr docker.io/rancher/k3s:v1.36.1-k3s1 \
  --address /run/k3s/containerd/containerd.sock -n k8s.io images import \
  --base-name "$repository" /image.oci.tar >/dev/null
docker run --rm --user 0 \
  -v /run/k3s/containerd/containerd.sock:/run/k3s/containerd/containerd.sock \
  --entrypoint /bin/ctr docker.io/rancher/k3s:v1.36.1-k3s1 \
  --address /run/k3s/containerd/containerd.sock -n k8s.io images tag --force \
  "$tag" "$exact_reference" >/dev/null

printf '%s\n' "$exact_reference" >"$state_directory/agent-runner-image"
chmod 0600 "$state_directory/agent-runner-image"
printf 'Kodex local runner image ready: %s\n' "$exact_reference"
