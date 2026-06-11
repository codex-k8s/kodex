package httptransport

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	agentsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/agents/v1"
	governancev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/governance/v1"
	interactionsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/interactions/v1"
	projectsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/projects/v1"
	runtimev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/runtime/v1"
	"github.com/codex-k8s/kodex/services/staff/staff-gateway/internal/transport/http/generated"
)

const defaultPageSize = 25
const (
	maxActivitySafeTextBytes     = 2000
	maxActivityDigestBytes       = 256
	maxActivityRefBytes          = 256
	maxActivitySafeJSONBytes     = 8192
	maxActivityIdentifierBytes   = 128
	maxAgentSafeTextBytes        = 2000
	maxAgentIdentifierBytes      = 256
	maxGovernanceKindBytes       = 128
	maxGovernanceRefBytes        = 256
	maxGovernanceTextBytes       = 2000
	maxGovernanceDigestBytes     = 256
	maxSelfDeployIdentifierBytes = 256
	maxSelfDeploySummaryBytes    = 2000
	selfDeploySummaryPageSize    = 1
)

type OwnerInboxRespondBody = generated.OwnerInboxRespondRequest
type SelfDeployGateDecisionBody = generated.SelfDeployGateDecisionRequest

type projectSelfDeployReadiness struct {
	projectID         string
	repositoryID      string
	providerSignalRef string
	projectMissing    bool
	signal            *projectsv1.SelfDeploySignalResponse
	repositories      *projectsv1.ListRepositoriesResponse
}

type queryMetaParts struct {
	actorType string
	actorID   string
	requestID string
	traceID   *string
	sessionID *string
}

var validAgentRunStatuses = enumSet(generated.AgentRunStatusRequested, generated.AgentRunStatusStarting, generated.AgentRunStatusRunning, generated.AgentRunStatusWaiting, generated.AgentRunStatusCompleted, generated.AgentRunStatusFailed, generated.AgentRunStatusCancelled)
var validAgentSessionStatuses = enumSet(generated.AgentSessionStatusOpen, generated.AgentSessionStatusWaiting, generated.AgentSessionStatusCompleted, generated.AgentSessionStatusFailed, generated.AgentSessionStatusCancelled)
var validRuntimeObservationStates = enumSet(generated.RuntimeObservationStateNotCreated, generated.RuntimeObservationStateStoredRef, generated.RuntimeObservationStateLive, generated.RuntimeObservationStateUnavailable, generated.RuntimeObservationStateConflict)
var validAgentRuntimeJobStatuses = enumSet(generated.AgentRuntimeJobStatusPending, generated.AgentRuntimeJobStatusClaimed, generated.AgentRuntimeJobStatusRunning, generated.AgentRuntimeJobStatusSucceeded, generated.AgentRuntimeJobStatusFailed, generated.AgentRuntimeJobStatusCancelled, generated.AgentRuntimeJobStatusTimedOut)
var validAgentActivityKinds = enumSet(generated.AgentActivityKindLifecycle, generated.AgentActivityKindToolUse, generated.AgentActivityKindToolResult, generated.AgentActivityKindPermission, generated.AgentActivityKindProviderSignal, generated.AgentActivityKindRuntimeSignal, generated.AgentActivityKindCheckpoint, generated.AgentActivityKindOther)
var validAgentActivityStatuses = enumSet(generated.AgentActivityStatusPlanned, generated.AgentActivityStatusStarted, generated.AgentActivityStatusSucceeded, generated.AgentActivityStatusFailed, generated.AgentActivityStatusDenied, generated.AgentActivityStatusWaiting, generated.AgentActivityStatusCancelled, generated.AgentActivityStatusSkipped)
var validGovernanceTargetTypes = enumSet(generated.GovernanceTargetTypeTransition, generated.GovernanceTargetTypePullRequest, generated.GovernanceTargetTypeReleaseCandidate, generated.GovernanceTargetTypeRuntimeJob, generated.GovernanceTargetTypePolicyChange, generated.GovernanceTargetTypeDocument, generated.GovernanceTargetTypeMerge, generated.GovernanceTargetTypePostdeploy, generated.GovernanceTargetTypeRollback, generated.GovernanceTargetTypeSelfDeployPlan)
var validGovernanceDecisionSummaryKinds = enumSet(generated.GovernanceDecisionSummaryKindRiskAssessment, generated.GovernanceDecisionSummaryKindReviewSignal, generated.GovernanceDecisionSummaryKindGateRequest, generated.GovernanceDecisionSummaryKindGateDecision, generated.GovernanceDecisionSummaryKindReleaseDecisionPackage, generated.GovernanceDecisionSummaryKindReleaseDecision, generated.GovernanceDecisionSummaryKindBlockingSignal, generated.GovernanceDecisionSummaryKindReleaseSafetyState)
var validGovernanceDecisionAttentions = enumSet(generated.GovernanceDecisionAttentionPending, generated.GovernanceDecisionAttentionCompleted, generated.GovernanceDecisionAttentionBlocked, generated.GovernanceDecisionAttentionInformational)
var validGovernanceRiskClasses = enumSet(generated.GovernanceRiskClassR0, generated.GovernanceRiskClassR1, generated.GovernanceRiskClassR2, generated.GovernanceRiskClassR3)
var validGovernanceReviewOutcomes = enumSet(generated.GovernanceReviewOutcomePass, generated.GovernanceReviewOutcomePassWithNotes, generated.GovernanceReviewOutcomeBlock, generated.GovernanceReviewOutcomeRequestChanges, generated.GovernanceReviewOutcomeRaiseRisk, generated.GovernanceReviewOutcomeInformational)
var validGovernanceGateRequestStatuses = enumSet(generated.GovernanceGateRequestStatusRequested, generated.GovernanceGateRequestStatusDelivering, generated.GovernanceGateRequestStatusAwaitingDecision, generated.GovernanceGateRequestStatusResolved, generated.GovernanceGateRequestStatusExpired, generated.GovernanceGateRequestStatusCancelled)
var validGovernanceGateOutcomes = enumSet(generated.GovernanceGateOutcomeApprove, generated.GovernanceGateOutcomeApproveWithConditions, generated.GovernanceGateOutcomeRevise, generated.GovernanceGateOutcomeReject, generated.GovernanceGateOutcomeHold, generated.GovernanceGateOutcomeRollback, generated.GovernanceGateOutcomeEscalate)
var validGovernanceReleasePackageStatuses = enumSet(generated.GovernanceReleasePackageStatusDraft, generated.GovernanceReleasePackageStatusReady, generated.GovernanceReleasePackageStatusDecisionRequested, generated.GovernanceReleasePackageStatusClosed)
var validGovernanceReleaseDecisionStatuses = enumSet(generated.GovernanceReleaseDecisionStatusRequested, generated.GovernanceReleaseDecisionStatusResolved, generated.GovernanceReleaseDecisionStatusCancelled)
var validGovernanceReleaseDecisionOutcomes = enumSet(generated.GovernanceReleaseDecisionOutcomeGo, generated.GovernanceReleaseDecisionOutcomeGoWithConditions, generated.GovernanceReleaseDecisionOutcomeNoGo, generated.GovernanceReleaseDecisionOutcomeHold, generated.GovernanceReleaseDecisionOutcomeRollback, generated.GovernanceReleaseDecisionOutcomeFollowUpRequired)
var validGovernanceBlockingSignalStatuses = enumSet(generated.GovernanceBlockingSignalStatusActive, generated.GovernanceBlockingSignalStatusResolved, generated.GovernanceBlockingSignalStatusDismissed)
var validGovernanceSignalSeverities = enumSet(generated.GovernanceSignalSeverityInfo, generated.GovernanceSignalSeverityWarning, generated.GovernanceSignalSeverityBlocking, generated.GovernanceSignalSeverityCritical)
var validGovernanceEvidenceKinds = enumSet(generated.GovernanceEvidenceKindProviderComment, generated.GovernanceEvidenceKindProviderReview, generated.GovernanceEvidenceKindProviderCheck, generated.GovernanceEvidenceKindRuntimeSummary, generated.GovernanceEvidenceKindDocument, generated.GovernanceEvidenceKindRiskFactor, generated.GovernanceEvidenceKindReviewSignal, generated.GovernanceEvidenceKindInteractionCallback, generated.GovernanceEvidenceKindObjectRef, generated.GovernanceEvidenceKindCustom, generated.GovernanceEvidenceKindAgentAcceptance, generated.GovernanceEvidenceKindAgentRun, generated.GovernanceEvidenceKindAgentHumanGate)
var validSelfDeployPlanStatusFilters = enumSet(generated.SelfDeployPlanStatusFilterUnspecified, generated.SelfDeployPlanStatusFilterPendingApproval, generated.SelfDeployPlanStatusFilterApproved, generated.SelfDeployPlanStatusFilterRejected, generated.SelfDeployPlanStatusFilterCancelled, generated.SelfDeployPlanStatusFilterFailed)
var validSelfDeployPlanStatuses = enumSet(generated.SelfDeployPlanStatusUnspecified, generated.SelfDeployPlanStatusPendingApproval, generated.SelfDeployPlanStatusApproved, generated.SelfDeployPlanStatusRejected, generated.SelfDeployPlanStatusCancelled, generated.SelfDeployPlanStatusFailed)
var validSelfDeployPathCategories = enumSet(generated.SelfDeployPathCategoryUnspecified, generated.SelfDeployPathCategoryServiceSource, generated.SelfDeployPathCategoryServiceConfig, generated.SelfDeployPathCategoryDeployManifest, generated.SelfDeployPathCategoryRuntimeConfig, generated.SelfDeployPathCategoryDocumentation, generated.SelfDeployPathCategoryTest, generated.SelfDeployPathCategoryPlatformPolicy, generated.SelfDeployPathCategoryOther, generated.SelfDeployPathCategoryServicesPolicy)
var validSelfDeployGateDecisionActions = enumSet(generated.SelfDeployGateDecisionActionApprove, generated.SelfDeployGateDecisionActionReject, generated.SelfDeployGateDecisionActionRequestChanges)
var selfDeployGateOutcomesByAction = map[generated.SelfDeployGateDecisionAction]governancev1.GateOutcome{
	generated.SelfDeployGateDecisionActionApprove:        governancev1.GateOutcome_GATE_OUTCOME_APPROVE,
	generated.SelfDeployGateDecisionActionReject:         governancev1.GateOutcome_GATE_OUTCOME_REJECT,
	generated.SelfDeployGateDecisionActionRequestChanges: governancev1.GateOutcome_GATE_OUTCOME_REVISE,
}

func ListAgentSessionsRequest(req *http.Request) (*agentsv1.ListAgentSessionsRequest, *SafeError) {
	meta, safeErr := agentQueryMeta(req)
	if safeErr != nil {
		return nil, safeErr
	}
	query := req.URL.Query()
	scope, safeErr := agentScopeFromQuery(req)
	if safeErr != nil {
		return nil, safeErr
	}
	status, safeErr := agentSessionStatusFromQuery(query.Get("status"))
	if safeErr != nil {
		return nil, safeErr
	}
	page, safeErr := agentPageFromQuery(req)
	if safeErr != nil {
		return nil, safeErr
	}
	return &agentsv1.ListAgentSessionsRequest{
		Meta:                meta,
		Scope:               scope,
		Status:              status,
		ProviderWorkItemRef: optionalString(query.Get("provider_work_item_ref")),
		CreatedByActorRef:   optionalString(query.Get("created_by_actor_ref")),
		CreatedAfter:        optionalString(query.Get("created_after")),
		CreatedBefore:       optionalString(query.Get("created_before")),
		Page:                page,
	}, nil
}

func ListAgentRunSummariesRequest(req *http.Request) (*agentsv1.ListAgentRunSummariesRequest, *SafeError) {
	meta, safeErr := agentQueryMeta(req)
	if safeErr != nil {
		return nil, safeErr
	}
	query := req.URL.Query()
	scope, safeErr := agentScopeFromQuery(req)
	if safeErr != nil {
		return nil, safeErr
	}
	status, safeErr := agentRunStatusFromQuery(query.Get("status"))
	if safeErr != nil {
		return nil, safeErr
	}
	sessionID, safeErr := optionalUUIDFromQuery(query.Get("session_id"), "session id is invalid")
	if safeErr != nil {
		return nil, safeErr
	}
	roleProfileID, safeErr := optionalUUIDFromQuery(query.Get("role_profile_id"), "role profile id is invalid")
	if safeErr != nil {
		return nil, safeErr
	}
	page, safeErr := agentPageFromQuery(req)
	if safeErr != nil {
		return nil, safeErr
	}
	return &agentsv1.ListAgentRunSummariesRequest{
		Meta:                   meta,
		Scope:                  scope,
		SessionId:              sessionID,
		RoleProfileId:          roleProfileID,
		Status:                 status,
		ProviderWorkItemRef:    optionalString(query.Get("provider_work_item_ref")),
		ProviderPullRequestRef: optionalString(query.Get("provider_pull_request_ref")),
		CreatedAfter:           optionalString(query.Get("created_after")),
		CreatedBefore:          optionalString(query.Get("created_before")),
		Page:                   page,
	}, nil
}

func GetAgentRunRuntimeStatusRequest(req *http.Request) (*agentsv1.GetAgentRunRuntimeStatusRequest, *SafeError) {
	meta, safeErr := agentQueryMeta(req)
	if safeErr != nil {
		return nil, safeErr
	}
	runID, safeErr := runIDFromPath(req)
	if safeErr != nil {
		return nil, safeErr
	}
	return &agentsv1.GetAgentRunRuntimeStatusRequest{Meta: meta, RunId: runID}, nil
}

func ListAgentActivitiesRequest(req *http.Request) (*agentsv1.ListAgentActivitiesRequest, *SafeError) {
	meta, safeErr := agentQueryMeta(req)
	if safeErr != nil {
		return nil, safeErr
	}
	runID, safeErr := runIDFromPath(req)
	if safeErr != nil {
		return nil, safeErr
	}
	activityKind, safeErr := activityKindFromQuery(req.URL.Query().Get("activity_kind"))
	if safeErr != nil {
		return nil, safeErr
	}
	activityStatus, safeErr := activityStatusFromQuery(req.URL.Query().Get("status"))
	if safeErr != nil {
		return nil, safeErr
	}
	page, safeErr := agentPageFromQuery(req)
	if safeErr != nil {
		return nil, safeErr
	}
	return &agentsv1.ListAgentActivitiesRequest{
		Meta:         meta,
		RunId:        optionalString(runID),
		ActivityKind: activityKind,
		Status:       activityStatus,
		Page:         page,
	}, nil
}

func GetGovernanceSummaryRequest(req *http.Request) (*governancev1.GetGovernanceSummaryRequest, *SafeError) {
	request := &governancev1.GetGovernanceSummaryRequest{}
	var safeErr *SafeError
	request.Meta, safeErr = governanceQueryMeta(req)
	if safeErr != nil {
		return nil, safeErr
	}
	request.Scope, safeErr = governanceSummaryScopeFromQuery(req)
	if safeErr != nil {
		return nil, safeErr
	}
	return request, nil
}

func GetSelfDeploySummaryRequest(req *http.Request) (*agentsv1.ListSelfDeployPlansRequest, *SafeError) {
	meta, safeErr := agentQueryMeta(req)
	if safeErr != nil {
		return nil, safeErr
	}
	scope, safeErr := agentScopeFromQuery(req)
	if safeErr != nil {
		return nil, safeErr
	}
	query := req.URL.Query()
	status, safeErr := selfDeployPlanStatusFromQuery(query.Get("status"))
	if safeErr != nil {
		return nil, safeErr
	}
	return &agentsv1.ListSelfDeployPlansRequest{
		Meta:              meta,
		Scope:             scope,
		ProjectRef:        optionalString(query.Get("project_ref")),
		RepositoryRef:     optionalString(query.Get("repository_ref")),
		ProviderSignalRef: optionalString(query.Get("provider_signal_ref")),
		Status:            status,
		Page:              &agentsv1.PageRequest{PageSize: selfDeploySummaryPageSize},
	}, nil
}

func GetSelfDeployGovernanceSummaryRequest(req *http.Request, plan *agentsv1.SelfDeployPlan) (*governancev1.GetGovernanceSummaryRequest, *SafeError) {
	target := selfDeployPlanGovernanceTarget(plan)
	if target == nil {
		return nil, nil
	}
	meta, safeErr := governanceQueryMeta(req)
	if safeErr != nil {
		return nil, safeErr
	}
	return &governancev1.GetGovernanceSummaryRequest{
		Meta: meta,
		Scope: &governancev1.GovernanceSummaryScope{
			Target:         target,
			ProjectContext: selfDeployPlanGovernanceProjectContext(plan),
		},
	}, nil
}

func SubmitSelfDeployGateDecisionRequest(req *http.Request, body SelfDeployGateDecisionBody) (*governancev1.SubmitGateDecisionRequest, *SafeError) {
	gateRequestID, safeErr := gateRequestIDFromPath(req)
	if safeErr != nil {
		return nil, safeErr
	}
	outcome, safeErr := selfDeployGateOutcomeProto(body.Action)
	if safeErr != nil {
		return nil, safeErr
	}
	if body.ExpectedStatus != nil && *body.ExpectedStatus != generated.SelfDeployGovernanceStatusPending {
		return nil, NewSafeError(http.StatusConflict, CodeStaleVersion, "self-deploy gate expected status is not pending", false)
	}
	meta, actorRef, safeErr := governanceCommandMeta(req, body)
	if safeErr != nil {
		return nil, safeErr
	}
	return &governancev1.SubmitGateDecisionRequest{
		GateRequestId:          gateRequestID,
		DecisionActorRef:       actorRef,
		DecisionPolicyRef:      boundedString(trimmed(body.DecisionPolicyRef), maxSelfDeployIdentifierBytes),
		Outcome:                outcome,
		Reason:                 selfDeployDecisionReason(body),
		InteractionDeliveryRef: selfDeployInteractionDeliveryRef(gateRequestID, body),
		Meta:                   meta,
	}, nil
}

