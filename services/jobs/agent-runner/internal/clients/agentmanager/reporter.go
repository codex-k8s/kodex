package agentmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	agentsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/agents/v1"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/app"
)

const (
	callerID            = "agent-runner"
	defaultCallTimeout  = 3 * time.Second
	sourceAgentRunner   = "agent-runner"
	actorTypeService    = "service"
	startedReasonCode   = "agent_runner_started"
	startedSummary      = "agent-runner accepted runtime context"
	maxSafeDetailsBytes = 4096
)

type Client interface {
	runtimeStatusReader
	runStateRecorder
	activityRecorder
}

type runtimeStatusReader interface {
	GetAgentRunRuntimeStatus(context.Context, *agentsv1.GetAgentRunRuntimeStatusRequest, ...grpc.CallOption) (*agentsv1.AgentRunRuntimeStatusResponse, error)
}

type runStateRecorder interface {
	RecordRunState(context.Context, *agentsv1.RecordRunStateRequest, ...grpc.CallOption) (*agentsv1.AgentRunResponse, error)
}

type activityRecorder interface {
	RecordAgentActivity(context.Context, *agentsv1.RecordAgentActivityRequest, ...grpc.CallOption) (*agentsv1.AgentActivityResponse, error)
}

type Reporter struct {
	client    Client
	authToken string
	timeout   time.Duration
}

func NewConnection(cfg app.ReporterConfig) (*grpc.ClientConn, error) {
	addr := strings.TrimSpace(cfg.GRPCAddr)
	if addr == "" {
		return nil, fmt.Errorf("agent-manager gRPC address is required")
	}
	return grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
}

func NewReporter(client Client, cfg app.ReporterConfig) (*Reporter, error) {
	if client == nil {
		return nil, fmt.Errorf("agent-manager client is required")
	}
	authToken := strings.TrimSpace(cfg.AuthToken)
	if authToken == "" {
		return nil, fmt.Errorf("agent-manager auth token is required")
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultCallTimeout
	}
	return &Reporter{client: client, authToken: authToken, timeout: timeout}, nil
}

func NewReporterFromConfig(cfg app.ReporterConfig) (app.Reporter, func(), error) {
	addr := strings.TrimSpace(cfg.GRPCAddr)
	token := strings.TrimSpace(cfg.AuthToken)
	if addr == "" && token == "" {
		return app.NoopReporter{}, func() {}, nil
	}
	if addr == "" {
		return nil, nil, fmt.Errorf("agent-manager gRPC address is required")
	}
	if token == "" {
		return nil, nil, fmt.Errorf("agent-manager auth token is required")
	}
	conn, err := NewConnection(app.ReporterConfig{GRPCAddr: addr})
	if err != nil {
		return nil, nil, err
	}
	reporter, err := NewReporter(agentsv1.NewAgentManagerServiceClient(conn), app.ReporterConfig{AuthToken: token, Timeout: cfg.Timeout})
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	return reporter, func() { _ = conn.Close() }, nil
}

func (r *Reporter) ReportStarted(ctx context.Context, input app.ReportInput) error {
	status, err := r.runtimeStatus(ctx, input.Config.AgentRunID)
	if err != nil {
		return err
	}
	run := status.GetRun()
	if isTerminal(run.GetStatus()) || run.GetStatus() == agentsv1.AgentRunStatus_AGENT_RUN_STATUS_RUNNING {
		return nil
	}
	if _, err := r.recordRunState(ctx, status, agentsv1.AgentRunStatus_AGENT_RUN_STATUS_STARTING, input, startedReasonCode, startedSummary, nil); err != nil {
		return err
	}
	_ = r.recordActivity(ctx, status, input, agentsv1.AgentActivityStatus_AGENT_ACTIVITY_STATUS_STARTED, startedReasonCode, startedSummary, nil)
	return nil
}

func (r *Reporter) ReportFailed(ctx context.Context, input app.ReportInput, diagnostic app.Diagnostic) error {
	status, err := r.runtimeStatus(ctx, input.Config.AgentRunID)
	if err != nil {
		return err
	}
	if isTerminal(status.GetRun().GetStatus()) {
		return nil
	}
	if _, err := r.recordRunState(ctx, status, agentsv1.AgentRunStatus_AGENT_RUN_STATUS_FAILED, input, diagnostic.Code, diagnostic.Summary, &diagnostic.Code); err != nil {
		return err
	}
	_ = r.recordActivity(ctx, status, input, agentsv1.AgentActivityStatus_AGENT_ACTIVITY_STATUS_FAILED, diagnostic.Code, diagnostic.Summary, &diagnostic.Code)
	return nil
}

