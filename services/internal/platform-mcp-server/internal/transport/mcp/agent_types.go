package mcptransport

import (
	"context"

	agentsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/agents/v1"
)

const (
	ToolAgentSessionStart          = "agent.session.start"
	ToolAgentRunStart              = "agent.run.start"
	ToolAgentRunRecordState        = "agent.run.record_state"
	ToolAgentSessionRecordSnapshot = "agent.session.record_snapshot"
	ToolAgentHumanGateRequest      = "agent.human_gate.request"
	ToolAgentHumanGateGet          = "agent.human_gate.get"
	ToolAgentHumanGateList         = "agent.human_gate.list"
	ToolDiagnosticsRunContextRead  = "diagnostics.run_context.read"
)

// AgentManagerClient is the owner route used by agent MCP tools.
type AgentManagerClient interface {
	StartAgentSession(context.Context, *agentsv1.StartAgentSessionRequest) (*agentsv1.AgentSessionResponse, error)
	StartAgentRun(context.Context, *agentsv1.StartAgentRunRequest) (*agentsv1.AgentRunResponse, error)
	RecordRunState(context.Context, *agentsv1.RecordRunStateRequest) (*agentsv1.AgentRunResponse, error)
	RecordSessionStateSnapshot(context.Context, *agentsv1.RecordSessionStateSnapshotRequest) (*agentsv1.AgentSessionStateSnapshotResponse, error)
	RequestHumanGate(context.Context, *agentsv1.RequestHumanGateRequest) (*agentsv1.HumanGateRequestResponse, error)
	GetHumanGateRequest(context.Context, *agentsv1.GetHumanGateRequestRequest) (*agentsv1.HumanGateRequestResponse, error)
	ListHumanGateRequests(context.Context, *agentsv1.ListHumanGateRequestsRequest) (*agentsv1.ListHumanGateRequestsResponse, error)
	GetAgentSession(context.Context, *agentsv1.GetAgentSessionRequest) (*agentsv1.AgentSessionResponse, error)
	ListAgentRuns(context.Context, *agentsv1.ListAgentRunsRequest) (*agentsv1.ListAgentRunsResponse, error)
}

// AgentCommandMetaInput carries safe command metadata for agent-manager tools.
type AgentCommandMetaInput struct {
	CommandID       string                   `json:"command_id,omitempty" jsonschema:"unique command identifier"`
	IdempotencyKey  string                   `json:"idempotency_key,omitempty" jsonschema:"idempotency key scoped by operation and actor"`
	ExpectedVersion *int64                   `json:"expected_version,omitempty" jsonschema:"expected aggregate version for optimistic concurrency"`
	Actor           AgentActorInput          `json:"actor" jsonschema:"authenticated caller"`
	Reason          string                   `json:"reason" jsonschema:"machine or operator reason for audit"`
	RequestID       string                   `json:"request_id" jsonschema:"request identifier for logs and audit"`
	RequestContext  AgentRequestContextInput `json:"request_context" jsonschema:"safe request context"`
}

// AgentQueryMetaInput carries safe read metadata for agent-manager tools.
type AgentQueryMetaInput struct {
	Actor          AgentActorInput          `json:"actor" jsonschema:"authenticated caller"`
	RequestID      string                   `json:"request_id" jsonschema:"request identifier for logs and audit"`
	RequestContext AgentRequestContextInput `json:"request_context" jsonschema:"safe request context"`
}

// AgentActorInput identifies a user, service, agent or external account.
type AgentActorInput struct {
	Type string `json:"type" jsonschema:"actor type such as user, service, agent or external_account"`
	ID   string `json:"id" jsonschema:"actor identifier in its owner domain"`
}

// AgentRequestContextInput carries safe metadata and never includes tokens or secrets.
type AgentRequestContextInput struct {
	Source       string `json:"source" jsonschema:"caller surface, for example platform-mcp-server"`
	TraceID      string `json:"trace_id,omitempty" jsonschema:"platform trace identifier"`
	SessionID    string `json:"session_id,omitempty" jsonschema:"user or agent session identifier"`
	ClientIPHash string `json:"client_ip_hash,omitempty" jsonschema:"hashed client address"`
}

// AgentScopeInput identifies where the session applies.
type AgentScopeInput struct {
	Type string `json:"type" jsonschema:"scope type: platform, organization, project or repository"`
	Ref  string `json:"ref" jsonschema:"scope identifier owned by another domain"`
}