func GetSelfDeploySignalRequest(req *http.Request, planRequest *agentsv1.ListSelfDeployPlansRequest) (*projectsv1.GetSelfDeploySignalRequest, *SafeError) {
	projectID := selfDeployProjectCatalogID(planRequest)
	providerSignalRef := strings.TrimSpace(planRequest.GetProviderSignalRef())
	if projectID == "" || providerSignalRef == "" {
		return nil, nil
	}
	meta, safeErr := projectQueryMeta(req)
	if safeErr != nil {
		return nil, safeErr
	}
	return &projectsv1.GetSelfDeploySignalRequest{
		ProjectId:         projectID,
		RepositoryId:      optionalString(planRequest.GetRepositoryRef()),
		ProviderSignalKey: optionalString(providerSignalRef),
		Meta:              meta,
	}, nil
}

func ListSelfDeployRepositoriesRequest(req *http.Request, planRequest *agentsv1.ListSelfDeployPlansRequest) (*projectsv1.ListRepositoriesRequest, *SafeError) {
	projectID := selfDeployProjectCatalogID(planRequest)
	if projectID == "" || strings.TrimSpace(planRequest.GetProviderSignalRef()) != "" {
		return nil, nil
	}
	meta, safeErr := projectQueryMeta(req)
	if safeErr != nil {
		return nil, safeErr
	}
	return &projectsv1.ListRepositoriesRequest{
		ProjectId: projectID,
		Statuses:  []projectsv1.RepositoryStatus{projectsv1.RepositoryStatus_REPOSITORY_STATUS_ACTIVE},
		Page:      &projectsv1.PageRequest{PageSize: 1},
		Meta:      meta,
	}, nil
}

func selfDeployProjectID(planRequest *agentsv1.ListSelfDeployPlansRequest) string {
	if planRequest == nil {
		return ""
	}
	if projectRef := strings.TrimSpace(planRequest.GetProjectRef()); projectRef != "" {
		return projectRef
	}
	if scope := planRequest.GetScope(); scope.GetType() == agentsv1.AgentScopeType_AGENT_SCOPE_TYPE_PROJECT {
		return strings.TrimSpace(scope.GetRef())
	}
	return ""
}

func selfDeployProjectCatalogID(planRequest *agentsv1.ListSelfDeployPlansRequest) string {
	projectID := selfDeployProjectID(planRequest)
	if _, err := uuid.Parse(projectID); err != nil {
		return ""
	}
	return projectID
}

func selfDeployPlanGovernanceTarget(plan *agentsv1.SelfDeployPlan) *governancev1.TargetRef {
	if plan == nil {
		return nil
	}
	ref := selfDeployPlanGovernanceRef(plan.GetId())
	if ref == "" {
		return nil
	}
	return &governancev1.TargetRef{
		Type: governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_SELF_DEPLOY_PLAN,
		Ref:  ref,
	}
}

func selfDeployPlanGovernanceProjectContext(plan *agentsv1.SelfDeployPlan) *governancev1.ProjectContextRef {
	if plan == nil {
		return nil
	}
	context := &governancev1.ProjectContextRef{
		ProjectRef:    optionalString(plan.GetProjectRef()),
		RepositoryRef: optionalString(plan.GetRepositoryRef()),
	}
	if context.GetProjectRef() == "" && context.GetRepositoryRef() == "" {
		return nil
	}
	return context
}

func selfDeployPlanGovernanceRef(planID string) string {
	planID = strings.TrimSpace(planID)
	if planID == "" {
		return ""
	}
	if strings.HasPrefix(planID, "agent:self-deploy-plan:") {
		return planID
	}
	if _, err := uuid.Parse(planID); err == nil {
		return "agent:self-deploy-plan:" + planID
	}
	return planID
}

func selfDeployDecisionReason(body SelfDeployGateDecisionBody) string {
	comment := trimmed(body.Comment)
	if comment != "" {
		return boundedString(comment, maxSelfDeploySummaryBytes)
	}
	return "self_deploy_gate:" + string(body.Action)
}

func selfDeployInteractionDeliveryRef(gateRequestID string, body SelfDeployGateDecisionBody) *governancev1.InteractionDeliveryRef {
	decisionRef := firstNonEmpty(trimmed(body.InteractionDecisionRef), "staff-gateway/self-deploy-gate/"+gateRequestID)
	return &governancev1.InteractionDeliveryRef{
		RequestRef:  trimOptional(body.InteractionRequestRef),
		DeliveryRef: trimOptional(body.InteractionDeliveryRef),
		CallbackRef: trimOptional(body.InteractionCallbackRef),
		DecisionRef: optionalString(decisionRef),
	}
}

func runIDFromPath(req *http.Request) (string, *SafeError) {
	return uuidPathValue(req, "run_id", "run id is invalid")
}

func gateRequestIDFromPath(req *http.Request) (string, *SafeError) {
	return uuidPathValue(req, "gate_request_id", "gate request id is invalid")
}

func uuidPathValue(req *http.Request, name string, invalidMessage string) (string, *SafeError) {
	value := strings.TrimSpace(req.PathValue(name))
	if _, err := uuid.Parse(value); err != nil {
		return "", NewSafeError(http.StatusBadRequest, CodeInvalidRequest, invalidMessage, false)
	}
	return value, nil
}

func ListOwnerInboxItemsRequest(req *http.Request) (*interactionsv1.ListOwnerInboxItemsRequest, *SafeError) {
	query := req.URL.Query()
	meta, safeErr := queryMeta(req)
	if safeErr != nil {
		return nil, safeErr
	}
	scope, safeErr := scopeFromQuery(req)
	if safeErr != nil {
		return nil, safeErr
	}
	requestKinds, safeErr := requestKindsFromQuery(queryValues(query, "request_kind"))
	if safeErr != nil {
		return nil, safeErr
	}
	statuses, safeErr := requestStatusesFromQuery(queryValues(query, "status"))
	if safeErr != nil {
		return nil, safeErr
	}
	sourceOwnerKind, safeErr := sourceOwnerKindFromQuery(query.Get("source_owner_kind"))
	if safeErr != nil {
		return nil, safeErr
	}
	correlationRef, safeErr := optionalProtoRef(query.Get("correlation_kind"), query.Get("correlation_ref"), "correlation ref is invalid", newExternalRef)
	if safeErr != nil {
		return nil, safeErr
	}
	assigneeRef, safeErr := optionalProtoRef(query.Get("assignee_kind"), query.Get("assignee_ref"), "assignee ref is invalid", newActorRef)
	if safeErr != nil {
		return nil, safeErr
	}
	page, safeErr := pageFromQuery(req)
	if safeErr != nil {
		return nil, safeErr
	}
	return &interactionsv1.ListOwnerInboxItemsRequest{
		Meta:               meta,
		Scope:              scope,
		RequestKinds:       requestKinds,
		Statuses:           statuses,
		SourceOwnerKind:    sourceOwnerKind,
		SourceOwnerRef:     optionalString(query.Get("source_owner_ref")),
		AssigneeRef:        assigneeRef,
		ActorRef:           optionalString(query.Get("actor_ref")),
		CorrelationRef:     correlationRef,
		CorrelationId:      optionalString(query.Get("correlation_id")),
		IncludeDiagnostics: parseBool(query.Get("include_diagnostics")),
		Page:               page,
	}, nil
}

func GetOwnerInboxItemRequest(req *http.Request) (*interactionsv1.GetOwnerInboxItemRequest, *SafeError) {
	meta, safeErr := queryMeta(req)
	if safeErr != nil {
		return nil, safeErr
	}
	scope, safeErr := scopeFromQuery(req)
	if safeErr != nil {
		return nil, safeErr
	}
	requestID := strings.TrimSpace(req.PathValue("request_id"))
	if _, err := uuid.Parse(requestID); err != nil {
		return nil, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "request id is invalid", false)
	}
	assigneeRef, safeErr := optionalProtoRef(req.URL.Query().Get("assignee_kind"), req.URL.Query().Get("assignee_ref"), "assignee ref is invalid", newActorRef)
	if safeErr != nil {
		return nil, safeErr
	}
	return &interactionsv1.GetOwnerInboxItemRequest{
		Meta:               meta,
		RequestId:          requestID,
		Scope:              scope,
		AssigneeRef:        assigneeRef,
		IncludeDiagnostics: parseBool(req.URL.Query().Get("include_diagnostics")),
	}, nil
}

func RecordInteractionResponseRequest(req *http.Request, body OwnerInboxRespondBody) (*interactionsv1.RecordInteractionResponseRequest, *SafeError) {
	meta, actor, safeErr := commandMeta(req, body)
	if safeErr != nil {
		return nil, safeErr
	}
	requestID := strings.TrimSpace(req.PathValue("request_id"))
	if _, err := uuid.Parse(requestID); err != nil {
		return nil, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "request id is invalid", false)
	}
	action, safeErr := responseActionProto(string(body.Action))
	if safeErr != nil {
		return nil, safeErr
	}
	return &interactionsv1.RecordInteractionResponseRequest{
		Meta:                meta,
		RequestId:           requestID,
		ResponseAction:      action,
		RespondedByActorRef: actorRefString(actor),
		ResponseSummary:     body.ResponseSummary,
		ResponseObject:      objectRefProto(body.ResponseObject),
		SourceKind:          interactionsv1.InteractionResponseSourceKind_INTERACTION_RESPONSE_SOURCE_KIND_WEB_CONSOLE,
		SourceRef:           optionalString("staff-gateway/" + requestIDFromContext(req.Context())),
		OwnerDecisionRef:    body.OwnerDecisionRef,
	}, nil
}

func ListOwnerInboxItemsResponse(response *interactionsv1.ListOwnerInboxItemsResponse, requestID string) (generated.OwnerInboxListResponse, *SafeError) {
	return castListEnvelope(
		response,
		requestID,
		"interaction-hub returned empty response",
		ownerInboxItemsOf,
		ownerInboxListPageOf,
		ownerInboxItem,
		ownerInboxListBuild,
	)
}

func OwnerInboxItemResponse(response *interactionsv1.OwnerInboxItemResponse, requestID string) (generated.OwnerInboxItemResponse, *SafeError) {
	if response == nil {
		return generated.OwnerInboxItemResponse{}, NewSafeError(http.StatusServiceUnavailable, CodeDownstreamUnavailable, "interaction-hub returned empty response", true)
	}
	item, safeErr := ownerInboxItem(response.GetItem())
	if safeErr != nil {
		return generated.OwnerInboxItemResponse{}, safeErr
	}
	return generated.OwnerInboxItemResponse{RequestId: requestID, CorrelationId: optionalString(requestID), Item: item}, nil
}

func OwnerInboxRespondResponse(response *interactionsv1.InteractionResponseResponse, requestID string) (generated.OwnerInboxRespondResponse, *SafeError) {
	if response == nil || response.GetRequest() == nil || response.GetResponse() == nil {
		return generated.OwnerInboxRespondResponse{}, NewSafeError(http.StatusServiceUnavailable, CodeDownstreamUnavailable, "interaction-hub returned empty response", true)
	}
	item, safeErr := responseItem(response.GetRequest(), response.GetResponse())
	if safeErr != nil {
		return generated.OwnerInboxRespondResponse{}, safeErr
	}
	summary, safeErr := responseSummary(response.GetResponse())
	if safeErr != nil {
		return generated.OwnerInboxRespondResponse{}, safeErr
	}
	return generated.OwnerInboxRespondResponse{RequestId: requestID, CorrelationId: optionalString(requestID), Item: item, Response: summary}, nil
}

func castRepeated[In any, Out any](items []In, cast func(In) (Out, *SafeError)) ([]Out, *SafeError) {
	result := make([]Out, 0, len(items))
	for _, item := range items {
		casted, safeErr := cast(item)
		if safeErr != nil {
			return nil, safeErr
		}
		result = append(result, casted)
	}
	return result, nil
}

func castListEnvelope[Response any, ProtoItem any, HTTPItem any, HTTPResponse any](
	response *Response,
	requestID string,
	emptyMessage string,
	itemsOf func(*Response) []ProtoItem,
	pageOf func(*Response) generated.PageInfo,
	cast func(ProtoItem) (HTTPItem, *SafeError),
	build func(string, []HTTPItem, generated.PageInfo) HTTPResponse,
) (HTTPResponse, *SafeError) {
	var zero HTTPResponse
	if response == nil {
		return zero, NewSafeError(http.StatusServiceUnavailable, CodeDownstreamUnavailable, emptyMessage, true)
	}
	items, safeErr := castRepeated(itemsOf(response), cast)
	if safeErr != nil {
		return zero, safeErr
	}
	return build(requestID, items, pageOf(response)), nil
}

func ownerInboxItemsOf(response *interactionsv1.ListOwnerInboxItemsResponse) []*interactionsv1.OwnerInboxItem {
	return response.GetItems()
}

func ownerInboxListPageOf(response *interactionsv1.ListOwnerInboxItemsResponse) generated.PageInfo {
	return pageInfo(response.GetPage())
}

func ownerInboxListBuild(requestID string, items []generated.OwnerInboxItem, page generated.PageInfo) generated.OwnerInboxListResponse {
	return generated.OwnerInboxListResponse{RequestId: requestID, CorrelationId: optionalString(requestID), Items: items, Page: page}
}

func agentSessionItemsOf(response *agentsv1.ListAgentSessionsResponse) []*agentsv1.AgentSessionListItem {
	return response.GetSessions()
}

func agentSessionListPageOf(response *agentsv1.ListAgentSessionsResponse) generated.PageInfo {
	return agentPageInfo(response.GetPage())
}

func agentSessionListBuild(requestID string, sessions []generated.AgentSessionSummary, page generated.PageInfo) generated.AgentSessionListResponse {
	return generated.AgentSessionListResponse{RequestId: requestID, CorrelationId: optionalString(requestID), Sessions: sessions, Page: page}
}

func agentRunItemsOf(response *agentsv1.ListAgentRunSummariesResponse) []*agentsv1.AgentRunListItem {
	return response.GetRuns()
}

func agentRunListPageOf(response *agentsv1.ListAgentRunSummariesResponse) generated.PageInfo {
	return agentPageInfo(response.GetPage())
}

func agentRunListBuild(requestID string, runs []generated.AgentRunSummary, page generated.PageInfo) generated.AgentRunSummaryListResponse {
	return generated.AgentRunSummaryListResponse{RequestId: requestID, CorrelationId: optionalString(requestID), Runs: runs, Page: page}
}

func AgentSessionListResponse(response *agentsv1.ListAgentSessionsResponse, requestID string) (generated.AgentSessionListResponse, *SafeError) {
	return castListEnvelope(
		response,
		requestID,
		"agent-manager returned empty session list response",
		agentSessionItemsOf,
		agentSessionListPageOf,
		agentSessionSummary,
		agentSessionListBuild,
	)
}

func AgentRunSummaryListResponse(response *agentsv1.ListAgentRunSummariesResponse, requestID string) (generated.AgentRunSummaryListResponse, *SafeError) {
	return castListEnvelope(
		response,
		requestID,
		"agent-manager returned empty run summary list response",
		agentRunItemsOf,
		agentRunListPageOf,
		agentRunSummary,
		agentRunListBuild,
	)
}

func AgentRunRuntimeStatusResponse(response *agentsv1.AgentRunRuntimeStatusResponse, requestID string) (generated.AgentRunRuntimeStatusResponse, *SafeError) {
	if response == nil || response.GetRuntimeStatus() == nil {
		return generated.AgentRunRuntimeStatusResponse{}, NewSafeError(http.StatusServiceUnavailable, CodeDownstreamUnavailable, "agent-manager returned empty runtime status", true)
	}
	status, safeErr := agentRunRuntimeStatus(response.GetRuntimeStatus())
	if safeErr != nil {
		return generated.AgentRunRuntimeStatusResponse{}, safeErr
	}
	return generated.AgentRunRuntimeStatusResponse{
		RequestId:     requestID,
		CorrelationId: optionalString(requestID),
		RuntimeStatus: status,
	}, nil
}

func AgentRunActivitiesResponse(response *agentsv1.ListAgentActivitiesResponse, runID string, requestID string) (generated.AgentRunActivitiesResponse, *SafeError) {
	if response == nil {
		return generated.AgentRunActivitiesResponse{}, NewSafeError(http.StatusServiceUnavailable, CodeDownstreamUnavailable, "agent-manager returned empty activities response", true)
	}
	activities := make([]generated.AgentRunActivity, 0, len(response.GetActivities()))
	for _, activity := range response.GetActivities() {
		casted, safeErr := agentRunActivity(activity, runID)
		if safeErr != nil {
			return generated.AgentRunActivitiesResponse{}, safeErr
		}
		activities = append(activities, casted)
	}
	return generated.AgentRunActivitiesResponse{
		RequestId:     requestID,
		CorrelationId: optionalString(requestID),
		RunId:         optionalString(runID),
		Activities:    activities,
		Page:          agentPageInfo(response.GetPage()),
	}, nil
}

func GovernanceSummaryResponse(response *governancev1.GovernanceSummaryResponse, requestID string) (generated.GovernanceSummaryResponse, *SafeError) {
	if response == nil || response.GetSummary() == nil {
		return generated.GovernanceSummaryResponse{}, NewSafeError(http.StatusServiceUnavailable, CodeDownstreamUnavailable, "governance-manager returned empty summary", true)
	}
	summary, safeErr := governanceSummary(response.GetSummary())
	if safeErr != nil {
		return generated.GovernanceSummaryResponse{}, safeErr
	}
	output := generated.GovernanceSummaryResponse{Summary: summary}
	output.RequestId = requestID
	output.CorrelationId = optionalString(requestID)
	return output, nil
}

