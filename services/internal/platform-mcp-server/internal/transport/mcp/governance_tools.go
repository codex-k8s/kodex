package mcptransport

import (
	"context"
	"strings"

	governancev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/governance/v1"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type governanceEnumPair[Enum comparable] struct {
	name  string
	value Enum
}

var governanceToolDescriptions = map[string]string{
	ToolGovernanceRiskEvaluate:   "Evaluate risk through governance-manager from safe refs and summaries.",
	ToolGovernanceRiskReevaluate: "Reevaluate an existing risk assessment through governance-manager.",
	ToolGovernanceRiskGet:        "Read a safe risk assessment summary through governance-manager.",
	ToolGovernanceRiskList:       "List safe risk assessment summaries through governance-manager.",

	ToolGovernanceGateRequest:        "Request a governance gate through governance-manager without storing decision state in MCP.",
	ToolGovernanceGateGet:            "Read a safe governance gate request summary through governance-manager.",
	ToolGovernanceGateList:           "List safe governance gate request summaries through governance-manager.",
	ToolGovernanceGateSubmitDecision: "Submit a governance gate decision through governance-manager.",
	ToolGovernanceGateCancel:         "Cancel an open governance gate request through governance-manager.",
	ToolGovernanceGateExpire:         "Expire an open governance gate request through governance-manager.",

	ToolGovernanceReleasePrepareDecisionPackage: "Prepare a release decision package through governance-manager from safe refs.",
	ToolGovernanceReleaseGetDecisionPackage:     "Read a safe release decision package summary through governance-manager.",
	ToolGovernanceReleaseListDecisionPackages:   "List safe release decision package summaries through governance-manager.",
	ToolGovernanceReleaseRequestDecision:        "Request a release decision through governance-manager.",
	ToolGovernanceReleaseSubmitDecision:         "Submit a release decision through governance-manager.",
	ToolGovernanceReleaseGetDecision:            "Read a safe release decision summary through governance-manager.",
	ToolGovernanceReleaseListDecisions:          "List safe release decision summaries through governance-manager.",
	ToolGovernanceReleaseRecordBlockingSignal:   "Record a release blocking signal through governance-manager.",
	ToolGovernanceReleaseResolveBlockingSignal:  "Resolve a release blocking signal through governance-manager.",
	ToolGovernanceReleaseListBlockingSignals:    "List safe release blocking signal summaries through governance-manager.",
	ToolGovernanceReleaseRecordSafetyState:      "Record release safety-loop state through governance-manager.",
	ToolGovernanceReleaseGetSafetyState:         "Read release safety-loop state through governance-manager.",
	ToolGovernanceSignalRecordReview:            "Записать review signal через governance-manager без хранения состояния в MCP.",
	ToolGovernanceSignalListReview:              "Прочитать безопасные сводки review signals через governance-manager.",
}

var governanceTargetTypes = map[string]governancev1.GovernanceTargetType{
	"transition":        governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_TRANSITION,
	"pull_request":      governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_PULL_REQUEST,
	"release_candidate": governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_RELEASE_CANDIDATE,
	"runtime_job":       governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_RUNTIME_JOB,
	"policy_change":     governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_POLICY_CHANGE,
	"document":          governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_DOCUMENT,
	"merge":             governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_MERGE,
	"postdeploy":        governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_POSTDEPLOY,
	"rollback":          governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_ROLLBACK,
}

var governanceTargetTypeNames = map[governancev1.GovernanceTargetType]string{
	governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_TRANSITION:        "transition",
	governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_PULL_REQUEST:      "pull_request",
	governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_RELEASE_CANDIDATE: "release_candidate",
	governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_RUNTIME_JOB:       "runtime_job",
	governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_POLICY_CHANGE:     "policy_change",
	governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_DOCUMENT:          "document",
	governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_MERGE:             "merge",
	governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_POSTDEPLOY:        "postdeploy",
	governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_ROLLBACK:          "rollback",
}

var governanceEvidenceKindPairs = []governanceEnumPair[governancev1.EvidenceKind]{
	{name: "provider_comment", value: governancev1.EvidenceKind_EVIDENCE_KIND_PROVIDER_COMMENT},
	{name: "provider_review", value: governancev1.EvidenceKind_EVIDENCE_KIND_PROVIDER_REVIEW},
	{name: "provider_check", value: governancev1.EvidenceKind_EVIDENCE_KIND_PROVIDER_CHECK},
	{name: "runtime_summary", value: governancev1.EvidenceKind_EVIDENCE_KIND_RUNTIME_SUMMARY},
	{name: "document", value: governancev1.EvidenceKind_EVIDENCE_KIND_DOCUMENT},
	{name: "risk_factor", value: governancev1.EvidenceKind_EVIDENCE_KIND_RISK_FACTOR},
	{name: "review_signal", value: governancev1.EvidenceKind_EVIDENCE_KIND_REVIEW_SIGNAL},
	{name: "interaction_callback", value: governancev1.EvidenceKind_EVIDENCE_KIND_INTERACTION_CALLBACK},
	{name: "object_ref", value: governancev1.EvidenceKind_EVIDENCE_KIND_OBJECT_REF},
	{name: "custom", value: governancev1.EvidenceKind_EVIDENCE_KIND_CUSTOM},
}

var governanceEvidenceKinds = governanceEnumValues(governanceEvidenceKindPairs)
var governanceEvidenceKindNames = governanceEnumNames(governanceEvidenceKindPairs)

var governanceRiskClasses = map[string]governancev1.RiskClass{
	"r0": governancev1.RiskClass_RISK_CLASS_R0,
	"r1": governancev1.RiskClass_RISK_CLASS_R1,
	"r2": governancev1.RiskClass_RISK_CLASS_R2,
	"r3": governancev1.RiskClass_RISK_CLASS_R3,
}

var governanceRiskClassNames = map[governancev1.RiskClass]string{
	governancev1.RiskClass_RISK_CLASS_R0: "r0",
	governancev1.RiskClass_RISK_CLASS_R1: "r1",
	governancev1.RiskClass_RISK_CLASS_R2: "r2",
	governancev1.RiskClass_RISK_CLASS_R3: "r3",
}

var governanceRiskAssessmentStatuses = map[string]governancev1.RiskAssessmentStatus{
	"draft":      governancev1.RiskAssessmentStatus_RISK_ASSESSMENT_STATUS_DRAFT,
	"active":     governancev1.RiskAssessmentStatus_RISK_ASSESSMENT_STATUS_ACTIVE,
	"superseded": governancev1.RiskAssessmentStatus_RISK_ASSESSMENT_STATUS_SUPERSEDED,
	"closed":     governancev1.RiskAssessmentStatus_RISK_ASSESSMENT_STATUS_CLOSED,
}

var governanceRiskAssessmentStatusNames = map[governancev1.RiskAssessmentStatus]string{
	governancev1.RiskAssessmentStatus_RISK_ASSESSMENT_STATUS_DRAFT:      "draft",
	governancev1.RiskAssessmentStatus_RISK_ASSESSMENT_STATUS_ACTIVE:     "active",
	governancev1.RiskAssessmentStatus_RISK_ASSESSMENT_STATUS_SUPERSEDED: "superseded",
	governancev1.RiskAssessmentStatus_RISK_ASSESSMENT_STATUS_CLOSED:     "closed",
}

var governanceRiskFactorSourceTypeNames = map[governancev1.RiskFactorSourceType]string{
	governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_POLICY:         "policy",
	governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_CHANGED_FILE:   "changed_file",
	governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_SERVICE:        "service",
	governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_API:            "api",
	governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_DATABASE:       "database",
	governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_SECRET:         "secret",
	governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_RELEASE:        "release",
	governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_RUNTIME:        "runtime",
	governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_REVIEW_SIGNAL:  "review_signal",
	governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_HUMAN_DECISION: "human_decision",
}

var governanceGateKindNames = map[governancev1.GateKind]string{
	governancev1.GateKind_GATE_KIND_PRODUCT:      "product",
	governancev1.GateKind_GATE_KIND_ARCHITECTURE: "architecture",
	governancev1.GateKind_GATE_KIND_TECHNICAL:    "technical",
	governancev1.GateKind_GATE_KIND_QA:           "qa",
	governancev1.GateKind_GATE_KIND_RELEASE:      "release",
	governancev1.GateKind_GATE_KIND_POSTDEPLOY:   "postdeploy",
	governancev1.GateKind_GATE_KIND_EMERGENCY:    "emergency",
	governancev1.GateKind_GATE_KIND_CUSTOM:       "custom",
}

var governanceGateStatuses = map[string]governancev1.GateRequestStatus{
	"requested":         governancev1.GateRequestStatus_GATE_REQUEST_STATUS_REQUESTED,
	"delivering":        governancev1.GateRequestStatus_GATE_REQUEST_STATUS_DELIVERING,
	"awaiting_decision": governancev1.GateRequestStatus_GATE_REQUEST_STATUS_AWAITING_DECISION,
	"resolved":          governancev1.GateRequestStatus_GATE_REQUEST_STATUS_RESOLVED,
	"expired":           governancev1.GateRequestStatus_GATE_REQUEST_STATUS_EXPIRED,
	"cancelled":         governancev1.GateRequestStatus_GATE_REQUEST_STATUS_CANCELLED,
}

var governanceGateStatusNames = map[governancev1.GateRequestStatus]string{
	governancev1.GateRequestStatus_GATE_REQUEST_STATUS_REQUESTED:         "requested",
	governancev1.GateRequestStatus_GATE_REQUEST_STATUS_DELIVERING:        "delivering",
	governancev1.GateRequestStatus_GATE_REQUEST_STATUS_AWAITING_DECISION: "awaiting_decision",
	governancev1.GateRequestStatus_GATE_REQUEST_STATUS_RESOLVED:          "resolved",
	governancev1.GateRequestStatus_GATE_REQUEST_STATUS_EXPIRED:           "expired",
	governancev1.GateRequestStatus_GATE_REQUEST_STATUS_CANCELLED:         "cancelled",
}

var governanceGateOutcomes = map[string]governancev1.GateOutcome{
	"approve":                 governancev1.GateOutcome_GATE_OUTCOME_APPROVE,
	"approve_with_conditions": governancev1.GateOutcome_GATE_OUTCOME_APPROVE_WITH_CONDITIONS,
	"revise":                  governancev1.GateOutcome_GATE_OUTCOME_REVISE,
	"reject":                  governancev1.GateOutcome_GATE_OUTCOME_REJECT,
	"hold":                    governancev1.GateOutcome_GATE_OUTCOME_HOLD,
	"rollback":                governancev1.GateOutcome_GATE_OUTCOME_ROLLBACK,
	"escalate":                governancev1.GateOutcome_GATE_OUTCOME_ESCALATE,
}

var governanceGateOutcomeNames = map[governancev1.GateOutcome]string{
	governancev1.GateOutcome_GATE_OUTCOME_APPROVE:                 "approve",
	governancev1.GateOutcome_GATE_OUTCOME_APPROVE_WITH_CONDITIONS: "approve_with_conditions",
	governancev1.GateOutcome_GATE_OUTCOME_REVISE:                  "revise",
	governancev1.GateOutcome_GATE_OUTCOME_REJECT:                  "reject",
	governancev1.GateOutcome_GATE_OUTCOME_HOLD:                    "hold",
	governancev1.GateOutcome_GATE_OUTCOME_ROLLBACK:                "rollback",
	governancev1.GateOutcome_GATE_OUTCOME_ESCALATE:                "escalate",
}

var governanceReleasePackageStatusPairs = []governanceEnumPair[governancev1.ReleaseDecisionPackageStatus]{
	{name: "draft", value: governancev1.ReleaseDecisionPackageStatus_RELEASE_DECISION_PACKAGE_STATUS_DRAFT},
	{name: "ready", value: governancev1.ReleaseDecisionPackageStatus_RELEASE_DECISION_PACKAGE_STATUS_READY},
	{name: "decision_requested", value: governancev1.ReleaseDecisionPackageStatus_RELEASE_DECISION_PACKAGE_STATUS_DECISION_REQUESTED},
	{name: "closed", value: governancev1.ReleaseDecisionPackageStatus_RELEASE_DECISION_PACKAGE_STATUS_CLOSED},
}

var governanceReleasePackageStatuses = governanceEnumValues(governanceReleasePackageStatusPairs)
var governanceReleasePackageStatusNames = governanceEnumNames(governanceReleasePackageStatusPairs)

var governanceReleaseDecisionStatusPairs = []governanceEnumPair[governancev1.ReleaseDecisionStatus]{
	{name: "requested", value: governancev1.ReleaseDecisionStatus_RELEASE_DECISION_STATUS_REQUESTED},
	{name: "resolved", value: governancev1.ReleaseDecisionStatus_RELEASE_DECISION_STATUS_RESOLVED},
	{name: "cancelled", value: governancev1.ReleaseDecisionStatus_RELEASE_DECISION_STATUS_CANCELLED},
}

var governanceReleaseDecisionStatuses = governanceEnumValues(governanceReleaseDecisionStatusPairs)
var governanceReleaseDecisionStatusNames = governanceEnumNames(governanceReleaseDecisionStatusPairs)