func (r *Reporter) runtimeStatus(ctx context.Context, runID string) (*agentsv1.AgentRunRuntimeStatusResponse, error) {
	callCtx, cancel := context.WithTimeout(r.outgoingContext(ctx), r.timeout)
	defer cancel()
	response, err := r.client.GetAgentRunRuntimeStatus(callCtx, &agentsv1.GetAgentRunRuntimeStatusRequest{
		Meta:  r.queryMeta(runID, "runtime-status"),
		RunId: strings.TrimSpace(runID),
	})
	if err != nil {
		return nil, err
	}
	if response == nil || response.GetRun() == nil || response.GetRuntimeStatus() == nil {
		return nil, fmt.Errorf("agent-manager runtime status response is invalid")
	}
	return response, nil
}

func (r *Reporter) recordRunState(
	ctx context.Context,
	status *agentsv1.AgentRunRuntimeStatusResponse,
	next agentsv1.AgentRunStatus,
	input app.ReportInput,
	reasonCode string,
	summary string,
	failureCode *string,
) (*agentsv1.AgentRunResponse, error) {
	version := status.GetRuntimeStatus().GetRunVersion()
	if version <= 0 {
		version = status.GetRun().GetVersion()
	}
	request := &agentsv1.RecordRunStateRequest{
		Meta:           r.commandMeta(input.Config, commandKey(input.Config, "run-state", reasonCode), version, reasonCode),
		RunId:          input.Config.AgentRunID,
		Status:         next,
		RuntimeContext: runtimeContext(status),
		ProviderTarget: status.GetRun().GetProviderTarget(),
		ResultSummary:  ptrString(summary),
		ReasonCode:     ptrString(reasonCode),
	}
	if failureCode != nil {
		request.FailureCode = ptrString(*failureCode)
	}
	if !input.StartedAt.IsZero() {
		request.StartedAt = ptrString(formatTime(input.StartedAt))
	}
	if !input.FinishedAt.IsZero() {
		request.FinishedAt = ptrString(formatTime(input.FinishedAt))
	}
	callCtx, cancel := context.WithTimeout(r.outgoingContext(ctx), r.timeout)
	defer cancel()
	return r.client.RecordRunState(callCtx, request)
}

func (r *Reporter) recordActivity(
	ctx context.Context,
	status *agentsv1.AgentRunRuntimeStatusResponse,
	input app.ReportInput,
	activityStatus agentsv1.AgentActivityStatus,
	reasonCode string,
	summary string,
	failureCode *string,
) error {
	run := status.GetRun()
	sessionID := strings.TrimSpace(run.GetSessionId())
	if sessionID == "" {
		sessionID = strings.TrimSpace(input.Context.AgentSessionID)
	}
	if sessionID == "" {
		return nil
	}
	request := &agentsv1.RecordAgentActivityRequest{
		Meta:            r.commandMeta(input.Config, commandKey(input.Config, "activity", reasonCode), 0, reasonCode),
		SessionId:       sessionID,
		RunId:           ptrString(input.Config.AgentRunID),
		ActivityKind:    agentsv1.AgentActivityKind_AGENT_ACTIVITY_KIND_RUNTIME_SIGNAL,
		Status:          activityStatus,
		SafeSummary:     ptrString(summary),
		SafeRefsJson:    safeJSON(safeRefs(input)),
		SafeDetailsJson: safeJSON(safeDetails(input)),
		CorrelationId:   ptrString(input.Config.RuntimeJobID),
	}
	if !input.StartedAt.IsZero() {
		request.StartedAt = ptrString(formatTime(input.StartedAt))
	}
	if !input.FinishedAt.IsZero() {
		request.FinishedAt = ptrString(formatTime(input.FinishedAt))
	}
	if failureCode != nil {
		bounded := *failureCode + ": " + summary
		request.BoundedError = ptrString(bounded)
	}
	callCtx, cancel := context.WithTimeout(r.outgoingContext(ctx), r.timeout)
	defer cancel()
	_, err := r.client.RecordAgentActivity(callCtx, request)
	return err
}

