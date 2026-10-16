#!/usr/bin/env bash
set -euo pipefail
umask 077

fail() { printf 'Kodex local control plane build failed: %s\n' "$*" >&2; exit 1; }
usage() {
  printf '%s\n' \
    'Usage: build-local-control-plane.sh --source-root <path> --source-sha <sha>' \
    '  --state-directory <path> [--import --context k3d-kodex]' >&2
}

source_root=""
source_sha=""
state_directory=""
context=""
import_image=false
while (($# > 0)); do
  case "$1" in
    --source-root) source_root=${2:-}; shift 2 ;;
    --source-sha) source_sha=${2:-}; shift 2 ;;
    --state-directory) state_directory=${2:-}; shift 2 ;;
    --context) context=${2:-}; shift 2 ;;
    --import) import_image=true; shift ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done
for command_name in docker git jq tar timeout realpath; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$source_root" == /* && -d "$source_root" && ! -L "$source_root" ]] || fail 'source root is invalid'
source_root=$(realpath "$source_root")
[[ -f "$source_root/services/internal/control-plane/Dockerfile" ]] || fail 'control plane Dockerfile is absent'
[[ "$source_sha" =~ ^[a-f0-9]{40}$ && "$(git -C "$source_root" rev-parse HEAD)" == "$source_sha" ]] ||
  fail 'exact current source commit is required'
[[ -z "$(git -C "$source_root" status --porcelain --untracked-files=normal)" ]] || fail 'source worktree must be clean'
[[ "$state_directory" == /* && "$state_directory" != / && ! -L "$state_directory" ]] || fail 'state directory is invalid'
state_directory=$(realpath -m "$state_directory")
[[ "$state_directory" != "$source_root" && "$state_directory" != "$source_root/"* ]] || fail 'state directory must be outside source root'
if "$import_image"; then
  [[ "$context" == k3d-kodex ]] || fail 'only the explicit local Kodex context is supported'
fi

install -d -m 0700 "$state_directory/cache"
builder=kodex-local-dev
"$source_root/tools/dev/ensure-local-buildx-builder.sh" "$builder"
repository=registry.local.kodex/kodex/control-plane
tag="$repository:local-$source_sha"
archive="$state_directory/cache/control-plane-$source_sha.oci.tar"
[[ ! -L "$archive" && ! -L "$archive.next" ]] || fail 'archive symlink is forbidden'
if [[ ! -s "$archive" ]]; then
  # Только отслеживаемые исходники точного commit; env и рабочие файлы не входят в context.
  git -C "$source_root" archive "$source_sha" -- libs/go services/internal/control-plane |
    timeout 1800 docker buildx build --builder "$builder" \
      --file services/internal/control-plane/Dockerfile --target runtime \
      --platform linux/amd64 --provenance=false --sbom=false \
      --build-arg "VERSION=$source_sha" \
      --label "org.opencontainers.image.revision=$source_sha" \
      --tag "$tag" --output "type=oci,dest=$archive.next" -
  [[ -s "$archive.next" ]] || fail 'control plane OCI archive was not produced'
  mv "$archive.next" "$archive"
fi
manifest_digest=$(tar -xOf "$archive" index.json | jq -er '
  if (.manifests | length) != 1 then error("one image manifest is required")
  else .manifests[0].digest end
') || fail 'OCI manifest digest is unavailable'
[[ "$manifest_digest" =~ ^sha256:[a-f0-9]{64}$ ]] || fail 'OCI manifest digest is invalid'
exact_reference="$repository@$manifest_digest"
if "$import_image"; then
  "$source_root/tools/dev/import-local-image.sh" --context "$context" --archive "$archive" \
    --repository "$repository" --tag "$tag" --exact-reference "$exact_reference"
fi
printf '%s\n' "$exact_reference" >"$state_directory/control-plane-image"
printf '%s\n' "$source_sha" >"$state_directory/control-plane-source-sha"
printf 'Kodex local control plane image ready: %s\n' "$exact_reference"
