package app

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/runtime-controller/internal/callback"
	"github.com/codex-k8s/kodex/services/internal/runtime-controller/internal/workload"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type runtimeWorkClientStub struct {
	controlplanev1.RuntimeWorkServiceClient
	mu       sync.Mutex
	requests []*controlplanev1.CompleteExecutionRequest
	complete func(int) error
}

func (client *runtimeWorkClientStub) ReportExecutionProgress(context.Context, *controlplanev1.ReportExecutionProgressRequest, ...grpc.CallOption) (*controlplanev1.ReportExecutionProgressResponse, error) {
	return &controlplanev1.ReportExecutionProgressResponse{}, nil
}

func (client *runtimeWorkClientStub) CompleteExecution(_ context.Context, request *controlplanev1.CompleteExecutionRequest, _ ...grpc.CallOption) (*controlplanev1.CompleteExecutionResponse, error) {
	client.mu.Lock()
	client.requests = append(client.requests, proto.Clone(request).(*controlplanev1.CompleteExecutionRequest))
	attempt := len(client.requests)
	client.mu.Unlock()
	if client.complete != nil {
		if err := client.complete(attempt); err != nil {
			return nil, err
		}
	}
	return &controlplanev1.CompleteExecutionResponse{}, nil
}

func (client *runtimeWorkClientStub) count() int {
	client.mu.Lock()
	defer client.mu.Unlock()
	return len(client.requests)
}

type turnLifecycleStub struct {
	observation workload.TurnPodObservation
	observed    chan struct{}
	once        sync.Once
	deletions   atomic.Int32
}

func (turns *turnLifecycleStub) ObserveTurnPod(context.Context, runtimecontract.RunnerInput, bool) (workload.TurnPodObservation, error) {
	if turns.observed != nil {
		turns.once.Do(func() { close(turns.observed) })
	}
	return turns.observation, nil
}

func (turns *turnLifecycleStub) DeleteTurn(context.Context, string) error {
	turns.deletions.Add(1)
	return nil
}

func TestTrackAcceptsCallbackDuringTerminalGrace(t *testing.T) {
	input := runtimeTrackingInput()
	coordinator := callback.NewCoordinator()
	done := coordinator.Register(input)
	client := &runtimeWorkClientStub{}
	turns := &turnLifecycleStub{
		observation: workload.TurnPodObservation{State: "SUCCEEDED", DiagnosticCode: "POD_SUCCEEDED_WITHOUT_CALLBACK"},
		observed:    make(chan struct{}),
	}
	runtime := trackingRuntime(client, turns, coordinator)

	go func() {
		<-turns.observed
		coordinator.Complete(input.LeaseRef)
	}()
	runtime.track(t.Context(), input, done, false)

	if client.count() != 0 {
		t.Fatalf("failure completion count = %d, want 0", client.count())
	}
	if turns.deletions.Load() != 0 {
		t.Fatalf("runtime deletions = %d, want 0", turns.deletions.Load())
	}
	if len(runtime.capacity) != 0 {
		t.Fatalf("runtime capacity usage = %d, want 0", len(runtime.capacity))
	}
}

func TestTrackRetriesFailureCompletionBeforeCleanup(t *testing.T) {
	input := runtimeTrackingInput()
	coordinator := callback.NewCoordinator()
	done := coordinator.Register(input)
	client := &runtimeWorkClientStub{complete: func(attempt int) error {
		if attempt < 3 {
			return status.Error(codes.Unavailable, "control plane restarting")
		}
		return nil
	}}
	turns := &turnLifecycleStub{observation: workload.TurnPodObservation{State: "FAILED", DiagnosticCode: "ROLE_RUNTIME_EXITED_NONZERO"}}
	runtime := trackingRuntime(client, turns, coordinator)

	runtime.track(t.Context(), input, done, false)

	if client.count() != 3 {
		t.Fatalf("failure completion count = %d, want 3", client.count())
	}
	if turns.deletions.Load() != 1 {
		t.Fatalf("runtime deletions = %d, want 1", turns.deletions.Load())
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	first := client.requests[0]
	for _, request := range client.requests[1:] {
		if request.GetMutation().GetIdempotencyKey() != first.GetMutation().GetIdempotencyKey() ||
			request.GetLeaseRef() != first.GetLeaseRef() || request.GetFence() != first.GetFence() ||
			request.GetGeneration() != first.GetGeneration() {
			t.Fatalf("failure retry changed the immutable request: first=%#v retry=%#v", first, request)
		}
	}
}

func TestCompleteFailureKeepsRuntimeUntilDurableCommit(t *testing.T) {
	input := runtimeTrackingInput()
	coordinator := callback.NewCoordinator()
	done := coordinator.Register(input)
	client := &runtimeWorkClientStub{complete: func(int) error {
		return status.Error(codes.Unavailable, "control plane unavailable")
	}}
	turns := &turnLifecycleStub{}
	runtime := trackingRuntime(client, turns, coordinator)
	runtime.completionRetries = nil

	runtime.completeFailure(t.Context(), input, "RUNTIME_WORKLOAD_EXITED", "ROLE_RUNTIME_EXITED_NONZERO")

	if client.count() != 1 {
		t.Fatalf("failure completion count = %d, want 1", client.count())
	}
	if turns.deletions.Load() != 0 {
		t.Fatalf("runtime deletions = %d, want 0", turns.deletions.Load())
	}
	select {
	case <-done:
		t.Fatal("coordinator completed before durable control-plane commit")
	default:
	}
}

func trackingRuntime(client controlplanev1.RuntimeWorkServiceClient, turns turnLifecycle, coordinator *callback.Coordinator) *runtime {
	capacity := make(chan struct{}, 1)
	capacity <- struct{}{}
	return &runtime{
		control:     client,
		turns:       turns,
		coordinator: coordinator,
		config: Config{
			RequestTimeout:     100 * time.Millisecond,
			ExecutionTimeout:   time.Second,
			LeaseRenewInterval: time.Hour,
		},
		logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		capacity:          capacity,
		inspectInterval:   time.Millisecond,
		terminalGrace:     3 * time.Millisecond,
		completionRetries: []time.Duration{time.Millisecond, time.Millisecond},
	}
}

func runtimeTrackingInput() runtimecontract.RunnerInput {
	return runtimecontract.RunnerInput{
		LeaseRef:        "lease_runtime_tracking_abcdefgh",
		LeaseFence:      "fence_runtime_tracking_abcdefgh",
		LeaseGeneration: 3,
	}
}
