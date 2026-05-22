package casters

import (
	"strings"
	"time"

	"github.com/google/uuid"

	agentsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/agents/v1"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

func CreateFlowInput(request *agentsv1.CreateFlowRequest) (service.CreateFlowInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return service.CreateFlowInput{}, err
	}
	scope, err := ScopeFromProto(request.GetScope())
	if err != nil {
		return service.CreateFlowInput{}, err
	}
	return service.CreateFlowInput{
		Meta:          meta,
		Scope:         scope,
		Slug:          strings.TrimSpace(request.GetSlug()),
		DisplayName:   LocalizedTextFromProto(request.GetDisplayName()),
		Description:   LocalizedTextFromProto(request.GetDescription()),
		IconObjectURI: strings.TrimSpace(request.GetIconObjectUri()),
	}, nil
}

func UpdateFlowInput(request *agentsv1.UpdateFlowRequest) (service.UpdateFlowInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return service.UpdateFlowInput{}, err
	}
	flowID, err := requiredUUID(request.GetFlowId())
	if err != nil {
		return service.UpdateFlowInput{}, err
	}
	status, err := OptionalFlowStatusFromProto(request.Status)
	if err != nil {
		return service.UpdateFlowInput{}, err
	}
	input := service.UpdateFlowInput{
		Meta:          meta,
		FlowID:        flowID,
		DisplayName:   LocalizedTextFromProto(request.GetDisplayName()),
		Description:   LocalizedTextFromProto(request.GetDescription()),
		IconObjectURI: strings.TrimSpace(request.GetIconObjectUri()),
	}
	if status != nil {
		input.Status = *status
	}
	return input, nil
}

func CreateFlowVersionInput(request *agentsv1.CreateFlowVersionRequest) (service.CreateFlowVersionInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return service.CreateFlowVersionInput{}, err
	}
	flowID, err := requiredUUID(request.GetFlowId())
	if err != nil {
		return service.CreateFlowVersionInput{}, err
	}
	definition := request.GetDefinition()
	if definition == nil {
		return service.CreateFlowVersionInput{}, errs.ErrInvalidArgument
	}
	stages, err := stageInputs(definition.GetStages())
	if err != nil {
		return service.CreateFlowVersionInput{}, err
	}
	transitions := stageTransitionInputs(definition.GetTransitions())
	bindings, err := stageRoleBindingInputs(definition.GetStageRoleBindings())
	if err != nil {
		return service.CreateFlowVersionInput{}, err
	}
	return service.CreateFlowVersionInput{
		Meta:             meta,
		FlowID:           flowID,
		SourceRef:        strings.TrimSpace(request.GetSourceRef()),
		DefinitionDigest: strings.TrimSpace(definition.GetDefinitionDigest()),
		Stages:           stages,
		Transitions:      transitions,
		RoleBindings:     bindings,
	}, nil
}

func ActivateFlowVersionInput(request *agentsv1.ActivateFlowVersionRequest) (service.ActivateFlowVersionInput, error) {
	return idCommandInput(request.GetFlowVersionId(), request.GetMeta(), newActivateFlowVersionInput)
}

func GetFlowInput(request *agentsv1.GetFlowRequest) (IDQueryInput, error) {
	return idQueryInput(request.GetFlowId(), request.GetMeta())
}

func ListFlowsInput(request *agentsv1.ListFlowsRequest) (service.FlowList, error) {
	return scopedListInput(request.GetMeta(), request.GetScope(), request.Status, request.GetPage(), OptionalFlowStatusFromProto,
		func(scope value.ScopeRef, status *serviceFlowStatus, page value.PageRequest) service.FlowList {
			return service.FlowList{Scope: scope, Status: status, Page: page}
		})
}

