package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"github.com/codex-k8s/kodex/services/internal/runtime-controller/internal/callback"
	"github.com/codex-k8s/kodex/services/internal/runtime-controller/internal/credentialprojection"
	"github.com/codex-k8s/kodex/services/internal/runtime-controller/internal/workload"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// defaultTurnInspectionInterval schedules local workload observations every two seconds; it is independent of owner lease renewal.
	// defaultTurnInspectionInterval задаёт локальное наблюдение нагрузки раз в две секунды отдельно от продления аренды владельцем.
	defaultTurnInspectionInterval = 2 * time.Second
	// defaultTerminalCallbackGrace allows up to 120 seconds for terminal callback delivery or graceful drain.
	// The grace does not extend confirmed lease authority or override an execution deadline.
	// defaultTerminalCallbackGrace даёт до 120 секунд на доставку завершающего обратного вызова или мягкое завершение.
	// Этот период не продлевает подтверждённые полномочия аренды и не отменяет срок выполнения.
	defaultTerminalCallbackGrace = 120 * time.Second
	// failureCompletionMaximumTries caps repeated delivery of one immutable failure-completion request at five calls.
	// failureCompletionMaximumTries ограничивает повторную доставку одного неизменного запроса завершения с ошибкой пятью вызовами.
	failureCompletionMaximumTries = 5
	// warmReconcileRPCFailure supplies the fixed operation label for sanitized warm-reconciliation RPC failures.
	// warmReconcileRPCFailure задаёт фиксированную метку операции для очищенных ошибок RPC согласования прогретой среды.
	warmReconcileRPCFailure = "reconcile system assistant warm runtime"
	// warmReportRPCFailure supplies the fixed operation label for sanitized warm-state reporting failures.
	// warmReportRPCFailure задаёт фиксированную метку операции для очищенных ошибок отчёта о прогретой среде.
	warmReportRPCFailure = "report system assistant warm runtime"
	// runtimeClaimRPCFailure identifies sanitized claim-delivery diagnostics without embedding an upstream message.
	// runtimeClaimRPCFailure обозначает очищенную диагностику доставки заявки без включения исходного сообщения вышестоящего сервиса.
	runtimeClaimRPCFailure = "claim runtime execution"
	// warmDesiredRevisionMissing reports an absent desired warm revision outside the explicit provider-unconfigured response.
	// warmDesiredRevisionMissing сообщает об отсутствующей требуемой ревизии прогретой среды вне явного ответа о ненастроенном провайдере.
	warmDesiredRevisionMissing = "system assistant warm runtime desired revision is missing"
	// turnMaterializationFailure labels sanitized credential-projection or workload-creation errors in materialization logs.
	// turnMaterializationFailure помечает очищенные ошибки проекции учётных данных или создания нагрузки в журнале размещения.
	turnMaterializationFailure = "runtime turn materialization"
	// turnInputRejectionMessage fixes the English warning for rejected runtime input, with stage and closed validation reason supplied separately.
	// turnInputRejectionMessage задаёт английское предупреждение об отклонённом входе выполнения; стадия и закрытая причина передаются отдельно.
	turnInputRejectionMessage = "runtime turn input rejected"
	// turnInputStageAttribute names the structured field identifying the rejected input-build stage.
	// turnInputStageAttribute задаёт имя структурированного поля стадии отклонённого построения входа.
	turnInputStageAttribute = "stage"
	// turnInputReasonAttribute names the structured field carrying a bounded input-validation reason rather than raw input.
	// turnInputReasonAttribute задаёт имя структурированного поля ограниченной причины проверки вместо исходного входа.
	turnInputReasonAttribute = "reason"
)

// defaultFailureCompletionRetryDelays provides four backoff intervals between at most five failure-completion calls.
// New runtimes initially reference this shared array as a slice; the schedule does not itself authorize a retry.
// defaultFailureCompletionRetryDelays задаёт четыре задержки между максимум пятью вызовами завершения с ошибкой.
// Новые среды сначала ссылаются на этот общий массив срезом; само расписание не разрешает повтор.
var defaultFailureCompletionRetryDelays = [...]time.Duration{
	500 * time.Millisecond,
	1500 * time.Millisecond,
	3 * time.Second,
	6 * time.Second,
}

// turnLifecycle is the controller's narrow workload-observation and cleanup port for one exact turn.
// Owner completion and credential materialization remain separate operations.
// turnLifecycle является узким портом контроллера для наблюдения нагрузки и очистки одного точного хода.
// Завершение у владельца и размещение учётных данных остаются отдельными операциями.
type turnLifecycle interface {
	// ObserveTurnPod requests a workload observation bound to the supplied execution and warm/nonwarm mode.
	// A Pod state alone is not an owner completion receipt.
	// ObserveTurnPod запрашивает наблюдение нагрузки, привязанное к переданному выполнению и режиму прогретой/обычной среды.
	// Само состояние Pod не является квитанцией завершения у владельца.
	ObserveTurnPod(context.Context, runtimecontract.RunnerInput, bool) (workload.TurnPodObservation, error)
	// StopTurn requests stopping resources for the supplied lease within the caller's context.
	// The operation does not itself finalize owner execution state.
	// StopTurn запрашивает остановку ресурсов переданной аренды в контексте потребителя.
	// Операция сама не завершает состояние выполнения у владельца.
	StopTurn(context.Context, string) error
	// DeleteTurn requests cleanup of a lease's turn resources; the concrete workload adapter owns exact binding and warm-resource retention.
	// It does not replace an owner completion decision.
	// DeleteTurn запрашивает очистку ресурсов хода по аренде; конкретный адаптер нагрузки отвечает за точные привязки и сохранение прогретых ресурсов.
	// Решение владельца о завершении этим не заменяется.
	DeleteTurn(context.Context, string) error
}

