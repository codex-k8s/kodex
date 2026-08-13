#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Authority source revision test failed: %s\n' "$*" >&2
  exit 1
}

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
resolver="$script_directory/resolve-authority-source-revision.sh"
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT

write_registry() {
  local path=$1 revision=$2 workload=${3:-control-plane}
  cat >"$path" <<EOF
version: 1
source_revision: $revision
targets:
  - workload_id: $workload
    role: AUTHORIZATION_RESOLVER
EOF
}

write_policy() {
  local path=$1 revision=$2 operation=${3:-control.read}
  cat >"$path" <<EOF
{"v":1,"policy_revision":$revision,"policy":{"default_decision":"DENY","operation":"$operation"}}
EOF
}

write_snapshot() {
  local path=$1 source_revision=$2 policy_revision=$3 operation=${4:-control.read}
  cat >"$path" <<EOF
{"v":1,"source_revision":$source_revision,"policy_revision":$policy_revision,"policy":{"operation":"$operation","default_decision":"DENY"}}
EOF
}

assert_revision() {
  local expected=$1 name=$2
  shift 2
  local actual
  actual=$("$resolver" "$@")
  [[ "$actual" == "$expected" ]] || fail "$name: expected $expected, got $actual"
}

desired_registry="$temporary_directory/desired-registry.yaml"
desired_policy="$temporary_directory/desired-policy.json"
current_registry="$temporary_directory/current-registry.yaml"
snapshot_payload="$temporary_directory/snapshot-payload.json"
write_registry "$desired_registry" 1
write_policy "$desired_policy" 30

assert_revision 1 fresh \
  --desired-registry "$desired_registry" --desired-policy "$desired_policy"

write_registry "$current_registry" 3
write_snapshot "$snapshot_payload" 3 30
assert_revision 3 unchanged \
  --desired-registry "$desired_registry" --desired-policy "$desired_policy" \
  --current-registry "$current_registry" --snapshot-payload "$snapshot_payload"

write_policy "$desired_policy" 31 control.write
assert_revision 4 policy-change \
  --desired-registry "$desired_registry" --desired-policy "$desired_policy" \
  --current-registry "$current_registry" --snapshot-payload "$snapshot_payload"

write_policy "$desired_policy" 30
write_registry "$desired_registry" 1 runtime-controller
assert_revision 4 registry-change \
  --desired-registry "$desired_registry" --desired-policy "$desired_policy" \
  --current-registry "$current_registry" --snapshot-payload "$snapshot_payload"

write_registry "$desired_registry" 1
write_registry "$current_registry" 4
assert_revision 4 partial-apply \
  --desired-registry "$desired_registry" --desired-policy "$desired_policy" \
  --current-registry "$current_registry" --snapshot-payload "$snapshot_payload"

printf '{"v":1,"source_revision":0,"policy_revision":30,"policy":{}}\n' >"$snapshot_payload"
if "$resolver" \
  --desired-registry "$desired_registry" --desired-policy "$desired_policy" \
  --current-registry "$current_registry" --snapshot-payload "$snapshot_payload" >/dev/null 2>&1; then
  fail "malformed snapshot was accepted"
fi

printf 'Authority source revision tests passed\n'
