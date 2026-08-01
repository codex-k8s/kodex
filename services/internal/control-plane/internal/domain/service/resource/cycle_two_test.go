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
	project.State = enum.StateDeleted
	if memoryResourceEligible(project, eligibility) {
		t.Fatal("deleted project memory must remain a hidden tombstone")
	}
	role.State = enum.StateDeleted
	if memoryResourceEligible(role, eligibility) {
		t.Fatal("deleted role memory must remain a hidden tombstone")
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

func TestPrepareRetryTurnSpecKeepsBoundedSourceRef(t *testing.T) {
	t.Parallel()

	const digest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	sourceRef := "server://" + string(make([]byte, 503))
	bytes := []byte(sourceRef)
	for index := len("server://"); index < len(bytes); index++ {
		bytes[index] = 'a'
	}
	sourceRef = string(bytes)
	if len(sourceRef) != 512 {
		t.Fatalf("fixture source ref length = %d, want 512", len(sourceRef))
	}
	spec, err := prepareRetryTurnSpec(entity.TurnSpec{
		SourceRef:    sourceRef,
		Attempt:      99,
		ProcessRunID: "",
	}, "11111111-1111-4111-8111-111111111111", digest, digest)
	if err != nil {
		t.Fatalf("prepare retry: %v", err)
	}
	if spec.SourceRef != sourceRef || spec.Attempt != 100 ||
		len(spec.EffectiveInputSHA256) != 64 {
		t.Fatalf("retry changed immutable source or tuple: %#v", spec)
	}
	if _, err := prepareRetryTurnSpec(spec,
		"11111111-1111-4111-8111-111111111111", digest, digest); err == nil {
		t.Fatal("attempt beyond the closed maximum must fail")
	}
}

func TestScheduledExecutionMayWaitOwnerRepeatedly(t *testing.T) {
	t.Parallel()

	for _, states := range [][2]string{{"CLAIMED", "CLAIMED"}, {"CONTINUATION", "CONTINUATION"}} {
		if !scheduledExecutionMayWaitOwner(states[0], states[1]) {
			t.Fatalf("current scheduled execution %v must allow a fresh immutable gate", states)
		}
	}
	for _, states := range [][2]string{{"WAITING_OWNER", "WAITING_OWNER"}, {"CONTINUATION", "CLAIMED"}, {"TERMINAL", "TERMINAL"}} {
		if scheduledExecutionMayWaitOwner(states[0], states[1]) {
			t.Fatalf("incoherent scheduled execution %v must fail closed", states)
		}
	}
}