// credentialMaterializer supplies only the protected materialization operation required before ordinary Pod publication.
// It returns a version-pinned descriptor rather than credential contents.
// credentialMaterializer предоставляет только защищённую операцию размещения, необходимую до публикации обычного Pod.
// Возвращается привязанный к версии дескриптор, а не содержимое учётных данных.
type credentialMaterializer interface {
	// Materialize resolves a credential projection for the exact runner input under the provided request context.
	// Returned metadata must remain bound to that execution when passed to workload construction.
	// Materialize разрешает проекцию учётных данных для точного входа исполнителя в переданном контексте запроса.
	// Возвращённые метаданные должны сохранять привязку к выполнению при передаче созданию нагрузки.
	Materialize(context.Context, runtimecontract.RunnerInput) (credentialprojection.Projection, error)
}

// warmDeactivator separates warm-workload deactivation from ordinary turn lifecycle so provider-unconfigured handling can be tested locally.
// warmDeactivator отделяет отключение прогретой нагрузки от обычного жизненного цикла хода для локальной проверки ненастроенного провайдера.
type warmDeactivator interface {
	// DeactivateWarm requests removal of the currently managed warm workload within the caller's budget.
	// It does not manufacture a replacement provider configuration or a successful readiness observation.
	// DeactivateWarm запрашивает удаление текущей управляемой прогретой нагрузки в бюджете потребителя.
	// Замещающая конфигурация провайдера или успешное наблюдение готовности не выдумываются.
	DeactivateWarm(context.Context) error
}

// runtime coordinates owner claims, materialization, warm compatibility and independent per-turn trackers.
// Its capacity channel bounds concurrency; tracker lifetime and warm-cache locking are local control mechanisms, not sources of owner authority.
// runtime координирует заявки владельца, размещение, совместимость прогретой среды и независимое отслеживание ходов.
// Канал ёмкости ограничивает параллелизм; время жизни отслеживания и блокировка прогретого кэша — локальные механизмы, а не источники полномочий владельца.
type runtime struct {
	control           controlplanev1.RuntimeWorkServiceClient
	credentials       credentialMaterializer
	manager           *workload.Manager
	warmDeactivator   warmDeactivator
	turns             turnLifecycle
	coordinator       *callback.Coordinator
	config            Config
	assistant         *serviceruntime.Readiness
	logger            *slog.Logger
	capacity          chan struct{}
	trackers          sync.WaitGroup
	inspectInterval   time.Duration
	terminalGrace     time.Duration
	completionRetries []time.Duration
	warmMu            sync.RWMutex
	warmCompatibility string
	warmTicket        string
}

// newRuntime wires already constructed clients/manager/coordinator and initializes concurrency, inspection and failure-delivery defaults.
// It starts no goroutine, performs no claim and assumes the supplied configuration has been validated.
// newRuntime связывает уже созданные клиенты, менеджер и координатор и задаёт параллелизм, наблюдение и расписание доставки ошибок.
// Горутины не запускаются, заявка не запрашивается; предполагается, что переданная конфигурация уже проверена.
func newRuntime(control controlplanev1.RuntimeWorkServiceClient, credentials credentialMaterializer, manager *workload.Manager, coordinator *callback.Coordinator, config Config, assistant *serviceruntime.Readiness, logger *slog.Logger) *runtime {
	return &runtime{control: control, credentials: credentials, manager: manager, warmDeactivator: manager, turns: manager, coordinator: coordinator, config: config, assistant: assistant, logger: logger,
		capacity: make(chan struct{}, config.MaximumConcurrentTurns), inspectInterval: defaultTurnInspectionInterval,
		terminalGrace: defaultTerminalCallbackGrace, completionRetries: defaultFailureCompletionRetryDelays[:]}
}

