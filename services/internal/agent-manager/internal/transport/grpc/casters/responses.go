package casters

import (
	"time"

	"github.com/google/uuid"

	agentsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/agents/v1"
	runtimev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/runtime/v1"
	agentservice "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

type FlowOutput struct {
	Flow          entity.Flow
	ActiveVersion *entity.FlowVersion
}

type PageOutput[T any] struct {
	Items []T
	Page  value.PageResult
}

type FlowListOutput = PageOutput[entity.Flow]

type RoleProfileListOutput = PageOutput[entity.RoleProfile]

type PromptTemplateOutput struct {
	PromptTemplate entity.PromptTemplate
	ActiveVersion  *entity.PromptTemplateVersion
}

type PromptTemplateListOutput = PageOutput[entity.PromptTemplate]

type PromptTemplateVersionListOutput = PageOutput[entity.PromptTemplateVersion]

type AgentRunListOutput = PageOutput[entity.AgentRun]
type AgentSessionSummaryListOutput = PageOutput[entity.AgentSessionListItem]
type AgentRunSummaryListOutput = PageOutput[entity.AgentRunListItem]

type AcceptanceResultListOutput = PageOutput[entity.AcceptanceResult]

type AgentActivityListOutput = PageOutput[entity.AgentActivity]

type HumanGateListOutput = PageOutput[entity.HumanGateRequest]

type SelfDeployPlanListOutput = PageOutput[entity.SelfDeployPlan]

type AgentSessionOutput struct {
	Session        entity.AgentSession
	LatestSnapshot *entity.AgentSessionStateSnapshot
}

func NewFlowOutput(flow entity.Flow, active *entity.FlowVersion) FlowOutput {
	return FlowOutput{Flow: flow, ActiveVersion: active}
}

func NewPromptTemplateOutput(template entity.PromptTemplate, active *entity.PromptTemplateVersion) PromptTemplateOutput {
	return PromptTemplateOutput{PromptTemplate: template, ActiveVersion: active}
}

func FlowResponse(output FlowOutput) *agentsv1.FlowResponse {
	response := &agentsv1.FlowResponse{Flow: FlowToProto(output.Flow)}
	if output.ActiveVersion != nil {
		response.ActiveVersion = FlowVersionToProto(*output.ActiveVersion)
	}
	return response
}

func FlowEntityResponse(flow entity.Flow) *agentsv1.FlowResponse {
	return &agentsv1.FlowResponse{Flow: FlowToProto(flow)}
}

func FlowVersionResponse(version entity.FlowVersion) *agentsv1.FlowVersionResponse {
	return &agentsv1.FlowVersionResponse{FlowVersion: FlowVersionToProto(version)}
}

func ListFlowsResponse(output FlowListOutput) *agentsv1.ListFlowsResponse {
	return &agentsv1.ListFlowsResponse{Flows: protoList(output.Items, FlowToProto), Page: pageResponseToProto(output.Page)}
}

func RoleProfileResponse(role entity.RoleProfile) *agentsv1.RoleProfileResponse {
	return &agentsv1.RoleProfileResponse{RoleProfile: RoleProfileToProto(role)}
}

func ListRoleProfilesResponse(output RoleProfileListOutput) *agentsv1.ListRoleProfilesResponse {
	return &agentsv1.ListRoleProfilesResponse{RoleProfiles: protoList(output.Items, RoleProfileToProto), Page: pageResponseToProto(output.Page)}
}

func PromptTemplateResponse(output PromptTemplateOutput) *agentsv1.PromptTemplateResponse {
	response := &agentsv1.PromptTemplateResponse{PromptTemplate: PromptTemplateToProto(output.PromptTemplate)}
	if output.ActiveVersion != nil {
		response.ActiveVersion = PromptTemplateVersionToProto(*output.ActiveVersion)
	}
	return response
}

func ListPromptTemplatesResponse(output PromptTemplateListOutput) *agentsv1.ListPromptTemplatesResponse {
	return &agentsv1.ListPromptTemplatesResponse{PromptTemplates: protoList(output.Items, PromptTemplateToProto), Page: pageResponseToProto(output.Page)}
}

func PromptTemplateVersionResponse(version entity.PromptTemplateVersion) *agentsv1.PromptTemplateVersionResponse {
	return &agentsv1.PromptTemplateVersionResponse{PromptTemplateVersion: PromptTemplateVersionToProto(version)}
}

func ListPromptTemplateVersionsResponse(output PromptTemplateVersionListOutput) *agentsv1.ListPromptTemplateVersionsResponse {
	return &agentsv1.ListPromptTemplateVersionsResponse{PromptTemplateVersions: protoList(output.Items, PromptTemplateVersionToProto), Page: pageResponseToProto(output.Page)}
}

func NewAgentSessionOutput(session entity.AgentSession, snapshot *entity.AgentSessionStateSnapshot) AgentSessionOutput {
	return AgentSessionOutput{Session: session, LatestSnapshot: snapshot}
}

func AgentSessionEntityResponse(session entity.AgentSession) *agentsv1.AgentSessionResponse {
	return AgentSessionResponse(NewAgentSessionOutput(session, nil))
}

func AgentSessionResponse(output AgentSessionOutput) *agentsv1.AgentSessionResponse {
	response := &agentsv1.AgentSessionResponse{Session: AgentSessionToProto(output.Session)}
	if output.LatestSnapshot != nil {
		response.LatestStateSnapshot = AgentSessionStateSnapshotToProto(*output.LatestSnapshot)
	}
	return response
}

func AgentRunResponse(run entity.AgentRun) *agentsv1.AgentRunResponse {
	return &agentsv1.AgentRunResponse{Run: AgentRunToProto(run)}
}

func AgentRunRuntimeStatusResponse(output agentservice.AgentRunRuntimeStatusResult) *agentsv1.AgentRunRuntimeStatusResponse {
	return &agentsv1.AgentRunRuntimeStatusResponse{
		Run:           AgentRunToProto(output.Run),
		RuntimeStatus: AgentRunRuntimeStatusToProto(output.RuntimeStatus),
	}
}

func AgentRunRuntimeStatusToProto(status agentservice.AgentRunRuntimeStatus) *agentsv1.AgentRunRuntimeStatus {
	return &agentsv1.AgentRunRuntimeStatus{
		RunId:                status.RunID.String(),
		RunStatus:            AgentRunStatusToProto(status.RunStatus),
		RuntimeContext:       RuntimeContextToProto(status.RuntimeContext),
		ObservationState:     RuntimeObservationStateToProto(status.ObservationState),
		RuntimeJobRef:        optionalStringPtr(status.RuntimeJobRef),
		RuntimeJobStatus:     RuntimeJobStatusToProto(status.RuntimeJobStatus),
		RuntimeJobCommandRef: optionalStringPtr(status.RuntimeJobCommandRef),
		RuntimeJobVersion:    optionalPositiveInt64Ptr(status.RuntimeJobVersion),
		RuntimeJobCreatedAt:  optionalTimePtr(status.RuntimeJobCreatedAt),
		RuntimeJobStartedAt:  optionalTimePtr(status.RuntimeJobStartedAt),
		RuntimeJobFinishedAt: optionalTimePtr(status.RuntimeJobFinishedAt),
		RuntimeJobUpdatedAt:  optionalTimePtr(status.RuntimeJobUpdatedAt),
		SafeErrorCode:        optionalStringPtr(status.SafeErrorCode),
		SafeSummary:          optionalStringPtr(status.SafeSummary),
		RunStartedAt:         optionalTimePtr(status.RunStartedAt),
		RunFinishedAt:        optionalTimePtr(status.RunFinishedAt),
		RunUpdatedAt:         formatTime(status.RunUpdatedAt),
		RunVersion:           status.RunVersion,
		HumanGateWaiting:     status.HumanGateWaiting,
		HumanGateRequestRef:  optionalStringPtr(status.HumanGateRequestRef),
		HumanGateReasonCode:  optionalStringPtr(status.HumanGateReasonCode),
		FollowUpWaiting:      status.FollowUpWaiting,
	}
}

func AgentSessionStateSnapshotResponse(output agentservice.SessionSnapshotResult) *agentsv1.AgentSessionStateSnapshotResponse {
	return &agentsv1.AgentSessionStateSnapshotResponse{
		Snapshot: AgentSessionStateSnapshotToProto(output.Snapshot),
		Session:  AgentSessionToProto(output.Session),
	}
}

func ListAgentRunsResponse(output AgentRunListOutput) *agentsv1.ListAgentRunsResponse {
	return &agentsv1.ListAgentRunsResponse{Runs: protoList(output.Items, AgentRunToProto), Page: pageResponseToProto(output.Page)}
}

func ListAgentSessionsResponse(output AgentSessionSummaryListOutput) *agentsv1.ListAgentSessionsResponse {
	return &agentsv1.ListAgentSessionsResponse{Sessions: protoList(output.Items, AgentSessionListItemToProto), Page: pageResponseToProto(output.Page)}
}

func ListAgentRunSummariesResponse(output AgentRunSummaryListOutput) *agentsv1.ListAgentRunSummariesResponse {
	return &agentsv1.ListAgentRunSummariesResponse{Runs: protoList(output.Items, AgentRunListItemToProto), Page: pageResponseToProto(output.Page)}
}

func AcceptanceResultResponse(acceptance entity.AcceptanceResult) *agentsv1.AcceptanceResultResponse {
	return &agentsv1.AcceptanceResultResponse{AcceptanceResult: AcceptanceResultToProto(acceptance)}
}

func ListAcceptanceResultsResponse(output AcceptanceResultListOutput) *agentsv1.ListAcceptanceResultsResponse {
	return &agentsv1.ListAcceptanceResultsResponse{AcceptanceResults: protoList(output.Items, AcceptanceResultToProto), Page: pageResponseToProto(output.Page)}
}

func FollowUpIntentResponse(intent entity.FollowUpIntent) *agentsv1.FollowUpIntentResponse {
	return &agentsv1.FollowUpIntentResponse{FollowUpIntent: FollowUpIntentToProto(intent)}
}

func AgentActivityResponse(activity entity.AgentActivity) *agentsv1.AgentActivityResponse {
	return &agentsv1.AgentActivityResponse{Activity: AgentActivityToProto(activity)}
}

func ListAgentActivitiesResponse(output AgentActivityListOutput) *agentsv1.ListAgentActivitiesResponse {
	return &agentsv1.ListAgentActivitiesResponse{Activities: protoList(output.Items, AgentActivityToProto), Page: pageResponseToProto(output.Page)}
}

func HumanGateRequestResponse(gate entity.HumanGateRequest) *agentsv1.HumanGateRequestResponse {
	return &agentsv1.HumanGateRequestResponse{HumanGateRequest: HumanGateRequestToProto(gate)}
}

func ListHumanGateRequestsResponse(output HumanGateListOutput) *agentsv1.ListHumanGateRequestsResponse {
	return &agentsv1.ListHumanGateRequestsResponse{HumanGateRequests: protoList(output.Items, HumanGateRequestToProto), Page: pageResponseToProto(output.Page)}
}

func SelfDeployPlanResponse(plan entity.SelfDeployPlan) *agentsv1.SelfDeployPlanResponse {
	return &agentsv1.SelfDeployPlanResponse{SelfDeployPlan: SelfDeployPlanToProto(plan)}
}

func ListSelfDeployPlansResponse(output SelfDeployPlanListOutput) *agentsv1.ListSelfDeployPlansResponse {
	return &agentsv1.ListSelfDeployPlansResponse{SelfDeployPlans: protoList(output.Items, SelfDeployPlanToProto), Page: pageResponseToProto(output.Page)}
}

func protoList[Domain any, Proto any](items []Domain, cast func(Domain) *Proto) []*Proto {
	if len(items) == 0 {
		return nil
	}
	result := make([]*Proto, len(items))
	for index := range items {
		result[index] = cast(items[index])
	}
	return result
}

func FlowToProto(flow entity.Flow) *agentsv1.Flow {
	return &agentsv1.Flow{
		Id:              flow.ID.String(),
		Scope:           ScopeToProto(flow.Scope),
		Slug:            flow.Slug,
		DisplayName:     LocalizedTextToProto(flow.DisplayName),
		Description:     LocalizedTextToProto(flow.Description),
		IconObjectUri:   optionalStringPtr(flow.IconObjectURI),
		Status:          FlowStatusToProto(flow.Status),
		ActiveVersionId: optionalUUIDStringPtr(flow.ActiveVersionID),
		Version:         flow.Version,
		CreatedAt:       formatTime(flow.CreatedAt),
		UpdatedAt:       formatTime(flow.UpdatedAt),
	}
}

func FlowVersionToProto(version entity.FlowVersion) *agentsv1.FlowVersion {
	stages := make([]*agentsv1.Stage, 0, len(version.Stages))
	for _, stage := range version.Stages {
		stages = append(stages, StageToProto(stage))
	}
	transitions := make([]*agentsv1.StageTransition, 0, len(version.Transitions))
	for _, transition := range version.Transitions {
		transitions = append(transitions, StageTransitionToProto(transition))
	}
	bindings := make([]*agentsv1.StageRoleBinding, 0, len(version.RoleBindings))
	for _, binding := range version.RoleBindings {
		bindings = append(bindings, StageRoleBindingToProto(binding))
	}
	return &agentsv1.FlowVersion{
		Id:                version.ID.String(),
		FlowId:            version.FlowID.String(),
		Version:           version.Version,
		SourceRef:         optionalStringPtr(version.SourceRef),
		DefinitionDigest:  version.DefinitionDigest,
		Status:            FlowVersionStatusToProto(version.Status),
		Stages:            stages,
		Transitions:       transitions,
		StageRoleBindings: bindings,
		ActivatedAt:       optionalTimePtr(version.ActivatedAt),
		CreatedAt:         formatTime(version.CreatedAt),
	}
}

func StageToProto(stage entity.Stage) *agentsv1.Stage {
	return &agentsv1.Stage{
		Id:                    stage.ID.String(),
		FlowVersionId:         stage.FlowVersionID.String(),
		Slug:                  stage.Slug,
		StageType:             StageTypeToProto(stage.StageType),
		DisplayName:           LocalizedTextToProto(stage.DisplayName),
		IconObjectUri:         optionalStringPtr(stage.IconObjectURI),
		RequiredArtifactsJson: string(stage.RequiredArtifactsJSON),
		AcceptancePolicyJson:  string(stage.AcceptancePolicyJSON),
		Position:              stage.Position,
	}
}

func StageTransitionToProto(transition entity.StageTransition) *agentsv1.StageTransition {
	return &agentsv1.StageTransition{
		Id:            transition.ID.String(),
		FlowVersionId: transition.FlowVersionID.String(),
		FromStageId:   optionalUUIDStringPtr(transition.FromStageID),
		ToStageId:     transition.ToStageID.String(),
		ConditionJson: string(transition.ConditionJSON),
		FollowUpType:  optionalStringPtr(transition.FollowUpType),
	}
}

func StageRoleBindingToProto(binding entity.StageRoleBinding) *agentsv1.StageRoleBinding {
	return &agentsv1.StageRoleBinding{
		Id:                    binding.ID.String(),
		StageId:               binding.StageID.String(),
		RoleProfileId:         binding.RoleProfileID.String(),
		BindingKind:           StageRoleBindingKindToProto(binding.BindingKind),
		LaunchPolicyJson:      string(binding.LaunchPolicyJSON),
		RequiredForAcceptance: binding.RequiredForAcceptance,
	}
}

func RoleProfileToProto(role entity.RoleProfile) *agentsv1.RoleProfile {
	return &agentsv1.RoleProfile{
		Id:                       role.ID.String(),
		Scope:                    ScopeToProto(role.Scope),
		Slug:                     role.Slug,
		DisplayName:              LocalizedTextToProto(role.DisplayName),
		IconObjectUri:            optionalStringPtr(role.IconObjectURI),
		RoleKind:                 RoleKindToProto(role.RoleKind),
		RuntimeProfile:           role.RuntimeProfile,
		AllowedMcpTools:          role.AllowedMCPTools,
		ProviderAccountPolicyRef: optionalStringPtr(role.ProviderAccountPolicyRef),
		Status:                   RoleStatusToProto(role.Status),
		Version:                  role.Version,
		CreatedAt:                formatTime(role.CreatedAt),
		UpdatedAt:                formatTime(role.UpdatedAt),
	}
}

func PromptTemplateToProto(template entity.PromptTemplate) *agentsv1.PromptTemplate {
	return &agentsv1.PromptTemplate{
		Id:              template.ID.String(),
		RoleProfileId:   template.RoleProfileID.String(),
		PromptKind:      PromptKindToProto(template.PromptKind),
		ActiveVersionId: optionalUUIDStringPtr(template.ActiveVersionID),
		Version:         template.Version,
		CreatedAt:       formatTime(template.CreatedAt),
		UpdatedAt:       formatTime(template.UpdatedAt),
	}
}

func PromptTemplateVersionToProto(version entity.PromptTemplateVersion) *agentsv1.PromptTemplateVersion {
	return &agentsv1.PromptTemplateVersion{
		Id:               version.ID.String(),
		PromptTemplateId: version.PromptTemplateID.String(),
		RoleProfileId:    version.RoleProfileID.String(),
		PromptKind:       PromptKindToProto(version.PromptKind),
		Version:          version.Version,
		SourceRef:        optionalStringPtr(version.SourceRef),
		TemplateObject:   ObjectRefToProto(version.TemplateObject),
		TemplateDigest:   version.TemplateDigest,
		Status:           PromptVersionStatusToProto(version.Status),
		CreatedAt:        formatTime(version.CreatedAt),
		ActivatedAt:      optionalTimePtr(version.ActivatedAt),
	}
}

func AgentSessionToProto(session entity.AgentSession) *agentsv1.AgentSession {
	return &agentsv1.AgentSession{
		Id:                    session.ID.String(),
		Scope:                 ScopeToProto(session.Scope),
		ProviderWorkItemRef:   optionalStringPtr(session.ProviderWorkItemRef),
		FlowVersionId:         optionalUUIDStringPtr(session.FlowVersionID),
		CurrentStageId:        optionalUUIDStringPtr(session.CurrentStageID),
		LatestStateSnapshotId: optionalUUIDStringPtr(session.LatestStateSnapshotID),
		Status:                AgentSessionStatusToProto(session.Status),
		CreatedByActorRef:     session.CreatedByActorRef,
		Version:               session.Version,
		CreatedAt:             formatTime(session.CreatedAt),
		UpdatedAt:             formatTime(session.UpdatedAt),
	}
}

func AgentRunToProto(run entity.AgentRun) *agentsv1.AgentRun {
	return &agentsv1.AgentRun{
		Id:                      run.ID.String(),
		SessionId:               run.SessionID.String(),
		FlowVersionId:           optionalUUIDStringPtr(run.FlowVersionID),
		StageId:                 optionalUUIDStringPtr(run.StageID),
		RoleProfileId:           run.RoleProfileID.String(),
		RoleProfileVersion:      run.RoleProfileVersion,
		RoleProfileDigest:       run.RoleProfileDigest,
		PromptTemplateVersionId: run.PromptTemplateVersionID.String(),
		PromptTemplateDigest:    run.PromptTemplateDigest,
		RuntimeContext:          RuntimeContextToProto(run.RuntimeContext),
		ProviderTarget:          ProviderTargetToProto(run.ProviderTarget),
		GuidanceRefs:            protoList(run.GuidanceRefs, GuidanceRefToProto),
		Status:                  AgentRunStatusToProto(run.Status),
		ResultSummary:           optionalStringPtr(run.ResultSummary),
		FailureCode:             optionalStringPtr(run.FailureCode),
		Version:                 run.Version,
		StartedAt:               optionalTimePtr(run.StartedAt),
		FinishedAt:              optionalTimePtr(run.FinishedAt),
		CreatedAt:               formatTime(run.CreatedAt),
		UpdatedAt:               formatTime(run.UpdatedAt),
	}
}

func AgentRunListItemToProto(item entity.AgentRunListItem) *agentsv1.AgentRunListItem {
	jobRef := item.Run.RuntimeContext.JobRef
	return &agentsv1.AgentRunListItem{
		Run:                     AgentRunToProto(item.Run),
		RuntimeJobRef:           optionalStringPtr(jobRef),
		RuntimeObservationState: runtimeObservationStateForJobRef(jobRef),
		RuntimeSafeErrorCode:    optionalStringPtr(item.Run.FailureCode),
		RuntimeSafeSummary:      optionalStringPtr(item.Run.ResultSummary),
		HumanGateWaiting:        item.HumanGateWaiting,
		HumanGateRequestRef:     optionalStringPtr(item.HumanGateRequestRef),
		HumanGateReasonCode:     optionalStringPtr(item.HumanGateReasonCode),
		FollowUpWaiting:         item.FollowUpWaiting,
		LatestActivity:          AgentActivitySummaryToProto(item.LatestActivity),
	}
}

func AgentSessionListItemToProto(item entity.AgentSessionListItem) *agentsv1.AgentSessionListItem {
	return &agentsv1.AgentSessionListItem{
		Session:              AgentSessionToProto(item.Session),
		LatestRunId:          optionalUUIDStringPtr(item.LatestRunID),
		LatestRunStatus:      optionalAgentRunStatusPtr(item.LatestRunStatus),
		LatestRuntimeJobRef:  optionalStringPtr(item.LatestRuntimeJobRef),
		LatestRunSafeSummary: optionalStringPtr(item.LatestRunSafeSummary),
		ActiveRunCount:       item.ActiveRunCount,
		HumanGateWaiting:     item.HumanGateWaiting,
		HumanGateRequestRef:  optionalStringPtr(item.HumanGateRequestRef),
		HumanGateReasonCode:  optionalStringPtr(item.HumanGateReasonCode),
		LatestActivity:       AgentActivitySummaryToProto(item.LatestActivity),
		FollowUpWaiting:      item.FollowUpWaiting,
		FollowUpRef:          optionalStringPtr(item.FollowUpRef),
	}
}

func runtimeObservationStateForJobRef(jobRef string) agentsv1.AgentRunRuntimeObservationState {
	if jobRef == "" {
		return agentsv1.AgentRunRuntimeObservationState_AGENT_RUN_RUNTIME_OBSERVATION_STATE_NOT_CREATED
	}
	return agentsv1.AgentRunRuntimeObservationState_AGENT_RUN_RUNTIME_OBSERVATION_STATE_STORED_REF
}

func AgentSessionStateSnapshotToProto(snapshot entity.AgentSessionStateSnapshot) *agentsv1.AgentSessionStateSnapshot {
	return &agentsv1.AgentSessionStateSnapshot{
		Id:           snapshot.ID.String(),
		SessionId:    snapshot.SessionID.String(),
		RunId:        optionalUUIDStringPtr(snapshot.RunID),
		SnapshotKind: AgentSessionSnapshotKindToProto(snapshot.SnapshotKind),
		TurnIndex:    snapshot.TurnIndex,
		Object:       ObjectRefToProto(snapshot.Object),
		CapturedAt:   formatTime(snapshot.CapturedAt),
		CreatedAt:    formatTime(snapshot.CreatedAt),
	}
}

func AcceptanceResultToProto(acceptance entity.AcceptanceResult) *agentsv1.AcceptanceResult {
	return &agentsv1.AcceptanceResult{
		Id:                acceptance.ID.String(),
		SessionId:         acceptance.SessionID.String(),
		RunId:             optionalUUIDStringPtr(acceptance.RunID),
		StageId:           optionalUUIDStringPtr(acceptance.StageID),
		CheckKind:         AcceptanceCheckKindToProto(acceptance.CheckKind),
		Status:            AcceptanceStatusToProto(acceptance.Status),
		TargetRef:         optionalStringPtr(acceptance.TargetRef),
		DetailsJson:       string(acceptance.DetailsJSON),
		Version:           acceptance.Version,
		CreatedAt:         formatTime(acceptance.CreatedAt),
		UpdatedAt:         formatTime(acceptance.UpdatedAt),
		GovernanceContext: GovernanceContextToProto(acceptance.GovernanceContext),
	}
}

func FollowUpIntentToProto(intent entity.FollowUpIntent) *agentsv1.FollowUpIntent {
	return &agentsv1.FollowUpIntent{
		Id:                    intent.ID.String(),
		SessionId:             intent.SessionID.String(),
		FromStageId:           optionalUUIDStringPtr(intent.FromStageID),
		ToStageId:             optionalUUIDStringPtr(intent.ToStageID),
		ProviderWorkItemType:  intent.ProviderWorkItemType,
		ProviderOperationRef:  optionalStringPtr(intent.ProviderOperationRef),
		Status:                FollowUpIntentStatusToProto(intent.Status),
		InstructionBodyDigest: optionalStringPtr(intent.InstructionBodyDigest),
		Version:               intent.Version,
		CreatedAt:             formatTime(intent.CreatedAt),
		UpdatedAt:             formatTime(intent.UpdatedAt),
		RunId:                 optionalUUIDStringPtr(intent.RunID),
		AcceptanceResultId:    optionalUUIDStringPtr(intent.AcceptanceResultID),
		ProviderTarget:        ProviderTargetToProto(intent.ProviderTarget),
		SafeTitle:             optionalStringPtr(intent.SafeTitle),
		SafeSummary:           optionalStringPtr(intent.SafeSummary),
		RoleHint:              optionalStringPtr(intent.RoleHint),
		StageHint:             optionalStringPtr(intent.StageHint),
		IdempotencyKey:        intent.IdempotencyKey,
		GovernanceContext:     GovernanceContextToProto(intent.GovernanceContext),
	}
}

func AgentActivityToProto(activity entity.AgentActivity) *agentsv1.AgentActivity {
	return &agentsv1.AgentActivity{
		Id:              activity.ID.String(),
		SessionId:       activity.SessionID.String(),
		RunId:           optionalUUIDStringPtr(activity.RunID),
		TurnId:          optionalStringPtr(activity.TurnID),
		ToolUseId:       optionalStringPtr(activity.ToolUseID),
		ActivityKind:    AgentActivityKindToProto(activity.ActivityKind),
		ToolName:        optionalStringPtr(activity.ToolName),
		ToolCategory:    optionalStringPtr(activity.ToolCategory),
		Status:          AgentActivityStatusToProto(activity.Status),
		StartedAt:       optionalTimeValuePtr(activity.StartedAt),
		FinishedAt:      optionalTimePtr(activity.FinishedAt),
		DurationMs:      activity.DurationMs,
		SafeSummary:     optionalStringPtr(activity.SafeSummary),
		PayloadDigest:   optionalStringPtr(activity.PayloadDigest),
		BoundedError:    optionalStringPtr(activity.BoundedError),
		SafeRefsJson:    string(activity.SafeRefsJSON),
		SafeDetailsJson: string(activity.SafeDetailsJSON),
		CorrelationId:   optionalStringPtr(activity.CorrelationID),
		IdempotencyKey:  activity.IdempotencyKey,
		Version:         activity.Version,
		CreatedAt:       formatTime(activity.CreatedAt),
		UpdatedAt:       formatTime(activity.UpdatedAt),
	}
}

func AgentActivitySummaryToProto(activity *entity.AgentActivitySummary) *agentsv1.AgentActivitySummary {
	if activity == nil {
		return nil
	}
	return &agentsv1.AgentActivitySummary{
		ActivityId:    activity.ID.String(),
		ActivityKind:  AgentActivityKindToProto(activity.ActivityKind),
		Status:        AgentActivityStatusToProto(activity.Status),
		ToolName:      optionalStringPtr(activity.ToolName),
		ToolCategory:  optionalStringPtr(activity.ToolCategory),
		SafeSummary:   optionalStringPtr(activity.SafeSummary),
		PayloadDigest: optionalStringPtr(activity.PayloadDigest),
		BoundedError:  optionalStringPtr(activity.BoundedError),
		StartedAt:     optionalTimePtr(activity.StartedAt),
		FinishedAt:    optionalTimePtr(activity.FinishedAt),
		UpdatedAt:     formatTime(activity.UpdatedAt),
		Version:       activity.Version,
	}
}

func HumanGateRequestToProto(gate entity.HumanGateRequest) *agentsv1.HumanGateRequest {
	return &agentsv1.HumanGateRequest{
		Id:                       gate.ID.String(),
		SessionId:                gate.SessionID.String(),
		RunId:                    optionalUUIDStringPtr(gate.RunID),
		StageId:                  optionalUUIDStringPtr(gate.StageID),
		AcceptanceResultId:       optionalUUIDStringPtr(gate.AcceptanceResultID),
		ProviderTarget:           ProviderTargetToProto(gate.ProviderTarget),
		TargetRef:                optionalStringPtr(gate.TargetRef),
		RequestKind:              gate.RequestKind,
		ReasonCode:               gate.ReasonCode,
		SafeSummary:              optionalStringPtr(gate.SafeSummary),
		InteractionRequestRef:    optionalStringPtr(gate.InteractionRequestRef),
		InteractionResponseRef:   optionalStringPtr(gate.InteractionResponseRef),
		GovernanceGateRequestRef: optionalStringPtr(gate.GovernanceGateRequestRef),
		GovernanceDecisionRef:    optionalStringPtr(gate.GovernanceDecisionRef),
		Status:                   HumanGateStatusToProto(gate.Status),
		Outcome:                  HumanGateOutcomeToProto(gate.Outcome),
		IdempotencyKey:           gate.IdempotencyKey,
		Version:                  gate.Version,
		CreatedAt:                formatTime(gate.CreatedAt),
		UpdatedAt:                formatTime(gate.UpdatedAt),
		ResolvedAt:               optionalTimePtr(gate.ResolvedAt),
		GovernanceContext:        GovernanceContextToProto(gate.GovernanceContext),
	}
}

func SelfDeployPlanToProto(plan entity.SelfDeployPlan) *agentsv1.SelfDeployPlan {
	return &agentsv1.SelfDeployPlan{
		Id:                      plan.ID.String(),
		Scope:                   ScopeToProto(plan.Scope),
		ProjectRef:              plan.ProjectRef,
		RepositoryRef:           plan.RepositoryRef,
		ProviderSignalRef:       optionalStringPtr(plan.ProviderSignalRef),
		SourceRef:               plan.SourceRef,
		MergeCommitSha:          plan.MergeCommitSHA,
		ServicesYamlRef:         optionalStringPtr(plan.ServicesYAMLRef),
		ServicesYamlDigest:      plan.ServicesYAMLDigest,
		AffectedServiceKeys:     append([]string(nil), plan.AffectedServiceKeys...),
		PathCategories:          SelfDeployPathCategoriesToProto(plan.PathCategories),
		ExpectedRuntimeJobTypes: SelfDeployRuntimeJobTypesToProto(plan.ExpectedRuntimeJobTypes),
		GovernanceContext:       GovernanceContextToProto(plan.GovernanceContext),
		SafeSummary:             optionalStringPtr(plan.SafeSummary),
		PlanFingerprint:         plan.PlanFingerprint,
		IdempotencyKey:          plan.IdempotencyKey,
		Status:                  SelfDeployPlanStatusToProto(plan.Status),
		Version:                 plan.Version,
		CreatedAt:               formatTime(plan.CreatedAt),
		UpdatedAt:               formatTime(plan.UpdatedAt),
	}
}

func SelfDeployPathCategoriesToProto(values []enum.SelfDeployPathCategory) []agentsv1.SelfDeployPathCategory {
	result := make([]agentsv1.SelfDeployPathCategory, 0, len(values))
	for _, value := range values {
		result = append(result, SelfDeployPathCategoryToProto(value))
	}
	return result
}

func SelfDeployRuntimeJobTypesToProto(values []enum.SelfDeployRuntimeJobType) []runtimev1.JobType {
	result := make([]runtimev1.JobType, 0, len(values))
	for _, value := range values {
		result = append(result, SelfDeployRuntimeJobTypeToProto(value))
	}
	return result
}

func ScopeToProto(scope value.ScopeRef) *agentsv1.ScopeRef {
	return &agentsv1.ScopeRef{Type: ScopeTypeToProto(scope.Type), Ref: scope.Ref}
}

func LocalizedTextToProto(text []value.LocalizedText) []*agentsv1.LocalizedText {
	return collectProtoValues(text, localizedTextProto)
}

func ObjectRefToProto(object value.ObjectRef) *agentsv1.ObjectRef {
	if objectRefIsEmpty(object) {
		return nil
	}
	return objectRefProto(object)
}

func localizedTextProto(item value.LocalizedText) (*agentsv1.LocalizedText, bool) {
	return &agentsv1.LocalizedText{Locale: item.Locale, Text: item.Text}, true
}

func objectRefIsEmpty(object value.ObjectRef) bool {
	return object.ObjectURI == "" && object.ObjectDigest == "" && object.ObjectSizeBytes == nil
}

func objectRefProto(object value.ObjectRef) *agentsv1.ObjectRef {
	return &agentsv1.ObjectRef{
		ObjectUri:       object.ObjectURI,
		ObjectDigest:    object.ObjectDigest,
		ObjectSizeBytes: object.ObjectSizeBytes,
	}
}

func RuntimeContextToProto(context value.RuntimeContextRef) *agentsv1.RuntimeContextRef {
	if context.SlotRef == "" && context.JobRef == "" && context.WorkspaceRef == "" && context.ContextRef == "" {
		return nil
	}
	slotRef := optionalStringPtr(context.SlotRef)
	jobRef := optionalStringPtr(context.JobRef)
	workspaceRef := optionalStringPtr(context.WorkspaceRef)
	contextRef := optionalStringPtr(context.ContextRef)
	return &agentsv1.RuntimeContextRef{
		SlotRef:      slotRef,
		JobRef:       jobRef,
		WorkspaceRef: workspaceRef,
		ContextRef:   contextRef,
	}
}

func ProviderTargetToProto(target value.ProviderTargetRef) *agentsv1.ProviderTargetRef {
	if target.WorkItemRef == "" && target.PullRequestRef == "" && target.CommentRef == "" && target.ReviewSignalRef == "" {
		return nil
	}
	return &agentsv1.ProviderTargetRef{
		WorkItemRef:     optionalStringPtr(target.WorkItemRef),
		PullRequestRef:  optionalStringPtr(target.PullRequestRef),
		CommentRef:      optionalStringPtr(target.CommentRef),
		ReviewSignalRef: optionalStringPtr(target.ReviewSignalRef),
	}
}

func GovernanceContextToProto(context value.GovernanceContextRef) *agentsv1.GovernanceContextRef {
	if context.RiskAssessmentRef == "" &&
		context.GateRequestRef == "" &&
		context.GateDecisionRef == "" &&
		context.ReleaseDecisionPackageRef == "" &&
		context.ReleaseDecisionRef == "" &&
		context.RiskProfileRef == "" &&
		context.GatePolicyRef == "" &&
		context.ReleasePolicyRef == "" {
		return nil
	}
	return &agentsv1.GovernanceContextRef{
		RiskAssessmentRef:         optionalStringPtr(context.RiskAssessmentRef),
		GateRequestRef:            optionalStringPtr(context.GateRequestRef),
		GateDecisionRef:           optionalStringPtr(context.GateDecisionRef),
		ReleaseDecisionPackageRef: optionalStringPtr(context.ReleaseDecisionPackageRef),
		ReleaseDecisionRef:        optionalStringPtr(context.ReleaseDecisionRef),
		RiskProfileRef:            optionalStringPtr(context.RiskProfileRef),
		GatePolicyRef:             optionalStringPtr(context.GatePolicyRef),
		ReleasePolicyRef:          optionalStringPtr(context.ReleasePolicyRef),
	}
}

func GuidanceRefToProto(ref value.GuidanceRef) *agentsv1.GuidanceRef {
	return &agentsv1.GuidanceRef{
		PackageInstallationRef: ref.PackageInstallationRef,
		PackageVersionRef:      ref.PackageVersionRef,
		ManifestDigest:         ref.ManifestDigest,
		SourceRef:              optionalStringPtr(ref.SourceRef),
		CapabilityRef:          optionalStringPtr(ref.CapabilityRef),
		CapabilityKind:         optionalStringPtr(ref.CapabilityKind),
		PackageRef:             optionalStringPtr(ref.PackageRef),
		PackageSlug:            optionalStringPtr(ref.PackageSlug),
		PackageVersionLabel:    optionalStringPtr(ref.PackageVersionLabel),
		PolicySummaryJson:      optionalStringPtr(ref.PolicySummaryJSON),
	}
}

func optionalUUIDStringPtr(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	value := id.String()
	return &value
}

func optionalAgentRunStatusPtr(status enum.AgentRunStatus) *agentsv1.AgentRunStatus {
	if status == "" {
		return nil
	}
	value := AgentRunStatusToProto(status)
	return &value
}

func optionalPositiveInt64Ptr(value int64) *int64 {
	if value <= 0 {
		return nil
	}
	copied := value
	return &copied
}

func optionalTimePtr(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := formatTime(*value)
	return &formatted
}

func optionalTimeValuePtr(value time.Time) *string {
	if value.IsZero() {
		return nil
	}
	formatted := formatTime(value)
	return &formatted
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
