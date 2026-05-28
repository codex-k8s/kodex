package mcptransport

import (
	"context"
	"fmt"
	"strings"

	agentsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/agents/v1"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	agentSessionStartDescription          = "Начать или продолжить агентную сессию через agent-manager."
	agentRunStartDescription              = "Запустить роль в рамках агентной сессии через agent-manager."
	agentRunRecordStateDescription        = "Зафиксировать состояние агентного запуска через agent-manager."
	agentSessionRecordSnapshotDescription = "Записать ссылку на снимок состояния сессии через agent-manager."
	agentHumanGateRequestDescription      = "Зафиксировать ожидание решения человека через agent-manager."
	agentHumanGateGetDescription          = "Прочитать ожидание или результат Human gate через agent-manager."
	agentHumanGateListDescription         = "Получить список ожиданий Human gate через agent-manager."
	diagnosticsRunContextReadDescription  = "Прочитать безопасную сводку сессии и агентных запусков через agent-manager без бизнес-состояния в MCP."
)

var (
	agentScopeTypes = map[string]agentsv1.AgentScopeType{
		"platform":     agentsv1.AgentScopeType_AGENT_SCOPE_TYPE_PLATFORM,
		"organization": agentsv1.AgentScopeType_AGENT_SCOPE_TYPE_ORGANIZATION,
		"project":      agentsv1.AgentScopeType_AGENT_SCOPE_TYPE_PROJECT,
		"repository":   agentsv1.AgentScopeType_AGENT_SCOPE_TYPE_REPOSITORY,
	}
	agentScopeTypeNames = map[agentsv1.AgentScopeType]string{
		agentsv1.AgentScopeType_AGENT_SCOPE_TYPE_PLATFORM:     "platform",
		agentsv1.AgentScopeType_AGENT_SCOPE_TYPE_ORGANIZATION: "organization",
		agentsv1.AgentScopeType_AGENT_SCOPE_TYPE_PROJECT:      "project",
		agentsv1.AgentScopeType_AGENT_SCOPE_TYPE_REPOSITORY:   "repository",
	}
	agentRunStatuses = map[string]agentsv1.AgentRunStatus{
		"requested": agentsv1.AgentRunStatus_AGENT_RUN_STATUS_REQUESTED,
		"starting":  agentsv1.AgentRunStatus_AGENT_RUN_STATUS_STARTING,
		"running":   agentsv1.AgentRunStatus_AGENT_RUN_STATUS_RUNNING,
		"waiting":   agentsv1.AgentRunStatus_AGENT_RUN_STATUS_WAITING,
		"completed": agentsv1.AgentRunStatus_AGENT_RUN_STATUS_COMPLETED,
		"failed":    agentsv1.AgentRunStatus_AGENT_RUN_STATUS_FAILED,
		"cancelled": agentsv1.AgentRunStatus_AGENT_RUN_STATUS_CANCELLED,
		"canceled":  agentsv1.AgentRunStatus_AGENT_RUN_STATUS_CANCELLED,
	}
	agentRunStatusNames = map[agentsv1.AgentRunStatus]string{
		agentsv1.AgentRunStatus_AGENT_RUN_STATUS_REQUESTED: "requested",
		agentsv1.AgentRunStatus_AGENT_RUN_STATUS_STARTING:  "starting",
		agentsv1.AgentRunStatus_AGENT_RUN_STATUS_RUNNING:   "running",
		agentsv1.AgentRunStatus_AGENT_RUN_STATUS_WAITING:   "waiting",
		agentsv1.AgentRunStatus_AGENT_RUN_STATUS_COMPLETED: "completed",
		agentsv1.AgentRunStatus_AGENT_RUN_STATUS_FAILED:    "failed",
		agentsv1.AgentRunStatus_AGENT_RUN_STATUS_CANCELLED: "cancelled",
	}
	agentSessionSnapshotKinds = map[string]agentsv1.AgentSessionSnapshotKind{
		"turn_checkpoint":     agentsv1.AgentSessionSnapshotKind_AGENT_SESSION_SNAPSHOT_KIND_TURN_CHECKPOINT,
		"run_completion":      agentsv1.AgentSessionSnapshotKind_AGENT_SESSION_SNAPSHOT_KIND_RUN_COMPLETION,
		"manual_checkpoint":   agentsv1.AgentSessionSnapshotKind_AGENT_SESSION_SNAPSHOT_KIND_MANUAL_CHECKPOINT,
		"recovery_checkpoint": agentsv1.AgentSessionSnapshotKind_AGENT_SESSION_SNAPSHOT_KIND_RECOVERY_CHECKPOINT,
	}
	agentSessionSnapshotKindNames = map[agentsv1.AgentSessionSnapshotKind]string{
		agentsv1.AgentSessionSnapshotKind_AGENT_SESSION_SNAPSHOT_KIND_TURN_CHECKPOINT:     "turn_checkpoint",
		agentsv1.AgentSessionSnapshotKind_AGENT_SESSION_SNAPSHOT_KIND_RUN_COMPLETION:      "run_completion",
		agentsv1.AgentSessionSnapshotKind_AGENT_SESSION_SNAPSHOT_KIND_MANUAL_CHECKPOINT:   "manual_checkpoint",
		agentsv1.AgentSessionSnapshotKind_AGENT_SESSION_SNAPSHOT_KIND_RECOVERY_CHECKPOINT: "recovery_checkpoint",
	}
	agentSessionStatusNames = map[agentsv1.AgentSessionStatus]string{
		agentsv1.AgentSessionStatus_AGENT_SESSION_STATUS_OPEN:      "open",
		agentsv1.AgentSessionStatus_AGENT_SESSION_STATUS_WAITING:   "waiting",
		agentsv1.AgentSessionStatus_AGENT_SESSION_STATUS_COMPLETED: "completed",
		agentsv1.AgentSessionStatus_AGENT_SESSION_STATUS_FAILED:    "failed",
		agentsv1.AgentSessionStatus_AGENT_SESSION_STATUS_CANCELLED: "cancelled",
	}
	agentHumanGateStatuses = map[string]agentsv1.HumanGateStatus{
		"requested": agentsv1.HumanGateStatus_HUMAN_GATE_STATUS_REQUESTED,
		"waiting":   agentsv1.HumanGateStatus_HUMAN_GATE_STATUS_WAITING,
		"resolved":  agentsv1.HumanGateStatus_HUMAN_GATE_STATUS_RESOLVED,
		"failed":    agentsv1.HumanGateStatus_HUMAN_GATE_STATUS_FAILED,
		"cancelled": agentsv1.HumanGateStatus_HUMAN_GATE_STATUS_CANCELLED,
		"canceled":  agentsv1.HumanGateStatus_HUMAN_GATE_STATUS_CANCELLED,
	}
	agentHumanGateStatusNames = map[agentsv1.HumanGateStatus]string{
		agentsv1.HumanGateStatus_HUMAN_GATE_STATUS_REQUESTED: "requested",
		agentsv1.HumanGateStatus_HUMAN_GATE_STATUS_WAITING:   "waiting",
		agentsv1.HumanGateStatus_HUMAN_GATE_STATUS_RESOLVED:  "resolved",
		agentsv1.HumanGateStatus_HUMAN_GATE_STATUS_FAILED:    "failed",
		agentsv1.HumanGateStatus_HUMAN_GATE_STATUS_CANCELLED: "cancelled",
	}
	agentHumanGateOutcomes = map[string]agentsv1.HumanGateOutcome{
		"none":            agentsv1.HumanGateOutcome_HUMAN_GATE_OUTCOME_NONE,
		"approve":         agentsv1.HumanGateOutcome_HUMAN_GATE_OUTCOME_APPROVE,
		"reject":          agentsv1.HumanGateOutcome_HUMAN_GATE_OUTCOME_REJECT,
		"request_changes": agentsv1.HumanGateOutcome_HUMAN_GATE_OUTCOME_REQUEST_CHANGES,
		"answer":          agentsv1.HumanGateOutcome_HUMAN_GATE_OUTCOME_ANSWER,
	}
	agentHumanGateOutcomeNames = map[agentsv1.HumanGateOutcome]string{
		agentsv1.HumanGateOutcome_HUMAN_GATE_OUTCOME_NONE:            "none",
		agentsv1.HumanGateOutcome_HUMAN_GATE_OUTCOME_APPROVE:         "approve",
		agentsv1.HumanGateOutcome_HUMAN_GATE_OUTCOME_REJECT:          "reject",
		agentsv1.HumanGateOutcome_HUMAN_GATE_OUTCOME_REQUEST_CHANGES: "request_changes",
		agentsv1.HumanGateOutcome_HUMAN_GATE_OUTCOME_ANSWER:          "answer",
	}
)

