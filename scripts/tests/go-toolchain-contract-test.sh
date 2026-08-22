#!/usr/bin/env bash
set -euo pipefail

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
guard="$repository_root/scripts/check-go-toolchain.sh"

"$guard" >/dev/null

jq -e '
  .schema_version == 2 and
  (.images | length > 0) and
  ([.images[].component] | length == (unique | length)) and
  all(.images[];
    (.component | test("^[a-z0-9-]+$")) and
    (.dockerfile | startswith("services/")) and
    ((has("target") | not) or
      (.component == "role-image-builder" and .target == "runtime") or
      (.component == "image-admission" and .target == "admission-runtime")))
' "$repository_root/tools/release/images.json" >/dev/null || {
  printf 'Release image inventory is invalid\n' >&2
  exit 1
}

while IFS=$'\t' read -r component dockerfile; do
  [[ -f "$repository_root/$dockerfile" ]] || {
    printf 'Dockerfile is absent for %s\n' "$component" >&2
    exit 1
  }
done < <(jq -r '.images[] | [.component,.dockerfile] | @tsv' "$repository_root/tools/release/images.json")

if rg -q 'bot-service|legacy-data-migration' \
  "$repository_root/tools/release/images.json" "$repository_root/Makefile"; then
  printf 'Retired unit returned to an active entrypoint\n' >&2
  exit 1
fi

printf 'Go toolchain contract tests passed\n'
