package mcptransport

import (
	"context"

	governancev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/governance/v1"
)

const (
	ToolGovernanceRiskEvaluate   = "governance.risk.evaluate"
	ToolGovernanceRiskReevaluate = "governance.risk.reevaluate"
	ToolGovernanceRiskGet        = "governance.risk.get"
	ToolGovernanceRiskList       = "governance.risk.list"

	ToolGovernanceGateRequest        = "governance.gate.request"
	ToolGovernanceGateGet            = "governance.gate.get"
	ToolGovernanceGateList           = "governance.gate.list"
	ToolGovernanceGateSubmitDecision = "governance.gate.submit_decision"
	ToolGovernanceGateCancel         = "governance.gate.cancel"
	ToolGovernanceGateExpire         = "governance.gate.expire"
)

// GovernanceManagerClient is the owner route used by governance MCP tools.
type GovernanceManagerClient interface {
	GovernanceRiskAssessmentClient
	GovernanceGateRequestClient
	GovernanceGateDecisionClient
	GovernanceGateTerminalClient
}

// GovernanceRiskAssessmentClient evaluates and reads risk assessments.
type GovernanceRiskAssessmentClient interface {
	EvaluateRisk(context.Context, *governancev1.EvaluateRiskRequest) (*governancev1.RiskAssessmentResponse, error)
	ReevaluateRisk(context.Context, *governancev1.ReevaluateRiskRequest) (*governancev1.RiskAssessmentResponse, error)
	GetRiskAssessment(context.Context, *governancev1.GetRiskAssessmentRequest) (*governancev1.RiskAssessmentResponse, error)
	ListRiskAssessments(context.Context, *governancev1.ListRiskAssessmentsRequest) (*governancev1.ListRiskAssessmentsResponse, error)
}

// GovernanceGateRequestClient reads and creates gate requests.
type GovernanceGateRequestClient interface {
	RequestGate(context.Context, *governancev1.RequestGateRequest) (*governancev1.GateRequestResponse, error)
	GetGateRequest(context.Context, *governancev1.GetGateRequestRequest) (*governancev1.GateRequestResponse, error)
	ListGateRequests(context.Context, *governancev1.ListGateRequestsRequest) (*governancev1.ListGateRequestsResponse, error)
}

// GovernanceGateDecisionClient submits final gate decisions.
type GovernanceGateDecisionClient interface {
	SubmitGateDecision(context.Context, *governancev1.SubmitGateDecisionRequest) (*governancev1.GateDecisionResponse, error)
}

// GovernanceGateTerminalClient closes open gate requests.
type GovernanceGateTerminalClient interface {
	CancelGate(context.Context, *governancev1.CancelGateRequest) (*governancev1.GateRequestResponse, error)
	ExpireGate(context.Context, *governancev1.ExpireGateRequest) (*governancev1.GateRequestResponse, error)
}

// GovernanceCommandMetaInput carries safe command metadata for governance-manager tools.
type GovernanceCommandMetaInput struct {
	CommandID       string                        `json:"command_id,omitempty" jsonschema:"unique command identifier"`
	IdempotencyKey  string                        `json:"idempotency_key,omitempty" jsonschema:"idempotency key scoped by operation and actor"`
	ExpectedVersion *int64                        `json:"expected_version,omitempty" jsonschema:"expected aggregate version for optimistic concurrency"`
	Actor           GovernanceActorInput          `json:"actor" jsonschema:"authenticated caller"`
	Reason          string                        `json:"reason" jsonschema:"machine or operator reason for audit"`
	RequestID       string                        `json:"request_id" jsonschema:"request identifier for logs and audit"`
	RequestContext  GovernanceRequestContextInput `json:"request_context" jsonschema:"safe request context"`
}

