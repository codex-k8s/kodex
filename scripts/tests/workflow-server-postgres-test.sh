#!/usr/bin/env bash
set -euo pipefail

# Отдельный составной контур серверного DAG сохраняет прежний default suite.
# Общая оснастка создаёт только локальную одноразовую БД и удаляет её при выходе.
repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
exec bash "$repository_root/scripts/tests/control-plane-postgres-test.sh" \
  '^(TestWorkflowDispatchComponent|TestWorkflowCaptureComponent|TestWorkflowCaptureProgressComponent|TestWorkflowExceptionRecoveryComponent|TestWorkflowExceptionParallelRecoveryComponent|TestWorkflowExceptionRecoveryExpiryComponent|TestWorkflowRecoveryFeedbackComponent|TestWorkflowConfigurationSavepoint|TestWorkflowEffectRejoinComponent)$'