func SelfDeploySummaryResponse(response *agentsv1.ListSelfDeployPlansResponse, readiness *projectSelfDeployReadiness, request *agentsv1.ListSelfDeployPlansRequest, governance *governancev1.GovernanceSummaryResponse, requestID string) (generated.SelfDeploySummaryResponse, *SafeError) {
	if response == nil {
		return generated.SelfDeploySummaryResponse{}, NewSafeError(http.StatusServiceUnavailable, CodeDownstreamUnavailable, "agent-manager returned empty self-deploy summary response", true)
	}
	summary, safeErr := selfDeploySummary(firstSelfDeployPlan(response.GetSelfDeployPlans()), readiness, request, governance.GetSummary())
	if safeErr != nil {
		return generated.SelfDeploySummaryResponse{}, safeErr
	}
	return generated.SelfDeploySummaryResponse{
		RequestId:     requestID,
		CorrelationId: optionalString(requestID),
		Summary:       summary,
	}, nil
}

func SelfDeployGateDecisionResponse(response *governancev1.GateDecisionResponse, body SelfDeployGateDecisionBody, gateRequestID string, requestID string) (generated.SelfDeployGateDecisionResponse, *SafeError) {
	if response == nil || response.GetGateDecision() == nil || response.GetGateRequest() == nil {
		return generated.SelfDeployGateDecisionResponse{}, NewSafeError(http.StatusServiceUnavailable, CodeDownstreamUnavailable, "governance-manager returned empty gate decision response", true)
	}
	decision := response.GetGateDecision()
	gateRequest := response.GetGateRequest()
	action, ok := selfDeployGateDecisionAction(decision.GetOutcome())
	if !ok {
		return generated.SelfDeployGateDecisionResponse{}, NewSafeError(http.StatusServiceUnavailable, CodeDownstreamUnavailable, "governance-manager returned unsupported self-deploy gate outcome", true)
	}
	status := generated.SelfDeployGovernanceStatusResolved
	if !selfDeployGateRequestTerminal(gateRequest.GetStatus()) {
		status = generated.SelfDeployGovernanceStatusPending
	}
	summary := boundedString(firstNonEmpty(decision.GetReason(), gateRequest.GetEvidenceSummary(), "self-deploy gate decision recorded"), maxSelfDeploySummaryBytes)
	output := generated.SelfDeployGateDecisionResponse{
		RequestId:     requestID,
		CorrelationId: optionalString(requestID),
		Decision: generated.SelfDeployGateDecisionSummary{
			SelfDeployPlanRef: boundedString(body.SelfDeployPlanRef, maxSelfDeployIdentifierBytes),
			GateRequestRef:    boundedString(firstNonEmpty(decision.GetGateRequestId(), gateRequest.GetId(), gateRequestID), maxSelfDeployIdentifierBytes),
			GateDecisionRef:   optionalBoundedString(decision.GetId(), maxSelfDeployIdentifierBytes),
			Outcome:           protoEnum(decision.GetOutcome().String(), "GATE_OUTCOME_", generated.GovernanceGateOutcomeUnspecified, validGovernanceGateOutcomes),
			Action:            action,
			Status:            status,
			GateRequestStatus: optionalGateRequestStatus(gateRequest.GetStatus()),
			Summary:           summary,
			DecidedAt:         optionalTime(decision.GetDecidedAt()),
			Version:           optionalPositiveInt64(gateRequest.GetVersion()),
		},
	}
	return output, nil
}

func firstSelfDeployPlan(plans []*agentsv1.SelfDeployPlan) *agentsv1.SelfDeployPlan {
	if len(plans) == 0 {
		return nil
	}
	return plans[0]
}

func queryMeta(req *http.Request) (*interactionsv1.QueryMeta, *SafeError) {
	actor, safeErr := interactionActorFromHeaders(req)
	if safeErr != nil {
		return nil, safeErr
	}
	return &interactionsv1.QueryMeta{
		Actor:     actor,
		RequestId: requestIDFromContext(req.Context()),
		RequestContext: &interactionsv1.RequestContext{
			Source:    "staff-gateway",
			TraceId:   optionalString(traceID(req)),
			SessionId: optionalString(req.Header.Get(headerSessionID)),
		},
	}, nil
}

func agentQueryMeta(req *http.Request) (*agentsv1.QueryMeta, *SafeError) {
	parts, safeErr := queryMetaPartsFromRequest(req)
	if safeErr != nil {
		return nil, safeErr
	}
	return &agentsv1.QueryMeta{
		Actor:     &agentsv1.Actor{Type: parts.actorType, Id: parts.actorID},
		RequestId: parts.requestID,
		RequestContext: &agentsv1.RequestContext{
			Source:    "staff-gateway",
			TraceId:   parts.traceID,
			SessionId: parts.sessionID,
		},
	}, nil
}

func governanceQueryMeta(req *http.Request) (*governancev1.QueryMeta, *SafeError) {
	actorType, actorID, safeErr := actorPartsFromHeaders(req)
	if safeErr != nil {
		return nil, safeErr
	}
	meta := &governancev1.QueryMeta{Actor: &governancev1.Actor{Type: actorType, Id: actorID}}
	meta.RequestId = requestIDFromContext(req.Context())
	meta.RequestContext = &governancev1.RequestContext{
		Source:    "staff-gateway",
		TraceId:   optionalString(traceID(req)),
		SessionId: optionalString(req.Header.Get(headerSessionID)),
	}
	return meta, nil
}

func projectQueryMeta(req *http.Request) (*projectsv1.QueryMeta, *SafeError) {
	parts, safeErr := queryMetaPartsFromRequest(req)
	if safeErr != nil {
		return nil, safeErr
	}
	meta := &projectsv1.QueryMeta{
		Actor:          &projectsv1.Actor{Type: parts.actorType, Id: parts.actorID},
		RequestId:      parts.requestID,
		RequestContext: &projectsv1.RequestContext{Source: "staff-gateway"},
	}
	meta.RequestContext.TraceId = parts.traceID
	meta.RequestContext.SessionId = parts.sessionID
	return meta, nil
}

func queryMetaPartsFromRequest(req *http.Request) (queryMetaParts, *SafeError) {
	actorType, actorID, safeErr := actorPartsFromHeaders(req)
	if safeErr != nil {
		return queryMetaParts{}, safeErr
	}
	return queryMetaParts{
		actorType: actorType,
		actorID:   actorID,
		requestID: requestIDFromContext(req.Context()),
		traceID:   optionalString(traceID(req)),
		sessionID: optionalString(req.Header.Get(headerSessionID)),
	}, nil
}

func commandMeta(req *http.Request, body OwnerInboxRespondBody) (*interactionsv1.CommandMeta, *interactionsv1.Actor, *SafeError) {
	actor, safeErr := interactionActorFromHeaders(req)
	if safeErr != nil {
		return nil, nil, safeErr
	}
	if trimmed(body.CommandId) == "" && trimmed(body.IdempotencyKey) == "" {
		return nil, nil, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "command id or idempotency key is required", false)
	}
	if body.ExpectedVersion <= 0 {
		return nil, nil, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "expected version is required", false)
	}
	reason := trimmed(body.Reason)
	if reason == "" {
		reason = "staff-gateway owner inbox response"
	}
	meta := &interactionsv1.CommandMeta{
		CommandId:       trimOptional(body.CommandId),
		IdempotencyKey:  trimOptional(body.IdempotencyKey),
		ExpectedVersion: &body.ExpectedVersion,
		Actor:           actor,
		Reason:          reason,
		RequestId:       requestIDFromContext(req.Context()),
		RequestContext: &interactionsv1.RequestContext{
			Source:    "staff-gateway",
			TraceId:   optionalString(traceID(req)),
			SessionId: optionalString(req.Header.Get(headerSessionID)),
		},
	}
	return meta, actor, nil
}

func governanceCommandMeta(req *http.Request, body SelfDeployGateDecisionBody) (*governancev1.CommandMeta, string, *SafeError) {
	parts, safeErr := queryMetaPartsFromRequest(req)
	if safeErr != nil {
		return nil, "", safeErr
	}
	if strings.TrimSpace(body.IdempotencyKey) == "" {
		return nil, "", NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "idempotency key is required", false)
	}
	if body.ExpectedVersion <= 0 {
		return nil, "", NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "expected version is required", false)
	}
	comment := trimmed(body.Comment)
	if len([]byte(comment)) > maxSelfDeploySummaryBytes {
		return nil, "", NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "comment is too long", false)
	}
	meta := &governancev1.CommandMeta{
		IdempotencyKey:  optionalString(body.IdempotencyKey),
		ExpectedVersion: &body.ExpectedVersion,
		Actor:           &governancev1.Actor{Type: parts.actorType, Id: parts.actorID},
		Reason:          selfDeployDecisionReason(body),
		RequestId:       parts.requestID,
		RequestContext: &governancev1.RequestContext{
			Source:    "staff-gateway",
			TraceId:   parts.traceID,
			SessionId: parts.sessionID,
		},
	}
	return meta, parts.actorType + "/" + parts.actorID, nil
}

func interactionActorFromHeaders(req *http.Request) (*interactionsv1.Actor, *SafeError) {
	actorType, actorID, safeErr := actorPartsFromHeaders(req)
	if safeErr != nil {
		return nil, safeErr
	}
	return &interactionsv1.Actor{Type: actorType, Id: actorID}, nil
}

func actorPartsFromHeaders(req *http.Request) (string, string, *SafeError) {
	actorType := strings.TrimSpace(req.Header.Get(headerActorType))
	actorID := strings.TrimSpace(req.Header.Get(headerActorID))
	if actorID == "" || len(actorID) > 256 || !validActorType(actorType) {
		return "", "", NewSafeError(http.StatusUnauthorized, CodeUnauthenticated, "actor context is required", false)
	}
	return actorType, actorID, nil
}

func validActorType(value string) bool {
	switch value {
	case "user", "service", "agent", "external_account":
		return true
	default:
		return false
	}
}

func actorRefString(actor *interactionsv1.Actor) string {
	return actor.GetType() + "/" + actor.GetId()
}

func traceID(req *http.Request) string {
	if value := strings.TrimSpace(req.Header.Get(headerTraceID)); value != "" {
		return value
	}
	return requestIDFromContext(req.Context())
}

func scopeFromQuery(req *http.Request) (*interactionsv1.ScopeRef, *SafeError) {
	return queryScopeRef(req, scopeTypeProto, interactionScopeRefBuild)
}

func agentScopeFromQuery(req *http.Request) (*agentsv1.ScopeRef, *SafeError) {
	return queryScopeRef(req, agentScopeTypeProto, agentScopeRefBuild)
}

func queryScopeRef[ScopeType any, ScopeRef any](req *http.Request, parse func(string) (ScopeType, *SafeError), build func(ScopeType, string) ScopeRef) (ScopeRef, *SafeError) {
	var zero ScopeRef
	scopeType, safeErr := parse(req.URL.Query().Get("scope_type"))
	if safeErr != nil {
		return zero, safeErr
	}
	scopeRef := strings.TrimSpace(req.URL.Query().Get("scope_ref"))
	if scopeRef == "" {
		return zero, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "scope ref is required", false)
	}
	return build(scopeType, scopeRef), nil
}

func interactionScopeRefBuild(scopeType interactionsv1.InteractionScopeType, scopeRef string) *interactionsv1.ScopeRef {
	return &interactionsv1.ScopeRef{Type: scopeType, Ref: scopeRef}
}

func agentScopeRefBuild(scopeType agentsv1.AgentScopeType, scopeRef string) *agentsv1.ScopeRef {
	return &agentsv1.ScopeRef{Type: scopeType, Ref: scopeRef}
}

func scopeTypeProto(value string) (interactionsv1.InteractionScopeType, *SafeError) {
	switch strings.TrimSpace(value) {
	case "platform":
		return interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_PLATFORM, nil
	case "organization":
		return interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_ORGANIZATION, nil
	case "project":
		return interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_PROJECT, nil
	case "repository":
		return interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_REPOSITORY, nil
	case "service":
		return interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_SERVICE, nil
	default:
		return interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_UNSPECIFIED, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "scope type is invalid", false)
	}
}

func agentScopeTypeProto(value string) (agentsv1.AgentScopeType, *SafeError) {
	switch strings.TrimSpace(value) {
	case "platform":
		return agentsv1.AgentScopeType_AGENT_SCOPE_TYPE_PLATFORM, nil
	case "organization":
		return agentsv1.AgentScopeType_AGENT_SCOPE_TYPE_ORGANIZATION, nil
	case "project":
		return agentsv1.AgentScopeType_AGENT_SCOPE_TYPE_PROJECT, nil
	case "repository":
		return agentsv1.AgentScopeType_AGENT_SCOPE_TYPE_REPOSITORY, nil
	default:
		return agentsv1.AgentScopeType_AGENT_SCOPE_TYPE_UNSPECIFIED, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "agent scope type is invalid", false)
	}
}

func governanceSummaryScopeFromQuery(req *http.Request) (*governancev1.GovernanceSummaryScope, *SafeError) {
	query := req.URL.Query()
	scope := &governancev1.GovernanceSummaryScope{}
	selectorCount := 0

	target, safeErr := governanceTargetFromQuery(query.Get("target_type"), query.Get("target_ref"))
	if safeErr != nil {
		return nil, safeErr
	}
	if target != nil {
		scope.Target = target
		selectorCount++
	}

	projectContext, safeErr := governanceProjectContextFromQuery(query)
	if safeErr != nil {
		return nil, safeErr
	}
	if projectContext != nil {
		scope.ProjectContext = projectContext
		selectorCount++
	}

	if releaseCandidateRef := strings.TrimSpace(query.Get("release_candidate_ref")); releaseCandidateRef != "" {
		scope.ReleaseCandidateRef = &releaseCandidateRef
		selectorCount++
	}
	if packageID := strings.TrimSpace(query.Get("release_decision_package_id")); packageID != "" {
		if _, err := uuid.Parse(packageID); err != nil {
			return nil, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "release decision package id is invalid", false)
		}
		scope.ReleaseDecisionPackageId = &packageID
		selectorCount++
	}

	integrationRef, safeErr := governanceIntegrationRefFromQuery(query.Get("integration_domain"), query.Get("integration_kind"), query.Get("integration_ref"))
	if safeErr != nil {
		return nil, safeErr
	}
	if integrationRef != nil {
		scope.IntegrationRef = integrationRef
		selectorCount++
	}

	if selectorCount != 1 {
		return nil, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "exactly one governance summary selector is required", false)
	}
	return scope, nil
}

func governanceTargetFromQuery(kind string, ref string) (*governancev1.TargetRef, *SafeError) {
	kind, ref, safeErr := optionalRefParts(kind, ref, "target ref is invalid")
	if safeErr != nil || kind == "" {
		return nil, safeErr
	}
	targetType, safeErr := governanceTargetTypeProto(kind)
	if safeErr != nil {
		return nil, safeErr
	}
	return &governancev1.TargetRef{Type: targetType, Ref: ref}, nil
}

func governanceTargetTypeProto(value string) (governancev1.GovernanceTargetType, *SafeError) {
	switch strings.TrimSpace(value) {
	case "transition":
		return governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_TRANSITION, nil
	case "pull_request":
		return governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_PULL_REQUEST, nil
	case "release_candidate":
		return governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_RELEASE_CANDIDATE, nil
	case "runtime_job":
		return governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_RUNTIME_JOB, nil
	case "policy_change":
		return governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_POLICY_CHANGE, nil
	case "document":
		return governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_DOCUMENT, nil
	case "merge":
		return governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_MERGE, nil
	case "postdeploy":
		return governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_POSTDEPLOY, nil
	case "rollback":
		return governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_ROLLBACK, nil
	case "self_deploy_plan":
		return governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_SELF_DEPLOY_PLAN, nil
	default:
		return governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_UNSPECIFIED, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "target type is invalid", false)
	}
}

func governanceProjectContextFromQuery(query url.Values) (*governancev1.ProjectContextRef, *SafeError) {
	context := &governancev1.ProjectContextRef{
		ProjectRef:       optionalString(query.Get("project_ref")),
		RepositoryRef:    optionalString(query.Get("repository_ref")),
		ServiceRef:       optionalString(query.Get("service_ref")),
		BranchRulesRef:   optionalString(query.Get("branch_rules_ref")),
		ReleasePolicyRef: optionalString(query.Get("release_policy_ref")),
		ReleaseLineRef:   optionalString(query.Get("release_line_ref")),
	}
	if context.GetProjectRef() == "" &&
		context.GetRepositoryRef() == "" &&
		context.GetServiceRef() == "" &&
		context.GetBranchRulesRef() == "" &&
		context.GetReleasePolicyRef() == "" &&
		context.GetReleaseLineRef() == "" {
		return nil, nil
	}
	if context.GetProjectRef() == "" && context.GetRepositoryRef() == "" {
		return nil, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "project or repository ref is required", false)
	}
	return context, nil
}

func governanceIntegrationRefFromQuery(domain string, kind string, ref string) (*governancev1.ReleaseIntegrationRef, *SafeError) {
	domain = strings.TrimSpace(domain)
	kind = strings.TrimSpace(kind)
	ref = strings.TrimSpace(ref)
	if domain == "" && kind == "" && ref == "" {
		return nil, nil
	}
	if domain == "" || kind == "" || ref == "" {
		return nil, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "integration ref is invalid", false)
	}
	return &governancev1.ReleaseIntegrationRef{Domain: domain, Kind: kind, Ref: ref}, nil
}

func requestKindsFromQuery(values []string) ([]interactionsv1.InteractionRequestKind, *SafeError) {
	items := splitQueryValues(values)
	result := make([]interactionsv1.InteractionRequestKind, 0, len(items))
	for _, item := range items {
		switch item {
		case "feedback":
			result = append(result, interactionsv1.InteractionRequestKind_INTERACTION_REQUEST_KIND_FEEDBACK)
		case "approval":
			result = append(result, interactionsv1.InteractionRequestKind_INTERACTION_REQUEST_KIND_APPROVAL)
		case "human_gate":
			result = append(result, interactionsv1.InteractionRequestKind_INTERACTION_REQUEST_KIND_HUMAN_GATE)
		default:
			return nil, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "request kind is invalid", false)
		}
	}
	return result, nil
}

