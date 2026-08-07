package casters

import (
	"time"

	interactiongatewayv1 "github.com/codex-k8s/matter-codex/libs/go/interactiongatewayapi/gen/interactiongateway/v1"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/enum"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func ListResponse(teams []entity.MattermostTeam, nextCursor string) *interactiongatewayv1.ListMattermostTeamsResponse {
	response := &interactiongatewayv1.ListMattermostTeamsResponse{
		NextCursor: nextCursor,
		Teams:      make([]*interactiongatewayv1.MattermostTeamView, 0, len(teams)),
	}
	for _, team := range teams {
		response.Teams = append(response.Teams, TeamView(team))
	}
	return response
}

func CreateResponse(operation entity.MattermostTeamOperation, binding entity.WorkspaceMattermostBinding) *interactiongatewayv1.CreateMattermostTeamResponse {
	return &interactiongatewayv1.CreateMattermostTeamResponse{Operation: OperationView(operation), Binding: BindingView(binding),
		MappingOperation: MappingOperationView(binding.Operation)}
}

func MappingOperationView(operation entity.WorkspaceMappingOperation) *interactiongatewayv1.WorkspaceMattermostMappingOperationView {
	if operation.ID == "" {
		return nil
	}
	action := interactiongatewayv1.WorkspaceMattermostMappingAction_WORKSPACE_MATTERMOST_MAPPING_ACTION_UNSPECIFIED
	switch operation.Action {
	case "bind":
		action = interactiongatewayv1.WorkspaceMattermostMappingAction_WORKSPACE_MATTERMOST_MAPPING_ACTION_BIND
	case "relink":
		action = interactiongatewayv1.WorkspaceMattermostMappingAction_WORKSPACE_MATTERMOST_MAPPING_ACTION_RELINK
	case "unlink":
		action = interactiongatewayv1.WorkspaceMattermostMappingAction_WORKSPACE_MATTERMOST_MAPPING_ACTION_UNLINK
	}
	state := interactiongatewayv1.WorkspaceMattermostMappingOperationState_WORKSPACE_MATTERMOST_MAPPING_OPERATION_STATE_UNSPECIFIED
	switch operation.State {
	case enum.WorkspaceMappingOperationPending:
		state = interactiongatewayv1.WorkspaceMattermostMappingOperationState_WORKSPACE_MATTERMOST_MAPPING_OPERATION_STATE_PENDING
	case enum.WorkspaceMappingOperationAmbiguous:
		state = interactiongatewayv1.WorkspaceMattermostMappingOperationState_WORKSPACE_MATTERMOST_MAPPING_OPERATION_STATE_AMBIGUOUS
	case enum.WorkspaceMappingOperationBound:
		state = interactiongatewayv1.WorkspaceMattermostMappingOperationState_WORKSPACE_MATTERMOST_MAPPING_OPERATION_STATE_BOUND
	case enum.WorkspaceMappingOperationUnlinked:
		state = interactiongatewayv1.WorkspaceMattermostMappingOperationState_WORKSPACE_MATTERMOST_MAPPING_OPERATION_STATE_UNLINKED
	case enum.WorkspaceMappingOperationRepairRequired:
		state = interactiongatewayv1.WorkspaceMattermostMappingOperationState_WORKSPACE_MATTERMOST_MAPPING_OPERATION_STATE_REPAIR_REQUIRED
	}
	result := &interactiongatewayv1.WorkspaceMattermostMappingOperationView{
		OperationId: operation.ID, Action: action, State: state, FailureCode: safeFailureCode(operation.FailureCode),
		RetryNotBefore: timestamp(operation.RetryNotBefore), RecoveryDeadline: timestamp(operation.RecoveryDeadline),
		CreatedAt: timestamp(operation.CreatedAt), UpdatedAt: timestamp(operation.UpdatedAt),
	}
	if operation.Result.ID != "" {
		result.Result = BindingView(entity.WorkspaceMattermostBinding{Mapping: operation.Result})
		result.Result.Team = nil
	}
	return result
}

func MappingOperationResponse(operation entity.WorkspaceMappingOperation) *interactiongatewayv1.GetMattermostTeamMappingOperationResponse {
	return &interactiongatewayv1.GetMattermostTeamMappingOperationResponse{Operation: MappingOperationView(operation)}
}

func safeFailureCode(code string) string {
	switch code {
	case "", "OWNER_OUTCOME_UNKNOWN", "OWNER_STATE_CONFLICT", "OWNER_READBACK_MISMATCH",
		"PROVIDER_ELIGIBILITY_LOST", "PROVIDER_ROUTE_UNAVAILABLE", "RECOVERY_TIMEOUT":
		return code
	default:
		return "SAFE_FAILURE"
	}
}

