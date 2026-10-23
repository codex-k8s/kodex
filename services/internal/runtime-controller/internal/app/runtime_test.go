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
	"google.golang.org/protobuf/types/known/timestamppb"
)

// runtimeWorkClientStub records deep-copied completion attempts under a mutex and allows synthetic renewal/completion failures.
// Its successful responses do not represent network delivery or durable owner commits.
// runtimeWorkClientStub сохраняет глубокие копии попыток завершения под мьютексом и позволяет задавать синтетические ошибки продления/завершения.
// Успешные ответы не означают сетевую доставку или устойчивую фиксацию владельцем.
type runtimeWorkClientStub struct {
	controlplanev1.RuntimeWorkServiceClient
	mu       sync.Mutex
	requests []*controlplanev1.CompleteExecutionRequest
	complete func(int) error
	renew    func() error
}

// claimBatchClient records the requested batch limit and returns no executions, isolating claim-size tests from materialization.
// claimBatchClient сохраняет запрошенный размер пакета и возвращает пустой список выполнений, отделяя проверку размера заявки от размещения.
type claimBatchClient struct {
	controlplanev1.RuntimeWorkServiceClient
	limit int32
}

// ClaimExecution captures only the limit and returns an empty synthetic response without changing capacity or issuing authority.
// ClaimExecution сохраняет только лимит и возвращает пустой синтетический ответ без изменения ёмкости или выдачи полномочий.
func (client *claimBatchClient) ClaimExecution(_ context.Context, request *controlplanev1.ClaimExecutionRequest, _ ...grpc.CallOption) (*controlplanev1.ClaimExecutionResponse, error) {
	client.limit = request.Limit
	return &controlplanev1.ClaimExecutionResponse{}, nil
}

// TestClaimSeparatesCatalogTransactionsWithoutSerializingTrackers checks the observed request limit across five capacity occupancies.
// Each nonfull case requests one execution while the existing 16-slot capacity and occupied entries remain unchanged; no trackers are launched.
// TestClaimSeparatesCatalogTransactionsWithoutSerializingTrackers проверяет наблюдаемый лимит запроса при пяти вариантах занятой ёмкости.
// При неполной ёмкости запрашивается одно выполнение, а имеющиеся 16 мест и занятые элементы сохраняются; отслеживающие работники не запускаются.
func TestClaimSeparatesCatalogTransactionsWithoutSerializingTrackers(t *testing.T) {
	for _, occupied := range []int{0, 1, 9, 15, 16} {
		client := &claimBatchClient{}
		runtime := &runtime{control: client, capacity: make(chan struct{}, 16), config: Config{RequestTimeout: time.Second}}
		for range occupied {
			runtime.capacity <- struct{}{}
		}
		if _, err := runtime.claim(t.Context()); err != nil {
			t.Fatal(err)
		}
		want := int32(1)
		if occupied == 16 {
			want = 0
		}
		if client.limit != want || len(runtime.capacity) != occupied || cap(runtime.capacity) != 16 {
			t.Fatal("claim batch must be bounded independently of parallel tracker capacity")
		}
	}
}

// RenewExecution applies an optional synthetic failure before returning a lease with the exact request pins and an expiry one minute ahead.
// It neither records a real owner lease nor performs a gRPC call.
// RenewExecution применяет необязательную синтетическую ошибку перед возвратом аренды с точными привязками запроса и сроком на минуту вперёд.
// Настоящая аренда владельца не сохраняется, вызов gRPC не выполняется.
func (client *runtimeWorkClientStub) RenewExecution(_ context.Context, request *controlplanev1.RenewExecutionRequest, _ ...grpc.CallOption) (*controlplanev1.RenewExecutionResponse, error) {
	if client.renew != nil {
		if err := client.renew(); err != nil {
			return nil, err
		}
	}
	return renewalTestResponse(request, time.Now().Add(time.Minute)), nil
}

// renewalTestResponse copies lease reference, fence and generation into a synthetic response with the supplied protobuf expiry.
// It is a mapping fixture, not a signed authorization receipt.
// renewalTestResponse копирует ссылку аренды, ограждение и поколение в синтетический ответ с переданным сроком protobuf.
// Это образец преобразования, а не подписанная квитанция полномочий.
func renewalTestResponse(request *controlplanev1.RenewExecutionRequest, expiry time.Time) *controlplanev1.RenewExecutionResponse {
	return &controlplanev1.RenewExecutionResponse{Lease: &controlplanev1.WorkLease{Ref: request.LeaseRef, Fence: request.Fence,
		Generation: request.Generation, ExpiresAt: timestamppb.New(expiry)}}
}