var governanceReleaseDecisionOutcomePairs = []governanceEnumPair[governancev1.ReleaseDecisionOutcome]{
	{name: "go", value: governancev1.ReleaseDecisionOutcome_RELEASE_DECISION_OUTCOME_GO},
	{name: "go_with_conditions", value: governancev1.ReleaseDecisionOutcome_RELEASE_DECISION_OUTCOME_GO_WITH_CONDITIONS},
	{name: "no_go", value: governancev1.ReleaseDecisionOutcome_RELEASE_DECISION_OUTCOME_NO_GO},
	{name: "hold", value: governancev1.ReleaseDecisionOutcome_RELEASE_DECISION_OUTCOME_HOLD},
	{name: "rollback", value: governancev1.ReleaseDecisionOutcome_RELEASE_DECISION_OUTCOME_ROLLBACK},
	{name: "follow_up_required", value: governancev1.ReleaseDecisionOutcome_RELEASE_DECISION_OUTCOME_FOLLOW_UP_REQUIRED},
}

var governanceReleaseDecisionOutcomes = governanceEnumValues(governanceReleaseDecisionOutcomePairs)
var governanceReleaseDecisionOutcomeNames = governanceEnumNames(governanceReleaseDecisionOutcomePairs)

var governanceReleaseSafetyStatePairs = []governanceEnumPair[governancev1.ReleaseSafetyStateKind]{
	{name: "release_candidate", value: governancev1.ReleaseSafetyStateKind_RELEASE_SAFETY_STATE_KIND_RELEASE_CANDIDATE},
	{name: "awaiting_release_gate", value: governancev1.ReleaseSafetyStateKind_RELEASE_SAFETY_STATE_KIND_AWAITING_RELEASE_GATE},
	{name: "deploying", value: governancev1.ReleaseSafetyStateKind_RELEASE_SAFETY_STATE_KIND_DEPLOYING},
	{name: "postdeploy_observation", value: governancev1.ReleaseSafetyStateKind_RELEASE_SAFETY_STATE_KIND_POSTDEPLOY_OBSERVATION},
	{name: "stable", value: governancev1.ReleaseSafetyStateKind_RELEASE_SAFETY_STATE_KIND_STABLE},
	{name: "hold", value: governancev1.ReleaseSafetyStateKind_RELEASE_SAFETY_STATE_KIND_HOLD},
	{name: "rollback", value: governancev1.ReleaseSafetyStateKind_RELEASE_SAFETY_STATE_KIND_ROLLBACK},
	{name: "follow_up_required", value: governancev1.ReleaseSafetyStateKind_RELEASE_SAFETY_STATE_KIND_FOLLOW_UP_REQUIRED},
}

var governanceReleaseSafetyStates = governanceEnumValues(governanceReleaseSafetyStatePairs)
var governanceReleaseSafetyStateNames = governanceEnumNames(governanceReleaseSafetyStatePairs)

var governanceBlockingSignalSourcePairs = []governanceEnumPair[governancev1.BlockingSignalSourceType]{
	{name: "acceptance", value: governancev1.BlockingSignalSourceType_BLOCKING_SIGNAL_SOURCE_TYPE_ACCEPTANCE},
	{name: "review_signal", value: governancev1.BlockingSignalSourceType_BLOCKING_SIGNAL_SOURCE_TYPE_REVIEW_SIGNAL},
	{name: "runtime", value: governancev1.BlockingSignalSourceType_BLOCKING_SIGNAL_SOURCE_TYPE_RUNTIME},
	{name: "provider", value: governancev1.BlockingSignalSourceType_BLOCKING_SIGNAL_SOURCE_TYPE_PROVIDER},
	{name: "interaction", value: governancev1.BlockingSignalSourceType_BLOCKING_SIGNAL_SOURCE_TYPE_INTERACTION},
	{name: "human", value: governancev1.BlockingSignalSourceType_BLOCKING_SIGNAL_SOURCE_TYPE_HUMAN},
	{name: "monitoring", value: governancev1.BlockingSignalSourceType_BLOCKING_SIGNAL_SOURCE_TYPE_MONITORING},
}

var governanceBlockingSignalSources = governanceEnumValues(governanceBlockingSignalSourcePairs)
var governanceBlockingSignalSourceNames = governanceEnumNames(governanceBlockingSignalSourcePairs)

var governanceBlockingSignalStatusPairs = []governanceEnumPair[governancev1.BlockingSignalStatus]{
	{name: "active", value: governancev1.BlockingSignalStatus_BLOCKING_SIGNAL_STATUS_ACTIVE},
	{name: "resolved", value: governancev1.BlockingSignalStatus_BLOCKING_SIGNAL_STATUS_RESOLVED},
	{name: "dismissed", value: governancev1.BlockingSignalStatus_BLOCKING_SIGNAL_STATUS_DISMISSED},
}

var governanceBlockingSignalStatuses = governanceEnumValues(governanceBlockingSignalStatusPairs)
var governanceBlockingSignalStatusNames = governanceEnumNames(governanceBlockingSignalStatusPairs)

var governanceSignalSeverityPairs = []governanceEnumPair[governancev1.SignalSeverity]{
	{name: "info", value: governancev1.SignalSeverity_SIGNAL_SEVERITY_INFO},
	{name: "warning", value: governancev1.SignalSeverity_SIGNAL_SEVERITY_WARNING},
	{name: "blocking", value: governancev1.SignalSeverity_SIGNAL_SEVERITY_BLOCKING},
	{name: "critical", value: governancev1.SignalSeverity_SIGNAL_SEVERITY_CRITICAL},
}

var governanceSignalSeverities = governanceEnumValues(governanceSignalSeverityPairs)
var governanceSignalSeverityNames = governanceEnumNames(governanceSignalSeverityPairs)

var governanceReviewRoleKinds = map[string]governancev1.ReviewRoleKind{
	"reviewer":           governancev1.ReviewRoleKind_REVIEW_ROLE_KIND_REVIEWER,
	"qa":                 governancev1.ReviewRoleKind_REVIEW_ROLE_KIND_QA,
	"lexical_gatekeeper": governancev1.ReviewRoleKind_REVIEW_ROLE_KIND_LEXICAL_GATEKEEPER,
	"risk_gatekeeper":    governancev1.ReviewRoleKind_REVIEW_ROLE_KIND_RISK_GATEKEEPER,
	"sre":                governancev1.ReviewRoleKind_REVIEW_ROLE_KIND_SRE,
	"security":           governancev1.ReviewRoleKind_REVIEW_ROLE_KIND_SECURITY,
	"owner":              governancev1.ReviewRoleKind_REVIEW_ROLE_KIND_OWNER,
	"custom":             governancev1.ReviewRoleKind_REVIEW_ROLE_KIND_CUSTOM,
}

var governanceReviewRoleKindNames = map[governancev1.ReviewRoleKind]string{
	governancev1.ReviewRoleKind_REVIEW_ROLE_KIND_REVIEWER:           "reviewer",
	governancev1.ReviewRoleKind_REVIEW_ROLE_KIND_QA:                 "qa",
	governancev1.ReviewRoleKind_REVIEW_ROLE_KIND_LEXICAL_GATEKEEPER: "lexical_gatekeeper",
	governancev1.ReviewRoleKind_REVIEW_ROLE_KIND_RISK_GATEKEEPER:    "risk_gatekeeper",
	governancev1.ReviewRoleKind_REVIEW_ROLE_KIND_SRE:                "sre",
	governancev1.ReviewRoleKind_REVIEW_ROLE_KIND_SECURITY:           "security",
	governancev1.ReviewRoleKind_REVIEW_ROLE_KIND_OWNER:              "owner",
	governancev1.ReviewRoleKind_REVIEW_ROLE_KIND_CUSTOM:             "custom",
}

var governanceReviewSignalOutcomes = map[string]governancev1.ReviewSignalOutcome{
	"pass":            governancev1.ReviewSignalOutcome_REVIEW_SIGNAL_OUTCOME_PASS,
	"pass_with_notes": governancev1.ReviewSignalOutcome_REVIEW_SIGNAL_OUTCOME_PASS_WITH_NOTES,
	"block":           governancev1.ReviewSignalOutcome_REVIEW_SIGNAL_OUTCOME_BLOCK,
	"request_changes": governancev1.ReviewSignalOutcome_REVIEW_SIGNAL_OUTCOME_REQUEST_CHANGES,
	"raise_risk":      governancev1.ReviewSignalOutcome_REVIEW_SIGNAL_OUTCOME_RAISE_RISK,
	"informational":   governancev1.ReviewSignalOutcome_REVIEW_SIGNAL_OUTCOME_INFORMATIONAL,
}

var governanceReviewSignalOutcomeNames = map[governancev1.ReviewSignalOutcome]string{
	governancev1.ReviewSignalOutcome_REVIEW_SIGNAL_OUTCOME_PASS:            "pass",
	governancev1.ReviewSignalOutcome_REVIEW_SIGNAL_OUTCOME_PASS_WITH_NOTES: "pass_with_notes",
	governancev1.ReviewSignalOutcome_REVIEW_SIGNAL_OUTCOME_BLOCK:           "block",
	governancev1.ReviewSignalOutcome_REVIEW_SIGNAL_OUTCOME_REQUEST_CHANGES: "request_changes",
	governancev1.ReviewSignalOutcome_REVIEW_SIGNAL_OUTCOME_RAISE_RISK:      "raise_risk",
	governancev1.ReviewSignalOutcome_REVIEW_SIGNAL_OUTCOME_INFORMATIONAL:   "informational",
}

var governanceConfidencePairs = []governanceEnumPair[governancev1.Confidence]{
	{name: "low", value: governancev1.Confidence_CONFIDENCE_LOW},
	{name: "medium", value: governancev1.Confidence_CONFIDENCE_MEDIUM},
	{name: "high", value: governancev1.Confidence_CONFIDENCE_HIGH},
}

var governanceConfidences = governanceEnumValues(governanceConfidencePairs)
var governanceConfidenceNames = governanceEnumNames(governanceConfidencePairs)

// GovernanceToolsHandler routes governance tools to governance-manager.
type GovernanceToolsHandler struct {
	client GovernanceManagerClient
}

type governanceRiskCommandRequest interface {
	GetMeta() *governancev1.CommandMeta
}

// NewGovernanceToolsHandler creates a governance tool handler.
func NewGovernanceToolsHandler(client GovernanceManagerClient) *GovernanceToolsHandler {
	return &GovernanceToolsHandler{client: client}
}

func (handler *GovernanceToolsHandler) EvaluateRisk(ctx context.Context, _ *mcpsdk.CallToolRequest, input EvaluateGovernanceRiskInput) (*mcpsdk.CallToolResult, GovernanceRiskAssessmentOutput, error) {
	return routeRiskCommand(ctx, handler, input, evaluateRiskRequest, handler.client.EvaluateRisk, ToolGovernanceRiskEvaluate)
}

func (handler *GovernanceToolsHandler) ReevaluateRisk(ctx context.Context, _ *mcpsdk.CallToolRequest, input ReevaluateGovernanceRiskInput) (*mcpsdk.CallToolResult, GovernanceRiskAssessmentOutput, error) {
	return routeRiskCommand(ctx, handler, input, reevaluateRiskRequest, handler.client.ReevaluateRisk, ToolGovernanceRiskReevaluate)
}

func (handler *GovernanceToolsHandler) GetRiskAssessment(ctx context.Context, _ *mcpsdk.CallToolRequest, input GetGovernanceRiskAssessmentInput) (*mcpsdk.CallToolResult, GovernanceRiskAssessmentOutput, error) {
	return routeOwnerTool(ctx, input, getRiskAssessmentRequest, handler.client.GetRiskAssessment, governanceRiskAssessmentOutput, ToolGovernanceRiskGet)
}

func (handler *GovernanceToolsHandler) ListRiskAssessments(ctx context.Context, _ *mcpsdk.CallToolRequest, input ListGovernanceRiskAssessmentsInput) (*mcpsdk.CallToolResult, GovernanceRiskAssessmentListOutput, error) {
	var empty GovernanceRiskAssessmentListOutput
	request, err := listRiskAssessmentsRequest(input)
	if err != nil {
		return nil, empty, err
	}
	response, err := handler.client.ListRiskAssessments(ctx, request)
	if err != nil {
		return nil, empty, ownerToolError(ToolGovernanceRiskList, err)
	}
	factorsByAssessmentID, err := handler.riskFactorsByAssessmentID(ctx, response.GetRiskAssessments(), request.GetMeta(), ToolGovernanceRiskList)
	if err != nil {
		return nil, empty, err
	}
	return nil, governanceRiskAssessmentListOutput(response, factorsByAssessmentID), nil
}

func (handler *GovernanceToolsHandler) RecordReviewSignal(ctx context.Context, _ *mcpsdk.CallToolRequest, input RecordGovernanceReviewSignalInput) (*mcpsdk.CallToolResult, GovernanceReviewSignalOutput, error) {
	return routeOwnerTool(ctx, input, recordReviewSignalRequest, handler.client.RecordReviewSignal, governanceReviewSignalOutput, ToolGovernanceSignalRecordReview)
}

func (handler *GovernanceToolsHandler) ListReviewSignals(ctx context.Context, _ *mcpsdk.CallToolRequest, input ListGovernanceReviewSignalsInput) (*mcpsdk.CallToolResult, GovernanceReviewSignalListOutput, error) {
	return routeOwnerTool(ctx, input, listReviewSignalsRequest, handler.client.ListReviewSignals, governanceReviewSignalListOutput, ToolGovernanceSignalListReview)
}

func (handler *GovernanceToolsHandler) RequestGate(ctx context.Context, _ *mcpsdk.CallToolRequest, input RequestGovernanceGateInput) (*mcpsdk.CallToolResult, GovernanceGateOutput, error) {
	return routeOwnerTool(ctx, input, requestGateRequest, handler.client.RequestGate, governanceGateOutput, ToolGovernanceGateRequest)
}