// Run cleans stale turns, attempts initial warm reconciliation and then drives separate warm/claim timers with bounded idle backoff.
// Warm or claim delivery failures are logged without silently completing executions; exit cancels the local child context and waits for every tracker.
// Run очищает устаревшие ходы, пытается согласовать прогретую среду и затем обслуживает раздельные таймеры прогрева/заявок с ограниченной задержкой простоя.
// Ошибки доставки прогрева или заявок фиксируются без скрытого завершения выполнений; при выходе отменяется локальный дочерний контекст и ожидаются все отслеживающие работники.
func (runtime *runtime) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer func() {
		cancel()
		runtime.trackers.Wait()
	}()
	if err := runtime.manager.CleanupStaleTurns(ctx); err != nil {
		return err
	}
	if err := runtime.reconcileWarm(ctx); err != nil {
		runtime.logger.WarnContext(ctx, "system assistant warm runtime reconciliation failed", "error", err)
	}
	poll := time.NewTimer(runtime.config.PollInterval)
	defer poll.Stop()
	idleBackoff := serviceruntime.NewIdleBackoff(runtime.config.PollInterval, 5*time.Second)
	warm := time.NewTicker(runtime.config.InfrastructureCheckInterval)
	defer warm.Stop()
	claimDegraded := false
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-warm.C:
			if err := runtime.reconcileWarm(ctx); err != nil {
				runtime.logger.WarnContext(ctx, "system assistant warm runtime reconciliation failed", "error", err)
				runtime.setAssistantUnavailable(ctx)
			}
		case <-poll.C:
			if len(runtime.capacity) >= cap(runtime.capacity) {
				poll.Reset(idleBackoff.Next(true))
				continue
			}
			claimed, err := runtime.claim(ctx)
			if err != nil && !errors.Is(err, context.Canceled) && !claimDegraded {
				claimDegraded = true
				runtime.logger.WarnContext(ctx, "runtime claim delivery degraded", "error_class", "control_plane", "error", boundedRPCFailure(runtimeClaimRPCFailure, err))
			} else if err == nil && claimDegraded {
				claimDegraded = false
				runtime.logger.InfoContext(ctx, "runtime claim delivery restored")
			}
			poll.Reset(idleBackoff.Next(err == nil && claimed > 0))
		}
	}
}

// reconcileWarm reads desired owner state and builds/ensures the pinned warm input, reporting its actual provisioning/ready state.
// An explicit unconfigured-provider failure clears cached ticket/compatibility and deactivates warm resources; only a ready workload with a retrieved ticket updates the cache and ready flag.
// reconcileWarm читает требуемое состояние владельца, строит и обеспечивает привязанный вход прогретой среды и сообщает фактическую подготовку/готовность.
// Явная ошибка ненастроенного провайдера очищает кэш билета/совместимости и отключает прогретые ресурсы; кэш и готовность обновляются только для готовой нагрузки с полученным билетом.
func (runtime *runtime) reconcileWarm(ctx context.Context) error {
	request, cancel := context.WithTimeout(ctx, runtime.config.RequestTimeout)
	defer cancel()
	response, err := runtime.control.ReconcileWarmRuntime(request, &controlplanev1.ReconcileWarmRuntimeRequest{WorkloadInstance: runtime.config.PodUID})
	if err != nil {
		return boundedRPCFailure(warmReconcileRPCFailure, err)
	}
	if response != nil && response.GetDesiredRevision() == nil && !response.GetMaterializationRequired() &&
		response.GetAssistant().GetRuntimeState() == controlplanev1.AssistantRuntimeState_ASSISTANT_RUNTIME_STATE_FAILED {
		runtime.warmMu.Lock()
		runtime.warmCompatibility = ""
		runtime.warmTicket = ""
		runtime.warmMu.Unlock()
		runtime.assistant.Set(false, "assistant_runtime_provider_unconfigured")
		cleanupContext, cleanupCancel := context.WithTimeout(ctx, runtime.config.RequestTimeout)
		deactivateErr := runtime.warmDeactivator.DeactivateWarm(cleanupContext)
		cleanupCancel()
		return deactivateErr
	}
	if response == nil || response.GetDesiredRevision() == nil {
		return errors.New(warmDesiredRevisionMissing)
	}
	input, providerBinding, err := runtime.manager.BuildWarmInput(response.GetDesiredRevision())
	if err != nil {
		return err
	}
	compatibilityDigest, err := runtimecontract.WarmCompatibilityDigest(input)
	if err != nil {
		return err
	}
	ready, err := runtime.manager.EnsureWarm(request, input, providerBinding)
	if err != nil {
		return err
	}
	state := controlplanev1.AssistantRuntimeState_ASSISTANT_RUNTIME_STATE_PROVISIONING
	if ready {
		state = controlplanev1.AssistantRuntimeState_ASSISTANT_RUNTIME_STATE_READY
	}
	if err := runtime.reportWarm(ctx, input.RuntimeRevisionRef, state, ""); err != nil {
		return err
	}
	if ready {
		ticket, ticketErr := runtime.manager.WarmTicket(request, input.RuntimeRevisionRef, input.RuntimeRevisionDigest)
		if ticketErr != nil {
			return ticketErr
		}
		runtime.warmMu.Lock()
		runtime.warmCompatibility = compatibilityDigest
		runtime.warmTicket = ticket
		runtime.warmMu.Unlock()
		if runtime.assistant.Set(true, "ready") {
			runtime.logger.InfoContext(ctx, "system assistant warm runtime restored")
		}
	} else {
		runtime.assistant.Set(false, "assistant_runtime_materializing")
	}
	return nil
}

// reportWarm sends controller instance, exact runtime revision, state and supplied safe error code under a request timeout.
// RPC failure is converted to a bounded diagnostic without adding a retry or marking the report successful.
// reportWarm передаёт экземпляр контроллера, точную ревизию, состояние и переданный безопасный код ошибки с таймаутом запроса.
// Ошибка RPC преобразуется в ограниченную диагностику без добавления повтора или успешного статуса отчёта.
func (runtime *runtime) reportWarm(ctx context.Context, revision string, state controlplanev1.AssistantRuntimeState, code string) error {
	request, cancel := context.WithTimeout(ctx, runtime.config.RequestTimeout)
	defer cancel()
	_, err := runtime.control.ReportWarmRuntime(request, &controlplanev1.ReportWarmRuntimeRequest{WorkloadInstance: runtime.config.PodUID, RuntimeRevision: revision, State: state, SafeErrorCode: code})
	if err != nil {
		return boundedRPCFailure(warmReportRPCFailure, err)
	}
	return nil
}

