#!/usr/bin/env bash
set -euo pipefail
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
cd "$root/services/internal/control-plane"
exec go run ./cmd/runtime-secret-maintenance "$@"
