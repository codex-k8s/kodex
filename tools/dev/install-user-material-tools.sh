#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex user material tools installation failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf 'Usage: %s --mode apply|readback --state-directory <absolute-path>\n' "$0" >&2
}

mode=""
state_directory=""
while (($# > 0)); do
  case "$1" in
    --mode) mode=${2:-}; shift 2 ;;
    --state-directory) state_directory=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

case "$mode" in apply|readback) ;; *) fail 'mode is invalid' ;; esac
[[ "$state_directory" == /* && "$state_directory" != / && "$state_directory" != "$HOME" ]] ||
  fail 'state directory is invalid'
for command_name in awk cmp curl find grep install jq ln mktemp python3 readlink sha256sum unzip; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
python3 -c 'import bcrypt' >/dev/null 2>&1 || fail 'Python bcrypt module is required'

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repository_root=$(cd -- "$script_directory/../.." && pwd -P)
lock_file="$repository_root/tools/install/components.lock.json"
tool_bin="$state_directory/tools/bin"
material_root="$state_directory/tools/material"

download_artifact() {
  local name=$1 output=$2 url expected_sha actual_sha
  url=$(jq -er --arg name "$name" '.artifacts[] | select(.name == $name) | .url' "$lock_file")
  expected_sha=$(jq -er --arg name "$name" '.artifacts[] | select(.name == $name) | .sha256' "$lock_file")
  curl --proto '=https' --tlsv1.2 --fail --silent --show-error --location \
    --retry 5 --retry-all-errors --retry-delay 2 --connect-timeout 15 \
    "$url" --output "$output"
  actual_sha=$(sha256sum "$output" | awk '{print $1}')
  [[ "$actual_sha" == "$expected_sha" ]] || fail "artifact digest mismatch: $name"
}

if [[ "$mode" == apply ]]; then
  install -d -m 0700 "$tool_bin" "$material_root"
  temporary_directory=$(mktemp -d "$material_root/.install.XXXXXX")
  trap 'rm -rf -- "$temporary_directory"' EXIT

  download_artifact cosign "$temporary_directory/cosign"
  install -m 0555 "$temporary_directory/cosign" "$material_root/cosign"
  ln -sfn "$material_root/cosign" "$tool_bin/cosign"

  download_artifact nsc "$temporary_directory/nsc.zip"
  unzip -Z1 "$temporary_directory/nsc.zip" | awk '
    /^\// || /(^|\/)\.\.(\/|$)/ { exit 1 }
  ' || fail 'nsc archive contains an unsafe path'
  unzip -q "$temporary_directory/nsc.zip" -d "$temporary_directory/nsc"
  nsc_binary=$(find "$temporary_directory/nsc" -type f -name nsc -print -quit)
  [[ -n "$nsc_binary" ]] || fail 'nsc binary is absent from the archive'
  install -m 0555 "$nsc_binary" "$material_root/nsc"
  ln -sfn "$material_root/nsc" "$tool_bin/nsc"

  install -m 0555 "$script_directory/htpasswd.py" "$material_root/htpasswd"
  ln -sfn "$material_root/htpasswd" "$tool_bin/htpasswd"
fi

for command_name in cosign nsc htpasswd; do
  [[ -x "$tool_bin/$command_name" ]] || fail "$command_name is absent"
  resolved=$(readlink -f "$tool_bin/$command_name")
  case "$resolved" in "$material_root"/*) ;; *) fail "$command_name resolves outside the private tool root" ;; esac
done
"$tool_bin/cosign" version --json | jq -e '.gitVersion == "v3.1.3"' >/dev/null ||
  fail 'cosign version mismatch'
"$tool_bin/nsc" --version 2>&1 | grep -Fq '2.15.0' || fail 'nsc version mismatch'
cmp --silent "$script_directory/htpasswd.py" "$material_root/htpasswd" ||
  fail 'htpasswd helper differs from the repository source'

readback_directory=$(mktemp -d "$material_root/.readback.XXXXXX")
trap 'rm -rf -- "$readback_directory"' EXIT
printf '%s\n' fixture-password | "$tool_bin/htpasswd" -i -B -C 4 -c \
  "$readback_directory/users" fixture >/dev/null
grep -Eq '^fixture:\$2[aby]\$04\$' "$readback_directory/users" ||
  fail 'htpasswd readback failed'

printf 'Kodex user material tools ready: %s\n' "$tool_bin"
