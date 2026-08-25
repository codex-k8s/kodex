package grpc

import (
	"context"
	"strings"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
)

func (server *Server) ListInteractionSources(ctx context.Context, _ *controlplanev1.ListInteractionSourcesRequest) (*controlplanev1.ListInteractionSourcesResponse, error) {
	p, err := principal(ctx, controlplanev1.InteractionWorkService_ListInteractionSources_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, err := server.service.ListInteractionSources(ctx, p)
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListInteractionSourcesResponse{}
	for _, item := range items {
		response.Sources = append(response.Sources, &controlplanev1.InteractionSource{
			ConnectionRef: itemString(item, "connectionRef"), CredentialMaterializationRef: itemString(item, "credentialRef"),
			BaseUrl: itemString(item, "baseURL"), TeamName: itemString(item, "teamName"),
			ChannelName: itemString(item, "channelName"), Locale: itemString(item, "locale"),
			EnabledCapabilities: itemStrings(item, "capabilities"),
		})
	}
	return response, nil
}

func (server *Server) ClaimInteractionDeliveries(ctx context.Context, request *controlplanev1.ClaimInteractionDeliveriesRequest) (*controlplanev1.ClaimInteractionDeliveriesResponse, error) {
	p, err := principal(ctx, controlplanev1.InteractionWorkService_ClaimInteractionDeliveries_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, err := server.service.ClaimInteractionDeliveries(ctx, p, request.GetWorkloadInstance(), request.GetLimit())
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ClaimInteractionDeliveriesResponse{}
	for _, item := range items {
		templateData, _ := item["templateData"].(map[string]any)
		response.Claims = append(response.Claims, &controlplanev1.InteractionDeliveryClaim{
			DeliveryRef: itemString(item, "deliveryRef"), ConnectionRef: itemString(item, "connectionRef"),
			CredentialMaterializationRef: itemString(item, "credentialRef"), BaseUrl: itemString(item, "baseURL"),
			TeamName: itemString(item, "teamName"), ChannelName: itemString(item, "channelName"), Locale: itemString(item, "locale"),
			CapabilityKey: itemString(item, "capabilityKey"), MessageKey: itemString(item, "messageKey"),
			TemplateData: structure(templateData), Lease: castLease(item),
		})
	}
	return response, nil
}

func (server *Server) CompleteInteractionDelivery(ctx context.Context, request *controlplanev1.CompleteInteractionDeliveryRequest) (*controlplanev1.CompleteInteractionDeliveryResponse, error) {
	payload := command.InteractionDeliveryInput{
		DeliveryRef: request.GetDeliveryRef(), LeaseRef: request.GetLeaseRef(), Fence: request.GetFence(),
		Generation: request.GetGeneration(), Success: request.GetSuccess(), ExternalPostRef: request.GetExternalPostRef(),
		ExternalThreadRef: request.GetExternalThreadRef(), SafeErrorCode: request.GetSafeErrorCode(),
	}
	result, err := execute(ctx, server.service, controlplanev1.InteractionWorkService_CompleteInteractionDelivery_FullMethodName, command.CompleteInteractionDelivery, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.CompleteInteractionDeliveryResponse{
		DeliveryRef: mapString(result.Runtime, "deliveryRef"), State: mapString(result.Runtime, "state"),
		CoreRunAffected: false,
	}, nil
}

func (server *Server) AcceptInteractionMessage(ctx context.Context, request *controlplanev1.AcceptInteractionMessageRequest) (*controlplanev1.AcceptInteractionMessageResponse, error) {
	decision := ""
	if request.GetDecision() != controlplanev1.OwnerGateDecision_OWNER_GATE_DECISION_UNSPECIFIED {
		decision = enumSuffix(request.GetDecision(), "OWNER_GATE_DECISION_")
	}
	payload := command.InteractionMessageInput{
		ConnectionRef: request.GetConnectionRef(), ExternalEventRef: request.GetExternalEventRef(),
		ExternalPostRef: request.GetExternalPostRef(), ExternalRootPostRef: request.GetExternalRootPostRef(),
		ExternalChannelRef: request.GetExternalChannelRef(), ExternalUserDigest: request.GetExternalUserDigest(),
		Message: request.GetMessage(), Decision: decision,
	}
	result, err := execute(ctx, server.service, controlplanev1.InteractionWorkService_AcceptInteractionMessage_FullMethodName, command.AcceptInteractionMessage, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.AcceptInteractionMessageResponse{
		Outcome:             interactionOutcome(mapString(result.Runtime, "outcome")),
		MessageKey:          strings.TrimPrefix(mapString(result.Runtime, "messageKey"), "i18n:"),
		AcceptedResourceRef: mapString(result.Runtime, "acceptedResourceRef"),
	}, nil
}

func itemString(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

func itemStrings(values map[string]any, key string) []string {
	value, _ := values[key].([]string)
	return append([]string(nil), value...)
}

func interactionOutcome(value string) controlplanev1.InteractionMessageOutcome {
	switch value {
	case "IGNORED":
		return controlplanev1.InteractionMessageOutcome_INTERACTION_MESSAGE_OUTCOME_IGNORED
	case "RUN_STARTED":
		return controlplanev1.InteractionMessageOutcome_INTERACTION_MESSAGE_OUTCOME_RUN_STARTED
	case "GATE_RESOLVED":
		return controlplanev1.InteractionMessageOutcome_INTERACTION_MESSAGE_OUTCOME_GATE_RESOLVED
	case "STALE":
		return controlplanev1.InteractionMessageOutcome_INTERACTION_MESSAGE_OUTCOME_STALE
	case "DUPLICATE":
		return controlplanev1.InteractionMessageOutcome_INTERACTION_MESSAGE_OUTCOME_DUPLICATE
	default:
		return controlplanev1.InteractionMessageOutcome_INTERACTION_MESSAGE_OUTCOME_UNSPECIFIED
	}
}
