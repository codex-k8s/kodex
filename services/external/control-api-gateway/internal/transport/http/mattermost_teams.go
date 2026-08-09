package httptransport

import (
	"errors"
	"net/http"
	"strings"
	"time"

	interactiongatewayv1 "github.com/codex-k8s/matter-codex/libs/go/interactiongatewayapi/gen/interactiongateway/v1"
	generated "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (server *Server) ListMattermostTeams(writer http.ResponseWriter, request *http.Request, params generated.ListMattermostTeamsParams) {
	size, token, ok := ownerPage(writer, params.PageSize, params.PageToken)
	if !ok {
		return
	}
	response, err := server.interaction.ListMattermostTeams(request.Context(), &interactiongatewayv1.ListMattermostTeamsRequest{PageSize: size, Cursor: token})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	result := generated.MattermostTeamPage{Teams: make([]generated.MattermostTeam, 0, len(response.GetTeams()))}
	for _, item := range response.GetTeams() {
		team, convertErr := ConvertMattermostTeam(item)
		if convertErr != nil {
			server.writeInternal(writer, request.Context(), convertErr)
			return
		}
		result.Teams = append(result.Teams, team)
	}
	if next := response.GetNextCursor(); next != "" {
		result.NextPageToken = &next
	}
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) CreateMattermostTeam(writer http.ResponseWriter, request *http.Request, params generated.CreateMattermostTeamParams) {
	var body generated.CreateMattermostTeamJSONRequestBody
	if !decodeJSON(writer, request, &body) {
		return
	}
	response, err := server.interaction.CreateMattermostTeam(request.Context(), &interactiongatewayv1.CreateMattermostTeamRequest{
		DisplayName: body.DisplayName, SlugIntent: body.SlugIntent, IdempotencyKey: params.IdempotencyKey.String(),
	})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, true)
		return
	}
	result, convertErr := mattermostMutation(response.GetBinding(), response.GetMappingOperation())
	if convertErr != nil || response.GetOperation() == nil {
		server.writeInternal(writer, request.Context(), errors.New("Mattermost team create readback is invalid"))
		return
	}
	state, ok := teamOperationState(response.GetOperation().GetState())
	if !ok {
		server.writeInternal(writer, request.Context(), errors.New("Mattermost provider operation state is invalid"))
		return
	}
	result.ProviderOperationState = &state
	writeJSON(writer, http.StatusAccepted, result)
}

func (server *Server) GetMattermostTeamProviderReadback(writer http.ResponseWriter, request *http.Request, selector generated.Selector) {
	response, err := server.interaction.GetMattermostTeamProviderReadback(request.Context(), &interactiongatewayv1.GetMattermostTeamProviderReadbackRequest{Selector: string(selector)})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	team, convertErr := ConvertMattermostTeam(response.GetTeam())
	if convertErr != nil || team.Selector != string(selector) {
		server.writeInternal(writer, request.Context(), errors.New("Mattermost provider readback is invalid"))
		return
	}
	writeJSON(writer, http.StatusOK, team)
}

func (server *Server) GetMattermostTeamBinding(writer http.ResponseWriter, request *http.Request) {
	response, err := server.interaction.GetMattermostTeamBinding(request.Context(), &interactiongatewayv1.GetMattermostTeamBindingRequest{})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	binding, convertErr := convertMattermostBinding(response.GetBinding())
	if convertErr != nil {
		server.writeInternal(writer, request.Context(), convertErr)
		return
	}
	writer.Header().Set("ETag", etag(uint64(binding.MappingVersion)))
	writeJSON(writer, http.StatusOK, binding)
}

func (server *Server) LinkMattermostTeam(writer http.ResponseWriter, request *http.Request, params generated.LinkMattermostTeamParams) {
	var body generated.LinkMattermostTeamJSONRequestBody
	if !decodeJSON(writer, request, &body) {
		return
	}
	response, err := server.interaction.LinkMattermostTeam(request.Context(), &interactiongatewayv1.LinkMattermostTeamRequest{Selector: body.Selector, IdempotencyKey: params.IdempotencyKey.String()})
	server.writeMattermostMutation(writer, request, response.GetBinding(), response.GetOperation(), err)
}

