#!/usr/bin/env sh
set -eu
name=${1:-}
instance=${2:-}
if [ "$#" -ne 2 ] || ! printf '%s' "$name" | grep -Eq '^[a-z0-9][a-z0-9-]*$'; then
  printf '%s\n' 'Kodex development runtime identity is invalid' >&2
  exit 1
fi
if [ -n "$instance" ]; then
  printf '%s' "$instance" | grep -Eq '^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$' || {
    printf '%s\n' 'Kodex development Pod identity is invalid' >&2
    exit 1
  }
  printf '/go/build-cache/runtime-%s-%s\n' "$name" "$instance"
else
  printf '/go/build-cache/runtime-%s\n' "$name"
fi