func (handler *GovernanceToolsHandler) GetGateRequest(ctx context.Context, _ *mcpsdk.CallToolRequest, input GetGovernanceGateInput) (*mcpsdk.CallToolResult, GovernanceGateOutput, error) {
	return routeOwnerTool(ctx, input, getGateRequest, handler.client.GetGateRequest, governanceGateOutput, ToolGovernanceGateGet)
}

func (handler *GovernanceToolsHandler) ListGateRequests(ctx context.Context, _ *mcpsdk.CallToolRequest, input ListGovernanceGatesInput) (*mcpsdk.CallToolResult, GovernanceGateListOutput, error) {
	return routeOwnerTool(ctx, input, listGateRequests, handler.client.ListGateRequests, governanceGateListOutput, ToolGovernanceGateList)
}

func (handler *GovernanceToolsHandler) SubmitGateDecision(ctx context.Context, _ *mcpsdk.CallToolRequest, input SubmitGovernanceGateDecisionInput) (*mcpsdk.CallToolResult, GovernanceGateDecisionOutput, error) {
	return routeOwnerTool(ctx, input, submitGateDecisionRequest, handler.client.SubmitGateDecision, governanceGateDecisionOutput, ToolGovernanceGateSubmitDecision)
}

func (handler *GovernanceToolsHandler) CancelGate(ctx context.Context, _ *mcpsdk.CallToolRequest, input CancelGovernanceGateInput) (*mcpsdk.CallToolResult, GovernanceGateOutput, error) {
	return routeOwnerTool(ctx, input, cancelGateRequest, handler.client.CancelGate, governanceGateOutput, ToolGovernanceGateCancel)
}

func (handler *GovernanceToolsHandler) ExpireGate(ctx context.Context, _ *mcpsdk.CallToolRequest, input ExpireGovernanceGateInput) (*mcpsdk.CallToolResult, GovernanceGateOutput, error) {
	return routeOwnerTool(ctx, input, expireGateRequest, handler.client.ExpireGate, governanceGateOutput, ToolGovernanceGateExpire)
}

func (handler *GovernanceToolsHandler) PrepareReleaseDecisionPackage(ctx context.Context, _ *mcpsdk.CallToolRequest, input PrepareGovernanceReleaseDecisionPackageInput) (*mcpsdk.CallToolResult, GovernanceReleaseDecisionPackageOutput, error) {
	return routeOwnerTool(ctx, input, prepareReleaseDecisionPackageRequest, handler.client.BuildReleaseDecisionPackage, governanceReleaseDecisionPackageOutput, ToolGovernanceReleasePrepareDecisionPackage)
}

func (handler *GovernanceToolsHandler) GetReleaseDecisionPackage(ctx context.Context, _ *mcpsdk.CallToolRequest, input GetGovernanceReleaseDecisionPackageInput) (*mcpsdk.CallToolResult, GovernanceReleaseDecisionPackageOutput, error) {
	return routeOwnerTool(ctx, input, getReleaseDecisionPackageRequest, handler.client.GetReleaseDecisionPackage, governanceReleaseDecisionPackageOutput, ToolGovernanceReleaseGetDecisionPackage)
}

func (handler *GovernanceToolsHandler) ListReleaseDecisionPackages(ctx context.Context, _ *mcpsdk.CallToolRequest, input ListGovernanceReleaseDecisionPackagesInput) (*mcpsdk.CallToolResult, GovernanceReleaseDecisionPackageListOutput, error) {
	return routeOwnerTool(ctx, input, listReleaseDecisionPackagesRequest, handler.client.ListReleaseDecisionPackages, governanceReleaseDecisionPackageListOutput, ToolGovernanceReleaseListDecisionPackages)
}

func (handler *GovernanceToolsHandler) RequestReleaseDecision(ctx context.Context, _ *mcpsdk.CallToolRequest, input RequestGovernanceReleaseDecisionInput) (*mcpsdk.CallToolResult, GovernanceReleaseDecisionOutput, error) {
	return routeOwnerTool(ctx, input, requestReleaseDecisionRequest, handler.client.RequestReleaseDecision, governanceReleaseDecisionOutput, ToolGovernanceReleaseRequestDecision)
}

func (handler *GovernanceToolsHandler) SubmitReleaseDecision(ctx context.Context, _ *mcpsdk.CallToolRequest, input SubmitGovernanceReleaseDecisionInput) (*mcpsdk.CallToolResult, GovernanceReleaseDecisionOutput, error) {
	return routeOwnerTool(ctx, input, submitReleaseDecisionRequest, handler.client.SubmitReleaseDecision, governanceReleaseDecisionOutput, ToolGovernanceReleaseSubmitDecision)
}

func (handler *GovernanceToolsHandler) GetReleaseDecision(ctx context.Context, _ *mcpsdk.CallToolRequest, input GetGovernanceReleaseDecisionInput) (*mcpsdk.CallToolResult, GovernanceReleaseDecisionOutput, error) {
	return routeOwnerTool(ctx, input, getReleaseDecisionRequest, handler.client.GetReleaseDecision, governanceReleaseDecisionOutput, ToolGovernanceReleaseGetDecision)
}

func (handler *GovernanceToolsHandler) ListReleaseDecisions(ctx context.Context, _ *mcpsdk.CallToolRequest, input ListGovernanceReleaseDecisionsInput) (*mcpsdk.CallToolResult, GovernanceReleaseDecisionListOutput, error) {
	return routeOwnerTool(ctx, input, listReleaseDecisionsRequest, handler.client.ListReleaseDecisions, governanceReleaseDecisionListOutput, ToolGovernanceReleaseListDecisions)
}

func (handler *GovernanceToolsHandler) RecordBlockingSignal(ctx context.Context, _ *mcpsdk.CallToolRequest, input RecordGovernanceBlockingSignalInput) (*mcpsdk.CallToolResult, GovernanceBlockingSignalOutput, error) {
	return routeOwnerTool(ctx, input, recordBlockingSignalRequest, handler.client.RecordBlockingSignal, governanceBlockingSignalOutput, ToolGovernanceReleaseRecordBlockingSignal)
}

func (handler *GovernanceToolsHandler) ResolveBlockingSignal(ctx context.Context, _ *mcpsdk.CallToolRequest, input ResolveGovernanceBlockingSignalInput) (*mcpsdk.CallToolResult, GovernanceBlockingSignalOutput, error) {
	return routeOwnerTool(ctx, input, resolveBlockingSignalRequest, handler.client.ResolveBlockingSignal, governanceBlockingSignalOutput, ToolGovernanceReleaseResolveBlockingSignal)
}

func (handler *GovernanceToolsHandler) ListBlockingSignals(ctx context.Context, _ *mcpsdk.CallToolRequest, input ListGovernanceBlockingSignalsInput) (*mcpsdk.CallToolResult, GovernanceBlockingSignalListOutput, error) {
	return routeOwnerTool(ctx, input, listBlockingSignalsRequest, handler.client.ListBlockingSignals, governanceBlockingSignalListOutput, ToolGovernanceReleaseListBlockingSignals)
}

func (handler *GovernanceToolsHandler) RecordReleaseSafetyState(ctx context.Context, _ *mcpsdk.CallToolRequest, input RecordGovernanceReleaseSafetyStateInput) (*mcpsdk.CallToolResult, GovernanceReleaseSafetyStateOutput, error) {
	return routeOwnerTool(ctx, input, recordReleaseSafetyStateRequest, handler.client.RecordReleaseSafetyState, governanceReleaseSafetyStateOutput, ToolGovernanceReleaseRecordSafetyState)
}

func (handler *GovernanceToolsHandler) GetReleaseSafetyState(ctx context.Context, _ *mcpsdk.CallToolRequest, input GetGovernanceReleaseSafetyStateInput) (*mcpsdk.CallToolResult, GovernanceReleaseSafetyStateOutput, error) {
	return routeOwnerTool(ctx, input, getReleaseSafetyStateRequest, handler.client.GetReleaseSafetyState, governanceReleaseSafetyStateOutput, ToolGovernanceReleaseGetSafetyState)
}

func routeRiskCommand[Input any, Request governanceRiskCommandRequest](
	ctx context.Context,
	handler *GovernanceToolsHandler,
	input Input,
	build func(Input) (Request, error),
	call func(context.Context, Request) (*governancev1.RiskAssessmentResponse, error),
	tool string,
) (*mcpsdk.CallToolResult, GovernanceRiskAssessmentOutput, error) {
	var empty GovernanceRiskAssessmentOutput
	request, err := build(input)
	if err != nil {
		return nil, empty, err
	}
	response, err := call(ctx, request)
	if err != nil {
		return nil, empty, ownerToolError(tool, err)
	}
	enriched, err := handler.riskAssessmentWithFactors(ctx, response, governanceQueryMetaFromCommand(request.GetMeta()), true, tool)
	if err != nil {
		return nil, empty, err
	}
	return nil, governanceRiskAssessmentOutput(enriched), nil
}

func (handler *GovernanceToolsHandler) riskAssessmentWithFactors(
	ctx context.Context,
	response *governancev1.RiskAssessmentResponse,
	meta *governancev1.QueryMeta,
	includeReviewSignals bool,
	tool string,
) (*governancev1.RiskAssessmentResponse, error) {
	assessmentID := riskAssessmentID(response.GetRiskAssessment())
	if assessmentID == "" {
		return response, nil
	}
	enriched, err := handler.client.GetRiskAssessment(ctx, &governancev1.GetRiskAssessmentRequest{
		RiskAssessmentId:     assessmentID,
		IncludeFactors:       true,
		IncludeReviewSignals: includeReviewSignals,
		Meta:                 meta,
	})
	if err != nil {
		return nil, ownerToolError(tool, err)
	}
	if enriched.GetRiskAssessment() == nil {
		return response, nil
	}
	return enriched, nil
}

func (handler *GovernanceToolsHandler) riskFactorsByAssessmentID(
	ctx context.Context,
	assessments []*governancev1.RiskAssessment,
	meta *governancev1.QueryMeta,
	tool string,
) (map[string][]*governancev1.RiskFactor, error) {
	if len(assessments) == 0 {
		return nil, nil
	}
	result := make(map[string][]*governancev1.RiskFactor, len(assessments))
	for _, assessment := range assessments {
		assessmentID := riskAssessmentID(assessment)
		if assessmentID == "" {
			continue
		}
		enriched, err := handler.riskAssessmentWithFactors(ctx, &governancev1.RiskAssessmentResponse{RiskAssessment: assessment}, meta, false, tool)
		if err != nil {
			return nil, err
		}
		result[assessmentID] = enriched.GetRiskFactors()
	}
	return result, nil
}

func evaluateRiskRequest(input EvaluateGovernanceRiskInput) (*governancev1.EvaluateRiskRequest, error) {
	meta, err := governanceCommandMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	target, err := governanceTarget(input.Target, true, "target")
	if err != nil {
		return nil, err
	}
	evidenceRefs, err := governanceEvidenceRefs(input.EvidenceRefs)
	if err != nil {
		return nil, err
	}
	evaluationSummary, err := governanceRiskEvaluationSummary(input.EvaluationSummary)
	if err != nil {
		return nil, err
	}
	return &governancev1.EvaluateRiskRequest{
		Target:            target,
		ProjectContext:    governanceProjectContext(input.ProjectContext, true),
		ProviderContext:   governanceProviderContext(input.ProviderContext),
		AgentContext:      governanceAgentContext(input.AgentContext),
		RuntimeContext:    governanceRuntimeContext(input.RuntimeContext),
		EvidenceRefs:      evidenceRefs,
		RiskProfileRef:    optionalString(input.RiskProfileRef),
		Meta:              meta,
		EvaluationSummary: evaluationSummary,
	}, nil
}

func reevaluateRiskRequest(input ReevaluateGovernanceRiskInput) (*governancev1.ReevaluateRiskRequest, error) {
	meta, err := governanceCommandMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	riskAssessmentID, err := requiredTrimmed(input.RiskAssessmentID, "risk_assessment_id")
	if err != nil {
		return nil, err
	}
	evidenceRefs, err := governanceEvidenceRefs(input.NewEvidenceRefs)
	if err != nil {
		return nil, err
	}
	reevaluationReason, err := requiredTrimmed(input.ReevaluationReason, "reevaluation_reason")
	if err != nil {
		return nil, err
	}
	evaluationSummary, err := governanceRiskEvaluationSummary(input.EvaluationSummary)
	if err != nil {
		return nil, err
	}
	return &governancev1.ReevaluateRiskRequest{
		RiskAssessmentId:   riskAssessmentID,
		NewEvidenceRefs:    evidenceRefs,
		ReevaluationReason: reevaluationReason,
		Meta:               meta,
		EvaluationSummary:  evaluationSummary,
		RiskProfileRef:     optionalString(input.RiskProfileRef),
	}, nil
}

func getRiskAssessmentRequest(input GetGovernanceRiskAssessmentInput) (*governancev1.GetRiskAssessmentRequest, error) {
	meta, err := governanceQueryMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	riskAssessmentID, err := requiredTrimmed(input.RiskAssessmentID, "risk_assessment_id")
	if err != nil {
		return nil, err
	}
	return &governancev1.GetRiskAssessmentRequest{
		RiskAssessmentId:     riskAssessmentID,
		IncludeFactors:       input.IncludeFactors,
		IncludeReviewSignals: input.IncludeReviewSignals,
		Meta:                 meta,
	}, nil
}

