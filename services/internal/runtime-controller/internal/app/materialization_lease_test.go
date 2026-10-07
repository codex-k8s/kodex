package app

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type materializationRenewClient struct {
	controlplanev1.RuntimeWorkServiceClient
	mu       sync.Mutex
	requests []*controlplanev1.RenewExecutionRequest
	renew    func(context.Context, *controlplanev1.RenewExecutionRequest) error
}

func (client *materializationRenewClient) RenewExecution(ctx context.Context, request *controlplanev1.RenewExecutionRequest, _ ...grpc.CallOption) (*controlplanev1.RenewExecutionResponse, error) {
	client.mu.Lock()
	client.requests = append(client.requests, proto.Clone(request).(*controlplanev1.RenewExecutionRequest))
	client.mu.Unlock()
	if client.renew != nil {
		if err := client.renew(ctx, request); err != nil {
			return nil, err
		}
	}
	return &controlplanev1.RenewExecutionResponse{}, nil
}

func keeperTestRuntime(client *materializationRenewClient) *runtime {
	return &runtime{control: client, config: Config{RequestTimeout: time.Second, LeaseRenewInterval: 5 * time.Millisecond}}
}

func awaitKeeperTest(t *testing.T, channel <-chan struct{}) {
	t.Helper()
	select {
	case <-channel:
	case <-time.After(time.Second):
		t.Fatal("lease keeper did not reach the expected phase")
	}
}

