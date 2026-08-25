package restoreagent

import (
	"context"
	"strings"
	"testing"
	"time"

	internalrpcauthorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestPollНеЗакрываетAdmissionВоВремяШтатнойПроверки(t *testing.T) {
	admission := &recordingAdmission{}
	agent := &Agent{
		now: func() time.Time { return time.Unix(100, 0).UTC() },
		directiveFetcher: func(context.Context) (
			*internalrpcauthorityv1.GetRestoreDirectiveResponse,
			string,
			error,
		) {
			if len(admission.calls) != 0 {
				t.Fatalf("admission changed before restore controller response: %v", admission.calls)
			}
			return &internalrpcauthorityv1.GetRestoreDirectiveResponse{
				Result: &internalrpcauthorityv1.GetRestoreDirectiveResponse_NoDirective{
					NoDirective: &internalrpcauthorityv1.NoRestoreDirective{
						CoordinationRevision: 3,
						RestoreEpoch:         2,
						VerifiedTransition: safeTransition(
							internalrpcauthorityv1.RestorePhase_RESTORE_PHASE_OPEN,
						),
					},
				},
			}, "", nil
		},
	}
	if err := agent.Poll(context.Background(), admission); err != nil {
		t.Fatalf("poll failed: %v", err)
	}
	expected := []string{"served-state-ready", "restore-blocked:false", "available:true"}
	if strings.Join(admission.calls, ",") != strings.Join(expected, ",") {
		t.Fatalf("unexpected admission transitions: got %v, want %v", admission.calls, expected)
	}
}

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

type recordingAdmission struct {
	calls []string
}

func (admission *recordingAdmission) SetAvailable(value bool) {
	admission.calls = append(admission.calls, "available:"+boolString(value))
}

func (admission *recordingAdmission) SetRestoreBlocked(value bool) {
	admission.calls = append(admission.calls, "restore-blocked:"+boolString(value))
}

func (*recordingAdmission) WaitDrained(context.Context) error { return nil }
func (*recordingAdmission) Inflight() int64                   { return 0 }
func (*recordingAdmission) SnapshotState() model.SnapshotState {
	return model.SnapshotState{}
}

func (admission *recordingAdmission) ServedStateReady(context.Context) error {
	admission.calls = append(admission.calls, "served-state-ready")
	return nil
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
