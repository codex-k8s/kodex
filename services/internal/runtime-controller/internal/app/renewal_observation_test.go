package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	sharedobservability "github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/runtime-controller/internal/callback"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// renewalPrivateSentinel is synthetic private-looking text inserted into errors, fences and input to detect accidental log disclosure.
// It is not a real token or credential.
// renewalPrivateSentinel является синтетическим текстом, похожим на частные данные, в ошибках, ограждениях и входе для обнаружения утечки в журнал.
// Это не настоящий токен или учётные данные.
const renewalPrivateSentinel = "PRIVATE_RENEWAL_SENTINEL token=private payload=private"

// renewalObservationInput adds deterministic execution pins and private-looking sentinel fields to the tracking fixture.
// The fixture permits checking allowed log fields without granting or renewing any real lease.
// renewalObservationInput добавляет детерминированные привязки выполнения и поля с частным синтетическим текстом к образцу отслеживания.
// Образец позволяет проверять допустимые поля журнала без выдачи или продления настоящей аренды.
func renewalObservationInput() runtimecontract.RunnerInput {
	input := runtimeTrackingInput()
	input.RunRef, input.NodeRef = "run_observation_abcdefgh", "nod_observation_abcdefgh"
	input.SessionRef, input.TurnRef, input.Attempt = "ses_observation_abcdefgh", "trn_observation_abcdefgh", 7
	input.InputDigest, input.RuntimeRevisionDigest, input.ExecutionBindingDigest = strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
	input.LeaseFence, input.Task, input.Instructions = renewalPrivateSentinel, renewalPrivateSentinel, renewalPrivateSentinel
	return input
}

// renewalRecord rejects sentinel disclosure and decodes exactly one JSON warning with the fixed renewal message.
// Malformed, additional or differently classified records fail the test rather than being accepted as evidence.
// renewalRecord отклоняет раскрытие синтетических частных данных и разбирает ровно одно JSON-предупреждение с фиксированным сообщением продления.
// Неверные, дополнительные или иначе классифицированные записи проваливают тест, а не принимаются как доказательство.
func renewalRecord(t *testing.T, wire *bytes.Buffer) map[string]any {
	t.Helper()
	if strings.Contains(wire.String(), "PRIVATE_RENEWAL_SENTINEL") || strings.Contains(wire.String(), "token=private") {
		t.Fatal("renewal observation disclosed private cause, fence or input")
	}
	decoder := json.NewDecoder(bytes.NewReader(wire.Bytes()))
	var record map[string]any
	if decoder.Decode(&record) != nil || decoder.Decode(new(any)) != io.EOF || record["msg"] != renewalFailureMessage {
		t.Fatal("renewal did not produce exactly one closed observation")
	}
	return record
}

// assertRenewalPins compares all expected locator, generation/attempt, digest and binding-status fields after JSON decoding.
// Numeric expectations use float64 because the decoded record is map[string]any.
// assertRenewalPins сопоставляет ожидаемые поля указателей, поколения/попытки, дайджестов и статуса привязки после разбора JSON.
// Числовые ожидания используют float64, поскольку разобранная запись имеет тип map[string]any.
func assertRenewalPins(t *testing.T, record map[string]any, input runtimecontract.RunnerInput, binding string) {
	t.Helper()
	for key, expected := range map[string]any{
		"lease_ref": input.LeaseRef, "lease_generation": float64(input.LeaseGeneration),
		"run_ref": input.RunRef, "node_ref": input.NodeRef, "session_ref": input.SessionRef,
		"turn_ref": input.TurnRef, "attempt": float64(input.Attempt), "input_digest": input.InputDigest,
		"runtime_revision_digest": input.RuntimeRevisionDigest, "execution_binding_digest": input.ExecutionBindingDigest,
		"binding_status": binding,
	} {
		if record[key] != expected {
			t.Fatalf("renewal observation changed trusted pin %s", key)
		}
	}
}