// boundedRPCFailure emits a caller-supplied fixed operation label, normalized gRPC code and optional closed local-authority diagnostic.
// It deliberately does not wrap or retain the raw cause, status details or payload; callers must not put untrusted content in the operation label.
// boundedRPCFailure выдаёт фиксированную метку операции от потребителя, нормализованный код gRPC и необязательную закрытую диагностику локальных полномочий.
// Исходная причина, детали статуса и содержимое намеренно не оборачиваются и не сохраняются; потребители не должны передавать недоверенные данные в метку операции.
func boundedRPCFailure(operation string, cause error) error {
	code := status.Code(cause)
	if code < codes.Canceled || code > codes.Unauthenticated {
		code = codes.Unknown
	}
	var local *authorityclient.LocalAuthorityError
	if errors.As(cause, &local) && local != nil {
		return fmt.Errorf("%s: grpc_code=%s [%s]", operation, code, local.Diagnostic())
	}
	return fmt.Errorf("%s: grpc_code=%s", operation, code)
}

// setAssistantUnavailable updates only the local assistant readiness flag and logs the transition once when it changes.
// It does not revoke a provider account or alter the owner's warm-runtime state.
// setAssistantUnavailable меняет только локальный признак готовности помощника и фиксирует переход один раз при изменении.
// Аккаунт провайдера не отзывается, состояние прогретой среды у владельца не меняется.
func (runtime *runtime) setAssistantUnavailable(ctx context.Context) {
	if runtime.assistant.Set(false, "assistant_runtime_unavailable") {
		runtime.logger.WarnContext(ctx, "system assistant warm runtime lost", "error_class", "dependency")
	}
}

// claim requests at most one owner execution per available-capacity poll, then protects materialization with independent lease keepers.
// It chooses eligible warm dispatch or exact credential projection/guarded Pod creation, hands renewal off before starting a tracker and releases capacity on failure.
// The returned count is the response execution count, not proof that every returned execution started successfully; owner claims remain distinct from concurrent tracker capacity.
// claim запрашивает не более одного выполнения владельца за опрос при наличии ёмкости, затем защищает размещение независимыми держателями аренды.
// Выбирается допустимая прогретая отправка либо точная проекция учётных данных/защищённое создание Pod, продление передаётся до запуска отслеживания, а при ошибке ёмкость освобождается.
// Возвращаемое число — количество выполнений в ответе, а не доказательство успешного запуска каждого; заявки владельца отделены от ёмкости параллельного отслеживания.
func (runtime *runtime) claim(ctx context.Context) (int, error) {
	limit := cap(runtime.capacity) - len(runtime.capacity)
	// Each task's catalog and authority are pinned by a separate bounded transaction;
	// this does not limit the number of concurrently running trackers.
	// Каталог и authority каждого задания фиксируются отдельной bounded
	// транзакцией. Это не ограничивает число одновременно работающих tracker.
	if limit > 1 {
		limit = 1
	}
	if limit < 1 {
		return 0, nil
	}
	request, cancel := context.WithTimeout(ctx, runtime.config.RequestTimeout)
	response, err := runtime.control.ClaimExecution(request, &controlplanev1.ClaimExecutionRequest{WorkloadInstance: runtime.config.PodUID, Limit: int32(limit)})
	cancel()
	if err != nil {
		return 0, err
	}
	keepers := runtime.keepMaterializationClaims(ctx, response.GetExecutions())
	defer func() {
		for _, keeper := range keepers {
			keeper.stop()
		}
	}()
	for index, execution := range response.GetExecutions() {
		keeper := keepers[index]
		if keeper.await() != nil {
			continue
		}
		input, providerBinding, buildErr := runtime.manager.BuildTurnInput(execution)
		if buildErr != nil {
			stage := "unknown"
			reason := "UNKNOWN"
			var inputFailure *workload.TurnInputBuildError
			if errors.As(buildErr, &inputFailure) {
				stage = inputFailure.Stage
				reason = inputFailure.ValidationReason()
			}
			runtime.logger.WarnContext(ctx, turnInputRejectionMessage, turnInputStageAttribute, stage, turnInputReasonAttribute, reason)
			runtime.failClaim(ctx, input, execution, "RUNTIME_REVISION_INVALID")
			continue
		}
		runtime.capacity <- struct{}{}
		done := runtime.coordinator.Register(input)
		warmExecution := false
		if input.IsSystemAssistant() {
			runtime.warmMu.RLock()
			warmCompatibility, warmTicket := runtime.warmCompatibility, runtime.warmTicket
			runtime.warmMu.RUnlock()
			turnCompatibility, compatibilityErr := runtimecontract.WarmCompatibilityDigest(input)
			if compatibilityErr == nil && warmCompatibility == turnCompatibility && warmTicket != "" && warmFileProjectionEligible(input) {
				err := keeper.publish(func(dispatchContext context.Context) error {
					if err := runtime.manager.RegisterWarmTurn(dispatchContext, input, warmTicket); err != nil {
						return err
					}
					return runtime.coordinator.EnqueueWarm(input, turnCompatibility)
				})
				if err != nil {
					<-runtime.capacity
					if context.Cause(keeper.ctx) != nil {
						runtime.closeRevokedTurn(ctx, input, done)
					} else {
						runtime.failClaim(ctx, input, execution, "SYSTEM_ASSISTANT_DISPATCH_FAILED")
					}
					continue
				}
				warmExecution = true
				_ = runtime.reportWarm(ctx, input.RuntimeRevisionRef, controlplanev1.AssistantRuntimeState_ASSISTANT_RUNTIME_STATE_BUSY, "")
			}
		}
		if !warmExecution {
			projectionContext, cancelProjection := context.WithTimeout(keeper.ctx, runtime.config.RequestTimeout)
			projection, projectionErr := runtime.credentials.Materialize(projectionContext, input)
			cancelProjection()
			failureStage := "credential_projection"
			if projectionErr == nil {
				failureStage = "ensure_turn"
				projectionErr = runtime.manager.EnsureTurnGuarded(keeper.ctx, input, providerBinding, workload.CredentialProjection{
					Namespace: projection.Namespace, SecretName: projection.SecretName, SecretUID: projection.SecretUID,
					SecretResourceVersion: projection.SecretResourceVersion, ContentSHA256: projection.ContentSHA256,
					ProviderAuthKey: projection.ProviderAuthKey, RuntimeSecretKeys: projection.RuntimeSecretKeys,
				}, keeper.publish)
			}
			if projectionErr != nil {
				if context.Cause(keeper.ctx) != nil {
					runtime.closeRevokedTurn(ctx, input, done)
					<-runtime.capacity
					continue
				}
				attributes := []any{"error_class", "dependency", "stage", failureStage, "error", boundedRPCFailure(turnMaterializationFailure, projectionErr)}
				if failureStage == "ensure_turn" {
					attributes = append(attributes, "ensure_turn_reason", boundedEnsureTurnReason(projectionErr))
				}
				if input.Validate() != nil {
					attributes = append(attributes, "input_valid", false)
				}
				if requestDigest, digestErr := credentialprojection.MaterializationRequestDigest(input); digestErr == nil {
					attributes = append(attributes, "request_digest_sha256", requestDigest)
				}
				runtime.logger.WarnContext(ctx, "runtime turn materialization failed", attributes...)
				<-runtime.capacity
				runtime.failClaim(ctx, input, execution, "RUNTIME_MATERIALIZATION_FAILED")
				continue
			}
		}
		if keeper.handoff() != nil {
			runtime.closeRevokedTurn(ctx, input, done)
			<-runtime.capacity
			continue
		}
		runtime.trackers.Add(1)
		go func() {
			defer runtime.trackers.Done()
			runtime.track(ctx, input, done, warmExecution, keeper.confirmedUntil)
		}()
	}
	return len(response.GetExecutions()), nil
}

