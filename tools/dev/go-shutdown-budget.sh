#!/usr/bin/env sh
set -eu
# Air оставляет время для полного drain Go-процесса внутри Pod grace.
case "${1:-}" in
  runtime-controller) printf '230s\n' ;;
  *) printf '90s\n' ;;
esac
