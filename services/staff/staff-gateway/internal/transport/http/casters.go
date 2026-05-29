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
	"github.com/codex-k8s/kodex/services/staff/staff-gateway/internal/transport/http/generated"
)

const defaultPageSize = 25
const (
	maxActivitySafeTextBytes   = 2000
	maxActivityDigestBytes     = 256
	maxActivityRefBytes        = 256
	maxActivitySafeJSONBytes   = 8192
	maxActivityIdentifierBytes = 128
	maxGovernanceKindBytes     = 128
	maxGovernanceRefBytes      = 256
	maxGovernanceTextBytes     = 2000
	maxGovernanceDigestBytes   = 256
)

type OwnerInboxRespondBody = generated.OwnerInboxRespondRequest

var validAgentRunStatuses = enumSet(generated.AgentRunStatusRequested, generated.AgentRunStatusStarting, generated.AgentRunStatusRunning, generated.AgentRunStatusWaiting, generated.AgentRunStatusCompleted, generated.AgentRunStatusFailed, generated.AgentRunStatusCancelled)
var validRuntimeObservationStates = enumSet(generated.RuntimeObservationStateNotCreated, generated.RuntimeObservationStateStoredRef, generated.RuntimeObservationStateLive, generated.RuntimeObservationStateUnavailable, generated.RuntimeObservationStateConflict)
var validAgentRuntimeJobStatuses = enumSet(generated.AgentRuntimeJobStatusPending, generated.AgentRuntimeJobStatusClaimed, generated.AgentRuntimeJobStatusRunning, generated.AgentRuntimeJobStatusSucceeded, generated.AgentRuntimeJobStatusFailed, generated.AgentRuntimeJobStatusCancelled, generated.AgentRuntimeJobStatusTimedOut)
var validAgentActivityKinds = enumSet(generated.AgentActivityKindLifecycle, generated.AgentActivityKindToolUse, generated.AgentActivityKindToolResult, generated.AgentActivityKindPermission, generated.AgentActivityKindProviderSignal, generated.AgentActivityKindRuntimeSignal, generated.AgentActivityKindCheckpoint, generated.AgentActivityKindOther)
var validAgentActivityStatuses = enumSet(generated.AgentActivityStatusPlanned, generated.AgentActivityStatusStarted, generated.AgentActivityStatusSucceeded, generated.AgentActivityStatusFailed, generated.AgentActivityStatusDenied, generated.AgentActivityStatusWaiting, generated.AgentActivityStatusCancelled, generated.AgentActivityStatusSkipped)
var validGovernanceTargetTypes = enumSet(generated.Transition, generated.PullRequest, generated.ReleaseCandidate, generated.RuntimeJob, generated.PolicyChange, generated.Document, generated.Merge, generated.Postdeploy, generated.Rollback)
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

func runIDFromPath(req *http.Request) (string, *SafeError) {
	runID := strings.TrimSpace(req.PathValue("run_id"))
	if _, err := uuid.Parse(runID); err != nil {
		return "", NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "run id is invalid", false)
	}
	return runID, nil
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
	if response == nil {
		return generated.OwnerInboxListResponse{}, NewSafeError(http.StatusServiceUnavailable, CodeDownstreamUnavailable, "interaction-hub returned empty response", true)
	}
	items := make([]generated.OwnerInboxItem, 0, len(response.GetItems()))
	for _, item := range response.GetItems() {
		casted, safeErr := ownerInboxItem(item)
		if safeErr != nil {
			return generated.OwnerInboxListResponse{}, safeErr
		}
		items = append(items, casted)
	}
	return generated.OwnerInboxListResponse{
		RequestId:     requestID,
		CorrelationId: optionalString(requestID),
		Items:         items,
		Page:          pageInfo(response.GetPage()),
	}, nil
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
	actorType, actorID, safeErr := actorPartsFromHeaders(req)
	if safeErr != nil {
		return nil, safeErr
	}
	return &agentsv1.QueryMeta{
		Actor:     &agentsv1.Actor{Type: actorType, Id: actorID},
		RequestId: requestIDFromContext(req.Context()),
		RequestContext: &agentsv1.RequestContext{
			Source:    "staff-gateway",
			TraceId:   optionalString(traceID(req)),
			SessionId: optionalString(req.Header.Get(headerSessionID)),
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
	scopeType, safeErr := scopeTypeProto(req.URL.Query().Get("scope_type"))
	if safeErr != nil {
		return nil, safeErr
	}
	scopeRef := strings.TrimSpace(req.URL.Query().Get("scope_ref"))
	if scopeRef == "" {
		return nil, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "scope ref is required", false)
	}
	return &interactionsv1.ScopeRef{Type: scopeType, Ref: scopeRef}, nil
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
	return protoEnum(value.String(), "GOVERNANCE_TARGET_TYPE_", generated.Unspecified, validGovernanceTargetTypes)
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