// AgentToolsHandler routes agent MCP tools to agent-manager.
type AgentToolsHandler struct {
	client AgentManagerClient
}

// NewAgentToolsHandler creates the agent tool boundary.
func NewAgentToolsHandler(client AgentManagerClient) *AgentToolsHandler {
	return &AgentToolsHandler{client: client}
}

// StartSession routes agent.session.start to agent-manager.
func (handler *AgentToolsHandler) StartSession(ctx context.Context, _ *mcpsdk.CallToolRequest, input StartAgentSessionInput) (*mcpsdk.CallToolResult, AgentSessionToolOutput, error) {
	return routeOwnerTool(ctx, input, startAgentSessionRequest, handler.client.StartAgentSession, agentSessionToolOutput, ToolAgentSessionStart)
}

// StartRun routes agent.run.start to agent-manager.
func (handler *AgentToolsHandler) StartRun(ctx context.Context, _ *mcpsdk.CallToolRequest, input StartAgentRunInput) (*mcpsdk.CallToolResult, AgentRunToolOutput, error) {
	return routeOwnerTool(ctx, input, startAgentRunRequest, handler.client.StartAgentRun, agentRunToolOutput, ToolAgentRunStart)
}

// RecordRunState routes agent.run.record_state to agent-manager.
func (handler *AgentToolsHandler) RecordRunState(ctx context.Context, _ *mcpsdk.CallToolRequest, input RecordRunStateInput) (*mcpsdk.CallToolResult, AgentRunToolOutput, error) {
	return routeOwnerTool(ctx, input, recordRunStateRequest, handler.client.RecordRunState, agentRunToolOutput, ToolAgentRunRecordState)
}