// ReportExecutionProgress returns an empty successful synthetic acknowledgement without persisting or inspecting progress.
// ReportExecutionProgress возвращает пустое успешное синтетическое подтверждение без сохранения или проверки прогресса.
func (client *runtimeWorkClientStub) ReportExecutionProgress(context.Context, *controlplanev1.ReportExecutionProgressRequest, ...grpc.CallOption) (*controlplanev1.ReportExecutionProgressResponse, error) {
	return &controlplanev1.ReportExecutionProgressResponse{}, nil
}

// CompleteExecution stores an independent protobuf copy before applying the failure callback with the one-based attempt number.
// Failed calls remain in the request count; a successful empty response does not mutate a real owner execution.
// CompleteExecution сохраняет независимую копию protobuf до вызова функции ошибки с номером попытки, начиная с единицы.
// Неуспешные вызовы остаются в счётчике запросов; пустой успешный ответ не меняет настоящее выполнение владельца.
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

// count returns the number of recorded completion attempts while holding the request mutex, including failed deliveries.
// count возвращает число записанных попыток завершения под мьютексом запросов, включая неуспешные доставки.
func (client *runtimeWorkClientStub) count() int {
	client.mu.Lock()
	defer client.mu.Unlock()
	return len(client.requests)
}

// turnLifecycleStub supplies one configured Pod observation and atomic stop/delete counters for local tracker tests.
// Optional channels signal observation or stop; no Kubernetes object is read or changed.
// turnLifecycleStub задаёт одно настроенное наблюдение Pod и атомарные счётчики остановки/удаления для локальных тестов отслеживания.
// Необязательные каналы сигнализируют наблюдение или остановку; объекты Kubernetes не читаются и не меняются.
type turnLifecycleStub struct {
	observation workload.TurnPodObservation
	observed    chan struct{}
	once        sync.Once
	deletions   atomic.Int32
	stops       atomic.Int32
	stopped     chan struct{}
}

// StopTurn increments the synthetic stop count and closes the optional stopped channel before succeeding.
// A configured channel assumes one stop call; this stub does not implement an idempotent real workload stop.
// StopTurn увеличивает синтетический счётчик остановок и перед успехом закрывает необязательный канал stopped.
// Настроенный канал предполагает один вызов остановки; идемпотентная остановка настоящей нагрузки не реализована.
func (turns *turnLifecycleStub) StopTurn(context.Context, string) error {
	turns.stops.Add(1)
	if turns.stopped != nil {
		close(turns.stopped)
	}
	return nil
}

// TestTrackShutdownWaitsForReceiptWithoutDeletingCallbackMaterial cancels tracking, waits for the stop signal and then completes the local callback.
// Before and after that callback, no fallback completion or deletion may replace the successful result, and the tracker must join.
// TestTrackShutdownWaitsForReceiptWithoutDeletingCallbackMaterial отменяет отслеживание, ждёт сигнал остановки и затем завершает локальный обратный вызов.
// До и после него запасное завершение или удаление не должны подменять успешный результат, а отслеживающий работник должен завершиться.
func TestTrackShutdownWaitsForReceiptWithoutDeletingCallbackMaterial(t *testing.T) {
	input := runtimeTrackingInput()
	coordinator := callback.NewCoordinator()
	done := coordinator.Register(input)
	client := &runtimeWorkClientStub{}
	turns := &turnLifecycleStub{stopped: make(chan struct{})}
	runtime := trackingRuntime(client, turns, coordinator)
	runtime.terminalGrace = time.Second
	ctx, cancel := context.WithCancel(t.Context())
	finished := make(chan struct{})
	go func() { runtime.track(ctx, input, done, false, time.Time{}); close(finished) }()
	cancel()
	<-turns.stopped
	if turns.deletions.Load() != 0 || client.count() != 0 {
		t.Fatal("shutdown revoked execution before receipt")
	}
	coordinator.Complete(input.LeaseRef)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("tracker did not join after receipt")
	}
	if turns.deletions.Load() != 0 || client.count() != 0 {
		t.Fatal("successful receipt was replaced with fallback")
	}
}