func TestMaterializationKeepersRenewLaterLeaseWhileFirstIsDelayed(t *testing.T) {
	// Масштабированная модель: owner TTL 30ms, первая материализация 90ms.
	// Изменения production TTL или конфигурации для теста не требуются.
	var mu sync.Mutex
	expires := map[string]time.Time{}
	client := &materializationRenewClient{renew: func(_ context.Context, request *controlplanev1.RenewExecutionRequest) error {
		mu.Lock()
		expires[request.LeaseRef] = time.Now().Add(30 * time.Millisecond)
		mu.Unlock()
		return nil
	}}
	runtime := keeperTestRuntime(client)
	firstInput, laterInput := runtimeTrackingInput(), runtimeTrackingInput()
	laterInput.LeaseRef += "_later"
	keepers := runtime.keepMaterializationClaims(t.Context(), []*controlplanev1.ClaimedExecution{
		{Lease: &controlplanev1.WorkLease{Ref: firstInput.LeaseRef, Fence: firstInput.LeaseFence, Generation: firstInput.LeaseGeneration}},
		{Lease: &controlplanev1.WorkLease{Ref: laterInput.LeaseRef, Fence: laterInput.LeaseFence, Generation: laterInput.LeaseGeneration}},
	})
	first, later := keepers[0], keepers[1]
	defer first.stop()
	defer later.stop()
	if first.await() != nil || later.await() != nil {
		t.Fatal("initial exact renew failed")
	}
	timer := time.NewTimer(90 * time.Millisecond)
	defer timer.Stop()
	<-timer.C
	if later.publish(func(ctx context.Context) error {
		mu.Lock()
		defer mu.Unlock()
		if ctx.Err() != nil || !expires[laterInput.LeaseRef].After(time.Now()) {
			t.Fatal("later claim expired behind the first materialization")
		}
		return nil
	}) != nil {
		t.Fatal("later claim could not publish")
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	counts := map[string]int{}
	for _, request := range client.requests {
		counts[request.LeaseRef]++
		if request.Fence != firstInput.LeaseFence || request.Generation != firstInput.LeaseGeneration {
			t.Fatal("renew changed the exact fence or generation")
		}
	}
	if counts[firstInput.LeaseRef] < 3 || counts[laterInput.LeaseRef] < 3 {
		t.Fatal("batch claims were not renewed during materialization")
	}
}

func TestMaterializationKeeperInitialDenialPreventsPublication(t *testing.T) {
	denied := status.Error(codes.PermissionDenied, "lease revoked")
	client := &materializationRenewClient{renew: func(context.Context, *controlplanev1.RenewExecutionRequest) error { return denied }}
	keeper := keeperTestRuntime(client).keepMaterializationLease(t.Context(), runtimeTrackingInput())
	defer keeper.stop()
	if !errors.Is(keeper.await(), denied) {
		t.Fatal("initial owner denial was lost")
	}
	called := false
	if !errors.Is(keeper.publish(func(context.Context) error { called = true; return nil }), denied) || called {
		t.Fatal("revoked claim published a workload")
	}
	if !errors.Is(keeper.handoff(), denied) {
		t.Fatal("revoked claim was handed to tracker")
	}
}

func TestMaterializationKeeperPeriodicDenialCancelsWaitingMaterialization(t *testing.T) {
	var reject atomic.Bool
	denied := status.Error(codes.PermissionDenied, "lease revoked")
	client := &materializationRenewClient{renew: func(context.Context, *controlplanev1.RenewExecutionRequest) error {
		if reject.Load() {
			return denied
		}
		return nil
	}}
	keeper := keeperTestRuntime(client).keepMaterializationLease(t.Context(), runtimeTrackingInput())
	defer keeper.stop()
	if keeper.await() != nil {
		t.Fatal("initial renew failed")
	}
	reject.Store(true)
	awaitKeeperTest(t, keeper.ctx.Done())
	called := false
	if !errors.Is(keeper.publish(func(context.Context) error { called = true; return nil }), denied) || called {
		t.Fatal("periodic owner denial allowed publication")
	}
}

func TestMaterializationPublicationFreshRenewDenialPreventsEffect(t *testing.T) {
	var calls atomic.Int32
	denied := status.Error(codes.PermissionDenied, "lease revoked")
	client := &materializationRenewClient{renew: func(context.Context, *controlplanev1.RenewExecutionRequest) error {
		if calls.Add(1) > 1 {
			return denied
		}
		return nil
	}}
	runtime := keeperTestRuntime(client)
	runtime.config.LeaseRenewInterval = time.Hour
	keeper := runtime.keepMaterializationLease(t.Context(), runtimeTrackingInput())
	defer keeper.stop()
	if keeper.await() != nil {
		t.Fatal("initial renew failed")
	}
	called := false
	if !errors.Is(keeper.publish(func(context.Context) error { called = true; return nil }), denied) || called {
		t.Fatal("publication did not honor its fresh owner fence")
	}
}

func TestMaterializationKeeperHandoffJoinsBeforeFinalRenew(t *testing.T) {
	var active, maximum atomic.Int32
	client := &materializationRenewClient{renew: func(context.Context, *controlplanev1.RenewExecutionRequest) error {
		value := active.Add(1)
		for old := maximum.Load(); value > old && !maximum.CompareAndSwap(old, value); old = maximum.Load() {
		}
		defer active.Add(-1)
		return nil
	}}
	runtime := keeperTestRuntime(client)
	runtime.config.LeaseRenewInterval = time.Hour
	keeper := runtime.keepMaterializationLease(t.Context(), runtimeTrackingInput())
	defer keeper.stop()
	if keeper.await() != nil || keeper.handoff() != nil {
		t.Fatal("handoff failed")
	}
	select {
	case <-keeper.done:
	default:
		t.Fatal("tracker received a live competing keeper")
	}
	client.mu.Lock()
	count := len(client.requests)
	client.mu.Unlock()
	if count != 2 || maximum.Load() != 1 {
		t.Fatal("handoff did not preserve a single exact renew owner")
	}
}

func TestMaterializationPublicationHasBoundedBudget(t *testing.T) {
	runtime := keeperTestRuntime(&materializationRenewClient{})
	runtime.config.RequestTimeout = 5 * time.Millisecond
	keeper := runtime.keepMaterializationLease(t.Context(), runtimeTrackingInput())
	defer keeper.stop()
	if keeper.await() != nil {
		t.Fatal("initial renew failed")
	}
	err := keeper.publish(func(ctx context.Context) error {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("publication blocked periodic renew without a deadline")
		}
		<-ctx.Done()
		return ctx.Err()
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("publication did not honor its independent bounded budget")
	}
}

func TestMaterializationPublicationWaitsForInFlightDenial(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	denied := status.Error(codes.PermissionDenied, "lease revoked")
	client := &materializationRenewClient{renew: func(ctx context.Context, _ *controlplanev1.RenewExecutionRequest) error {
		if calls.Add(1) == 2 {
			close(entered)
			select {
			case <-release:
				return denied
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}}
	keeper := keeperTestRuntime(client).keepMaterializationLease(t.Context(), runtimeTrackingInput())
	defer keeper.stop()
	if keeper.await() != nil {
		t.Fatal("initial renew failed")
	}
	awaitKeeperTest(t, entered)
	var published atomic.Bool
	finished := make(chan struct{})
	var publishErr error
	go func() {
		publishErr = keeper.publish(func(context.Context) error { published.Store(true); return nil })
		close(finished)
	}()
	close(release)
	awaitKeeperTest(t, finished)
	if !errors.Is(publishErr, denied) || published.Load() {
		t.Fatal("publication passed an in-flight owner denial")
	}
}

func TestMaterializationHandoffJoinsInFlightRenew(t *testing.T) {
	entered := make(chan struct{})
	var calls, active, maximum atomic.Int32
	client := &materializationRenewClient{renew: func(ctx context.Context, _ *controlplanev1.RenewExecutionRequest) error {
		value := active.Add(1)
		for old := maximum.Load(); value > old && !maximum.CompareAndSwap(old, value); old = maximum.Load() {
		}
		defer active.Add(-1)
		if calls.Add(1) == 2 {
			close(entered)
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	}}
	keeper := keeperTestRuntime(client).keepMaterializationLease(t.Context(), runtimeTrackingInput())
	defer keeper.stop()
	if keeper.await() != nil {
		t.Fatal("initial renew failed")
	}
	awaitKeeperTest(t, entered)
	if keeper.handoff() != nil || maximum.Load() != 1 || calls.Load() != 3 {
		t.Fatal("handoff raced the keeper instead of joining it")
	}
}

func TestMaterializationKeeperShutdownCancelsAndJoinsInFlightRPC(t *testing.T) {
	entered := make(chan struct{})
	var once sync.Once
	var active atomic.Int32
	client := &materializationRenewClient{renew: func(ctx context.Context, _ *controlplanev1.RenewExecutionRequest) error {
		active.Add(1)
		defer active.Add(-1)
		once.Do(func() { close(entered) })
		<-ctx.Done()
		return ctx.Err()
	}}
	ctx, cancel := context.WithCancel(t.Context())
	keeper := keeperTestRuntime(client).keepMaterializationLease(ctx, runtimeTrackingInput())
	awaitKeeperTest(t, entered)
	cancel()
	finished := make(chan struct{})
	go func() { keeper.stop(); close(finished) }()
	awaitKeeperTest(t, finished)
	if active.Load() != 0 || !errors.Is(context.Cause(keeper.ctx), context.Canceled) {
		t.Fatal("shutdown left an external renew in flight")
	}
}

func TestMaterializationKeeperInvalidTupleDoesNotCallOwner(t *testing.T) {
	for _, input := range []runtimecontract.RunnerInput{{}, {LeaseRef: "lease_test", LeaseFence: "fence_test"}} {
		client := &materializationRenewClient{}
		keeper := keeperTestRuntime(client).keepMaterializationLease(t.Context(), input)
		if keeper.await() == nil {
			t.Fatal("invalid tuple was admitted")
		}
		keeper.stop()
		if len(client.requests) != 0 {
			t.Fatal("invalid tuple called the owner")
		}
	}
}

func TestMaterializationHandoffDoesNotHideConcurrentOwnerDenial(t *testing.T) {
	entered := make(chan struct{})
	var calls atomic.Int32
	denied := status.Error(codes.PermissionDenied, "lease revoked")
	client := &materializationRenewClient{renew: func(ctx context.Context, _ *controlplanev1.RenewExecutionRequest) error {
		if calls.Add(1) == 2 {
			close(entered)
			<-ctx.Done()
			return denied
		}
		return nil
	}}
	keeper := keeperTestRuntime(client).keepMaterializationLease(t.Context(), runtimeTrackingInput())
	defer keeper.stop()
	if keeper.await() != nil {
		t.Fatal("initial renew failed")
	}
	awaitKeeperTest(t, entered)
	if !errors.Is(keeper.handoff(), denied) || calls.Load() != 2 {
		t.Fatal("local handoff cancellation hid an owner denial or retried it")
	}
}
