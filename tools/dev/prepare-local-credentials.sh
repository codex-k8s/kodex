#!/usr/bin/env bash
set +x
set -euo pipefail
[[ $# == 1 ]] || { printf 'Local credentials preparation failed: one file is required\n' >&2; exit 1; }
exec python3 "${BASH_SOURCE[0]%/*}/local_credentials.py" prepare local "$1"
