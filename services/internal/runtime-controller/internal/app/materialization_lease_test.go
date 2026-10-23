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

// materializationRenewClient records cloned synthetic renewal requests under a mutex and supports an injected response failure.
// Its embedded generated interface is not a live RPC connection.
// materializationRenewClient сохраняет копии синтетических запросов продления под мьютексом и поддерживает заданную ошибку ответа.
// Встроенный сгенерированный интерфейс не является живым RPC-соединением.
type materializationRenewClient struct {
	controlplanev1.RuntimeWorkServiceClient
	mu       sync.Mutex
	requests []*controlplanev1.RenewExecutionRequest
	renew    func(context.Context, *controlplanev1.RenewExecutionRequest) error
}

// RenewExecution records an independent request copy, optionally invokes the configured failure callback and otherwise returns a matching expiry one minute ahead.
// It performs no network call or persistent owner mutation.
// RenewExecution сохраняет независимую копию запроса, при наличии вызывает настроенный обратный вызов ошибки и иначе возвращает совпадающий срок на минуту вперёд.
// Сетевое обращение и устойчивое изменение у владельца не выполняются.
func (client *materializationRenewClient) RenewExecution(ctx context.Context, request *controlplanev1.RenewExecutionRequest, _ ...grpc.CallOption) (*controlplanev1.RenewExecutionResponse, error) {
	client.mu.Lock()
	client.requests = append(client.requests, proto.Clone(request).(*controlplanev1.RenewExecutionRequest))
	client.mu.Unlock()
	if client.renew != nil {
		if err := client.renew(ctx, request); err != nil {
			return nil, err
		}
	}
	return renewalTestResponse(request, time.Now().Add(time.Minute)), nil
}

// keeperTestRuntime builds the minimal synthetic runtime with a one-second request budget and five-millisecond renewal interval.
// It does not start the service or a Kubernetes workload.
// keeperTestRuntime создаёт минимальную синтетическую среду с секундным бюджетом запроса и периодом продления пять миллисекунд.
// Сервис и нагрузка Kubernetes не запускаются.
func keeperTestRuntime(client *materializationRenewClient) *runtime {
	return &runtime{control: client, config: Config{RequestTimeout: time.Second, LeaseRenewInterval: 5 * time.Millisecond}}
}

// awaitKeeperTest waits at most one second for a supplied synchronization channel and fails the test if it remains open.
// The helper bounds test coordination rather than changing any production lease duration.
// awaitKeeperTest ожидает переданный канал синхронизации не более секунды и проваливает тест, если канал остаётся открытым.
// Ограничивается синхронизация теста, а не производственная длительность аренды.
func awaitKeeperTest(t *testing.T, channel <-chan struct{}) {
	t.Helper()
	select {
	case <-channel:
	case <-time.After(time.Second):
		t.Fatal("lease keeper did not reach the expected phase")
	}
}

// TestMaterializationExpiredDeadlineCannotPublishPod gives the keeper an already elapsed execution deadline
// and requires both readiness and publication to fail without running the publication callback.
// TestMaterializationExpiredDeadlineCannotPublishPod задаёт держателю уже истёкший срок выполнения
// и требует отказа готовности и публикации без вызова действия публикации.
func TestMaterializationExpiredDeadlineCannotPublishPod(t *testing.T) {
	client := &materializationRenewClient{}
	runtime := keeperTestRuntime(client)
	input := runtimeTrackingInput()
	input.ExecutionDeadline = &runtimecontract.RuntimeExecutionDeadline{EffectiveDeadlineAt: time.Now().Add(-time.Second)}
	keeper := runtime.keepMaterializationLease(t.Context(), input)
	defer keeper.stop()
	if keeper.await() == nil {
		t.Fatal("expired materialization became ready")
	}
	called := false
	if keeper.publish(func(context.Context) error { called = true; return nil }) == nil || called {
		t.Fatal("expired deadline published Pod")
	}
}

