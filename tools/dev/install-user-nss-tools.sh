#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex user NSS tools installation failed: %s\n' "$*" >&2
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
for command_name in apt-get dpkg-deb find install mktemp readlink; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done

tool_root="$state_directory/tools/nss"
tool_bin="$state_directory/tools/bin"
certutil="$tool_bin/certutil"
if [[ "$mode" == apply && ! -x "$certutil" ]]; then
  temporary_directory=$(mktemp -d)
  trap 'rm -rf -- "$temporary_directory"' EXIT
  (
    cd "$temporary_directory"
    apt-get download libnss3-tools >/dev/null
  ) || fail 'libnss3-tools download failed'
  package=$(find "$temporary_directory" -maxdepth 1 -type f -name 'libnss3-tools_*.deb' -print -quit)
  [[ -n "$package" ]] || fail 'libnss3-tools package is absent'
  install -d -m 0700 "$tool_root" "$tool_bin"
  dpkg-deb -x "$package" "$tool_root"
  extracted=$(find "$tool_root" -type f -path '*/bin/certutil' -print -quit)
  [[ -n "$extracted" && -x "$extracted" ]] || fail 'certutil was not extracted'
  ln -sfn "$extracted" "$certutil"
fi

[[ -x "$certutil" && ! -L "$tool_root" && ! -L "$tool_bin" ]] ||
  fail 'private certutil installation is absent or unsafe'
resolved=$(readlink -f "$certutil")
case "$resolved" in "$tool_root"/*) ;; *) fail 'certutil resolves outside the private tool root' ;; esac
"$certutil" --build-flags >/dev/null 2>&1 || fail 'certutil readback failed'

printf 'Kodex user NSS tools ready: %s\n' "$tool_bin"
