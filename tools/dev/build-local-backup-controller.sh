#!/usr/bin/env bash
set -euo pipefail

# Библиотека выбирается рядом с этим скриптом через Bash builtins до разбора CLI.
# shellcheck source=lib/local-image-build.sh
source "${BASH_SOURCE[0]%"${BASH_SOURCE[0]##*/}"}lib/local-image-build.sh"

fail() {
  printf 'Kodex local backup-controller build failed: %s\n' "$*" >&2
  exit 1
}

source_root="" state_directory="" context=""
kodex_local_image_build_parse source_root state_directory context "$@"
kodex_local_image_build_validate "$source_root" "$state_directory" "$context" \
  "services/jobs/backup-controller/Dockerfile"

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

manifest_digest=$(kodex_local_image_manifest_digest "$archive") || fail 'backup-controller OCI manifest digest is unavailable'
[[ "$manifest_digest" =~ ^sha256:[a-f0-9]{64}$ ]] ||
  fail 'backup-controller OCI manifest digest is invalid'
exact_reference="$repository@$manifest_digest"

"$source_root/tools/dev/import-local-image.sh" --context "$context" --archive "$archive" \
  --repository "$repository" --tag "$tag" --exact-reference "$exact_reference" >/dev/null

printf '%s\n' "$exact_reference" >"$state_directory/backup-controller-image"
chmod 0600 "$state_directory/backup-controller-image"
printf 'Kodex local backup-controller image ready: %s\n' "$exact_reference"