func CreateRoleProfileInput(request *agentsv1.CreateRoleProfileRequest) (service.CreateRoleProfileInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return service.CreateRoleProfileInput{}, err
	}
	scope, err := ScopeFromProto(request.GetScope())
	if err != nil {
		return service.CreateRoleProfileInput{}, err
	}
	roleKind, err := RoleKindFromProto(request.GetRoleKind())
	if err != nil {
		return service.CreateRoleProfileInput{}, err
	}
	return service.CreateRoleProfileInput{
		Meta:                     meta,
		Scope:                    scope,
		Slug:                     strings.TrimSpace(request.GetSlug()),
		DisplayName:              LocalizedTextFromProto(request.GetDisplayName()),
		IconObjectURI:            strings.TrimSpace(request.GetIconObjectUri()),
		RoleKind:                 roleKind,
		RuntimeProfile:           strings.TrimSpace(request.GetRuntimeProfile()),
		AllowedMCPTools:          stringList(request.GetAllowedMcpTools()),
		ProviderAccountPolicyRef: strings.TrimSpace(request.GetProviderAccountPolicyRef()),
	}, nil
}

func UpdateRoleProfileInput(request *agentsv1.UpdateRoleProfileRequest) (service.UpdateRoleProfileInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return service.UpdateRoleProfileInput{}, err
	}
	roleProfileID, err := requiredUUID(request.GetRoleProfileId())
	if err != nil {
		return service.UpdateRoleProfileInput{}, err
	}
	roleKind, err := OptionalRoleKindFromProto(request.RoleKind)
	if err != nil {
		return service.UpdateRoleProfileInput{}, err
	}
	status, err := OptionalRoleStatusFromProto(request.Status)
	if err != nil {
		return service.UpdateRoleProfileInput{}, err
	}
	input := service.UpdateRoleProfileInput{
		Meta:                     meta,
		RoleProfileID:            roleProfileID,
		DisplayName:              LocalizedTextFromProto(request.GetDisplayName()),
		IconObjectURI:            strings.TrimSpace(request.GetIconObjectUri()),
		RuntimeProfile:           strings.TrimSpace(request.GetRuntimeProfile()),
		AllowedMCPTools:          stringList(request.GetAllowedMcpTools()),
		ProviderAccountPolicyRef: strings.TrimSpace(request.GetProviderAccountPolicyRef()),
	}
	if roleKind != nil {
		input.RoleKind = *roleKind
	}
	if status != nil {
		input.Status = *status
	}
	return input, nil
}

func GetRoleProfileInput(request *agentsv1.GetRoleProfileRequest) (IDQueryInput, error) {
	return idQueryInput(request.GetRoleProfileId(), request.GetMeta())
}

func ListRoleProfilesInput(request *agentsv1.ListRoleProfilesRequest) (service.RoleProfileList, error) {
	return roleProfileListInput(request.GetMeta(), request.GetScope(), request.RoleKind, request.Status, request.GetPage())
}

func GetPromptTemplateInput(request *agentsv1.GetPromptTemplateRequest) (IDQueryInput, error) {
	return idQueryInput(request.GetPromptTemplateId(), request.GetMeta())
}

func ListPromptTemplatesInput(request *agentsv1.ListPromptTemplatesRequest) (service.PromptTemplateList, error) {
	roleProfileID, kind, page, err := promptListBase(request.GetMeta(), request.GetRoleProfileId(), request.PromptKind, request.GetPage())
	if err != nil {
		return service.PromptTemplateList{}, err
	}
	return service.PromptTemplateList{RoleProfileID: roleProfileID, Kind: kind, Page: page}, nil
}