func (server *Server) RelinkMattermostTeam(writer http.ResponseWriter, request *http.Request, params generated.RelinkMattermostTeamParams) {
	version, ok := requireETag(writer, params.IfMatch)
	if !ok {
		return
	}
	var body generated.RelinkMattermostTeamJSONRequestBody
	if !decodeJSON(writer, request, &body) {
		return
	}
	response, err := server.interaction.RelinkMattermostTeam(request.Context(), &interactiongatewayv1.RelinkMattermostTeamRequest{
		Selector: body.Selector, ExpectedMappingVersion: version, ExpectedMappingGeneration: uint64(body.ExpectedGeneration), IdempotencyKey: params.IdempotencyKey.String(),
	})
	server.writeMattermostMutation(writer, request, response.GetBinding(), response.GetOperation(), err)
}

func (server *Server) UnlinkMattermostTeam(writer http.ResponseWriter, request *http.Request, params generated.UnlinkMattermostTeamParams) {
	version, ok := requireETag(writer, params.IfMatch)
	if !ok {
		return
	}
	response, err := server.interaction.UnlinkMattermostTeam(request.Context(), &interactiongatewayv1.UnlinkMattermostTeamRequest{
		ExpectedMappingVersion: version, ExpectedMappingGeneration: uint64(params.Generation), IdempotencyKey: params.IdempotencyKey.String(),
	})
	server.writeMattermostMutation(writer, request, response.GetBinding(), response.GetOperation(), err)
}

func (server *Server) GetMattermostTeamMappingOperation(writer http.ResponseWriter, request *http.Request, params generated.GetMattermostTeamMappingOperationParams) {
	action := map[string]interactiongatewayv1.WorkspaceMattermostMappingAction{
		"BIND":   interactiongatewayv1.WorkspaceMattermostMappingAction_WORKSPACE_MATTERMOST_MAPPING_ACTION_BIND,
		"RELINK": interactiongatewayv1.WorkspaceMattermostMappingAction_WORKSPACE_MATTERMOST_MAPPING_ACTION_RELINK,
		"UNLINK": interactiongatewayv1.WorkspaceMattermostMappingAction_WORKSPACE_MATTERMOST_MAPPING_ACTION_UNLINK,
	}[string(params.Action)]
	if action == interactiongatewayv1.WorkspaceMattermostMappingAction_WORKSPACE_MATTERMOST_MAPPING_ACTION_UNSPECIFIED {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	response, err := server.interaction.GetMattermostTeamMappingOperation(request.Context(), &interactiongatewayv1.GetMattermostTeamMappingOperationRequest{Action: action, IdempotencyKey: params.IdempotencyKey.String()})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	operation, convertErr := convertMattermostOperation(response.GetOperation())
	if convertErr != nil {
		server.writeInternal(writer, request.Context(), convertErr)
		return
	}
	writeJSON(writer, http.StatusOK, operation)
}

func (server *Server) writeMattermostMutation(writer http.ResponseWriter, request *http.Request, binding *interactiongatewayv1.WorkspaceMattermostTeamBindingView, operation *interactiongatewayv1.WorkspaceMattermostMappingOperationView, err error) {
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, true)
		return
	}
	result, convertErr := mattermostMutation(binding, operation)
	if convertErr != nil {
		server.writeInternal(writer, request.Context(), convertErr)
		return
	}
	if result.Binding != nil {
		writer.Header().Set("ETag", etag(uint64(result.Binding.MappingVersion)))
	}
	writeJSON(writer, http.StatusOK, result)
}

func mattermostMutation(binding *interactiongatewayv1.WorkspaceMattermostTeamBindingView, operation *interactiongatewayv1.WorkspaceMattermostMappingOperationView) (generated.MattermostTeamMutationResult, error) {
	convertedOperation, err := convertMattermostOperation(operation)
	if err != nil {
		return generated.MattermostTeamMutationResult{}, err
	}
	result := generated.MattermostTeamMutationResult{Operation: convertedOperation}
	if binding != nil {
		convertedBinding, bindingErr := convertMattermostBinding(binding)
		if bindingErr != nil {
			return generated.MattermostTeamMutationResult{}, bindingErr
		}
		result.Binding = &convertedBinding
	}
	return result, nil
}