// boundedEnsureTurnReason classifies a nonnil workload error by the first matching known fragment and returns only a closed reason or other.
// It does not expose the original message or establish whether the described resource action committed.
// boundedEnsureTurnReason классифицирует ненулевую ошибку нагрузки по первому известному фрагменту и возвращает только закрытую причину либо other.
// Исходное сообщение не раскрывается, фиксация описанного действия с ресурсом не подтверждается.
func boundedEnsureTurnReason(err error) string {
	message := err.Error()
	for _, match := range []struct{ text, reason string }{
		{"existing runtime session volume conflicts with exact session binding", "session_binding_conflict"},
		{"assistant session volume still has an active consumer", "active_session_consumer"},
		{"promote assistant session volume project binding", "session_promotion_update"},
		{"assistant session volume project binding readback failed", "session_promotion_readback"},
		{"runtime turn input is invalid", "turn_input_invalid"},
		{"runtime revision digest mismatch", "revision_digest_mismatch"},
		{"runtime credential projection binding is invalid", "projection_binding_invalid"},
	} {
		if strings.Contains(message, match.text) {
			return match.reason
		}
	}
	return "other"
}

// warmFileProjectionEligible permits the current warm dispatch path only when attachment sets and input artifacts are both absent.
// This local eligibility predicate does not validate runtime authority or warm compatibility by itself.
// warmFileProjectionEligible допускает текущую прогретую отправку только при отсутствии наборов вложений и входных артефактов.
// Сам локальный предикат не проверяет полномочия выполнения или совместимость прогретой среды.
func warmFileProjectionEligible(input runtimecontract.RunnerInput) bool {
	return len(input.AttachmentSets) == 0 && len(input.InputArtifacts) == 0
}

