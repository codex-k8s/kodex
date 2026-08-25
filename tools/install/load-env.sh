#!/usr/bin/env bash
# shellcheck shell=bash

kodex_load_env() {
  local env_file=$1 line key value mode line_number=0
  [[ -f "$env_file" && -r "$env_file" && ! -L "$env_file" ]] || {
    printf 'Kodex env loading failed: .kodex-env is absent or unreadable\n' >&2
    return 1
  }
  mode=$(stat -c '%a' "$env_file")
  (((8#$mode & 0077) == 0)) || {
    printf 'Kodex env loading failed: .kodex-env permissions must be 0600\n' >&2
    return 1
  }
  declare -A seen=()
  while IFS= read -r line || [[ -n "$line" ]]; do
    line_number=$((line_number + 1))
    line=${line%$'\r'}
    [[ -z "$line" || "$line" =~ ^[[:space:]]*# ]] && continue
    if [[ ! "$line" =~ ^(KODEX_[A-Z0-9_]+)=(.*)$ ]]; then
      printf 'Kodex env loading failed: invalid line %d\n' "$line_number" >&2
      return 1
    fi
    key=${BASH_REMATCH[1]}
    value=${BASH_REMATCH[2]}
    [[ -z "${seen[$key]:-}" ]] || {
      printf 'Kodex env loading failed: duplicate key %s\n' "$key" >&2
      return 1
    }
    seen[$key]=true
    if [[ "$value" =~ ^\'(.*)\'$ || "$value" =~ ^\"(.*)\"$ ]]; then
      value=${BASH_REMATCH[1]}
    fi
    [[ "$value" != *'`'* && "$value" != *'$('* && "$value" != *$'\n'* ]] || {
      printf 'Kodex env loading failed: unsafe value for %s\n' "$key" >&2
      return 1
    }
    printf -v "$key" '%s' "$value"
    export "$key"
  done <"$env_file"
}

kodex_require_env() {
  local key
  for key in "$@"; do
    [[ -n "${!key:-}" ]] || {
      printf 'Kodex env validation failed: %s is required\n' "$key" >&2
      return 1
    }
  done
}
