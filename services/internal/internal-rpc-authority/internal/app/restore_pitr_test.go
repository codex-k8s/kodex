package app

import (
	"testing"

	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
)

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

func TestShouldExecuteRestorePITRRejectsUnknownPhase(t *testing.T) {
	t.Parallel()

	if _, err := shouldExecuteRestorePITR(model.RestoreState{Phase: "RESTORING"}); err == nil {
		t.Fatal("unknown persisted PITR phase was accepted")
	}
}
