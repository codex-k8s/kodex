package mcptransport

import (
	"context"

	governancev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/governance/v1"
)

const (
	ToolGovernanceGateRequest        = "governance.gate.request"
	ToolGovernanceGateGet            = "governance.gate.get"
	ToolGovernanceGateList           = "governance.gate.list"
	ToolGovernanceGateSubmitDecision = "governance.gate.submit_decision"
	ToolGovernanceGateCancel         = "governance.gate.cancel"
	ToolGovernanceGateExpire         = "governance.gate.expire"
)

// GovernanceManagerClient is the owner route used by governance MCP tools.
type GovernanceManagerClient interface {
	GovernanceGateRequestClient
	GovernanceGateDecisionClient
	GovernanceGateTerminalClient
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

// GovernancePageInput limits list responses.
type GovernancePageInput = AgentPageInput

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