func listRiskAssessmentsRequest(input ListGovernanceRiskAssessmentsInput) (*governancev1.ListRiskAssessmentsRequest, error) {
	meta, err := governanceQueryMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	target, err := governanceTarget(input.Target, false, "target")
	if err != nil {
		return nil, err
	}
	projectContext := governanceProjectContext(input.ProjectContext, false)
	if target == nil && !governanceProjectContextHasListScope(input.ProjectContext) {
		return nil, invalidInput("target or project_context.project_ref/repository_ref is required")
	}
	riskClass, err := optionalGovernanceRiskClass(input.EffectiveRiskClass)
	if err != nil {
		return nil, err
	}
	status, err := optionalGovernanceRiskAssessmentStatus(input.Status)
	if err != nil {
		return nil, err
	}
	return &governancev1.ListRiskAssessmentsRequest{
		Target:             target,
		ProjectContext:     projectContext,
		EffectiveRiskClass: riskClass,
		Status:             status,
		Page:               governancePageRequest(input.Page),
		Meta:               meta,
	}, nil
}

func recordReviewSignalRequest(input RecordGovernanceReviewSignalInput) (*governancev1.RecordReviewSignalRequest, error) {
	meta, err := governanceCommandMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	target, err := governanceTarget(input.Target, true, "target")
	if err != nil {
		return nil, err
	}
	roleKind, err := governanceReviewRoleKind(input.RoleKind)
	if err != nil {
		return nil, err
	}
	authorRef, err := requiredTrimmed(input.AuthorRef, "author_ref")
	if err != nil {
		return nil, err
	}
	outcome, err := governanceReviewSignalOutcome(input.Outcome)
	if err != nil {
		return nil, err
	}
	severity, err := governanceSignalSeverity(input.Severity)
	if err != nil {
		return nil, err
	}
	confidence, err := optionalGovernanceConfidence(input.Confidence)
	if err != nil {
		return nil, err
	}
	evidenceRefs, err := governanceEvidenceRefs(input.EvidenceRefs)
	if err != nil {
		return nil, err
	}
	summary, err := requiredTrimmed(input.Summary, "summary")
	if err != nil {
		return nil, err
	}
	return &governancev1.RecordReviewSignalRequest{
		RiskAssessmentId: optionalString(input.RiskAssessmentID),
		Target:           target,
		RoleKind:         roleKind,
		AuthorRef:        authorRef,
		Outcome:          outcome,
		Severity:         severity,
		Confidence:       confidence,
		EvidenceRefs:     evidenceRefs,
		Summary:          summary,
		Meta:             meta,
	}, nil
}

func listReviewSignalsRequest(input ListGovernanceReviewSignalsInput) (*governancev1.ListReviewSignalsRequest, error) {
	meta, err := governanceQueryMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	target, err := governanceTarget(input.Target, true, "target")
	if err != nil {
		return nil, err
	}
	roleKind, err := optionalGovernanceReviewRoleKind(input.RoleKind)
	if err != nil {
		return nil, err
	}
	outcome, err := optionalGovernanceReviewSignalOutcome(input.Outcome)
	if err != nil {
		return nil, err
	}
	return &governancev1.ListReviewSignalsRequest{
		RiskAssessmentId: optionalString(input.RiskAssessmentID),
		Target:           target,
		RoleKind:         roleKind,
		Outcome:          outcome,
		Page:             governancePageRequest(input.Page),
		Meta:             meta,
	}, nil
}

func requestGateRequest(input RequestGovernanceGateInput) (*governancev1.RequestGateRequest, error) {
	meta, err := governanceCommandMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	target, err := governanceTarget(input.Target, true, "target")
	if err != nil {
		return nil, err
	}
	evidenceRefs, err := governanceEvidenceRefs(input.EvidenceRefs)
	if err != nil {
		return nil, err
	}
	evidenceSummary, err := requiredTrimmed(input.EvidenceSummary, "evidence_summary")
	if err != nil {
		return nil, err
	}
	return &governancev1.RequestGateRequest{
		RiskAssessmentId:       optionalString(input.RiskAssessmentID),
		GatePolicyId:           optionalString(input.GatePolicyID),
		Target:                 target,
		InteractionDeliveryRef: governanceInteractionDeliveryRef(input.InteractionDeliveryRef),
		EvidenceRefs:           evidenceRefs,
		EvidenceSummary:        evidenceSummary,
		DeadlineAt:             optionalString(input.DeadlineAt),
		Meta:                   meta,
	}, nil
}

func getGateRequest(input GetGovernanceGateInput) (*governancev1.GetGateRequestRequest, error) {
	return governanceQueryRequestWithID(input.Meta, input.GateRequestID, "gate_request_id", func(id string, meta *governancev1.QueryMeta) *governancev1.GetGateRequestRequest {
		return &governancev1.GetGateRequestRequest{GateRequestId: id, IncludeDecision: input.IncludeDecision, Meta: meta}
	})
}

func listGateRequests(input ListGovernanceGatesInput) (*governancev1.ListGateRequestsRequest, error) {
	meta, err := governanceQueryMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	target, err := governanceTarget(input.Target, false, "target")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.RiskAssessmentID) == "" && target == nil {
		return nil, invalidInput("risk_assessment_id or target is required")
	}
	status, err := optionalGovernanceGateStatus(input.Status)
	if err != nil {
		return nil, err
	}
	return &governancev1.ListGateRequestsRequest{
		RiskAssessmentId: optionalString(input.RiskAssessmentID),
		Target:           target,
		Status:           status,
		Page:             governancePageRequest(input.Page),
		Meta:             meta,
	}, nil
}

func submitGateDecisionRequest(input SubmitGovernanceGateDecisionInput) (*governancev1.SubmitGateDecisionRequest, error) {
	meta, err := governanceCommandMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	gateRequestID, err := requiredTrimmed(input.GateRequestID, "gate_request_id")
	if err != nil {
		return nil, err
	}
	decisionActorRef, err := requiredTrimmed(input.DecisionActorRef, "decision_actor_ref")
	if err != nil {
		return nil, err
	}
	decisionPolicyRef, err := requiredTrimmed(input.DecisionPolicyRef, "decision_policy_ref")
	if err != nil {
		return nil, err
	}
	outcome, err := governanceGateOutcome(input.Outcome)
	if err != nil {
		return nil, err
	}
	reason, err := requiredTrimmed(input.Reason, "reason")
	if err != nil {
		return nil, err
	}
	return &governancev1.SubmitGateDecisionRequest{
		GateRequestId:          gateRequestID,
		DecisionActorRef:       decisionActorRef,
		DecisionPolicyRef:      decisionPolicyRef,
		Outcome:                outcome,
		Reason:                 reason,
		ConditionsSummary:      optionalString(input.ConditionsSummary),
		InteractionDeliveryRef: governanceInteractionDeliveryRef(input.InteractionDeliveryRef),
		Meta:                   meta,
	}, nil
}

func cancelGateRequest(input CancelGovernanceGateInput) (*governancev1.CancelGateRequest, error) {
	return terminalGovernanceGateRequest(input, func(gateRequestID string, reason string, meta *governancev1.CommandMeta, ref *governancev1.InteractionDeliveryRef) *governancev1.CancelGateRequest {
		return &governancev1.CancelGateRequest{
			GateRequestId:          gateRequestID,
			Reason:                 reason,
			InteractionDeliveryRef: ref,
			Meta:                   meta,
		}
	})
}

func expireGateRequest(input ExpireGovernanceGateInput) (*governancev1.ExpireGateRequest, error) {
	gateRequestID, reason, meta, ref, err := terminalGovernanceGateInput(input)
	if err != nil {
		return nil, err
	}
	request := &governancev1.ExpireGateRequest{Meta: meta}
	request.GateRequestId = gateRequestID
	request.Reason = reason
	request.InteractionDeliveryRef = ref
	return request, nil
}

func terminalGovernanceGateRequest[Request any](
	input CancelGovernanceGateInput,
	build func(string, string, *governancev1.CommandMeta, *governancev1.InteractionDeliveryRef) Request,
) (Request, error) {
	var zero Request
	gateRequestID, reason, meta, ref, err := terminalGovernanceGateInput(input)
	if err != nil {
		return zero, err
	}
	return build(gateRequestID, reason, meta, ref), nil
}

func terminalGovernanceGateInput(input CancelGovernanceGateInput) (string, string, *governancev1.CommandMeta, *governancev1.InteractionDeliveryRef, error) {
	meta, err := governanceCommandMeta(input.Meta)
	if err != nil {
		return "", "", nil, nil, err
	}
	gateRequestID, err := requiredTrimmed(input.GateRequestID, "gate_request_id")
	if err != nil {
		return "", "", nil, nil, err
	}
	reason, err := requiredTrimmed(input.Reason, "reason")
	if err != nil {
		return "", "", nil, nil, err
	}
	return gateRequestID, reason, meta, governanceInteractionDeliveryRef(input.InteractionDeliveryRef), nil
}

func prepareReleaseDecisionPackageRequest(input PrepareGovernanceReleaseDecisionPackageInput) (*governancev1.BuildReleaseDecisionPackageRequest, error) {
	meta, err := governanceCommandMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	releaseCandidateRef, err := requiredTrimmed(input.ReleaseCandidateRef, "release_candidate_ref")
	if err != nil {
		return nil, err
	}
	if !governanceProjectContextHasListScope(input.ProjectContext) {
		return nil, invalidInput("project_context.project_ref or project_context.repository_ref is required")
	}
	evidenceRefs, err := governanceEvidenceRefs(input.EvidenceRefs)
	if err != nil {
		return nil, err
	}
	return &governancev1.BuildReleaseDecisionPackageRequest{
		ReleaseCandidateRef:     releaseCandidateRef,
		ProjectContext:          governanceProjectContext(input.ProjectContext, true),
		RepositoryRefs:          trimmedStrings(input.RepositoryRefs),
		ProviderRefs:            governanceProviderContexts(input.ProviderRefs),
		RuntimeRefs:             governanceRuntimeContexts(input.RuntimeRefs),
		AgentContext:            governanceAgentContext(input.AgentContext),
		ReviewSignalIds:         trimmedStrings(input.ReviewSignalIDs),
		EvidenceRefs:            evidenceRefs,
		KnownLimitationsSummary: strings.TrimSpace(input.KnownLimitationsSummary),
		Meta:                    meta,
		RiskAssessmentId:        optionalString(input.RiskAssessmentID),
	}, nil
}

func getReleaseDecisionPackageRequest(input GetGovernanceReleaseDecisionPackageInput) (*governancev1.GetReleaseDecisionPackageRequest, error) {
	return governanceQueryRequestWithID(input.Meta, input.ReleaseDecisionPackageID, "release_decision_package_id", newGetReleaseDecisionPackageRequest)
}

func newGetReleaseDecisionPackageRequest(id string, meta *governancev1.QueryMeta) *governancev1.GetReleaseDecisionPackageRequest {
	return &governancev1.GetReleaseDecisionPackageRequest{ReleaseDecisionPackageId: id, Meta: meta}
}

func listReleaseDecisionPackagesRequest(input ListGovernanceReleaseDecisionPackagesInput) (*governancev1.ListReleaseDecisionPackagesRequest, error) {
	meta, err := governanceQueryMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	releaseCandidateRef := strings.TrimSpace(input.ReleaseCandidateRef)
	if releaseCandidateRef == "" && !governanceProjectContextHasListScope(input.ProjectContext) {
		return nil, invalidInput("release_candidate_ref or project_context.project_ref/repository_ref is required")
	}
	status, err := optionalGovernanceReleasePackageStatus(input.Status)
	if err != nil {
		return nil, err
	}
	return &governancev1.ListReleaseDecisionPackagesRequest{
		ProjectContext:      governanceProjectContext(input.ProjectContext, false),
		ReleaseCandidateRef: optionalString(releaseCandidateRef),
		Status:              status,
		Page:                governancePageRequest(input.Page),
		Meta:                meta,
	}, nil
}

func requestReleaseDecisionRequest(input RequestGovernanceReleaseDecisionInput) (*governancev1.RequestReleaseDecisionRequest, error) {
	build := func(id string, meta *governancev1.CommandMeta) *governancev1.RequestReleaseDecisionRequest {
		return &governancev1.RequestReleaseDecisionRequest{ReleaseDecisionPackageId: id, RequestGateIfRequired: input.RequestGateIfRequired, Meta: meta}
	}
	return governanceCommandRequestWithID(input.Meta, input.ReleaseDecisionPackageID, "release_decision_package_id", build)
}

func submitReleaseDecisionRequest(input SubmitGovernanceReleaseDecisionInput) (*governancev1.SubmitReleaseDecisionRequest, error) {
	meta, err := governanceCommandMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	packageID, actorRef, policyRef, reason, err := releaseDecisionRequiredFields(input)
	if err != nil {
		return nil, err
	}
	outcome, err := governanceReleaseDecisionOutcome(input.Outcome)
	if err != nil {
		return nil, err
	}
	return &governancev1.SubmitReleaseDecisionRequest{
		ReleaseDecisionPackageId: packageID,
		GateDecisionId:           optionalString(input.GateDecisionID),
		Outcome:                  outcome,
		DecisionActorRef:         actorRef,
		DecisionPolicyRef:        policyRef,
		Reason:                   reason,
		ConditionsSummary:        optionalString(input.ConditionsSummary),
		Meta:                     meta,
	}, nil
}

func releaseDecisionRequiredFields(input SubmitGovernanceReleaseDecisionInput) (string, string, string, string, error) {
	packageID, err := requiredTrimmed(input.ReleaseDecisionPackageID, "release_decision_package_id")
	if err != nil {
		return "", "", "", "", err
	}
	actorRef, err := requiredTrimmed(input.DecisionActorRef, "decision_actor_ref")
	if err != nil {
		return "", "", "", "", err
	}
	policyRef, err := requiredTrimmed(input.DecisionPolicyRef, "decision_policy_ref")
	if err != nil {
		return "", "", "", "", err
	}
	reason, err := requiredTrimmed(input.Reason, "reason")
	if err != nil {
		return "", "", "", "", err
	}
	return packageID, actorRef, policyRef, reason, nil
}