// TestRenewalObservationClosedCodesPhasesAndPrivateJoinedErrors covers every supported failure code across renewal phases,
// ensuring joined private errors do not replace the primary code or disclose details; unknown inputs normalize to closed fallback values.
// TestRenewalObservationClosedCodesPhasesAndPrivateJoinedErrors охватывает поддержанные коды ошибок во всех фазах продления,
// проверяя, что объединённые частные ошибки не заменяют основной код и не раскрывают детали; неизвестные входы нормализуются в закрытые значения.
func TestRenewalObservationClosedCodesPhasesAndPrivateJoinedErrors(t *testing.T) {
	input := renewalObservationInput()
	for _, phase := range []renewalPhase{renewalPhaseTrack, renewalPhaseDrain, renewalPhaseMaterialization} {
		for code := codes.Canceled; code <= codes.Unauthenticated; code++ {
			t.Run(string(phase)+"/"+code.String(), func(t *testing.T) {
				var wire bytes.Buffer
				runtime := &runtime{logger: slog.New(slog.NewJSONHandler(&wire, nil))}
				primary, err := status.New(code, renewalPrivateSentinel).WithDetails(wrapperspb.String(renewalPrivateSentinel))
				if err != nil {
					t.Fatal("private status fixture failed")
				}
				runtime.logRenewalFailure(t.Context(), t.Context(), input, phase,
					errors.Join(primary.Err(), context.Canceled, errors.New(renewalPrivateSentinel)))
				record := renewalRecord(t, &wire)
				if len(record) != 18 || record[renewalPhaseKey] != string(phase) || record[renewalCodeKey] != code.String() ||
					record[renewalRequestContextKey] != observationContextActive || record[renewalParentContextKey] != observationContextActive {
					t.Fatal("primary code, phase or active context was replaced by secondary cancellation")
				}
				assertRenewalPins(t, record, input, "EXECUTION_PINS")
			})
		}
	}
	for _, err := range []error{errors.New(renewalPrivateSentinel), status.Error(codes.Code(12345), renewalPrivateSentinel)} {
		var wire bytes.Buffer
		runtime := &runtime{logger: slog.New(slog.NewJSONHandler(&wire, nil))}
		runtime.logRenewalFailure(t.Context(), t.Context(), input, renewalPhase(renewalPrivateSentinel), err)
		record := renewalRecord(t, &wire)
		if record[renewalPhaseKey] != observationUnknown || record[renewalCodeKey] != codes.Unknown.String() {
			t.Fatal("unknown phase or noncanonical status escaped the closed observation")
		}
	}
}

// renewalUnknownContext embeds a real test context but supplies an unrecognized Err result for diagnostic normalization tests.
// It does not alter the embedded context's deadline or cancellation channel.
// renewalUnknownContext встраивает настоящий тестовый контекст, но задаёт неизвестный результат Err для проверки нормализации диагностики.
// Срок и канал отмены встроенного контекста не меняются.
type renewalUnknownContext struct{ context.Context }

// Err returns a fresh synthetic private error so classification must use UNKNOWN rather than logging the cause.
// Err возвращает новую синтетическую частную ошибку, чтобы классификация использовала UNKNOWN вместо вывода причины.
func (ctx renewalUnknownContext) Err() error { return errors.New(renewalPrivateSentinel) }

// TestRenewalObservationSnapshotsContextBeforeCleanupAndKeepsTrace checks active, canceled, expired and unknown request contexts
// are logged before cleanup while preserving parent state, trace/span identifiers and the unchanged global logger.
// TestRenewalObservationSnapshotsContextBeforeCleanupAndKeepsTrace проверяет, что активный, отменённый, просроченный и неизвестный контексты запроса
// фиксируются до очистки с сохранением состояния родителя, идентификаторов trace/span и неизменного глобального журнала.
func TestRenewalObservationSnapshotsContextBeforeCleanupAndKeepsTrace(t *testing.T) {
	for _, state := range []string{observationContextActive, observationContextCanceled, observationContextDeadline, observationUnknown} {
		t.Run(state, func(t *testing.T) {
			span := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, TraceFlags: trace.FlagsSampled})
			parent, stopParent := context.WithCancelCause(trace.ContextWithSpanContext(t.Context(), span))
			defer stopParent(nil)
			if state == observationContextCanceled {
				stopParent(errors.New(renewalPrivateSentinel))
			}
			var request context.Context
			var cleanup context.CancelFunc
			if state == observationContextDeadline {
				request, cleanup = context.WithDeadline(parent, time.Now().Add(-time.Second))
			} else {
				request, cleanup = context.WithCancel(parent)
				if state == observationUnknown {
					request = renewalUnknownContext{request}
				}
			}
			defer cleanup()
			var wire bytes.Buffer
			telemetry := &sharedobservability.Runtime{}
			runtime := &runtime{logger: telemetry.Logger(&wire)}
			previous := slog.Default()
			runtime.logRenewalFailure(request, parent, renewalObservationInput(), renewalPhaseTrack, status.Error(codes.Unavailable, renewalPrivateSentinel))
			cleanup()
			record := renewalRecord(t, &wire)
			wantParent := observationContextActive
			if state == observationContextCanceled {
				wantParent = observationContextCanceled
			}
			if record[renewalRequestContextKey] != state || record[renewalParentContextKey] != wantParent || record[renewalCodeKey] != codes.Unavailable.String() {
				t.Fatal("cleanup cancellation replaced the observed request or parent context")
			}
			if slog.Default() != previous || record["trace_id"] != span.TraceID().String() || record["span_id"] != span.SpanID().String() {
				t.Fatal("renewal observation lost the shared trace logger or changed the global logger")
			}
		})
	}
}

