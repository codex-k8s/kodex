package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/integration"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// kubernetesUnusedBroker satisfies construction only; activity tests must never call an unconfigured broker method.
// kubernetesUnusedBroker нужен только для создания consumer; тесты операций не вызывают ненастроенные методы broker.
type kubernetesUnusedBroker struct {
	sb.ManagedKubernetesServiceClient
}

// kubernetesConsumerClaim supplies a synthetic original lease, purpose and mutable input to test the actual admission copy boundary.
// kubernetesConsumerClaim задаёт synthetic original lease, purpose и изменяемый input для проверки фактической границы копирования при приёме.
func kubernetesConsumerClaim() *cp.IntegrationInvocationClaim {
	return &cp.IntegrationInvocationClaim{InvocationRef: "inv_fixture", InputDigest: strings.Repeat("a", 64),
		Lease:                  &cp.WorkLease{Ref: "lease_fixture", Fence: "fence_fixture", Generation: 7, ExpiresAt: timestamppb.New(time.Now().Add(time.Minute))},
		WorkMode:               cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_EXECUTE,
		CredentialDeliveryKind: cp.IntegrationCredentialDeliveryKind_INTEGRATION_CREDENTIAL_DELIVERY_KIND_BROKER_IDENTITY,
		BrokerIdentityPurpose:  cp.IntegrationBrokerIdentityPurpose_INTEGRATION_BROKER_IDENTITY_PURPOSE_KUBERNETES_TARGET,
		BrokerIdentityRef:      "identity_fixture", BrokerIdentityRevision: 3,
		BoundedInput: &structpb.Struct{Fields: map[string]*structpb.Value{"namespace": structpb.NewStringValue("fixture")}},
	}
}

// newKubernetesConsumerFixture registers bounded cleanup and uses a real ActivityGate around the supplied joined runner.
// newKubernetesConsumerFixture регистрирует ограниченную очистку и использует настоящий ActivityGate вокруг переданного joined runner.
func newKubernetesConsumerFixture(t *testing.T, capacity int, run kubernetesActivityRunner) *kubernetesConsumer {
	t.Helper()
	consumer, err := newKubernetesConsumer(t.Context(), "gateway_fixture", capacity, &integration.Adapter{}, &kubernetesUnusedBroker{}, run)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := consumer.Close(); err != nil {
			t.Error(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = consumer.Wait(ctx)
		if !consumer.Joined() {
			t.Error("consumer cleanup did not join")
		}
	})
	return consumer
}

