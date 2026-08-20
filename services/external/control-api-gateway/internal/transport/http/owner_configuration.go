package httptransport

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/transport/http/generated"
)

func (server *Server) ListRoleDefinitions(writer http.ResponseWriter, request *http.Request, params generated.ListRoleDefinitionsParams) {
	size, token, ok := ownerPage(writer, params.PageSize, params.PageToken)
	if !ok {
		return
	}
	response, err := server.control.ListRoleDefinitions(request.Context(), &controlplanev1.ListRoleDefinitionsRequest{PageSize: size, PageToken: token})
	server.writeResourcePage(writer, request, response.GetRoleDefinitions(), response.GetNextPageToken(), err)
}

func (server *Server) GetRoleDefinition(writer http.ResponseWriter, request *http.Request, resourceRef generated.ResourceRef) {
	response, err := server.control.GetRoleDefinition(request.Context(), &controlplanev1.GetRoleDefinitionRequest{RoleDefinitionId: string(resourceRef)})
	server.writeResourceResponse(writer, request.Context(), http.StatusOK, response.GetRoleDefinition(), err, false)
}

func (server *Server) ListRoleDefinitionHistory(writer http.ResponseWriter, request *http.Request, resourceRef generated.ResourceRef, params generated.ListRoleDefinitionHistoryParams) {
	size, token, ok := ownerPage(writer, params.PageSize, params.PageToken)
	if !ok {
		return
	}
	response, err := server.control.ListRoleDefinitionHistory(request.Context(), &controlplanev1.ListRoleDefinitionHistoryRequest{RoleDefinitionId: string(resourceRef), PageSize: size, PageToken: token})
	server.writeHistoryPage(writer, request, response.GetEntries(), response.GetNextPageToken(), err)
}

func (server *Server) ManageRoleDefinition(writer http.ResponseWriter, request *http.Request, params generated.ManageRoleDefinitionParams) {
	var body generated.ManageRoleDefinitionJSONRequestBody
	if !decodeJSON(writer, request, &body) {
		return
	}
	action := protectedAction(string(body.Action))
	version, ok := commandVersion(writer, action == controlplanev1.ProtectedConfigurationAction_PROTECTED_CONFIGURATION_ACTION_CREATE, params.IfMatch)
	if !ok || action == controlplanev1.ProtectedConfigurationAction_PROTECTED_CONFIGURATION_ACTION_UNSPECIFIED || !validateRoleDefinitionCommand(body, action) {
		if ok {
			writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		}
		return
	}
	response, err := server.control.ManageRoleDefinition(request.Context(), &controlplanev1.ManageRoleDefinitionRequest{
		IdempotencyKey: params.IdempotencyKey.String(), Action: action, RoleDefinitionId: stringValue(body.ResourceRef), ExpectedVersion: version,
		Name: stringValue(body.Name), Spec: &controlplanev1.RoleDefinitionSpec{
			StableKey: stringValue(body.StableKey), Description: stringValue(body.Description), Capabilities: sliceValue(body.Capabilities),
			AllowedTargetRoleDefinitionIds: sliceValue(body.AllowedTargetRoleDefinitionRefs), RoleImageRecipeId: stringValue(body.RoleImageRecipeRef),
			RoleImageRecipeVersion: uint64Value(body.RoleImageRecipeVersion), RoleImageRecipeSha256: shaValue(body.RoleImageRecipeSha256), Ownership: uiOwnership(),
		},
	})
	statusCode := http.StatusOK
	if action == controlplanev1.ProtectedConfigurationAction_PROTECTED_CONFIGURATION_ACTION_CREATE {
		statusCode = http.StatusCreated
	}
	server.writeResourceResponse(writer, request.Context(), statusCode, response.GetRoleDefinition(), err, true)
}

func (server *Server) ListAgents(writer http.ResponseWriter, request *http.Request, params generated.ListAgentsParams) {
	size, token, ok := ownerPage(writer, params.PageSize, params.PageToken)
	if !ok {
		return
	}
	response, err := server.control.ListAgents(request.Context(), &controlplanev1.ListAgentsRequest{PageSize: size, PageToken: token})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	result := generated.AgentPage{Agents: make([]generated.AgentView, 0, len(response.GetProjections()))}
	for _, item := range response.GetProjections() {
		value, castErr := castAgentView(item)
		if castErr != nil {
			server.writeInternal(writer, request.Context(), castErr)
			return
		}
		result.Agents = append(result.Agents, value)
	}
	if len(response.GetAgents()) != 0 {
		server.writeInternal(writer, request.Context(), errors.New("agent projection page contains legacy resources"))
		return
	}
	result.NextPageToken = optionalString(response.GetNextPageToken())
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) GetAgent(writer http.ResponseWriter, request *http.Request, resourceRef generated.ResourceRef) {
	response, err := server.control.GetAgent(request.Context(), &controlplanev1.GetAgentRequest{AgentId: string(resourceRef)})
	server.writeAgentView(writer, request, http.StatusOK, string(resourceRef), response.GetProjection(), err, false)
}

