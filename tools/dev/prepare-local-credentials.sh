#!/usr/bin/env bash
set +x
set -euo pipefail

fail() { printf 'Local credentials preparation failed: %s\n' "$*" >&2; exit 1; }

credentials_file=${1:-}
[[ $# == 1 && "$credentials_file" == /* && ! -L "$credentials_file" ]] ||
  fail 'one exact credentials file is required'
directory=$(dirname -- "$credentials_file")
[[ -d "$directory" && ! -L "$directory" && "$(realpath -- "$directory")" == "$directory" &&
  "$(stat -c '%u' "$directory")" == "$(id -u)" &&
  $((8#$(stat -c '%a' "$directory") & 8#077)) == 0 ]] || fail 'private state directory is required'

names=(KODEX_LOCAL_OWNER_USERNAME KODEX_LOCAL_OWNER_EMAIL KODEX_LOCAL_OWNER_PASSWORD)
requested=()
provided=0
for name in "${names[@]}"; do
  requested+=("${!name:-}")
  [[ -z "${!name:-}" ]] || provided=$((provided + 1))
done
[[ "$provided" == 0 || "$provided" == 3 ]] || fail 'all owner environment keys must be supplied together'

if [[ -e "$credentials_file" ]]; then
  [[ -f "$credentials_file" && "$(stat -c '%u' "$credentials_file")" == "$(id -u)" &&
    $((8#$(stat -c '%a' "$credentials_file") & 8#077)) == 0 ]] || fail 'credentials file must be private and owned by the host user'
  # Файл создаётся только этим helper и остаётся в приватном state владельца.
  # shellcheck disable=SC1090
  source "$credentials_file"
  for index in "${!names[@]}"; do
    name=${names[index]}
    [[ -n "${!name:-}" ]] || fail 'stored owner credentials are incomplete'
    if [[ "$provided" == 3 && "${!name}" != "${requested[index]}" ]]; then
      fail 'stored owner differs from supplied environment; explicit account reconciliation is required'
    fi
  done
  exit 0
fi

umask 077
temporary=$(mktemp "$directory/.credentials.XXXXXXXX")
trap 'rm -f -- "$temporary"' EXIT
admin_username=${KODEX_LOCAL_ADMIN_USERNAME:-admin}
admin_password=${KODEX_LOCAL_ADMIN_PASSWORD:-$(openssl rand -hex 32)}
owner_username=${KODEX_LOCAL_OWNER_USERNAME:-owner}
owner_email=${KODEX_LOCAL_OWNER_EMAIL:-owner@kodex.local}
owner_password=${KODEX_LOCAL_OWNER_PASSWORD:-$(openssl rand -hex 32)}
{
  printf 'KODEX_LOCAL_ADMIN_USERNAME=%q\n' "$admin_username"
  printf 'KODEX_LOCAL_ADMIN_PASSWORD=%q\n' "$admin_password"
  printf 'KODEX_LOCAL_OWNER_USERNAME=%q\n' "$owner_username"
  printf 'KODEX_LOCAL_OWNER_EMAIL=%q\n' "$owner_email"
  printf 'KODEX_LOCAL_OWNER_PASSWORD=%q\n' "$owner_password"
} >"$temporary"
# hard link публикует файл без перезаписи появившегося параллельно state.
ln -- "$temporary" "$credentials_file" || fail 'credentials file appeared concurrently'
