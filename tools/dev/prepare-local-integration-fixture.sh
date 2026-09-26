#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex local integration fixture preparation failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf 'Usage: %s --state-directory <private-path>\n' "$0" >&2
}

state_directory=""
while (($# > 0)); do
  case "$1" in
    --state-directory) state_directory=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ "$state_directory" == /* && "$state_directory" != / && "$state_directory" != "$HOME" &&
  -d "$state_directory" && ! -L "$state_directory" ]] || fail 'state directory is invalid'
[[ "$(stat -c '%u' "$state_directory")" == "$(id -u)" &&
  $((8#$(stat -c '%a' "$state_directory") & 8#077)) == 0 ]] ||
  fail 'state directory must be owner-private'
command -v openssl >/dev/null 2>&1 || fail 'openssl is required'

token_file="$state_directory/integration-fixture-bearer-token"
if [[ ! -e "$token_file" ]]; then
  temporary_token=$(mktemp "$state_directory/.integration-fixture-token.XXXXXX")
  trap 'rm -f -- "${temporary_token:-}"' EXIT
  chmod 0600 "$temporary_token"
  openssl rand -hex 32 >"$temporary_token"
  mv -- "$temporary_token" "$token_file"
  temporary_token=""
fi

[[ -f "$token_file" && ! -L "$token_file" && "$(stat -c '%u' "$token_file")" == "$(id -u)" &&
  $((8#$(stat -c '%a' "$token_file") & 8#077)) == 0 ]] || fail 'fixture token file is unsafe'
IFS= read -r token <"$token_file" || fail 'fixture token is unavailable'
[[ "$token" =~ ^[a-f0-9]{64}$ ]] || fail 'fixture token is invalid'