// RecordSessionSnapshot routes agent.session.record_snapshot to agent-manager.
func (handler *AgentToolsHandler) RecordSessionSnapshot(ctx context.Context, _ *mcpsdk.CallToolRequest, input RecordSessionSnapshotInput) (*mcpsdk.CallToolResult, AgentSnapshotToolOutput, error) {
	return routeOwnerTool(ctx, input, recordSessionSnapshotRequest, handler.client.RecordSessionStateSnapshot, agentSnapshotToolOutput, ToolAgentSessionRecordSnapshot)
}

// RequestHumanGate routes agent.human_gate.request to agent-manager.
func (handler *AgentToolsHandler) RequestHumanGate(ctx context.Context, _ *mcpsdk.CallToolRequest, input RequestHumanGateInput) (*mcpsdk.CallToolResult, HumanGateToolOutput, error) {
	return routeOwnerTool(ctx, input, requestHumanGateRequest, handler.client.RequestHumanGate, humanGateToolOutput, ToolAgentHumanGateRequest)
}

// GetHumanGate routes agent.human_gate.get to agent-manager.
func (handler *AgentToolsHandler) GetHumanGate(ctx context.Context, _ *mcpsdk.CallToolRequest, input GetHumanGateInput) (*mcpsdk.CallToolResult, HumanGateToolOutput, error) {
	return routeOwnerTool(ctx, input, getHumanGateRequest, handler.client.GetHumanGateRequest, humanGateToolOutput, ToolAgentHumanGateGet)
}

// ListHumanGates routes agent.human_gate.list to agent-manager.
func (handler *AgentToolsHandler) ListHumanGates(ctx context.Context, _ *mcpsdk.CallToolRequest, input ListHumanGatesInput) (*mcpsdk.CallToolResult, HumanGateListOutput, error) {
	return routeOwnerTool(ctx, input, listHumanGatesRequest, handler.client.ListHumanGateRequests, humanGateListOutput, ToolAgentHumanGateList)
}

// ReadRunContext returns a bounded diagnostic view of session and run state.
func (handler *AgentToolsHandler) ReadRunContext(ctx context.Context, _ *mcpsdk.CallToolRequest, input RunContextReadInput) (*mcpsdk.CallToolResult, RunContextOutput, error) {
	sessionRequest, err := getAgentSessionRequest(input)
	if err != nil {
		return nil, RunContextOutput{}, err
	}
	sessionResponse, err := handler.client.GetAgentSession(ctx, sessionRequest)
	if err != nil {
		return nil, RunContextOutput{}, ownerToolError(ToolDiagnosticsRunContextRead, err)
	}
	output := RunContextOutput{Session: agentSessionSummary(sessionResponse.GetSession(), sessionResponse.GetLatestStateSnapshot())}
	if !input.IncludeRuns {
		return nil, output, nil
	}
	listRequest, err := listAgentRunsRequest(input)
	if err != nil {
		return nil, RunContextOutput{}, err
	}
	listResponse, err := handler.client.ListAgentRuns(ctx, listRequest)
	if err != nil {
		return nil, RunContextOutput{}, ownerToolError(ToolDiagnosticsRunContextRead, err)
	}
	output.Runs = agentRunSummaries(listResponse.GetRuns())
	output.Page = pageSummary(listResponse.GetPage())
	return nil, output, nil
}

func agentSessionToolOutput(response *agentsv1.AgentSessionResponse) AgentSessionToolOutput {
	return AgentSessionToolOutput{Session: agentSessionSummary(response.GetSession(), response.GetLatestStateSnapshot())}
}

func agentRunToolOutput(response *agentsv1.AgentRunResponse) AgentRunToolOutput {
	return AgentRunToolOutput{Run: agentRunSummary(response.GetRun())}
}