// GovernanceQueryMetaInput carries safe read metadata for governance-manager tools.
type GovernanceQueryMetaInput struct {
	Actor          GovernanceActorInput          `json:"actor" jsonschema:"authenticated caller"`
	RequestID      string                        `json:"request_id" jsonschema:"request identifier for logs and traces"`
	RequestContext GovernanceRequestContextInput `json:"request_context" jsonschema:"safe request context"`
}

// GovernanceActorInput identifies a user, service, agent or external account.
type GovernanceActorInput struct {
	Type string `json:"type" jsonschema:"actor type such as user, service, agent or external_account"`
	ID   string `json:"id" jsonschema:"actor identifier in its owner domain"`
}

// GovernanceRequestContextInput carries safe metadata and never includes tokens or secrets.
type GovernanceRequestContextInput struct {
	Source       string `json:"source" jsonschema:"caller surface, for example platform-mcp-server"`
	TraceID      string `json:"trace_id,omitempty" jsonschema:"platform trace identifier"`
	SessionID    string `json:"session_id,omitempty" jsonschema:"user or agent session identifier"`
	ClientIPHash string `json:"client_ip_hash,omitempty" jsonschema:"hashed client address"`
}

// GovernanceTargetInput points to a governance target by safe reference.
type GovernanceTargetInput struct {
	Type string `json:"type" jsonschema:"target type: transition, pull_request, release_candidate, runtime_job, policy_change, document, merge, postdeploy or rollback"`
	Ref  string `json:"ref" jsonschema:"external or governance aggregate reference"`
}

// GovernanceInteractionDeliveryRefInput points to interaction-hub delivery facts.
type GovernanceInteractionDeliveryRefInput struct {
	RequestRef  string `json:"request_ref,omitempty" jsonschema:"interaction request reference"`
	DeliveryRef string `json:"delivery_ref,omitempty" jsonschema:"interaction delivery attempt reference"`
	CallbackRef string `json:"callback_ref,omitempty" jsonschema:"interaction callback reference"`
	DecisionRef string `json:"decision_ref,omitempty" jsonschema:"interaction delivery answer reference"`
}

// GovernanceEvidenceRefInput points to bounded evidence without embedding raw payloads.
type GovernanceEvidenceRefInput struct {
	Kind           string `json:"kind" jsonschema:"evidence kind such as provider_comment, runtime_summary, document or object_ref"`
	Ref            string `json:"ref" jsonschema:"safe source object or summary reference"`
	Summary        string `json:"summary" jsonschema:"short safe human-readable explanation"`
	Digest         string `json:"digest,omitempty" jsonschema:"optional integrity digest"`
	RetentionClass string `json:"retention_class,omitempty" jsonschema:"evidence retention class"`
}

// GovernanceProjectContextRefInput carries project-catalog refs without copying policy.
type GovernanceProjectContextRefInput struct {
	ProjectRef       string `json:"project_ref,omitempty" jsonschema:"project-catalog project reference"`
	RepositoryRef    string `json:"repository_ref,omitempty" jsonschema:"project-catalog repository binding reference"`
	ServiceRef       string `json:"service_ref,omitempty" jsonschema:"checked services.yaml service reference"`
	BranchRulesRef   string `json:"branch_rules_ref,omitempty" jsonschema:"project branch rules reference"`
	ReleasePolicyRef string `json:"release_policy_ref,omitempty" jsonschema:"project release policy reference"`
	ReleaseLineRef   string `json:"release_line_ref,omitempty" jsonschema:"project release line reference"`
}

// GovernanceProviderContextRefInput carries provider-hub projection refs.
type GovernanceProviderContextRefInput struct {
	WorkItemRef            string `json:"work_item_ref,omitempty" jsonschema:"provider work item projection reference"`
	PullRequestRef         string `json:"pull_request_ref,omitempty" jsonschema:"provider PR/MR projection reference"`
	CommentRef             string `json:"comment_ref,omitempty" jsonschema:"provider comment projection reference"`
	ReviewSignalRef        string `json:"review_signal_ref,omitempty" jsonschema:"provider review or check projection reference"`
	ProviderOperationRef   string `json:"provider_operation_ref,omitempty" jsonschema:"provider-hub operation reference"`
	ChangedFilesSummaryRef string `json:"changed_files_summary_ref,omitempty" jsonschema:"bounded changed files summary reference"`
}

