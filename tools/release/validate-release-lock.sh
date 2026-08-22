#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Release lock validation failed: %s\n' "$*" >&2
  exit 1
}

lock_file=""
source_sha=""
expected_sha256=""
while (($# > 0)); do
  case "$1" in
    --lock) lock_file="${2:-}"; shift 2 ;;
    --source-sha) source_sha="${2:-}"; shift 2 ;;
    --sha256) expected_sha256="${2:-}"; shift 2 ;;
    *) fail "unsupported argument: $1" ;;
  esac
done

[[ -r "$lock_file" ]] || fail 'release lock is not readable'
[[ "$source_sha" =~ ^[a-f0-9]{40}$ ]] || fail 'source SHA must be exact lowercase 40-hex'
[[ "$expected_sha256" =~ ^[a-f0-9]{64}$ && "$expected_sha256" != 0000000000000000000000000000000000000000000000000000000000000000 ]] ||
  fail 'release lock SHA-256 is invalid'
actual_sha256=$(sha256sum "$lock_file" | awk '{print $1}')
[[ "$actual_sha256" == "$expected_sha256" ]] || fail 'release lock SHA-256 mismatch'

manifest=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)/images.json
jq -e --arg source_sha "$source_sha" --slurpfile manifest "$manifest" '
  def registry_path:
    type == "string" and
    test("^[a-z0-9][a-z0-9.:-]*(/[a-z0-9][a-z0-9._/-]*)?$") and
    (contains("://") | not) and (contains("@") | not) and
    (endswith("/") | not) and (contains("//") | not);
  def digest:
    type == "string" and test("^sha256:[a-f0-9]{64}$") and
    . != "sha256:0000000000000000000000000000000000000000000000000000000000000000";
  . as $root |
  .schema_version == 2 and
  .profile == "web-only" and
  .source_sha == $source_sha and
  (.build_run_id | type == "string" and test("^(local|[0-9]+)$")) and
  (.registry.push | registry_path) and
  (.registry.push | split("/")[0] | test("^[a-z0-9][a-z0-9.-]*[a-z0-9](?::443)?$") and contains(".")) and
  (.registry.node_pull | registry_path) and
  (.registry.repository_prefix | type == "string" and test("^[a-z0-9][a-z0-9._/-]*$") and (endswith("/") | not)) and
  ($manifest[0].schema_version == 2) and
  ([.images[].component] == [$manifest[0].images[].component]) and
  ([.images[].component] | unique | length) == (.images | length) and
  all(.images[];
    (.repository == ($root.registry.repository_prefix + "/" + .component)) and
    (.digest | digest) and
    (.pull_ref == ($root.registry.node_pull + "/" + .repository + "@" + .digest)) and
    (.pull_ref | contains(":latest") | not) and
    (.pull_ref | contains("placeholder") | not)
  ) and
  (.external_images | length == 1) and
  (.external_images[0].component == "admission-tools") and
  (.external_images[0].digest | digest) and
  (.external_images[0].pull_ref | type == "string" and
    test("^[a-z0-9][a-z0-9._:/-]*@sha256:[a-f0-9]{64}$")) and
  ($root.external_images[0].pull_ref | endswith("@" + $root.external_images[0].digest)) and
  (.role_image_input.repository == (.registry.repository_prefix + "/role-image-inputs")) and
  (.role_image_input.manifest_digest | digest) and
  (.role_image_input.payload_sha256 | type == "string" and test("^[a-f0-9]{64}$") and
    . != "0000000000000000000000000000000000000000000000000000000000000000") and
  (.role_image_input.source_sha256 | type == "string" and test("^[a-f0-9]{64}$") and
    . != "0000000000000000000000000000000000000000000000000000000000000000") and
  (.role_image_input.pull_ref == ($root.registry.node_pull + "/" + .role_image_input.repository + "@" + .role_image_input.manifest_digest))
' "$lock_file" >/dev/null || fail 'release lock schema or provenance is invalid'

printf 'Release lock validation completed\n'