func agentSnapshotToolOutput(response *agentsv1.AgentSessionStateSnapshotResponse) AgentSnapshotToolOutput {
	return AgentSnapshotToolOutput{
		Snapshot: agentSessionSnapshotSummary(response.GetSnapshot()),
		Session:  agentSessionSummary(response.GetSession(), nil),
	}
}

func humanGateToolOutput(response *agentsv1.HumanGateRequestResponse) HumanGateToolOutput {
	return HumanGateToolOutput{HumanGate: humanGateSummary(response.GetHumanGateRequest())}
}

func humanGateListOutput(response *agentsv1.ListHumanGateRequestsResponse) HumanGateListOutput {
	return HumanGateListOutput{
		HumanGates: humanGateSummaries(response.GetHumanGateRequests()),
		Page:       pageSummary(response.GetPage()),
	}
}

func startAgentSessionRequest(input StartAgentSessionInput) (*agentsv1.StartAgentSessionRequest, error) {
	meta, err := commandMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	scope, err := scopeRef(input.Scope)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.CreatedByActorRef) == "" {
		return nil, invalidInput("created_by_actor_ref is required")
	}
	return &agentsv1.StartAgentSessionRequest{
		Meta:                meta,
		Scope:               scope,
		ProviderWorkItemRef: optionalString(input.ProviderWorkItemRef),
		FlowVersionId:       optionalString(input.FlowVersionID),
		CurrentStageId:      optionalString(input.CurrentStageID),
		CreatedByActorRef:   strings.TrimSpace(input.CreatedByActorRef),
	}, nil
}

func startAgentRunRequest(input StartAgentRunInput) (*agentsv1.StartAgentRunRequest, error) {
	meta, err := commandMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.SessionID) == "" {
		return nil, invalidInput("session_id is required")
	}
	if strings.TrimSpace(input.RoleProfileID) == "" {
		return nil, invalidInput("role_profile_id is required")
	}
	if strings.TrimSpace(input.PromptTemplateVersionID) == "" {
		return nil, invalidInput("prompt_template_version_id is required")
	}
	return &agentsv1.StartAgentRunRequest{
		Meta:                    meta,
		SessionId:               strings.TrimSpace(input.SessionID),
		FlowVersionId:           optionalString(input.FlowVersionID),
		StageId:                 optionalString(input.StageID),
		RoleProfileId:           strings.TrimSpace(input.RoleProfileID),
		PromptTemplateVersionId: strings.TrimSpace(input.PromptTemplateVersionID),
		ProviderTarget:          providerTargetRef(input.ProviderTarget),
		GuidanceSelectionHints:  guidanceSelectionHints(input.GuidanceSelectionHints),
	}, nil
}

func recordRunStateRequest(input RecordRunStateInput) (*agentsv1.RecordRunStateRequest, error) {
	meta, err := commandMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	runStatus, err := agentRunStatus(input.Status)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.RunID) == "" {
		return nil, invalidInput("run_id is required")
	}
	return &agentsv1.RecordRunStateRequest{
		Meta:           meta,
		RunId:          strings.TrimSpace(input.RunID),
		Status:         runStatus,
		RuntimeContext: runtimeContextRef(input.RuntimeContext),
		ProviderTarget: providerTargetRef(input.ProviderTarget),
		ResultSummary:  optionalString(input.ResultSummary),
		FailureCode:    optionalString(input.FailureCode),
		StartedAt:      optionalString(input.StartedAt),
		FinishedAt:     optionalString(input.FinishedAt),
		ReasonCode:     optionalString(input.ReasonCode),
	}, nil
}

func recordSessionSnapshotRequest(input RecordSessionSnapshotInput) (*agentsv1.RecordSessionStateSnapshotRequest, error) {
	meta, err := commandMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	snapshotKind, err := agentSessionSnapshotKind(input.SnapshotKind)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.SessionID) == "" {
		return nil, invalidInput("session_id is required")
	}
	if strings.TrimSpace(input.CapturedAt) == "" {
		return nil, invalidInput("captured_at is required")
	}
	object, err := objectRef(input.Object)
	if err != nil {
		return nil, err
	}
	return &agentsv1.RecordSessionStateSnapshotRequest{
		Meta:         meta,
		SessionId:    strings.TrimSpace(input.SessionID),
		RunId:        optionalString(input.RunID),
		SnapshotKind: snapshotKind,
		TurnIndex:    input.TurnIndex,
		Object:       object,
		CapturedAt:   strings.TrimSpace(input.CapturedAt),
	}, nil
}

func getAgentSessionRequest(input RunContextReadInput) (*agentsv1.GetAgentSessionRequest, error) {
	meta, err := queryMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	sessionID, err := requiredTrimmed(input.SessionID, "session_id")
	if err != nil {
		return nil, err
	}
	return &agentsv1.GetAgentSessionRequest{Meta: meta, SessionId: sessionID}, nil
}

