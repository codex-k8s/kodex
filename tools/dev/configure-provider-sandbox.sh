#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex provider sandbox configuration failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf 'Usage: %s --mode apply|readback\n' "$0" >&2
}

mode=""
while (($# > 0)); do
  case "$1" in
    --mode) mode=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

case "$mode" in apply|readback) ;; *) fail 'mode is invalid' ;; esac
((EUID == 0)) || fail 'root is required for the host AppArmor profile'
[[ "$(uname -s)" == Linux ]] || fail 'Linux is required'

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
profile_source="$script_directory/../../infra/apparmor/kodex-provider-runtime"
profile_target=/etc/apparmor.d/kodex-provider-runtime

for command_name in apparmor_parser cmp grep install; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ -f "$profile_source" && ! -L "$profile_source" ]] || fail 'repository AppArmor profile is absent'
apparmor_parser -Q "$profile_source" >/dev/null || fail 'repository AppArmor profile is invalid'

if [[ "$mode" == apply ]]; then
  install -o root -g root -m 0644 "$profile_source" "$profile_target"
  apparmor_parser -r "$profile_target"
fi

[[ -f "$profile_target" && ! -L "$profile_target" ]] || fail 'installed AppArmor profile is absent'
cmp -s "$profile_source" "$profile_target" || fail 'installed AppArmor profile differs from repository source'
grep -Fxq 'kodex-provider-runtime (unconfined)' /sys/kernel/security/apparmor/profiles ||
  fail 'provider AppArmor profile is not loaded'

printf 'Kodex provider sandbox configuration completed: mode=%s\n' "$mode"