func getReleaseDecisionRequest(input GetGovernanceReleaseDecisionInput) (*governancev1.GetReleaseDecisionRequest, error) {
	return governanceQueryRequestWithID(input.Meta, input.ReleaseDecisionID, "release_decision_id", newGetReleaseDecisionRequest)
}

func newGetReleaseDecisionRequest(id string, meta *governancev1.QueryMeta) *governancev1.GetReleaseDecisionRequest {
	return &governancev1.GetReleaseDecisionRequest{ReleaseDecisionId: id, Meta: meta}
}

func listReleaseDecisionsRequest(input ListGovernanceReleaseDecisionsInput) (*governancev1.ListReleaseDecisionsRequest, error) {
	meta, err := governanceQueryMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	packageID := strings.TrimSpace(input.ReleaseDecisionPackageID)
	if packageID == "" && !governanceProjectContextHasListScope(input.ProjectContext) {
		return nil, invalidInput("release_decision_package_id or project_context.project_ref/repository_ref is required")
	}
	status, err := optionalGovernanceReleaseDecisionStatus(input.Status)
	if err != nil {
		return nil, err
	}
	outcome, err := optionalGovernanceReleaseDecisionOutcome(input.Outcome)
	if err != nil {
		return nil, err
	}
	return &governancev1.ListReleaseDecisionsRequest{
		ReleaseDecisionPackageId: optionalString(packageID),
		ProjectContext:           governanceProjectContext(input.ProjectContext, false),
		Status:                   status,
		Outcome:                  outcome,
		Page:                     governancePageRequest(input.Page),
		Meta:                     meta,
	}, nil
}

func recordBlockingSignalRequest(input RecordGovernanceBlockingSignalInput) (*governancev1.RecordBlockingSignalRequest, error) {
	meta, err := governanceCommandMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	target, err := governanceTarget(input.Target, true, "target")
	if err != nil {
		return nil, err
	}
	sourceType, severity, summary, err := blockingSignalFields(input.SourceType, input.Severity, input.Summary)
	if err != nil {
		return nil, err
	}
	return &governancev1.RecordBlockingSignalRequest{
		Target:     target,
		SourceType: sourceType,
		SourceRef:  optionalString(input.SourceRef),
		Severity:   severity,
		Summary:    summary,
		Meta:       meta,
	}, nil
}

func blockingSignalFields(sourceInput string, severityInput string, summaryInput string) (governancev1.BlockingSignalSourceType, governancev1.SignalSeverity, string, error) {
	sourceType, err := governanceBlockingSignalSource(sourceInput)
	if err != nil {
		return governancev1.BlockingSignalSourceType_BLOCKING_SIGNAL_SOURCE_TYPE_UNSPECIFIED, governancev1.SignalSeverity_SIGNAL_SEVERITY_UNSPECIFIED, "", err
	}
	severity, err := governanceSignalSeverity(severityInput)
	if err != nil {
		return governancev1.BlockingSignalSourceType_BLOCKING_SIGNAL_SOURCE_TYPE_UNSPECIFIED, governancev1.SignalSeverity_SIGNAL_SEVERITY_UNSPECIFIED, "", err
	}
	summary, err := requiredTrimmed(summaryInput, "summary")
	if err != nil {
		return governancev1.BlockingSignalSourceType_BLOCKING_SIGNAL_SOURCE_TYPE_UNSPECIFIED, governancev1.SignalSeverity_SIGNAL_SEVERITY_UNSPECIFIED, "", err
	}
	return sourceType, severity, summary, nil
}

func resolveBlockingSignalRequest(input ResolveGovernanceBlockingSignalInput) (*governancev1.ResolveBlockingSignalRequest, error) {
	meta, err := governanceCommandMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	signalID, err := requiredTrimmed(input.BlockingSignalID, "blocking_signal_id")
	if err != nil {
		return nil, err
	}
	terminalStatus, err := governanceTerminalBlockingSignalStatus(input.TerminalStatus)
	if err != nil {
		return nil, err
	}
	resolutionSummary, err := requiredTrimmed(input.ResolutionSummary, "resolution_summary")
	if err != nil {
		return nil, err
	}
	return &governancev1.ResolveBlockingSignalRequest{
		BlockingSignalId:  signalID,
		TerminalStatus:    terminalStatus,
		ResolutionSummary: resolutionSummary,
		Meta:              meta,
	}, nil
}

func listBlockingSignalsRequest(input ListGovernanceBlockingSignalsInput) (*governancev1.ListBlockingSignalsRequest, error) {
	meta, err := governanceQueryMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	target, err := governanceTarget(input.Target, true, "target")
	if err != nil {
		return nil, err
	}
	status, err := optionalGovernanceBlockingSignalStatus(input.Status)
	if err != nil {
		return nil, err
	}
	severity, err := optionalGovernanceSignalSeverity(input.Severity)
	if err != nil {
		return nil, err
	}
	return &governancev1.ListBlockingSignalsRequest{
		Target:   target,
		Status:   status,
		Severity: severity,
		Page:     governancePageRequest(input.Page),
		Meta:     meta,
	}, nil
}

func recordReleaseSafetyStateRequest(input RecordGovernanceReleaseSafetyStateInput) (*governancev1.RecordReleaseSafetyStateRequest, error) {
	meta, err := governanceCommandMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	packageID, err := requiredTrimmed(input.ReleaseDecisionPackageID, "release_decision_package_id")
	if err != nil {
		return nil, err
	}
	state, err := governanceReleaseSafetyState(input.CurrentState)
	if err != nil {
		return nil, err
	}
	reason, err := requiredTrimmed(input.LastStateReason, "last_state_reason")
	if err != nil {
		return nil, err
	}
	return &governancev1.RecordReleaseSafetyStateRequest{
		ReleaseDecisionPackageId: packageID,
		CurrentState:             state,
		RuntimeJobRef:            optionalString(input.RuntimeJobRef),
		LastStateReason:          reason,
		Meta:                     meta,
	}, nil
}

func getReleaseSafetyStateRequest(input GetGovernanceReleaseSafetyStateInput) (*governancev1.GetReleaseSafetyStateRequest, error) {
	return governanceQueryRequestWithID(input.Meta, input.ReleaseDecisionPackageID, "release_decision_package_id", newGetReleaseSafetyStateRequest)
}

func newGetReleaseSafetyStateRequest(id string, meta *governancev1.QueryMeta) *governancev1.GetReleaseSafetyStateRequest {
	return &governancev1.GetReleaseSafetyStateRequest{ReleaseDecisionPackageId: id, Meta: meta}
}

func governanceQueryRequestWithID[Request any](
	metaInput GovernanceQueryMetaInput,
	idInput string,
	field string,
	build func(string, *governancev1.QueryMeta) Request,
) (Request, error) {
	return governanceRequestWithID(metaInput, idInput, field, governanceQueryMeta, build)
}

func governanceCommandRequestWithID[Request any](
	metaInput GovernanceCommandMetaInput,
	idInput string,
	field string,
	build func(string, *governancev1.CommandMeta) Request,
) (Request, error) {
	return governanceRequestWithID(metaInput, idInput, field, governanceCommandMeta, build)
}

func governanceRequestWithID[MetaInput any, Meta any, Request any](
	metaInput MetaInput,
	idInput string,
	field string,
	buildMeta func(MetaInput) (Meta, error),
	build func(string, Meta) Request,
) (Request, error) {
	var zero Request
	meta, err := buildMeta(metaInput)
	if err != nil {
		return zero, err
	}
	id, err := requiredTrimmed(idInput, field)
	if err != nil {
		return zero, err
	}
	return build(id, meta), nil
}

func governanceCommandMeta(input GovernanceCommandMetaInput) (*governancev1.CommandMeta, error) {
	actorValue, contextValue, requestID, err := governanceMetaBase(input.Actor, input.RequestContext, input.RequestID)
	if err != nil {
		return nil, err
	}
	if !governanceHasReplayKey(input) {
		return nil, invalidInput("command_id or idempotency_key is required")
	}
	meta := &governancev1.CommandMeta{Actor: actorValue, RequestId: requestID, RequestContext: contextValue}
	meta.CommandId = optionalString(input.CommandID)
	meta.IdempotencyKey = optionalString(input.IdempotencyKey)
	meta.ExpectedVersion = input.ExpectedVersion
	meta.Reason = strings.TrimSpace(input.Reason)
	return meta, nil
}

func governanceQueryMeta(input GovernanceQueryMetaInput) (*governancev1.QueryMeta, error) {
	actorValue, contextValue, requestID, err := governanceMetaBase(input.Actor, input.RequestContext, input.RequestID)
	if err != nil {
		return nil, err
	}
	return &governancev1.QueryMeta{Actor: actorValue, RequestId: requestID, RequestContext: contextValue}, nil
}

func governanceQueryMetaFromCommand(meta *governancev1.CommandMeta) *governancev1.QueryMeta {
	if meta == nil {
		return nil
	}
	return &governancev1.QueryMeta{
		Actor:          meta.GetActor(),
		RequestId:      meta.GetRequestId(),
		RequestContext: meta.GetRequestContext(),
	}
}

func governanceMetaBase(actorInput GovernanceActorInput, contextInput GovernanceRequestContextInput, requestIDInput string) (*governancev1.Actor, *governancev1.RequestContext, string, error) {
	actorValue, err := governanceActor(actorInput)
	if err != nil {
		return nil, nil, "", err
	}
	contextValue, err := governanceRequestContext(contextInput)
	if err != nil {
		return nil, nil, "", err
	}
	requestID := strings.TrimSpace(requestIDInput)
	if requestID == "" {
		return nil, nil, "", invalidInput("request_id is required")
	}
	return actorValue, contextValue, requestID, nil
}

func governanceHasReplayKey(input GovernanceCommandMetaInput) bool {
	return strings.TrimSpace(input.CommandID) != "" || strings.TrimSpace(input.IdempotencyKey) != ""
}

func governanceActor(input GovernanceActorInput) (*governancev1.Actor, error) {
	actorType, actorID, err := actorFields(input.Type, input.ID)
	if err != nil {
		return nil, err
	}
	return &governancev1.Actor{Type: actorType, Id: actorID}, nil
}

func governanceRequestContext(input GovernanceRequestContextInput) (*governancev1.RequestContext, error) {
	source, traceID, sessionID, clientIPHash, err := safeRequestContext(input.Source, input.TraceID, input.SessionID, input.ClientIPHash)
	if err != nil {
		return nil, err
	}
	contextValue := &governancev1.RequestContext{}
	contextValue.Source = source
	contextValue.TraceId = traceID
	contextValue.SessionId = sessionID
	contextValue.ClientIpHash = clientIPHash
	return contextValue, nil
}

func governanceTarget(input GovernanceTargetInput, required bool, field string) (*governancev1.TargetRef, error) {
	empty := strings.TrimSpace(input.Type) == "" && strings.TrimSpace(input.Ref) == ""
	if empty && !required {
		return nil, nil
	}
	if strings.TrimSpace(input.Ref) == "" {
		return nil, invalidInput(field + ".ref is required")
	}
	targetType, err := governanceTargetType(input.Type, field+".type")
	if err != nil {
		return nil, err
	}
	return &governancev1.TargetRef{Type: targetType, Ref: strings.TrimSpace(input.Ref)}, nil
}

func governanceInteractionDeliveryRef(input GovernanceInteractionDeliveryRefInput) *governancev1.InteractionDeliveryRef {
	return &governancev1.InteractionDeliveryRef{
		RequestRef:  optionalString(input.RequestRef),
		DeliveryRef: optionalString(input.DeliveryRef),
		CallbackRef: optionalString(input.CallbackRef),
		DecisionRef: optionalString(input.DecisionRef),
	}
}

func governanceEvidenceRefs(inputs []GovernanceEvidenceRefInput) ([]*governancev1.EvidenceRef, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	result := make([]*governancev1.EvidenceRef, 0, len(inputs))
	for _, input := range inputs {
		kind, err := governanceEvidenceKind(input.Kind, "evidence_refs.kind")
		if err != nil {
			return nil, invalidInput("evidence_refs contains invalid kind")
		}
		ref, err := requiredTrimmed(input.Ref, "evidence_refs.ref")
		if err != nil {
			return nil, err
		}
		summary, err := requiredTrimmed(input.Summary, "evidence_refs.summary")
		if err != nil {
			return nil, err
		}
		result = append(result, &governancev1.EvidenceRef{
			Kind:           kind,
			Ref:            ref,
			Summary:        summary,
			Digest:         optionalString(input.Digest),
			RetentionClass: optionalString(input.RetentionClass),
		})
	}
	return result, nil
}

func governanceProjectContext(input GovernanceProjectContextRefInput, includeEmpty bool) *governancev1.ProjectContextRef {
	if !includeEmpty && governanceProjectContextEmpty(input) {
		return nil
	}
	return &governancev1.ProjectContextRef{
		ProjectRef:       optionalString(input.ProjectRef),
		RepositoryRef:    optionalString(input.RepositoryRef),
		ServiceRef:       optionalString(input.ServiceRef),
		BranchRulesRef:   optionalString(input.BranchRulesRef),
		ReleasePolicyRef: optionalString(input.ReleasePolicyRef),
		ReleaseLineRef:   optionalString(input.ReleaseLineRef),
	}
}

func governanceProjectContextEmpty(input GovernanceProjectContextRefInput) bool {
	return allBlankValues(input.ProjectRef, input.RepositoryRef, input.ServiceRef, input.BranchRulesRef, input.ReleasePolicyRef, input.ReleaseLineRef)
}

