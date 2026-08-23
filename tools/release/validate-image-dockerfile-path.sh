#!/usr/bin/env bash
set -euo pipefail

path=${1:-}
if [[ $# -ne 1 ]] ||
   [[ ! "$path" =~ ^services/(internal|external|staff|jobs)/[a-z0-9]([-a-z0-9]*[a-z0-9])?/(Dockerfile|[a-z0-9]([-a-z0-9.]*[a-z0-9])?\.Dockerfile)$ ]]; then
  printf 'Release image Dockerfile path is outside the canonical unit scope: %s\n' "$path" >&2
  exit 2
fi
