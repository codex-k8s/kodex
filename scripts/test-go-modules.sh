#!/usr/bin/env bash
set -euo pipefail

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)

mapfile -t module_files < <(
  find "$repository_root/libs/go" "$repository_root/services" -name go.mod -type f -print |
    sort
)
for module_file in "${module_files[@]}"; do
  module_directory=$(dirname -- "$module_file")
  relative_directory=${module_directory#"$repository_root"/}
  printf 'Go tests: %s\n' "$relative_directory"
  (
    cd -- "$module_directory"
    env -u GOFLAGS GOENV=off GOWORK=off go test -tags= ./...
  )
done
