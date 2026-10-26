#!/usr/bin/env bash
set -euo pipefail

# Отдельный disposable container со случайным loopback port; ни staging,
# ни пользовательский DSN не принимаются. Cleanup принадлежит штатной оснастке.
repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
exec timeout --signal=TERM --kill-after=15s 360s env -u KODEX_CONTROL_PLANE_TEST_FILTER \
  GOMAXPROCS=2 KODEX_CONTROL_PLANE_TEST_GO_PARALLELISM=1 \
  "$repository_root/scripts/tests/control-plane-postgres-test.sh" \
  '^(TestSSHComposeCustodyComponent|TestSSHComposeLeaseComponent|TestSSHComposeWorkflowComponent|TestServerWorkflowMigrationProofComponent)$'