func BindingView(binding entity.WorkspaceMattermostBinding) *interactiongatewayv1.WorkspaceMattermostTeamBindingView {
	state := interactiongatewayv1.WorkspaceMattermostMappingState_WORKSPACE_MATTERMOST_MAPPING_STATE_UNSPECIFIED
	switch binding.Mapping.State {
	case "BOUND":
		state = interactiongatewayv1.WorkspaceMattermostMappingState_WORKSPACE_MATTERMOST_MAPPING_STATE_BOUND
	case "UNLINKED":
		state = interactiongatewayv1.WorkspaceMattermostMappingState_WORKSPACE_MATTERMOST_MAPPING_STATE_UNLINKED
	}
	return &interactiongatewayv1.WorkspaceMattermostTeamBindingView{
		MappingRef: binding.Mapping.ID, MappingVersion: binding.Mapping.Version,
		MappingGeneration: binding.Mapping.Generation, State: state, Team: TeamView(binding.Team),
		ProviderEffectVersion:    binding.Mapping.ProviderEffectVersion,
		ProviderEffectGeneration: binding.Mapping.ProviderEffectGeneration,
		ProviderObservedAt:       timestamp(binding.Mapping.ProviderObservedAt), UpdatedAt: timestamp(binding.Mapping.UpdatedAt),
	}
}

func ProviderReadbackResponse(team entity.MattermostTeam) *interactiongatewayv1.GetMattermostTeamProviderReadbackResponse {
	return &interactiongatewayv1.GetMattermostTeamProviderReadbackResponse{Team: TeamView(team)}
}

func TeamView(team entity.MattermostTeam) *interactiongatewayv1.MattermostTeamView {
	statusValue := interactiongatewayv1.MattermostTeamStatus_MATTERMOST_TEAM_STATUS_UNSPECIFIED
	switch team.Status {
	case enum.MattermostTeamActive:
		statusValue = interactiongatewayv1.MattermostTeamStatus_MATTERMOST_TEAM_STATUS_ACTIVE
	case enum.MattermostTeamDeleted:
		statusValue = interactiongatewayv1.MattermostTeamStatus_MATTERMOST_TEAM_STATUS_DELETED
	}
	return &interactiongatewayv1.MattermostTeamView{
		Selector: team.Selector, DisplayName: team.DisplayName, Slug: team.Slug, Status: statusValue,
		ProviderSnapshotSha256: team.ProviderSnapshotSHA256, CreatedAt: timestamp(team.CreatedAt),
		UpdatedAt: timestamp(team.UpdatedAt), ObservedAt: timestamp(team.ObservedAt),
	}
}

func OperationView(operation entity.MattermostTeamOperation) *interactiongatewayv1.MattermostTeamOperationView {
	state := interactiongatewayv1.MattermostTeamOperationState_MATTERMOST_TEAM_OPERATION_STATE_UNSPECIFIED
	switch operation.State {
	case enum.TeamOperationPending:
		state = interactiongatewayv1.MattermostTeamOperationState_MATTERMOST_TEAM_OPERATION_STATE_PENDING
	case enum.TeamOperationEffectPending:
		state = interactiongatewayv1.MattermostTeamOperationState_MATTERMOST_TEAM_OPERATION_STATE_EFFECT_PENDING
	case enum.TeamOperationAmbiguous:
		state = interactiongatewayv1.MattermostTeamOperationState_MATTERMOST_TEAM_OPERATION_STATE_AMBIGUOUS
	case enum.TeamOperationProviderAccepted:
		state = interactiongatewayv1.MattermostTeamOperationState_MATTERMOST_TEAM_OPERATION_STATE_PROVIDER_ACCEPTED
	case enum.TeamOperationRepairRequired:
		state = interactiongatewayv1.MattermostTeamOperationState_MATTERMOST_TEAM_OPERATION_STATE_REPAIR_REQUIRED
	}
	result := &interactiongatewayv1.MattermostTeamOperationView{
		OperationId: operation.ID, State: state, RequestSha256: operation.Intent.RequestSHA256,
		ProviderReceiptSha256: operation.ProviderReceiptSHA256, ProviderGeneration: operation.ProviderGeneration,
		FailureCode: operation.FailureCode, RetryNotBefore: timestamp(operation.RetryNotBefore),
		CreatedAt: timestamp(operation.CreatedAt), UpdatedAt: timestamp(operation.UpdatedAt),
	}
	if operation.Team.Selector != "" {
		result.Team = TeamView(operation.Team)
	}
	return result
}

func timestamp(value time.Time) *timestamppb.Timestamp {
	if value.IsZero() {
		return nil
	}
	return timestamppb.New(value)
}