// track owns one reserved capacity slot until callback completion, revocation cleanup or bounded terminal fallback.
// It observes exact Pods, renews only owner-confirmed lease authority, preserves callback grace and memory diagnostics, and distinguishes leadership loss from graceful execution shutdown.
// track владеет одним зарезервированным местом ёмкости до завершения обратного вызова, очистки при отзыве или ограниченного завершающего запасного пути.
// Наблюдаются точные Pod, продлеваются только подтверждённые владельцем полномочия, сохраняются период ожидания обратного вызова и диагностика памяти, а потеря лидерства отличается от мягкого завершения выполнения.
func (runtime *runtime) track(parent context.Context, input runtimecontract.RunnerInput, done <-chan struct{}, warmExecution bool, confirmedUntil time.Time) {
	defer func() { <-runtime.capacity }()
	execution, cancel := input.BoundExecutionDeadline(parent, runtime.config.ExecutionTimeout)
	defer cancel()
	renew := time.NewTicker(runtime.config.LeaseRenewInterval)
	defer renew.Stop()
	inspect := time.NewTicker(runtime.inspectInterval)
	defer inspect.Stop()
	expiry := time.NewTimer(time.Hour)
	defer expiry.Stop()
	expired := resetLeaseExpiry(expiry, confirmedUntil)
	var terminalObservedAt time.Time
	terminalDiagnostic := ""
	progress, cancelProgress := leaseBoundedRequest(execution, runtime.config.RequestTimeout, confirmedUntil)
	_ = runtime.progress(progress, input, "WORKLOAD_SCHEDULED")
	cancelProgress()
	for {
		select {
		case <-done:
			if warmExecution {
				_ = runtime.reportWarm(parent, input.RuntimeRevisionRef, controlplanev1.AssistantRuntimeState_ASSISTANT_RUNTIME_STATE_READY, "")
			}
			return
		case <-execution.Done():
			if errors.Is(context.Cause(parent), workload.ErrLeadershipLost) {
				runtime.closeRevokedTurn(parent, input, done)
				return
			}
			runtime.drainTurn(parent, input, done, confirmedUntil)
			return
		case <-expired:
			runtime.closeRevokedTurn(parent, input, done)
			return
		case <-renew.C:
			select {
			case <-done:
				return
			default:
			}
			until, err := runtime.renewLease(execution, input, renewalPhaseTrack, confirmedUntil)
			if err != nil {
				if execution.Err() != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || transientControlPlaneFailure(err)) &&
					!errors.Is(context.Cause(parent), workload.ErrLeadershipLost) &&
					(confirmedUntil.IsZero() || time.Now().Before(confirmedUntil)) {
					runtime.drainTurn(parent, input, done, confirmedUntil)
					return
				}
				runtime.closeRevokedTurn(parent, input, done)
				return
			}
			confirmedUntil = until
			expired = resetLeaseExpiry(expiry, confirmedUntil)
		case <-inspect.C:
			request, cancelRequest := leaseBoundedRequest(execution, runtime.config.RequestTimeout, confirmedUntil)
			observation, err := runtime.turns.ObserveTurnPod(request, input, warmExecution)
			cancelRequest()
			if err != nil || !terminalTurnState(observation.State) {
				terminalObservedAt = time.Time{}
				terminalDiagnostic = ""
				continue
			}
			if terminalObservedAt.IsZero() {
				terminalObservedAt = time.Now()
				terminalDiagnostic = observation.DiagnosticCode
				continue
			}
			if time.Since(terminalObservedAt) < runtime.terminalGrace {
				if observation.DiagnosticCode == workload.TurnPodMemoryDiagnostic {
					terminalDiagnostic = observation.DiagnosticCode
				}
				continue
			}
			select {
			case <-done:
				return
			default:
			}
			failureCode := "RUNTIME_WORKLOAD_EXITED"
			if terminalDiagnostic == workload.TurnPodMemoryDiagnostic || observation.DiagnosticCode == workload.TurnPodMemoryDiagnostic {
				failureCode = "RUNTIME_MEMORY_EXHAUSTED"
				terminalDiagnostic = workload.TurnPodMemoryDiagnostic
			}
			runtime.logger.WarnContext(parent, "runtime terminal callback grace expired", "diagnostic_code", terminalDiagnostic)
			runtime.completeFailure(context.WithoutCancel(parent), input, failureCode, terminalDiagnostic)
			return
		}
	}
}

// drainTurn requests graceful stop and keeps callbacks/renewal alive under an independent bounded grace context, joining its watcher before return.
// Callback completion wins without a synthetic receipt; leadership/lease loss uses local revoked-turn cleanup, while grace exhaustion may report the existing timeout failure.
// drainTurn запрашивает мягкую остановку и сохраняет обратные вызовы/продление в независимом ограниченном периоде ожидания, дожидаясь наблюдателя перед выходом.
// Завершение обратного вызова не создаёт искусственной квитанции; потеря лидерства/аренды использует локальную очистку отозванного хода, а исчерпание ожидания может сообщить существующую ошибку таймаута.
func (runtime *runtime) drainTurn(parent context.Context, input runtimecontract.RunnerInput, done <-chan struct{}, confirmedUntil time.Time) {
	select {
	case <-done:
		return
	default:
	}
	base := context.WithoutCancel(parent)
	stop, cancelStop := context.WithTimeout(base, runtime.config.RequestTimeout)
	err := runtime.turns.StopTurn(stop, input.LeaseRef)
	cancelStop()
	if err != nil {
		runtime.logger.WarnContext(base, "runtime graceful stop failed", "error_class", "kubernetes")
	}
	timer := time.NewTimer(runtime.terminalGrace)
	defer timer.Stop()
	renew := time.NewTicker(runtime.config.LeaseRenewInterval)
	defer renew.Stop()
	expiry := time.NewTimer(time.Hour)
	defer expiry.Stop()
	expired := resetLeaseExpiry(expiry, confirmedUntil)
	drain, cancelDrain := context.WithTimeout(base, runtime.terminalGrace)
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		select {
		case <-workload.LeadershipDone(parent):
			cancelDrain()
		case <-done:
			cancelDrain()
		case <-drain.Done():
		}
	}()
	defer func() { cancelDrain(); <-joined }()
	for {
		select {
		case <-workload.LeadershipDone(parent):
			runtime.closeRevokedTurn(base, input, done)
			return
		case <-done:
			return
		case <-expired:
			runtime.closeRevokedTurn(base, input, done)
			return
		case <-timer.C:
			select {
			case <-done:
				return
			default:
			}
			runtime.completeFailure(base, input, "RUNTIME_TIMEOUT", "")
			return
		case <-renew.C:
			select {
			case <-done:
				return
			default:
			}
			until, err := runtime.renewLease(drain, input, renewalPhaseDrain, confirmedUntil)
			if err != nil {
				if errors.Is(renewalContextError(drain), context.DeadlineExceeded) && (errors.Is(err, context.DeadlineExceeded) || transientControlPlaneFailure(err)) &&
					(confirmedUntil.IsZero() || time.Now().Before(confirmedUntil)) {
					runtime.completeFailure(base, input, "RUNTIME_TIMEOUT", "")
					return
				}
				runtime.closeRevokedTurn(base, input, done)
				return
			}
			confirmedUntil = until
			expired = resetLeaseExpiry(expiry, confirmedUntil)
		}
	}
}