// TestMaterializationKeepersRenewLaterLeaseWhileFirstIsDelayed models two short owner-side expiries while the first materialization is delayed.
// Both keepers must renew their exact pins independently, leaving the later publication live; production TTL settings are unchanged.
// TestMaterializationKeepersRenewLaterLeaseWhileFirstIsDelayed моделирует два коротких срока у владельца при задержке первого размещения.
// Оба держателя должны независимо продлевать точные привязки, сохраняя допуск последующей публикации; производственные настройки TTL не меняются.
func TestMaterializationKeepersRenewLaterLeaseWhileFirstIsDelayed(t *testing.T) {
	// Scaled model: owner TTL is 30ms and the first materialization takes 90ms.
	// No production TTL or configuration change is required for this test.
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

// TestMaterializationKeeperInitialDenialPreventsPublication injects an initial PermissionDenied error
// and requires the same error from readiness, publication and handoff with no publication effect.
// TestMaterializationKeeperInitialDenialPreventsPublication задаёт исходную ошибку PermissionDenied
// и требует ту же ошибку готовности, публикации и передачи без последствий публикации.
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

// TestMaterializationKeeperPeriodicDenialCancelsWaitingMaterialization changes a successful renewal stub to denial
// and checks periodic renewal cancels the keeper before any later publication can run.
// TestMaterializationKeeperPeriodicDenialCancelsWaitingMaterialization переключает успешный подставной клиент продления на отказ
// и проверяет отмену держателя периодическим продлением до возможной последующей публикации.
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

// TestMaterializationPublicationFreshRenewDenialPreventsEffect leaves initial renewal successful but denies the fresh publication renewal.
// The publication callback must not run merely because an earlier acknowledgement existed.
// TestMaterializationPublicationFreshRenewDenialPreventsEffect допускает первое продление, но отклоняет свежее продление перед публикацией.
// Действие публикации не должно выполняться только потому, что ранее уже было подтверждение.
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

// TestMaterializationKeeperHandoffJoinsBeforeFinalRenew checks one initial and one final renewal, a closed keeper completion channel
// and no overlapping renewal callbacks when tracking takes over.
// TestMaterializationKeeperHandoffJoinsBeforeFinalRenew проверяет одно исходное и одно последнее продление, закрытый канал завершения держателя
// и отсутствие пересекающихся обратных вызовов продления при передаче отслеживанию.
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

// TestMaterializationPublicationHasBoundedBudget waits inside the synthetic publication action until its short request deadline expires.
// The resulting DeadlineExceeded verifies the action receives a bounded context while holding the publication mutex.
// TestMaterializationPublicationHasBoundedBudget ожидает внутри синтетического действия публикации истечения короткого срока запроса.
// Полученный DeadlineExceeded подтверждает ограниченный контекст действия при удержании мьютекса публикации.
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

// TestMaterializationPublicationWaitsForInFlightDenial blocks periodic renewal until a concurrent publication is waiting, then returns denial.
// Serialization must expose that denial to publication without executing its action.
// TestMaterializationPublicationWaitsForInFlightDenial блокирует периодическое продление до ожидания конкурирующей публикации, затем возвращает отказ.
// Упорядочивание должно передать отказ публикации без выполнения её действия.
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

// TestMaterializationHandoffJoinsInFlightRenew makes handoff cancel and join a blocked periodic renewal before final renewal.
// Exactly three calls and at most one active callback demonstrate the old keeper does not compete with the caller.
// TestMaterializationHandoffJoinsInFlightRenew заставляет передачу отменить и дождаться заблокированного периодического продления перед последним продлением.
// Ровно три вызова и не более одного активного обратного вызова подтверждают отсутствие конкуренции старого держателя с потребителем.
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

// TestMaterializationKeeperShutdownCancelsAndJoinsInFlightRPC cancels the parent while synthetic renewal is blocked.
// Stop must join within the test budget with zero active callbacks and the original parent cancellation cause.
// TestMaterializationKeeperShutdownCancelsAndJoinsInFlightRPC отменяет родителя во время блокировки синтетического продления.
// Остановка должна завершить ожидание в бюджете теста без активных обратных вызовов и с исходной причиной отмены родителя.
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

// TestMaterializationKeeperInvalidTupleDoesNotCallOwner supplies missing or incomplete lease pins
// and requires readiness failure before the synthetic owner sees any request.
// TestMaterializationKeeperInvalidTupleDoesNotCallOwner передаёт отсутствующие или неполные привязки аренды
// и требует отказа готовности до получения синтетическим владельцем любого запроса.
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

// TestMaterializationHandoffDoesNotHideConcurrentOwnerDenial returns PermissionDenied from a renewal interrupted by handoff.
// The denial must survive the earlier local stop cause, prevent final retry and be returned to the caller.
// TestMaterializationHandoffDoesNotHideConcurrentOwnerDenial возвращает PermissionDenied из продления, прерванного передачей.
// Отказ должен пережить более раннюю локальную причину остановки, исключить последнее повторное обращение и вернуться потребителю.
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