// TestRenewalObservationDoesNotLogSuccessOrUnsafePins requires silence on success and a missing logger,
// and checks invalid locator/digest fields and negative counters are sanitized without inventing complete bindings.
// TestRenewalObservationDoesNotLogSuccessOrUnsafePins требует отсутствия записи при успехе и отсутствии журнала
// и проверяет очистку неверных указателей/дайджестов и отрицательных счётчиков без выдумывания полных привязок.
func TestRenewalObservationDoesNotLogSuccessOrUnsafePins(t *testing.T) {
	var wire bytes.Buffer
	runtime := &runtime{logger: slog.New(slog.NewJSONHandler(&wire, nil))}
	input := renewalObservationInput()
	runtime.logRenewalFailure(t.Context(), t.Context(), input, renewalPhaseTrack, nil)
	if wire.Len() != 0 {
		t.Fatal("successful renew acquired a failure observation")
	}
	input.LeaseRef, input.RunRef, input.NodeRef, input.SessionRef, input.TurnRef = renewalPrivateSentinel, renewalPrivateSentinel, renewalPrivateSentinel, renewalPrivateSentinel, renewalPrivateSentinel
	input.InputDigest, input.RuntimeRevisionDigest, input.ExecutionBindingDigest = renewalPrivateSentinel, renewalPrivateSentinel, renewalPrivateSentinel
	input.Attempt, input.LeaseGeneration = -1, -1
	runtime.logRenewalFailure(t.Context(), t.Context(), input, renewalPhaseTrack, errors.New(renewalPrivateSentinel))
	record := renewalRecord(t, &wire)
	if record["binding_status"] != "INCOMPLETE_PINS" || record["attempt"] != float64(0) || record["lease_generation"] != float64(0) {
		t.Fatal("unsafe or absent pins fabricated a complete binding")
	}
	for _, field := range []string{"lease_ref", "run_ref", "node_ref", "session_ref", "turn_ref", "input_digest", "runtime_revision_digest", "execution_binding_digest"} {
		if record[field] != "" {
			t.Fatal("unsafe pin escaped the closed observation")
		}
	}
	runtime.logger = nil
	runtime.logRenewalFailure(t.Context(), t.Context(), input, renewalPhaseTrack, errors.New(renewalPrivateSentinel))
}

// renewalObservationClient records synthetic renewals, verifies exact lease/fence/generation pins and delegates the failure to a test callback.
// It retains the request context so tests can distinguish primary observation from later cleanup cancellation.
// renewalObservationClient записывает синтетические продления, проверяет точные привязки аренды/ограждения/поколения и передаёт ошибку тестовому обратному вызову.
// Контекст запроса сохраняется, чтобы тесты отличали исходное наблюдение от последующей отмены при очистке.
type renewalObservationClient struct {
	runtimeWorkClientStub
	calls   atomic.Int32
	request context.Context
	renew   func(context.Context) error
	input   runtimecontract.RunnerInput
	t       *testing.T
}

// RenewExecution counts the call, saves its context and rejects any changed request pin through the owning test.
// It returns the configured failure without issuing a network call or an owner receipt.
// RenewExecution считает обращение, сохраняет контекст и отклоняет изменённые привязки запроса через тест-владелец.
// Возвращается настроенная ошибка без сетевого обращения или квитанции владельца.
func (client *renewalObservationClient) RenewExecution(ctx context.Context, request *controlplanev1.RenewExecutionRequest, _ ...grpc.CallOption) (*controlplanev1.RenewExecutionResponse, error) {
	client.calls.Add(1)
	client.request = ctx
	if request.LeaseRef != client.input.LeaseRef || request.Fence != client.input.LeaseFence || request.Generation != client.input.LeaseGeneration {
		client.t.Fatal("observation changed the owner request binding")
	}
	return nil, client.renew(ctx)
}