// TestTrackDrainHonorsOwnerRenewalDenial injects PermissionDenied during the canceled tracker's drain renewal.
// The local cleanup counter must advance without creating a synthetic owner completion.
// TestTrackDrainHonorsOwnerRenewalDenial задаёт PermissionDenied при продлении во время завершения отменённого отслеживания.
// Локальный счётчик очистки должен увеличиться без создания синтетического завершения владельца.
func TestTrackDrainHonorsOwnerRenewalDenial(t *testing.T) {
	input := runtimeTrackingInput()
	coordinator := callback.NewCoordinator()
	done := coordinator.Register(input)
	client := &runtimeWorkClientStub{renew: func() error { return status.Error(codes.PermissionDenied, "execution revoked") }}
	turns := &turnLifecycleStub{}
	runtime := trackingRuntime(client, turns, coordinator)
	runtime.terminalGrace = time.Second
	runtime.config.LeaseRenewInterval = time.Millisecond
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	runtime.track(ctx, input, done, false, time.Time{})
	if turns.deletions.Load() != 1 || client.count() != 0 {
		t.Fatal("revoked lease was extended or completed with fabricated result")
	}
}

// TestDrainDoesNotStopAlreadyCompletedWarmTurn closes the local callback before drain begins.
// No stop, delete or failure-completion attempt may be introduced for that already completed turn.
// TestDrainDoesNotStopAlreadyCompletedWarmTurn закрывает локальный обратный вызов до начала завершения работы.
// Для уже завершённого хода не должны добавляться остановка, удаление или попытка завершения с ошибкой.
func TestDrainDoesNotStopAlreadyCompletedWarmTurn(t *testing.T) {
	input := runtimeTrackingInput()
	coordinator := callback.NewCoordinator()
	done := coordinator.Register(input)
	coordinator.Complete(input.LeaseRef)
	turns := &turnLifecycleStub{}
	client := &runtimeWorkClientStub{}
	runtime := trackingRuntime(client, turns, coordinator)
	runtime.drainTurn(t.Context(), input, done, time.Time{})
	if turns.stops.Load() != 0 || turns.deletions.Load() != 0 || client.count() != 0 {
		t.Fatal("completed warm turn was stopped by shutdown fallback")
	}
}

// ObserveTurnPod closes the optional first-observation signal once and returns the fixed observation unchanged.
// It does not validate execution identity or call a Kubernetes API.
// ObserveTurnPod один раз закрывает необязательный сигнал первого наблюдения и возвращает заданное наблюдение без изменения.
// Идентичность выполнения не проверяется, API Kubernetes не вызывается.
func (turns *turnLifecycleStub) ObserveTurnPod(context.Context, runtimecontract.RunnerInput, bool) (workload.TurnPodObservation, error) {
	if turns.observed != nil {
		turns.once.Do(func() { close(turns.observed) })
	}
	return turns.observation, nil
}

// DeleteTurn increments the local deletion counter and succeeds without deleting files, Pods, Secrets or volumes.
// DeleteTurn увеличивает локальный счётчик удалений и сообщает успех без удаления файлов, Pod, Secret или томов.
func (turns *turnLifecycleStub) DeleteTurn(context.Context, string) error {
	turns.deletions.Add(1)
	return nil
}

// TestTrackAcceptsCallbackDuringTerminalGrace supplies a terminal Pod observation, then completes the callback on its first inspection signal.
// The tracker must release capacity without fallback completion or cleanup during the configured grace period.
// TestTrackAcceptsCallbackDuringTerminalGrace задаёт завершающее наблюдение Pod и завершает обратный вызов по сигналу первой проверки.
// Отслеживание должно освободить ёмкость без запасного завершения или очистки в настроенном периоде ожидания.
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
	runtime.track(t.Context(), input, done, false, time.Time{})

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

// TestTrackRetriesFailureCompletionBeforeCleanup makes the first two completion deliveries unavailable and accepts the third.
// It checks three attempts, one cleanup and the unchanged idempotency key and lease/fence/generation tuple across retries.
// TestTrackRetriesFailureCompletionBeforeCleanup задаёт недоступность первых двух доставок завершения и принимает третью.
// Проверяются три попытки, одна очистка и неизменные ключ идемпотентности и набор аренды/ограждения/поколения при повторах.
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

	runtime.track(t.Context(), input, done, false, time.Time{})

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