// GovernanceAgentContextRefInput carries agent-manager refs.
type GovernanceAgentContextRefInput struct {
	SessionRef    string `json:"session_ref,omitempty" jsonschema:"agent-manager session reference"`
	RunRef        string `json:"run_ref,omitempty" jsonschema:"agent-manager run reference"`
	StageRef      string `json:"stage_ref,omitempty" jsonschema:"flow stage reference"`
	AcceptanceRef string `json:"acceptance_ref,omitempty" jsonschema:"agent acceptance reference"`
	RoleRef       string `json:"role_ref,omitempty" jsonschema:"agent role reference"`
}

// GovernanceRuntimeContextRefInput carries runtime-manager refs.
type GovernanceRuntimeContextRefInput struct {
	SlotRef        string `json:"slot_ref,omitempty" jsonschema:"runtime slot reference"`
	JobRef         string `json:"job_ref,omitempty" jsonschema:"platform job reference"`
	EnvironmentRef string `json:"environment_ref,omitempty" jsonschema:"runtime environment reference"`
	ArtifactRef    string `json:"artifact_ref,omitempty" jsonschema:"runtime artifact reference"`
	SummaryRef     string `json:"summary_ref,omitempty" jsonschema:"bounded runtime summary reference"`
}

// GovernanceRiskEvaluationSummaryInput carries bounded classifier inputs.
type GovernanceRiskEvaluationSummaryInput struct {
	ChangedFilesSummaryRef string                                `json:"changed_files_summary_ref,omitempty" jsonschema:"bounded changed files summary reference"`
	Summary                string                                `json:"summary,omitempty" jsonschema:"short safe classifier summary"`
	Factors                []GovernanceRiskEvaluationFactorInput `json:"factors,omitempty" jsonschema:"safe classifier factors"`
}

// GovernanceRiskEvaluationFactorInput is one safe input fact for the risk classifier.
type GovernanceRiskEvaluationFactorInput struct {
	SourceType string   `json:"source_type" jsonschema:"factor source: policy, changed_file, service, api, database, secret, release, runtime, review_signal or human_decision"`
	Ref        string   `json:"ref" jsonschema:"safe factor reference"`
	Summary    string   `json:"summary" jsonschema:"bounded safe factor summary"`
	Tags       []string `json:"tags,omitempty" jsonschema:"bounded classifier tags"`
}

// GovernancePageInput limits list responses.
type GovernancePageInput = AgentPageInput

type EvaluateGovernanceRiskInput struct {
	Meta              GovernanceCommandMetaInput           `json:"meta" jsonschema:"command metadata"`
	Target            GovernanceTargetInput                `json:"target" jsonschema:"risk target"`
	ProjectContext    GovernanceProjectContextRefInput     `json:"project_context,omitempty" jsonschema:"project-catalog refs"`
	ProviderContext   GovernanceProviderContextRefInput    `json:"provider_context,omitempty" jsonschema:"provider-hub refs"`
	AgentContext      GovernanceAgentContextRefInput       `json:"agent_context,omitempty" jsonschema:"agent-manager refs"`
	RuntimeContext    GovernanceRuntimeContextRefInput     `json:"runtime_context,omitempty" jsonschema:"runtime-manager refs"`
	EvidenceRefs      []GovernanceEvidenceRefInput         `json:"evidence_refs,omitempty" jsonschema:"bounded evidence refs"`
	RiskProfileRef    string                               `json:"risk_profile_ref,omitempty" jsonschema:"governance risk profile id or ref"`
	EvaluationSummary GovernanceRiskEvaluationSummaryInput `json:"evaluation_summary,omitempty" jsonschema:"bounded classifier input snapshot"`
}