// renewalObservationTurns wraps local lifecycle counters with assertions that a complete renewal warning already exists before deletion.
// It carries only synthetic pins and a test buffer, not a Kubernetes workload client.
// renewalObservationTurns дополняет локальные счётчики жизненного цикла проверкой наличия полного предупреждения продления до удаления.
// Хранятся только синтетические привязки и тестовый буфер, а не клиент нагрузки Kubernetes.
type renewalObservationTurns struct {
	turnLifecycleStub
	wire  *bytes.Buffer
	t     *testing.T
	input runtimecontract.RunnerInput
	phase renewalPhase
}

// DeleteTurn verifies the warning phase, pre-cleanup request state and all expected pins before delegating to the local delete counter.
// No real Pod, Secret or volume is deleted.
// DeleteTurn проверяет фазу предупреждения, состояние запроса до очистки и все ожидаемые привязки перед передачей локальному счётчику удаления.
// Настоящие Pod, Secret и тома не удаляются.
func (turns *renewalObservationTurns) DeleteTurn(ctx context.Context, lease string) error {
	record := renewalRecord(turns.t, turns.wire)
	if record[renewalPhaseKey] != string(turns.phase) || record[renewalRequestContextKey] != observationContextActive {
		turns.t.Fatal("deletion preceded the primary observation or cleanup contaminated its context")
	}
	assertRenewalPins(turns.t, record, turns.input, "EXECUTION_PINS")
	return turns.turnLifecycleStub.DeleteTurn(ctx, lease)
}

// TestRenewalObservationPrecedesTrackAndDrainDeletionWithoutChangingFailures covers track/drain with denial and transient status fixtures.
// It checks observation order, unchanged stop/delete and completion behavior, request cleanup and tracker capacity release.
// TestRenewalObservationPrecedesTrackAndDrainDeletionWithoutChangingFailures проверяет отслеживание и завершение работы с отказами и временными статусами.
// Проверяются порядок наблюдения, неизменные остановка/удаление и завершение, очистка запроса и освобождение ёмкости отслеживания.
func TestRenewalObservationPrecedesTrackAndDrainDeletionWithoutChangingFailures(t *testing.T) {
	for _, phase := range []renewalPhase{renewalPhaseTrack, renewalPhaseDrain} {
		for _, code := range []codes.Code{codes.PermissionDenied, codes.Unavailable, codes.DeadlineExceeded} {
			t.Run(string(phase)+"/"+code.String(), func(t *testing.T) {
				input := renewalObservationInput()
				var wire bytes.Buffer
				client := &renewalObservationClient{input: input, t: t, renew: func(context.Context) error { return status.Error(code, renewalPrivateSentinel) }}
				coordinator := callback.NewCoordinator()
				done := coordinator.Register(input)
				turns := &renewalObservationTurns{wire: &wire, t: t, input: input, phase: phase}
				runtime := trackingRuntime(client, turns, coordinator)
				runtime.logger = slog.New(slog.NewJSONHandler(&wire, nil))
				runtime.config.LeaseRenewInterval, runtime.inspectInterval, runtime.terminalGrace = time.Millisecond, time.Hour, time.Second
				wantStops := int32(1)
				if phase == renewalPhaseTrack {
					runtime.track(t.Context(), input, done, false, time.Time{})
				} else {
					wantStops = 2
					parent, cancel := context.WithCancel(t.Context())
					cancel()
					runtime.drainTurn(parent, input, done, time.Time{})
				}
				if client.calls.Load() != 1 || client.count() != 0 || turns.stops.Load() != wantStops || turns.deletions.Load() != 1 || client.request.Err() != context.Canceled {
					t.Fatal("observation changed renew retry, fallback completion, stop/delete or request cleanup")
				}
				select {
				case <-done:
				default:
					t.Fatal("existing revoked coordinator completion was not preserved")
				}
				if phase == renewalPhaseTrack && len(runtime.capacity) != 0 {
					t.Fatal("tracker did not release its existing capacity")
				}
			})
		}
	}
}