func governanceProjectContextHasListScope(input GovernanceProjectContextRefInput) bool {
	return strings.TrimSpace(input.ProjectRef) != "" || strings.TrimSpace(input.RepositoryRef) != ""
}

func governanceProviderContext(input GovernanceProviderContextRefInput) *governancev1.ProviderContextRef {
	refs := optionalRefValues(
		input.WorkItemRef,
		input.PullRequestRef,
		input.CommentRef,
		input.ReviewSignalRef,
		input.ProviderOperationRef,
		input.ChangedFilesSummaryRef,
	)
	return &governancev1.ProviderContextRef{
		WorkItemRef:            refs[0],
		PullRequestRef:         refs[1],
		CommentRef:             refs[2],
		ReviewSignalRef:        refs[3],
		ProviderOperationRef:   refs[4],
		ChangedFilesSummaryRef: refs[5],
	}
}

func governanceProviderContexts(inputs []GovernanceProviderContextRefInput) []*governancev1.ProviderContextRef {
	return compactGovernanceContexts(inputs, governanceProviderContextEmpty, governanceProviderContext)
}

func governanceProviderContextEmpty(input GovernanceProviderContextRefInput) bool {
	return allBlankValues(input.WorkItemRef, input.PullRequestRef, input.CommentRef, input.ReviewSignalRef, input.ProviderOperationRef, input.ChangedFilesSummaryRef)
}

func governanceAgentContext(input GovernanceAgentContextRefInput) *governancev1.AgentContextRef {
	refs := optionalRefValues(input.SessionRef, input.RunRef, input.StageRef, input.AcceptanceRef, input.RoleRef)
	return &governancev1.AgentContextRef{
		SessionRef:    refs[0],
		RunRef:        refs[1],
		StageRef:      refs[2],
		AcceptanceRef: refs[3],
		RoleRef:       refs[4],
	}
}

func governanceRuntimeContext(input GovernanceRuntimeContextRefInput) *governancev1.RuntimeContextRef {
	return &governancev1.RuntimeContextRef{
		SlotRef:        optionalString(input.SlotRef),
		JobRef:         optionalString(input.JobRef),
		EnvironmentRef: optionalString(input.EnvironmentRef),
		ArtifactRef:    optionalString(input.ArtifactRef),
		SummaryRef:     optionalString(input.SummaryRef),
	}
}

func governanceRuntimeContexts(inputs []GovernanceRuntimeContextRefInput) []*governancev1.RuntimeContextRef {
	return compactGovernanceContexts(inputs, governanceRuntimeContextEmpty, governanceRuntimeContext)
}

func governanceRuntimeContextEmpty(input GovernanceRuntimeContextRefInput) bool {
	return allBlankValues(input.SlotRef, input.JobRef, input.EnvironmentRef, input.ArtifactRef, input.SummaryRef)
}

func governanceRiskEvaluationSummary(input GovernanceRiskEvaluationSummaryInput) (*governancev1.RiskEvaluationSummary, error) {
	factors, err := governanceRiskEvaluationFactors(input.Factors)
	if err != nil {
		return nil, err
	}
	return &governancev1.RiskEvaluationSummary{
		ChangedFilesSummaryRef: optionalString(input.ChangedFilesSummaryRef),
		Summary:                strings.TrimSpace(input.Summary),
		Factors:                factors,
	}, nil
}

func governanceRiskEvaluationFactors(inputs []GovernanceRiskEvaluationFactorInput) ([]*governancev1.RiskEvaluationFactor, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	result := make([]*governancev1.RiskEvaluationFactor, 0, len(inputs))
	for _, input := range inputs {
		sourceType, err := governanceRiskFactorSourceType(input.SourceType, "evaluation_summary.factors.source_type")
		if err != nil {
			return nil, err
		}
		ref, err := requiredTrimmed(input.Ref, "evaluation_summary.factors.ref")
		if err != nil {
			return nil, err
		}
		summary, err := requiredTrimmed(input.Summary, "evaluation_summary.factors.summary")
		if err != nil {
			return nil, err
		}
		result = append(result, &governancev1.RiskEvaluationFactor{
			SourceType: sourceType,
			Ref:        ref,
			Summary:    summary,
			Tags:       trimmedStrings(input.Tags),
		})
	}
	return result, nil
}

func trimmedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func optionalRefValues(values ...string) []*string {
	result := make([]*string, len(values))
	for index, value := range values {
		result[index] = optionalString(value)
	}
	return result
}

func compactGovernanceContexts[Input any, Output any](inputs []Input, empty func(Input) bool, cast func(Input) *Output) []*Output {
	if len(inputs) == 0 {
		return nil
	}
	result := make([]*Output, 0, len(inputs))
	for _, input := range inputs {
		if !empty(input) {
			result = append(result, cast(input))
		}
	}
	return result
}

func allBlankValues(values ...string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}

func governancePageRequest(input GovernancePageInput) *governancev1.PageRequest {
	return &governancev1.PageRequest{
		PageSize:  input.PageSize,
		PageToken: optionalString(input.PageToken),
	}
}

func governanceRiskAssessmentOutput(response *governancev1.RiskAssessmentResponse) GovernanceRiskAssessmentOutput {
	if response == nil {
		return GovernanceRiskAssessmentOutput{}
	}
	factors := response.GetRiskFactors()
	return GovernanceRiskAssessmentOutput{
		RiskAssessment:    governanceRiskAssessmentSummary(response.GetRiskAssessment(), factors),
		RiskFactors:       governanceRiskFactorSummaries(factors),
		ReviewSignalCount: len(response.GetReviewSignals()),
	}
}

func governanceRiskAssessmentListOutput(
	response *governancev1.ListRiskAssessmentsResponse,
	factorsByAssessmentID map[string][]*governancev1.RiskFactor,
) GovernanceRiskAssessmentListOutput {
	if response == nil {
		return GovernanceRiskAssessmentListOutput{}
	}
	return GovernanceRiskAssessmentListOutput{
		RiskAssessments: governanceRiskAssessmentSummaries(response.GetRiskAssessments(), factorsByAssessmentID),
		Page:            governancePageSummary(response.GetPage()),
	}
}

func governanceRiskAssessmentSummaries(
	assessments []*governancev1.RiskAssessment,
	factorsByAssessmentID map[string][]*governancev1.RiskFactor,
) []GovernanceRiskAssessmentSummary {
	return summarizeItems(assessments, func(assessment *governancev1.RiskAssessment) GovernanceRiskAssessmentSummary {
		return governanceRiskAssessmentSummary(assessment, factorsByAssessmentID[riskAssessmentID(assessment)])
	})
}

func governanceRiskAssessmentSummary(assessment *governancev1.RiskAssessment, factors []*governancev1.RiskFactor) GovernanceRiskAssessmentSummary {
	if assessment == nil {
		return GovernanceRiskAssessmentSummary{}
	}
	requiredGates := governanceRequiredGateSummaries(assessment.GetRequiredGates())
	matchedRuleRefs := governanceMatchedRuleRefs(factors)
	return GovernanceRiskAssessmentSummary{
		ID:                 assessment.GetId(),
		Target:             governanceTargetSummary(assessment.GetTarget()),
		ProjectContext:     governanceProjectContextSummary(assessment.GetProjectContext()),
		ProviderContext:    governanceProviderContextSummary(assessment.GetProviderContext()),
		AgentContext:       governanceAgentContextSummary(assessment.GetAgentContext()),
		RuntimeContext:     governanceRuntimeContextSummary(assessment.GetRuntimeContext()),
		InitialRiskClass:   governanceRiskClassName(assessment.GetInitialRiskClass()),
		EffectiveRiskClass: governanceRiskClassName(assessment.GetEffectiveRiskClass()),
		Status:             governanceRiskAssessmentStatusName(assessment.GetStatus()),
		Summary:            assessment.GetExplanation(),
		RequiredGates:      requiredGates,
		RequiredGateCount:  len(requiredGates),
		RequiredGateRefs:   governanceRequiredGateRefs(requiredGates),
		MatchedRuleRefs:    matchedRuleRefs,
		MatchedRuleCount:   len(matchedRuleRefs),
		RiskFactorCount:    len(factors),
		RiskProfileID:      assessment.GetRiskProfileId(),
		RiskProfileVersion: assessment.GetRiskProfileVersion(),
		EvaluationSummary:  governanceEvaluationSummarySummary(assessment.GetEvaluationSummary()),
		EvidenceRefs:       governanceEvidenceSummaries(assessment.GetEvidenceRefs()),
		Version:            assessment.GetVersion(),
		CreatedAt:          assessment.GetCreatedAt(),
		UpdatedAt:          assessment.GetUpdatedAt(),
	}
}

func riskAssessmentID(assessment *governancev1.RiskAssessment) string {
	if assessment == nil {
		return ""
	}
	return strings.TrimSpace(assessment.GetId())
}

func governanceRequiredGateSummaries(gates []*governancev1.RequiredGate) []GovernanceRequiredGateSummary {
	return summarizeItems(gates, func(gate *governancev1.RequiredGate) GovernanceRequiredGateSummary {
		if gate == nil {
			return GovernanceRequiredGateSummary{}
		}
		return GovernanceRequiredGateSummary{
			GatePolicyID: gate.GetGatePolicyId(),
			GateKind:     governanceGateKindName(gate.GetGateKind()),
			MinRiskClass: governanceRiskClassName(gate.GetMinRiskClass()),
			Reason:       gate.GetReason(),
		}
	})
}