type ReevaluateGovernanceRiskInput struct {
	Meta               GovernanceCommandMetaInput           `json:"meta" jsonschema:"command metadata"`
	RiskAssessmentID   string                               `json:"risk_assessment_id" jsonschema:"risk assessment identifier"`
	NewEvidenceRefs    []GovernanceEvidenceRefInput         `json:"new_evidence_refs,omitempty" jsonschema:"new bounded evidence refs"`
	ReevaluationReason string                               `json:"reevaluation_reason" jsonschema:"bounded reevaluation reason"`
	EvaluationSummary  GovernanceRiskEvaluationSummaryInput `json:"evaluation_summary,omitempty" jsonschema:"replacement bounded classifier input snapshot"`
	RiskProfileRef     string                               `json:"risk_profile_ref,omitempty" jsonschema:"governance risk profile id or ref"`
}

type GetGovernanceRiskAssessmentInput struct {
	Meta                 GovernanceQueryMetaInput `json:"meta" jsonschema:"query metadata"`
	RiskAssessmentID     string                   `json:"risk_assessment_id" jsonschema:"risk assessment identifier"`
	IncludeFactors       bool                     `json:"include_factors,omitempty" jsonschema:"include bounded risk factors"`
	IncludeReviewSignals bool                     `json:"include_review_signals,omitempty" jsonschema:"include review signal count from owner response"`
}

type ListGovernanceRiskAssessmentsInput struct {
	Meta               GovernanceQueryMetaInput         `json:"meta" jsonschema:"query metadata"`
	Target             GovernanceTargetInput            `json:"target,omitempty" jsonschema:"target filter and authorization context"`
	ProjectContext     GovernanceProjectContextRefInput `json:"project_context,omitempty" jsonschema:"project or repository filter and authorization context"`
	EffectiveRiskClass string                           `json:"effective_risk_class,omitempty" jsonschema:"risk class filter: r0, r1, r2 or r3"`
	Status             string                           `json:"status,omitempty" jsonschema:"assessment status filter: draft, active, superseded or closed"`
	Page               GovernancePageInput              `json:"page,omitempty" jsonschema:"page request"`
}

type RequestGovernanceGateInput struct {
	Meta                   GovernanceCommandMetaInput            `json:"meta" jsonschema:"command metadata"`
	RiskAssessmentID       string                                `json:"risk_assessment_id,omitempty" jsonschema:"risk assessment identifier"`
	GatePolicyID           string                                `json:"gate_policy_id,omitempty" jsonschema:"gate policy identifier"`
	Target                 GovernanceTargetInput                 `json:"target" jsonschema:"gate target"`
	InteractionDeliveryRef GovernanceInteractionDeliveryRefInput `json:"interaction_delivery_ref,omitempty" jsonschema:"interaction-hub delivery refs"`
	EvidenceRefs           []GovernanceEvidenceRefInput          `json:"evidence_refs,omitempty" jsonschema:"bounded evidence refs"`
	EvidenceSummary        string                                `json:"evidence_summary" jsonschema:"short safe evidence summary"`
	DeadlineAt             string                                `json:"deadline_at,omitempty" jsonschema:"RFC3339 deadline timestamp"`
}

type GetGovernanceGateInput struct {
	Meta            GovernanceQueryMetaInput `json:"meta" jsonschema:"query metadata"`
	GateRequestID   string                   `json:"gate_request_id" jsonschema:"gate request identifier"`
	IncludeDecision bool                     `json:"include_decision,omitempty" jsonschema:"include resolved decision summary when present"`
}