func CreatePromptTemplateVersionInput(request *agentsv1.CreatePromptTemplateVersionRequest) (service.CreatePromptTemplateVersionInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return service.CreatePromptTemplateVersionInput{}, err
	}
	roleProfileID, err := requiredUUID(request.GetRoleProfileId())
	if err != nil {
		return service.CreatePromptTemplateVersionInput{}, err
	}
	promptKind, err := PromptKindFromProto(request.GetPromptKind())
	if err != nil {
		return service.CreatePromptTemplateVersionInput{}, err
	}
	return service.CreatePromptTemplateVersionInput{
		Meta:           meta,
		RoleProfileID:  roleProfileID,
		PromptKind:     promptKind,
		SourceRef:      strings.TrimSpace(request.GetSourceRef()),
		TemplateObject: ObjectRefFromProto(request.GetTemplateObject()),
		TemplateDigest: strings.TrimSpace(request.GetTemplateDigest()),
	}, nil
}

func ActivatePromptTemplateVersionInput(request *agentsv1.ActivatePromptTemplateVersionRequest) (service.ActivatePromptTemplateVersionInput, error) {
	return idCommandInput(request.GetPromptTemplateVersionId(), request.GetMeta(), newActivatePromptTemplateVersionInput)
}

func GetPromptTemplateVersionInput(request *agentsv1.GetPromptTemplateVersionRequest) (IDQueryInput, error) {
	return idQueryInput(request.GetPromptTemplateVersionId(), request.GetMeta())
}

func ListPromptTemplateVersionsInput(request *agentsv1.ListPromptTemplateVersionsRequest) (service.PromptTemplateVersionList, error) {
	roleProfileID, kind, page, err := promptListBase(request.GetMeta(), request.GetRoleProfileId(), request.PromptKind, request.GetPage())
	if err != nil {
		return service.PromptTemplateVersionList{}, err
	}
	status, err := OptionalPromptVersionStatusFromProto(request.Status)
	if err != nil {
		return service.PromptTemplateVersionList{}, err
	}
	return service.PromptTemplateVersionList{RoleProfileID: roleProfileID, Kind: kind, Status: status, Page: page}, nil
}

func StartAgentSessionInput(request *agentsv1.StartAgentSessionRequest) (service.StartAgentSessionInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return service.StartAgentSessionInput{}, err
	}
	scope, err := ScopeFromProto(request.GetScope())
	if err != nil {
		return service.StartAgentSessionInput{}, err
	}
	flowVersionID, err := optionalUUIDPtr(request.GetFlowVersionId())
	if err != nil {
		return service.StartAgentSessionInput{}, err
	}
	currentStageID, err := optionalUUIDPtr(request.GetCurrentStageId())
	if err != nil {
		return service.StartAgentSessionInput{}, err
	}
	return service.StartAgentSessionInput{
		Meta:                meta,
		Scope:               scope,
		ProviderWorkItemRef: strings.TrimSpace(request.GetProviderWorkItemRef()),
		FlowVersionID:       flowVersionID,
		CurrentStageID:      currentStageID,
		CreatedByActorRef:   strings.TrimSpace(request.GetCreatedByActorRef()),
	}, nil
}

func StartAgentRunInput(request *agentsv1.StartAgentRunRequest) (service.StartAgentRunInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return service.StartAgentRunInput{}, err
	}
	sessionID, err := requiredUUID(request.GetSessionId())
	if err != nil {
		return service.StartAgentRunInput{}, err
	}
	roleID, err := requiredUUID(request.GetRoleProfileId())
	if err != nil {
		return service.StartAgentRunInput{}, err
	}
	promptVersionID, err := requiredUUID(request.GetPromptTemplateVersionId())
	if err != nil {
		return service.StartAgentRunInput{}, err
	}
	flowVersionID, err := optionalUUIDPtr(request.GetFlowVersionId())
	if err != nil {
		return service.StartAgentRunInput{}, err
	}
	stageID, err := optionalUUIDPtr(request.GetStageId())
	if err != nil {
		return service.StartAgentRunInput{}, err
	}
	return service.StartAgentRunInput{
		Meta:                    meta,
		SessionID:               sessionID,
		FlowVersionID:           flowVersionID,
		StageID:                 stageID,
		RoleProfileID:           roleID,
		PromptTemplateVersionID: promptVersionID,
		ProviderTarget:          ProviderTargetFromProto(request.GetProviderTarget()),
		GuidanceSelectionHints:  GuidanceSelectionHintsFromProto(request.GetGuidanceSelectionHints()),
	}, nil
}