func listAgentRunsRequest(input RunContextReadInput) (*agentsv1.ListAgentRunsRequest, error) {
	meta, err := queryMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	statusFilter, err := optionalAgentRunStatus(input.Status)
	if err != nil {
		return nil, err
	}
	return &agentsv1.ListAgentRunsRequest{
		Meta:                meta,
		SessionId:           optionalString(input.SessionID),
		Status:              statusFilter,
		ProviderWorkItemRef: optionalString(input.ProviderWorkItemRef),
		Page:                pageRequest(input.Page),
	}, nil
}

func requestHumanGateRequest(input RequestHumanGateInput) (*agentsv1.RequestHumanGateRequest, error) {
	meta, err := commandMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.SessionID) == "" {
		return nil, invalidInput("session_id is required")
	}
	if strings.TrimSpace(input.RequestKind) == "" {
		return nil, invalidInput("request_kind is required")
	}
	if strings.TrimSpace(input.ReasonCode) == "" {
		return nil, invalidInput("reason_code is required")
	}
	return &agentsv1.RequestHumanGateRequest{
		Meta:                     meta,
		SessionId:                strings.TrimSpace(input.SessionID),
		RunId:                    optionalString(input.RunID),
		StageId:                  optionalString(input.StageID),
		AcceptanceResultId:       optionalString(input.AcceptanceResultID),
		ProviderTarget:           providerTargetRef(input.ProviderTarget),
		TargetRef:                optionalString(input.TargetRef),
		RequestKind:              strings.TrimSpace(input.RequestKind),
		ReasonCode:               strings.TrimSpace(input.ReasonCode),
		SafeSummary:              optionalString(input.SafeSummary),
		InteractionRequestRef:    optionalString(input.InteractionRequestRef),
		GovernanceGateRequestRef: optionalString(input.GovernanceGateRequestRef),
	}, nil
}

func getHumanGateRequest(input GetHumanGateInput) (*agentsv1.GetHumanGateRequestRequest, error) {
	meta, err := queryMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	requestID, err := requiredTrimmed(input.HumanGateRequestID, "human_gate_request_id")
	if err != nil {
		return nil, err
	}
	request := &agentsv1.GetHumanGateRequestRequest{Meta: meta}
	request.HumanGateRequestId = requestID
	return request, nil
}

func listHumanGatesRequest(input ListHumanGatesInput) (*agentsv1.ListHumanGateRequestsRequest, error) {
	meta, err := queryMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	statusFilter, err := optionalHumanGateStatus(input.Status)
	if err != nil {
		return nil, err
	}
	outcomeFilter, err := optionalHumanGateOutcome(input.Outcome)
	if err != nil {
		return nil, err
	}
	return &agentsv1.ListHumanGateRequestsRequest{
		Meta:      meta,
		SessionId: optionalString(input.SessionID),
		RunId:     optionalString(input.RunID),
		StageId:   optionalString(input.StageID),
		Status:    statusFilter,
		Outcome:   outcomeFilter,
		Page:      pageRequest(input.Page),
	}, nil
}

func commandMeta(input AgentCommandMetaInput) (*agentsv1.CommandMeta, error) {
	actor, err := actor(input.Actor)
	if err != nil {
		return nil, err
	}
	requestContext, err := requestContext(input.RequestContext)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.CommandID) == "" && strings.TrimSpace(input.IdempotencyKey) == "" {
		return nil, invalidInput("command_id or idempotency_key is required")
	}
	if strings.TrimSpace(input.RequestID) == "" {
		return nil, invalidInput("request_id is required")
	}
	return &agentsv1.CommandMeta{
		CommandId:       optionalString(input.CommandID),
		IdempotencyKey:  optionalString(input.IdempotencyKey),
		ExpectedVersion: input.ExpectedVersion,
		Actor:           actor,
		Reason:          strings.TrimSpace(input.Reason),
		RequestId:       strings.TrimSpace(input.RequestID),
		RequestContext:  requestContext,
	}, nil
}

func queryMeta(input AgentQueryMetaInput) (*agentsv1.QueryMeta, error) {
	actor, err := actor(input.Actor)
	if err != nil {
		return nil, err
	}
	requestContext, err := requestContext(input.RequestContext)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.RequestID) == "" {
		return nil, invalidInput("request_id is required")
	}
	return &agentsv1.QueryMeta{
		Actor:          actor,
		RequestId:      strings.TrimSpace(input.RequestID),
		RequestContext: requestContext,
	}, nil
}

func actor(input AgentActorInput) (*agentsv1.Actor, error) {
	actorType, actorID, err := actorFields(input.Type, input.ID)
	if err != nil {
		return nil, err
	}
	return &agentsv1.Actor{Type: actorType, Id: actorID}, nil
}

