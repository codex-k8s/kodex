package service

import (
	"testing"

	model "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
)

func TestReadbackIntentIdentityIncludesSourceRevision(t *testing.T) {
	t.Parallel()

	const (
		targetID          = "control-plane-authorization-verifier"
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