func requestStatusesFromQuery(values []string) ([]interactionsv1.InteractionRequestStatus, *SafeError) {
	items := splitQueryValues(values)
	result := make([]interactionsv1.InteractionRequestStatus, 0, len(items))
	for _, item := range items {
		status, ok := map[string]interactionsv1.InteractionRequestStatus{
			"created":   interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_CREATED,
			"routed":    interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_ROUTED,
			"waiting":   interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_WAITING,
			"answered":  interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_ANSWERED,
			"expired":   interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_EXPIRED,
			"cancelled": interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_CANCELLED,
			"failed":    interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_FAILED,
		}[item]
		if !ok {
			return nil, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "request status is invalid", false)
		}
		result = append(result, status)
	}
	return result, nil
}

func agentSessionStatusFromQuery(value string) (*agentsv1.AgentSessionStatus, *SafeError) {
	return optionalAgentListStatus(value, "AGENT_SESSION_STATUS_", "session status is invalid", agentsv1.AgentSessionStatus_value, func(number int32) agentsv1.AgentSessionStatus {
		return agentsv1.AgentSessionStatus(number)
	})
}

func agentRunStatusFromQuery(value string) (*agentsv1.AgentRunStatus, *SafeError) {
	return optionalAgentListStatus(value, "AGENT_RUN_STATUS_", "run status is invalid", agentsv1.AgentRunStatus_value, func(number int32) agentsv1.AgentRunStatus {
		return agentsv1.AgentRunStatus(number)
	})
}

func activityKindFromQuery(value string) (*agentsv1.AgentActivityKind, *SafeError) {
	return optionalAgentProtoEnum(value, "AGENT_ACTIVITY_KIND_", "activity kind is invalid", agentsv1.AgentActivityKind_value, func(number int32) agentsv1.AgentActivityKind {
		return agentsv1.AgentActivityKind(number)
	})
}

func activityStatusFromQuery(value string) (*agentsv1.AgentActivityStatus, *SafeError) {
	return optionalAgentProtoEnum(value, "AGENT_ACTIVITY_STATUS_", "activity status is invalid", agentsv1.AgentActivityStatus_value, func(number int32) agentsv1.AgentActivityStatus {
		return agentsv1.AgentActivityStatus(number)
	})
}

func selfDeployPlanStatusFromQuery(value string) (*agentsv1.SelfDeployPlanStatus, *SafeError) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed == "unspecified" {
		return nil, nil
	}
	status := generated.SelfDeployPlanStatusFilter(trimmed)
	if _, ok := validSelfDeployPlanStatusFilters[status]; !ok {
		return nil, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "self-deploy plan status is invalid", false)
	}
	protoStatus, ok := agentsv1.SelfDeployPlanStatus_value["SELF_DEPLOY_PLAN_STATUS_"+strings.ToUpper(trimmed)]
	if !ok {
		return nil, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "self-deploy plan status is invalid", false)
	}
	item := agentsv1.SelfDeployPlanStatus(protoStatus)
	return &item, nil
}

func selfDeployGateOutcomeProto(value generated.SelfDeployGateDecisionAction) (governancev1.GateOutcome, *SafeError) {
	if _, ok := validSelfDeployGateDecisionActions[value]; !ok {
		return governancev1.GateOutcome_GATE_OUTCOME_UNSPECIFIED, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "self-deploy gate action is invalid", false)
	}
	return selfDeployGateOutcomesByAction[value], nil
}

func selfDeployGateDecisionAction(value governancev1.GateOutcome) (generated.SelfDeployGateDecisionAction, bool) {
	for action, outcome := range selfDeployGateOutcomesByAction {
		if outcome == value {
			return action, true
		}
	}
	return "", false
}

func optionalGateRequestStatus(status governancev1.GateRequestStatus) *generated.GovernanceGateRequestStatus {
	if status == governancev1.GateRequestStatus_GATE_REQUEST_STATUS_UNSPECIFIED {
		return nil
	}
	value := protoEnum(status.String(), "GATE_REQUEST_STATUS_", generated.GovernanceGateRequestStatusUnspecified, validGovernanceGateRequestStatuses)
	return &value
}

func selfDeployGateRequestTerminal(status governancev1.GateRequestStatus) bool {
	switch status {
	case governancev1.GateRequestStatus_GATE_REQUEST_STATUS_RESOLVED,
		governancev1.GateRequestStatus_GATE_REQUEST_STATUS_EXPIRED,
		governancev1.GateRequestStatus_GATE_REQUEST_STATUS_CANCELLED:
		return true
	default:
		return false
	}
}

func sourceOwnerKindFromQuery(value string) (*interactionsv1.SourceOwnerKind, *SafeError) {
	switch strings.TrimSpace(value) {
	case "":
		return nil, nil
	case "agent_manager":
		item := interactionsv1.SourceOwnerKind_SOURCE_OWNER_KIND_AGENT_MANAGER
		return &item, nil
	case "slot_agent":
		item := interactionsv1.SourceOwnerKind_SOURCE_OWNER_KIND_SLOT_AGENT
		return &item, nil
	case "governance_manager":
		item := interactionsv1.SourceOwnerKind_SOURCE_OWNER_KIND_GOVERNANCE_MANAGER
		return &item, nil
	case "provider_hub":
		item := interactionsv1.SourceOwnerKind_SOURCE_OWNER_KIND_PROVIDER_HUB
		return &item, nil
	case "operations_hub":
		item := interactionsv1.SourceOwnerKind_SOURCE_OWNER_KIND_OPERATIONS_HUB
		return &item, nil
	case "user":
		item := interactionsv1.SourceOwnerKind_SOURCE_OWNER_KIND_USER
		return &item, nil
	case "system":
		item := interactionsv1.SourceOwnerKind_SOURCE_OWNER_KIND_SYSTEM
		return &item, nil
	default:
		return nil, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "source owner kind is invalid", false)
	}
}

func responseActionProto(value string) (interactionsv1.InteractionResponseAction, *SafeError) {
	switch strings.TrimSpace(value) {
	case "answer":
		return interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_ANSWER, nil
	case "approve":
		return interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_APPROVE, nil
	case "reject":
		return interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_REJECT, nil
	case "defer":
		return interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_DEFER, nil
	case "acknowledge":
		return interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_ACKNOWLEDGE, nil
	case "custom":
		return interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_CUSTOM, nil
	case "request_changes":
		return interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_REQUEST_CHANGES, nil
	default:
		return interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_UNSPECIFIED, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "response action is invalid", false)
	}
}

func optionalProtoRef[T any](kind string, ref string, invalidMessage string, build func(string, string) *T) (*T, *SafeError) {
	kind, ref, safeErr := optionalRefParts(kind, ref, invalidMessage)
	if safeErr != nil || kind == "" {
		return nil, safeErr
	}
	return build(kind, ref), nil
}

func newActorRef(kind string, ref string) *interactionsv1.ActorRef {
	return &interactionsv1.ActorRef{RefKind: kind, Ref: ref}
}

func newExternalRef(kind string, ref string) *interactionsv1.ExternalRef {
	return &interactionsv1.ExternalRef{RefKind: kind, Ref: ref}
}

func optionalRefParts(kind string, ref string, invalidMessage string) (string, string, *SafeError) {
	kind = strings.TrimSpace(kind)
	ref = strings.TrimSpace(ref)
	if kind == "" && ref == "" {
		return "", "", nil
	}
	if kind == "" || ref == "" {
		return "", "", NewSafeError(http.StatusBadRequest, CodeInvalidRequest, invalidMessage, false)
	}
	return kind, ref, nil
}

func optionalAgentProtoEnum[Target ~int32](value string, prefix string, invalidMessage string, values map[string]int32, convert func(int32) Target) (*Target, *SafeError) {
	key := strings.TrimSpace(value)
	if key == "" {
		return nil, nil
	}
	number, ok := values[prefix+strings.ToUpper(key)]
	if !ok {
		return nil, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, invalidMessage, false)
	}
	item := convert(number)
	return &item, nil
}

func optionalAgentListStatus[Target ~int32](value string, prefix string, invalidMessage string, values map[string]int32, convert func(int32) Target) (*Target, *SafeError) {
	if strings.TrimSpace(value) == "unspecified" {
		return nil, nil
	}
	return optionalAgentProtoEnum(value, prefix, invalidMessage, values, convert)
}

func optionalUUIDFromQuery(value string, invalidMessage string) (*string, *SafeError) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, nil
	}
	if _, err := uuid.Parse(trimmed); err != nil {
		return nil, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, invalidMessage, false)
	}
	return &trimmed, nil
}

func pageFromQuery(req *http.Request) (*interactionsv1.PageRequest, *SafeError) {
	pageSize, pageToken, safeErr := pageParamsFromQuery(req)
	if safeErr != nil {
		return nil, safeErr
	}
	return &interactionsv1.PageRequest{PageSize: pageSize, PageToken: pageToken}, nil
}

func agentPageFromQuery(req *http.Request) (*agentsv1.PageRequest, *SafeError) {
	pageSize, pageToken, safeErr := pageParamsFromQuery(req)
	if safeErr != nil {
		return nil, safeErr
	}
	return &agentsv1.PageRequest{PageSize: pageSize, PageToken: pageToken}, nil
}

func pageParamsFromQuery(req *http.Request) (int32, *string, *SafeError) {
	query := req.URL.Query()
	pageSize := defaultPageSize
	if raw := strings.TrimSpace(query.Get("page_size")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			return 0, nil, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "page size is invalid", false)
		}
		pageSize = parsed
	}
	return int32(pageSize), optionalString(query.Get("page_token")), nil
}

func ownerInboxItem(item *interactionsv1.OwnerInboxItem) (generated.OwnerInboxItem, *SafeError) {
	if item == nil {
		return generated.OwnerInboxItem{}, NewSafeError(http.StatusServiceUnavailable, CodeDownstreamUnavailable, "interaction-hub returned empty owner inbox item", true)
	}
	if _, err := uuid.Parse(item.GetRequestId()); err != nil {
		return generated.OwnerInboxItem{}, NewSafeError(http.StatusServiceUnavailable, CodeDownstreamUnavailable, "interaction-hub returned invalid owner inbox item", true)
	}
	createdAt, safeErr := requiredTime(item.GetCreatedAt())
	if safeErr != nil {
		return generated.OwnerInboxItem{}, safeErr
	}
	updatedAt, safeErr := requiredTime(item.GetUpdatedAt())
	if safeErr != nil {
		return generated.OwnerInboxItem{}, safeErr
	}
	deliverySummary, safeErr := deliverySummary(item.GetDeliverySummary())
	if safeErr != nil {
		return generated.OwnerInboxItem{}, safeErr
	}
	output := generated.OwnerInboxItem{
		RequestId:         item.GetRequestId(),
		RequestKind:       generated.RequestKind(enumName(item.GetRequestKind().String(), "INTERACTION_REQUEST_KIND_")),
		RequestStatus:     generated.RequestStatus(enumName(item.GetRequestStatus().String(), "INTERACTION_REQUEST_STATUS_")),
		Scope:             scopeRef(item.GetScope()),
		Requester:         sourceOwnerRef(item.GetRequester()),
		DecisionOwner:     decisionOwnerRef(item.GetDecisionOwner()),
		AssigneeRefs:      actorRefs(item.GetAssigneeRefs()),
		ContextRefs:       externalRefs(item.GetContextRefs()),
		Title:             item.GetTitle(),
		Summary:           item.GetSummary(),
		DeadlineAt:        optionalTime(item.GetDeadlineAt()),
		ReminderPolicyRef: optionalString(item.GetReminderPolicyRef()),
		DeliverySummary:   deliverySummary,
		CreatedAt:         createdAt,
		UpdatedAt:         updatedAt,
		ResolvedAt:        optionalTime(item.GetResolvedAt()),
		Version:           item.GetVersion(),
		AllowedActions:    interactionActions(item.GetAllowedActions()),
	}
	if item.GetLatestCallback() != nil {
		casted, safeErr := callbackSummary(item.GetLatestCallback())
		if safeErr != nil {
			return generated.OwnerInboxItem{}, safeErr
		}
		output.LatestCallback = &casted
	}
	if item.GetLatestResponse() != nil {
		casted, safeErr := responseSummaryFromInbox(item.GetLatestResponse())
		if safeErr != nil {
			return generated.OwnerInboxItem{}, safeErr
		}
		output.LatestResponse = &casted
	}
	return output, nil
}

func responseItem(request *interactionsv1.InteractionRequest, response *interactionsv1.InteractionResponse) (generated.OwnerInboxItem, *SafeError) {
	item := &interactionsv1.OwnerInboxItem{
		RequestId:       request.GetId(),
		RequestKind:     request.GetRequestKind(),
		RequestStatus:   request.GetStatus(),
		Scope:           request.GetScope(),
		Requester:       request.GetSourceOwner(),
		DecisionOwner:   request.GetDecisionOwner(),
		AssigneeRefs:    request.GetTargetRefs(),
		ContextRefs:     request.GetContextRefs(),
		Title:           request.GetPromptSummary(),
		Summary:         request.GetPromptSummary(),
		DeadlineAt:      request.DeadlineAt,
		DeliverySummary: &interactionsv1.OwnerInboxDeliverySummary{},
		CreatedAt:       request.GetCreatedAt(),
		UpdatedAt:       request.GetUpdatedAt(),
		ResolvedAt:      request.ResolvedAt,
		Version:         request.GetVersion(),
		LatestResponse:  protoResponseSummary(response),
	}
	return ownerInboxItem(item)
}

func agentSessionSummary(item *agentsv1.AgentSessionListItem) (generated.AgentSessionSummary, *SafeError) {
	if item == nil || item.GetSession() == nil {
		return generated.AgentSessionSummary{}, NewSafeError(http.StatusServiceUnavailable, CodeDownstreamUnavailable, "agent-manager returned empty session summary", true)
	}
	session := item.GetSession()
	if _, err := uuid.Parse(session.GetId()); err != nil {
		return generated.AgentSessionSummary{}, NewSafeError(http.StatusServiceUnavailable, CodeDownstreamUnavailable, "agent-manager returned invalid session id", true)
	}
	createdAt, safeErr := requiredTime(session.GetCreatedAt())
	if safeErr != nil {
		return generated.AgentSessionSummary{}, safeErr
	}
	updatedAt, safeErr := requiredTime(session.GetUpdatedAt())
	if safeErr != nil {
		return generated.AgentSessionSummary{}, safeErr
	}
	scope, safeErr := agentScopeRef(session.GetScope())
	if safeErr != nil {
		return generated.AgentSessionSummary{}, safeErr
	}
	createdByActorRef, safeErr := requiredBoundedString(session.GetCreatedByActorRef(), maxAgentIdentifierBytes, "agent-manager returned invalid session actor ref")
	if safeErr != nil {
		return generated.AgentSessionSummary{}, safeErr
	}
	if item.GetActiveRunCount() < 0 {
		return generated.AgentSessionSummary{}, NewSafeError(http.StatusServiceUnavailable, CodeDownstreamUnavailable, "agent-manager returned invalid active run count", true)
	}
	output := generated.AgentSessionSummary{
		SessionId:             session.GetId(),
		Scope:                 scope,
		ProviderWorkItemRef:   optionalBoundedString(session.GetProviderWorkItemRef(), maxAgentIdentifierBytes),
		FlowVersionId:         optionalBoundedString(session.GetFlowVersionId(), maxAgentIdentifierBytes),
		CurrentStageId:        optionalBoundedString(session.GetCurrentStageId(), maxAgentIdentifierBytes),
		LatestStateSnapshotId: optionalBoundedString(session.GetLatestStateSnapshotId(), maxAgentIdentifierBytes),
		Status:                agentSessionStatus(session.GetStatus()),
		CreatedByActorRef:     createdByActorRef,
		LatestRunId:           optionalUUIDFromDownstream(item.GetLatestRunId()),
		LatestRuntimeJobRef:   optionalBoundedString(item.GetLatestRuntimeJobRef(), maxAgentIdentifierBytes),
		LatestRunSafeSummary:  optionalBoundedString(item.GetLatestRunSafeSummary(), maxAgentSafeTextBytes),
		ActiveRunCount:        int(item.GetActiveRunCount()),
		HumanGateWaiting:      item.GetHumanGateWaiting(),
		HumanGateRequestRef:   optionalBoundedString(item.GetHumanGateRequestRef(), maxAgentIdentifierBytes),
		HumanGateReasonCode:   optionalBoundedString(item.GetHumanGateReasonCode(), maxAgentIdentifierBytes),
		FollowUpWaiting:       item.GetFollowUpWaiting(),
		FollowUpRef:           optionalBoundedString(item.GetFollowUpRef(), maxAgentIdentifierBytes),
		CreatedAt:             createdAt,
		UpdatedAt:             updatedAt,
		Version:               session.GetVersion(),
	}
	if item.LatestRunStatus != nil {
		status := agentRunStatus(item.GetLatestRunStatus())
		output.LatestRunStatus = &status
	}
	if latest := agentActivitySummary(item.GetLatestActivity()); latest != nil {
		output.LatestActivity = latest
	}
	return output, nil
}

