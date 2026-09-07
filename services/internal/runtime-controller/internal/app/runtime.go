package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	defaultTurnInspectionInterval = 2 * time.Second
	defaultTerminalCallbackGrace  = 120 * time.Second
	failureCompletionMaximumTries = 5
	warmReconcileRPCFailure       = "reconcile system assistant warm runtime"
	warmReportRPCFailure          = "report system assistant warm runtime"
	warmDesiredRevisionMissing    = "system assistant warm runtime desired revision is missing"
)

var defaultFailureCompletionRetryDelays = [...]time.Duration{
	500 * time.Millisecond,
	1500 * time.Millisecond,
	3 * time.Second,
	6 * time.Second,
}

type turnLifecycle interface {
	ObserveTurnPod(context.Context, runtimecontract.RunnerInput, bool) (workload.TurnPodObservation, error)
	StopTurn(context.Context, string) error
	DeleteTurn(context.Context, string) error
}

type credentialMaterializer interface {
	Materialize(context.Context, runtimecontract.RunnerInput) (credentialprojection.Projection, error)
}

type runtime struct {
	control           controlplanev1.RuntimeWorkServiceClient
	credentials       credentialMaterializer
	manager           *workload.Manager
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

func newRuntime(control controlplanev1.RuntimeWorkServiceClient, credentials credentialMaterializer, manager *workload.Manager, coordinator *callback.Coordinator, config Config, assistant *serviceruntime.Readiness, logger *slog.Logger) *runtime {
	return &runtime{control: control, credentials: credentials, manager: manager, turns: manager, coordinator: coordinator, config: config, assistant: assistant, logger: logger,
		capacity: make(chan struct{}, config.MaximumConcurrentTurns), inspectInterval: defaultTurnInspectionInterval,
		terminalGrace: defaultTerminalCallbackGrace, completionRetries: defaultFailureCompletionRetryDelays[:]}
}

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
				runtime.logger.WarnContext(ctx, "runtime claim delivery degraded", "error_class", "control_plane")
			} else if err == nil && claimDegraded {
				claimDegraded = false
				runtime.logger.InfoContext(ctx, "runtime claim delivery restored")
			}
			poll.Reset(idleBackoff.Next(err == nil && claimed > 0))
		}
	}
}

