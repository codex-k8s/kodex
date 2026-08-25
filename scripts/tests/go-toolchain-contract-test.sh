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
  dockerfile_path="$repository_root/$dockerfile"
  [[ -f "$dockerfile_path" ]] || {
    printf 'Dockerfile is absent for %s\n' "$component" >&2
    exit 1
  }

  module_directory=$(dirname -- "$dockerfile_path")
  module_file="$module_directory/go.mod"
  [[ -f "$module_file" ]] && grep -Fq 'go mod download' "$dockerfile_path" || continue

  while IFS= read -r replacement_path; do
    [[ -n "$replacement_path" ]] || continue
    replacement_directory=$(realpath -m -- "$module_directory/$replacement_path")
    [[ "$replacement_directory" == "$repository_root"/* && -f "$replacement_directory/go.mod" ]] || {
      printf 'Local Go replacement is invalid for %s: %s\n' "$component" "$replacement_path" >&2
      exit 1
    }
    repository_relative_path=${replacement_directory#"$repository_root/"}
    if ! grep -Fq "COPY $repository_relative_path/" "$dockerfile_path" &&
       ! grep -Fq "COPY $repository_relative_path/go.mod " "$dockerfile_path" &&
       ! grep -Fq 'COPY libs/go ' "$dockerfile_path" &&
       ! grep -Fq 'COPY libs/go/ ' "$dockerfile_path"; then
      printf 'Dockerfile does not materialize local Go replacement for %s: %s\n' \
        "$component" "$repository_relative_path" >&2
      exit 1
    fi
  done < <(awk \
    '$1 == "replace" && $2 ~ /^github\.com\/codex-k8s\/kodex\/libs\/go\// && $3 == "=>" {print $4}' \
    "$module_file")
done < <(jq -r '.images[] | [.component,.dockerfile] | @tsv' "$repository_root/tools/release/images.json")

if rg -q 'bot-service|legacy-data-migration' \
  "$repository_root/tools/release/images.json" "$repository_root/Makefile"; then
  printf 'Retired unit returned to an active entrypoint\n' >&2
  exit 1
fi

printf 'Go toolchain contract tests passed\n'