func RecordRunStateInput(request *agentsv1.RecordRunStateRequest) (service.RecordRunStateInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return service.RecordRunStateInput{}, err
	}
	runID, err := requiredUUID(request.GetRunId())
	if err != nil {
		return service.RecordRunStateInput{}, err
	}
	status, err := AgentRunStatusFromProto(request.GetStatus())
	if err != nil {
		return service.RecordRunStateInput{}, err
	}
	startedAt, err := optionalTimeFromProto(request.StartedAt)
	if err != nil {
		return service.RecordRunStateInput{}, err
	}
	finishedAt, err := optionalTimeFromProto(request.FinishedAt)
	if err != nil {
		return service.RecordRunStateInput{}, err
	}
	return service.RecordRunStateInput{
		Meta:           meta,
		RunID:          runID,
		Status:         status,
		RuntimeContext: runtimeContextInput(request.RuntimeContext),
		ProviderTarget: providerTargetInput(request.ProviderTarget),
		ResultSummary:  optionalPresentString(request.ResultSummary),
		FailureCode:    optionalPresentString(request.FailureCode),
		ReasonCode:     optionalPresentString(request.ReasonCode),
		StartedAt:      startedAt,
		FinishedAt:     finishedAt,
	}, nil
}

func RecordSessionStateSnapshotInput(request *agentsv1.RecordSessionStateSnapshotRequest) (service.RecordSessionStateSnapshotInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return service.RecordSessionStateSnapshotInput{}, err
	}
	sessionID, err := requiredUUID(request.GetSessionId())
	if err != nil {
		return service.RecordSessionStateSnapshotInput{}, err
	}
	runID, err := optionalUUIDPtr(request.GetRunId())
	if err != nil {
		return service.RecordSessionStateSnapshotInput{}, err
	}
	kind, err := AgentSessionSnapshotKindFromProto(request.GetSnapshotKind())
	if err != nil {
		return service.RecordSessionStateSnapshotInput{}, err
	}
	capturedAt, err := requiredTimeFromProto(request.GetCapturedAt())
	if err != nil {
		return service.RecordSessionStateSnapshotInput{}, err
	}
	return service.RecordSessionStateSnapshotInput{
		Meta:         meta,
		SessionID:    sessionID,
		RunID:        runID,
		SnapshotKind: kind,
		TurnIndex:    request.TurnIndex,
		Object:       ObjectRefFromProto(request.GetObject()),
		CapturedAt:   capturedAt,
	}, nil
}

func GetAgentSessionInput(request *agentsv1.GetAgentSessionRequest) (IDQueryInput, error) {
	return idQueryInput(request.GetSessionId(), request.GetMeta())
}

func ListAgentRunsInput(request *agentsv1.ListAgentRunsRequest) (service.AgentRunList, error) {
	if _, err := QueryMetaFromProto(request.GetMeta()); err != nil {
		return service.AgentRunList{}, err
	}
	sessionID, err := optionalUUIDValue(request.GetSessionId())
	if err != nil {
		return service.AgentRunList{}, err
	}
	roleID, err := optionalUUIDValue(request.GetRoleProfileId())
	if err != nil {
		return service.AgentRunList{}, err
	}
	status, err := OptionalAgentRunStatusFromProto(request.Status)
	if err != nil {
		return service.AgentRunList{}, err
	}
	return service.AgentRunList{
		SessionID:           sessionID,
		RoleProfileID:       roleID,
		Status:              status,
		ProviderWorkItemRef: strings.TrimSpace(request.GetProviderWorkItemRef()),
		Page:                pageRequestFromProto(request.GetPage()),
	}, nil
}

