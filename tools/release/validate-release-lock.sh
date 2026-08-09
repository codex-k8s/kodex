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
[[ -r "$lock_file" ]] || fail "release lock is not readable"
[[ "$source_sha" =~ ^[a-f0-9]{40}$ ]] || fail "source SHA must be exact lowercase 40-hex"
[[ "$expected_sha256" =~ ^[a-f0-9]{64}$ && "$expected_sha256" != 0000000000000000000000000000000000000000000000000000000000000000 ]] ||
  fail "release lock SHA-256 is invalid"
actual_sha256=$(sha256sum "$lock_file" | awk '{print $1}')
[[ "$actual_sha256" == "$expected_sha256" ]] || fail "release lock SHA-256 mismatch"

manifest=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)/images.json
jq -e --arg source_sha "$source_sha" --slurpfile manifest "$manifest" '
  .schema_version == 1 and
  .profile == "direct-production single-node prototype" and
  .source_sha == $source_sha and
  (.build_run_id | type == "string" and test("^(local|[0-9]+)$")) and
  .registry_push == "matter-codex-registry.matter-kodex-prod.svc.cluster.local:5000" and
  .node_pull == "localhost:5001" and
  ([.images[].component] == [$manifest[0].images[].component]) and
  ([.images[].component] | unique | length) == (.images | length) and
  all(.images[];
    (.repository == ("mattercodex/" + .component)) and
    (.digest | test("^sha256:[a-f0-9]{64}$")) and
    .digest != "sha256:0000000000000000000000000000000000000000000000000000000000000000" and
    (.pull_ref == ("localhost:5001/" + .repository + "@" + .digest)) and
    (.pull_ref | contains(":latest") | not) and
    (.pull_ref | contains("placeholder") | not)
  )
' "$lock_file" >/dev/null || fail "release lock schema or provenance is invalid"
printf 'Release lock validation completed\n'
