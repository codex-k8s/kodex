package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/libs/go/controlplaneclient"
	"github.com/codex-k8s/matter-codex/libs/go/serviceruntime"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/provider"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

type runtime struct {
	control   *controlplaneclient.Client
	provider  *provider.Responses
	config    Config
	assistant *serviceruntime.Readiness
	logger    *slog.Logger
	revision  string
}

func newRuntime(control *controlplaneclient.Client, model *provider.Responses, config Config, assistant *serviceruntime.Readiness, logger *slog.Logger) *runtime {
	assistant.Set(false, "assistant_runtime_starting")
	return &runtime{control: control, provider: model, config: config, assistant: assistant, logger: logger}
}

func (runtime *runtime) Run(ctx context.Context) error {
	if err := runtime.reconcile(ctx); err != nil {
		runtime.setAssistantUnavailable(ctx)
	}
	heartbeat := time.NewTicker(runtime.config.HeartbeatInterval)
	poll := time.NewTicker(runtime.config.PollInterval)
	defer heartbeat.Stop()
	defer poll.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-heartbeat.C:
			if err := runtime.reconcile(ctx); err != nil {
				runtime.setAssistantUnavailable(ctx)
			}
		case <-poll.C:
			ready, _ := runtime.assistant.Ready()
			if !ready {
				continue
			}
			if err := runtime.claimAndExecute(ctx); err != nil && !errors.Is(err, context.Canceled) {
				runtime.setAssistantUnavailable(ctx)
			}
		}
	}
}

func (runtime *runtime) reconcile(ctx context.Context) error {
	requestContext, cancel := context.WithTimeout(ctx, runtime.config.RequestTimeout)
	defer cancel()
	response, err := runtime.control.Runtime.ReconcileWarmRuntime(requestContext, &controlplanev1.ReconcileWarmRuntimeRequest{WorkloadInstance: runtime.config.WorkloadInstance})
	if err != nil || response.GetDesiredRevision() == nil || response.GetDesiredRevision().GetRuntime() == nil {
		return errors.New("reconcile warm runtime")
	}
	revision := response.GetDesiredRevision()
	if err := runtime.provider.Check(requestContext, revision.GetRuntime().GetProvider(), revision.GetRuntime().GetModel()); err != nil {
		_ = runtime.reportWarm(ctx, revision.GetRef(), controlplanev1.AssistantRuntimeState_ASSISTANT_RUNTIME_STATE_FAILED, safeCode(err))
		return errors.New("check warm provider runtime")
	}
	runtime.revision = revision.GetRef()
	if err := runtime.reportWarm(ctx, runtime.revision, controlplanev1.AssistantRuntimeState_ASSISTANT_RUNTIME_STATE_READY, ""); err != nil {
		return err
	}
	if runtime.assistant.Set(true, "ready") {
		runtime.logger.InfoContext(ctx, "warm runtime availability restored")
	}
	return nil
}

func (runtime *runtime) setAssistantUnavailable(ctx context.Context) {
	if runtime.assistant.Set(false, "assistant_runtime_unavailable") {
		runtime.logger.WarnContext(ctx, "warm runtime availability lost", "error_class", "dependency")
	}
}

func (runtime *runtime) reportWarm(ctx context.Context, revision string, state controlplanev1.AssistantRuntimeState, code string) error {
	requestContext, cancel := context.WithTimeout(ctx, runtime.config.RequestTimeout)
	defer cancel()
	_, err := runtime.control.Runtime.ReportWarmRuntime(requestContext, &controlplanev1.ReportWarmRuntimeRequest{
		Mutation: &controlplanev1.MutationContext{IdempotencyKey: uuid.NewString()}, WorkloadInstance: runtime.config.WorkloadInstance,
		RuntimeRevision: revision, State: state, SafeErrorCode: code,
	})
	return err
}

func (runtime *runtime) claimAndExecute(ctx context.Context) error {
	requestContext, cancel := context.WithTimeout(ctx, runtime.config.RequestTimeout)
	response, err := runtime.control.Runtime.ClaimExecution(requestContext, &controlplanev1.ClaimExecutionRequest{WorkloadInstance: runtime.config.WorkloadInstance, Limit: 1})
	cancel()
	if err != nil {
		return err
	}
	for _, execution := range response.GetExecutions() {
		if execution.GetRevision().GetSystemAssistant() {
			_ = runtime.reportWarm(ctx, runtime.revision, controlplanev1.AssistantRuntimeState_ASSISTANT_RUNTIME_STATE_BUSY, "")
		}
		runtime.execute(ctx, execution)
		if execution.GetRevision().GetSystemAssistant() {
			_ = runtime.reportWarm(ctx, runtime.revision, controlplanev1.AssistantRuntimeState_ASSISTANT_RUNTIME_STATE_READY, "")
		}
	}
	return nil
}