func stageInputs(items []*agentsv1.StageInput) ([]service.StageInput, error) {
	result := make([]service.StageInput, 0, len(items))
	for _, item := range items {
		stageType, err := StageTypeFromProto(item.GetStageType())
		if err != nil {
			return nil, err
		}
		result = append(result, service.StageInput{
			Slug:                  strings.TrimSpace(item.GetSlug()),
			StageType:             stageType,
			DisplayName:           LocalizedTextFromProto(item.GetDisplayName()),
			IconObjectURI:         strings.TrimSpace(item.GetIconObjectUri()),
			RequiredArtifactsJSON: []byte(strings.TrimSpace(item.GetRequiredArtifactsJson())),
			AcceptancePolicyJSON:  []byte(strings.TrimSpace(item.GetAcceptancePolicyJson())),
			Position:              item.GetPosition(),
		})
	}
	return result, nil
}

func stageTransitionInputs(items []*agentsv1.StageTransitionInput) []service.StageTransitionInput {
	result := make([]service.StageTransitionInput, 0, len(items))
	for position, item := range items {
		var fromStageSlug *string
		if item != nil {
			fromStageSlug = optionalPresentString(item.FromStageSlug)
		}
		result = append(result, service.StageTransitionInput{
			FromStageSlug: fromStageSlug,
			ToStageSlug:   strings.TrimSpace(item.GetToStageSlug()),
			ConditionJSON: []byte(strings.TrimSpace(item.GetConditionJson())),
			FollowUpType:  strings.TrimSpace(item.GetFollowUpType()),
			Position:      int32(position),
		})
	}
	return result
}

func stageRoleBindingInputs(items []*agentsv1.StageRoleBindingInput) ([]service.StageRoleBindingInput, error) {
	result := make([]service.StageRoleBindingInput, 0, len(items))
	for _, item := range items {
		roleID, err := requiredUUID(item.GetRoleProfileId())
		if err != nil {
			return nil, err
		}
		bindingKind, err := StageRoleBindingKindFromProto(item.GetBindingKind())
		if err != nil {
			return nil, err
		}
		result = append(result, service.StageRoleBindingInput{
			StageSlug:             strings.TrimSpace(item.GetStageSlug()),
			RoleProfileID:         roleID,
			BindingKind:           bindingKind,
			LaunchPolicyJSON:      []byte(strings.TrimSpace(item.GetLaunchPolicyJson())),
			RequiredForAcceptance: item.GetRequiredForAcceptance(),
		})
	}
	return result, nil
}

func idQueryInput(rawID string, rawMeta *agentsv1.QueryMeta) (IDQueryInput, error) {
	var result IDQueryInput
	meta, metaErr := QueryMetaFromProto(rawMeta)
	if metaErr != nil {
		return result, metaErr
	}
	id, idErr := requiredUUID(rawID)
	if idErr != nil {
		return result, idErr
	}
	result.ID = id
	result.Meta = meta
	return result, nil
}

func idCommandInput[T any](rawID string, rawMeta *agentsv1.CommandMeta, build func(uuid.UUID, value.CommandMeta) T) (T, error) {
	var zero T
	meta, err := CommandMetaFromProto(rawMeta)
	if err != nil {
		return zero, err
	}
	id, err := requiredUUID(rawID)
	if err != nil {
		return zero, err
	}
	return build(id, meta), nil
}

func newActivateFlowVersionInput(id uuid.UUID, meta value.CommandMeta) service.ActivateFlowVersionInput {
	return service.ActivateFlowVersionInput{Meta: meta, FlowVersionID: id}
}

func newActivatePromptTemplateVersionInput(id uuid.UUID, meta value.CommandMeta) service.ActivatePromptTemplateVersionInput {
	return service.ActivatePromptTemplateVersionInput{Meta: meta, PromptTemplateVersionID: id}
}

type serviceFlowStatus = enum.FlowStatus

