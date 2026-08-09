package httptransport

import (
	"errors"
	"net/http"

	interactiongatewayv1 "github.com/codex-k8s/matter-codex/libs/go/interactiongatewayapi/gen/interactiongateway/v1"
	generated "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/transport/http/generated"
)

func (server *Server) ListAgentBotIdentities(writer http.ResponseWriter, request *http.Request, params generated.ListAgentBotIdentitiesParams) {
	size, ok := pageSizeValue(params.PageSize)
	if !ok {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	response, err := server.bot.ListAgentMattermostBotIdentities(request.Context(), &interactiongatewayv1.ListAgentMattermostBotIdentitiesRequest{PageSize: size, Cursor: stringValue(params.PageToken)})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	result := generated.AgentBotIdentityPage{Identities: make([]generated.AgentBotIdentity, 0, len(response.GetIdentities()))}
	for _, item := range response.GetIdentities() {
		value, castErr := castBotIdentity(item)
		if castErr != nil {
			server.writeInternal(writer, request.Context(), castErr)
			return
		}
		result.Identities = append(result.Identities, value)
	}
	result.NextPageToken = optionalString(response.GetNextCursor())
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) GetAgentBotIdentity(writer http.ResponseWriter, request *http.Request, resourceRef generated.ResourceRef) {
	response, err := server.bot.GetAgentMattermostBotIdentity(request.Context(), &interactiongatewayv1.GetAgentMattermostBotIdentityRequest{AgentRef: string(resourceRef)})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	binding, castErr := castBotBinding(response.GetBinding())
	if castErr != nil || binding.AgentRef != string(resourceRef) {
		server.writeInternal(writer, request.Context(), errors.New("agent bot identity binding readback is invalid"))
		return
	}
	writer.Header().Set("ETag", etag(uint64(binding.AgentVersion)))
	writeJSON(writer, http.StatusOK, binding)
}

func (server *Server) ManageAgentBotIdentity(writer http.ResponseWriter, request *http.Request, resourceRef generated.ResourceRef, params generated.ManageAgentBotIdentityParams) {
	version, ok := requireETag(writer, params.IfMatch)
	if !ok {
		return
	}
	var body generated.ManageAgentBotIdentityJSONRequestBody
	if !decodeJSON(writer, request, &body) {
		return
	}
	if !validAgentBotCommand(body) {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	action := interactiongatewayv1.AgentMattermostBotIdentityAction(interactiongatewayv1.AgentMattermostBotIdentityAction_value["AGENT_MATTERMOST_BOT_IDENTITY_ACTION_"+string(body.Action)])
	var operation *interactiongatewayv1.AgentMattermostBotIdentityOperationView
	var binding *interactiongatewayv1.AgentMattermostBotIdentityBindingView
	var err error
	switch action {
	case interactiongatewayv1.AgentMattermostBotIdentityAction_AGENT_MATTERMOST_BOT_IDENTITY_ACTION_CREATE_AND_BIND:
		response, callErr := server.bot.CreateAndBindAgentMattermostBotIdentity(request.Context(), &interactiongatewayv1.CreateAndBindAgentMattermostBotIdentityRequest{AgentRef: string(resourceRef), ExpectedAgentVersion: version, UsernameIntent: stringValue(body.UsernameIntent), DisplayName: stringValue(body.DisplayName), IdempotencyKey: params.IdempotencyKey.String()})
		err, operation, binding = callErr, response.GetOperation(), response.GetBinding()
	case interactiongatewayv1.AgentMattermostBotIdentityAction_AGENT_MATTERMOST_BOT_IDENTITY_ACTION_BIND:
		response, callErr := server.bot.BindAgentMattermostBotIdentity(request.Context(), &interactiongatewayv1.BindAgentMattermostBotIdentityRequest{AgentRef: string(resourceRef), ExpectedAgentVersion: version, IdentitySelector: stringValue(body.IdentitySelector), IdempotencyKey: params.IdempotencyKey.String()})
		err, operation, binding = callErr, response.GetOperation(), response.GetBinding()
	case interactiongatewayv1.AgentMattermostBotIdentityAction_AGENT_MATTERMOST_BOT_IDENTITY_ACTION_REBIND:
		response, callErr := server.bot.RebindAgentMattermostBotIdentity(request.Context(), &interactiongatewayv1.RebindAgentMattermostBotIdentityRequest{AgentRef: string(resourceRef), ExpectedAgentVersion: version, ExpectedProviderGeneration: uint64Value(body.ExpectedProviderGeneration), IdentitySelector: stringValue(body.IdentitySelector), IdempotencyKey: params.IdempotencyKey.String()})
		err, operation, binding = callErr, response.GetOperation(), response.GetBinding()
	case interactiongatewayv1.AgentMattermostBotIdentityAction_AGENT_MATTERMOST_BOT_IDENTITY_ACTION_REVOKE:
		response, callErr := server.bot.RevokeAgentMattermostBotIdentity(request.Context(), &interactiongatewayv1.RevokeAgentMattermostBotIdentityRequest{AgentRef: string(resourceRef), ExpectedAgentVersion: version, ExpectedProviderGeneration: uint64Value(body.ExpectedProviderGeneration), IdempotencyKey: params.IdempotencyKey.String()})
		err, operation, binding = callErr, response.GetOperation(), response.GetBinding()
	default:
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	server.writeAgentBotCommandResult(writer, request, resourceRef, operation, binding, err)
}

func validAgentBotCommand(body generated.AgentBotIdentityCommand) bool {
	switch body.Action {
	case generated.AgentBotIdentityActionCREATEANDBIND:
		return stringValue(body.UsernameIntent) != "" && stringValue(body.DisplayName) != "" && body.IdentitySelector == nil && body.ExpectedProviderGeneration == nil
	case generated.AgentBotIdentityActionBIND:
		return stringValue(body.IdentitySelector) != "" && body.UsernameIntent == nil && body.DisplayName == nil && body.ExpectedProviderGeneration == nil
	case generated.AgentBotIdentityActionREBIND:
		return stringValue(body.IdentitySelector) != "" && uint64Value(body.ExpectedProviderGeneration) > 0 && body.UsernameIntent == nil && body.DisplayName == nil
	case generated.AgentBotIdentityActionREVOKE:
		return body.IdentitySelector == nil && uint64Value(body.ExpectedProviderGeneration) > 0 && body.UsernameIntent == nil && body.DisplayName == nil
	default:
		return false
	}
}

func (server *Server) writeAgentBotCommandResult(writer http.ResponseWriter, request *http.Request, expected generated.ResourceRef, operation *interactiongatewayv1.AgentMattermostBotIdentityOperationView, binding *interactiongatewayv1.AgentMattermostBotIdentityBindingView, err error) {
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, true)
		return
	}
	projectedOperation, castErr := castBotOperation(operation)
	if castErr != nil || projectedOperation.AgentRef != string(expected) {
		server.writeInternal(writer, request.Context(), errors.New("agent bot identity operation readback is invalid"))
		return
	}
	result := generated.AgentBotIdentityCommandResult{Operation: projectedOperation}
	if binding != nil {
		projectedBinding, bindingErr := castBotBinding(binding)
		if bindingErr != nil || projectedBinding.AgentRef != string(expected) {
			server.writeInternal(writer, request.Context(), errors.New("agent bot identity command binding is invalid"))
			return
		}
		result.Binding = &projectedBinding
		writer.Header().Set("ETag", etag(uint64(projectedBinding.AgentVersion)))
	}
	status := http.StatusAccepted
	if projectedOperation.State == generated.AgentBotIdentityOperationStateBOUND || projectedOperation.State == generated.AgentBotIdentityOperationStateREVOKED {
		status = http.StatusOK
	}
	writeJSON(writer, status, result)
}

func (server *Server) GetAgentBotIdentityOperation(writer http.ResponseWriter, request *http.Request, resourceRef generated.ResourceRef, params generated.GetAgentBotIdentityOperationParams) {
	action := interactiongatewayv1.AgentMattermostBotIdentityAction(interactiongatewayv1.AgentMattermostBotIdentityAction_value["AGENT_MATTERMOST_BOT_IDENTITY_ACTION_"+string(params.Action)])
	if action == interactiongatewayv1.AgentMattermostBotIdentityAction_AGENT_MATTERMOST_BOT_IDENTITY_ACTION_UNSPECIFIED {
		writeProblem(writer, localProblem(http.StatusBadRequest, "INVALID_REQUEST", false))
		return
	}
	response, err := server.bot.GetAgentMattermostBotIdentityOperation(request.Context(), &interactiongatewayv1.GetAgentMattermostBotIdentityOperationRequest{AgentRef: string(resourceRef), Action: action, IdempotencyKey: params.IdempotencyKey.String()})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	value, castErr := castBotOperation(response.GetOperation())
	if castErr != nil || value.AgentRef != string(resourceRef) {
		server.writeInternal(writer, request.Context(), errors.New("agent bot identity operation readback is invalid"))
		return
	}
	writeJSON(writer, http.StatusOK, value)
}

func (server *Server) GetAgentBotIdentityProviderReadback(writer http.ResponseWriter, request *http.Request, resourceRef generated.ResourceRef, params generated.GetAgentBotIdentityProviderReadbackParams) {
	response, err := server.bot.GetAgentMattermostBotIdentityProviderReadback(request.Context(), &interactiongatewayv1.GetAgentMattermostBotIdentityProviderReadbackRequest{AgentRef: string(resourceRef), IdentitySelector: params.IdentitySelector})
	if err != nil {
		server.writeRPCError(writer, request.Context(), err, false)
		return
	}
	value, castErr := castBotIdentity(response.GetIdentity())
	if castErr != nil || value.Selector != params.IdentitySelector {
		server.writeInternal(writer, request.Context(), errors.New("agent bot identity provider readback is invalid"))
		return
	}
	writeJSON(writer, http.StatusOK, value)
}