// closeRevokedTurn returns immediately for an already completed callback; otherwise it best-effort stops and deletes the turn under one fresh cleanup budget.
// It then completes only the local coordinator even if cleanup failed; no owner completion RPC or successful resource-deletion receipt is manufactured.
// closeRevokedTurn сразу возвращается для уже завершённого обратного вызова; иначе запрашивает остановку и удаление в одном свежем бюджете очистки по возможности.
// Затем завершается только локальный координатор, даже если очистка не удалась; RPC завершения владельца и успешная квитанция удаления ресурсов не выдумываются.
func (runtime *runtime) closeRevokedTurn(base context.Context, input runtimecontract.RunnerInput, done <-chan struct{}) {
	select {
	case <-done:
		return
	default:
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(base), runtime.config.RequestTimeout)
	defer cancel()
	// An authority denial also stops the warm Pod; a successful DeleteTurn retains it.
	// Отказ authority останавливает также warm Pod; успешный DeleteTurn его сохраняет.
	_ = runtime.turns.StopTurn(cleanup, input.LeaseRef)
	_ = runtime.turns.DeleteTurn(cleanup, input.LeaseRef)
	runtime.coordinator.Complete(input.LeaseRef)
}

// progress sends a lease/fence/generation-bound progress code with the existing i18n prefix under a request timeout.
// It returns the RPC error and does not retry or finish the execution.
// progress передаёт код прогресса с привязками аренды/ограждения/поколения и существующим префиксом i18n в таймауте запроса.
// Ошибка RPC возвращается без повтора или завершения выполнения.
func (runtime *runtime) progress(ctx context.Context, input runtimecontract.RunnerInput, code string) error {
	request, cancel := context.WithTimeout(ctx, runtime.config.RequestTimeout)
	defer cancel()
	_, err := runtime.control.ReportExecutionProgress(request, &controlplanev1.ReportExecutionProgressRequest{LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration, Progress: "i18n:" + code})
	return err
}

// completeFailure builds one stable-idempotency failure request and retries only transient delivery errors within the fixed attempt/delay limits.
// Success or AlreadyExists follows the existing delete/coordinator-completion path; PermissionDenied requests revoked-turn cleanup, while other unresolved failures keep resources and log a closed diagnostic.
// Each RPC has an independent budget, but retry waits still honor the supplied base context; cleanup is best effort rather than proof of owner commit or exactly-once delivery.
// completeFailure строит один запрос ошибки со стабильной идемпотентностью и повторяет только временные ошибки доставки в фиксированных пределах попыток/задержек.
// Успех или AlreadyExists ведут по существующему пути удаления/завершения координатора; PermissionDenied запрашивает очистку отозванного хода, а прочие неразрешённые ошибки сохраняют ресурсы и фиксируют закрытую диагностику.
// Каждый RPC имеет независимый бюджет, но ожидание повторов соблюдает переданный базовый контекст; очистка выполняется по возможности и не доказывает фиксацию владельцем или однократную доставку.
func (runtime *runtime) completeFailure(base context.Context, input runtimecontract.RunnerInput, code, diagnosticCode string) {
	safeCode := safeRuntimeErrorCode(code)
	if input.ExecutionDeadline != nil && code == "RUNTIME_TIMEOUT" {
		safeCode = code
	}
	summary := "i18n:" + code
	if code == "RUNTIME_REVISION_INVALID" {
		summary = "i18n:" + safeCode
	}
	request := &controlplanev1.CompleteExecutionRequest{Mutation: &controlplanev1.MutationContext{IdempotencyKey: stableIdempotency(input.LeaseRef, "failure:"+code)}, LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration, Success: false, ResultSummary: summary, SafeErrorCode: safeCode}
	var err error
	for attempt := 0; attempt < failureCompletionMaximumTries; attempt++ {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(base), runtime.config.RequestTimeout)
		_, err = runtime.control.CompleteExecution(ctx, request)
		cancel()
		if err == nil || status.Code(err) == codes.AlreadyExists {
			err = nil
			break
		}
		if !transientControlPlaneFailure(err) || attempt >= len(runtime.completionRetries) {
			break
		}
		if !waitWithoutCancel(base, runtime.completionRetries[attempt]) {
			break
		}
	}
	if err != nil {
		if status.Code(err) == codes.PermissionDenied {
			// The exact-lease operation was denied; cancel/interrupt can revoke it without a deadline.
			// This denial does not permit retaining the previous Pod and its sidecars.
			// В операции с точной арендой отказано; отмена/прерывание могут отозвать её без срока.
			// Такой отказ не разрешает сохранять прежний Pod и его sidecars.
			runtime.closeRevokedTurn(base, input, nil)
			return
		}
		runtime.logger.ErrorContext(base, "complete failed runtime execution failed", "error_class", "control_plane", "diagnostic_code", diagnosticCode)
		return
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(base), runtime.config.RequestTimeout)
	defer cancel()
	if err := runtime.turns.DeleteTurn(cleanup, input.LeaseRef); err != nil {
		runtime.logger.ErrorContext(cleanup, "failed runtime resource cleanup failed", "error_class", "kubernetes", "diagnostic_code", diagnosticCode)
	}
	runtime.coordinator.Complete(input.LeaseRef)
}