// TestRenewalObservationMaterializationUsesTrustedClaimPinsBeforeCancellation records a claim-only materialization denial before cancellation.
// The original error, exact renewal request and closed publication/handoff remain unchanged, with one observation and no retry.
// TestRenewalObservationMaterializationUsesTrustedClaimPinsBeforeCancellation фиксирует отказ размещения с привязками только заявки до отмены.
// Исходная ошибка, точный запрос продления и запрет публикации/передачи сохраняются, с одним наблюдением и без повторов.
func TestRenewalObservationMaterializationUsesTrustedClaimPinsBeforeCancellation(t *testing.T) {
	input := renewalObservationInput()
	input.ExecutionBindingDigest = ""
	denied := status.Error(codes.PermissionDenied, renewalPrivateSentinel)
	var wire bytes.Buffer
	client := &materializationRenewClient{renew: func(context.Context, *controlplanev1.RenewExecutionRequest) error { return denied }}
	runtime := keeperTestRuntime(client)
	runtime.logger = slog.New(slog.NewJSONHandler(&wire, nil))
	keepers := runtime.keepMaterializationClaims(t.Context(), []*controlplanev1.ClaimedExecution{{
		Lease: &controlplanev1.WorkLease{Ref: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration},
		Run:   &controlplanev1.Run{Ref: input.RunRef}, Node: &controlplanev1.RunNode{Ref: input.NodeRef},
		Revision: &controlplanev1.RuntimeRevisionSnapshot{SessionRef: input.SessionRef, TurnRef: input.TurnRef, Attempt: input.Attempt,
			InputDigest: input.InputDigest, RevisionDigest: input.RuntimeRevisionDigest},
	}})
	keeper := keepers[0]
	defer keeper.stop()
	if !errors.Is(keeper.await(), denied) {
		t.Fatal("observation replaced the original materialization denial")
	}
	record := renewalRecord(t, &wire)
	assertRenewalPins(t, record, input, "CLAIM_PINS")
	if record[renewalPhaseKey] != string(renewalPhaseMaterialization) || record[renewalRequestContextKey] != observationContextActive ||
		record[renewalParentContextKey] != observationContextActive || keeper.ctx.Err() != context.Canceled {
		t.Fatal("materialization cleanup replaced the primary active-context observation")
	}
	called := false
	if !errors.Is(keeper.publish(func(context.Context) error { called = true; return nil }), denied) || called || !errors.Is(keeper.handoff(), denied) {
		t.Fatal("observed denial changed publication or handoff authority")
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.requests) != 1 || client.requests[0].Fence != input.LeaseFence || client.requests[0].Generation != input.LeaseGeneration {
		t.Fatal("materialization observation added a retry or changed the exact owner request")
	}
	renewalRecord(t, &wire)
}

// TestRenewalObservationMaterializationRPCDeadlineIsNotCleanupCancellation makes the request deadline expire during materialization renewal.
// The warning must retain DeadlineExceeded and an active parent, distinguishing that failure from later keeper cancellation without adding a retry.
// TestRenewalObservationMaterializationRPCDeadlineIsNotCleanupCancellation доводит запрос до истечения срока во время продления для размещения.
// Предупреждение должно сохранить DeadlineExceeded и активного родителя, отличая ошибку от последующей отмены держателя без добавления повтора.
func TestRenewalObservationMaterializationRPCDeadlineIsNotCleanupCancellation(t *testing.T) {
	input := renewalObservationInput()
	var wire bytes.Buffer
	client := &materializationRenewClient{renew: func(ctx context.Context, _ *controlplanev1.RenewExecutionRequest) error {
		<-ctx.Done()
		return status.Error(codes.DeadlineExceeded, renewalPrivateSentinel)
	}}
	runtime := keeperTestRuntime(client)
	runtime.config.RequestTimeout = 5 * time.Millisecond
	runtime.logger = slog.New(slog.NewJSONHandler(&wire, nil))
	keeper := runtime.keepMaterializationLease(t.Context(), input)
	defer keeper.stop()
	if status.Code(keeper.await()) != codes.DeadlineExceeded {
		t.Fatal("observation changed the original request deadline failure")
	}
	record := renewalRecord(t, &wire)
	if record[renewalCodeKey] != codes.DeadlineExceeded.String() || record[renewalRequestContextKey] != observationContextDeadline ||
		record[renewalParentContextKey] != observationContextActive || keeper.ctx.Err() != context.Canceled {
		t.Fatal("RPC deadline was confused with the later materialization cancellation")
	}
	assertRenewalPins(t, record, input, "EXECUTION_PINS")
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.requests) != 1 {
		t.Fatal("observation retried an expired materialization request")
	}
}