func (server *Server) ListAgentHistory(writer http.ResponseWriter, request *http.Request, resourceRef generated.ResourceRef, params generated.ListAgentHistoryParams) {
	size, token, ok := ownerPage(writer, params.PageSize, params.PageToken)
	if !ok {
		return
	}
	response, err := server.control.ListAgentHistory(request.Context(), &controlplanev1.ListAgentHistoryRequest{AgentId: string(resourceRef), PageSize: size, PageToken: token})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	if len(response.GetEntries()) != 0 {
		server.writeInternal(writer, request.Context(), errors.New("agent history projection contains legacy resources"))
		return
	}
	result := generated.AgentHistoryPage{Entries: make([]generated.AgentHistoryEntry, 0, len(response.GetProjections()))}
	for _, item := range response.GetProjections() {
		agent, castErr := castAgentView(item.GetProjection())
		action, actionErr := castClosedEnum(item.GetAction().String(), "AGENT_HISTORY_ACTION_", generated.AgentHistoryAction.Valid)
		occurred, timeErr := requiredTimestamp(item.GetOccurredAt())
		if castErr != nil || actionErr != nil || timeErr != nil || !validSHA256(item.GetSnapshotSha256()) || agent.AgentRef != string(resourceRef) {
			server.writeInternal(writer, request.Context(), errors.New("agent history entry is invalid"))
			return
		}
		result.Entries = append(result.Entries, generated.AgentHistoryEntry{Agent: agent, Action: action, SnapshotSha256: generated.Sha256(strings.ToLower(item.GetSnapshotSha256())), OccurredAt: occurred})
	}
	result.NextPageToken = optionalString(response.GetNextPageToken())
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) ManageAgent(writer http.ResponseWriter, request *http.Request, params generated.ManageAgentParams) {
	var body generated.ManageAgentJSONRequestBody
	if !decodeJSON(writer, request, &body) {
		return
	}
	action := protectedAction(string(body.Action))
	version, ok := commandVersion(writer, action == controlplanev1.ProtectedConfigurationAction_PROTECTED_CONFIGURATION_ACTION_CREATE, params.IfMatch)
	if !ok || action == controlplanev1.ProtectedConfigurationAction_PROTECTED_CONFIGURATION_ACTION_UNSPECIFIED || !validateAgentCommand(body, action) {
		if ok {
			writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		}
		return
	}
	response, err := server.control.ManageAgent(request.Context(), &controlplanev1.ManageAgentRequest{
		IdempotencyKey: params.IdempotencyKey.String(), Action: action, AgentId: stringValue(body.ResourceRef), ExpectedVersion: version,
		Name: stringValue(body.Name), RoleDefinitionStableKey: stringValue(body.RuntimeSelectionKey),
		InstructionSetStableKey: stringValue(body.InstructionSetStableKey), ProviderPoolStableKey: stringValue(body.ProviderPoolStableKey),
		Spec: &controlplanev1.AgentSpec{StableKey: stringValue(body.StableKey), Capabilities: sliceValue(body.Capabilities), Enabled: boolValue(body.Enabled), Ownership: uiOwnership()},
	})
	statusCode := http.StatusOK
	if action == controlplanev1.ProtectedConfigurationAction_PROTECTED_CONFIGURATION_ACTION_CREATE {
		statusCode = http.StatusCreated
	}
	server.writeAgentView(writer, request, statusCode, stringValue(body.ResourceRef), response.GetProjection(), err, true)
}

func (server *Server) writeAgentView(writer http.ResponseWriter, request *http.Request, status int, expected string, input *controlplanev1.AgentOwnerProjection, err error, mutation bool) {
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, mutation)
		return
	}
	value, castErr := castAgentView(input)
	if castErr != nil || (expected != "" && value.AgentRef != expected) {
		server.writeInternal(writer, request.Context(), errors.New("agent projection readback is invalid"))
		return
	}
	writer.Header().Set("ETag", etag(uint64(value.Version)))
	writeJSON(writer, status, value)
}