func scopedListInput[RawStatus comparable, Status any, Result any](
	rawMeta *agentsv1.QueryMeta,
	rawScope *agentsv1.ScopeRef,
	rawStatus *RawStatus,
	rawPage *agentsv1.PageRequest,
	castStatus func(*RawStatus) (*Status, error),
	build func(value.ScopeRef, *Status, value.PageRequest) Result,
) (Result, error) {
	var zero Result
	scope, err := scopedListBase(rawMeta, rawScope)
	if err != nil {
		return zero, err
	}
	status, err := castStatus(rawStatus)
	if err != nil {
		return zero, err
	}
	return build(scope, status, pageRequestFromProto(rawPage)), nil
}

func roleProfileListInput(
	rawMeta *agentsv1.QueryMeta,
	rawScope *agentsv1.ScopeRef,
	rawKind *agentsv1.RoleKind,
	rawStatus *agentsv1.RoleStatus,
	rawPage *agentsv1.PageRequest,
) (service.RoleProfileList, error) {
	scope, err := scopedListBase(rawMeta, rawScope)
	if err != nil {
		return service.RoleProfileList{}, err
	}
	kind, err := OptionalRoleKindFromProto(rawKind)
	if err != nil {
		return service.RoleProfileList{}, err
	}
	status, err := OptionalRoleStatusFromProto(rawStatus)
	if err != nil {
		return service.RoleProfileList{}, err
	}
	return service.RoleProfileList{Scope: scope, Kind: kind, Status: status, Page: pageRequestFromProto(rawPage)}, nil
}

func scopedListBase(rawMeta *agentsv1.QueryMeta, rawScope *agentsv1.ScopeRef) (value.ScopeRef, error) {
	if _, err := QueryMetaFromProto(rawMeta); err != nil {
		return value.ScopeRef{}, err
	}
	return ScopeFromProto(rawScope)
}

func promptListBase(rawMeta *agentsv1.QueryMeta, rawRoleProfileID string, rawKind *agentsv1.PromptKind, rawPage *agentsv1.PageRequest) (uuid.UUID, *enum.PromptKind, value.PageRequest, error) {
	roleProfileID, err := roleProfileListBase(rawMeta, rawRoleProfileID)
	if err != nil {
		return uuid.Nil, nil, value.PageRequest{}, err
	}
	kind, err := OptionalPromptKindFromProto(rawKind)
	if err != nil {
		return uuid.Nil, nil, value.PageRequest{}, err
	}
	return roleProfileID, kind, pageRequestFromProto(rawPage), nil
}

func roleProfileListBase(rawMeta *agentsv1.QueryMeta, rawRoleProfileID string) (uuid.UUID, error) {
	if _, err := QueryMetaFromProto(rawMeta); err != nil {
		return uuid.Nil, err
	}
	return requiredUUID(rawRoleProfileID)
}

func optionalUUIDPtr(text string) (*uuid.UUID, error) {
	id, ok, err := parseUUID(text)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return &id, nil
}

func requiredTimeFromProto(text string) (time.Time, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return time.Time{}, errs.ErrInvalidArgument
	}
	parsed, err := time.Parse(time.RFC3339Nano, trimmed)
	if err != nil {
		return time.Time{}, errs.ErrInvalidArgument
	}
	return parsed.UTC(), nil
}

func optionalTimeFromProto(text *string) (*time.Time, error) {
	if text == nil {
		return nil, nil
	}
	parsed, err := requiredTimeFromProto(*text)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func runtimeContextInput(raw *agentsv1.RuntimeContextRef) *value.RuntimeContextRef {
	if raw == nil {
		return nil
	}
	context := RuntimeContextFromProto(raw)
	return &context
}

func providerTargetInput(raw *agentsv1.ProviderTargetRef) *value.ProviderTargetRef {
	if raw == nil {
		return nil
	}
	target := ProviderTargetFromProto(raw)
	return &target
}

func stringList(items []string) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		result = append(result, trimmed)
	}
	return result
}