// TestFailureOwnerDenialWithoutDeadlineClosesRevokedPod removes the execution deadline and rejects completion with PermissionDenied.
// One synthetic request must lead to one stop/delete pair and local callback completion without retrying the denied operation.
// TestFailureOwnerDenialWithoutDeadlineClosesRevokedPod убирает срок выполнения и отклоняет завершение с PermissionDenied.
// Один синтетический запрос должен привести к одной паре остановки/удаления и завершению локального обратного вызова без повтора запрещённой операции.
func TestFailureOwnerDenialWithoutDeadlineClosesRevokedPod(t *testing.T) {
	input := runtimeTrackingInput()
	input.ExecutionDeadline = nil
	coordinator := callback.NewCoordinator()
	done := coordinator.Register(input)
	client := &runtimeWorkClientStub{complete: func(int) error { return status.Error(codes.PermissionDenied, "lease revoked") }}
	turns := &turnLifecycleStub{}
	runtime := trackingRuntime(client, turns, coordinator)
	runtime.completeFailure(t.Context(), input, "RUNTIME_WORKLOAD_EXITED", "ROLE_RUNTIME_EXITED_NONZERO")
	if client.count() != 1 || turns.deletions.Load() != 1 || turns.stops.Load() != 1 {
		t.Fatal("revoked non-deadline execution retained runtime or retried authority")
	}
	select {
	case <-done:
	default:
		t.Fatal("revoked execution tracker remained active")
	}
}