// AgentProviderTargetInput points to provider-native artifacts by safe refs.
type AgentProviderTargetInput struct {
	WorkItemRef     string `json:"work_item_ref,omitempty" jsonschema:"provider-native work item reference"`
	PullRequestRef  string `json:"pull_request_ref,omitempty" jsonschema:"pull request or merge request reference"`
	CommentRef      string `json:"comment_ref,omitempty" jsonschema:"provider comment reference"`
	ReviewSignalRef string `json:"review_signal_ref,omitempty" jsonschema:"provider review signal reference"`
}

// AgentRuntimeContextInput points to runtime-manager state by safe refs.
type AgentRuntimeContextInput struct {
	SlotRef      string `json:"slot_ref,omitempty" jsonschema:"runtime slot reference"`
	JobRef       string `json:"job_ref,omitempty" jsonschema:"platform job reference"`
	WorkspaceRef string `json:"workspace_ref,omitempty" jsonschema:"workspace materialization reference"`
	ContextRef   string `json:"context_ref,omitempty" jsonschema:"opaque runtime context reference"`
}

// AgentGuidanceSelectionHintInput narrows guidance package selection.
type AgentGuidanceSelectionHintInput struct {
	PackageInstallationRef string `json:"package_installation_ref,omitempty" jsonschema:"package installation reference"`
	PackageSlug            string `json:"package_slug,omitempty" jsonschema:"package slug in run scope"`
}

// AgentObjectInput points to object storage without embedding object content.
type AgentObjectInput struct {
	ObjectURI       string `json:"object_uri" jsonschema:"object storage URI"`
	ObjectDigest    string `json:"object_digest" jsonschema:"object digest for integrity checks"`
	ObjectSizeBytes *int64 `json:"object_size_bytes,omitempty" jsonschema:"optional object size"`
}

// AgentPageInput limits list responses.
type AgentPageInput struct {
	PageSize  int32  `json:"page_size,omitempty" jsonschema:"maximum item count"`
	PageToken string `json:"page_token,omitempty" jsonschema:"opaque continuation token"`
}

// StartAgentSessionInput is the MCP input for agent.session.start.
type StartAgentSessionInput struct {
	Meta                AgentCommandMetaInput `json:"meta" jsonschema:"command metadata"`
	Scope               AgentScopeInput       `json:"scope" jsonschema:"session scope"`
	ProviderWorkItemRef string                `json:"provider_work_item_ref,omitempty" jsonschema:"provider-native work item reference"`
	FlowVersionID       string                `json:"flow_version_id,omitempty" jsonschema:"flow version identifier"`
	CurrentStageID      string                `json:"current_stage_id,omitempty" jsonschema:"current stage identifier"`
	CreatedByActorRef   string                `json:"created_by_actor_ref" jsonschema:"actor reference that owns the created session"`
}

// StartAgentRunInput is the MCP input for agent.run.start.
type StartAgentRunInput struct {
	Meta                    AgentCommandMetaInput             `json:"meta" jsonschema:"command metadata"`
	SessionID               string                            `json:"session_id" jsonschema:"agent session identifier"`
	FlowVersionID           string                            `json:"flow_version_id,omitempty" jsonschema:"flow version identifier"`
	StageID                 string                            `json:"stage_id,omitempty" jsonschema:"stage identifier"`
	RoleProfileID           string                            `json:"role_profile_id" jsonschema:"role profile identifier"`
	PromptTemplateVersionID string                            `json:"prompt_template_version_id" jsonschema:"prompt template version identifier"`
	ProviderTarget          AgentProviderTargetInput          `json:"provider_target,omitempty" jsonschema:"provider target refs"`
	GuidanceSelectionHints  []AgentGuidanceSelectionHintInput `json:"guidance_selection_hints,omitempty" jsonschema:"guidance package selection hints"`
}

