#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Go toolchain check failed: %s\n' "$*" >&2
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
expected_version=1.26.10
expected_builder='docker.io/library/golang:1.26.10-alpine@sha256:87c65a688975c0b1d4585d204874c45ca78354670e3a5f11368c5a492303d696'

[[ "$(env -u GOFLAGS GOENV=off GOWORK=off go env GOVERSION)" == "go$expected_version" ]] ||
  fail "go$expected_version is required"

mapfile -t module_files < <(
  find "$repository_root/libs/go" "$repository_root/services" -name go.mod -type f -print |
    sort
)
module_files=("$repository_root/go.mod" "${module_files[@]}")
for module_file in "${module_files[@]}"; do
  [[ "$(awk '$1 == "go" {print $2}' "$module_file")" == "$expected_version" ]] ||
    fail "unexpected Go version in ${module_file#"$repository_root"/}"
done

while IFS= read -r dockerfile; do
  if ! rg -q '^FROM .*golang:' "$dockerfile"; then
    continue
  fi
  mapfile -t go_stages < <(rg '^FROM .*golang:' "$dockerfile")
  for stage in "${go_stages[@]}"; do
    [[ "$stage" =~ ^FROM\ ${expected_builder//./\.}\ AS\ [a-zA-Z0-9_-]+$ ]] ||
      fail "builder image is not pinned in ${dockerfile#"$repository_root"/}"
  done
  [[ "$(rg -c '^ENV GOTOOLCHAIN=local$' "$dockerfile")" -ge "${#go_stages[@]}" ]] ||
    fail "GOTOOLCHAIN is not fixed in ${dockerfile#"$repository_root"/}"
  [[ "$(rg -c -F 'RUN test "$(go env GOVERSION)" = "go1.26.10"' "$dockerfile")" == "${#go_stages[@]}" ]] ||
    fail "builder does not verify its Go version in ${dockerfile#"$repository_root"/}"
done < <(find "$repository_root/services" "$repository_root/infra" -type f \
  \( -name Dockerfile -o -name '*.Dockerfile' \) -print | sort)

printf 'Go toolchain check passed: go%s\n' "$expected_version"