func agentRunSummary(item *agentsv1.AgentRunListItem) (generated.AgentRunSummary, *SafeError) {
	if item == nil || item.GetRun() == nil {
		return generated.AgentRunSummary{}, NewSafeError(http.StatusServiceUnavailable, CodeDownstreamUnavailable, "agent-manager returned empty run summary", true)
	}
	run := item.GetRun()
	if _, err := uuid.Parse(run.GetId()); err != nil {
		return generated.AgentRunSummary{}, NewSafeError(http.StatusServiceUnavailable, CodeDownstreamUnavailable, "agent-manager returned invalid run id", true)
	}
	if _, err := uuid.Parse(run.GetSessionId()); err != nil {
		return generated.AgentRunSummary{}, NewSafeError(http.StatusServiceUnavailable, CodeDownstreamUnavailable, "agent-manager returned invalid run session id", true)
	}
	roleProfileID, safeErr := requiredBoundedString(run.GetRoleProfileId(), maxAgentIdentifierBytes, "agent-manager returned invalid role profile id")
	if safeErr != nil {
		return generated.AgentRunSummary{}, safeErr
	}
	createdAt, safeErr := requiredTime(run.GetCreatedAt())
	if safeErr != nil {
		return generated.AgentRunSummary{}, safeErr
	}
	updatedAt, safeErr := requiredTime(run.GetUpdatedAt())
	if safeErr != nil {
		return generated.AgentRunSummary{}, safeErr
	}
	output := generated.AgentRunSummary{
		RunId:                   run.GetId(),
		SessionId:               run.GetSessionId(),
		FlowVersionId:           optionalBoundedString(run.GetFlowVersionId(), maxAgentIdentifierBytes),
		StageId:                 optionalBoundedString(run.GetStageId(), maxAgentIdentifierBytes),
		RoleProfileId:           roleProfileID,
		RoleProfileVersion:      run.GetRoleProfileVersion(),
		ProviderTarget:          agentProviderTarget(run.GetProviderTarget()),
		RuntimeSlotRef:          optionalBoundedString(run.GetRuntimeContext().GetSlotRef(), maxAgentIdentifierBytes),
		RuntimeContextRef:       optionalBoundedString(run.GetRuntimeContext().GetContextRef(), maxAgentIdentifierBytes),
		RuntimeJobRef:           firstBoundedString(maxAgentIdentifierBytes, item.GetRuntimeJobRef(), run.GetRuntimeContext().GetJobRef()),
		Status:                  agentRunStatus(run.GetStatus()),
		ResultSummary:           optionalBoundedString(run.GetResultSummary(), maxAgentSafeTextBytes),
		FailureCode:             optionalBoundedString(run.GetFailureCode(), maxAgentIdentifierBytes),
		RuntimeObservationState: runtimeObservationState(item.GetRuntimeObservationState()),
		RuntimeSafeErrorCode:    optionalBoundedString(item.GetRuntimeSafeErrorCode(), maxAgentIdentifierBytes),
		RuntimeSafeSummary:      optionalBoundedString(item.GetRuntimeSafeSummary(), maxAgentSafeTextBytes),
		HumanGateWaiting:        item.GetHumanGateWaiting(),
		HumanGateRequestRef:     optionalBoundedString(item.GetHumanGateRequestRef(), maxAgentIdentifierBytes),
		HumanGateReasonCode:     optionalBoundedString(item.GetHumanGateReasonCode(), maxAgentIdentifierBytes),
		FollowUpWaiting:         item.GetFollowUpWaiting(),
		LatestActivity:          agentActivitySummary(item.GetLatestActivity()),
		StartedAt:               optionalTime(run.GetStartedAt()),
		FinishedAt:              optionalTime(run.GetFinishedAt()),
		CreatedAt:               createdAt,
		UpdatedAt:               updatedAt,
		Version:                 run.GetVersion(),
	}
	return output, nil
}

func agentActivitySummary(input *agentsv1.AgentActivitySummary) *generated.AgentActivitySummary {
	if input == nil {
		return nil
	}
	updatedAt := optionalTime(input.GetUpdatedAt())
	if updatedAt == nil {
		return nil
	}
	activityID := boundedString(input.GetActivityId(), maxActivityIdentifierBytes)
	if activityID == "" {
		return nil
	}
	return &generated.AgentActivitySummary{
		ActivityId:    activityID,
		ActivityKind:  agentActivityKind(input.GetActivityKind()),
		Status:        agentActivityStatus(input.GetStatus()),
		ToolName:      optionalBoundedString(input.GetToolName(), maxActivityIdentifierBytes),
		ToolCategory:  optionalBoundedString(input.GetToolCategory(), maxActivityIdentifierBytes),
		SafeSummary:   optionalBoundedString(input.GetSafeSummary(), maxActivitySafeTextBytes),
		PayloadDigest: optionalBoundedString(input.GetPayloadDigest(), maxActivityDigestBytes),
		BoundedError:  optionalBoundedString(input.GetBoundedError(), maxActivitySafeTextBytes),
		StartedAt:     optionalTime(input.GetStartedAt()),
		FinishedAt:    optionalTime(input.GetFinishedAt()),
		UpdatedAt:     *updatedAt,
		Version:       input.GetVersion(),
	}
}

func agentProviderTarget(input *agentsv1.ProviderTargetRef) *generated.ProviderTargetRef {
	if input == nil {
		return nil
	}
	output := generated.ProviderTargetRef{
		WorkItemRef:     optionalBoundedString(input.GetWorkItemRef(), maxAgentIdentifierBytes),
		PullRequestRef:  optionalBoundedString(input.GetPullRequestRef(), maxAgentIdentifierBytes),
		CommentRef:      optionalBoundedString(input.GetCommentRef(), maxAgentIdentifierBytes),
		ReviewSignalRef: optionalBoundedString(input.GetReviewSignalRef(), maxAgentIdentifierBytes),
	}
	if output.WorkItemRef == nil && output.PullRequestRef == nil && output.CommentRef == nil && output.ReviewSignalRef == nil {
		return nil
	}
	return &output
}

func agentRunRuntimeStatus(status *agentsv1.AgentRunRuntimeStatus) (generated.AgentRunRuntimeStatus, *SafeError) {
	if _, err := uuid.Parse(status.GetRunId()); err != nil {
		return generated.AgentRunRuntimeStatus{}, NewSafeError(http.StatusServiceUnavailable, CodeDownstreamUnavailable, "agent-manager returned invalid runtime status", true)
	}
	runUpdatedAt, safeErr := requiredTime(status.GetRunUpdatedAt())
	if safeErr != nil {
		return generated.AgentRunRuntimeStatus{}, safeErr
	}
	return generated.AgentRunRuntimeStatus{
		RunId:                status.GetRunId(),
		RunStatus:            agentRunStatus(status.GetRunStatus()),
		ObservationState:     runtimeObservationState(status.GetObservationState()),
		RuntimeSlotRef:       optionalString(status.GetRuntimeContext().GetSlotRef()),
		RuntimeContextRef:    optionalString(status.GetRuntimeContext().GetContextRef()),
		RuntimeJobRef:        optionalString(status.GetRuntimeJobRef()),
		RuntimeJobStatus:     agentRuntimeJobStatus(status.GetRuntimeJobStatus()),
		RuntimeJobCommandRef: optionalString(status.GetRuntimeJobCommandRef()),
		RuntimeJobVersion:    optionalPositiveInt64(status.GetRuntimeJobVersion()),
		RuntimeJobCreatedAt:  optionalTime(status.GetRuntimeJobCreatedAt()),
		RuntimeJobStartedAt:  optionalTime(status.GetRuntimeJobStartedAt()),
		RuntimeJobFinishedAt: optionalTime(status.GetRuntimeJobFinishedAt()),
		RuntimeJobUpdatedAt:  optionalTime(status.GetRuntimeJobUpdatedAt()),
		SafeErrorCode:        optionalString(status.GetSafeErrorCode()),
		SafeSummary:          optionalString(status.GetSafeSummary()),
		RunStartedAt:         optionalTime(status.GetRunStartedAt()),
		RunFinishedAt:        optionalTime(status.GetRunFinishedAt()),
		RunUpdatedAt:         runUpdatedAt,
		RunVersion:           status.GetRunVersion(),
		HumanGateWaiting:     status.GetHumanGateWaiting(),
		HumanGateRequestRef:  optionalString(status.GetHumanGateRequestRef()),
		HumanGateReasonCode:  optionalString(status.GetHumanGateReasonCode()),
		FollowUpWaiting:      status.GetFollowUpWaiting(),
	}, nil
}

func agentRunActivity(activity *agentsv1.AgentActivity, runID string) (generated.AgentRunActivity, *SafeError) {
	if activity == nil {
		return generated.AgentRunActivity{}, NewSafeError(http.StatusServiceUnavailable, CodeDownstreamUnavailable, "agent-manager returned empty activity", true)
	}
	if activity.GetRunId() != "" && activity.GetRunId() != runID {
		return generated.AgentRunActivity{}, NewSafeError(http.StatusServiceUnavailable, CodeDownstreamUnavailable, "agent-manager returned activity for another run", true)
	}
	createdAt, safeErr := requiredTime(activity.GetCreatedAt())
	if safeErr != nil {
		return generated.AgentRunActivity{}, safeErr
	}
	updatedAt, safeErr := requiredTime(activity.GetUpdatedAt())
	if safeErr != nil {
		return generated.AgentRunActivity{}, safeErr
	}
	activityID, safeErr := requiredBoundedString(activity.GetId(), maxActivityIdentifierBytes, "agent-manager returned invalid activity id")
	if safeErr != nil {
		return generated.AgentRunActivity{}, safeErr
	}
	sessionID, safeErr := requiredBoundedString(activity.GetSessionId(), maxActivityIdentifierBytes, "agent-manager returned invalid activity session id")
	if safeErr != nil {
		return generated.AgentRunActivity{}, safeErr
	}
	output := generated.AgentRunActivity{
		ActivityId:      activityID,
		SessionId:       sessionID,
		RunId:           optionalString(runID),
		TurnId:          optionalBoundedString(activity.GetTurnId(), maxActivityIdentifierBytes),
		ToolUseId:       optionalBoundedString(activity.GetToolUseId(), maxActivityIdentifierBytes),
		ActivityKind:    agentActivityKind(activity.GetActivityKind()),
		ToolName:        optionalBoundedString(activity.GetToolName(), maxActivityIdentifierBytes),
		ToolCategory:    optionalBoundedString(activity.GetToolCategory(), maxActivityIdentifierBytes),
		Status:          agentActivityStatus(activity.GetStatus()),
		StartedAt:       optionalTime(activity.GetStartedAt()),
		FinishedAt:      optionalTime(activity.GetFinishedAt()),
		DurationMs:      optionalPositiveInt64(activity.GetDurationMs()),
		SafeSummary:     optionalBoundedString(activity.GetSafeSummary(), maxActivitySafeTextBytes),
		PayloadDigest:   optionalBoundedString(activity.GetPayloadDigest(), maxActivityDigestBytes),
		BoundedError:    optionalBoundedString(activity.GetBoundedError(), maxActivitySafeTextBytes),
		SafeRefsJson:    optionalBoundedString(activity.GetSafeRefsJson(), maxActivitySafeJSONBytes),
		SafeDetailsJson: optionalBoundedString(activity.GetSafeDetailsJson(), maxActivitySafeJSONBytes),
		CorrelationId:   optionalBoundedString(activity.GetCorrelationId(), maxActivityRefBytes),
		CreatedAt:       createdAt,
		UpdatedAt:       updatedAt,
		Version:         activity.GetVersion(),
	}
	return output, nil
}

func selfDeploySummary(plan *agentsv1.SelfDeployPlan, readiness *projectSelfDeployReadiness, request *agentsv1.ListSelfDeployPlansRequest, governance *governancev1.GovernanceSummary) (generated.SelfDeploySummary, *SafeError) {
	if plan == nil {
		return selfDeployPrePlanSummary(readiness, request), nil
	}
	chainStatus, nextStep, safeError := selfDeployPlanChain(plan)
	governanceSummary := selfDeployGovernanceSummary(plan.GetGovernanceContext(), governance)
	return generated.SelfDeploySummary{
		Availability:            generated.SelfDeploySummaryAvailabilityReady,
		ChainStatus:             chainStatus,
		NextStep:                nextStep,
		SelfDeployPlanId:        optionalBoundedString(plan.GetId(), maxSelfDeployIdentifierBytes),
		ProjectRef:              optionalBoundedString(plan.GetProjectRef(), maxSelfDeployIdentifierBytes),
		RepositoryRef:           optionalBoundedString(plan.GetRepositoryRef(), maxSelfDeployIdentifierBytes),
		SourceRef:               optionalBoundedString(plan.GetSourceRef(), maxSelfDeployIdentifierBytes),
		MergeCommitSha:          optionalBoundedString(plan.GetMergeCommitSha(), maxSelfDeployIdentifierBytes),
		ServicesYamlRef:         optionalBoundedString(plan.GetServicesYamlRef(), maxSelfDeployIdentifierBytes),
		ServicesYamlDigest:      optionalBoundedString(plan.GetServicesYamlDigest(), maxSelfDeployIdentifierBytes),
		PlanFingerprint:         optionalBoundedString(plan.GetPlanFingerprint(), maxSelfDeployIdentifierBytes),
		SafeSummary:             optionalBoundedString(plan.GetSafeSummary(), maxSelfDeploySummaryBytes),
		AffectedServiceKeys:     boundedStrings(plan.GetAffectedServiceKeys(), maxSelfDeployIdentifierBytes),
		PathCategories:          selfDeployPathCategories(plan.GetPathCategories()),
		ExpectedRuntimeJobTypes: selfDeployRuntimeJobTypes(plan.GetExpectedRuntimeJobTypes()),
		ProviderSignal:          selfDeployProviderSignal(plan.GetProviderSignalRef()),
		DeployPlan:              generated.SelfDeployPlanSummary{Status: selfDeployPlanStatus(plan.GetStatus())},
		Governance:              governanceSummary,
		Runtime:                 selfDeployRuntimeSummary(plan),
		SafeError:               safeError,
		CreatedAt:               optionalTime(plan.GetCreatedAt()),
		UpdatedAt:               optionalTime(plan.GetUpdatedAt()),
		Version:                 optionalPositiveInt64(plan.GetVersion()),
	}, nil
}

func selfDeployPrePlanSummary(readiness *projectSelfDeployReadiness, request *agentsv1.ListSelfDeployPlansRequest) generated.SelfDeploySummary {
	if readiness != nil {
		if readiness.projectMissing {
			return selfDeployUnavailableSummary(readiness.projectID, generated.ProjectMissing, generated.SelfDeployNextStep{
				Code:    generated.RestoreProject,
				Summary: "Проект не найден или недоступен для текущего actor context.",
			}, "project_missing", "Project не найден или недоступен.")
		}
		if readiness.signal != nil {
			return selfDeploySignalSummary(readiness)
		}
		if readiness.repositories != nil {
			return selfDeployRepositoryReadinessSummary(readiness)
		}
	}
	projectID := selfDeployProjectCatalogID(request)
	if projectID == "" {
		return selfDeployUnavailableSummary("", generated.NotConfigured, generated.SelfDeployNextStep{
			Code:    generated.ConfigureProject,
			Summary: "Project id не передан, поэтому self-deploy chain ещё не привязан к project-catalog project.",
		}, "self_deploy_project_not_configured", "Project id не задан для self-deploy наблюдения.")
	}
	return selfDeployUnavailableSummary(projectID, generated.WaitingForProviderSignal, generated.SelfDeployNextStep{
		Code:    generated.WaitProviderSignal,
		Summary: "Project известен, но provider signal ещё не передан в read surface.",
	}, "provider_signal_unavailable", "Provider signal ещё не найден или не передан в запрос.")
}

func selfDeploySignalSummary(readiness *projectSelfDeployReadiness) generated.SelfDeploySummary {
	response := readiness.signal
	signal := response.GetSignal()
	summary := generated.SelfDeploySummary{
		Availability:            generated.SelfDeploySummaryAvailabilityUnavailable,
		ProjectRef:              firstBoundedString(maxSelfDeployIdentifierBytes, signal.GetProjectRef(), readiness.projectID),
		RepositoryRef:           firstBoundedString(maxSelfDeployIdentifierBytes, signal.GetRepositoryRef(), readiness.repositoryID),
		SourceRef:               optionalBoundedString(signal.GetSourceRef(), maxSelfDeployIdentifierBytes),
		MergeCommitSha:          optionalBoundedString(signal.GetMergeCommitSha(), maxSelfDeployIdentifierBytes),
		ServicesYamlRef:         optionalBoundedString(signal.GetServicesYaml().GetServicesYamlRef(), maxSelfDeployIdentifierBytes),
		ServicesYamlDigest:      optionalBoundedString(signal.GetServicesYaml().GetServicesYamlDigest(), maxSelfDeployIdentifierBytes),
		PlanFingerprint:         optionalBoundedString(signal.GetProjectSignalFingerprint(), maxSelfDeployIdentifierBytes),
		SafeSummary:             optionalBoundedString(signal.GetSafeSummary(), maxSelfDeploySummaryBytes),
		AffectedServiceKeys:     boundedStrings(signal.GetAffectedServiceKeys(), maxSelfDeployIdentifierBytes),
		PathCategories:          selfDeployProjectPathCategories(signal.GetPathCategories()),
		ExpectedRuntimeJobTypes: selfDeployProjectRuntimeJobTypes(signal.GetExpectedRuntimeJobTypes()),
		ProviderSignal:          selfDeployProviderSignal(firstNonEmpty(signal.GetProviderSignalRef(), readiness.providerSignalRef)),
		DeployPlan:              generated.SelfDeployPlanSummary{Status: generated.SelfDeployPlanStatusUnavailable},
		Governance:              generated.SelfDeployGovernanceSummary{Status: generated.SelfDeployGovernanceStatusUnavailable},
		Runtime:                 generated.SelfDeployRuntimeSummary{Status: generated.SelfDeployRuntimeStatusUnavailable},
		UpdatedAt:               optionalTime(signal.GetObservedAt()),
		Version:                 optionalPositiveInt64(signal.GetVersion()),
	}
	summary.ChainStatus, summary.NextStep, summary.SafeError = selfDeploySignalChain(response)
	return summary
}