type ListGovernanceGatesInput struct {
	Meta             GovernanceQueryMetaInput `json:"meta" jsonschema:"query metadata"`
	RiskAssessmentID string                   `json:"risk_assessment_id,omitempty" jsonschema:"risk assessment filter and authorization context"`
	Target           GovernanceTargetInput    `json:"target,omitempty" jsonschema:"target filter and authorization context"`
	Status           string                   `json:"status,omitempty" jsonschema:"gate request status filter"`
	Page             GovernancePageInput      `json:"page,omitempty" jsonschema:"page request"`
}

type SubmitGovernanceGateDecisionInput struct {
	Meta                   GovernanceCommandMetaInput            `json:"meta" jsonschema:"command metadata"`
	GateRequestID          string                                `json:"gate_request_id" jsonschema:"gate request identifier"`
	DecisionActorRef       string                                `json:"decision_actor_ref" jsonschema:"actor reference that made the decision"`
	DecisionPolicyRef      string                                `json:"decision_policy_ref" jsonschema:"policy or actor version used for validation"`
	Outcome                string                                `json:"outcome" jsonschema:"outcome: approve, approve_with_conditions, revise, reject, hold, rollback or escalate"`
	Reason                 string                                `json:"reason" jsonschema:"bounded safe decision reason"`
	ConditionsSummary      string                                `json:"conditions_summary,omitempty" jsonschema:"bounded safe follow-up summary"`
	InteractionDeliveryRef GovernanceInteractionDeliveryRefInput `json:"interaction_delivery_ref,omitempty" jsonschema:"interaction-hub delivery refs"`
}

type CancelGovernanceGateInput struct {
	Meta                   GovernanceCommandMetaInput            `json:"meta" jsonschema:"command metadata"`
	GateRequestID          string                                `json:"gate_request_id" jsonschema:"gate request identifier"`
	Reason                 string                                `json:"reason" jsonschema:"bounded safe cancellation reason"`
	InteractionDeliveryRef GovernanceInteractionDeliveryRefInput `json:"interaction_delivery_ref,omitempty" jsonschema:"interaction-hub delivery refs"`
}

type ExpireGovernanceGateInput = CancelGovernanceGateInput

// GovernanceRiskAssessmentOutput is a safe risk assessment response.
type GovernanceRiskAssessmentOutput struct {
	RiskAssessment    GovernanceRiskAssessmentSummary `json:"risk_assessment" jsonschema:"risk assessment"`
	RiskFactors       []GovernanceRiskFactorSummary   `json:"risk_factors,omitempty" jsonschema:"bounded risk factors"`
	ReviewSignalCount int                             `json:"review_signal_count,omitempty" jsonschema:"review signal count returned by owner"`
}

// GovernanceRiskAssessmentListOutput is a safe risk assessment list response.
type GovernanceRiskAssessmentListOutput struct {
	RiskAssessments []GovernanceRiskAssessmentSummary `json:"risk_assessments" jsonschema:"risk assessments"`
	Page            PageSummary                       `json:"page" jsonschema:"page metadata"`
}

