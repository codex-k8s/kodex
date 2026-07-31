package restoreagent

import (
	"strings"
	"testing"
	"time"

	internalrpcauthorityv1 "github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestVerifySafeNoDirectiveFailClosedДоПервогоВнешнегоPoll(t *testing.T) {
	agent := &Agent{now: func() time.Time { return time.Unix(100, 0).UTC() }}
	result := &internalrpcauthorityv1.NoRestoreDirective{
		CoordinationRevision: 3,
		RestoreEpoch:         2,
	}
	if err := agent.verifySafeNoDirective(result, nil); err == nil {
		t.Fatal("missing external transition opened startup barrier")
	}
	for _, phase := range []internalrpcauthorityv1.RestorePhase{
		internalrpcauthorityv1.RestorePhase_RESTORE_PHASE_QUIESCING,
		internalrpcauthorityv1.RestorePhase_RESTORE_PHASE_PREPARED,
		internalrpcauthorityv1.RestorePhase_RESTORE_PHASE_RESTORING,
	} {
		transition := safeTransition(phase)
		if err := agent.verifySafeNoDirective(result, transition); err == nil {
			t.Fatalf("phase %s opened startup barrier", phase)
		}
	}
}

func TestVerifySafeNoDirectiveПринимаетТолькоOpenИСозревшийCompleted(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	agent := &Agent{now: func() time.Time { return now }}
	result := &internalrpcauthorityv1.NoRestoreDirective{
		CoordinationRevision: 3,
		RestoreEpoch:         2,
	}
	open := safeTransition(
		internalrpcauthorityv1.RestorePhase_RESTORE_PHASE_OPEN,
	)
	if err := agent.verifySafeNoDirective(result, open); err != nil {
		t.Fatalf("OPEN rejected: %v", err)
	}
	completed := safeTransition(
		internalrpcauthorityv1.RestorePhase_RESTORE_PHASE_COMPLETED,
	)
	completed.SafeWindowNotBefore = timestamppb.New(now.Add(time.Second))
	if err := agent.verifySafeNoDirective(result, completed); err == nil {
		t.Fatal("pre-recovery COMPLETED opened startup barrier")
	}
	completed.SafeWindowNotBefore = timestamppb.New(now)
	if err := agent.verifySafeNoDirective(result, completed); err != nil {
		t.Fatalf("mature COMPLETED rejected: %v", err)
	}
}

func safeTransition(
	phase internalrpcauthorityv1.RestorePhase,
) *internalrpcauthorityv1.RestoreTransition {
	return &internalrpcauthorityv1.RestoreTransition{
		RestoreId:            "00000000-0000-4000-8000-000000000001",
		Phase:                phase,
		AnchorRevision:       2,
		RestoreEpoch:         2,
		EvidenceDigestSha256: strings.Repeat("a", 64),
	}
}
