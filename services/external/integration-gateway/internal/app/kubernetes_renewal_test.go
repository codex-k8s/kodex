package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/integration"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// kubernetesRenewalOwner supplies only the actual renewal RPC used by the activity wrapper; other owner operations must remain unreachable here.
// kubernetesRenewalOwner предоставляет только фактический renewal RPC обёртки activity; остальные owner operations здесь недостижимы.
type kubernetesRenewalOwner struct {
	cp.ManagedKubernetesOwnerServiceClient
	renew func(context.Context, *cp.RenewManagedKubernetesInvocationRequest) (*cp.RenewManagedKubernetesInvocationResponse, error)
	read  func(context.Context, *cp.GetManagedKubernetesInvocationRequest) (*cp.GetManagedKubernetesInvocationResponse, error)
}

// RenewManagedKubernetesInvocation forwards the observed request and cancellation context unchanged to the controlled owner boundary.
// RenewManagedKubernetesInvocation передаёт наблюдаемый request и cancellation context в контролируемую owner boundary без изменений.
func (owner *kubernetesRenewalOwner) RenewManagedKubernetesInvocation(ctx context.Context, request *cp.RenewManagedKubernetesInvocationRequest, _ ...grpc.CallOption) (*cp.RenewManagedKubernetesInvocationResponse, error) {
	return owner.renew(ctx, request)
}

// GetManagedKubernetesInvocation supplies a fresh exact closed receipt for independent cleanup, without creating a success result or another execution grant.
// GetManagedKubernetesInvocation предоставляет свежую точную closed receipt для независимой очистки без создания success result либо нового execution grant.
func (owner *kubernetesRenewalOwner) GetManagedKubernetesInvocation(ctx context.Context, request *cp.GetManagedKubernetesInvocationRequest, _ ...grpc.CallOption) (*cp.GetManagedKubernetesInvocationResponse, error) {
	return owner.read(ctx, request)
}

// kubernetesRenewalInput uses the production claim preparation and a short explicit test lease, avoiding a second request mapper.
// kubernetesRenewalInput использует production claim preparation и короткую явную тестовую lease без второго request mapper.
func kubernetesRenewalInput(t *testing.T, lifetime time.Duration) kubernetesActivityInput {
	t.Helper()
	claim := kubernetesConsumerClaim()
	claim.Lease.ExpiresAt = timestamppb.New(time.Now().Add(lifetime))
	request, binding, err := integration.PrepareKubernetesInvocation(claim, "gateway_fixture")
	if err != nil {
		t.Fatal(err)
	}
	return kubernetesActivityInput{request: request, binding: binding, leaseUntil: claim.Lease.ExpiresAt.AsTime()}
}

// awaitKubernetesRenewal bounds asynchronous assertions without replacing a missing positive join with a successful test result.
// awaitKubernetesRenewal ограничивает асинхронные assertions, не заменяя отсутствующий positive join успешным результатом теста.
func awaitKubernetesRenewal[T any](t *testing.T, channel <-chan T) T {
	t.Helper()
	select {
	case value := <-channel:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("Kubernetes renewal activity did not reach expected boundary")
	}
	var zero T
	return zero
}

// TestKubernetesRenewalExtendsConfirmedLifetime keeps the execution alive past the initial deadline only after the actual owner adapter validates a renewal, then joins every owned worker on normal completion.
// TestKubernetesRenewalExtendsConfirmedLifetime сохраняет execution после исходного срока только после проверки renewal фактическим owner adapter, затем присоединяет все workers при обычном завершении.
func TestKubernetesRenewalExtendsConfirmedLifetime(t *testing.T) {
	input := kubernetesRenewalInput(t, 600*time.Millisecond)
	requests := make(chan *cp.RenewManagedKubernetesInvocationRequest, 8)
	owner := &kubernetesRenewalOwner{renew: func(_ context.Context, request *cp.RenewManagedKubernetesInvocationRequest) (*cp.RenewManagedKubernetesInvocationResponse, error) {
		requests <- request
		return &cp.RenewManagedKubernetesInvocationResponse{LeaseRef: request.Binding.LeaseRef, Generation: request.Binding.Generation, ExpiresAt: timestamppb.New(time.Now().Add(time.Second))}, nil
	}}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finish := make(chan struct{})
	var finishOnce sync.Once
	release := func() { finishOnce.Do(func() { close(finish) }) }
	defer release()
	workContext := make(chan context.Context, 1)
	joined := make(chan error, 1)
	go func() {
		joined <- runKubernetesRenewed(ctx, input, owner, func(work context.Context, _ kubernetesActivityInput) error {
			workContext <- work
			select {
			case <-finish:
				return nil
			case <-work.Done():
				return work.Err()
			}
		})
	}()
	work := awaitKubernetesRenewal(t, workContext)
	request := awaitKubernetesRenewal(t, requests)
	if request.Binding.Fence != input.binding.Fence || request.Binding.Generation != input.binding.Generation || request.Binding.LeaseRef != input.binding.LeaseRef {
		t.Fatal("renewal changed original claim")
	}
	timer := time.NewTimer(time.Until(input.leaseUntil.Add(100 * time.Millisecond)))
	defer timer.Stop()
	select {
	case <-work.Done():
		t.Fatal("confirmed renewal did not extend execution lifetime")
	case <-timer.C:
	}
	release()
	if err := awaitKubernetesRenewal(t, joined); err != nil {
		t.Fatal("normal activity completion failed to join", err)
	}
}