func requestContext(input AgentRequestContextInput) (*agentsv1.RequestContext, error) {
	source, traceID, sessionID, clientIPHash, err := safeRequestContext(input.Source, input.TraceID, input.SessionID, input.ClientIPHash)
	if err != nil {
		return nil, err
	}
	return &agentsv1.RequestContext{
		Source:       source,
		TraceId:      traceID,
		SessionId:    sessionID,
		ClientIpHash: clientIPHash,
	}, nil
}

func scopeRef(input AgentScopeInput) (*agentsv1.ScopeRef, error) {
	if strings.TrimSpace(input.Ref) == "" {
		return nil, invalidInput("scope.ref is required")
	}
	scopeType, err := agentScopeType(input.Type)
	if err != nil {
		return nil, err
	}
	return &agentsv1.ScopeRef{Type: scopeType, Ref: strings.TrimSpace(input.Ref)}, nil
}

func providerTargetRef(input AgentProviderTargetInput) *agentsv1.ProviderTargetRef {
	return &agentsv1.ProviderTargetRef{
		WorkItemRef:     optionalString(input.WorkItemRef),
		PullRequestRef:  optionalString(input.PullRequestRef),
		CommentRef:      optionalString(input.CommentRef),
		ReviewSignalRef: optionalString(input.ReviewSignalRef),
	}
}

func runtimeContextRef(input AgentRuntimeContextInput) *agentsv1.RuntimeContextRef {
	return &agentsv1.RuntimeContextRef{
		SlotRef:      optionalString(input.SlotRef),
		JobRef:       optionalString(input.JobRef),
		WorkspaceRef: optionalString(input.WorkspaceRef),
		ContextRef:   optionalString(input.ContextRef),
	}
}

func guidanceSelectionHints(inputs []AgentGuidanceSelectionHintInput) []*agentsv1.GuidanceSelectionHint {
	if len(inputs) == 0 {
		return nil
	}
	result := make([]*agentsv1.GuidanceSelectionHint, 0, len(inputs))
	for _, input := range inputs {
		result = append(result, &agentsv1.GuidanceSelectionHint{
			PackageInstallationRef: optionalString(input.PackageInstallationRef),
			PackageSlug:            optionalString(input.PackageSlug),
		})
	}
	return result
}

func objectRef(input AgentObjectInput) (*agentsv1.ObjectRef, error) {
	if strings.TrimSpace(input.ObjectURI) == "" {
		return nil, invalidInput("object.object_uri is required")
	}
	if strings.TrimSpace(input.ObjectDigest) == "" {
		return nil, invalidInput("object.object_digest is required")
	}
	return &agentsv1.ObjectRef{
		ObjectUri:       strings.TrimSpace(input.ObjectURI),
		ObjectDigest:    strings.TrimSpace(input.ObjectDigest),
		ObjectSizeBytes: input.ObjectSizeBytes,
	}, nil
}

func pageRequest(input AgentPageInput) *agentsv1.PageRequest {
	return &agentsv1.PageRequest{
		PageSize:  input.PageSize,
		PageToken: optionalString(input.PageToken),
	}
}

func agentSessionSummary(session *agentsv1.AgentSession, snapshot *agentsv1.AgentSessionStateSnapshot) AgentSessionSummary {
	if session == nil {
		return AgentSessionSummary{}
	}
	output := AgentSessionSummary{
		ID:                    session.GetId(),
		Scope:                 scopeSummary(session.GetScope()),
		ProviderWorkItemRef:   session.GetProviderWorkItemRef(),
		FlowVersionID:         session.GetFlowVersionId(),
		CurrentStageID:        session.GetCurrentStageId(),
		LatestStateSnapshotID: session.GetLatestStateSnapshotId(),
		Status:                sessionStatusName(session.GetStatus()),
		CreatedByActorRef:     session.GetCreatedByActorRef(),
		Version:               session.GetVersion(),
		CreatedAt:             session.GetCreatedAt(),
		UpdatedAt:             session.GetUpdatedAt(),
	}
	if snapshot != nil {
		snapshotSummary := agentSessionSnapshotSummary(snapshot)
		output.LatestStateSnapshot = &snapshotSummary
	}
	return output
}