// ConvertMattermostTeam возвращает только bounded owner-safe provider readback.
func ConvertMattermostTeam(input *interactiongatewayv1.MattermostTeamView) (generated.MattermostTeam, error) {
	if input == nil || input.GetSelector() == "" || input.GetDisplayName() == "" || input.GetSlug() == "" || !validSHA256(input.GetProviderSnapshotSha256()) {
		return generated.MattermostTeam{}, errors.New("Mattermost team projection is incomplete")
	}
	status, ok := mapMattermostTeamStatus(input.GetStatus())
	created, createdErr := requiredTimestamp(input.GetCreatedAt())
	updated, updatedErr := requiredTimestamp(input.GetUpdatedAt())
	observed, observedErr := requiredTimestamp(input.GetObservedAt())
	if !ok || createdErr != nil || updatedErr != nil || observedErr != nil {
		return generated.MattermostTeam{}, errors.New("Mattermost team projection values are invalid")
	}
	return generated.MattermostTeam{Selector: input.GetSelector(), DisplayName: input.GetDisplayName(), Slug: input.GetSlug(), Status: status,
		ProviderSnapshotSha256: generated.Sha256(strings.ToLower(input.GetProviderSnapshotSha256())), CreatedAt: created, UpdatedAt: updated, ObservedAt: observed}, nil
}

func convertMattermostBinding(input *interactiongatewayv1.WorkspaceMattermostTeamBindingView) (generated.MattermostTeamBinding, error) {
	if input == nil || input.GetMappingRef() == "" || input.GetMappingVersion() == 0 || input.GetMappingGeneration() == 0 || input.GetProviderEffectVersion() == 0 || input.GetProviderEffectGeneration() == 0 {
		return generated.MattermostTeamBinding{}, errors.New("Mattermost binding projection is incomplete")
	}
	team, err := ConvertMattermostTeam(input.GetTeam())
	state, ok := mapMattermostBindingState(input.GetState())
	observed, observedErr := requiredTimestamp(input.GetProviderObservedAt())
	updated, updatedErr := requiredTimestamp(input.GetUpdatedAt())
	if err != nil || !ok || observedErr != nil || updatedErr != nil {
		return generated.MattermostTeamBinding{}, errors.New("Mattermost binding projection values are invalid")
	}
	return generated.MattermostTeamBinding{MappingRef: input.GetMappingRef(), MappingVersion: int64(input.GetMappingVersion()), MappingGeneration: int64(input.GetMappingGeneration()), State: state, Team: team,
		ProviderEffectVersion: int64(input.GetProviderEffectVersion()), ProviderEffectGeneration: int64(input.GetProviderEffectGeneration()), ProviderObservedAt: observed, UpdatedAt: updated}, nil
}

func convertMattermostOperation(input *interactiongatewayv1.WorkspaceMattermostMappingOperationView) (generated.MattermostMappingOperation, error) {
	if input == nil || input.GetOperationId() == "" {
		return generated.MattermostMappingOperation{}, errors.New("Mattermost mapping operation is missing")
	}
	action, actionOK := mapMattermostAction(input.GetAction())
	state, stateOK := mapMattermostOperationState(input.GetState())
	created, createdErr := requiredTimestamp(input.GetCreatedAt())
	updated, updatedErr := requiredTimestamp(input.GetUpdatedAt())
	if !actionOK || !stateOK || createdErr != nil || updatedErr != nil {
		return generated.MattermostMappingOperation{}, errors.New("Mattermost mapping operation values are invalid")
	}
	result := generated.MattermostMappingOperation{OperationRef: input.GetOperationId(), Action: action, State: state, CreatedAt: created, UpdatedAt: updated,
		FailureCode: optionalString(input.GetFailureCode())}
	if input.GetRetryNotBefore() != nil {
		value, err := requiredTimestamp(input.GetRetryNotBefore())
		if err != nil {
			return generated.MattermostMappingOperation{}, err
		}
		result.RetryNotBefore = &value
	}
	if input.GetRecoveryDeadline() != nil {
		value, err := requiredTimestamp(input.GetRecoveryDeadline())
		if err != nil {
			return generated.MattermostMappingOperation{}, err
		}
		result.RecoveryDeadline = &value
	}
	if input.GetResult() != nil {
		value, err := convertMattermostBinding(input.GetResult())
		if err != nil {
			return generated.MattermostMappingOperation{}, err
		}
		result.Result = &value
	}
	return result, nil
}