// TestKubernetesConsumerHandoffPreservesActivity proves caller cancellation and ReleaseUnused cannot free adopted capacity or mutate copied owner pins.
// TestKubernetesConsumerHandoffPreservesActivity доказывает, что caller cancellation и ReleaseUnused не освобождают принятую ёмкость и не меняют копию owner pins.
func TestKubernetesConsumerHandoffPreservesActivity(t *testing.T) {
	entered := make(chan kubernetesActivityInput, 1)
	finish := make(chan struct{})
	finished := make(chan struct{})
	consumer := newKubernetesConsumerFixture(t, 1, func(ctx context.Context, input kubernetesActivityInput) error {
		entered <- input
		select {
		case <-finish:
			close(finished)
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	slot, ok := consumer.ReserveClaim()
	if !ok {
		t.Fatal("reservation rejected")
	}
	claim := kubernetesConsumerClaim()
	handoff, cancel := context.WithCancel(t.Context())
	progress, err := slot.ExecuteClaim(handoff, claim)
	if err != nil || progress.State != "RUNNING" || progress.RemoteOutcome != "NOT_STARTED" || progress.Attempted || progress.Generation != 7 {
		t.Fatal("handoff was rejected or reported an external effect")
	}
	input := <-entered
	cancel()
	slot.ReleaseUnused()
	if _, ok := consumer.ReserveClaim(); ok {
		t.Fatal("adopted slot was released by caller")
	}
	claim.BoundedInput.Fields["namespace"] = structpb.NewStringValue("foreign")
	claim.Lease.Fence = "foreign"
	if input.request.Input["namespace"] != "fixture" || input.binding.Fence != "fence_fixture" || input.request.EmailExecution != nil {
		t.Fatal("admitted input aliases caller state or another credential lane")
	}
	if err := consumer.Wait(t.Context()); !errors.Is(err, serviceruntime.ErrActivityGateLifecycle) {
		t.Fatal("open admission reported join")
	}
	close(finish)
	<-finished
	if err := consumer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := consumer.Wait(t.Context()); err != nil || !consumer.Joined() {
		t.Fatal("completed activity did not join")
	}
}

// TestKubernetesConsumerCloseWaitsForCleanup distinguishes cancellation from actual local join and releases unused reservations without releasing adopted work.
// TestKubernetesConsumerCloseWaitsForCleanup различает отмену и фактический локальный join, освобождая неиспользованные резервации без освобождения принятой работы.
func TestKubernetesConsumerCloseWaitsForCleanup(t *testing.T) {
	canceled := make(chan struct{})
	cleanup := make(chan struct{})
	consumer := newKubernetesConsumerFixture(t, 2, func(ctx context.Context, _ kubernetesActivityInput) error {
		<-ctx.Done()
		close(canceled)
		<-cleanup
		return nil
	})
	defer close(cleanup)
	slot, _ := consumer.ReserveClaim()
	unused, _ := consumer.ReserveClaim()
	if _, err := slot.ExecuteClaim(t.Context(), kubernetesConsumerClaim()); err != nil {
		t.Fatal(err)
	}
	if err := consumer.Close(); err != nil {
		t.Fatal(err)
	}
	<-canceled
	unused.ReleaseUnused()
	slot.ReleaseUnused()
	if _, err := unused.ExecuteClaim(t.Context(), kubernetesConsumerClaim()); err == nil {
		t.Fatal("closed reservation adopted work")
	}
	observation, cancel := context.WithCancel(t.Context())
	cancel()
	if err := consumer.Wait(observation); !errors.Is(err, context.Canceled) || consumer.Joined() {
		t.Fatal("cancellation was mistaken for join")
	}
	if _, ok := consumer.ReserveClaim(); ok {
		t.Fatal("closed consumer admitted work")
	}
}

// TestKubernetesConsumerAdoptsExactlyOnce races handoff and duplicate ReleaseUnused calls against one reserved slot without starting duplicate work.
// TestKubernetesConsumerAdoptsExactlyOnce проверяет конкурентную передачу и повторные ReleaseUnused для одного slot без повторного запуска работы.
func TestKubernetesConsumerAdoptsExactlyOnce(t *testing.T) {
	var calls atomic.Int32
	consumer := newKubernetesConsumerFixture(t, 1, func(ctx context.Context, _ kubernetesActivityInput) error {
		calls.Add(1)
		<-ctx.Done()
		return nil
	})
	slot, _ := consumer.ReserveClaim()
	var accepted atomic.Int32
	var group sync.WaitGroup
	for range 24 {
		group.Go(func() {
			if _, err := slot.ExecuteClaim(t.Context(), kubernetesConsumerClaim()); err == nil {
				accepted.Add(1)
			}
		})
	}
	group.Wait()
	for range 24 {
		group.Go(slot.ReleaseUnused)
	}
	group.Wait()
	if accepted.Load() != 1 {
		t.Fatal("reservation did not have exactly one adopter")
	}
	if _, ok := consumer.ReserveClaim(); ok {
		t.Fatal("duplicate handoff released adopted slot")
	}
	if err := consumer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := consumer.Wait(t.Context()); err != nil || calls.Load() != 1 {
		t.Fatal("duplicate runner or missing join")
	}
}

// TestKubernetesConsumerRejectsBadClaimAndRetainsFailure verifies failed admission returns capacity and joined runner errors stay visible without leaking their cause.
// TestKubernetesConsumerRejectsBadClaimAndRetainsFailure проверяет возврат ёмкости при отказе и сохранение ошибки завершившегося runner без раскрытия причины.
func TestKubernetesConsumerRejectsBadClaimAndRetainsFailure(t *testing.T) {
	consumer := newKubernetesConsumerFixture(t, 1, func(context.Context, kubernetesActivityInput) error { return errors.New("private fixture failure") })
	slot, _ := consumer.ReserveClaim()
	claim := kubernetesConsumerClaim()
	claim.BrokerIdentityPurpose = cp.IntegrationBrokerIdentityPurpose_INTEGRATION_BROKER_IDENTITY_PURPOSE_SSH_TARGET
	if _, err := slot.ExecuteClaim(t.Context(), claim); err == nil {
		t.Fatal("foreign purpose admitted")
	}
	slot.ReleaseUnused()
	slot, ok := consumer.ReserveClaim()
	if !ok {
		t.Fatal("rejected claim leaked capacity")
	}
	if _, err := slot.ExecuteClaim(t.Context(), kubernetesConsumerClaim()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-consumer.Failure():
	case <-time.After(5 * time.Second):
		t.Fatal("activity failure was not signaled before shutdown")
	}
	if _, ok := consumer.ReserveClaim(); ok {
		t.Fatal("failed consumer continued admitting claims")
	}
	if err := consumer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := consumer.Wait(t.Context()); !errors.Is(err, errKubernetesActivity) || !consumer.Joined() || strings.Contains(err.Error(), "private") {
		t.Fatal("joined error lost or disclosed its cause")
	}
}

// TestKubernetesConsumerRequiresFiniteCapacity rejects missing execution dependencies and unsupported capacity before creating an activity context.
// TestKubernetesConsumerRequiresFiniteCapacity отвергает отсутствующие зависимости и неподдержанную ёмкость до создания контекста операции.
func TestKubernetesConsumerRequiresFiniteCapacity(t *testing.T) {
	run := func(context.Context, kubernetesActivityInput) error { return nil }
	for _, capacity := range []int{-1, 0, 5, 128} {
		if consumer, err := newKubernetesConsumer(t.Context(), "gateway_fixture", capacity, &integration.Adapter{}, &kubernetesUnusedBroker{}, run); err == nil || consumer != nil {
			t.Fatal("invalid capacity admitted")
		}
	}
	if consumer, err := newKubernetesConsumer(t.Context(), "gateway_fixture", 1, &integration.Adapter{}, &kubernetesUnusedBroker{}, nil); err == nil || consumer != nil {
		t.Fatal("missing runner admitted")
	}
}

// TestKubernetesConsumerFailureRejectsReservedHandoff closes admission for a slot issued before another activity failed, without starting another runner.
// TestKubernetesConsumerFailureRejectsReservedHandoff закрывает приём для slot, выданного до ошибки другой операции, без запуска нового runner.
func TestKubernetesConsumerFailureRejectsReservedHandoff(t *testing.T) {
	var calls atomic.Int32
	consumer := newKubernetesConsumerFixture(t, 2, func(context.Context, kubernetesActivityInput) error {
		calls.Add(1)
		return errors.New("synthetic unresolved lifecycle")
	})
	active, ok := consumer.ReserveClaim()
	if !ok {
		t.Fatal("active reservation rejected")
	}
	unused, ok := consumer.ReserveClaim()
	if !ok {
		t.Fatal("unused reservation rejected")
	}
	if _, err := active.ExecuteClaim(t.Context(), kubernetesConsumerClaim()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-consumer.Failure():
	case <-time.After(5 * time.Second):
		t.Fatal("activity failure was not signaled")
	}
	if _, err := unused.ExecuteClaim(t.Context(), kubernetesConsumerClaim()); err == nil {
		t.Fatal("old reservation bypassed failed admission")
	}
	unused.ReleaseUnused()
	if err := consumer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := consumer.Wait(t.Context()); !errors.Is(err, errKubernetesActivity) || !consumer.Joined() || calls.Load() != 1 {
		t.Fatal("failed admission started work or lost joined failure")
	}
}