// failClaim fills missing lease identity from the owner's claimed execution only when the built input has no lease reference.
// If a reference is available it delegates failure delivery; it neither invents a claim nor independently validates ownership.
// failClaim восполняет отсутствующую идентичность аренды из заявленного выполнения владельца только при отсутствии ссылки аренды в построенном входе.
// При наличии ссылки передаётся доставка ошибки; заявка не выдумывается, владение отдельно не проверяется.
func (runtime *runtime) failClaim(ctx context.Context, input runtimecontract.RunnerInput, execution *controlplanev1.ClaimedExecution, code string) {
	if input.LeaseRef == "" && execution != nil && execution.GetLease() != nil {
		input.LeaseRef, input.LeaseFence, input.LeaseGeneration = execution.GetLease().GetRef(), execution.GetLease().GetFence(), execution.GetLease().GetGeneration()
	}
	if input.LeaseRef != "" {
		runtime.completeFailure(ctx, input, code, "")
	}
}

// terminalTurnState recognizes exactly FAILED, SUCCEEDED, MISSING and CONFLICT as terminal workload observations.
// This is local Pod-observation classification, not authoritative owner execution status.
// terminalTurnState распознаёт только FAILED, SUCCEEDED, MISSING и CONFLICT как завершающие наблюдения нагрузки.
// Это локальная классификация наблюдения Pod, а не авторитетный статус выполнения у владельца.
func terminalTurnState(state string) bool {
	return state == "FAILED" || state == "SUCCEEDED" || state == "MISSING" || state == "CONFLICT"
}

// transientControlPlaneFailure permits only Unavailable and DeadlineExceeded gRPC codes for the existing bounded delivery retry policy.
// It does not itself authorize retry after lease expiry or an owner denial.
// transientControlPlaneFailure допускает только коды gRPC Unavailable и DeadlineExceeded для существующей ограниченной политики повторной доставки.
// Сам предикат не разрешает повтор после истечения аренды или отказа владельца.
func transientControlPlaneFailure(err error) bool {
	return status.Code(err) == codes.Unavailable || status.Code(err) == codes.DeadlineExceeded
}

// waitWithoutCancel waits for the delay or the supplied context's cancellation and returns whether the timer won.
// Despite its name it does not detach cancellation, create a root context or retry an operation itself.
// waitWithoutCancel ожидает задержку либо отмену переданного контекста и возвращает признак победы таймера.
// Несмотря на имя, отмена не отделяется, корневой контекст не создаётся и операция самостоятельно не повторяется.
func waitWithoutCancel(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// stableIdempotency derives a deterministic version-5 UUID in the OID namespace from the NUL-separated inputs.
// It does not validate those inputs or serve as an authorization token, signature or collision-free arbitrary-string encoding.
// stableIdempotency получает детерминированный UUID версии 5 в пространстве OID из входов, разделённых NUL.
// Входы не проверяются; результат не является токеном полномочий, подписью или бесколлизионным кодированием произвольных строк.
func stableIdempotency(left, right string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(left+"\x00"+right)).String()
}

// safeRuntimeErrorCode maps invalid revisions and stale configuration to their public input/profile codes, with a generic unavailable fallback.
// The mapper does not forward arbitrary error text or determine owner execution state.
// safeRuntimeErrorCode переводит неверную ревизию и устаревшую конфигурацию в публичные коды входа/профиля, иначе используя общий код недоступности.
// Произвольный текст ошибки не передаётся, состояние выполнения у владельца не определяется.
func safeRuntimeErrorCode(code string) string {
	if code == "RUNTIME_REVISION_INVALID" {
		return "RUNTIME_INPUT_INVALID"
	}
	if code == "RUNTIME_CONFIGURATION_STALE" {
		return "RUNTIME_PROFILE_UNSUPPORTED"
	}
	return "RUNTIME_UNAVAILABLE"
}