// TestKubernetesRenewalFailureCancelsButWaitsForCleanup rejects a lost renewal acknowledgement and proves cancellation does not release execution before cleanup returns.
// TestKubernetesRenewalFailureCancelsButWaitsForCleanup отклоняет потерянное renewal acknowledgement и доказывает, что cancellation не освобождает execution до возврата очистки.
func TestKubernetesRenewalFailureCancelsButWaitsForCleanup(t *testing.T) {
	input := kubernetesRenewalInput(t, 600*time.Millisecond)
	owner := &kubernetesRenewalOwner{renew: func(context.Context, *cp.RenewManagedKubernetesInvocationRequest) (*cp.RenewManagedKubernetesInvocationResponse, error) {
		return nil, errors.New("synthetic lost renewal acknowledgement")
	}}
	canceled := make(chan struct{})
	cleanup := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(cleanup) }) }
	defer release()
	joined := make(chan error, 1)
	go func() {
		joined <- runKubernetesRenewed(t.Context(), input, owner, func(ctx context.Context, _ kubernetesActivityInput) error {
			<-ctx.Done()
			close(canceled)
			<-cleanup
			return ctx.Err()
		})
	}()
	awaitKubernetesRenewal(t, canceled)
	select {
	case <-joined:
		t.Fatal("cancellation was mistaken for cleanup join")
	default:
	}
	release()
	if err := awaitKubernetesRenewal(t, joined); !errors.Is(err, errKubernetesLease) {
		t.Fatal("lost renewal acknowledgement created successful execution")
	}
}

// TestKubernetesRenewalWatchdogSurvivesBlockedRPC expires execution independently of an unresponsive renewal and retains the slot until that local RPC actually returns.
// TestKubernetesRenewalWatchdogSurvivesBlockedRPC завершает срок execution независимо от зависшего renewal и сохраняет slot до фактического возврата локального RPC.
func TestKubernetesRenewalWatchdogSurvivesBlockedRPC(t *testing.T) {
	input := kubernetesRenewalInput(t, 600*time.Millisecond)
	started := make(chan struct{})
	rpcRelease := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(rpcRelease) }) }
	defer release()
	owner := &kubernetesRenewalOwner{renew: func(_ context.Context, request *cp.RenewManagedKubernetesInvocationRequest) (*cp.RenewManagedKubernetesInvocationResponse, error) {
		close(started)
		<-rpcRelease
		return &cp.RenewManagedKubernetesInvocationResponse{LeaseRef: request.Binding.LeaseRef, Generation: request.Binding.Generation, ExpiresAt: timestamppb.New(time.Now().Add(time.Second))}, nil
	}}
	canceled := make(chan struct{})
	joined := make(chan error, 1)
	go func() {
		joined <- runKubernetesRenewed(t.Context(), input, owner, func(ctx context.Context, _ kubernetesActivityInput) error {
			<-ctx.Done()
			close(canceled)
			return ctx.Err()
		})
	}()
	awaitKubernetesRenewal(t, started)
	awaitKubernetesRenewal(t, canceled)
	select {
	case <-joined:
		t.Fatal("blocked renewal was declared joined")
	default:
	}
	release()
	if err := awaitKubernetesRenewal(t, joined); !errors.Is(err, errKubernetesLease) {
		t.Fatal("late renewal revived expired execution")
	}
}

// TestKubernetesRenewalRejectsExpiredStart prevents expired or canceled activity input from starting even the controlled execution callback.
// TestKubernetesRenewalRejectsExpiredStart запрещает expired либо canceled activity input запускать даже контролируемый execution callback.
func TestKubernetesRenewalRejectsExpiredStart(t *testing.T) {
	input := kubernetesRenewalInput(t, time.Second)
	input.leaseUntil = time.Now().Add(-time.Second)
	owner := &kubernetesRenewalOwner{renew: func(context.Context, *cp.RenewManagedKubernetesInvocationRequest) (*cp.RenewManagedKubernetesInvocationResponse, error) {
		t.Error("expired start reached owner")
		return nil, errKubernetesLease
	}}
	called := false
	if err := runKubernetesRenewed(t.Context(), input, owner, func(context.Context, kubernetesActivityInput) error { called = true; return nil }); err == nil || called {
		t.Fatal("expired lease started execution")
	}
}

