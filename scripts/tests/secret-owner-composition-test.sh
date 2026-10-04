#!/usr/bin/env bash
set -euo pipefail

# Публичная точка входа выделяет собственную PostgreSQL template-копию;
# runner не получает credential-пути живого контура и не запускает агентов.
repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
if [[ -n "${KODEX_CONTROL_PLANE_TEST_DSN:-}" || -n "${KODEX_CONTROL_PLANE_TEST_ADMIN_DSN:-}" || -n "${KODEX_SECRET_COMPOSITION_OWNER_DSN:-}" || -n "${KODEX_SECRET_COMPOSITION_READY_FILE:-}" ]]; then
  printf 'Secret owner composition failed: external fixture credentials are forbidden\n' >&2
  exit 1
fi
export GOTOOLCHAIN=go1.26.6 GOMAXPROCS=2 GOWORK=off
exec timeout --signal=TERM --kill-after=15s 360s bash "$repository_root/scripts/tests/control-plane-postgres-test.sh" '^TestSecretOwnerCompositionComponent$'