func (runtime *runtime) reconcileWarm(ctx context.Context) error {
	request, cancel := context.WithTimeout(ctx, runtime.config.RequestTimeout)
	defer cancel()
	response, err := runtime.control.ReconcileWarmRuntime(request, &controlplanev1.ReconcileWarmRuntimeRequest{WorkloadInstance: runtime.config.PodUID})
	if err != nil {
		return warmRPCFailure(warmReconcileRPCFailure, err)
	}
	if response.GetDesiredRevision() == nil {
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

func (runtime *runtime) reportWarm(ctx context.Context, revision string, state controlplanev1.AssistantRuntimeState, code string) error {
	request, cancel := context.WithTimeout(ctx, runtime.config.RequestTimeout)
	defer cancel()
	_, err := runtime.control.ReportWarmRuntime(request, &controlplanev1.ReportWarmRuntimeRequest{WorkloadInstance: runtime.config.PodUID, RuntimeRevision: revision, State: state, SafeErrorCode: code})
	if err != nil {
		return warmRPCFailure(warmReportRPCFailure, err)
	}
	return nil
}

// В logger передаются только закрытые классы, без исходной цепочки ошибок.
func warmRPCFailure(operation string, cause error) error {
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

func (runtime *runtime) setAssistantUnavailable(ctx context.Context) {
	if runtime.assistant.Set(false, "assistant_runtime_unavailable") {
		runtime.logger.WarnContext(ctx, "system assistant warm runtime lost", "error_class", "dependency")
	}
}

func (runtime *runtime) claim(ctx context.Context) (int, error) {
	limit := cap(runtime.capacity) - len(runtime.capacity)
	if limit > 8 {
		limit = 8
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
	for _, execution := range response.GetExecutions() {
		input, providerBinding, buildErr := runtime.manager.BuildTurnInput(execution)
		if buildErr != nil {
			runtime.failClaim(ctx, input, execution, "RUNTIME_REVISION_INVALID")
			continue
		}
		runtime.capacity <- struct{}{}
		done := runtime.coordinator.Register(input)
		warmExecution := false
		if input.SystemAssistant {
			runtime.warmMu.RLock()
			warmCompatibility, warmTicket := runtime.warmCompatibility, runtime.warmTicket
			runtime.warmMu.RUnlock()
			turnCompatibility, compatibilityErr := runtimecontract.WarmCompatibilityDigest(input)
			if compatibilityErr == nil && warmCompatibility == turnCompatibility && warmTicket != "" && warmFileProjectionEligible(input) {
				if err := runtime.manager.RegisterWarmTurn(ctx, input, warmTicket); err != nil || runtime.coordinator.EnqueueWarm(input, turnCompatibility) != nil {
					<-runtime.capacity
					runtime.failClaim(ctx, input, execution, "SYSTEM_ASSISTANT_DISPATCH_FAILED")
					continue
				}
				warmExecution = true
				_ = runtime.reportWarm(ctx, input.RuntimeRevisionRef, controlplanev1.AssistantRuntimeState_ASSISTANT_RUNTIME_STATE_BUSY, "")
			}
		}
		if !warmExecution {
			projectionContext, cancelProjection := context.WithTimeout(ctx, runtime.config.RequestTimeout)
			projection, projectionErr := runtime.credentials.Materialize(projectionContext, input)
			cancelProjection()
			if projectionErr == nil {
				projectionErr = runtime.manager.EnsureTurn(ctx, input, providerBinding, workload.CredentialProjection{
					Namespace: projection.Namespace, SecretName: projection.SecretName, SecretUID: projection.SecretUID,
					SecretResourceVersion: projection.SecretResourceVersion, ContentSHA256: projection.ContentSHA256,
					ProviderAuthKey: projection.ProviderAuthKey, RuntimeSecretKeys: projection.RuntimeSecretKeys,
				})
			}
			if projectionErr != nil {
				runtime.logger.WarnContext(ctx, "runtime turn materialization failed", "error_class", "dependency")
				<-runtime.capacity
				runtime.failClaim(ctx, input, execution, "RUNTIME_MATERIALIZATION_FAILED")
				continue
			}
		}
		runtime.trackers.Add(1)
		go func() {
			defer runtime.trackers.Done()
			runtime.track(ctx, input, done, warmExecution)
		}()
	}
	return len(response.GetExecutions()), nil
}

func warmFileProjectionEligible(input runtimecontract.RunnerInput) bool {
	return len(input.AttachmentSets) == 0 && len(input.InputArtifacts) == 0
}

func (runtime *runtime) track(parent context.Context, input runtimecontract.RunnerInput, done <-chan struct{}, warmExecution bool) {
	defer func() { <-runtime.capacity }()
	execution, cancel := context.WithTimeout(parent, runtime.config.ExecutionTimeout)
	defer cancel()
	renew := time.NewTicker(runtime.config.LeaseRenewInterval)
	defer renew.Stop()
	inspect := time.NewTicker(runtime.inspectInterval)
	defer inspect.Stop()
	var terminalObservedAt time.Time
	terminalDiagnostic := ""
	_ = runtime.progress(execution, input, "WORKLOAD_SCHEDULED")
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
			runtime.drainTurn(parent, input, done)
			return
		case <-renew.C:
			request, cancelRequest := context.WithTimeout(execution, runtime.config.RequestTimeout)
			_, err := runtime.control.RenewExecution(request, &controlplanev1.RenewExecutionRequest{LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration})
			cancelRequest()
			if err != nil {
				runtime.closeRevokedTurn(parent, input, done)
				return
			}
		case <-inspect.C:
			request, cancelRequest := context.WithTimeout(execution, runtime.config.RequestTimeout)
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
				continue
			}
			select {
			case <-done:
				return
			default:
			}
			runtime.logger.WarnContext(parent, "runtime terminal callback grace expired", "diagnostic_code", terminalDiagnostic)
			runtime.completeFailure(context.WithoutCancel(parent), input, "RUNTIME_WORKLOAD_EXITED", terminalDiagnostic)
			return
		}
	}
}

func (runtime *runtime) drainTurn(parent context.Context, input runtimecontract.RunnerInput, done <-chan struct{}) {
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
	for {
		select {
		case <-workload.LeadershipDone(parent):
			runtime.closeRevokedTurn(base, input, done)
			return
		case <-done:
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
			request, cancel := context.WithTimeout(base, runtime.config.RequestTimeout)
			_, err := runtime.control.RenewExecution(request, &controlplanev1.RenewExecutionRequest{LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration})
			cancel()
			if err != nil {
				runtime.closeRevokedTurn(base, input, done)
				return
			}
		}
	}
}

func (runtime *runtime) closeRevokedTurn(base context.Context, input runtimecontract.RunnerInput, done <-chan struct{}) {
	select {
	case <-done:
		return
	default:
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(base), runtime.config.RequestTimeout)
	defer cancel()
	// Отказ authority останавливает также warm Pod; успешный DeleteTurn его сохраняет.
	_ = runtime.turns.StopTurn(cleanup, input.LeaseRef)
	_ = runtime.turns.DeleteTurn(cleanup, input.LeaseRef)
	runtime.coordinator.Complete(input.LeaseRef)
}

func (runtime *runtime) progress(ctx context.Context, input runtimecontract.RunnerInput, code string) error {
	request, cancel := context.WithTimeout(ctx, runtime.config.RequestTimeout)
	defer cancel()
	_, err := runtime.control.ReportExecutionProgress(request, &controlplanev1.ReportExecutionProgressRequest{LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration, Progress: "i18n:" + code})
	return err
}

func (runtime *runtime) completeFailure(base context.Context, input runtimecontract.RunnerInput, code, diagnosticCode string) {
	request := &controlplanev1.CompleteExecutionRequest{Mutation: &controlplanev1.MutationContext{IdempotencyKey: stableIdempotency(input.LeaseRef, "failure:"+code)}, LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration, Success: false, ResultSummary: "i18n:" + code, SafeErrorCode: safeRuntimeErrorCode(code)}
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

func (runtime *runtime) failClaim(ctx context.Context, input runtimecontract.RunnerInput, execution *controlplanev1.ClaimedExecution, code string) {
	if input.LeaseRef == "" && execution != nil && execution.GetLease() != nil {
		input.LeaseRef, input.LeaseFence, input.LeaseGeneration = execution.GetLease().GetRef(), execution.GetLease().GetFence(), execution.GetLease().GetGeneration()
	}
	if input.LeaseRef != "" {
		runtime.completeFailure(ctx, input, code, "")
	}
}

func terminalTurnState(state string) bool {
	return state == "FAILED" || state == "SUCCEEDED" || state == "MISSING" || state == "CONFLICT"
}

func transientControlPlaneFailure(err error) bool {
	return status.Code(err) == codes.Unavailable || status.Code(err) == codes.DeadlineExceeded
}

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

func stableIdempotency(left, right string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(left+"\x00"+right)).String()
}

func safeRuntimeErrorCode(code string) string {
	if code == "RUNTIME_REVISION_INVALID" || code == "RUNTIME_CONFIGURATION_STALE" {
		return "RUNTIME_PROFILE_UNSUPPORTED"
	}
	return "RUNTIME_UNAVAILABLE"
}