func (server *Server) GetOwnerConfigurationCatalog(writer http.ResponseWriter, request *http.Request, params generated.GetOwnerConfigurationCatalogParams) {
	size, token, ok := ownerPage(writer, params.PageSize, params.PageToken)
	if !ok {
		return
	}
	response, err := server.control.GetOwnerConfigurationCatalog(request.Context(), &controlplanev1.GetOwnerConfigurationCatalogRequest{PageSize: size, PageToken: token})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	defaults, castErr := castScheduleDefaults(response.GetScheduleDefaults())
	if castErr != nil {
		server.writeInternal(writer, request.Context(), castErr)
		return
	}
	result := generated.OwnerConfigurationCatalog{RuntimeSelections: make([]generated.RuntimeSelectionCatalogEntry, 0, len(response.GetRuntimeSelections())), SchedulePresets: make([]generated.SchedulePreset, 0, len(response.GetSchedulePresets())), ScheduleDefaults: defaults}
	for _, item := range response.GetRuntimeSelections() {
		value, itemErr := castRuntimeSelectionCatalogEntry(item)
		if itemErr != nil {
			server.writeInternal(writer, request.Context(), itemErr)
			return
		}
		result.RuntimeSelections = append(result.RuntimeSelections, value)
	}
	for _, item := range response.GetSchedulePresets() {
		value, itemErr := castSchedulePreset(item)
		if itemErr != nil {
			server.writeInternal(writer, request.Context(), itemErr)
			return
		}
		result.SchedulePresets = append(result.SchedulePresets, value)
	}
	result.NextPageToken = optionalString(response.GetNextPageToken())
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) ListAgentAssignments(writer http.ResponseWriter, request *http.Request, params generated.ListAgentAssignmentsParams) {
	size, token, ok := ownerPage(writer, params.PageSize, params.PageToken)
	if !ok {
		return
	}
	response, err := server.control.ListAgentAssignments(request.Context(), &controlplanev1.ListAgentAssignmentsRequest{AgentId: stringValue(params.AgentRef), PageSize: size, PageToken: token})
	server.writeResourcePage(writer, request, response.GetAssignments(), response.GetNextPageToken(), err)
}

func (server *Server) GetAgentAssignment(writer http.ResponseWriter, request *http.Request, resourceRef generated.ResourceRef) {
	response, err := server.control.GetAgentAssignment(request.Context(), &controlplanev1.GetAgentAssignmentRequest{AssignmentId: string(resourceRef)})
	server.writeResourceResponse(writer, request.Context(), http.StatusOK, response.GetAssignment(), err, false)
}

func (server *Server) ListAgentAssignmentHistory(writer http.ResponseWriter, request *http.Request, resourceRef generated.ResourceRef, params generated.ListAgentAssignmentHistoryParams) {
	size, token, ok := ownerPage(writer, params.PageSize, params.PageToken)
	if !ok {
		return
	}
	response, err := server.control.ListAgentAssignmentHistory(request.Context(), &controlplanev1.ListAgentAssignmentHistoryRequest{AssignmentId: string(resourceRef), PageSize: size, PageToken: token})
	server.writeHistoryPage(writer, request, response.GetEntries(), response.GetNextPageToken(), err)
}