// RecordRunStateInput is the MCP input for agent.run.record_state.
type RecordRunStateInput struct {
	Meta           AgentCommandMetaInput    `json:"meta" jsonschema:"command metadata"`
	RunID          string                   `json:"run_id" jsonschema:"agent run identifier"`
	Status         string                   `json:"status" jsonschema:"run status: requested, starting, running, waiting, completed, failed or cancelled"`
	RuntimeContext AgentRuntimeContextInput `json:"runtime_context,omitempty" jsonschema:"runtime refs"`
	ProviderTarget AgentProviderTargetInput `json:"provider_target,omitempty" jsonschema:"provider refs"`
	ResultSummary  string                   `json:"result_summary,omitempty" jsonschema:"short safe result summary"`
	FailureCode    string                   `json:"failure_code,omitempty" jsonschema:"machine failure code"`
	StartedAt      string                   `json:"started_at,omitempty" jsonschema:"RFC3339 start timestamp"`
	FinishedAt     string                   `json:"finished_at,omitempty" jsonschema:"RFC3339 finish timestamp"`
	ReasonCode     string                   `json:"reason_code,omitempty" jsonschema:"machine waiting or failure reason code"`
}

// RecordSessionSnapshotInput is the MCP input for agent.session.record_snapshot.
type RecordSessionSnapshotInput struct {
	Meta         AgentCommandMetaInput `json:"meta" jsonschema:"command metadata"`
	SessionID    string                `json:"session_id" jsonschema:"agent session identifier"`
	RunID        string                `json:"run_id,omitempty" jsonschema:"agent run identifier"`
	SnapshotKind string                `json:"snapshot_kind" jsonschema:"snapshot kind: turn_checkpoint, run_completion, manual_checkpoint or recovery_checkpoint"`
	TurnIndex    *int64                `json:"turn_index,omitempty" jsonschema:"optional turn index"`
	Object       AgentObjectInput      `json:"object" jsonschema:"object storage reference"`
	CapturedAt   string                `json:"captured_at" jsonschema:"RFC3339 capture timestamp"`
}

// RunContextReadInput is the MCP input for diagnostics.run_context.read.
type RunContextReadInput struct {
	Meta                AgentQueryMetaInput `json:"meta" jsonschema:"query metadata"`
	SessionID           string              `json:"session_id" jsonschema:"agent session identifier"`
	IncludeRuns         bool                `json:"include_runs,omitempty" jsonschema:"include agent runs for the session"`
	Status              string              `json:"status,omitempty" jsonschema:"optional run status filter"`
	ProviderWorkItemRef string              `json:"provider_work_item_ref,omitempty" jsonschema:"optional provider work item filter"`
	Page                AgentPageInput      `json:"page,omitempty" jsonschema:"run list page"`
}

// RequestHumanGateInput is the MCP input for agent.human_gate.request.
type RequestHumanGateInput struct {
	Meta                     AgentCommandMetaInput    `json:"meta" jsonschema:"command metadata"`
	SessionID                string                   `json:"session_id" jsonschema:"agent session identifier"`
	RunID                    string                   `json:"run_id,omitempty" jsonschema:"agent run identifier"`
	StageID                  string                   `json:"stage_id,omitempty" jsonschema:"flow stage identifier"`
	AcceptanceResultID       string                   `json:"acceptance_result_id,omitempty" jsonschema:"acceptance result identifier"`
	ProviderTarget           AgentProviderTargetInput `json:"provider_target,omitempty" jsonschema:"provider refs"`
	TargetRef                string                   `json:"target_ref,omitempty" jsonschema:"owner decision target ref"`
	RequestKind              string                   `json:"request_kind" jsonschema:"owner decision request kind"`
	ReasonCode               string                   `json:"reason_code" jsonschema:"machine reason code"`
	SafeSummary              string                   `json:"safe_summary,omitempty" jsonschema:"short safe summary"`
	InteractionRequestRef    string                   `json:"interaction_request_ref,omitempty" jsonschema:"interaction-hub request ref"`
	GovernanceGateRequestRef string                   `json:"governance_gate_request_ref,omitempty" jsonschema:"governance gate request ref"`
}

// GetHumanGateInput is the MCP input for agent.human_gate.get.
type GetHumanGateInput struct {
	Meta               AgentQueryMetaInput `json:"meta" jsonschema:"query metadata"`
	HumanGateRequestID string              `json:"human_gate_request_id" jsonschema:"human gate request identifier"`
}