// TestTrackMemoryFailurePreservesOwnerCompletionAndCleanup injects a memory-failure observation rather than causing a real OOM.
// It checks one completion/deletion, the exact lease tuple and the existing public safe code versus memory-specific summary.
// TestTrackMemoryFailurePreservesOwnerCompletionAndCleanup задаёт наблюдение ошибки памяти, а не вызывает настоящий OOM.
// Проверяются одно завершение/удаление, точный набор аренды и существующий публичный безопасный код отдельно от специального сообщения о памяти.
func TestTrackMemoryFailurePreservesOwnerCompletionAndCleanup(t *testing.T) {
	input := runtimeTrackingInput()
	coordinator := callback.NewCoordinator()
	done := coordinator.Register(input)
	client := &runtimeWorkClientStub{}
	turns := &turnLifecycleStub{observation: workload.TurnPodObservation{State: "FAILED", DiagnosticCode: workload.TurnPodMemoryDiagnostic}}
	runtime := trackingRuntime(client, turns, coordinator)
	runtime.track(t.Context(), input, done, false, time.Time{})
	if client.count() != 1 || turns.deletions.Load() != 1 {
		t.Fatal("memory failure changed completion cardinality or cleanup")
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	request := client.requests[0]
	if request.GetSuccess() || request.GetSafeErrorCode() != "RUNTIME_UNAVAILABLE" || request.GetResultSummary() != "i18n:RUNTIME_MEMORY_EXHAUSTED" || request.GetLeaseRef() != input.LeaseRef || request.GetFence() != input.LeaseFence || request.GetGeneration() != input.LeaseGeneration {
		t.Fatal("memory failure lost its exact owner tuple or safe summary")
	}
}

// TestCompleteFailureDistinguishesInvalidInputFromUnsupportedProfile checks safe-code and summary mapping for invalid input, stale profile and a synthetic unknown diagnostic.
// Every case preserves the exact failure key and lease pins and reaches local cleanup; it does not claim that arbitrary summary text is redacted.
// TestCompleteFailureDistinguishesInvalidInputFromUnsupportedProfile проверяет безопасные коды и сообщения для неверного входа, устаревшего профиля и синтетической неизвестной диагностики.
// Каждый случай сохраняет точный ключ ошибки и привязки аренды и доходит до локальной очистки; очистка произвольного текста сообщения этим не заявляется.
func TestCompleteFailureDistinguishesInvalidInputFromUnsupportedProfile(t *testing.T) {
	tests := []struct {
		code, safeCode, summary string
	}{
		{"RUNTIME_REVISION_INVALID", "RUNTIME_INPUT_INVALID", "i18n:RUNTIME_INPUT_INVALID"},
		{"RUNTIME_CONFIGURATION_STALE", "RUNTIME_PROFILE_UNSUPPORTED", "i18n:RUNTIME_CONFIGURATION_STALE"},
		{"UNKNOWN_PRIVATE_DIAGNOSTIC", "RUNTIME_UNAVAILABLE", "i18n:UNKNOWN_PRIVATE_DIAGNOSTIC"},
	}
	for _, test := range tests {
		t.Run(test.code, func(t *testing.T) {
			input := runtimeTrackingInput()
			coordinator := callback.NewCoordinator()
			done := coordinator.Register(input)
			client := &runtimeWorkClientStub{}
			turns := &turnLifecycleStub{}
			runtime := trackingRuntime(client, turns, coordinator)
			runtime.completeFailure(t.Context(), input, test.code, "")
			if len(client.requests) != 1 {
				t.Fatal("failure completion cardinality changed")
			}
			request := client.requests[0]
			if request.SafeErrorCode != test.safeCode || request.ResultSummary != test.summary || request.Success {
				t.Fatal("failure completion has an incorrect public classification")
			}
			if request.GetLeaseRef() != input.LeaseRef || request.GetFence() != input.LeaseFence || request.GetGeneration() != input.LeaseGeneration || request.GetMutation().GetIdempotencyKey() != stableIdempotency(input.LeaseRef, "failure:"+test.code) {
				t.Fatal("failure completion changed its immutable binding")
			}
			if turns.deletions.Load() != 1 {
				t.Fatal("failure cleanup cardinality changed")
			}
			select {
			case <-done:
			default:
				t.Fatal("coordinator remains active after durable failure completion")
			}
		})
	}
}

// TestCompleteFailureKeepsRuntimeUntilDurableCommit returns an unresolved unavailable response with retry delays disabled.
// The test requires one attempted delivery, no deletion and an open coordinator channel; no real database commit is exercised.
// TestCompleteFailureKeepsRuntimeUntilDurableCommit возвращает неразрешённую ошибку недоступности при отключённых задержках повторов.
// Требуются одна попытка доставки, отсутствие удаления и открытый канал координатора; настоящая фиксация БД не проверяется.
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

// TestWarmFileProjectionEligibilityFailsClosed accepts absent turn files but rejects either an attachment set or an input artifact.
// This exercises only the local file-projection eligibility predicate, not full runner-input validation or authority.
// TestWarmFileProjectionEligibilityFailsClosed принимает отсутствие файлов хода, но отклоняет наличие набора вложений или входного артефакта.
// Проверяется только локальная допустимость проекции файлов, а не полная проверка входа исполнителя или полномочий.
func TestWarmFileProjectionEligibilityFailsClosed(t *testing.T) {
	if !warmFileProjectionEligible(runtimecontract.RunnerInput{}) {
		t.Fatal("empty immutable file projection was rejected")
	}
	for name, input := range map[string]runtimecontract.RunnerInput{
		"attachment set": {AttachmentSets: []runtimecontract.RunnerAttachmentSet{{Ref: "aset_abcdefgh"}}},
		"artifact":       {InputArtifacts: []runtimecontract.RunnerInputArtifact{{Ref: "artifact_abcdefgh"}}},
	} {
		t.Run(name, func(t *testing.T) {
			if warmFileProjectionEligible(input) {
				t.Fatal("warm runtime accepted a turn-specific file projection")
			}
		})
	}
}

// trackingRuntime creates a synthetic tracker with one already occupied capacity slot, discard logging and short inspection/grace/retry delays.
// Owner and workload behavior come from the supplied stubs; no service or runner is started by construction.
// trackingRuntime создаёт синтетическое отслеживание с одним уже занятым местом ёмкости, отбрасываемым журналом и короткими задержками наблюдения/ожидания/повторов.
// Поведение владельца и нагрузки задаётся переданными заглушками; создание не запускает сервис или исполнителя.
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

// runtimeTrackingInput supplies only synthetic lease reference, fence and generation for local lifecycle tests.
// It is not a complete validated runner input or an issued owner grant.
// runtimeTrackingInput задаёт только синтетические ссылку аренды, ограждение и поколение для локальных тестов жизненного цикла.
// Это не полный проверенный вход исполнителя и не выданное владельцем разрешение.
func runtimeTrackingInput() runtimecontract.RunnerInput {
	return runtimecontract.RunnerInput{
		LeaseRef:        "lease_runtime_tracking_abcdefgh",
		LeaseFence:      "fence_runtime_tracking_abcdefgh",
		LeaseGeneration: 3,
	}
}

// TestTrackPinnedWorkflowDeadlineDoesNotUseShorterFallback gives the fixture a far pinned deadline and a much shorter configured fallback.
// After a short wait, local callback completion must join the tracker without a stop or synthetic failure request.
// TestTrackPinnedWorkflowDeadlineDoesNotUseShorterFallback задаёт образцу далёкий закреплённый срок и гораздо более короткий настроенный запасной срок.
// После короткого ожидания локальный обратный вызов должен завершить отслеживание без остановки или синтетического запроса ошибки.
func TestTrackPinnedWorkflowDeadlineDoesNotUseShorterFallback(t *testing.T) {
	input := runtimeTrackingInput()
	input.ExecutionDeadline = &runtimecontract.RuntimeExecutionDeadline{EffectiveDeadlineAt: time.Now().Add(time.Hour)}
	coordinator := callback.NewCoordinator()
	done := coordinator.Register(input)
	client := &runtimeWorkClientStub{}
	turns := &turnLifecycleStub{}
	runtime := trackingRuntime(client, turns, coordinator)
	runtime.config.ExecutionTimeout = time.Millisecond
	finished := make(chan struct{})
	go func() { runtime.track(t.Context(), input, done, false, time.Time{}); close(finished) }()
	time.Sleep(20 * time.Millisecond)
	coordinator.Complete(input.LeaseRef)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("tracker did not join")
	}
	if turns.stops.Load() != 0 || client.count() != 0 {
		t.Fatal("fallback truncated pinned workflow deadline")
	}
}

