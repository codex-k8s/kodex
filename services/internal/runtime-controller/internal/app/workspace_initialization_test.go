package app

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/runtime-controller/internal/callback"
	"github.com/codex-k8s/kodex/services/internal/runtime-controller/internal/workload"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// TestInitializationCompletionFreezesSummaryBeforeCleanup exercises existing bounded delivery.
// Each transient retry carries identical bytes; owner denial cannot log acknowledgment.
// TestInitializationCompletionFreezesSummaryBeforeCleanup проверяет existing bounded delivery.
// Transient retries несут одинаковые bytes; owner denial не логирует acknowledgment.
func TestInitializationCompletionFreezesSummaryBeforeCleanup(t *testing.T) {
	for _, scenario := range []string{"ack", "transient", "denied", "unavailable", "already-exists"} {
		t.Run(scenario, func(t *testing.T) {
			input := runtimeTrackingInput()
			input.RuntimeRevisionDigest = strings.Repeat("a", 64)
			input.InputDigest = strings.Repeat("b", 64)
			input.ExecutionBindingDigest = strings.Repeat("c", 64)
			input.SessionRef = "ses_fixture123"
			input.TurnRef = "trn_fixture123"
			input.Attempt = 1
			coordinator := callback.NewCoordinator()
			coordinator.Register(input)
			turns := &turnLifecycleStub{}
			client := &runtimeWorkClientStub{complete: func(attempt int) error {
				if turns.deletions.Load() != 0 {
					t.Fatal("cleanup preceded owner acknowledgment")
				}
				switch scenario {
				case "transient":
					if attempt < 3 {
						return status.Error(codes.Unavailable, "PRIVATE_SENTINEL")
					}
				case "denied":
					return status.Error(codes.PermissionDenied, "PRIVATE_SENTINEL")
				case "unavailable":
					return status.Error(codes.Unavailable, "PRIVATE_SENTINEL")
				case "already-exists":
					return status.Error(codes.AlreadyExists, "PRIVATE_SENTINEL")
				}
				return nil
			}}
			runtime := trackingRuntime(client, turns, coordinator)
			var log bytes.Buffer
			runtime.logger = slog.New(slog.NewTextHandler(&log, nil))
			observation := &workload.InitializationObservation{PodUID: "10000000-0000-4000-8000-000000001941", Stage: "ARTIFACT_TRANSFER", Reason: "AUTHORITY_REJECTED", RecordValidated: true}
			runtime.completeFailureWithInitialization(t.Context(), input, observation)
			client.mu.Lock()
			defer client.mu.Unlock()
			if len(client.requests) == 0 {
				t.Fatal("owner completion missing")
			}
			first := client.requests[0]
			if first.GetSuccess() || first.GetSafeErrorCode() != runtimecontract.InitializationFailureCode || first.GetResultSummary() != "i18n:RUNTIME_INIT_ARTIFACT_TRANSFER_AUTHORITY_REJECTED" || first.GetLeaseRef() != input.LeaseRef || first.GetGeneration() != input.LeaseGeneration {
				t.Fatal("init outcome or exact tuple changed")
			}
			for _, request := range client.requests[1:] {
				if !proto.Equal(first, request) {
					t.Fatal("retry changed immutable completion")
				}
			}
			ack := scenario == "ack" || scenario == "transient"
			if strings.Contains(log.String(), initializationAcknowledgedMessage) != ack || strings.Contains(log.String(), "PRIVATE_SENTINEL") {
				t.Fatal("ack log fabricated or raw cause leaked")
			}
			if scenario == "unavailable" {
				if turns.deletions.Load() != 0 {
					t.Fatal("unconfirmed owner completion deleted resources")
				}
			} else if turns.deletions.Load() != 1 {
				t.Fatal("existing cleanup boundary changed")
			}
		})
	}
}

// TestTrackInitializationKeepsCallbackWinner proves the existing grace still permits completion.
// TestTrackInitializationKeepsCallbackWinner проверяет сохранение callback winner в grace.
func TestTrackInitializationKeepsCallbackWinner(t *testing.T) {
	input := runtimeTrackingInput()
	coordinator := callback.NewCoordinator()
	done := coordinator.Register(input)
	client := &runtimeWorkClientStub{}
	turns := &turnLifecycleStub{observed: make(chan struct{}), observation: workload.TurnPodObservation{State: "FAILED", DiagnosticCode: "WORKSPACE_INIT_EXITED_NONZERO", Initialization: &workload.InitializationObservation{PodUID: "10000000-0000-4000-8000-000000001941", Stage: "UNKNOWN", Reason: "UNKNOWN"}}}
	runtime := trackingRuntime(client, turns, coordinator)
	runtime.terminalGrace = time.Second
	go func() { <-turns.observed; coordinator.Complete(input.LeaseRef) }()
	runtime.track(context.Background(), input, done, false, time.Time{})
	if client.count() != 0 || turns.deletions.Load() != 0 {
		t.Fatal("init fallback replaced callback winner")
	}
}

// replacingInitializationTurns changes the current Pod once while keeping one immutable lease.
// replacingInitializationTurns один раз меняет current Pod при неизменной lease.
type replacingInitializationTurns struct {
	turnLifecycleStub
	calls      int
	replacedAt time.Time
}

// ObserveTurnPod supplies a current-UID change without sleeping or invoking Kubernetes.
// ObserveTurnPod задаёт смену current UID без sleep либо обращения к Kubernetes.
func (turns *replacingInitializationTurns) ObserveTurnPod(context.Context, runtimecontract.RunnerInput, bool) (workload.TurnPodObservation, error) {
	turns.calls++
	uid := "10000000-0000-4000-8000-000000001941"
	if turns.calls > 1 {
		uid = "20000000-0000-4000-8000-000000001941"
		if turns.replacedAt.IsZero() {
			turns.replacedAt = time.Now()
		}
	}
	return workload.TurnPodObservation{State: "FAILED", DiagnosticCode: "WORKSPACE_INIT_EXITED_NONZERO", Initialization: &workload.InitializationObservation{PodUID: uid, Stage: "UNKNOWN", Reason: "UNKNOWN"}}, nil
}

// TestTrackInitializationRestartsGraceForReplacementUID prevents stale Pod attribution.
// The completion must wait a whole grace after the replacement's first observation.
// TestTrackInitializationRestartsGraceForReplacementUID предотвращает stale Pod attribution.
// Completion ждёт полный grace после первого наблюдения replacement.
func TestTrackInitializationRestartsGraceForReplacementUID(t *testing.T) {
	input := runtimeTrackingInput()
	coordinator := callback.NewCoordinator()
	done := coordinator.Register(input)
	turns := &replacingInitializationTurns{}
	grace := 20 * time.Millisecond
	client := &runtimeWorkClientStub{complete: func(int) error {
		if turns.replacedAt.IsZero() || time.Since(turns.replacedAt) < grace {
			t.Fatal("old Pod grace completed replacement")
		}
		return nil
	}}
	runtime := trackingRuntime(client, turns, coordinator)
	runtime.terminalGrace = grace
	runtime.track(t.Context(), input, done, false, time.Time{})
	if client.count() != 1 || turns.deletions.Load() != 1 {
		t.Fatal("replacement produced duplicate completion")
	}
}