func (runtime *runtime) execute(ctx context.Context, execution *controlplanev1.ClaimedExecution) {
	lease := execution.GetLease()
	revision := execution.GetRevision()
	if lease == nil || revision == nil || revision.GetRuntime() == nil {
		return
	}
	executionContext, cancel := context.WithTimeout(ctx, runtime.config.ExecutionTimeout)
	renewDone := make(chan error, 1)
	go runtime.renewLease(executionContext, cancel, lease, renewDone)
	defer func() {
		cancel()
		<-renewDone
	}()
	_ = runtime.progress(executionContext, lease, "MODEL_REQUEST_RUNNING")
	input := map[string]any{}
	if revision.GetBoundedInput() != nil {
		input = revision.GetBoundedInput().AsMap()
	}
	inputJSON, _ := json.Marshal(input)
	providerInput := provider.Request{
		IdempotencyKey: lease.GetRef() + "-" + execution.GetRun().GetRef(), Model: revision.GetRuntime().GetModel(), Instructions: revision.GetInstructions(), Task: execution.GetTask(), InputJSON: inputJSON,
		AllowDelegation: containsCapability(revision, "platform.run.delegate"),
	}
	for _, message := range revision.GetSessionContext() {
		providerInput.SessionContext = append(providerInput.SessionContext, provider.Message{Role: message.GetRole(), Content: message.GetContent()})
	}
	for _, target := range revision.GetDelegationTargets() {
		providerInput.DelegationTargets = append(providerInput.DelegationTargets, provider.DelegationTarget{Ref: target.GetRef(), Name: target.GetName(), Purpose: target.GetPurpose(), RoleDescription: target.GetRoleDescription()})
	}
	result, err := runtime.provider.Execute(executionContext, providerInput, func(toolContext context.Context, call provider.ToolCall) (string, error) {
		return runtime.delegate(toolContext, execution, call)
	})
	if err != nil {
		runtime.complete(executionContext, execution, false, "", safeCode(err), nil)
		return
	}
	body := []byte(result.Text)
	digest := sha256.Sum256(body)
	artifact := &controlplanev1.CompletedArtifactInput{FileName: "result.md", MediaType: "text/markdown", SizeBytes: int64(len(body)), Content: body, Sha256: hex.EncodeToString(digest[:])}
	runtime.complete(executionContext, execution, true, result.Text, "", []*controlplanev1.CompletedArtifactInput{artifact})
}

func (runtime *runtime) renewLease(ctx context.Context, cancelExecution context.CancelFunc, lease *controlplanev1.WorkLease, done chan<- error) {
	ticker := time.NewTicker(runtime.config.LeaseRenewInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			done <- nil
			return
		case <-ticker.C:
			requestContext, cancel := context.WithTimeout(ctx, runtime.config.RequestTimeout)
			response, err := runtime.control.Runtime.RenewExecution(requestContext, &controlplanev1.RenewExecutionRequest{LeaseRef: lease.GetRef(), Fence: lease.GetFence(), Generation: lease.GetGeneration()})
			cancel()
			if err != nil || response.GetLease() == nil {
				cancelExecution()
				done <- errors.New("renew execution lease")
				return
			}
		}
	}
}

func (runtime *runtime) progress(ctx context.Context, lease *controlplanev1.WorkLease, progress string) error {
	requestContext, cancel := context.WithTimeout(ctx, runtime.config.RequestTimeout)
	defer cancel()
	_, err := runtime.control.Runtime.ReportExecutionProgress(requestContext, &controlplanev1.ReportExecutionProgressRequest{LeaseRef: lease.GetRef(), Fence: lease.GetFence(), Generation: lease.GetGeneration(), Progress: progress})
	return err
}

func (runtime *runtime) delegate(ctx context.Context, execution *controlplanev1.ClaimedExecution, call provider.ToolCall) (string, error) {
	allowed := false
	for _, target := range execution.GetRevision().GetDelegationTargets() {
		if target.GetRef() == call.TargetAgentRef {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", errors.New("delegation target is not allowed")
	}
	requestContext, cancel := context.WithTimeout(ctx, runtime.config.RequestTimeout)
	defer cancel()
	response, err := runtime.control.Runtime.DelegateExecution(requestContext, &controlplanev1.DelegateExecutionRequest{
		Mutation: &controlplanev1.MutationContext{IdempotencyKey: stableIdempotencyKey(execution.GetLease().GetRef(), call.CallID)},
		LeaseRef: execution.GetLease().GetRef(), Fence: execution.GetLease().GetFence(), Generation: execution.GetLease().GetGeneration(), TargetAgentRef: call.TargetAgentRef, Task: call.Task, Input: &structpb.Struct{},
	})
	if err != nil {
		return "", err
	}
	payload := struct {
		OK              bool   `json:"ok"`
		ChildRunRef     string `json:"childRunRef"`
		CallbackEdgeRef string `json:"callbackEdgeRef"`
	}{OK: true, ChildRunRef: response.GetChildRun().GetRef(), CallbackEdgeRef: response.GetCallbackEdgeRef()}
	raw, _ := json.Marshal(payload)
	return string(raw), nil
}

func (runtime *runtime) complete(ctx context.Context, execution *controlplanev1.ClaimedExecution, success bool, result, code string, artifacts []*controlplanev1.CompletedArtifactInput) {
	requestContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), runtime.config.RequestTimeout)
	defer cancel()
	_, err := runtime.control.Runtime.CompleteExecution(requestContext, &controlplanev1.CompleteExecutionRequest{
		Mutation: &controlplanev1.MutationContext{IdempotencyKey: stableIdempotencyKey(execution.GetLease().GetRef(), "complete")},
		LeaseRef: execution.GetLease().GetRef(), Fence: execution.GetLease().GetFence(), Generation: execution.GetLease().GetGeneration(), Success: success, ResultSummary: bounded(result, 4000), SafeErrorCode: code, Artifacts: artifacts,
	})
	if err != nil && status.Code(err) != codes.AlreadyExists {
		runtime.logger.ErrorContext(ctx, "complete runtime execution failed", "error_class", "control_plane")
	}
}

func safeCode(err error) string {
	var safe *provider.SafeError
	if errors.As(err, &safe) {
		return safe.Code
	}
	return "RUNTIME_UNAVAILABLE"
}

func containsCapability(revision *controlplanev1.RuntimeRevisionSnapshot, key string) bool {
	for _, capability := range revision.GetCapabilities() {
		if capability.GetKey() == key {
			return true
		}
	}
	return false
}

func stableIdempotencyKey(left, right string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(left+"\x00"+right)).String()
}

func bounded(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return strings.TrimSpace(value[:limit])
}
