package service

import (
	"testing"

	model "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
)

// TestReadbackIntentIdentityIncludesSourceRevision keeps target and effective revision fixed, checks repeated source revision is deterministic and a different source revision changes the ID. It does not prove collision resistance for arbitrary inputs.
// TestReadbackIntentIdentityIncludesSourceRevision фиксирует target и effective revision, проверяет детерминизм повторной source revision и изменение ID при другой source revision. Стойкость к коллизиям произвольного ввода не доказывается.
func TestReadbackIntentIdentityIncludesSourceRevision(t *testing.T) {
	t.Parallel()

	const (
		// targetID fixes the delivery target so only source revision varies between identity comparisons.
		// targetID фиксирует цель доставки: между сравнениями идентичности меняется только source revision.
		targetID = "control-plane-authorization-verifier"
		// effectiveRevision holds the same effective credential revision across repeated and changed source cases.
		// effectiveRevision удерживает одинаковую эффективную ревизию credential в повторных и изменённых source cases.
		effectiveRevision = uint64(1_014_890_124)
	)
	intentID := func(sourceRevision uint64) string {
		return readbackIntentID(model.DeliveryTarget{
			TargetID:               targetID,
			ReadbackSourceRevision: sourceRevision,
		}, effectiveRevision)
	}

	revisionOne := intentID(1)
	if repeated := intentID(1); repeated != revisionOne {
		t.Fatalf("same source revision produced %q, want %q", repeated, revisionOne)
	}
	if revisionTwo := intentID(2); revisionTwo == revisionOne {
		t.Fatalf("different source revisions produced the same intent ID %q", revisionOne)
	}
}