// GovernanceRiskAssessmentSummary is a bounded risk assessment summary.
type GovernanceRiskAssessmentSummary struct {
	ID                 string                             `json:"id" jsonschema:"risk assessment identifier"`
	Target             GovernanceTargetSummary            `json:"target" jsonschema:"risk target"`
	ProjectContext     GovernanceProjectContextSummary    `json:"project_context" jsonschema:"project-catalog refs"`
	ProviderContext    GovernanceProviderContextSummary   `json:"provider_context" jsonschema:"provider-hub refs"`
	AgentContext       GovernanceAgentContextSummary      `json:"agent_context" jsonschema:"agent-manager refs"`
	RuntimeContext     GovernanceRuntimeContextSummary    `json:"runtime_context" jsonschema:"runtime-manager refs"`
	InitialRiskClass   string                             `json:"initial_risk_class" jsonschema:"initial risk class"`
	EffectiveRiskClass string                             `json:"effective_risk_class" jsonschema:"effective risk class"`
	Status             string                             `json:"status" jsonschema:"assessment status"`
	Summary            string                             `json:"summary,omitempty" jsonschema:"bounded safe explanation"`
	RequiredGates      []GovernanceRequiredGateSummary    `json:"required_gates,omitempty" jsonschema:"required gates"`
	RequiredGateCount  int                                `json:"required_gate_count" jsonschema:"required gate count"`
	RequiredGateRefs   []string                           `json:"required_gate_refs,omitempty" jsonschema:"required gate policy refs"`
	MatchedRuleRefs    []string                           `json:"matched_rule_refs,omitempty" jsonschema:"matched rule refs from bounded factors"`
	MatchedRuleCount   int                                `json:"matched_rule_count" jsonschema:"matched rule count"`
	RiskFactorCount    int                                `json:"risk_factor_count" jsonschema:"bounded factor count returned by owner"`
	RiskProfileID      string                             `json:"risk_profile_id,omitempty" jsonschema:"risk profile identifier"`
	RiskProfileVersion int64                              `json:"risk_profile_version,omitempty" jsonschema:"risk profile version"`
	EvaluationSummary  GovernanceEvaluationSummarySummary `json:"evaluation_summary" jsonschema:"bounded evaluation summary"`
	EvidenceRefs       []GovernanceEvidenceSummary        `json:"evidence_refs,omitempty" jsonschema:"bounded evidence refs"`
	Version            int64                              `json:"version" jsonschema:"assessment version"`
	CreatedAt          string                             `json:"created_at" jsonschema:"created timestamp"`
	UpdatedAt          string                             `json:"updated_at" jsonschema:"updated timestamp"`
}

// GovernanceRequiredGateSummary is a bounded required gate summary.
type GovernanceRequiredGateSummary struct {
	GatePolicyID string `json:"gate_policy_id" jsonschema:"gate policy identifier"`
	GateKind     string `json:"gate_kind" jsonschema:"gate kind"`
	MinRiskClass string `json:"min_risk_class" jsonschema:"minimum risk class"`
	Reason       string `json:"reason,omitempty" jsonschema:"bounded safe reason"`
}

// GovernanceRiskFactorSummary is a bounded risk factor summary.
type GovernanceRiskFactorSummary struct {
	ID               string `json:"id" jsonschema:"risk factor identifier"`
	RiskAssessmentID string `json:"risk_assessment_id" jsonschema:"risk assessment identifier"`
	SourceType       string `json:"source_type" jsonschema:"factor source type"`
	SourceRef        string `json:"source_ref,omitempty" jsonschema:"safe source reference"`
	RiskClass        string `json:"risk_class" jsonschema:"contributed risk class"`
	Summary          string `json:"summary,omitempty" jsonschema:"bounded safe explanation"`
	CreatedAt        string `json:"created_at" jsonschema:"created timestamp"`
}

// GovernanceEvaluationSummarySummary is a safe classifier input summary.
type GovernanceEvaluationSummarySummary struct {
	ChangedFilesSummaryRef string `json:"changed_files_summary_ref,omitempty" jsonschema:"bounded changed files summary reference"`
	Summary                string `json:"summary,omitempty" jsonschema:"bounded classifier summary"`
	FactorCount            int    `json:"factor_count" jsonschema:"bounded classifier factor count"`
}

// GovernanceProjectContextSummary is a safe project context reference set.
type GovernanceProjectContextSummary = GovernanceProjectContextRefInput

// GovernanceProviderContextSummary is a safe provider context reference set.
type GovernanceProviderContextSummary = GovernanceProviderContextRefInput

// GovernanceAgentContextSummary is a safe agent context reference set.
type GovernanceAgentContextSummary = GovernanceAgentContextRefInput

// GovernanceRuntimeContextSummary is a safe runtime context reference set.
type GovernanceRuntimeContextSummary = GovernanceRuntimeContextRefInput