// ListHumanGatesInput is the MCP input for agent.human_gate.list.
type ListHumanGatesInput struct {
	Meta      AgentQueryMetaInput `json:"meta" jsonschema:"query metadata"`
	SessionID string              `json:"session_id,omitempty" jsonschema:"agent session filter"`
	RunID     string              `json:"run_id,omitempty" jsonschema:"agent run filter"`
	StageID   string              `json:"stage_id,omitempty" jsonschema:"flow stage filter"`
	Status    string              `json:"status,omitempty" jsonschema:"status filter: requested, waiting, resolved, failed or cancelled"`
	Outcome   string              `json:"outcome,omitempty" jsonschema:"outcome filter: none, approve, reject, request_changes or answer"`
	Page      AgentPageInput      `json:"page,omitempty" jsonschema:"page request"`
}

// AgentSessionToolOutput is a safe session response.
type AgentSessionToolOutput struct {
	Session AgentSessionSummary `json:"session" jsonschema:"agent session"`
}

// AgentRunToolOutput is a safe run response.
type AgentRunToolOutput struct {
	Run AgentRunSummary `json:"run" jsonschema:"agent run"`
}

// AgentSnapshotToolOutput is a safe session snapshot response.
type AgentSnapshotToolOutput struct {
	Snapshot AgentSessionSnapshotSummary `json:"snapshot" jsonschema:"session snapshot"`
	Session  AgentSessionSummary         `json:"session" jsonschema:"agent session"`
}

// RunContextOutput is a bounded run context diagnostic response.
type RunContextOutput struct {
	Session AgentSessionSummary `json:"session" jsonschema:"agent session"`
	Runs    []AgentRunSummary   `json:"runs,omitempty" jsonschema:"agent runs"`
	Page    PageSummary         `json:"page" jsonschema:"page metadata"`
}

// HumanGateToolOutput is a safe human gate response.
type HumanGateToolOutput struct {
	HumanGate HumanGateSummary `json:"human_gate" jsonschema:"human gate request"`
}

// HumanGateListOutput is a safe human gate list response.
type HumanGateListOutput struct {
	HumanGates []HumanGateSummary `json:"human_gates" jsonschema:"human gate requests"`
	Page       PageSummary        `json:"page" jsonschema:"page metadata"`
}

// AgentSessionSummary is a value-safe summary of an agent session.
type AgentSessionSummary struct {
	ID                    string                       `json:"id" jsonschema:"session identifier"`
	Scope                 AgentScopeSummary            `json:"scope" jsonschema:"session scope"`
	ProviderWorkItemRef   string                       `json:"provider_work_item_ref,omitempty" jsonschema:"provider work item reference"`
	FlowVersionID         string                       `json:"flow_version_id,omitempty" jsonschema:"flow version identifier"`
	CurrentStageID        string                       `json:"current_stage_id,omitempty" jsonschema:"current stage identifier"`
	LatestStateSnapshotID string                       `json:"latest_state_snapshot_id,omitempty" jsonschema:"latest snapshot identifier"`
	Status                string                       `json:"status" jsonschema:"session status"`
	CreatedByActorRef     string                       `json:"created_by_actor_ref" jsonschema:"creator actor reference"`
	Version               int64                        `json:"version" jsonschema:"session version"`
	CreatedAt             string                       `json:"created_at" jsonschema:"created timestamp"`
	UpdatedAt             string                       `json:"updated_at" jsonschema:"updated timestamp"`
	LatestStateSnapshot   *AgentSessionSnapshotSummary `json:"latest_state_snapshot,omitempty" jsonschema:"latest state snapshot"`
}

// AgentRunSummary is a value-safe summary of an agent run.
type AgentRunSummary struct {
	ID                      string                     `json:"id" jsonschema:"run identifier"`
	SessionID               string                     `json:"session_id" jsonschema:"session identifier"`
	FlowVersionID           string                     `json:"flow_version_id,omitempty" jsonschema:"flow version identifier"`
	StageID                 string                     `json:"stage_id,omitempty" jsonschema:"stage identifier"`
	RoleProfileID           string                     `json:"role_profile_id" jsonschema:"role profile identifier"`
	RoleProfileVersion      int64                      `json:"role_profile_version" jsonschema:"role profile version"`
	PromptTemplateVersionID string                     `json:"prompt_template_version_id" jsonschema:"prompt template version identifier"`
	RuntimeContext          AgentRuntimeContextSummary `json:"runtime_context" jsonschema:"runtime context refs"`
	ProviderTarget          AgentProviderTargetSummary `json:"provider_target" jsonschema:"provider target refs"`
	Status                  string                     `json:"status" jsonschema:"run status"`
	ResultSummary           string                     `json:"result_summary,omitempty" jsonschema:"short safe result summary"`
	FailureCode             string                     `json:"failure_code,omitempty" jsonschema:"failure code"`
	Version                 int64                      `json:"version" jsonschema:"run version"`
	StartedAt               string                     `json:"started_at,omitempty" jsonschema:"started timestamp"`
	FinishedAt              string                     `json:"finished_at,omitempty" jsonschema:"finished timestamp"`
	CreatedAt               string                     `json:"created_at" jsonschema:"created timestamp"`
	UpdatedAt               string                     `json:"updated_at" jsonschema:"updated timestamp"`
}

