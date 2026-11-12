#!/usr/bin/env bash
# shellcheck shell=bash

kodex_load_local_credentials() {
  set +x
  local filename=$1 profile=${2:-local} index
  local helper="${BASH_SOURCE[0]%/*}/../local_credentials.py"
  local -a expected=() fields=()
  case "$profile" in
    local) expected=(KODEX_LOCAL_ADMIN_USERNAME KODEX_LOCAL_ADMIN_PASSWORD
      KODEX_LOCAL_OWNER_USERNAME KODEX_LOCAL_OWNER_EMAIL KODEX_LOCAL_OWNER_PASSWORD) ;;
    access) expected=(KODEX_ACCESS_ADMIN_USERNAME KODEX_ACCESS_OPERATOR_USERNAME
      KODEX_ACCESS_AUDITOR_USERNAME KODEX_ACCESS_TEST_PASSWORD) ;;
    *) printf 'Local credentials loading failed: invalid profile\n' >&2; return 1 ;;
  esac
  mapfile -d '' -t fields < <(python3 "$helper" read "$profile" "$filename")
  # Process substitution не передаёт exit status: проверяем весь закрытый envelope до присваиваний.
  [[ ${#fields[@]} == $((${#expected[@]} * 2)) ]] || {
    printf 'Local credentials loading failed: incomplete private input\n' >&2; return 1
  }
  for index in "${!expected[@]}"; do
    [[ "${fields[index * 2]}" == "${expected[index]}" && -n "${fields[index * 2 + 1]}" ]] || return 1
  done
  for index in "${!expected[@]}"; do
    printf -v "${expected[index]}" '%s' "${fields[index * 2 + 1]}"
  done
}
