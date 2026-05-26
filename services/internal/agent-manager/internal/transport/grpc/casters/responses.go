package casters

import (
	"time"

	"github.com/google/uuid"

	agentsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/agents/v1"
	agentservice "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
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

type AcceptanceResultListOutput = PageOutput[entity.AcceptanceResult]

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

func AgentSessionStateSnapshotResponse(output agentservice.SessionSnapshotResult) *agentsv1.AgentSessionStateSnapshotResponse {
	return &agentsv1.AgentSessionStateSnapshotResponse{
		Snapshot: AgentSessionStateSnapshotToProto(output.Snapshot),
		Session:  AgentSessionToProto(output.Session),
	}
}

func ListAgentRunsResponse(output AgentRunListOutput) *agentsv1.ListAgentRunsResponse {
	return &agentsv1.ListAgentRunsResponse{Runs: protoList(output.Items, AgentRunToProto), Page: pageResponseToProto(output.Page)}
}

func AcceptanceResultResponse(acceptance entity.AcceptanceResult) *agentsv1.AcceptanceResultResponse {
	return &agentsv1.AcceptanceResultResponse{AcceptanceResult: AcceptanceResultToProto(acceptance)}
}

func ListAcceptanceResultsResponse(output AcceptanceResultListOutput) *agentsv1.ListAcceptanceResultsResponse {
	return &agentsv1.ListAcceptanceResultsResponse{AcceptanceResults: protoList(output.Items, AcceptanceResultToProto), Page: pageResponseToProto(output.Page)}
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
		Id:          acceptance.ID.String(),
		SessionId:   acceptance.SessionID.String(),
		RunId:       optionalUUIDStringPtr(acceptance.RunID),
		StageId:     optionalUUIDStringPtr(acceptance.StageID),
		CheckKind:   AcceptanceCheckKindToProto(acceptance.CheckKind),
		Status:      AcceptanceStatusToProto(acceptance.Status),
		TargetRef:   optionalStringPtr(acceptance.TargetRef),
		DetailsJson: string(acceptance.DetailsJSON),
		Version:     acceptance.Version,
		CreatedAt:   formatTime(acceptance.CreatedAt),
		UpdatedAt:   formatTime(acceptance.UpdatedAt),
	}
}

func ScopeToProto(scope value.ScopeRef) *agentsv1.ScopeRef {
	return &agentsv1.ScopeRef{Type: ScopeTypeToProto(scope.Type), Ref: scope.Ref}
}

func LocalizedTextToProto(text []value.LocalizedText) []*agentsv1.LocalizedText {
	result := make([]*agentsv1.LocalizedText, 0, len(text))
	for _, item := range text {
		result = append(result, &agentsv1.LocalizedText{Locale: item.Locale, Text: item.Text})
	}
	return result
}

func ObjectRefToProto(object value.ObjectRef) *agentsv1.ObjectRef {
	if object.ObjectURI == "" && object.ObjectDigest == "" && object.ObjectSizeBytes == nil {
		return nil
	}
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

func optionalTimePtr(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := formatTime(*value)
	return &formatted
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