func (server *Server) ManageAgentAssignment(writer http.ResponseWriter, request *http.Request, params generated.ManageAgentAssignmentParams) {
	var body generated.ManageAgentAssignmentJSONRequestBody
	if !decodeJSON(writer, request, &body) {
		return
	}
	action := map[string]controlplanev1.AgentAssignmentAction{
		"ASSIGN":   controlplanev1.AgentAssignmentAction_AGENT_ASSIGNMENT_ACTION_ASSIGN,
		"UNASSIGN": controlplanev1.AgentAssignmentAction_AGENT_ASSIGNMENT_ACTION_UNASSIGN,
	}[string(body.Action)]
	version, ok := commandVersion(writer, action == controlplanev1.AgentAssignmentAction_AGENT_ASSIGNMENT_ACTION_ASSIGN, params.IfMatch)
	if !ok || action == controlplanev1.AgentAssignmentAction_AGENT_ASSIGNMENT_ACTION_UNSPECIFIED ||
		(action == controlplanev1.AgentAssignmentAction_AGENT_ASSIGNMENT_ACTION_ASSIGN &&
			(stringValue(body.Name) == "" || stringValue(body.AgentStableKey) == "")) ||
		(action == controlplanev1.AgentAssignmentAction_AGENT_ASSIGNMENT_ACTION_UNASSIGN && stringValue(body.ResourceRef) == "") {
		if ok {
			writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		}
		return
	}
	response, err := server.control.ManageAgentAssignment(request.Context(), &controlplanev1.ManageAgentAssignmentRequest{
		IdempotencyKey: params.IdempotencyKey.String(), Action: action, AssignmentId: stringValue(body.ResourceRef), ExpectedVersion: version,
		Name: stringValue(body.Name), AgentStableKey: stringValue(body.AgentStableKey), RoomStableKey: stringValue(body.RoomStableKey),
	})
	statusCode := http.StatusOK
	if action == controlplanev1.AgentAssignmentAction_AGENT_ASSIGNMENT_ACTION_ASSIGN {
		statusCode = http.StatusCreated
	}
	server.writeResourceResponse(writer, request.Context(), statusCode, response.GetAssignment(), err, true)
}

func (server *Server) ListInstructionSets(writer http.ResponseWriter, request *http.Request, params generated.ListInstructionSetsParams) {
	size, token, ok := ownerPage(writer, params.PageSize, params.PageToken)
	if !ok {
		return
	}
	response, err := server.control.ListInstructionSets(request.Context(), &controlplanev1.ListInstructionSetsRequest{PageSize: size, PageToken: token})
	server.writeResourcePage(writer, request, response.GetInstructionSets(), response.GetNextPageToken(), err)
}

func (server *Server) GetInstructionSet(writer http.ResponseWriter, request *http.Request, resourceRef generated.ResourceRef) {
	response, err := server.control.GetInstructionSet(request.Context(), &controlplanev1.GetInstructionSetRequest{InstructionSetId: string(resourceRef)})
	server.writeResourceResponse(writer, request.Context(), http.StatusOK, response.GetInstructionSet(), err, false)
}

func (server *Server) ListInstructionSetHistory(writer http.ResponseWriter, request *http.Request, resourceRef generated.ResourceRef, params generated.ListInstructionSetHistoryParams) {
	size, token, ok := ownerPage(writer, params.PageSize, params.PageToken)
	if !ok {
		return
	}
	response, err := server.control.ListInstructionSetHistory(request.Context(), &controlplanev1.ListInstructionSetHistoryRequest{InstructionSetId: string(resourceRef), PageSize: size, PageToken: token})
	server.writeHistoryPage(writer, request, response.GetEntries(), response.GetNextPageToken(), err)
}

func (server *Server) CompareInstructionSetVersions(writer http.ResponseWriter, request *http.Request, resourceRef generated.ResourceRef, params generated.CompareInstructionSetVersionsParams) {
	response, err := server.control.CompareInstructionSetVersions(request.Context(), &controlplanev1.CompareInstructionSetVersionsRequest{InstructionSetId: string(resourceRef), LeftVersion: uint64(params.LeftVersion), RightVersion: uint64(params.RightVersion)})
	server.writeInstructionComparison(writer, request, response, err)
}