func (r *Reporter) outgoingContext(ctx context.Context) context.Context {
	return metadata.AppendToOutgoingContext(
		ctx,
		grpcserver.MetadataAuthorization,
		"Bearer "+r.authToken,
		grpcserver.MetadataCallerType,
		actorTypeService,
		grpcserver.MetadataCallerID,
		callerID,
	)
}

func (r *Reporter) queryMeta(runID string, suffix string) *agentsv1.QueryMeta {
	return &agentsv1.QueryMeta{
		Actor:          actor(),
		RequestId:      requestID(runID, suffix),
		RequestContext: requestContext(),
	}
}

func (r *Reporter) commandMeta(cfg app.Config, idempotencyKey string, expectedVersion int64, reasonCode string) *agentsv1.CommandMeta {
	meta := &agentsv1.CommandMeta{
		IdempotencyKey: ptrString(idempotencyKey),
		Actor:          actor(),
		Reason:         reasonCode,
		RequestId:      requestID(cfg.AgentRunID, reasonCode),
		RequestContext: requestContext(),
	}
	if expectedVersion > 0 {
		meta.ExpectedVersion = ptrInt64(expectedVersion)
	}
	return meta
}

func actor() *agentsv1.Actor {
	return &agentsv1.Actor{Type: actorTypeService, Id: callerID}
}

func requestContext() *agentsv1.RequestContext {
	return &agentsv1.RequestContext{Source: sourceAgentRunner}
}

func commandKey(cfg app.Config, operation string, reasonCode string) string {
	return strings.Join([]string{
		sourceAgentRunner,
		strings.TrimSpace(cfg.RuntimeJobID),
		strings.TrimSpace(operation),
		strings.TrimSpace(reasonCode),
	}, ":")
}

func requestID(runID string, suffix string) string {
	return strings.Join([]string{sourceAgentRunner, strings.TrimSpace(runID), strings.TrimSpace(suffix)}, ":")
}

func isTerminal(status agentsv1.AgentRunStatus) bool {
	switch status {
	case agentsv1.AgentRunStatus_AGENT_RUN_STATUS_COMPLETED,
		agentsv1.AgentRunStatus_AGENT_RUN_STATUS_FAILED,
		agentsv1.AgentRunStatus_AGENT_RUN_STATUS_CANCELLED:
		return true
	default:
		return false
	}
}

func runtimeContext(status *agentsv1.AgentRunRuntimeStatusResponse) *agentsv1.RuntimeContextRef {
	contextRef := status.GetRun().GetRuntimeContext()
	if contextRef != nil {
		return contextRef
	}
	return status.GetRuntimeStatus().GetRuntimeContext()
}

func safeRefs(input app.ReportInput) map[string]string {
	return map[string]string{
		"agent_run_id":       input.Config.AgentRunID,
		"runtime_job_id":     input.Config.RuntimeJobID,
		"slot_id":            input.Config.SlotID,
		"materialization_id": input.Config.ExpectedMaterializationID,
		"workspace_ref":      input.Config.WorkspaceRef,
		"context_ref":        input.Config.ContextRef,
	}
}

func safeDetails(input app.ReportInput) map[string]string {
	return map[string]string{
		"runner_mode":           input.Config.RunnerMode,
		"runner_profile_ref":    input.Config.RunnerProfileRef,
		"context_digest":        input.Config.ContextDigest,
		"workspace_fingerprint": input.Config.ExpectedMaterializationFingerprint,
	}
}

func safeJSON(value map[string]string) string {
	safe := make(map[string]string, len(value))
	for key, item := range value {
		safeKey := safeJSONValue(key)
		if safeKey == "" {
			continue
		}
		safe[safeKey] = safeJSONValue(item)
	}
	payload, err := json.Marshal(safe)
	if err != nil {
		return "{}"
	}
	if len(payload) > maxSafeDetailsBytes {
		return "{}"
	}
	return string(payload)
}

func safeJSONValue(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) > 512 || unsafeDetail(trimmed) {
		return "redacted"
	}
	return trimmed
}

func unsafeDetail(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	markers := []string{
		"raw_provider_payload",
		"provider_payload",
		"prompt_text",
		"prompt_body",
		"transcript",
		"tool_input",
		"tool_output",
		"kubeconfig",
		"secret_value",
		"token=",
		"authorization",
		"stdout",
		"stderr",
		"-----begin",
		"bearer ",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func ptrString(value string) *string {
	trimmed := strings.TrimSpace(value)
	return &trimmed
}

func ptrInt64(value int64) *int64 {
	return &value
}
