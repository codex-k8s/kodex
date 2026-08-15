package app

import "testing"

func TestControlAPIProofProfileIncludesOwnerRPCProjectScope(t *testing.T) {
	t.Parallel()

	proofs := controlAPIProofOperations()
	projectRequired := controlAPIProjectRequiredOperations()
	if len(proofs) != 129 || len(projectRequired) != 119 {
		t.Fatalf("control API proof profile is incomplete: proofs=%d project=%d", len(proofs), len(projectRequired))
	}
	for operationID := range projectRequired {
		if _, ok := proofs[operationID]; !ok {
			t.Fatalf("project operation has no proof mapping: %s", operationID)
		}
	}
}