// GovernanceGateOutput is a safe gate request response.
type GovernanceGateOutput struct {
	GateRequest  GovernanceGateRequestSummary   `json:"gate_request" jsonschema:"gate request"`
	GateDecision *GovernanceGateDecisionSummary `json:"gate_decision,omitempty" jsonschema:"gate decision"`
}

// GovernanceGateDecisionOutput is a safe gate decision response.
type GovernanceGateDecisionOutput struct {
	GateDecision GovernanceGateDecisionSummary `json:"gate_decision" jsonschema:"gate decision"`
	GateRequest  GovernanceGateRequestSummary  `json:"gate_request" jsonschema:"gate request"`
}

// GovernanceGateListOutput is a safe gate request list response.
type GovernanceGateListOutput struct {
	GateRequests []GovernanceGateRequestSummary `json:"gate_requests" jsonschema:"gate requests"`
	Page         PageSummary                    `json:"page" jsonschema:"page metadata"`
}

// GovernanceGateRequestSummary is a value-safe summary of a governance gate request.
type GovernanceGateRequestSummary struct {
	ID                     string                               `json:"id" jsonschema:"gate request identifier"`
	RiskAssessmentID       string                               `json:"risk_assessment_id,omitempty" jsonschema:"risk assessment identifier"`
	GatePolicyID           string                               `json:"gate_policy_id,omitempty" jsonschema:"gate policy identifier"`
	Target                 GovernanceTargetSummary              `json:"target" jsonschema:"gate target"`
	InteractionDeliveryRef GovernanceInteractionDeliverySummary `json:"interaction_delivery_ref" jsonschema:"interaction delivery refs"`
	EvidenceRefs           []GovernanceEvidenceSummary          `json:"evidence_refs,omitempty" jsonschema:"bounded evidence refs"`
	EvidenceSummary        string                               `json:"evidence_summary,omitempty" jsonschema:"short safe evidence summary"`
	Status                 string                               `json:"status" jsonschema:"gate lifecycle status"`
	Version                int64                                `json:"version" jsonschema:"gate request version"`
	CreatedAt              string                               `json:"created_at" jsonschema:"created timestamp"`
	UpdatedAt              string                               `json:"updated_at" jsonschema:"updated timestamp"`
	TerminalActorRef       string                               `json:"terminal_actor_ref,omitempty" jsonschema:"terminal actor reference"`
	TerminalReason         string                               `json:"terminal_reason,omitempty" jsonschema:"bounded terminal reason"`
	TerminalAt             string                               `json:"terminal_at,omitempty" jsonschema:"terminal timestamp"`
}

// GovernanceGateDecisionSummary is a value-safe summary of a governance gate decision.
type GovernanceGateDecisionSummary struct {
	ID                string `json:"id" jsonschema:"gate decision identifier"`
	GateRequestID     string `json:"gate_request_id" jsonschema:"gate request identifier"`
	DecisionActorRef  string `json:"decision_actor_ref" jsonschema:"decision actor reference"`
	DecisionPolicyRef string `json:"decision_policy_ref" jsonschema:"decision policy reference"`
	Outcome           string `json:"outcome" jsonschema:"gate decision outcome"`
	Reason            string `json:"reason" jsonschema:"bounded safe reason"`
	ConditionsSummary string `json:"conditions_summary,omitempty" jsonschema:"bounded safe conditions summary"`
	SourceRef         string `json:"source_ref,omitempty" jsonschema:"safe source reference"`
	DecidedAt         string `json:"decided_at" jsonschema:"decision timestamp"`
}

// GovernanceTargetSummary is a safe governance target reference.
type GovernanceTargetSummary = GovernanceTargetInput

// GovernanceInteractionDeliverySummary is a safe interaction delivery reference.
type GovernanceInteractionDeliverySummary = GovernanceInteractionDeliveryRefInput

// GovernanceEvidenceSummary is a safe evidence reference.
type GovernanceEvidenceSummary = GovernanceEvidenceRefInput