func selfDeployRepositoryReadinessSummary(readiness *projectSelfDeployReadiness) generated.SelfDeploySummary {
	if len(readiness.repositories.GetRepositories()) == 0 {
		return selfDeployUnavailableSummary(readiness.projectID, generated.RepositoryBindingMissing, generated.SelfDeployNextStep{
			Code:    generated.BindRepository,
			Summary: "У проекта нет active repository binding для self-deploy signal.",
		}, "repository_binding_missing", "Repository binding не найден для project.")
	}
	repository := readiness.repositories.GetRepositories()[0]
	return generated.SelfDeploySummary{
		Availability:            generated.SelfDeploySummaryAvailabilityUnavailable,
		ChainStatus:             generated.WaitingForProviderSignal,
		NextStep:                generated.SelfDeployNextStep{Code: generated.WaitProviderSignal, Summary: "Active repository binding найден, ожидается safe provider signal."},
		ProjectRef:              optionalBoundedString(firstNonEmpty(repository.GetProjectId(), readiness.projectID), maxSelfDeployIdentifierBytes),
		RepositoryRef:           optionalBoundedString(repository.GetRepositoryId(), maxSelfDeployIdentifierBytes),
		AffectedServiceKeys:     []string{},
		PathCategories:          []generated.SelfDeployPathCategory{},
		ExpectedRuntimeJobTypes: []string{},
		ProviderSignal:          generated.SelfDeployProviderSignalSummary{Status: generated.SelfDeployProviderSignalStatusUnavailable},
		DeployPlan:              generated.SelfDeployPlanSummary{Status: generated.SelfDeployPlanStatusUnavailable},
		Governance:              generated.SelfDeployGovernanceSummary{Status: generated.SelfDeployGovernanceStatusUnavailable},
		Runtime:                 generated.SelfDeployRuntimeSummary{Status: generated.SelfDeployRuntimeStatusUnavailable},
		SafeError: &generated.SelfDeploySafeError{
			Code:    "provider_signal_unavailable",
			Summary: "Repository binding найден, но provider signal ещё не сохранён или не передан.",
		},
		Version: optionalPositiveInt64(repository.GetVersion()),
	}
}

func selfDeployUnavailableSummary(projectRef string, chainStatus generated.SelfDeployChainStatus, nextStep generated.SelfDeployNextStep, errorCode string, errorSummary string) generated.SelfDeploySummary {
	return generated.SelfDeploySummary{
		Availability:            generated.SelfDeploySummaryAvailabilityUnavailable,
		ChainStatus:             chainStatus,
		NextStep:                nextStep,
		ProjectRef:              optionalBoundedString(projectRef, maxSelfDeployIdentifierBytes),
		AffectedServiceKeys:     []string{},
		PathCategories:          []generated.SelfDeployPathCategory{},
		ExpectedRuntimeJobTypes: []string{},
		ProviderSignal:          generated.SelfDeployProviderSignalSummary{Status: generated.SelfDeployProviderSignalStatusUnavailable},
		DeployPlan:              generated.SelfDeployPlanSummary{Status: generated.SelfDeployPlanStatusUnavailable},
		Governance:              generated.SelfDeployGovernanceSummary{Status: generated.SelfDeployGovernanceStatusUnavailable},
		Runtime:                 generated.SelfDeployRuntimeSummary{Status: generated.SelfDeployRuntimeStatusUnavailable},
		SafeError: &generated.SelfDeploySafeError{
			Code:    boundedString(errorCode, maxSelfDeployIdentifierBytes),
			Summary: boundedString(errorSummary, maxSelfDeploySummaryBytes),
		},
	}
}

func selfDeployPlanChain(plan *agentsv1.SelfDeployPlan) (generated.SelfDeployChainStatus, generated.SelfDeployNextStep, *generated.SelfDeploySafeError) {
	switch plan.GetStatus() {
	case agentsv1.SelfDeployPlanStatus_SELF_DEPLOY_PLAN_STATUS_PENDING_APPROVAL:
		governance := selfDeployGovernanceSummary(plan.GetGovernanceContext(), nil)
		if governance.Status == generated.SelfDeployGovernanceStatusPending {
			return generated.GovernanceGatePending, generated.SelfDeployNextStep{
				Code:    generated.ReviewGovernanceGate,
				Summary: "Self-deploy plan создан и ожидает governance/owner решение.",
			}, nil
		}
		return generated.PlanCreated, generated.SelfDeployNextStep{
			Code:    generated.WaitSelfDeployPlan,
			Summary: "Self-deploy plan создан, ожидается подготовка governance gate или дальнейшая обработка agent-manager.",
		}, nil
	case agentsv1.SelfDeployPlanStatus_SELF_DEPLOY_PLAN_STATUS_APPROVED:
		return generated.ApprovedReadyForBuild, generated.SelfDeployNextStep{
			Code:    generated.ReadyForBuild,
			Summary: "Self-deploy plan утверждён; agent-manager продвигает chain через build context, build job и deploy job.",
		}, nil
	case agentsv1.SelfDeployPlanStatus_SELF_DEPLOY_PLAN_STATUS_FAILED:
		return generated.Blocked, generated.SelfDeployNextStep{Code: generated.InspectBlocker, Summary: "Self-deploy plan завершился ошибкой; смотри safe summary."}, selfDeploySafeError("self_deploy_plan_failed", plan.GetSafeSummary())
	case agentsv1.SelfDeployPlanStatus_SELF_DEPLOY_PLAN_STATUS_REJECTED:
		return generated.Blocked, generated.SelfDeployNextStep{Code: generated.InspectBlocker, Summary: "Self-deploy plan отклонён владельцем или governance decision."}, selfDeploySafeError("self_deploy_plan_rejected", plan.GetSafeSummary())
	case agentsv1.SelfDeployPlanStatus_SELF_DEPLOY_PLAN_STATUS_CANCELLED:
		return generated.Blocked, generated.SelfDeployNextStep{Code: generated.InspectBlocker, Summary: "Self-deploy plan отменён."}, selfDeploySafeError("self_deploy_plan_cancelled", plan.GetSafeSummary())
	default:
		return generated.PlanCreated, generated.SelfDeployNextStep{
			Code:    generated.WaitSelfDeployPlan,
			Summary: "Self-deploy plan создан, но lifecycle status ещё не уточнён.",
		}, nil
	}
}

func selfDeploySignalChain(response *projectsv1.SelfDeploySignalResponse) (generated.SelfDeployChainStatus, generated.SelfDeployNextStep, *generated.SelfDeploySafeError) {
	reason := response.GetSafeReason()
	switch response.GetStatus() {
	case projectsv1.SelfDeploySignalStatus_SELF_DEPLOY_SIGNAL_STATUS_READY:
		return generated.ProviderSignalFound, generated.SelfDeployNextStep{Code: generated.WaitSelfDeployPlan, Summary: "Provider signal найден и готов для создания self-deploy plan."}, nil
	case projectsv1.SelfDeploySignalStatus_SELF_DEPLOY_SIGNAL_STATUS_REPOSITORY_BINDING_NOT_FOUND:
		return generated.RepositoryBindingMissing, generated.SelfDeployNextStep{Code: generated.BindRepository, Summary: "Provider signal найден, но active repository binding не найден или не совпадает."}, selfDeploySafeError("repository_binding_missing", reason)
	case projectsv1.SelfDeploySignalStatus_SELF_DEPLOY_SIGNAL_STATUS_NEEDS_SERVICES_POLICY_RECONCILE,
		projectsv1.SelfDeploySignalStatus_SELF_DEPLOY_SIGNAL_STATUS_SERVICES_POLICY_NOT_FOUND,
		projectsv1.SelfDeploySignalStatus_SELF_DEPLOY_SIGNAL_STATUS_SERVICES_POLICY_NOT_READY:
		return generated.NeedsServicesPolicyReconcile, generated.SelfDeployNextStep{Code: generated.ReconcileServicesPolicy, Summary: "Нужен reconcile checked services policy перед созданием self-deploy plan."}, selfDeploySafeError("needs_services_policy_reconcile", reason)
	case projectsv1.SelfDeploySignalStatus_SELF_DEPLOY_SIGNAL_STATUS_PROVIDER_SIGNAL_NOT_FOUND,
		projectsv1.SelfDeploySignalStatus_SELF_DEPLOY_SIGNAL_STATUS_PROVIDER_SIGNAL_NOT_READY:
		return generated.WaitingForProviderSignal, generated.SelfDeployNextStep{Code: generated.WaitProviderSignal, Summary: "Provider signal ещё не найден или не готов."}, selfDeploySafeError("provider_signal_unavailable", reason)
	default:
		return generated.Blocked, generated.SelfDeployNextStep{Code: generated.InspectBlocker, Summary: "Project-side self-deploy signal не готов; смотри safe reason."}, selfDeploySafeError("self_deploy_signal_blocked", reason)
	}
}

func selfDeploySafeError(code string, summary string) *generated.SelfDeploySafeError {
	if strings.TrimSpace(summary) == "" {
		summary = "Self-deploy chain остановлена на безопасной диагностике без raw payload."
	}
	return &generated.SelfDeploySafeError{
		Code:    boundedString(code, maxSelfDeployIdentifierBytes),
		Summary: boundedString(summary, maxSelfDeploySummaryBytes),
	}
}

func selfDeployProviderSignal(providerSignalRef string) generated.SelfDeployProviderSignalSummary {
	output := generated.SelfDeployProviderSignalSummary{Status: generated.SelfDeployProviderSignalStatusUnavailable}
	if ref := optionalBoundedString(providerSignalRef, maxSelfDeployIdentifierBytes); ref != nil {
		output.Status = generated.SelfDeployProviderSignalStatusStoredRef
		output.Ref = ref
	}
	return output
}

func selfDeployGovernanceSummary(input *agentsv1.GovernanceContextRef, governance *governancev1.GovernanceSummary) generated.SelfDeployGovernanceSummary {
	output := generated.SelfDeployGovernanceSummary{Status: generated.SelfDeployGovernanceStatusUnavailable}
	if input == nil {
		return output
	}
	output.GateRequestRef = optionalBoundedString(input.GetGateRequestRef(), maxSelfDeployIdentifierBytes)
	output.GateDecisionRef = optionalBoundedString(input.GetGateDecisionRef(), maxSelfDeployIdentifierBytes)
	output.ReleaseDecisionPackageRef = optionalBoundedString(input.GetReleaseDecisionPackageRef(), maxSelfDeployIdentifierBytes)
	output.ReleaseDecisionRef = optionalBoundedString(input.GetReleaseDecisionRef(), maxSelfDeployIdentifierBytes)
	output.GatePolicyRef = optionalBoundedString(input.GetGatePolicyRef(), maxSelfDeployIdentifierBytes)
	if output.GateDecisionRef != nil || output.ReleaseDecisionRef != nil {
		output.Status = generated.SelfDeployGovernanceStatusResolved
	} else if output.GateRequestRef != nil || output.ReleaseDecisionPackageRef != nil {
		output.Status = generated.SelfDeployGovernanceStatusPending
	}
	enrichSelfDeployGovernanceSummary(&output, governance)
	return output
}

func enrichSelfDeployGovernanceSummary(output *generated.SelfDeployGovernanceSummary, governance *governancev1.GovernanceSummary) {
	if output == nil || governance == nil {
		return
	}
	gateRefID := governanceLocalRefID(output.GateRequestRef)
	for _, item := range governance.GetPendingDecisions() {
		if !selfDeployPendingGateMatches(item, gateRefID) {
			continue
		}
		output.Status = generated.SelfDeployGovernanceStatusPending
		output.GateRequestId = optionalBoundedString(item.GetId(), maxSelfDeployIdentifierBytes)
		if output.GateRequestRef == nil {
			output.GateRequestRef = optionalBoundedString(item.GetId(), maxSelfDeployIdentifierBytes)
		}
		output.GateRequestVersion = optionalPositiveInt64(item.GetVersion())
		if item.GetVersion() > 0 && selfDeployGateRequestAllowsDecision(item.GetGateRequestStatus()) {
			actions := []generated.SelfDeployGateDecisionAction{
				generated.SelfDeployGateDecisionActionApprove,
				generated.SelfDeployGateDecisionActionReject,
				generated.SelfDeployGateDecisionActionRequestChanges,
			}
			output.AllowedActions = &actions
		}
		return
	}
	for _, item := range governance.GetCompletedDecisions() {
		if item.GetKind() != governancev1.GovernanceDecisionSummaryKind_GOVERNANCE_DECISION_SUMMARY_KIND_GATE_DECISION {
			continue
		}
		if gateRefID != "" && item.GetParentId() != "" && item.GetParentId() != gateRefID {
			continue
		}
		output.Status = generated.SelfDeployGovernanceStatusResolved
		output.GateDecisionRef = optionalBoundedString(item.GetId(), maxSelfDeployIdentifierBytes)
		if item.GetParentId() != "" {
			output.GateRequestId = optionalBoundedString(item.GetParentId(), maxSelfDeployIdentifierBytes)
		}
		return
	}
}

func selfDeployPendingGateMatches(item *governancev1.GovernanceDecisionSummary, gateRefID string) bool {
	if item.GetKind() != governancev1.GovernanceDecisionSummaryKind_GOVERNANCE_DECISION_SUMMARY_KIND_GATE_REQUEST {
		return false
	}
	if !selfDeployGateRequestAllowsDecision(item.GetGateRequestStatus()) {
		return false
	}
	return gateRefID == "" || item.GetId() == gateRefID || item.GetId() == strings.TrimSpace(gateRefID)
}

func selfDeployGateRequestAllowsDecision(status governancev1.GateRequestStatus) bool {
	switch status {
	case governancev1.GateRequestStatus_GATE_REQUEST_STATUS_REQUESTED,
		governancev1.GateRequestStatus_GATE_REQUEST_STATUS_DELIVERING,
		governancev1.GateRequestStatus_GATE_REQUEST_STATUS_AWAITING_DECISION:
		return true
	default:
		return false
	}
}

func governanceLocalRefID(value *string) string {
	if value == nil {
		return ""
	}
	trimmedValue := strings.TrimSpace(*value)
	if trimmedValue == "" {
		return ""
	}
	if slash := strings.LastIndex(trimmedValue, "/"); slash >= 0 && slash+1 < len(trimmedValue) {
		return trimmedValue[slash+1:]
	}
	return trimmedValue
}

func selfDeployRuntimeSummary(plan *agentsv1.SelfDeployPlan) generated.SelfDeployRuntimeSummary {
	if plan == nil {
		return generated.SelfDeployRuntimeSummary{Status: generated.SelfDeployRuntimeStatusUnavailable}
	}
	status := generated.SelfDeployRuntimeStatusUnavailable
	switch {
	case plan.GetRuntimeDeployStatus() == agentsv1.SelfDeployRuntimeDeployStatus_SELF_DEPLOY_RUNTIME_DEPLOY_STATUS_SUCCEEDED:
		status = generated.SelfDeployRuntimeStatusCompleted
	case plan.GetRuntimeBuildStatus() == agentsv1.SelfDeployRuntimeBuildStatus_SELF_DEPLOY_RUNTIME_BUILD_STATUS_SUCCEEDED && !selfDeployExpectsRuntimeJob(plan, runtimev1.JobType_JOB_TYPE_DEPLOY):
		status = generated.SelfDeployRuntimeStatusCompleted
	case plan.GetRuntimeDeployStatus() == agentsv1.SelfDeployRuntimeDeployStatus_SELF_DEPLOY_RUNTIME_DEPLOY_STATUS_FAILED ||
		plan.GetRuntimeDeployStatus() == agentsv1.SelfDeployRuntimeDeployStatus_SELF_DEPLOY_RUNTIME_DEPLOY_STATUS_BLOCKED ||
		plan.GetRuntimeBuildStatus() == agentsv1.SelfDeployRuntimeBuildStatus_SELF_DEPLOY_RUNTIME_BUILD_STATUS_FAILED ||
		plan.GetRuntimeBuildStatus() == agentsv1.SelfDeployRuntimeBuildStatus_SELF_DEPLOY_RUNTIME_BUILD_STATUS_BLOCKED:
		status = generated.SelfDeployRuntimeStatusFailed
	case plan.GetRuntimeDeployStatus() == agentsv1.SelfDeployRuntimeDeployStatus_SELF_DEPLOY_RUNTIME_DEPLOY_STATUS_REQUESTED ||
		plan.GetRuntimeBuildStatus() == agentsv1.SelfDeployRuntimeBuildStatus_SELF_DEPLOY_RUNTIME_BUILD_STATUS_REQUESTED:
		status = generated.SelfDeployRuntimeStatusRunning
	case plan.GetRuntimeBuildStatus() == agentsv1.SelfDeployRuntimeBuildStatus_SELF_DEPLOY_RUNTIME_BUILD_STATUS_PREPARING_CONTEXT:
		status = generated.SelfDeployRuntimeStatusPending
	case plan.GetRuntimeBuildStatus() == agentsv1.SelfDeployRuntimeBuildStatus_SELF_DEPLOY_RUNTIME_BUILD_STATUS_SUCCEEDED ||
		len(plan.GetRuntimeBuildJobs()) > 0 ||
		len(plan.GetRuntimeDeployJobs()) > 0 ||
		len(plan.GetRuntimeBuildContexts()) > 0:
		status = generated.SelfDeployRuntimeStatusStoredRef
	case plan.GetStatus() == agentsv1.SelfDeployPlanStatus_SELF_DEPLOY_PLAN_STATUS_APPROVED && len(plan.GetExpectedRuntimeJobTypes()) > 0:
		status = generated.SelfDeployRuntimeStatusPending
	}
	if status != generated.SelfDeployRuntimeStatusUnavailable {
		return generated.SelfDeployRuntimeSummary{
			Status:               status,
			RuntimeJobRef:        optionalBoundedString(selfDeployLatestRuntimeJobRef(plan), maxSelfDeployIdentifierBytes),
			RuntimeStatusSummary: optionalBoundedString(selfDeployRuntimeStageSummary(plan), maxSelfDeploySummaryBytes),
		}
	}
	return generated.SelfDeployRuntimeSummary{Status: generated.SelfDeployRuntimeStatusUnavailable}
}

func selfDeployExpectsRuntimeJob(plan *agentsv1.SelfDeployPlan, want runtimev1.JobType) bool {
	for _, item := range plan.GetExpectedRuntimeJobTypes() {
		if item == want {
			return true
		}
	}
	return false
}