func governanceRequiredGateRefs(gates []GovernanceRequiredGateSummary) []string {
	if len(gates) == 0 {
		return nil
	}
	result := make([]string, 0, len(gates))
	seen := make(map[string]struct{}, len(gates))
	for _, gate := range gates {
		ref := strings.TrimSpace(gate.GatePolicyID)
		if ref == "" {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		result = append(result, ref)
	}
	return result
}

func governanceRiskFactorSummaries(factors []*governancev1.RiskFactor) []GovernanceRiskFactorSummary {
	return summarizeItems(factors, func(factor *governancev1.RiskFactor) GovernanceRiskFactorSummary {
		if factor == nil {
			return GovernanceRiskFactorSummary{}
		}
		return GovernanceRiskFactorSummary{
			ID:               factor.GetId(),
			RiskAssessmentID: factor.GetRiskAssessmentId(),
			SourceType:       governanceRiskFactorSourceTypeName(factor.GetSourceType()),
			SourceRef:        factor.GetSourceRef(),
			RiskClass:        governanceRiskClassName(factor.GetRiskClass()),
			Summary:          factor.GetSummary(),
			CreatedAt:        factor.GetCreatedAt(),
		}
	})
}

func governanceMatchedRuleRefs(factors []*governancev1.RiskFactor) []string {
	if len(factors) == 0 {
		return nil
	}
	result := make([]string, 0, len(factors))
	seen := make(map[string]struct{}, len(factors))
	for _, factor := range factors {
		if factor.GetSourceType() != governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_POLICY {
			continue
		}
		ref := strings.TrimSpace(factor.GetSourceRef())
		if ref == "" {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		result = append(result, ref)
	}
	return result
}

func governanceEvaluationSummarySummary(summary *governancev1.RiskEvaluationSummary) GovernanceEvaluationSummarySummary {
	if summary == nil {
		return GovernanceEvaluationSummarySummary{}
	}
	return GovernanceEvaluationSummarySummary{
		ChangedFilesSummaryRef: summary.GetChangedFilesSummaryRef(),
		Summary:                summary.GetSummary(),
		FactorCount:            len(summary.GetFactors()),
	}
}

func governanceGateOutput(response *governancev1.GateRequestResponse) GovernanceGateOutput {
	if response == nil {
		return GovernanceGateOutput{}
	}
	output := GovernanceGateOutput{GateRequest: governanceGateRequestSummary(response.GetGateRequest())}
	if response.GetGateDecision() != nil {
		decision := governanceGateDecisionSummary(response.GetGateDecision())
		output.GateDecision = &decision
	}
	return output
}

func governanceGateDecisionOutput(response *governancev1.GateDecisionResponse) GovernanceGateDecisionOutput {
	if response == nil {
		return GovernanceGateDecisionOutput{}
	}
	return GovernanceGateDecisionOutput{
		GateDecision: governanceGateDecisionSummary(response.GetGateDecision()),
		GateRequest:  governanceGateRequestSummary(response.GetGateRequest()),
	}
}

func governanceGateListOutput(response *governancev1.ListGateRequestsResponse) GovernanceGateListOutput {
	if response == nil {
		return GovernanceGateListOutput{}
	}
	return GovernanceGateListOutput{
		GateRequests: governanceGateRequestSummaries(response.GetGateRequests()),
		Page:         governancePageSummary(response.GetPage()),
	}
}

func governanceGateRequestSummaries(requests []*governancev1.GateRequest) []GovernanceGateRequestSummary {
	return summarizeItems(requests, governanceGateRequestSummary)
}

func governanceGateRequestSummary(request *governancev1.GateRequest) GovernanceGateRequestSummary {
	if request == nil {
		return GovernanceGateRequestSummary{}
	}
	return GovernanceGateRequestSummary{
		ID:                     request.GetId(),
		RiskAssessmentID:       request.GetRiskAssessmentId(),
		GatePolicyID:           request.GetGatePolicyId(),
		Target:                 governanceTargetSummary(request.GetTarget()),
		InteractionDeliveryRef: governanceInteractionDeliverySummary(request.GetInteractionDeliveryRef()),
		EvidenceRefs:           governanceEvidenceSummaries(request.GetEvidenceRefs()),
		EvidenceSummary:        request.GetEvidenceSummary(),
		Status:                 governanceGateStatusName(request.GetStatus()),
		Version:                request.GetVersion(),
		CreatedAt:              request.GetCreatedAt(),
		UpdatedAt:              request.GetUpdatedAt(),
		TerminalActorRef:       request.GetTerminalActorRef(),
		TerminalReason:         request.GetTerminalReason(),
		TerminalAt:             request.GetTerminalAt(),
	}
}

func governanceGateDecisionSummary(decision *governancev1.GateDecision) GovernanceGateDecisionSummary {
	if decision == nil {
		return GovernanceGateDecisionSummary{}
	}
	return GovernanceGateDecisionSummary{
		ID:                decision.GetId(),
		GateRequestID:     decision.GetGateRequestId(),
		DecisionActorRef:  decision.GetDecisionActorRef(),
		DecisionPolicyRef: decision.GetDecisionPolicyRef(),
		Outcome:           governanceGateOutcomeName(decision.GetOutcome()),
		Reason:            decision.GetReason(),
		ConditionsSummary: decision.GetConditionsSummary(),
		SourceRef:         decision.GetSourceRef(),
		DecidedAt:         decision.GetDecidedAt(),
	}
}

func governanceReleaseDecisionPackageOutput(response *governancev1.ReleaseDecisionPackageResponse) GovernanceReleaseDecisionPackageOutput {
	if response == nil {
		return GovernanceReleaseDecisionPackageOutput{}
	}
	return GovernanceReleaseDecisionPackageOutput{ReleaseDecisionPackage: governanceReleaseDecisionPackageSummary(response.GetReleaseDecisionPackage())}
}

func governanceReleaseDecisionPackageListOutput(response *governancev1.ListReleaseDecisionPackagesResponse) GovernanceReleaseDecisionPackageListOutput {
	if response == nil {
		return GovernanceReleaseDecisionPackageListOutput{}
	}
	return GovernanceReleaseDecisionPackageListOutput{
		ReleaseDecisionPackages: governanceReleaseDecisionPackageSummaries(response.GetReleaseDecisionPackages()),
		Page:                    governancePageSummary(response.GetPage()),
	}
}

func governanceReleaseDecisionPackageSummaries(packages []*governancev1.ReleaseDecisionPackage) []GovernanceReleaseDecisionPackageSummary {
	return summarizeItems(packages, governanceReleaseDecisionPackageSummary)
}

func governanceReleaseDecisionPackageSummary(pkg *governancev1.ReleaseDecisionPackage) GovernanceReleaseDecisionPackageSummary {
	if pkg == nil {
		return GovernanceReleaseDecisionPackageSummary{}
	}
	evidenceRefs := governanceEvidenceSummaries(pkg.GetEvidenceRefs())
	reviewSignalIDs := trimmedStrings(pkg.GetReviewSignalIds())
	return GovernanceReleaseDecisionPackageSummary{
		ID:                      pkg.GetId(),
		ReleaseCandidateRef:     pkg.GetReleaseCandidateRef(),
		ProjectContext:          governanceProjectContextSummary(pkg.GetProjectContext()),
		RepositoryRefs:          trimmedStrings(pkg.GetRepositoryRefs()),
		RiskAssessmentID:        pkg.GetRiskAssessmentId(),
		ProviderRefs:            governanceProviderContextSummaries(pkg.GetProviderRefs()),
		RuntimeRefs:             governanceRuntimeContextSummaries(pkg.GetRuntimeRefs()),
		AgentContext:            governanceAgentContextSummary(pkg.GetAgentContext()),
		ReviewSignalIDs:         reviewSignalIDs,
		ReviewSignalCount:       len(reviewSignalIDs),
		EvidenceRefs:            evidenceRefs,
		EvidenceRefCount:        len(evidenceRefs),
		KnownLimitationsSummary: pkg.GetKnownLimitationsSummary(),
		Status:                  governanceReleasePackageStatusName(pkg.GetStatus()),
		Version:                 pkg.GetVersion(),
		CreatedAt:               pkg.GetCreatedAt(),
		UpdatedAt:               pkg.GetUpdatedAt(),
	}
}

func governanceReleaseDecisionOutput(response *governancev1.ReleaseDecisionResponse) GovernanceReleaseDecisionOutput {
	if response == nil {
		return GovernanceReleaseDecisionOutput{}
	}
	return GovernanceReleaseDecisionOutput{
		ReleaseDecision:        governanceReleaseDecisionSummary(response.GetReleaseDecision()),
		ReleaseDecisionPackage: governanceReleaseDecisionPackageSummary(response.GetReleaseDecisionPackage()),
	}
}

func governanceReleaseDecisionListOutput(response *governancev1.ListReleaseDecisionsResponse) GovernanceReleaseDecisionListOutput {
	if response == nil {
		return GovernanceReleaseDecisionListOutput{}
	}
	return GovernanceReleaseDecisionListOutput{
		ReleaseDecisions: governanceReleaseDecisionSummaries(response.GetReleaseDecisions()),
		Page:             governancePageSummary(response.GetPage()),
	}
}

func governanceReleaseDecisionSummaries(decisions []*governancev1.ReleaseDecision) []GovernanceReleaseDecisionSummary {
	return summarizeItems(decisions, governanceReleaseDecisionSummary)
}

func governanceReleaseDecisionSummary(decision *governancev1.ReleaseDecision) GovernanceReleaseDecisionSummary {
	if decision == nil {
		return GovernanceReleaseDecisionSummary{}
	}
	return GovernanceReleaseDecisionSummary{
		ID:                       decision.GetId(),
		ReleaseDecisionPackageID: decision.GetReleaseDecisionPackageId(),
		GateDecisionID:           decision.GetGateDecisionId(),
		Outcome:                  governanceReleaseDecisionOutcomeName(decision.GetOutcome()),
		DecisionActorRef:         decision.GetDecisionActorRef(),
		DecisionPolicyRef:        decision.GetDecisionPolicyRef(),
		Reason:                   decision.GetReason(),
		ConditionsSummary:        decision.GetConditionsSummary(),
		Status:                   governanceReleaseDecisionStatusName(decision.GetStatus()),
		Version:                  decision.GetVersion(),
		DecidedAt:                decision.GetDecidedAt(),
	}
}

func governanceBlockingSignalOutput(response *governancev1.BlockingSignalResponse) GovernanceBlockingSignalOutput {
	if response == nil {
		return GovernanceBlockingSignalOutput{}
	}
	return GovernanceBlockingSignalOutput{BlockingSignal: governanceBlockingSignalSummary(response.GetBlockingSignal())}
}

func governanceBlockingSignalListOutput(response *governancev1.ListBlockingSignalsResponse) GovernanceBlockingSignalListOutput {
	if response == nil {
		return GovernanceBlockingSignalListOutput{}
	}
	return GovernanceBlockingSignalListOutput{
		BlockingSignals: governanceBlockingSignalSummaries(response.GetBlockingSignals()),
		Page:            governancePageSummary(response.GetPage()),
	}
}

func governanceBlockingSignalSummaries(signals []*governancev1.BlockingSignal) []GovernanceBlockingSignalSummary {
	return summarizeItems(signals, governanceBlockingSignalSummary)
}

func governanceBlockingSignalSummary(signal *governancev1.BlockingSignal) GovernanceBlockingSignalSummary {
	if signal == nil {
		return GovernanceBlockingSignalSummary{}
	}
	return GovernanceBlockingSignalSummary{
		ID:         signal.GetId(),
		Target:     governanceTargetSummary(signal.GetTarget()),
		SourceType: governanceBlockingSignalSourceName(signal.GetSourceType()),
		SourceRef:  signal.GetSourceRef(),
		Severity:   governanceSignalSeverityName(signal.GetSeverity()),
		Summary:    signal.GetSummary(),
		Status:     governanceBlockingSignalStatusName(signal.GetStatus()),
		Version:    signal.GetVersion(),
		CreatedAt:  signal.GetCreatedAt(),
		ResolvedAt: signal.GetResolvedAt(),
	}
}

func governanceReleaseSafetyStateOutput(response *governancev1.ReleaseSafetyStateResponse) GovernanceReleaseSafetyStateOutput {
	if response == nil {
		return GovernanceReleaseSafetyStateOutput{}
	}
	return GovernanceReleaseSafetyStateOutput{ReleaseSafetyState: governanceReleaseSafetyStateSummary(response.GetReleaseSafetyState())}
}

func governanceReleaseSafetyStateSummary(state *governancev1.ReleaseSafetyState) GovernanceReleaseSafetyStateSummary {
	if state == nil {
		return GovernanceReleaseSafetyStateSummary{}
	}
	return GovernanceReleaseSafetyStateSummary{
		ID:                       state.GetId(),
		ReleaseDecisionPackageID: state.GetReleaseDecisionPackageId(),
		CurrentState:             governanceReleaseSafetyStateName(state.GetCurrentState()),
		RuntimeJobRef:            state.GetRuntimeJobRef(),
		BlockingSignalCount:      state.GetBlockingSignalCount(),
		LastStateReason:          state.GetLastStateReason(),
		Version:                  state.GetVersion(),
		CreatedAt:                state.GetCreatedAt(),
		UpdatedAt:                state.GetUpdatedAt(),
	}
}

func governanceReviewSignalOutput(response *governancev1.ReviewSignalResponse) GovernanceReviewSignalOutput {
	if response == nil {
		return GovernanceReviewSignalOutput{}
	}
	return GovernanceReviewSignalOutput{ReviewSignal: governanceReviewSignalSummary(response.GetReviewSignal())}
}

func governanceReviewSignalListOutput(response *governancev1.ListReviewSignalsResponse) GovernanceReviewSignalListOutput {
	if response == nil {
		return GovernanceReviewSignalListOutput{}
	}
	return GovernanceReviewSignalListOutput{
		ReviewSignals: governanceReviewSignalSummaries(response.GetReviewSignals()),
		Page:          governancePageSummary(response.GetPage()),
	}
}

func governanceReviewSignalSummaries(signals []*governancev1.ReviewSignal) []GovernanceReviewSignalSummary {
	return summarizeItems(signals, governanceReviewSignalSummary)
}

func governanceReviewSignalSummary(signal *governancev1.ReviewSignal) GovernanceReviewSignalSummary {
	if signal == nil {
		return GovernanceReviewSignalSummary{}
	}
	return GovernanceReviewSignalSummary{
		ID:               signal.GetId(),
		RiskAssessmentID: signal.GetRiskAssessmentId(),
		Target:           governanceTargetSummary(signal.GetTarget()),
		RoleKind:         governanceReviewRoleKindName(signal.GetRoleKind()),
		AuthorRef:        signal.GetAuthorRef(),
		Outcome:          governanceReviewSignalOutcomeName(signal.GetOutcome()),
		Severity:         governanceSignalSeverityName(signal.GetSeverity()),
		Confidence:       governanceConfidenceName(signal.GetConfidence()),
		EvidenceRefs:     governanceEvidenceSummaries(signal.GetEvidenceRefs()),
		Summary:          signal.GetSummary(),
		CreatedAt:        signal.GetCreatedAt(),
	}
}

func governanceProjectContextSummary(contextRef *governancev1.ProjectContextRef) GovernanceProjectContextSummary {
	if contextRef == nil {
		return GovernanceProjectContextSummary{}
	}
	return GovernanceProjectContextSummary{
		ProjectRef:       contextRef.GetProjectRef(),
		RepositoryRef:    contextRef.GetRepositoryRef(),
		ServiceRef:       contextRef.GetServiceRef(),
		BranchRulesRef:   contextRef.GetBranchRulesRef(),
		ReleasePolicyRef: contextRef.GetReleasePolicyRef(),
		ReleaseLineRef:   contextRef.GetReleaseLineRef(),
	}
}

func governanceProviderContextSummary(contextRef *governancev1.ProviderContextRef) GovernanceProviderContextSummary {
	if contextRef == nil {
		return GovernanceProviderContextSummary{}
	}
	summary := GovernanceProviderContextSummary{}
	summary.WorkItemRef = contextRef.GetWorkItemRef()
	summary.PullRequestRef = contextRef.GetPullRequestRef()
	summary.CommentRef = contextRef.GetCommentRef()
	summary.ReviewSignalRef = contextRef.GetReviewSignalRef()
	summary.ProviderOperationRef = contextRef.GetProviderOperationRef()
	summary.ChangedFilesSummaryRef = contextRef.GetChangedFilesSummaryRef()
	return summary
}

func governanceProviderContextSummaries(contextRefs []*governancev1.ProviderContextRef) []GovernanceProviderContextSummary {
	return summarizeItems(contextRefs, governanceProviderContextSummary)
}

func governanceAgentContextSummary(contextRef *governancev1.AgentContextRef) GovernanceAgentContextSummary {
	if contextRef == nil {
		return GovernanceAgentContextSummary{}
	}
	return GovernanceAgentContextSummary{
		SessionRef:    contextRef.GetSessionRef(),
		RunRef:        contextRef.GetRunRef(),
		StageRef:      contextRef.GetStageRef(),
		AcceptanceRef: contextRef.GetAcceptanceRef(),
		RoleRef:       contextRef.GetRoleRef(),
	}
}

func governanceRuntimeContextSummary(contextRef *governancev1.RuntimeContextRef) GovernanceRuntimeContextSummary {
	if contextRef == nil {
		return GovernanceRuntimeContextSummary{}
	}
	summary := GovernanceRuntimeContextSummary{}
	summary.SlotRef = contextRef.GetSlotRef()
	summary.JobRef = contextRef.GetJobRef()
	summary.EnvironmentRef = contextRef.GetEnvironmentRef()
	summary.ArtifactRef = contextRef.GetArtifactRef()
	summary.SummaryRef = contextRef.GetSummaryRef()
	return summary
}

func governanceRuntimeContextSummaries(contextRefs []*governancev1.RuntimeContextRef) []GovernanceRuntimeContextSummary {
	return summarizeItems(contextRefs, governanceRuntimeContextSummary)
}

func governanceTargetSummary(target *governancev1.TargetRef) GovernanceTargetSummary {
	if target == nil {
		return GovernanceTargetSummary{}
	}
	return GovernanceTargetSummary{Type: governanceTargetTypeName(target.GetType()), Ref: target.GetRef()}
}

func governanceInteractionDeliverySummary(ref *governancev1.InteractionDeliveryRef) GovernanceInteractionDeliverySummary {
	if ref == nil {
		return GovernanceInteractionDeliverySummary{}
	}
	return GovernanceInteractionDeliverySummary{
		RequestRef:  ref.GetRequestRef(),
		DeliveryRef: ref.GetDeliveryRef(),
		CallbackRef: ref.GetCallbackRef(),
		DecisionRef: ref.GetDecisionRef(),
	}
}

func governanceEvidenceSummaries(evidenceRefs []*governancev1.EvidenceRef) []GovernanceEvidenceSummary {
	return summarizeItems(evidenceRefs, governanceEvidenceSummary)
}

func governanceEvidenceSummary(evidence *governancev1.EvidenceRef) GovernanceEvidenceSummary {
	if evidence == nil {
		return GovernanceEvidenceSummary{}
	}
	return GovernanceEvidenceSummary{
		Kind:           governanceEvidenceKindName(evidence.GetKind()),
		Ref:            evidence.GetRef(),
		Summary:        evidence.GetSummary(),
		Digest:         evidence.GetDigest(),
		RetentionClass: evidence.GetRetentionClass(),
	}
}

func governancePageSummary(page *governancev1.PageResponse) PageSummary {
	if page == nil {
		return PageSummary{}
	}
	return PageSummary{NextPageToken: page.GetNextPageToken()}
}

func governanceTargetType(value string, field string) (governancev1.GovernanceTargetType, error) {
	return requiredEnumValue(governanceEnumKey(value), governanceTargetTypes, governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_UNSPECIFIED, field)
}

func governanceEvidenceKind(value string, field string) (governancev1.EvidenceKind, error) {
	return requiredEnumValue(governanceEnumKey(value), governanceEvidenceKinds, governancev1.EvidenceKind_EVIDENCE_KIND_UNSPECIFIED, field)
}

func optionalGovernanceRiskClass(value string) (*governancev1.RiskClass, error) {
	return optionalGovernanceEnum(value, "effective_risk_class", governanceRiskClasses, governancev1.RiskClass_RISK_CLASS_UNSPECIFIED)
}

func optionalGovernanceRiskAssessmentStatus(value string) (*governancev1.RiskAssessmentStatus, error) {
	return optionalGovernanceEnum(value, "status", governanceRiskAssessmentStatuses, governancev1.RiskAssessmentStatus_RISK_ASSESSMENT_STATUS_UNSPECIFIED)
}

func governanceRiskFactorSourceType(value string, field string) (governancev1.RiskFactorSourceType, error) {
	key := governanceEnumKey(value)
	if key == "" {
		return governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_UNSPECIFIED, invalidInput(field + " is required")
	}
	switch key {
	case "policy":
		return governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_POLICY, nil
	case "changed_file":
		return governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_CHANGED_FILE, nil
	case "service":
		return governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_SERVICE, nil
	case "api":
		return governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_API, nil
	case "database":
		return governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_DATABASE, nil
	case "secret":
		return governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_SECRET, nil
	case "release":
		return governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_RELEASE, nil
	case "runtime":
		return governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_RUNTIME, nil
	case "review_signal":
		return governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_REVIEW_SIGNAL, nil
	case "human_decision":
		return governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_HUMAN_DECISION, nil
	default:
		return governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_UNSPECIFIED, invalidInput(field + " is invalid")
	}
}

func optionalGovernanceGateStatus(value string) (*governancev1.GateRequestStatus, error) {
	return optionalGovernanceEnum(value, "status", governanceGateStatuses, governancev1.GateRequestStatus_GATE_REQUEST_STATUS_UNSPECIFIED)
}

func optionalGovernanceReleasePackageStatus(value string) (*governancev1.ReleaseDecisionPackageStatus, error) {
	return optionalGovernanceEnum(value, "status", governanceReleasePackageStatuses, governancev1.ReleaseDecisionPackageStatus_RELEASE_DECISION_PACKAGE_STATUS_UNSPECIFIED)
}

func optionalGovernanceReleaseDecisionStatus(value string) (*governancev1.ReleaseDecisionStatus, error) {
	return optionalGovernanceEnum(value, "status", governanceReleaseDecisionStatuses, governancev1.ReleaseDecisionStatus_RELEASE_DECISION_STATUS_UNSPECIFIED)
}

func optionalGovernanceReleaseDecisionOutcome(value string) (*governancev1.ReleaseDecisionOutcome, error) {
	return optionalGovernanceEnum(value, "outcome", governanceReleaseDecisionOutcomes, governancev1.ReleaseDecisionOutcome_RELEASE_DECISION_OUTCOME_UNSPECIFIED)
}

func optionalGovernanceBlockingSignalStatus(value string) (*governancev1.BlockingSignalStatus, error) {
	return optionalGovernanceEnum(value, "status", governanceBlockingSignalStatuses, governancev1.BlockingSignalStatus_BLOCKING_SIGNAL_STATUS_UNSPECIFIED)
}

func optionalGovernanceSignalSeverity(value string) (*governancev1.SignalSeverity, error) {
	return optionalGovernanceEnum(value, "severity", governanceSignalSeverities, governancev1.SignalSeverity_SIGNAL_SEVERITY_UNSPECIFIED)
}

func optionalGovernanceReviewRoleKind(value string) (*governancev1.ReviewRoleKind, error) {
	return optionalGovernanceEnum(value, "role_kind", governanceReviewRoleKinds, governancev1.ReviewRoleKind_REVIEW_ROLE_KIND_UNSPECIFIED)
}

func optionalGovernanceReviewSignalOutcome(value string) (*governancev1.ReviewSignalOutcome, error) {
	return optionalGovernanceEnum(value, "outcome", governanceReviewSignalOutcomes, governancev1.ReviewSignalOutcome_REVIEW_SIGNAL_OUTCOME_UNSPECIFIED)
}

func optionalGovernanceConfidence(value string) (*governancev1.Confidence, error) {
	return optionalGovernanceEnum(value, "confidence", governanceConfidences, governancev1.Confidence_CONFIDENCE_UNSPECIFIED)
}

func optionalGovernanceEnum[Enum comparable](value string, field string, values map[string]Enum, zero Enum) (*Enum, error) {
	return optionalEnumValue(governanceEnumKey(value), values, zero, field)
}

func governanceGateOutcome(value string) (governancev1.GateOutcome, error) {
	return requiredEnumValue(governanceEnumKey(value), governanceGateOutcomes, governancev1.GateOutcome_GATE_OUTCOME_UNSPECIFIED, "outcome")
}

func governanceReleaseDecisionOutcome(value string) (governancev1.ReleaseDecisionOutcome, error) {
	return requiredEnumValue(governanceEnumKey(value), governanceReleaseDecisionOutcomes, governancev1.ReleaseDecisionOutcome_RELEASE_DECISION_OUTCOME_UNSPECIFIED, "outcome")
}

func governanceBlockingSignalSource(value string) (governancev1.BlockingSignalSourceType, error) {
	return requiredEnumValue(governanceEnumKey(value), governanceBlockingSignalSources, governancev1.BlockingSignalSourceType_BLOCKING_SIGNAL_SOURCE_TYPE_UNSPECIFIED, "source_type")
}

func governanceSignalSeverity(value string) (governancev1.SignalSeverity, error) {
	return requiredEnumValue(governanceEnumKey(value), governanceSignalSeverities, governancev1.SignalSeverity_SIGNAL_SEVERITY_UNSPECIFIED, "severity")
}

func governanceReviewRoleKind(value string) (governancev1.ReviewRoleKind, error) {
	return requiredEnumValue(governanceEnumKey(value), governanceReviewRoleKinds, governancev1.ReviewRoleKind_REVIEW_ROLE_KIND_UNSPECIFIED, "role_kind")
}

func governanceReviewSignalOutcome(value string) (governancev1.ReviewSignalOutcome, error) {
	return requiredEnumValue(governanceEnumKey(value), governanceReviewSignalOutcomes, governancev1.ReviewSignalOutcome_REVIEW_SIGNAL_OUTCOME_UNSPECIFIED, "outcome")
}

func governanceReleaseSafetyState(value string) (governancev1.ReleaseSafetyStateKind, error) {
	return requiredEnumValue(governanceEnumKey(value), governanceReleaseSafetyStates, governancev1.ReleaseSafetyStateKind_RELEASE_SAFETY_STATE_KIND_UNSPECIFIED, "current_state")
}

func governanceTerminalBlockingSignalStatus(value string) (governancev1.BlockingSignalStatus, error) {
	statusValue, err := requiredEnumValue(governanceEnumKey(value), governanceBlockingSignalStatuses, governancev1.BlockingSignalStatus_BLOCKING_SIGNAL_STATUS_UNSPECIFIED, "terminal_status")
	if err != nil {
		return governancev1.BlockingSignalStatus_BLOCKING_SIGNAL_STATUS_UNSPECIFIED, err
	}
	if statusValue == governancev1.BlockingSignalStatus_BLOCKING_SIGNAL_STATUS_ACTIVE {
		return governancev1.BlockingSignalStatus_BLOCKING_SIGNAL_STATUS_UNSPECIFIED, invalidInput("terminal_status must be resolved or dismissed")
	}
	return statusValue, nil
}

func governanceTargetTypeName(value governancev1.GovernanceTargetType) string {
	return enumName(value, governanceTargetTypeNames)
}

func governanceEvidenceKindName(value governancev1.EvidenceKind) string {
	return enumName(value, governanceEvidenceKindNames)
}

func governanceRiskClassName(value governancev1.RiskClass) string {
	return enumName(value, governanceRiskClassNames)
}

func governanceRiskAssessmentStatusName(value governancev1.RiskAssessmentStatus) string {
	return enumName(value, governanceRiskAssessmentStatusNames)
}

func governanceRiskFactorSourceTypeName(value governancev1.RiskFactorSourceType) string {
	return enumName(value, governanceRiskFactorSourceTypeNames)
}

func governanceGateKindName(value governancev1.GateKind) string {
	return enumName(value, governanceGateKindNames)
}

func governanceGateStatusName(value governancev1.GateRequestStatus) string {
	return enumName(value, governanceGateStatusNames)
}

func governanceGateOutcomeName(value governancev1.GateOutcome) string {
	return enumName(value, governanceGateOutcomeNames)
}

func governanceReleasePackageStatusName(value governancev1.ReleaseDecisionPackageStatus) string {
	return enumName(value, governanceReleasePackageStatusNames)
}

func governanceReleaseDecisionStatusName(value governancev1.ReleaseDecisionStatus) string {
	return enumName(value, governanceReleaseDecisionStatusNames)
}

func governanceReleaseDecisionOutcomeName(value governancev1.ReleaseDecisionOutcome) string {
	return enumName(value, governanceReleaseDecisionOutcomeNames)
}

func governanceReleaseSafetyStateName(value governancev1.ReleaseSafetyStateKind) string {
	return enumName(value, governanceReleaseSafetyStateNames)
}

func governanceBlockingSignalSourceName(value governancev1.BlockingSignalSourceType) string {
	return enumName(value, governanceBlockingSignalSourceNames)
}

func governanceBlockingSignalStatusName(value governancev1.BlockingSignalStatus) string {
	return enumName(value, governanceBlockingSignalStatusNames)
}

func governanceSignalSeverityName(value governancev1.SignalSeverity) string {
	return enumName(value, governanceSignalSeverityNames)
}

func governanceReviewRoleKindName(value governancev1.ReviewRoleKind) string {
	return enumName(value, governanceReviewRoleKindNames)
}

func governanceReviewSignalOutcomeName(value governancev1.ReviewSignalOutcome) string {
	return enumName(value, governanceReviewSignalOutcomeNames)
}

func governanceConfidenceName(value governancev1.Confidence) string {
	return enumName(value, governanceConfidenceNames)
}

func governanceEnumValues[Enum comparable](pairs []governanceEnumPair[Enum]) map[string]Enum {
	result := make(map[string]Enum, len(pairs))
	for _, pair := range pairs {
		result[pair.name] = pair.value
	}
	return result
}

func governanceEnumNames[Enum comparable](pairs []governanceEnumPair[Enum]) map[Enum]string {
	result := make(map[Enum]string, len(pairs))
	for _, pair := range pairs {
		result[pair.value] = pair.name
	}
	return result
}

func governanceEnumKey(value string) string {
	key := normalizedKey(value)
	prefixes := []string{
		"governance_target_type_",
		"evidence_kind_",
		"risk_class_",
		"risk_assessment_status_",
		"risk_factor_source_type_",
		"gate_kind_",
		"gate_request_status_",
		"gate_outcome_",
		"release_decision_package_status_",
		"release_decision_status_",
		"release_decision_outcome_",
		"release_safety_state_kind_",
		"blocking_signal_source_type_",
		"blocking_signal_status_",
		"signal_severity_",
		"review_role_kind_",
		"review_signal_outcome_",
		"confidence_",
	}
	for _, prefix := range prefixes {
		key = strings.TrimPrefix(key, prefix)
	}
	return key
}