func requiredTimestamp(value *timestamppb.Timestamp) (time.Time, error) {
	if value == nil || value.CheckValid() != nil {
		return time.Time{}, errors.New("required timestamp is invalid")
	}
	return value.AsTime(), nil
}

func mapMattermostTeamStatus(value interactiongatewayv1.MattermostTeamStatus) (generated.MattermostTeamStatus, bool) {
	switch value {
	case interactiongatewayv1.MattermostTeamStatus_MATTERMOST_TEAM_STATUS_ACTIVE:
		return "ACTIVE", true
	case interactiongatewayv1.MattermostTeamStatus_MATTERMOST_TEAM_STATUS_DELETED:
		return "DELETED", true
	default:
		return "", false
	}
}

func teamOperationState(value interactiongatewayv1.MattermostTeamOperationState) (generated.MattermostTeamMutationResultProviderOperationState, bool) {
	switch value {
	case interactiongatewayv1.MattermostTeamOperationState_MATTERMOST_TEAM_OPERATION_STATE_PENDING:
		return "PENDING", true
	case interactiongatewayv1.MattermostTeamOperationState_MATTERMOST_TEAM_OPERATION_STATE_AMBIGUOUS:
		return "AMBIGUOUS", true
	case interactiongatewayv1.MattermostTeamOperationState_MATTERMOST_TEAM_OPERATION_STATE_PROVIDER_ACCEPTED:
		return "PROVIDER_ACCEPTED", true
	case interactiongatewayv1.MattermostTeamOperationState_MATTERMOST_TEAM_OPERATION_STATE_REPAIR_REQUIRED:
		return "REPAIR_REQUIRED", true
	case interactiongatewayv1.MattermostTeamOperationState_MATTERMOST_TEAM_OPERATION_STATE_EFFECT_PENDING:
		return "EFFECT_PENDING", true
	default:
		return "", false
	}
}

func mapMattermostBindingState(value interactiongatewayv1.WorkspaceMattermostMappingState) (generated.MattermostTeamBindingState, bool) {
	switch value {
	case interactiongatewayv1.WorkspaceMattermostMappingState_WORKSPACE_MATTERMOST_MAPPING_STATE_BOUND:
		return "BOUND", true
	case interactiongatewayv1.WorkspaceMattermostMappingState_WORKSPACE_MATTERMOST_MAPPING_STATE_UNLINKED:
		return "UNLINKED", true
	default:
		return "", false
	}
}

func mapMattermostAction(value interactiongatewayv1.WorkspaceMattermostMappingAction) (generated.MattermostMappingOperationAction, bool) {
	switch value {
	case interactiongatewayv1.WorkspaceMattermostMappingAction_WORKSPACE_MATTERMOST_MAPPING_ACTION_BIND:
		return "BIND", true
	case interactiongatewayv1.WorkspaceMattermostMappingAction_WORKSPACE_MATTERMOST_MAPPING_ACTION_RELINK:
		return "RELINK", true
	case interactiongatewayv1.WorkspaceMattermostMappingAction_WORKSPACE_MATTERMOST_MAPPING_ACTION_UNLINK:
		return "UNLINK", true
	default:
		return "", false
	}
}

func mapMattermostOperationState(value interactiongatewayv1.WorkspaceMattermostMappingOperationState) (generated.MattermostMappingOperationState, bool) {
	switch value {
	case interactiongatewayv1.WorkspaceMattermostMappingOperationState_WORKSPACE_MATTERMOST_MAPPING_OPERATION_STATE_PENDING:
		return "PENDING", true
	case interactiongatewayv1.WorkspaceMattermostMappingOperationState_WORKSPACE_MATTERMOST_MAPPING_OPERATION_STATE_AMBIGUOUS:
		return "AMBIGUOUS", true
	case interactiongatewayv1.WorkspaceMattermostMappingOperationState_WORKSPACE_MATTERMOST_MAPPING_OPERATION_STATE_BOUND:
		return "BOUND", true
	case interactiongatewayv1.WorkspaceMattermostMappingOperationState_WORKSPACE_MATTERMOST_MAPPING_OPERATION_STATE_UNLINKED:
		return "UNLINKED", true
	case interactiongatewayv1.WorkspaceMattermostMappingOperationState_WORKSPACE_MATTERMOST_MAPPING_OPERATION_STATE_REPAIR_REQUIRED:
		return "REPAIR_REQUIRED", true
	default:
		return "", false
	}
}
