#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Authority source revision resolution failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf 'Usage: %s --desired-registry <path> --desired-policy <path> [--current-registry <path> --snapshot-payload <path>]\n' "$0" >&2
}

desired_registry=""
desired_policy=""
current_registry=""
snapshot_payload=""
while (($# > 0)); do
  case "$1" in
    --desired-registry) desired_registry="${2:-}"; shift 2 ;;
    --desired-policy) desired_policy="${2:-}"; shift 2 ;;
    --current-registry) current_registry="${2:-}"; shift 2 ;;
    --snapshot-payload) snapshot_payload="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

for command_name in jq yq sha256sum; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ -r "$desired_registry" ]] || fail "desired registry is required"
[[ -r "$desired_policy" ]] || fail "desired policy is required"
if [[ -n "$current_registry" || -n "$snapshot_payload" ]]; then
  [[ -r "$current_registry" && -r "$snapshot_payload" ]] ||
    fail "current registry and snapshot payload must be provided together"
fi

is_safe_revision() {
  [[ "$1" =~ ^[1-9][0-9]*$ ]] || return 1
  ((10#$1 <= 9007199254740991))
}

yq -e '
  .version == 1 and
  ((.source_revision | tag) == "!!int") and
  (.source_revision >= 1 and .source_revision <= 9007199254740991) and
  ((.targets | tag) == "!!seq") and (.targets | length > 0)
' "$desired_registry" >/dev/null || fail "desired registry is invalid"
jq -e '
  .v == 1 and
  (.policy_revision | type == "number" and . >= 1 and . <= 9007199254740991 and floor == .) and
  (.policy | type == "object")
' "$desired_policy" >/dev/null || fail "desired policy is invalid"

if [[ -z "$current_registry" ]]; then
  printf '1\n'
  exit 0
fi

yq -e '
  .version == 1 and
  ((.source_revision | tag) == "!!int") and
  (.source_revision >= 1 and .source_revision <= 9007199254740991) and
  ((.targets | tag) == "!!seq") and (.targets | length > 0)
' "$current_registry" >/dev/null || fail "current registry is invalid"
jq -e '
  .v == 1 and
  (.source_revision | type == "number" and . >= 1 and . <= 9007199254740991 and floor == .) and
  (.policy_revision | type == "number" and . >= 1 and . <= 9007199254740991 and floor == .) and
  (.policy | type == "object")
' "$snapshot_payload" >/dev/null || fail "snapshot payload is invalid"

current_revision=$(yq -r '.source_revision' "$current_registry")
snapshot_revision=$(jq -r '.source_revision | tostring' "$snapshot_payload")
desired_policy_revision=$(jq -r '.policy_revision | tostring' "$desired_policy")
snapshot_policy_revision=$(jq -r '.policy_revision | tostring' "$snapshot_payload")
for revision in "$current_revision" "$snapshot_revision" "$desired_policy_revision" "$snapshot_policy_revision"; do
  is_safe_revision "$revision" || fail "revision is outside the safe integer range"
done

canonical_registry_digest() {
  yq -o=json '.' "$1" | jq -Sc 'del(.source_revision)' | sha256sum | awk '{print $1}'
}

canonical_policy_digest() {
  jq -Sc '.policy' "$1" | sha256sum | awk '{print $1}'
}

current_registry_digest=$(canonical_registry_digest "$current_registry")
desired_registry_digest=$(canonical_registry_digest "$desired_registry")
desired_policy_digest=$(canonical_policy_digest "$desired_policy")
snapshot_policy_digest=$(canonical_policy_digest "$snapshot_payload")

if [[ "$current_revision" == "$snapshot_revision" &&
      "$current_registry_digest" == "$desired_registry_digest" &&
      "$desired_policy_revision" == "$snapshot_policy_revision" &&
      "$desired_policy_digest" == "$snapshot_policy_digest" ]]; then
  printf '%s\n' "$snapshot_revision"
  exit 0
fi

((10#$snapshot_revision < 9007199254740991)) || fail "source revision is exhausted"
printf '%s\n' "$((10#$snapshot_revision + 1))"