// TestKubernetesConsumerRetainsSlotUntilRenewalJoins connects actual claim adoption to the renewal wrapper and proves consumer shutdown cannot release a blocked local owner RPC.
// TestKubernetesConsumerRetainsSlotUntilRenewalJoins связывает actual claim adoption с renewal wrapper и доказывает, что consumer shutdown не освобождает зависший локальный owner RPC.
func TestKubernetesConsumerRetainsSlotUntilRenewalJoins(t *testing.T) {
	started := make(chan struct{})
	rpcRelease := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(rpcRelease) }) }
	defer release()
	owner := &kubernetesRenewalOwner{renew: func(_ context.Context, _ *cp.RenewManagedKubernetesInvocationRequest) (*cp.RenewManagedKubernetesInvocationResponse, error) {
		close(started)
		<-rpcRelease
		return nil, errors.New("synthetic interrupted renewal")
	}}
	canceled := make(chan struct{})
	consumer := newKubernetesConsumerFixture(t, 1, func(ctx context.Context, input kubernetesActivityInput) error {
		return runKubernetesRenewed(ctx, input, owner, func(work context.Context, _ kubernetesActivityInput) error {
			<-work.Done()
			close(canceled)
			return work.Err()
		})
	})
	slot, ok := consumer.ReserveClaim()
	if !ok {
		t.Fatal("reservation rejected")
	}
	claim := kubernetesConsumerClaim()
	claim.Lease.ExpiresAt = timestamppb.New(time.Now().Add(600 * time.Millisecond))
	if _, err := slot.ExecuteClaim(t.Context(), claim); err != nil {
		t.Fatal(err)
	}
	awaitKubernetesRenewal(t, started)
	if err := consumer.Close(); err != nil {
		t.Fatal(err)
	}
	awaitKubernetesRenewal(t, canceled)
	observation, cancel := context.WithCancel(t.Context())
	cancel()
	if err := consumer.Wait(observation); !errors.Is(err, context.Canceled) || consumer.Joined() {
		t.Fatal("consumer released an unjoined owner RPC")
	}
	slot.ReleaseUnused()
	if consumer.Joined() {
		t.Fatal("ReleaseUnused abandoned adopted renewal")
	}
	release()
	bounded, stop := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Second)
	defer stop()
	if err := consumer.Wait(bounded); !errors.Is(err, errKubernetesActivity) || !consumer.Joined() {
		t.Fatal("joined cancellation lost lifecycle failure")
	}
}

// TestKubernetesRenewalTerminalProofResolvesOnlyLeaseLoss follows expiry through actual owner read validation; a malformed renewal remains fatal even after the same terminal proof.
// TestKubernetesRenewalTerminalProofResolvesOnlyLeaseLoss проводит expiry через actual owner read validation; malformed renewal остаётся fatal даже после того же terminal proof.
func TestKubernetesRenewalTerminalProofResolvesOnlyLeaseLoss(t *testing.T) {
	for _, malformed := range []bool{false, true} {
		input := kubernetesRenewalInput(t, 300*time.Millisecond)
		owner := &kubernetesRenewalOwner{}
		owner.renew = func(ctx context.Context, request *cp.RenewManagedKubernetesInvocationRequest) (*cp.RenewManagedKubernetesInvocationResponse, error) {
			if !malformed {
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return &cp.RenewManagedKubernetesInvocationResponse{LeaseRef: request.Binding.LeaseRef, Generation: request.Binding.Generation + 1, ExpiresAt: timestamppb.New(time.Now().Add(time.Second))}, nil
		}
		owner.read = func(_ context.Context, request *cp.GetManagedKubernetesInvocationRequest) (*cp.GetManagedKubernetesInvocationResponse, error) {
			b := request.Binding
			digest, err := managedkubernetes.BindingDigest(managedkubernetes.Binding{WorkloadInstance: b.WorkloadInstance, InvocationRef: b.InvocationRef, LeaseRef: b.LeaseRef, Fence: b.Fence, Generation: b.Generation, InputDigest: b.InputDigest})
			if err != nil {
				return nil, err
			}
			return &cp.GetManagedKubernetesInvocationResponse{Receipt: &cp.ManagedKubernetesInvocationReceipt{CompletionPendingReason: cp.ManagedKubernetesCompletionPendingReason_MANAGED_KUBERNETES_COMPLETION_PENDING_REASON_NONE, InvocationRef: b.InvocationRef, Generation: b.Generation, InputDigest: b.InputDigest, BindingDigest: digest, State: cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_CANCELLED}}, nil
		}
		var proved bool
		err := runKubernetesRenewed(t.Context(), input, owner, func(ctx context.Context, actual kubernetesActivityInput) error {
			<-ctx.Done()
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
			defer cancel()
			receipt, err := integration.ReadKubernetesInvocation(cleanup, actual.binding, owner)
			if err != nil {
				return err
			}
			if receipt.State != cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_CANCELLED || receipt.CompletionRecorded {
				return errKubernetesActivity
			}
			proved = true
			return nil
		})
		if !proved || !malformed && err != nil || malformed && !errors.Is(err, errKubernetesRenewalProtocol) {
			t.Fatal("terminal proof incorrectly resolved lifecycle outcome")
		}
	}
	if kubernetesResolvedLeaseLoss(errors.Join(context.Canceled, errKubernetesRenewalProtocol)) {
		t.Fatal("cancellation hid a joined protocol failure")
	}
}