func agentRunSummary(run *agentsv1.AgentRun) AgentRunSummary {
	if run == nil {
		return AgentRunSummary{}
	}
	return AgentRunSummary{
		ID:                      run.GetId(),
		SessionID:               run.GetSessionId(),
		FlowVersionID:           run.GetFlowVersionId(),
		StageID:                 run.GetStageId(),
		RoleProfileID:           run.GetRoleProfileId(),
		RoleProfileVersion:      run.GetRoleProfileVersion(),
		PromptTemplateVersionID: run.GetPromptTemplateVersionId(),
		RuntimeContext:          runtimeContextSummary(run.GetRuntimeContext()),
		ProviderTarget:          providerTargetSummary(run.GetProviderTarget()),
		Status:                  runStatusName(run.GetStatus()),
		ResultSummary:           run.GetResultSummary(),
		FailureCode:             run.GetFailureCode(),
		Version:                 run.GetVersion(),
		StartedAt:               run.GetStartedAt(),
		FinishedAt:              run.GetFinishedAt(),
		CreatedAt:               run.GetCreatedAt(),
		UpdatedAt:               run.GetUpdatedAt(),
	}
}

func agentRunSummaries(runs []*agentsv1.AgentRun) []AgentRunSummary {
	return summarizeItems(runs, agentRunSummary)
}

func agentSessionSnapshotSummary(snapshot *agentsv1.AgentSessionStateSnapshot) AgentSessionSnapshotSummary {
	if snapshot == nil {
		return AgentSessionSnapshotSummary{}
	}
	return AgentSessionSnapshotSummary{
		ID:           snapshot.GetId(),
		SessionID:    snapshot.GetSessionId(),
		RunID:        snapshot.GetRunId(),
		SnapshotKind: snapshotKindName(snapshot.GetSnapshotKind()),
		TurnIndex:    snapshot.TurnIndex,
		Object:       objectSummary(snapshot.GetObject()),
		CapturedAt:   snapshot.GetCapturedAt(),
		CreatedAt:    snapshot.GetCreatedAt(),
	}
}

func humanGateSummaries(requests []*agentsv1.HumanGateRequest) []HumanGateSummary {
	return summarizeItems(requests, humanGateSummary)
}

func humanGateSummary(request *agentsv1.HumanGateRequest) HumanGateSummary {
	if request == nil {
		return HumanGateSummary{}
	}
	return HumanGateSummary{
		ID:                       request.GetId(),
		SessionID:                request.GetSessionId(),
		RunID:                    request.GetRunId(),
		StageID:                  request.GetStageId(),
		AcceptanceResultID:       request.GetAcceptanceResultId(),
		ProviderTarget:           providerTargetSummary(request.GetProviderTarget()),
		TargetRef:                request.GetTargetRef(),
		RequestKind:              request.GetRequestKind(),
		ReasonCode:               request.GetReasonCode(),
		SafeSummary:              request.GetSafeSummary(),
		InteractionRequestRef:    request.GetInteractionRequestRef(),
		InteractionResponseRef:   request.GetInteractionResponseRef(),
		GovernanceGateRequestRef: request.GetGovernanceGateRequestRef(),
		GovernanceDecisionRef:    request.GetGovernanceDecisionRef(),
		Status:                   humanGateStatusName(request.GetStatus()),
		Outcome:                  humanGateOutcomeName(request.GetOutcome()),
		Version:                  request.GetVersion(),
		CreatedAt:                request.GetCreatedAt(),
		UpdatedAt:                request.GetUpdatedAt(),
		ResolvedAt:               request.GetResolvedAt(),
	}
}

func scopeSummary(scope *agentsv1.ScopeRef) AgentScopeSummary {
	if scope == nil {
		return AgentScopeSummary{}
	}
	return AgentScopeSummary{Type: scopeTypeName(scope.GetType()), Ref: scope.GetRef()}
}

func providerTargetSummary(target *agentsv1.ProviderTargetRef) AgentProviderTargetSummary {
	if target == nil {
		return AgentProviderTargetSummary{}
	}
	return AgentProviderTargetSummary{
		WorkItemRef:     target.GetWorkItemRef(),
		PullRequestRef:  target.GetPullRequestRef(),
		CommentRef:      target.GetCommentRef(),
		ReviewSignalRef: target.GetReviewSignalRef(),
	}
}

func runtimeContextSummary(runtime *agentsv1.RuntimeContextRef) AgentRuntimeContextSummary {
	if runtime == nil {
		return AgentRuntimeContextSummary{}
	}
	return AgentRuntimeContextSummary{
		SlotRef:      runtime.GetSlotRef(),
		JobRef:       runtime.GetJobRef(),
		WorkspaceRef: runtime.GetWorkspaceRef(),
		ContextRef:   runtime.GetContextRef(),
	}
}

func objectSummary(object *agentsv1.ObjectRef) AgentObjectSummary {
	if object == nil {
		return AgentObjectSummary{}
	}
	return AgentObjectSummary{
		ObjectURI:       object.GetObjectUri(),
		ObjectDigest:    object.GetObjectDigest(),
		ObjectSizeBytes: object.ObjectSizeBytes,
	}
}

