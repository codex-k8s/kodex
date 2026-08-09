// Package casters преобразует generated Agent bot DTO в domain-safe values.
package casters

import (
	"errors"
	"strings"

	interactiongatewayv1 "github.com/codex-k8s/matter-codex/libs/go/interactiongatewayapi/gen/interactiongateway/v1"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/enum"
	"github.com/google/uuid"
)

func ListRequest(request *interactiongatewayv1.ListAgentMattermostBotIdentitiesRequest) (uint32, string, error) {
	if request == nil || request.GetPageSize() > 100 || (request.GetCursor() != "" && !validUUID(request.GetCursor())) {
		return 0, "", errors.New("Agent bot identity catalog request is invalid")
	}
	return request.GetPageSize(), request.GetCursor(), nil
}

func CreateRequest(request *interactiongatewayv1.CreateAndBindAgentMattermostBotIdentityRequest) (string, uint64, string, string, string, error) {
	if request == nil || !validUUID(request.GetAgentRef()) || request.GetExpectedAgentVersion() == 0 ||
		!validUUID(request.GetIdempotencyKey()) || request.GetDisplayName() == "" || len(request.GetDisplayName()) > 64 ||
		len(request.GetUsernameIntent()) > 128 || strings.ContainsAny(request.GetDisplayName()+request.GetUsernameIntent(), "\x00\r\n") {
		return "", 0, "", "", "", errors.New("Agent bot identity create request is invalid")
	}
	return request.GetAgentRef(), request.GetExpectedAgentVersion(), request.GetUsernameIntent(),
		request.GetDisplayName(), request.GetIdempotencyKey(), nil
}

func BindRequest(request *interactiongatewayv1.BindAgentMattermostBotIdentityRequest) (string, uint64, string, string, error) {
	if request == nil || !validUUID(request.GetAgentRef()) || request.GetExpectedAgentVersion() == 0 ||
		!validUUID(request.GetIdentitySelector()) || !validUUID(request.GetIdempotencyKey()) {
		return "", 0, "", "", errors.New("Agent bot identity bind request is invalid")
	}
	return request.GetAgentRef(), request.GetExpectedAgentVersion(), request.GetIdentitySelector(), request.GetIdempotencyKey(), nil
}

func RebindRequest(request *interactiongatewayv1.RebindAgentMattermostBotIdentityRequest) (string, uint64, uint64, string, string, error) {
	if request == nil || !validUUID(request.GetAgentRef()) || request.GetExpectedAgentVersion() == 0 ||
		request.GetExpectedProviderGeneration() == 0 || !validUUID(request.GetIdentitySelector()) ||
		!validUUID(request.GetIdempotencyKey()) {
		return "", 0, 0, "", "", errors.New("Agent bot identity rebind request is invalid")
	}
	return request.GetAgentRef(), request.GetExpectedAgentVersion(), request.GetExpectedProviderGeneration(),
		request.GetIdentitySelector(), request.GetIdempotencyKey(), nil
}

func RevokeRequest(request *interactiongatewayv1.RevokeAgentMattermostBotIdentityRequest) (string, uint64, uint64, string, error) {
	if request == nil || !validUUID(request.GetAgentRef()) || request.GetExpectedAgentVersion() == 0 ||
		request.GetExpectedProviderGeneration() == 0 || !validUUID(request.GetIdempotencyKey()) {
		return "", 0, 0, "", errors.New("Agent bot identity revoke request is invalid")
	}
	return request.GetAgentRef(), request.GetExpectedAgentVersion(), request.GetExpectedProviderGeneration(),
		request.GetIdempotencyKey(), nil
}

func AgentRequest(agentRef string) (string, error) {
	if !validUUID(agentRef) {
		return "", errors.New("Agent bot identity reference is invalid")
	}
	return agentRef, nil
}

func OperationRequest(request *interactiongatewayv1.GetAgentMattermostBotIdentityOperationRequest) (string, string, string, error) {
	if request == nil || !validUUID(request.GetAgentRef()) || !validUUID(request.GetIdempotencyKey()) {
		return "", "", "", errors.New("Agent bot identity operation request is invalid")
	}
	action := ""
	switch request.GetAction() {
	case interactiongatewayv1.AgentMattermostBotIdentityAction_AGENT_MATTERMOST_BOT_IDENTITY_ACTION_CREATE_AND_BIND:
		action = enum.AgentBotActionCreateAndBind
	case interactiongatewayv1.AgentMattermostBotIdentityAction_AGENT_MATTERMOST_BOT_IDENTITY_ACTION_BIND:
		action = enum.AgentBotActionBind
	case interactiongatewayv1.AgentMattermostBotIdentityAction_AGENT_MATTERMOST_BOT_IDENTITY_ACTION_REBIND:
		action = enum.AgentBotActionRebind
	case interactiongatewayv1.AgentMattermostBotIdentityAction_AGENT_MATTERMOST_BOT_IDENTITY_ACTION_REVOKE:
		action = enum.AgentBotActionRevoke
	default:
		return "", "", "", errors.New("Agent bot identity operation action is invalid")
	}
	return request.GetAgentRef(), action, request.GetIdempotencyKey(), nil
}

func ReadbackRequest(request *interactiongatewayv1.GetAgentMattermostBotIdentityProviderReadbackRequest) (string, string, error) {
	if request == nil || !validUUID(request.GetAgentRef()) ||
		(request.GetIdentitySelector() != "" && !validUUID(request.GetIdentitySelector())) {
		return "", "", errors.New("Agent bot identity provider readback request is invalid")
	}
	return request.GetAgentRef(), request.GetIdentitySelector(), nil
}

func validUUID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed != uuid.Nil
}
