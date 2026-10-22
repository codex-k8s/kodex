package app

import (
	"testing"

	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
)

// TestShouldExecuteRestorePITR checks the four recognized no-op phases and the sole executable PREPARED phase through the pure predicate. It creates no executor, database instances, evidence or Kubernetes client.
// TestShouldExecuteRestorePITR проверяет четыре распознанные фазы без действия и единственную исполняемую PREPARED через чистый предикат. Исполнитель, экземпляры БД, evidence и клиент Kubernetes не создаются.
func TestShouldExecuteRestorePITR(t *testing.T) {
	t.Parallel()

	for _, phase := range []string{"", "OPEN", "QUIESCING", "COMPLETED"} {
		execute, err := shouldExecuteRestorePITR(model.RestoreState{Phase: phase})
		if err != nil || execute {
			t.Fatalf("phase %q must be a successful no-op: execute=%t err=%v", phase, execute, err)
		}
	}
	execute, err := shouldExecuteRestorePITR(model.RestoreState{Phase: "PREPARED"})
	if err != nil || !execute {
		t.Fatalf("PREPARED must execute PITR: execute=%t err=%v", execute, err)
	}
}

// TestShouldExecuteRestorePITRRejectsUnknownPhase requires the unrecognized RESTORING string to fail rather than being treated as an executable or silently completed state. This is local phase validation, not a real recovery transition.
// TestShouldExecuteRestorePITRRejectsUnknownPhase требует отказ для нераспознанной строки RESTORING вместо её трактовки как исполняемого или тихо завершённого состояния. Это локальная проверка фазы, а не реальный переход восстановления.
func TestShouldExecuteRestorePITRRejectsUnknownPhase(t *testing.T) {
	t.Parallel()

	if _, err := shouldExecuteRestorePITR(model.RestoreState{Phase: "RESTORING"}); err == nil {
		t.Fatal("unknown persisted PITR phase was accepted")
	}
}
