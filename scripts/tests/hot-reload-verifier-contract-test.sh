#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex hot reload verifier contract test failed: %s\n' "$*" >&2
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
verifier="$repository_root/tools/dev/verify-hot-reload.sh"

# These are intentional literal contracts from the verifier source.
# shellcheck disable=SC2016
grep -Fq 'ensure_port_forward control-api-gateway 9090 "$go_port"' "$verifier" ||
  fail 'Go status polling does not restore a stopped port-forward'
# shellcheck disable=SC2016
grep -Fq 'ensure_port_forward staff-control-center 8080 "$vue_port"' "$verifier" ||
  fail 'Vue module polling does not restore a stopped port-forward'
# shellcheck disable=SC2016
grep -Fq 'previous_pid=${!pid_variable:-}' "$verifier" ||
  fail 'port-forward restart does not reap the previous process'
# shellcheck disable=SC2016
grep -Fq 'while [[ -z "$vue_port" || "$vue_port" == "$go_port" ]]' "$verifier" ||
  fail 'Go and Vue loopback ports are not guaranteed to differ'
# shellcheck disable=SC2016
grep -Fq 'last observed status: $last_status' "$verifier" ||
  fail 'Go timeout diagnostic omits the last observed status'

printf 'Kodex hot reload verifier contract test passed\n'
