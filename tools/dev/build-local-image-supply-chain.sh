#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex local image supply-chain build failed: %s\n' "$*" >&2
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

[[ "$source_root" == /* && -f "$source_root/tools/dev/Dockerfile.local-image-supply-chain" &&
  -f "$source_root/services/jobs/role-image-builder/Dockerfile" &&
  -f "$source_root/services/internal/internal-rpc-authority/Dockerfile" ]] ||
  fail 'source root is invalid'
[[ "$state_directory" == /* && "$state_directory" != / && "$state_directory" != "$HOME" ]] ||
  fail 'state directory is invalid'
for command_name in docker git jq k3s sha256sum sudo tar; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
docker buildx version >/dev/null 2>&1 || fail 'docker buildx is required'
[[ -S /run/k3s/containerd/containerd.sock ]] || fail 'local k3s containerd socket is absent'
sudo -n true >/dev/null 2>&1 || fail 'passwordless sudo is required for local k3s image import'

builder=kodex-local-dev
"$source_root/tools/dev/ensure-local-buildx-builder.sh" "$builder"

install -d -m 0700 "$state_directory/cache/image-supply-chain"
source_revision=$(git -C "$source_root" rev-parse HEAD)
[[ "$source_revision" =~ ^[a-f0-9]{40}$ ]] || fail 'source revision is invalid'
input_digest=$(
  tar --sort=name --mtime='UTC 1970-01-01' --owner=0 --group=0 --numeric-owner \
    -C "$source_root" -cf - \
	    tools/dev/Dockerfile.local-image-supply-chain \
	    infra/dockerfile-frontend/Dockerfile \
	    tools/render-image-admission-job.sh \
    infra/admission-tools/Dockerfile \
    services/jobs/role-image-builder \
    services/internal/internal-rpc-authority \
    libs/go |
    sha256sum | awk '{print $1}'
)
[[ "$input_digest" =~ ^[a-f0-9]{64}$ ]] || fail 'supply-chain input digest is invalid'

import_oci() {
  local archive=$1 tag=$2 repository=$3 manifest_digest exact_reference
  manifest_digest=$(tar -xOf "$archive" index.json | jq -er '
    if (.manifests | length) != 1 then error("one image manifest is required")
    else .manifests[0].digest end
  ') || fail "OCI manifest digest is unavailable: $repository"
  [[ "$manifest_digest" =~ ^sha256:[a-f0-9]{64}$ ]] ||
    fail "OCI manifest digest is invalid: $repository"
  exact_reference="$repository@$manifest_digest"
  sudo -n k3s ctr -n k8s.io images import \
    --base-name "$repository" "$archive" >/dev/null
  sudo -n k3s ctr -n k8s.io images tag --force \
    "$tag" "$exact_reference" >/dev/null
  printf '%s' "$exact_reference"
}

build_target() {
  local name=$1 dockerfile=$2 target=$3 repository=$4
  shift 4
  local tag archive next_archive exact_reference
  tag="$repository:local-$input_digest"
  archive="$state_directory/cache/image-supply-chain/$name-$input_digest.oci.tar"
  if [[ ! -s "$archive" ]]; then
    next_archive="$archive.next"
    rm -f -- "$next_archive"
    docker buildx build --builder "$builder" \
      --file "$source_root/$dockerfile" --target "$target" \
      --platform linux/amd64 --provenance=false --sbom=false \
      --tag "$tag" --output "type=oci,dest=$next_archive" \
      "$@" "$source_root"
    [[ -s "$next_archive" ]] || fail "OCI archive was not produced: $name"
    mv -- "$next_archive" "$archive"
  fi
  exact_reference=$(import_oci "$archive" "$tag" "$repository")
  printf '%s\n' "$exact_reference" >"$state_directory/$name-image"
  chmod 0600 "$state_directory/$name-image"
}

build_target image-admission-tools tools/dev/Dockerfile.local-image-supply-chain \
  admission-tools registry.local.kodex/kodex/image-admission-tools \
  --build-arg "SOURCE_SHA=$source_revision"
build_target image-admission tools/dev/Dockerfile.local-image-supply-chain \
  image-admission registry.local.kodex/kodex/image-admission \
  --build-arg "SOURCE_SHA=$source_revision"
build_target role-image-builder services/jobs/role-image-builder/Dockerfile \
  runtime registry.local.kodex/kodex/role-image-builder \
  --build-arg "VERSION=local-$source_revision"
build_target internal-rpc-authority services/internal/internal-rpc-authority/Dockerfile \
  runtime registry.local.kodex/kodex/internal-rpc-authority \
  --build-arg "VERSION=local-$source_revision"

tools_tag="kodex-local/image-admission-tools:$input_digest"
docker buildx build --builder "$builder" \
  --file "$source_root/tools/dev/Dockerfile.local-image-supply-chain" \
  --target admission-tools --platform linux/amd64 --provenance=false --sbom=false \
  --build-arg "SOURCE_SHA=$source_revision" --tag "$tools_tag" --load "$source_root" >/dev/null
printf '%s\n' "$tools_tag" >"$state_directory/image-supply-chain-tools-docker-tag"

role_input_directory="$state_directory/cache/image-supply-chain/role-input-$source_revision"
role_input_archive="$state_directory/cache/image-supply-chain/role-input-$source_revision.oci.tar"
role_input_metadata="$state_directory/role-image-input.json"
if [[ ! -s "$role_input_archive" || ! -s "$role_input_metadata" ]]; then
  rm -rf -- "$role_input_directory"
  install -d -m 0700 "$role_input_directory/payload/.kodex" "$role_input_directory/layout"
  source_sha256=$(printf '%s' "$source_revision" | sha256sum | awk '{print $1}')
  printf '%s' "$source_sha256" >"$role_input_directory/payload/.kodex/source.sha256"
  tar --sort=name --mtime='UTC 1970-01-01' --owner=0 --group=0 --numeric-owner \
    -C "$role_input_directory/payload" -cf "$role_input_directory/payload.tar" .kodex/source.sha256
  payload_sha256=$(sha256sum "$role_input_directory/payload.tar" | awk '{print $1}')
  printf '{}' >"$role_input_directory/config.json"
  # The image is non-root by default. Root inside the rootless Docker user
  # namespace maps to the daemon owner and can safely write this private bind
  # mount; forcing the host numeric UID maps to an unwritable subordinate UID.
  manifest_digest=$(docker run --rm --user 0:0 \
    -v "$role_input_directory:/work" "$tools_tag" \
    regctl artifact put \
      --config-type application/vnd.kodex.role-image-input.config.v1+json \
      --config-file /work/config.json \
      --file-media-type application/vnd.kodex.role-image-input.v1 \
      --file /work/payload.tar \
      --format '{{ .Manifest.GetDescriptor.Digest }}' \
      "ocidir:///work/layout:$source_revision")
  [[ "$manifest_digest" =~ ^sha256:[a-f0-9]{64}$ ]] ||
    fail 'role image input manifest digest is invalid'
  docker run --rm --user 0:0 \
    -v "$role_input_directory:/work" "$tools_tag" \
    regctl image export "ocidir:///work/layout:$source_revision" /work/role-input.oci.tar
  install -m 0600 "$role_input_directory/role-input.oci.tar" "$role_input_archive"
  jq -n --arg manifest_digest "$manifest_digest" --arg payload_sha256 "$payload_sha256" \
    --arg source_sha256 "$source_sha256" --arg source_revision "$source_revision" '
      {version:1,manifestDigest:$manifest_digest,payloadSha256:$payload_sha256,
       sourceSha256:$source_sha256,sourceRevision:$source_revision}
    ' >"$role_input_metadata"
  chmod 0600 "$role_input_metadata"
fi

printf 'Kodex local image supply-chain images are ready for source %s\n' "$source_revision"