func selfDeployLatestRuntimeJobRef(plan *agentsv1.SelfDeployPlan) string {
	deployJobs := plan.GetRuntimeDeployJobs()
	if len(deployJobs) > 0 {
		return deployJobs[len(deployJobs)-1].GetRuntimeJobRef()
	}
	buildJobs := plan.GetRuntimeBuildJobs()
	if len(buildJobs) > 0 {
		return buildJobs[len(buildJobs)-1].GetRuntimeJobRef()
	}
	contexts := plan.GetRuntimeBuildContexts()
	if len(contexts) > 0 {
		return contexts[len(contexts)-1].GetRuntimeBuildContextRef()
	}
	return ""
}

func selfDeployRuntimeStageSummary(plan *agentsv1.SelfDeployPlan) string {
	buildContextStatus := "not_requested"
	if contexts := plan.GetRuntimeBuildContexts(); len(contexts) > 0 {
		buildContextStatus = firstNonEmpty(contexts[len(contexts)-1].GetRuntimeBuildContextStatus(), "pending")
	}
	buildStatus := strings.TrimPrefix(plan.GetRuntimeBuildStatus().String(), "SELF_DEPLOY_RUNTIME_BUILD_STATUS_")
	deployStatus := strings.TrimPrefix(plan.GetRuntimeDeployStatus().String(), "SELF_DEPLOY_RUNTIME_DEPLOY_STATUS_")
	healthStatus := "unavailable"
	return boundedString(strings.ToLower(strings.Join([]string{
		"signal=stored",
		"plan=" + strings.TrimPrefix(plan.GetStatus().String(), "SELF_DEPLOY_PLAN_STATUS_"),
		"gate=" + selfDeployGateStage(plan.GetGovernanceContext()),
		"approval=" + selfDeployApprovalStage(plan.GetGovernanceContext()),
		"build_context=" + buildContextStatus,
		"build=" + buildStatus,
		"deploy=" + deployStatus,
		"health=" + healthStatus,
	}, " ")), maxSelfDeploySummaryBytes)
}

func selfDeployGateStage(context *agentsv1.GovernanceContextRef) string {
	if context == nil || strings.TrimSpace(context.GetGateRequestRef()) == "" {
		return "unavailable"
	}
	return "linked"
}

func selfDeployApprovalStage(context *agentsv1.GovernanceContextRef) string {
	if context == nil || strings.TrimSpace(context.GetGateDecisionRef()) == "" {
		return "pending"
	}
	return "approved_or_resolved"
}

func selfDeployPlanStatus(value agentsv1.SelfDeployPlanStatus) generated.SelfDeployPlanStatus {
	return protoEnum(value.String(), "SELF_DEPLOY_PLAN_STATUS_", generated.SelfDeployPlanStatusUnspecified, validSelfDeployPlanStatuses)
}

func selfDeployPathCategories(values []agentsv1.SelfDeployPathCategory) []generated.SelfDeployPathCategory {
	result := make([]generated.SelfDeployPathCategory, 0, len(values))
	for _, value := range values {
		result = append(result, protoEnum(value.String(), "SELF_DEPLOY_PATH_CATEGORY_", generated.SelfDeployPathCategoryUnspecified, validSelfDeployPathCategories))
	}
	return result
}

func selfDeployProjectPathCategories(values []*projectsv1.SelfDeployPathCategoryCount) []generated.SelfDeployPathCategory {
	result := make([]generated.SelfDeployPathCategory, 0, len(values))
	for _, value := range values {
		if value.GetCount() <= 0 {
			continue
		}
		result = append(result, protoEnum(value.GetCategory().String(), "SELF_DEPLOY_PATH_CATEGORY_", generated.SelfDeployPathCategoryUnspecified, validSelfDeployPathCategories))
	}
	return result
}

func selfDeployRuntimeJobTypes(values []runtimev1.JobType) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, enumName(value.String(), "JOB_TYPE_"))
	}
	return result
}

func selfDeployProjectRuntimeJobTypes(values []projectsv1.SelfDeployExpectedRuntimeJobType) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		jobType := enumName(value.String(), "SELF_DEPLOY_EXPECTED_RUNTIME_JOB_TYPE_")
		if jobType != "unspecified" {
			result = append(result, jobType)
		}
	}
	return result
}

func governanceSummary(input *governancev1.GovernanceSummary) (generated.GovernanceSummary, *SafeError) {
	return generated.GovernanceSummary{
		Scope:              governanceSummaryScope(input.GetScope()),
		PendingDecisions:   governanceDecisionSummaries(input.GetPendingDecisions()),
		CompletedDecisions: governanceDecisionSummaries(input.GetCompletedDecisions()),
		EvidenceSummaries:  governanceEvidenceSummaries(input.GetEvidenceSummaries()),
		Diagnostics:        boundedStrings(input.GetDiagnostics(), maxGovernanceTextBytes),
	}, nil
}

func governanceSummaryScope(input *governancev1.GovernanceSummaryScope) generated.GovernanceSummaryScope {
	output := generated.GovernanceSummaryScope{
		Target:                   governanceTargetRef(input.GetTarget()),
		ProjectContext:           governanceProjectContextRef(input.GetProjectContext()),
		ReleaseCandidateRef:      optionalBoundedString(input.GetReleaseCandidateRef(), maxGovernanceRefBytes),
		ReleaseDecisionPackageId: optionalBoundedString(input.GetReleaseDecisionPackageId(), maxGovernanceRefBytes),
	}
	if ref := governanceReleaseIntegrationRefPtr(input.GetIntegrationRef()); ref != nil {
		output.IntegrationRef = ref
	}
	return output
}

func governanceDecisionSummaries(input []*governancev1.GovernanceDecisionSummary) []generated.GovernanceDecisionSummary {
	result := make([]generated.GovernanceDecisionSummary, 0, len(input))
	for _, item := range input {
		if item != nil {
			result = append(result, governanceDecisionSummary(item))
		}
	}
	return result
}

func governanceDecisionSummary(input *governancev1.GovernanceDecisionSummary) generated.GovernanceDecisionSummary {
	output := generated.GovernanceDecisionSummary{
		Kind:                     governanceDecisionSummaryKind(input.GetKind()),
		Attention:                governanceDecisionAttention(input.GetAttention()),
		Id:                       boundedString(input.GetId(), maxGovernanceRefBytes),
		ParentId:                 optionalBoundedString(input.GetParentId(), maxGovernanceRefBytes),
		Target:                   governanceTargetRef(input.GetTarget()),
		ProjectContext:           governanceProjectContextRef(input.GetProjectContext()),
		ReleaseCandidateRef:      optionalBoundedString(input.GetReleaseCandidateRef(), maxGovernanceRefBytes),
		ReleaseDecisionPackageId: optionalBoundedString(input.GetReleaseDecisionPackageId(), maxGovernanceRefBytes),
		RiskClass:                optionalGovernanceRiskClass(input.GetRiskClass()),
		ReviewOutcome:            optionalGovernanceReviewOutcome(input.GetReviewOutcome()),
		GateRequestStatus:        optionalGovernanceGateRequestStatus(input.GetGateRequestStatus()),
		GateOutcome:              optionalGovernanceGateOutcome(input.GetGateOutcome()),
		ReleasePackageStatus:     optionalGovernanceReleasePackageStatus(input.GetReleasePackageStatus()),
		ReleaseDecisionStatus:    optionalGovernanceReleaseDecisionStatus(input.GetReleaseDecisionStatus()),
		ReleaseDecisionOutcome:   optionalGovernanceReleaseDecisionOutcome(input.GetReleaseDecisionOutcome()),
		BlockingSignalStatus:     optionalGovernanceBlockingSignalStatus(input.GetBlockingSignalStatus()),
		Severity:                 optionalGovernanceSignalSeverity(input.GetSeverity()),
		SafeSummary:              boundedString(input.GetSafeSummary(), maxGovernanceTextBytes),
		EvidenceRefs:             governanceEvidenceRefs(input.GetEvidenceRefs()),
		IntegrationRefs:          governanceReleaseIntegrationRefs(input.GetIntegrationRefs()),
		ProviderRefs:             governanceProviderContextRefs(input.GetProviderRefs()),
		RuntimeRefs:              governanceRuntimeContextRefs(input.GetRuntimeRefs()),
		AgentContext:             governanceAgentContextRef(input.GetAgentContext()),
		Version:                  input.GetVersion(),
		CreatedAt:                boundedString(input.GetCreatedAt(), maxGovernanceRefBytes),
		UpdatedAt:                boundedString(input.GetUpdatedAt(), maxGovernanceRefBytes),
		ObservedAt:               optionalTime(input.GetObservedAt()),
	}
	return output
}

func governanceEvidenceSummaries(input []*governancev1.GovernanceEvidenceSummary) []generated.GovernanceEvidenceSummary {
	result := make([]generated.GovernanceEvidenceSummary, 0, len(input))
	for _, item := range input {
		if item != nil {
			result = append(result, generated.GovernanceEvidenceSummary{
				SourceKind:      boundedString(item.GetSourceKind(), maxGovernanceKindBytes),
				SourceRef:       boundedString(item.GetSourceRef(), maxGovernanceRefBytes),
				Status:          optionalBoundedString(item.GetStatus(), maxGovernanceKindBytes),
				Outcome:         optionalBoundedString(item.GetOutcome(), maxGovernanceKindBytes),
				SafeSummary:     boundedString(item.GetSafeSummary(), maxGovernanceTextBytes),
				ErrorCode:       optionalBoundedString(item.GetErrorCode(), maxGovernanceKindBytes),
				Digest:          optionalBoundedString(item.GetDigest(), maxGovernanceDigestBytes),
				ObservedAt:      optionalTime(item.GetObservedAt()),
				Version:         optionalBoundedString(item.GetVersion(), maxGovernanceRefBytes),
				EvidenceRefs:    governanceEvidenceRefs(item.GetEvidenceRefs()),
				IntegrationRefs: governanceReleaseIntegrationRefs(item.GetIntegrationRefs()),
			})
		}
	}
	return result
}

func governanceTargetRef(input *governancev1.TargetRef) *generated.GovernanceTargetRef {
	if input == nil || input.GetRef() == "" {
		return nil
	}
	return &generated.GovernanceTargetRef{
		Type: governanceTargetType(input.GetType()),
		Ref:  boundedString(input.GetRef(), maxGovernanceRefBytes),
	}
}

func governanceProjectContextRef(input *governancev1.ProjectContextRef) *generated.GovernanceProjectContextRef {
	if input == nil {
		return nil
	}
	output := generated.GovernanceProjectContextRef{}
	output.ProjectRef = optionalBoundedString(input.GetProjectRef(), maxGovernanceRefBytes)
	output.RepositoryRef = optionalBoundedString(input.GetRepositoryRef(), maxGovernanceRefBytes)
	output.ServiceRef = optionalBoundedString(input.GetServiceRef(), maxGovernanceRefBytes)
	output.BranchRulesRef = optionalBoundedString(input.GetBranchRulesRef(), maxGovernanceRefBytes)
	output.ReleasePolicyRef = optionalBoundedString(input.GetReleasePolicyRef(), maxGovernanceRefBytes)
	output.ReleaseLineRef = optionalBoundedString(input.GetReleaseLineRef(), maxGovernanceRefBytes)
	if output.ProjectRef == nil && output.RepositoryRef == nil && output.ServiceRef == nil &&
		output.BranchRulesRef == nil && output.ReleasePolicyRef == nil && output.ReleaseLineRef == nil {
		return nil
	}
	return &output
}

func governanceReleaseIntegrationRefs(input []*governancev1.ReleaseIntegrationRef) []generated.GovernanceReleaseIntegrationRef {
	result := make([]generated.GovernanceReleaseIntegrationRef, 0, len(input))
	for _, item := range input {
		if casted := governanceReleaseIntegrationRefPtr(item); casted != nil {
			result = append(result, *casted)
		}
	}
	return result
}

func governanceReleaseIntegrationRefPtr(input *governancev1.ReleaseIntegrationRef) *generated.GovernanceReleaseIntegrationRef {
	if input == nil || input.GetDomain() == "" || input.GetKind() == "" || input.GetRef() == "" {
		return nil
	}
	return &generated.GovernanceReleaseIntegrationRef{
		Domain:     boundedString(input.GetDomain(), maxGovernanceKindBytes),
		Kind:       boundedString(input.GetKind(), maxGovernanceKindBytes),
		Ref:        boundedString(input.GetRef(), maxGovernanceRefBytes),
		Status:     optionalBoundedString(input.GetStatus(), maxGovernanceKindBytes),
		Summary:    optionalBoundedString(input.GetSummary(), maxGovernanceTextBytes),
		Digest:     optionalBoundedString(input.GetDigest(), maxGovernanceDigestBytes),
		ObservedAt: optionalTime(input.GetObservedAt()),
		Version:    optionalBoundedString(input.GetVersion(), maxGovernanceRefBytes),
		ErrorCode:  optionalBoundedString(input.GetErrorCode(), maxGovernanceKindBytes),
	}
}

func governanceEvidenceRefs(input []*governancev1.EvidenceRef) []generated.GovernanceEvidenceRef {
	result := make([]generated.GovernanceEvidenceRef, 0, len(input))
	for _, item := range input {
		if item == nil || item.GetRef() == "" {
			continue
		}
		result = append(result, generated.GovernanceEvidenceRef{
			Kind:           governanceEvidenceKind(item.GetKind()),
			Ref:            boundedString(item.GetRef(), maxGovernanceRefBytes),
			Summary:        optionalBoundedString(item.GetSummary(), maxGovernanceTextBytes),
			Digest:         optionalBoundedString(item.GetDigest(), maxGovernanceDigestBytes),
			RetentionClass: optionalBoundedString(item.GetRetentionClass(), maxGovernanceKindBytes),
		})
	}
	return result
}

func governanceProviderContextRefs(input []*governancev1.ProviderContextRef) []generated.GovernanceProviderContextRef {
	result := make([]generated.GovernanceProviderContextRef, 0, len(input))
	for _, item := range input {
		if item == nil {
			continue
		}
		casted := generated.GovernanceProviderContextRef{
			WorkItemRef:            optionalBoundedString(item.GetWorkItemRef(), maxGovernanceRefBytes),
			PullRequestRef:         optionalBoundedString(item.GetPullRequestRef(), maxGovernanceRefBytes),
			CommentRef:             optionalBoundedString(item.GetCommentRef(), maxGovernanceRefBytes),
			ReviewSignalRef:        optionalBoundedString(item.GetReviewSignalRef(), maxGovernanceRefBytes),
			ProviderOperationRef:   optionalBoundedString(item.GetProviderOperationRef(), maxGovernanceRefBytes),
			ChangedFilesSummaryRef: optionalBoundedString(item.GetChangedFilesSummaryRef(), maxGovernanceRefBytes),
		}
		if casted.WorkItemRef != nil || casted.PullRequestRef != nil || casted.CommentRef != nil ||
			casted.ReviewSignalRef != nil || casted.ProviderOperationRef != nil || casted.ChangedFilesSummaryRef != nil {
			result = append(result, casted)
		}
	}
	return result
}

func governanceRuntimeContextRefs(input []*governancev1.RuntimeContextRef) []generated.GovernanceRuntimeContextRef {
	result := make([]generated.GovernanceRuntimeContextRef, 0, len(input))
	for _, item := range input {
		if item == nil {
			continue
		}
		casted := generated.GovernanceRuntimeContextRef{
			SlotRef:        optionalBoundedString(item.GetSlotRef(), maxGovernanceRefBytes),
			JobRef:         optionalBoundedString(item.GetJobRef(), maxGovernanceRefBytes),
			EnvironmentRef: optionalBoundedString(item.GetEnvironmentRef(), maxGovernanceRefBytes),
			ArtifactRef:    optionalBoundedString(item.GetArtifactRef(), maxGovernanceRefBytes),
			SummaryRef:     optionalBoundedString(item.GetSummaryRef(), maxGovernanceRefBytes),
		}
		if casted.SlotRef != nil || casted.JobRef != nil || casted.EnvironmentRef != nil ||
			casted.ArtifactRef != nil || casted.SummaryRef != nil {
			result = append(result, casted)
		}
	}
	return result
}

func governanceAgentContextRef(input *governancev1.AgentContextRef) *generated.GovernanceAgentContextRef {
	if input == nil {
		return nil
	}
	output := generated.GovernanceAgentContextRef{}
	output.SessionRef = optionalBoundedString(input.GetSessionRef(), maxGovernanceRefBytes)
	output.RunRef = optionalBoundedString(input.GetRunRef(), maxGovernanceRefBytes)
	output.StageRef = optionalBoundedString(input.GetStageRef(), maxGovernanceRefBytes)
	output.AcceptanceRef = optionalBoundedString(input.GetAcceptanceRef(), maxGovernanceRefBytes)
	output.RoleRef = optionalBoundedString(input.GetRoleRef(), maxGovernanceRefBytes)
	if output.SessionRef == nil && output.RunRef == nil && output.StageRef == nil &&
		output.AcceptanceRef == nil && output.RoleRef == nil {
		return nil
	}
	return &output
}

func agentRunStatus(value agentsv1.AgentRunStatus) generated.AgentRunStatus {
	return protoEnum(value.String(), "AGENT_RUN_STATUS_", generated.AgentRunStatusUnspecified, validAgentRunStatuses)
}

func agentSessionStatus(value agentsv1.AgentSessionStatus) generated.AgentSessionStatus {
	return protoEnum(value.String(), "AGENT_SESSION_STATUS_", generated.AgentSessionStatusUnspecified, validAgentSessionStatuses)
}