func (server *Server) GetConfigurationDiff(writer http.ResponseWriter, request *http.Request, params generated.GetConfigurationDiffParams) {
	size, token, ok := ownerPage(writer, params.PageSize, params.PageToken)
	if !ok {
		return
	}
	response, err := server.control.CompareInstructionSetVersions(request.Context(), &controlplanev1.CompareInstructionSetVersionsRequest{InstructionSetId: params.InstructionSetRef, LeftVersion: uint64(params.LeftVersion), RightVersion: uint64(params.RightVersion), PageSize: size, PageToken: token})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	left, leftErr := configurationVersionRef(response.GetLeftVersionRef())
	right, rightErr := configurationVersionRef(response.GetRightVersionRef())
	if leftErr != nil || rightErr != nil || response.GetTruncated() != (response.GetNextPageToken() != "") || len(response.GetChanges()) > int(size) {
		server.writeInternal(writer, request.Context(), errors.New("configuration diff readback is invalid"))
		return
	}
	result := generated.ConfigurationDiff{Left: left, Right: right, Changes: make([]generated.ConfigurationDiffChange, 0, len(response.GetChanges())), Truncated: response.GetTruncated(), NextPageToken: optionalString(response.GetNextPageToken())}
	for _, item := range response.GetChanges() {
		if item == nil || item.GetPath() == "" || len(item.GetPath()) > 512 || len(item.GetBefore()) > 4000 || len(item.GetAfter()) > 4000 {
			server.writeInternal(writer, request.Context(), errors.New("configuration diff change is invalid"))
			return
		}
		kind, kindErr := castClosedEnum(item.GetKind().String(), "CONFIGURATION_CHANGE_KIND_", generated.ConfigurationChangeKind.Valid)
		display, displayErr := castClosedEnum(item.GetDisplay().String(), "CONFIGURATION_CHANGE_DISPLAY_", generated.ConfigurationChangeDisplay.Valid)
		if kindErr != nil || displayErr != nil || (display == generated.REDACTED && (item.GetBefore() != "[REDACTED]" || item.GetAfter() != "[REDACTED]")) {
			server.writeInternal(writer, request.Context(), errors.New("configuration diff change values are invalid"))
			return
		}
		result.Changes = append(result.Changes, generated.ConfigurationDiffChange{Kind: kind, Path: item.GetPath(), Display: display, Before: item.GetBefore(), After: item.GetAfter()})
	}
	writeJSON(writer, http.StatusOK, result)
}

func configurationVersionRef(input *controlplanev1.ConfigurationVersionRef) (generated.ConfigurationVersionRef, error) {
	if input == nil || input.GetVersion() == 0 || !validSHA256(input.GetContentSha256()) || !validSHA256(input.GetSnapshotSha256()) {
		return generated.ConfigurationVersionRef{}, errors.New("configuration version ref is invalid")
	}
	return generated.ConfigurationVersionRef{Version: int64(input.GetVersion()), ContentSha256: generated.Sha256(strings.ToLower(input.GetContentSha256())), SnapshotSha256: generated.Sha256(strings.ToLower(input.GetSnapshotSha256()))}, nil
}

func (server *Server) ManageInstructionSet(writer http.ResponseWriter, request *http.Request, params generated.ManageInstructionSetParams) {
	var body generated.ManageInstructionSetJSONRequestBody
	if !decodeJSON(writer, request, &body) {
		return
	}
	action := instructionAction(string(body.Action))
	version, ok := commandVersion(writer, action == controlplanev1.InstructionSetAction_INSTRUCTION_SET_ACTION_CREATE, params.IfMatch)
	if !ok || action == controlplanev1.InstructionSetAction_INSTRUCTION_SET_ACTION_UNSPECIFIED || !validateInstructionCommand(body, action) {
		if ok {
			writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		}
		return
	}
	content := stringValue(body.Content)
	contentDigest := sha256.Sum256([]byte(content))
	response, err := server.control.ManageInstructionSet(request.Context(), &controlplanev1.ManageInstructionSetRequest{
		IdempotencyKey: params.IdempotencyKey.String(), Action: action, InstructionSetId: stringValue(body.ResourceRef), ExpectedVersion: version,
		Name: stringValue(body.Name), TargetVersion: uint64Value(body.TargetVersion), Spec: &controlplanev1.InstructionSetSpec{
			StableKey: stringValue(body.StableKey), Locale: stringValue(body.Locale), CurrentVersion: 1,
			Content: content, ContentSha256: hex.EncodeToString(contentDigest[:]), VersionState: controlplanev1.InstructionVersionState_INSTRUCTION_VERSION_STATE_DRAFT,
			Ownership: uiOwnership(),
		},
	})
	statusCode := http.StatusOK
	if action == controlplanev1.InstructionSetAction_INSTRUCTION_SET_ACTION_CREATE {
		statusCode = http.StatusCreated
	}
	server.writeResourceResponse(writer, request.Context(), statusCode, response.GetInstructionSet(), err, true)
}

func ownerPage(writer http.ResponseWriter, pageSize *generated.PageSize, pageToken *generated.PageToken) (uint32, string, bool) {
	size, ok := pageSizeValue(pageSize)
	if !ok {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return 0, "", false
	}
	return size, stringValue(pageToken), true
}

func pageSizeValue(value *generated.PageSize) (uint32, bool) {
	if value == nil {
		return defaultPageSize, true
	}
	converted := int(*value)
	if converted < 1 || converted > maximumPageSize {
		return 0, false
	}
	return uint32(converted), true
}

