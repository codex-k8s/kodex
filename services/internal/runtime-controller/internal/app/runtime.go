package app

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"github.com/codex-k8s/kodex/services/internal/runtime-controller/internal/callback"
	"github.com/codex-k8s/kodex/services/internal/runtime-controller/internal/workload"
	"github.com/google/uuid"
)

type runtime struct {
	control           *controlplaneclient.Client
	manager           *workload.Manager
	coordinator       *callback.Coordinator
	config            Config
	assistant         *serviceruntime.Readiness
	logger            *slog.Logger
	capacity          chan struct{}
	warmMu            sync.RWMutex
	warmCompatibility string
	warmTicket        string
}

func newRuntime(control *controlplaneclient.Client, manager *workload.Manager, coordinator *callback.Coordinator, config Config, assistant *serviceruntime.Readiness, logger *slog.Logger) *runtime {
	return &runtime{control: control, manager: manager, coordinator: coordinator, config: config, assistant: assistant, logger: logger, capacity: make(chan struct{}, config.MaximumConcurrentTurns)}
}

func (runtime *runtime) Run(ctx context.Context) error {
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
	response, err := runtime.control.Runtime.ReconcileWarmRuntime(request, &controlplanev1.ReconcileWarmRuntimeRequest{WorkloadInstance: runtime.config.PodUID})
	if err != nil || response.GetDesiredRevision() == nil {
		return errors.New("reconcile system assistant warm runtime")
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
	_, err := runtime.control.Runtime.ReportWarmRuntime(request, &controlplanev1.ReportWarmRuntimeRequest{WorkloadInstance: runtime.config.PodUID, RuntimeRevision: revision, State: state, SafeErrorCode: code})
	return err
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
	response, err := runtime.control.Runtime.ClaimExecution(request, &controlplanev1.ClaimExecutionRequest{WorkloadInstance: runtime.config.PodUID, Limit: int32(limit)})
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
			if compatibilityErr == nil && warmCompatibility == turnCompatibility && warmTicket != "" {
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
			if err := runtime.manager.EnsureTurn(ctx, input, providerBinding); err != nil {
				runtime.logger.WarnContext(ctx, "runtime turn materialization failed", "error_class", "kubernetes", "reason", err.Error())
				<-runtime.capacity
				runtime.failClaim(ctx, input, execution, "RUNTIME_MATERIALIZATION_FAILED")
				continue
			}
		}
		go runtime.track(ctx, input, done, warmExecution)
	}
	return len(response.GetExecutions()), nil
}

func (runtime *runtime) track(parent context.Context, input runtimecontract.RunnerInput, done <-chan struct{}, warmExecution bool) {
	defer func() { <-runtime.capacity }()
	execution, cancel := context.WithTimeout(parent, runtime.config.ExecutionTimeout)
	defer cancel()
	renew := time.NewTicker(runtime.config.LeaseRenewInterval)
	defer renew.Stop()
	inspect := time.NewTicker(2 * time.Second)
	defer inspect.Stop()
	_ = runtime.progress(execution, input, "WORKLOAD_SCHEDULED")
	for {
		select {
		case <-done:
			if warmExecution {
				_ = runtime.reportWarm(parent, input.RuntimeRevisionRef, controlplanev1.AssistantRuntimeState_ASSISTANT_RUNTIME_STATE_READY, "")
			}
			return
		case <-execution.Done():
			runtime.completeFailure(context.WithoutCancel(parent), input, "RUNTIME_TIMEOUT")
			return
		case <-renew.C:
			request, cancelRequest := context.WithTimeout(execution, runtime.config.RequestTimeout)
			_, err := runtime.control.Runtime.RenewExecution(request, &controlplanev1.RenewExecutionRequest{LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration})
			cancelRequest()
			if err != nil {
				_ = runtime.manager.DeleteTurn(context.WithoutCancel(parent), input.LeaseRef)
				runtime.coordinator.Complete(input.LeaseRef)
				return
			}
		case <-inspect.C:
			request, cancelRequest := context.WithTimeout(execution, runtime.config.RequestTimeout)
			state, err := runtime.manager.TurnPodState(request, input, warmExecution)
			cancelRequest()
			if err == nil && (state == "FAILED" || state == "SUCCEEDED" || state == "MISSING" || state == "CONFLICT") {
				runtime.completeFailure(context.WithoutCancel(parent), input, "RUNTIME_WORKLOAD_EXITED")
				return
			}
		}
	}
}

func (runtime *runtime) progress(ctx context.Context, input runtimecontract.RunnerInput, code string) error {
	request, cancel := context.WithTimeout(ctx, runtime.config.RequestTimeout)
	defer cancel()
	_, err := runtime.control.Runtime.ReportExecutionProgress(request, &controlplanev1.ReportExecutionProgressRequest{LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration, Progress: "i18n:" + code})
	return err
}

func (runtime *runtime) completeFailure(base context.Context, input runtimecontract.RunnerInput, code string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(base), runtime.config.RequestTimeout)
	defer cancel()
	_, err := runtime.control.Runtime.CompleteExecution(ctx, &controlplanev1.CompleteExecutionRequest{Mutation: &controlplanev1.MutationContext{IdempotencyKey: stableIdempotency(input.LeaseRef, "failure:"+code)}, LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration, Success: false, ResultSummary: "i18n:" + code, SafeErrorCode: safeRuntimeErrorCode(code)})
	if err != nil {
		runtime.logger.ErrorContext(ctx, "complete failed runtime execution failed", "error_class", "control_plane")
	}
	_ = runtime.manager.DeleteTurn(ctx, input.LeaseRef)
	runtime.coordinator.Complete(input.LeaseRef)
}

func (runtime *runtime) failClaim(ctx context.Context, input runtimecontract.RunnerInput, execution *controlplanev1.ClaimedExecution, code string) {
	if input.LeaseRef == "" && execution != nil && execution.GetLease() != nil {
		input.LeaseRef, input.LeaseFence, input.LeaseGeneration = execution.GetLease().GetRef(), execution.GetLease().GetFence(), execution.GetLease().GetGeneration()
	}
	if input.LeaseRef != "" {
		runtime.completeFailure(ctx, input, code)
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