// TestTrackExpiredWorkflowDeadlineClosesWithTimeoutAndJoins supplies an already elapsed execution deadline.
// The local path must stop/delete once and send one failure request preserving the explicit runtime-timeout safe code.
// TestTrackExpiredWorkflowDeadlineClosesWithTimeoutAndJoins задаёт уже истёкший срок выполнения.
// Локальный путь должен один раз остановить/удалить нагрузку и отправить один запрос ошибки с сохранением явного безопасного кода таймаута выполнения.
func TestTrackExpiredWorkflowDeadlineClosesWithTimeoutAndJoins(t *testing.T) {
	input := runtimeTrackingInput()
	input.ExecutionDeadline = &runtimecontract.RuntimeExecutionDeadline{EffectiveDeadlineAt: time.Now().Add(-time.Second)}
	coordinator := callback.NewCoordinator()
	done := coordinator.Register(input)
	client := &runtimeWorkClientStub{}
	turns := &turnLifecycleStub{}
	runtime := trackingRuntime(client, turns, coordinator)
	runtime.track(t.Context(), input, done, false, time.Time{})
	client.mu.Lock()
	defer client.mu.Unlock()
	if turns.stops.Load() != 1 || turns.deletions.Load() != 1 || len(client.requests) != 1 || client.requests[0].SafeErrorCode != "RUNTIME_TIMEOUT" {
		t.Fatal("expired workflow did not cancel/join through durable timeout completion")
	}
}

// TestWorkflowDeadlineOwnerDenialClosesRevokedPodWithoutRetry combines an expired deadline with a denied completion response.
// It checks one request, the existing two-stop/one-delete cleanup and local callback closure without retrying authority.
// TestWorkflowDeadlineOwnerDenialClosesRevokedPodWithoutRetry сочетает истёкший срок с отказом в ответе завершения.
// Проверяются один запрос, существующая очистка с двумя остановками и одним удалением и закрытие локального обратного вызова без повтора полномочий.
func TestWorkflowDeadlineOwnerDenialClosesRevokedPodWithoutRetry(t *testing.T) {
	input := runtimeTrackingInput()
	input.ExecutionDeadline = &runtimecontract.RuntimeExecutionDeadline{EffectiveDeadlineAt: time.Now().UTC().Add(-time.Second)}
	coordinator := callback.NewCoordinator()
	done := coordinator.Register(input)
	client := &runtimeWorkClientStub{complete: func(int) error { return status.Error(codes.PermissionDenied, "lease revoked") }}
	turns := &turnLifecycleStub{}
	runtime := trackingRuntime(client, turns, coordinator)
	runtime.track(t.Context(), input, done, false, time.Time{})
	if client.count() != 1 || turns.deletions.Load() != 1 || turns.stops.Load() != 2 {
		t.Fatal("owner expiry denial retained old Pod or retried authority")
	}
	select {
	case <-done:
	default:
		t.Fatal("revoked tracker not joined")
	}
}
