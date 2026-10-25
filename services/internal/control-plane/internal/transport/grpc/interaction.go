package grpc

import (
	"context"
	"strings"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// ListInteractionSources reads authorized sources, requires a typed nonempty credential revision
// and projects definition, connection and capability metadata.
// ListInteractionSources читает авторизованные источники, требует типизированную непустую ревизию
// credential и преобразует метаданные определения, подключения и возможностей.
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
		credential, ok := item["credential"].(entity.IntegrationCredentialRevision)
		if !ok || credential.Ref == "" {
			return nil, transportError(errs.ErrUnavailable)
		}
		response.Sources = append(response.Sources, &controlplanev1.InteractionSource{
			DefinitionKey: mapString(item, "definitionKey"), DefinitionVersion: mapString(item, "definitionVersion"), DefinitionDigest: mapString(item, "definitionDigest"), DefinitionPackage: interactionPackageBytes(item),
			CredentialDescriptor: castIntegrationCredential(credential),
			ConnectionRef:        mapString(item, "connectionRef"), CredentialMaterializationRef: mapString(item, "credentialRef"),
			BaseUrl: mapString(item, "baseURL"), TeamName: mapString(item, "teamName"),
			ChannelName: mapString(item, "channelName"), Locale: mapString(item, "locale"),
			EnabledCapabilities: itemStrings(item, "capabilities"),
			ConnectionVersion:   mapInt64(item, "connectionVersion"), CredentialRevisionRef: mapString(item, "credentialRevisionRef"), CredentialRevision: mapInt64(item, "credentialRevision"),
		})
	}
	return response, nil
}

// ClaimInteractionDeliveries reads workload delivery claims, requires typed credential metadata
// and preserves definition, lease, gate, approval and external thread pins.
// ClaimInteractionDeliveries читает claims доставки для workload, требует типизированные
// метаданные credential и сохраняет привязки определения, lease, шлюзов, одобрения и внешней
// ветки.
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
		credential, ok := item["credential"].(entity.IntegrationCredentialRevision)
		if !ok || credential.Ref == "" {
			return nil, transportError(errs.ErrUnavailable)
		}
		response.Claims = append(response.Claims, &controlplanev1.InteractionDeliveryClaim{
			DefinitionKey: mapString(item, "definitionKey"), DefinitionVersion: mapString(item, "definitionVersion"), DefinitionDigest: mapString(item, "definitionDigest"), DefinitionPackage: interactionPackageBytes(item),
			ConnectionVersion: mapInt64(item, "connectionVersion"), ApprovalGateRef: mapString(item, "approvalGateRef"), ApprovalGateVersion: mapInt64(item, "approvalGateVersion"),
			SourceCapabilityKey:  mapString(item, "sourceCapabilityKey"),
			CredentialDescriptor: castIntegrationCredential(credential),
			DeliveryRef:          mapString(item, "deliveryRef"), ConnectionRef: mapString(item, "connectionRef"),
			CredentialMaterializationRef: mapString(item, "credentialRef"), BaseUrl: mapString(item, "baseURL"),
			TeamName: mapString(item, "teamName"), ChannelName: mapString(item, "channelName"), Locale: mapString(item, "locale"),
			CapabilityKey: mapString(item, "capabilityKey"), MessageKey: mapString(item, "messageKey"),
			TemplateData: structure(templateData), Lease: castLease(item),
			GateRef: mapString(item, "gateRef"), GateVersion: mapInt64(item, "gateVersion"), RunRef: mapString(item, "runRef"),
			ExternalTeamRef: mapString(item, "externalTeamRef"), ExternalChannelRef: mapString(item, "externalChannelRef"),
			ExternalRootPostRef: mapString(item, "externalRootPostRef"), AcceptanceReceiptRef: mapString(item, "acceptanceReceiptRef"),
		})
	}
	return response, nil
}

// CompleteInteractionDelivery passes exact lease and external-effect evidence through the
// dedicated completion command and returns delivery state with CoreRunAffected false.
// CompleteInteractionDelivery передаёт точную lease и свидетельства внешнего эффекта отдельной
// команде завершения и возвращает состояние доставки с CoreRunAffected=false.
func (server *Server) CompleteInteractionDelivery(ctx context.Context, request *controlplanev1.CompleteInteractionDeliveryRequest) (*controlplanev1.CompleteInteractionDeliveryResponse, error) {
	payload := command.InteractionDeliveryInput{
		DeliveryRef: request.GetDeliveryRef(), LeaseRef: request.GetLeaseRef(), Fence: request.GetFence(),
		Generation: request.GetGeneration(), Success: request.GetSuccess(), ExternalPostRef: request.GetExternalPostRef(),
		ExternalThreadRef: request.GetExternalThreadRef(), SafeErrorCode: request.GetSafeErrorCode(),
		UnknownOutcome: request.GetUnknownOutcome(), ConfirmedNoEffect: request.GetConfirmedNoEffect(),
		ExternalTeamRef: request.GetExternalTeamRef(), ExternalChannelRef: request.GetExternalChannelRef(),
	}
	return executeResponse(ctx, server.service, controlplanev1.InteractionWorkService_CompleteInteractionDelivery_FullMethodName, command.CompleteInteractionDelivery, request.GetMutation(), payload, func(result command.Result) *controlplanev1.CompleteInteractionDeliveryResponse {
		return &controlplanev1.CompleteInteractionDeliveryResponse{DeliveryRef: mapString(result.Runtime, "deliveryRef"), State: mapString(result.Runtime, "state"), CoreRunAffected: false}
	})
}

// AcceptInteractionMessage converts the optional gate decision and passes external message
// locators through the dedicated command, stripping only the returned i18n prefix.
// AcceptInteractionMessage преобразует необязательное решение шлюза и передаёт ссылки внешнего
// сообщения отдельной команде, удаляя только префикс i18n из результата.
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
		ExternalTeamRef: request.GetExternalTeamRef(), GateRef: request.GetGateRef(), RunRef: request.GetRunRef(), ExpectedGateVersion: request.GetExpectedGateVersion(),
	}
	return executeResponse(ctx, server.service, controlplanev1.InteractionWorkService_AcceptInteractionMessage_FullMethodName, command.AcceptInteractionMessage, request.GetMutation(), payload, func(result command.Result) *controlplanev1.AcceptInteractionMessageResponse {
		return &controlplanev1.AcceptInteractionMessageResponse{Outcome: interactionOutcome(mapString(result.Runtime, "outcome")), MessageKey: strings.TrimPrefix(mapString(result.Runtime, "messageKey"), "i18n:"), AcceptedResourceRef: mapString(result.Runtime, "acceptedResourceRef")}
	})
}

// interactionPackageBytes copies the definition package byte slice so the returned payload does
// not alias the owner result buffer.
// interactionPackageBytes копирует байты пакета определения, чтобы возвращаемый payload не
// ссылался на буфер результата владельца.
func interactionPackageBytes(values map[string]any) []byte {
	value, _ := values["definitionPackage"].([]byte)
	return append([]byte(nil), value...)
}

// itemStrings copies only a string-typed slice, returning nil for other types or an empty slice
// without accepting any-typed lists.
// itemStrings копирует только срез строк, возвращая nil для других типов либо пустого среза без
// принятия списков типа any.
func itemStrings(values map[string]any, key string) []string {
	value, _ := values[key].([]string)
	return append([]string(nil), value...)
}

// interactionOutcome maps the five supported domain interaction outcomes and uses UNSPECIFIED for
// every other string.
// interactionOutcome переводит пять поддерживаемых доменных исходов interaction и использует
// UNSPECIFIED для прочих строк.
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