// AgentSessionSnapshotSummary is a safe object reference for session state.
type AgentSessionSnapshotSummary struct {
	ID           string             `json:"id" jsonschema:"snapshot identifier"`
	SessionID    string             `json:"session_id" jsonschema:"session identifier"`
	RunID        string             `json:"run_id,omitempty" jsonschema:"run identifier"`
	SnapshotKind string             `json:"snapshot_kind" jsonschema:"snapshot kind"`
	TurnIndex    *int64             `json:"turn_index,omitempty" jsonschema:"turn index"`
	Object       AgentObjectSummary `json:"object" jsonschema:"object reference"`
	CapturedAt   string             `json:"captured_at" jsonschema:"captured timestamp"`
	CreatedAt    string             `json:"created_at" jsonschema:"created timestamp"`
}

// AgentScopeSummary is a safe scope reference.
type AgentScopeSummary struct {
	Type string `json:"type" jsonschema:"scope type"`
	Ref  string `json:"ref" jsonschema:"scope reference"`
}

// AgentProviderTargetSummary is a safe provider target reference set.
type AgentProviderTargetSummary = AgentProviderTargetInput

// AgentRuntimeContextSummary is a safe runtime context reference set.
type AgentRuntimeContextSummary = AgentRuntimeContextInput

// AgentObjectSummary is a safe object storage reference.
type AgentObjectSummary = AgentObjectInput

// HumanGateSummary is a safe owner-decision wait/result summary.
type HumanGateSummary struct {
	ID                       string                     `json:"id" jsonschema:"human gate request identifier"`
	SessionID                string                     `json:"session_id" jsonschema:"agent session identifier"`
	RunID                    string                     `json:"run_id,omitempty" jsonschema:"agent run identifier"`
	StageID                  string                     `json:"stage_id,omitempty" jsonschema:"flow stage identifier"`
	AcceptanceResultID       string                     `json:"acceptance_result_id,omitempty" jsonschema:"acceptance result identifier"`
	ProviderTarget           AgentProviderTargetSummary `json:"provider_target" jsonschema:"provider refs"`
	TargetRef                string                     `json:"target_ref,omitempty" jsonschema:"owner decision target ref"`
	RequestKind              string                     `json:"request_kind" jsonschema:"request kind"`
	ReasonCode               string                     `json:"reason_code" jsonschema:"reason code"`
	SafeSummary              string                     `json:"safe_summary,omitempty" jsonschema:"short safe summary"`
	InteractionRequestRef    string                     `json:"interaction_request_ref,omitempty" jsonschema:"interaction request ref"`
	InteractionResponseRef   string                     `json:"interaction_response_ref,omitempty" jsonschema:"interaction response ref"`
	GovernanceGateRequestRef string                     `json:"governance_gate_request_ref,omitempty" jsonschema:"governance gate request ref"`
	GovernanceDecisionRef    string                     `json:"governance_decision_ref,omitempty" jsonschema:"governance decision ref"`
	Status                   string                     `json:"status" jsonschema:"status"`
	Outcome                  string                     `json:"outcome" jsonschema:"outcome"`
	Version                  int64                      `json:"version" jsonschema:"version"`
	CreatedAt                string                     `json:"created_at" jsonschema:"created timestamp"`
	UpdatedAt                string                     `json:"updated_at" jsonschema:"updated timestamp"`
	ResolvedAt               string                     `json:"resolved_at,omitempty" jsonschema:"resolved timestamp"`
}

// PageSummary describes list continuation state.
type PageSummary struct {
	NextPageToken string `json:"next_page_token,omitempty" jsonschema:"next page token"`
}
