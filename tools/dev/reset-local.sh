#!/usr/bin/env bash
set -euo pipefail
# Общий операторский boundary проверяет cluster/source и оба namespace до DELETE.
directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
exec bash "$directory/runtime-secret-maintenance.sh" --mode reset "$@"