func commandVersion(writer http.ResponseWriter, create bool, header *generated.OptionalIfMatch) (uint64, bool) {
	if create {
		if header != nil {
			writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
			return 0, false
		}
		return 0, true
	}
	if header == nil {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return 0, false
	}
	return requireETag(writer, string(*header))
}

func protectedAction(action string) controlplanev1.ProtectedConfigurationAction {
	return map[string]controlplanev1.ProtectedConfigurationAction{
		"CREATE":  controlplanev1.ProtectedConfigurationAction_PROTECTED_CONFIGURATION_ACTION_CREATE,
		"UPDATE":  controlplanev1.ProtectedConfigurationAction_PROTECTED_CONFIGURATION_ACTION_UPDATE,
		"ARCHIVE": controlplanev1.ProtectedConfigurationAction_PROTECTED_CONFIGURATION_ACTION_ARCHIVE,
		"DELETE":  controlplanev1.ProtectedConfigurationAction_PROTECTED_CONFIGURATION_ACTION_DELETE,
		"PAUSE":   controlplanev1.ProtectedConfigurationAction_PROTECTED_CONFIGURATION_ACTION_PAUSE,
		"RESUME":  controlplanev1.ProtectedConfigurationAction_PROTECTED_CONFIGURATION_ACTION_RESUME,
		"ENABLE":  controlplanev1.ProtectedConfigurationAction_PROTECTED_CONFIGURATION_ACTION_ENABLE,
		"DISABLE": controlplanev1.ProtectedConfigurationAction_PROTECTED_CONFIGURATION_ACTION_DISABLE,
	}[action]
}

func instructionAction(action string) controlplanev1.InstructionSetAction {
	return map[string]controlplanev1.InstructionSetAction{
		"CREATE": controlplanev1.InstructionSetAction_INSTRUCTION_SET_ACTION_CREATE, "UPDATE": controlplanev1.InstructionSetAction_INSTRUCTION_SET_ACTION_UPDATE,
		"VALIDATE": controlplanev1.InstructionSetAction_INSTRUCTION_SET_ACTION_VALIDATE, "PUBLISH": controlplanev1.InstructionSetAction_INSTRUCTION_SET_ACTION_PUBLISH,
		"ROLLBACK": controlplanev1.InstructionSetAction_INSTRUCTION_SET_ACTION_ROLLBACK, "DETACH": controlplanev1.InstructionSetAction_INSTRUCTION_SET_ACTION_DETACH,
		"COPY": controlplanev1.InstructionSetAction_INSTRUCTION_SET_ACTION_COPY, "ARCHIVE": controlplanev1.InstructionSetAction_INSTRUCTION_SET_ACTION_ARCHIVE,
		"DELETE": controlplanev1.InstructionSetAction_INSTRUCTION_SET_ACTION_DELETE,
	}[action]
}

func validateRoleDefinitionCommand(body generated.RoleDefinitionCommand, action controlplanev1.ProtectedConfigurationAction) bool {
	if action == controlplanev1.ProtectedConfigurationAction_PROTECTED_CONFIGURATION_ACTION_CREATE || action == controlplanev1.ProtectedConfigurationAction_PROTECTED_CONFIGURATION_ACTION_UPDATE {
		return stringValue(body.Name) != "" && stringValue(body.StableKey) != "" && body.Capabilities != nil && body.AllowedTargetRoleDefinitionRefs != nil &&
			((body.RoleImageRecipeRef == nil && body.RoleImageRecipeVersion == nil && body.RoleImageRecipeSha256 == nil) ||
				(stringValue(body.RoleImageRecipeRef) != "" && uint64Value(body.RoleImageRecipeVersion) > 0 && shaValue(body.RoleImageRecipeSha256) != ""))
	}
	return stringValue(body.ResourceRef) != ""
}

func validateAgentCommand(body generated.AgentCommand, action controlplanev1.ProtectedConfigurationAction) bool {
	if action == controlplanev1.ProtectedConfigurationAction_PROTECTED_CONFIGURATION_ACTION_CREATE || action == controlplanev1.ProtectedConfigurationAction_PROTECTED_CONFIGURATION_ACTION_UPDATE {
		return stringValue(body.Name) != "" && stringValue(body.StableKey) != "" && stringValue(body.RuntimeSelectionKey) != "" &&
			stringValue(body.InstructionSetStableKey) != "" && stringValue(body.ProviderPoolStableKey) != "" && body.Capabilities != nil && body.Enabled != nil
	}
	return stringValue(body.ResourceRef) != ""
}

