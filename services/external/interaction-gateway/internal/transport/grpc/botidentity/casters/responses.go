package casters

import (
	"time"

	interactiongatewayv1 "github.com/codex-k8s/matter-codex/libs/go/interactiongatewayapi/gen/interactiongateway/v1"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/enum"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func ListResponse(identities []entity.AgentMattermostBotIdentity, next string) *interactiongatewayv1.ListAgentMattermostBotIdentitiesResponse {
	result := &interactiongatewayv1.ListAgentMattermostBotIdentitiesResponse{NextCursor: next,
		Identities: make([]*interactiongatewayv1.AgentMattermostBotIdentityView, 0, len(identities))}
	for _, identity := range identities {
		result.Identities = append(result.Identities, IdentityView(identity))
	}
	return result
}

func IdentityView(identity entity.AgentMattermostBotIdentity) *interactiongatewayv1.AgentMattermostBotIdentityView {
	identityRef := identity.ProviderObjectRef
	if identityRef == "" {
		identityRef = identity.IdentityRef
	}
	status := interactiongatewayv1.AgentMattermostBotIdentityStatus_AGENT_MATTERMOST_BOT_IDENTITY_STATUS_UNKNOWN
	switch identity.Status {
	case enum.AgentBotIdentityAvailable:
		status = interactiongatewayv1.AgentMattermostBotIdentityStatus_AGENT_MATTERMOST_BOT_IDENTITY_STATUS_AVAILABLE
	case enum.AgentBotIdentityRevoked:
		status = interactiongatewayv1.AgentMattermostBotIdentityStatus_AGENT_MATTERMOST_BOT_IDENTITY_STATUS_REVOKED
	case enum.AgentBotIdentityDeleted:
		status = interactiongatewayv1.AgentMattermostBotIdentityStatus_AGENT_MATTERMOST_BOT_IDENTITY_STATUS_DELETED
	}
	return &interactiongatewayv1.AgentMattermostBotIdentityView{
		IdentityRef: identityRef, Selector: identity.Selector, Username: identity.Username,
		DisplayName: identity.DisplayName, Status: status, ProviderVersion: identity.ProviderVersion,
		ProviderGeneration: identity.ProviderGeneration, ProviderSnapshotSha256: identity.ProviderSnapshotSHA256,
		ObservedAt: timestamp(identity.ObservedAt),
		UpdatedAt:  timestamp(identity.UpdatedAt),
	}
}

func BindingView(binding entity.AgentMattermostBotBinding) *interactiongatewayv1.AgentMattermostBotIdentityBindingView {
	if binding.AgentRef == "" {
		return nil
	}
	return &interactiongatewayv1.AgentMattermostBotIdentityBindingView{AgentRef: binding.AgentRef,
		AgentVersion: binding.AgentVersion, Identity: IdentityView(binding.Identity),
		ReceiptSha256: binding.ReceiptSHA256, UpdatedAt: timestamp(binding.UpdatedAt)}
}

func OperationView(operation entity.AgentMattermostBotOperation) *interactiongatewayv1.AgentMattermostBotIdentityOperationView {
	action := interactiongatewayv1.AgentMattermostBotIdentityAction_AGENT_MATTERMOST_BOT_IDENTITY_ACTION_UNSPECIFIED
	switch operation.Action {
	case enum.AgentBotActionCreateAndBind:
		action = interactiongatewayv1.AgentMattermostBotIdentityAction_AGENT_MATTERMOST_BOT_IDENTITY_ACTION_CREATE_AND_BIND
	case enum.AgentBotActionBind:
		action = interactiongatewayv1.AgentMattermostBotIdentityAction_AGENT_MATTERMOST_BOT_IDENTITY_ACTION_BIND
	case enum.AgentBotActionRebind:
		action = interactiongatewayv1.AgentMattermostBotIdentityAction_AGENT_MATTERMOST_BOT_IDENTITY_ACTION_REBIND
	case enum.AgentBotActionRevoke:
		action = interactiongatewayv1.AgentMattermostBotIdentityAction_AGENT_MATTERMOST_BOT_IDENTITY_ACTION_REVOKE
	}
	state := interactiongatewayv1.AgentMattermostBotIdentityOperationState_AGENT_MATTERMOST_BOT_IDENTITY_OPERATION_STATE_UNSPECIFIED
	switch operation.State {
	case enum.AgentBotOperationEffectPending:
		state = interactiongatewayv1.AgentMattermostBotIdentityOperationState_AGENT_MATTERMOST_BOT_IDENTITY_OPERATION_STATE_EFFECT_PENDING
	case enum.AgentBotOperationMembershipPending:
		state = interactiongatewayv1.AgentMattermostBotIdentityOperationState_AGENT_MATTERMOST_BOT_IDENTITY_OPERATION_STATE_MEMBERSHIP_PENDING
	case enum.AgentBotOperationAmbiguous:
		state = interactiongatewayv1.AgentMattermostBotIdentityOperationState_AGENT_MATTERMOST_BOT_IDENTITY_OPERATION_STATE_AMBIGUOUS
	case enum.AgentBotOperationProviderAccepted:
		state = interactiongatewayv1.AgentMattermostBotIdentityOperationState_AGENT_MATTERMOST_BOT_IDENTITY_OPERATION_STATE_PROVIDER_ACCEPTED
	case enum.AgentBotOperationBound:
		state = interactiongatewayv1.AgentMattermostBotIdentityOperationState_AGENT_MATTERMOST_BOT_IDENTITY_OPERATION_STATE_BOUND
	case enum.AgentBotOperationRevoked:
		state = interactiongatewayv1.AgentMattermostBotIdentityOperationState_AGENT_MATTERMOST_BOT_IDENTITY_OPERATION_STATE_REVOKED
	case enum.AgentBotOperationRepairRequired:
		state = interactiongatewayv1.AgentMattermostBotIdentityOperationState_AGENT_MATTERMOST_BOT_IDENTITY_OPERATION_STATE_REPAIR_REQUIRED
	}
	return &interactiongatewayv1.AgentMattermostBotIdentityOperationView{OperationId: operation.ID,
		Action: action, State: state, AgentRef: operation.AgentRef,
		ExpectedAgentVersion: operation.ExpectedAgentVersion, PredecessorGeneration: operation.PredecessorGeneration,
		RequestSha256: operation.RequestSHA256, FailureCode: safeFailureCode(operation.FailureCode),
		RetryNotBefore: timestamp(operation.RetryNotBefore), RecoveryDeadline: timestamp(operation.RecoveryDeadline),
		CreatedAt: timestamp(operation.CreatedAt), UpdatedAt: timestamp(operation.UpdatedAt), Result: BindingView(operation.Result)}
}

func safeFailureCode(value string) string {
	switch value {
	case "", "PROVIDER_OUTCOME_UNKNOWN", "PROVIDER_READBACK_MISMATCH", "RECOVERY_TIMEOUT",
		"OWNER_OUTCOME_UNKNOWN", "OWNER_PREDECESSOR_MISMATCH", "OWNER_READBACK_MISMATCH":
		return value
	default:
		return "SAFE_FAILURE"
	}
}

func timestamp(value time.Time) *timestamppb.Timestamp {
	if value.IsZero() || value.Equal(time.Unix(0, 0).UTC()) {
		return nil
	}
	return timestamppb.New(value)
}
