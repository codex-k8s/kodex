#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)

fail() {
  printf 'Local image cache import contract failed: %s\n' "$*" >&2
  exit 1
}

for builder in build-local-runner.sh build-local-session-archive.sh build-local-backup-controller.sh; do
  path="$root/tools/dev/$builder"
  grep -Fq 'images import \' "$path" || fail "$builder does not import its cached OCI archive"
  grep -Fq -- '--base-name "$repository" /image.oci.tar' "$path" ||
    fail "$builder does not restore the exact repository tag from cache"
  import_line=$(grep -n -F 'images import \' "$path" | cut -d: -f1)
  tag_line=$(grep -n -F 'images tag --force' "$path" | cut -d: -f1)
  [[ "$import_line" =~ ^[0-9]+$ && "$tag_line" =~ ^[0-9]+$ && "$import_line" -lt "$tag_line" ]] ||
    fail "$builder tags the exact digest before restoring its source tag"
done

printf 'Local image cache import contract passed.\n'