func pageSummary(page *agentsv1.PageResponse) PageSummary {
	if page == nil {
		return PageSummary{}
	}
	return PageSummary{NextPageToken: page.GetNextPageToken()}
}

func agentScopeType(value string) (agentsv1.AgentScopeType, error) {
	return requiredEnumValue(normalizedKey(value), agentScopeTypes, agentsv1.AgentScopeType_AGENT_SCOPE_TYPE_UNSPECIFIED, "scope.type")
}

func agentRunStatus(value string) (agentsv1.AgentRunStatus, error) {
	statusValue, err := optionalAgentRunStatus(value)
	if err != nil {
		return agentsv1.AgentRunStatus_AGENT_RUN_STATUS_UNSPECIFIED, err
	}
	if statusValue == nil {
		return agentsv1.AgentRunStatus_AGENT_RUN_STATUS_UNSPECIFIED, invalidInput("status is required")
	}
	return *statusValue, nil
}

func optionalAgentRunStatus(value string) (*agentsv1.AgentRunStatus, error) {
	return optionalEnumValue(normalizedKey(value), agentRunStatuses, agentsv1.AgentRunStatus_AGENT_RUN_STATUS_UNSPECIFIED, "status")
}

func agentSessionSnapshotKind(value string) (agentsv1.AgentSessionSnapshotKind, error) {
	return requiredEnumValue(normalizedKey(value), agentSessionSnapshotKinds, agentsv1.AgentSessionSnapshotKind_AGENT_SESSION_SNAPSHOT_KIND_UNSPECIFIED, "snapshot_kind")
}

func optionalHumanGateStatus(value string) (*agentsv1.HumanGateStatus, error) {
	return optionalEnumValue(normalizedKey(value), agentHumanGateStatuses, agentsv1.HumanGateStatus_HUMAN_GATE_STATUS_UNSPECIFIED, "status")
}

func optionalHumanGateOutcome(value string) (*agentsv1.HumanGateOutcome, error) {
	return optionalEnumValue(normalizedKey(value), agentHumanGateOutcomes, agentsv1.HumanGateOutcome_HUMAN_GATE_OUTCOME_UNSPECIFIED, "outcome")
}

func scopeTypeName(value agentsv1.AgentScopeType) string {
	return enumName(value, agentScopeTypeNames)
}

func sessionStatusName(value agentsv1.AgentSessionStatus) string {
	return enumName(value, agentSessionStatusNames)
}

func runStatusName(value agentsv1.AgentRunStatus) string {
	return enumName(value, agentRunStatusNames)
}

func snapshotKindName(value agentsv1.AgentSessionSnapshotKind) string {
	return enumName(value, agentSessionSnapshotKindNames)
}

func humanGateStatusName(value agentsv1.HumanGateStatus) string {
	return enumName(value, agentHumanGateStatusNames)
}

func humanGateOutcomeName(value agentsv1.HumanGateOutcome) string {
	return enumName(value, agentHumanGateOutcomeNames)
}

func requiredEnumValue[T comparable](key string, values map[string]T, zero T, field string) (T, error) {
	if key == "" {
		return zero, invalidInput(field + " is required")
	}
	value, ok := values[key]
	if !ok || value == zero {
		return zero, invalidInput(field + " is invalid")
	}
	return value, nil
}

func optionalEnumValue[T comparable](key string, values map[string]T, zero T, field string) (*T, error) {
	if key == "" {
		return nil, nil
	}
	value, err := requiredEnumValue(key, values, zero, field)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func enumName[T comparable](value T, names map[T]string) string {
	if name, ok := names[value]; ok {
		return name
	}
	return "unspecified"
}

func safeRequestContext(sourceInput, traceIDInput, sessionIDInput, clientIPHashInput string) (string, *string, *string, *string, error) {
	source, err := safeRequestSource(sourceInput)
	if err != nil {
		return "", nil, nil, nil, err
	}
	return source, optionalString(traceIDInput), optionalString(sessionIDInput), optionalString(clientIPHashInput), nil
}

func optionalString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func normalizedKey(value string) string {
	normalized := strings.TrimSpace(strings.ToLower(value))
	normalized = strings.TrimPrefix(normalized, "agent_scope_type_")
	normalized = strings.TrimPrefix(normalized, "agent_run_status_")
	normalized = strings.TrimPrefix(normalized, "agent_session_snapshot_kind_")
	normalized = strings.ReplaceAll(normalized, "-", "_")
	return normalized
}

func invalidInput(message string) error {
	return fmt.Errorf("mcp.invalid_context: %s", message)
}

func ownerToolError(tool string, err error) error {
	if err == nil {
		return nil
	}
	if code := status.Code(err); code != codes.OK {
		return fmt.Errorf("%s failed: owner returned %s", tool, code.String())
	}
	return fmt.Errorf("%s failed: owner route unavailable", tool)
}