func agentScopeRef(input *agentsv1.ScopeRef) (generated.ScopeRef, *SafeError) {
	if input == nil || strings.TrimSpace(input.GetRef()) == "" {
		return generated.ScopeRef{}, NewSafeError(http.StatusServiceUnavailable, CodeDownstreamUnavailable, "agent-manager returned invalid scope ref", true)
	}
	var scopeType generated.ScopeType
	switch input.GetType() {
	case agentsv1.AgentScopeType_AGENT_SCOPE_TYPE_PLATFORM:
		scopeType = generated.ScopeTypePlatform
	case agentsv1.AgentScopeType_AGENT_SCOPE_TYPE_ORGANIZATION:
		scopeType = generated.ScopeTypeOrganization
	case agentsv1.AgentScopeType_AGENT_SCOPE_TYPE_PROJECT:
		scopeType = generated.ScopeTypeProject
	case agentsv1.AgentScopeType_AGENT_SCOPE_TYPE_REPOSITORY:
		scopeType = generated.ScopeTypeRepository
	default:
		return generated.ScopeRef{}, NewSafeError(http.StatusServiceUnavailable, CodeDownstreamUnavailable, "agent-manager returned invalid scope type", true)
	}
	return generated.ScopeRef{Type: scopeType, Ref: input.GetRef()}, nil
}

func runtimeObservationState(value agentsv1.AgentRunRuntimeObservationState) generated.RuntimeObservationState {
	return protoEnum(value.String(), "AGENT_RUN_RUNTIME_OBSERVATION_STATE_", generated.RuntimeObservationStateUnspecified, validRuntimeObservationStates)
}

func agentRuntimeJobStatus(value agentsv1.AgentRuntimeJobStatus) generated.AgentRuntimeJobStatus {
	return protoEnum(value.String(), "AGENT_RUNTIME_JOB_STATUS_", generated.AgentRuntimeJobStatusUnspecified, validAgentRuntimeJobStatuses)
}

func agentActivityKind(value agentsv1.AgentActivityKind) generated.AgentActivityKind {
	return protoEnum(value.String(), "AGENT_ACTIVITY_KIND_", generated.AgentActivityKindUnspecified, validAgentActivityKinds)
}

func agentActivityStatus(value agentsv1.AgentActivityStatus) generated.AgentActivityStatus {
	return protoEnum(value.String(), "AGENT_ACTIVITY_STATUS_", generated.AgentActivityStatusUnspecified, validAgentActivityStatuses)
}

func governanceTargetType(value governancev1.GovernanceTargetType) generated.GovernanceTargetType {
	return protoEnum(value.String(), "GOVERNANCE_TARGET_TYPE_", generated.GovernanceTargetTypeUnspecified, validGovernanceTargetTypes)
}

func governanceDecisionSummaryKind(value governancev1.GovernanceDecisionSummaryKind) generated.GovernanceDecisionSummaryKind {
	return protoEnum(value.String(), "GOVERNANCE_DECISION_SUMMARY_KIND_", generated.GovernanceDecisionSummaryKindUnspecified, validGovernanceDecisionSummaryKinds)
}

func governanceDecisionAttention(value governancev1.GovernanceDecisionAttention) generated.GovernanceDecisionAttention {
	return protoEnum(value.String(), "GOVERNANCE_DECISION_ATTENTION_", generated.GovernanceDecisionAttentionUnspecified, validGovernanceDecisionAttentions)
}

func optionalGovernanceRiskClass(value governancev1.RiskClass) *generated.GovernanceRiskClass {
	return optionalEnum(protoEnum(value.String(), "RISK_CLASS_", generated.GovernanceRiskClassUnspecified, validGovernanceRiskClasses), generated.GovernanceRiskClassUnspecified)
}

func optionalGovernanceReviewOutcome(value governancev1.ReviewSignalOutcome) *generated.GovernanceReviewOutcome {
	return optionalEnum(protoEnum(value.String(), "REVIEW_SIGNAL_OUTCOME_", generated.GovernanceReviewOutcomeUnspecified, validGovernanceReviewOutcomes), generated.GovernanceReviewOutcomeUnspecified)
}

func optionalGovernanceGateRequestStatus(value governancev1.GateRequestStatus) *generated.GovernanceGateRequestStatus {
	return optionalEnum(protoEnum(value.String(), "GATE_REQUEST_STATUS_", generated.GovernanceGateRequestStatusUnspecified, validGovernanceGateRequestStatuses), generated.GovernanceGateRequestStatusUnspecified)
}

func optionalGovernanceGateOutcome(value governancev1.GateOutcome) *generated.GovernanceGateOutcome {
	return optionalEnum(protoEnum(value.String(), "GATE_OUTCOME_", generated.GovernanceGateOutcomeUnspecified, validGovernanceGateOutcomes), generated.GovernanceGateOutcomeUnspecified)
}

func optionalGovernanceReleasePackageStatus(value governancev1.ReleaseDecisionPackageStatus) *generated.GovernanceReleasePackageStatus {
	return optionalEnum(protoEnum(value.String(), "RELEASE_DECISION_PACKAGE_STATUS_", generated.GovernanceReleasePackageStatusUnspecified, validGovernanceReleasePackageStatuses), generated.GovernanceReleasePackageStatusUnspecified)
}

func optionalGovernanceReleaseDecisionStatus(value governancev1.ReleaseDecisionStatus) *generated.GovernanceReleaseDecisionStatus {
	return optionalEnum(protoEnum(value.String(), "RELEASE_DECISION_STATUS_", generated.GovernanceReleaseDecisionStatusUnspecified, validGovernanceReleaseDecisionStatuses), generated.GovernanceReleaseDecisionStatusUnspecified)
}

func optionalGovernanceReleaseDecisionOutcome(value governancev1.ReleaseDecisionOutcome) *generated.GovernanceReleaseDecisionOutcome {
	return optionalEnum(protoEnum(value.String(), "RELEASE_DECISION_OUTCOME_", generated.GovernanceReleaseDecisionOutcomeUnspecified, validGovernanceReleaseDecisionOutcomes), generated.GovernanceReleaseDecisionOutcomeUnspecified)
}

func optionalGovernanceBlockingSignalStatus(value governancev1.BlockingSignalStatus) *generated.GovernanceBlockingSignalStatus {
	return optionalEnum(protoEnum(value.String(), "BLOCKING_SIGNAL_STATUS_", generated.GovernanceBlockingSignalStatusUnspecified, validGovernanceBlockingSignalStatuses), generated.GovernanceBlockingSignalStatusUnspecified)
}

func optionalGovernanceSignalSeverity(value governancev1.SignalSeverity) *generated.GovernanceSignalSeverity {
	return optionalEnum(protoEnum(value.String(), "SIGNAL_SEVERITY_", generated.GovernanceSignalSeverityUnspecified, validGovernanceSignalSeverities), generated.GovernanceSignalSeverityUnspecified)
}

func governanceEvidenceKind(value governancev1.EvidenceKind) generated.GovernanceEvidenceKind {
	return protoEnum(value.String(), "EVIDENCE_KIND_", generated.GovernanceEvidenceKindUnspecified, validGovernanceEvidenceKinds)
}

func optionalEnum[Target comparable](value Target, fallback Target) *Target {
	if value == fallback {
		return nil
	}
	return &value
}

func protoEnum[Target ~string](value string, prefix string, fallback Target, valid map[Target]struct{}) Target {
	item := Target(enumName(value, prefix))
	if _, ok := valid[item]; ok {
		return item
	}
	return fallback
}

func enumSet[Target comparable](items ...Target) map[Target]struct{} {
	result := make(map[Target]struct{}, len(items))
	for _, item := range items {
		result[item] = struct{}{}
	}
	return result
}

func deliverySummary(summary *interactionsv1.OwnerInboxDeliverySummary) (generated.OwnerInboxDeliverySummary, *SafeError) {
	if summary == nil {
		return generated.OwnerInboxDeliverySummary{
			LatestStatus:     generated.DeliveryAttemptStatusUnspecified,
			LatestErrorClass: generated.DeliveryErrorClassUnspecified,
		}, nil
	}
	return generated.OwnerInboxDeliverySummary{
		AttemptCount:            summary.GetAttemptCount(),
		LatestDeliveryAttemptId: optionalString(summary.GetLatestDeliveryAttemptId()),
		LatestDeliveryId:        optionalString(summary.GetLatestDeliveryId()),
		LatestStatus:            generated.DeliveryAttemptStatus(enumName(summary.GetLatestStatus().String(), "DELIVERY_ATTEMPT_STATUS_")),
		LatestErrorCode:         optionalString(summary.GetLatestErrorCode()),
		LatestErrorClass:        generated.DeliveryErrorClass(enumName(summary.GetLatestErrorClass().String(), "DELIVERY_ERROR_CLASS_")),
		NextRetryAt:             optionalTime(summary.GetNextRetryAt()),
		LatestUpdatedAt:         optionalTime(summary.GetLatestUpdatedAt()),
		RouteId:                 optionalString(summary.GetRouteId()),
		ChannelMessageRef:       optionalString(summary.GetChannelMessageRef()),
	}, nil
}

func callbackSummary(summary *interactionsv1.OwnerInboxCallbackSummary) (generated.OwnerInboxCallbackSummary, *SafeError) {
	receivedAt, safeErr := requiredTime(summary.GetReceivedAt())
	if safeErr != nil {
		return generated.OwnerInboxCallbackSummary{}, safeErr
	}
	return generated.OwnerInboxCallbackSummary{
		CallbackRef:      summary.GetCallbackRef(),
		CallbackId:       summary.GetCallbackId(),
		DeliveryId:       optionalString(summary.GetDeliveryId()),
		SignatureStatus:  generated.CallbackSignatureStatus(enumName(summary.GetSignatureStatus().String(), "CALLBACK_SIGNATURE_STATUS_")),
		ProcessingStatus: generated.CallbackProcessingStatus(enumName(summary.GetProcessingStatus().String(), "CALLBACK_PROCESSING_STATUS_")),
		ActorRef:         optionalString(summary.GetActorRef()),
		Action:           optionalString(summary.GetAction()),
		ErrorCode:        optionalString(summary.GetErrorCode()),
		ReceivedAt:       receivedAt,
		GatewayRef:       optionalString(summary.GetGatewayRef()),
		CorrelationId:    optionalString(summary.GetCorrelationId()),
	}, nil
}

func responseSummaryFromInbox(summary *interactionsv1.OwnerInboxResponseSummary) (generated.OwnerInboxResponseSummary, *SafeError) {
	createdAt, safeErr := requiredTime(summary.GetCreatedAt())
	if safeErr != nil {
		return generated.OwnerInboxResponseSummary{}, safeErr
	}
	return generated.OwnerInboxResponseSummary{
		ResponseId:             summary.GetResponseId(),
		ResponseAction:         generated.ResponseAction(enumName(summary.GetResponseAction().String(), "INTERACTION_RESPONSE_ACTION_")),
		RespondedByActorRef:    summary.GetRespondedByActorRef(),
		SourceKind:             generated.ResponseSourceKind(enumName(summary.GetSourceKind().String(), "INTERACTION_RESPONSE_SOURCE_KIND_")),
		SourceRef:              optionalString(summary.GetSourceRef()),
		OwnerDecisionRef:       optionalString(summary.GetOwnerDecisionRef()),
		CreatedAt:              createdAt,
		ResponseSummary:        optionalString(summary.GetResponseSummary()),
		ResponseSummaryDigest:  optionalString(summary.GetResponseSummaryDigest()),
		ResponseObject:         objectRef(summary.GetResponseObject()),
		InteractionResponseRef: optionalString(summary.GetInteractionResponseRef()),
	}, nil
}

func responseSummary(response *interactionsv1.InteractionResponse) (generated.OwnerInboxResponseSummary, *SafeError) {
	return responseSummaryFromInbox(protoResponseSummary(response))
}

func protoResponseSummary(response *interactionsv1.InteractionResponse) *interactionsv1.OwnerInboxResponseSummary {
	return &interactionsv1.OwnerInboxResponseSummary{
		ResponseId:             response.GetId(),
		ResponseAction:         response.GetResponseAction(),
		RespondedByActorRef:    response.GetRespondedByActorRef(),
		SourceKind:             response.GetSourceKind(),
		SourceRef:              response.SourceRef,
		OwnerDecisionRef:       response.OwnerDecisionRef,
		CreatedAt:              response.GetCreatedAt(),
		ResponseSummary:        response.ResponseSummary,
		ResponseObject:         response.GetResponseObject(),
		InteractionResponseRef: optionalString(response.GetId()),
	}
}

func scopeRef(input *interactionsv1.ScopeRef) generated.ScopeRef {
	return generated.ScopeRef{Type: generated.ScopeType(enumName(input.GetType().String(), "INTERACTION_SCOPE_TYPE_")), Ref: input.GetRef()}
}

func sourceOwnerRef(input *interactionsv1.SourceOwnerRef) generated.SourceOwnerRef {
	return generated.SourceOwnerRef{Kind: generated.SourceOwnerKind(enumName(input.GetKind().String(), "SOURCE_OWNER_KIND_")), Ref: optionalString(input.GetRef())}
}

func decisionOwnerRef(input *interactionsv1.DecisionOwnerRef) *generated.DecisionOwnerRef {
	if input == nil || input.GetOwnerKind() == interactionsv1.DecisionOwnerKind_DECISION_OWNER_KIND_UNSPECIFIED {
		return nil
	}
	return &generated.DecisionOwnerRef{
		OwnerKind:        generated.DecisionOwnerKind(enumName(input.GetOwnerKind().String(), "DECISION_OWNER_KIND_")),
		OwnerRequestRef:  input.GetOwnerRequestRef(),
		OwnerDecisionRef: optionalString(input.GetOwnerDecisionRef()),
	}
}

func actorRefs(input []*interactionsv1.ActorRef) []generated.ActorRef {
	result := make([]generated.ActorRef, 0, len(input))
	for index := range input {
		if input[index] != nil {
			result = append(result, generated.ActorRef{RefKind: input[index].GetRefKind(), Ref: input[index].GetRef()})
		}
	}
	return result
}

func externalRefs(input []*interactionsv1.ExternalRef) []generated.ExternalRef {
	return collectOwnerRefs(input, func(item *interactionsv1.ExternalRef) (generated.ExternalRef, bool) {
		if item == nil {
			return generated.ExternalRef{}, false
		}
		return generated.ExternalRef{RefKind: item.GetRefKind(), Ref: item.GetRef()}, true
	})
}

func collectOwnerRefs[Input any, Output any](input []Input, cast func(Input) (Output, bool)) []Output {
	result := make([]Output, 0, len(input))
	for index := range input {
		casted, ok := cast(input[index])
		if ok {
			result = append(result, casted)
		}
	}
	return result
}

func interactionActions(input []*interactionsv1.InteractionAction) []generated.InteractionAction {
	result := make([]generated.InteractionAction, 0, len(input))
	for _, item := range input {
		if item == nil {
			continue
		}
		result = append(result, generated.InteractionAction{
			ActionKey:        item.GetActionKey(),
			LabelTemplateRef: optionalString(item.GetLabelTemplateRef()),
			IsTerminal:       item.GetIsTerminal(),
		})
	}
	return result
}

func objectRefProto(input *generated.ObjectRef) *interactionsv1.ObjectRef {
	if input == nil {
		return nil
	}
	return &interactionsv1.ObjectRef{
		ObjectUri:       strings.TrimSpace(input.ObjectUri),
		ObjectDigest:    strings.TrimSpace(input.ObjectDigest),
		ObjectSizeBytes: input.ObjectSizeBytes,
	}
}

func objectRef(input *interactionsv1.ObjectRef) *generated.ObjectRef {
	if input == nil {
		return nil
	}
	return &generated.ObjectRef{
		ObjectUri:       input.GetObjectUri(),
		ObjectDigest:    input.GetObjectDigest(),
		ObjectSizeBytes: input.ObjectSizeBytes,
	}
}

func pageInfo(input *interactionsv1.PageResponse) generated.PageInfo {
	if input == nil {
		return generated.PageInfo{}
	}
	return generated.PageInfo{NextPageToken: optionalString(input.GetNextPageToken())}
}

func agentPageInfo(input *agentsv1.PageResponse) generated.PageInfo {
	if input == nil {
		return generated.PageInfo{}
	}
	return generated.PageInfo{NextPageToken: optionalString(input.GetNextPageToken())}
}

func requiredTime(value string) (time.Time, *SafeError) {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, NewSafeError(http.StatusServiceUnavailable, CodeDownstreamUnavailable, "downstream returned invalid timestamp", true)
	}
	return parsed.UTC(), nil
}

func optionalTime(value string) *time.Time {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return nil
	}
	result := parsed.UTC()
	return &result
}

func optionalPositiveInt64(value int64) *int64 {
	if value <= 0 {
		return nil
	}
	return &value
}

func enumName(value string, prefix string) string {
	trimmed := strings.TrimPrefix(value, prefix)
	if trimmed == "" {
		return "unspecified"
	}
	return strings.ToLower(trimmed)
}

func optionalString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func optionalBoundedString(value string, maxBytes int) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || len(trimmed) > maxBytes {
		return nil
	}
	return &trimmed
}

func optionalUUIDFromDownstream(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	if _, err := uuid.Parse(trimmed); err != nil {
		return nil
	}
	return &trimmed
}

func firstBoundedString(maxBytes int, values ...string) *string {
	for _, value := range values {
		if bounded := optionalBoundedString(value, maxBytes); bounded != nil {
			return bounded
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func boundedString(value string, maxBytes int) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) > maxBytes {
		return ""
	}
	return trimmed
}

func boundedStrings(values []string, maxBytes int) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if bounded := boundedString(value, maxBytes); bounded != "" {
			result = append(result, bounded)
		}
	}
	return result
}

func requiredBoundedString(value string, maxBytes int, invalidMessage string) (string, *SafeError) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || len(trimmed) > maxBytes {
		return "", NewSafeError(http.StatusServiceUnavailable, CodeDownstreamUnavailable, invalidMessage, true)
	}
	return trimmed, nil
}

func trimOptional(value *string) *string {
	if value == nil {
		return nil
	}
	return optionalString(*value)
}

func trimmed(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func parseBool(value string) bool {
	parsed, _ := strconv.ParseBool(strings.TrimSpace(value))
	return parsed
}

func splitQueryValues(values []string) []string {
	var result []string
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			item = strings.TrimSpace(item)
			if item != "" {
				result = append(result, item)
			}
		}
	}
	return result
}

func queryValues(values map[string][]string, key string) []string {
	return values[key]
}