func validateInstructionCommand(body generated.InstructionSetCommand, action controlplanev1.InstructionSetAction) bool {
	switch action {
	case controlplanev1.InstructionSetAction_INSTRUCTION_SET_ACTION_CREATE, controlplanev1.InstructionSetAction_INSTRUCTION_SET_ACTION_UPDATE:
		return stringValue(body.Name) != "" && stringValue(body.StableKey) != "" && stringValue(body.Locale) != "" && body.Content != nil
	case controlplanev1.InstructionSetAction_INSTRUCTION_SET_ACTION_VALIDATE, controlplanev1.InstructionSetAction_INSTRUCTION_SET_ACTION_PUBLISH,
		controlplanev1.InstructionSetAction_INSTRUCTION_SET_ACTION_ROLLBACK:
		return stringValue(body.ResourceRef) != "" && uint64Value(body.TargetVersion) > 0
	default:
		return stringValue(body.ResourceRef) != ""
	}
}

func (server *Server) writeResourcePage(writer http.ResponseWriter, request *http.Request, resources []*controlplanev1.Resource, next string, err error) {
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	page := generated.ResourcePage{Resources: make([]generated.Resource, 0, len(resources))}
	for _, resource := range resources {
		converted, castErr := ConvertResource(resource)
		if castErr != nil {
			server.writeInternal(writer, request.Context(), castErr)
			return
		}
		page.Resources = append(page.Resources, converted)
	}
	page.NextPageToken = optionalString(next)
	writeJSON(writer, http.StatusOK, page)
}

func (server *Server) writeHistoryPage(writer http.ResponseWriter, request *http.Request, entries []*controlplanev1.ProtectedResourceHistoryEntry, next string, err error) {
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	page := generated.ResourceHistoryPage{Entries: make([]generated.ResourceHistoryEntry, 0, len(entries)), NextPageToken: optionalString(next)}
	for _, entry := range entries {
		converted, castErr := historyEntry(entry)
		if castErr != nil {
			server.writeInternal(writer, request.Context(), castErr)
			return
		}
		page.Entries = append(page.Entries, converted)
	}
	writeJSON(writer, http.StatusOK, page)
}

func historyEntry(entry *controlplanev1.ProtectedResourceHistoryEntry) (generated.ResourceHistoryEntry, error) {
	if entry == nil || entry.GetResource() == nil || entry.GetOccurredAt() == nil || entry.GetOccurredAt().CheckValid() != nil || !validSHA256(entry.GetSnapshotSha256()) {
		return generated.ResourceHistoryEntry{}, errors.New("resource history entry is invalid")
	}
	resource, err := ConvertResource(entry.GetResource())
	if err != nil {
		return generated.ResourceHistoryEntry{}, err
	}
	return generated.ResourceHistoryEntry{Resource: resource, Action: entry.GetAction(), SnapshotSha256: generated.Sha256(strings.ToLower(entry.GetSnapshotSha256())), OccurredAt: entry.GetOccurredAt().AsTime()}, nil
}

func (server *Server) writeInstructionComparison(writer http.ResponseWriter, request *http.Request, response *controlplanev1.CompareInstructionSetVersionsResponse, err error) {
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	left, leftErr := historyEntry(response.GetLeft())
	right, rightErr := historyEntry(response.GetRight())
	if leftErr != nil || rightErr != nil || !validSHA256(response.GetComparisonSha256()) {
		server.writeInternal(writer, request.Context(), errors.New("instruction comparison is invalid"))
		return
	}
	writeJSON(writer, http.StatusOK, generated.InstructionSetComparison{Left: left, Right: right, ContentEqual: response.GetContentEqual(), ComparisonSha256: generated.Sha256(strings.ToLower(response.GetComparisonSha256()))})
}

func stringValue[T ~string](pointer *T) string {
	if pointer == nil {
		return ""
	}
	return string(*pointer)
}

func sliceValue(pointer *[]string) []string {
	if pointer == nil {
		return nil
	}
	return *pointer
}

func boolValue(pointer *bool) bool { return pointer != nil && *pointer }

func shaValue(pointer *generated.Sha256) string {
	if pointer == nil {
		return ""
	}
	return string(*pointer)
}

func etag(version uint64) string { return fmt.Sprintf("\"%d\"", version) }
