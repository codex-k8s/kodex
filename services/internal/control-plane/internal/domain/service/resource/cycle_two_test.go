package resource

import (
	"testing"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
)

func TestWeightedCandidateIndexExactCycle(t *testing.T) {
	t.Parallel()

	weights := []uint32{1, 2}
	want := []int{0, 1, 1}
	for slot, expected := range want {
		actual, ok := weightedCandidateIndex(weights, uint64(slot))
		if !ok || actual != expected {
			t.Fatalf("slot %d: got (%d, %t), want (%d, true)", slot, actual, ok, expected)
		}
	}
	if _, ok := weightedCandidateIndex(weights, uint64(len(want))); ok {
		t.Fatal("slot outside the exact cycle must fail closed")
	}
}

func TestWeightedCandidateIndexRejectsZeroWeight(t *testing.T) {
	t.Parallel()

	if _, ok := weightedCandidateIndex([]uint32{1, 0, 2}, 1); ok {
		t.Fatal("zero weight must fail closed")
	}
}

func TestMemoryEligibilityIsIdenticalForProjectAndRoleScopes(t *testing.T) {
	t.Parallel()

	project := entity.Resource{
		Kind: enum.KindMemoryRecord,
		Spec: entity.MemoryRecordSpec{Scope: "PROJECT"},
	}
	role := entity.Resource{
		Kind: enum.KindMemoryRecord,
		Spec: entity.MemoryRecordSpec{Scope: "ROLE", RoleID: "role-a"},
	}
	eligibility := memoryEligibility{CanReadProject: true, RoleIDs: []string{"role-b"}}
	if !memoryResourceEligible(project, eligibility) {
		t.Fatal("verified project member must read project memory")
	}
	if memoryResourceEligible(role, eligibility) {
		t.Fatal("role memory must be hidden without the exact persisted role")
	}
	eligibility.RoleIDs = append(eligibility.RoleIDs, "role-a")
	if !memoryResourceEligible(role, eligibility) {
		t.Fatal("exact persisted role must make role memory eligible")
	}
}

func TestCurrentExecutionRejectsPartialTuple(t *testing.T) {
	t.Parallel()

	_, err := currentExecution(entity.ProcessRunSpec{
		CurrentSessionID:      "session",
		CurrentSessionVersion: 1,
		CurrentTurnID:         "turn",
	})
	if err == nil {
		t.Fatal("partial current execution tuple must fail closed")
	}
}
