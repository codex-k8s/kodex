package casters

import (
	"strings"

	"github.com/google/uuid"

	providersv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/providers/v1"
	providerservice "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
)

// IngestWebhookEventInput maps a gRPC request to the domain command input.
func IngestWebhookEventInput(request *providersv1.IngestWebhookEventRequest) (providerservice.IngestWebhookEventInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return providerservice.IngestWebhookEventInput{}, err
	}
	receivedAt, err := requiredTime(request.GetReceivedAt())
	if err != nil {
		return providerservice.IngestWebhookEventInput{}, err
	}
	return providerservice.IngestWebhookEventInput{
		ProviderSlug:         providerSlug(request.GetProviderSlug()),
		DeliveryID:           strings.TrimSpace(request.GetDeliveryId()),
		EventName:            strings.TrimSpace(request.GetEventName()),
		RepositoryProviderID: strings.TrimSpace(request.GetRepositoryProviderId()),
		PayloadJSON:          []byte(strings.TrimSpace(request.GetPayloadJson())),
		ReceivedAt:           receivedAt,
		Meta:                 meta,
	}, nil
}

// GetWebhookEventInput maps a gRPC request to the domain read input.
func GetWebhookEventInput(request *providersv1.GetWebhookEventRequest) (providerservice.GetWebhookEventInput, error) {
	meta, err := QueryMetaFromProto(request.GetMeta())
	if err != nil {
		return providerservice.GetWebhookEventInput{}, err
	}
	id, err := requiredUUID(request.GetWebhookEventId())
	if err != nil {
		return providerservice.GetWebhookEventInput{}, err
	}
	input := providerservice.GetWebhookEventInput{
		WebhookEventID: id,
		Meta:           meta,
	}
	return input, nil
}

// ListWebhookEventsInput maps a gRPC request to the domain read input.
func ListWebhookEventsInput(request *providersv1.ListWebhookEventsRequest) (providerservice.ListWebhookEventsInput, error) {
	meta, err := QueryMetaFromProto(request.GetMeta())
	if err != nil {
		return providerservice.ListWebhookEventsInput{}, err
	}
	statuses, err := webhookStatusesFromProto(request.GetProcessingStatuses())
	if err != nil {
		return providerservice.ListWebhookEventsInput{}, err
	}
	receivedSince, err := optionalTimePtr(request.GetReceivedSince())
	if err != nil {
		return providerservice.ListWebhookEventsInput{}, err
	}
	receivedUntil, err := optionalTimePtr(request.GetReceivedUntil())
	if err != nil {
		return providerservice.ListWebhookEventsInput{}, err
	}
	return providerservice.ListWebhookEventsInput{
		ProviderSlug:         providerSlug(request.GetProviderSlug()),
		DeliveryID:           strings.TrimSpace(request.GetDeliveryId()),
		EventNames:           trimProtoStrings(request.GetEventNames()),
		ProcessingStatuses:   statuses,
		RepositoryProviderID: strings.TrimSpace(request.GetRepositoryProviderId()),
		ReceivedSince:        receivedSince,
		ReceivedUntil:        receivedUntil,
		Page:                 pageRequestFromProto(request.GetPage()),
		Meta:                 meta,
	}, nil
}

// RetryWebhookEventProcessingInput maps a gRPC request to the domain command input.
func RetryWebhookEventProcessingInput(request *providersv1.RetryWebhookEventProcessingRequest) (providerservice.RetryWebhookEventProcessingInput, error) {
	id, err := requiredUUID(request.GetWebhookEventId())
	if err != nil {
		return providerservice.RetryWebhookEventProcessingInput{}, err
	}
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return providerservice.RetryWebhookEventProcessingInput{}, err
	}
	return providerservice.RetryWebhookEventProcessingInput{WebhookEventID: id, Meta: meta}, nil
}

// GetProviderAccountRuntimeStateInput maps a gRPC request to the domain read input.
func GetProviderAccountRuntimeStateInput(request *providersv1.GetProviderAccountRuntimeStateRequest) (providerservice.GetProviderAccountRuntimeStateInput, error) {
	meta, err := QueryMetaFromProto(request.GetMeta())
	if err != nil {
		return providerservice.GetProviderAccountRuntimeStateInput{}, err
	}
	ids, err := optionalRuntimeStateIDs(request)
	if err != nil {
		return providerservice.GetProviderAccountRuntimeStateInput{}, err
	}
	return providerservice.GetProviderAccountRuntimeStateInput{
		ProviderAccountRuntimeStateID: ids.stateID,
		ExternalAccountID:             ids.externalAccountID,
		ProviderSlug:                  providerSlug(request.GetProviderSlug()),
		Meta:                          meta,
	}, nil
}

// ListProviderAccountRuntimeStatesInput maps a gRPC request to the domain read input.
func ListProviderAccountRuntimeStatesInput(request *providersv1.ListProviderAccountRuntimeStatesRequest) (providerservice.ListProviderAccountRuntimeStatesInput, error) {
	meta, err := QueryMetaFromProto(request.GetMeta())
	if err != nil {
		return providerservice.ListProviderAccountRuntimeStatesInput{}, err
	}
	accountIDs, err := requiredUUIDs(request.GetExternalAccountIds())
	if err != nil {
		return providerservice.ListProviderAccountRuntimeStatesInput{}, err
	}
	statuses, err := runtimeStatusesFromProto(request.GetStatuses())
	if err != nil {
		return providerservice.ListProviderAccountRuntimeStatesInput{}, err
	}
	projectID, err := optionalUUIDPtr(request.GetProjectId())
	if err != nil {
		return providerservice.ListProviderAccountRuntimeStatesInput{}, err
	}
	organizationID, err := optionalUUIDPtr(request.GetOrganizationId())
	if err != nil {
		return providerservice.ListProviderAccountRuntimeStatesInput{}, err
	}
	return providerservice.ListProviderAccountRuntimeStatesInput{
		ProviderSlug:       providerSlug(request.GetProviderSlug()),
		ExternalAccountIDs: accountIDs,
		Statuses:           statuses,
		ProjectID:          projectID,
		OrganizationID:     organizationID,
		Page:               pageRequestFromProto(request.GetPage()),
		Meta:               meta,
	}, nil
}

// RecordProviderLimitSnapshotInput maps a gRPC request to the domain command input.
func RecordProviderLimitSnapshotInput(request *providersv1.RecordProviderLimitSnapshotRequest) (providerservice.RecordProviderLimitSnapshotInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return providerservice.RecordProviderLimitSnapshotInput{}, err
	}
	externalAccountID, err := requiredUUID(request.GetExternalAccountId())
	if err != nil {
		return providerservice.RecordProviderLimitSnapshotInput{}, err
	}
	resetAt, err := optionalTimePtr(request.GetResetAt())
	if err != nil {
		return providerservice.RecordProviderLimitSnapshotInput{}, err
	}
	capturedAt, err := requiredTime(request.GetCapturedAt())
	if err != nil {
		return providerservice.RecordProviderLimitSnapshotInput{}, err
	}
	return providerservice.RecordProviderLimitSnapshotInput{
		ExternalAccountID: externalAccountID,
		ProviderSlug:      providerSlug(request.GetProviderSlug()),
		LimitClass:        strings.TrimSpace(request.GetLimitClass()),
		Remaining:         request.Remaining,
		LimitValue:        request.LimitValue,
		ResetAt:           resetAt,
		CapturedAt:        capturedAt,
		Source:            enum.ProviderLimitSource(strings.TrimSpace(request.GetSource())),
		Meta:              meta,
	}, nil
}

// ListProviderLimitSnapshotsInput maps a gRPC request to the domain read input.
func ListProviderLimitSnapshotsInput(request *providersv1.ListProviderLimitSnapshotsRequest) (providerservice.ListProviderLimitSnapshotsInput, error) {
	meta, err := QueryMetaFromProto(request.GetMeta())
	if err != nil {
		return providerservice.ListProviderLimitSnapshotsInput{}, err
	}
	externalAccountID, err := optionalUUIDPtr(request.GetExternalAccountId())
	if err != nil {
		return providerservice.ListProviderLimitSnapshotsInput{}, err
	}
	capturedSince, err := optionalTimePtr(request.GetCapturedSince())
	if err != nil {
		return providerservice.ListProviderLimitSnapshotsInput{}, err
	}
	return providerservice.ListProviderLimitSnapshotsInput{
		ExternalAccountID: externalAccountID,
		ProviderSlug:      providerSlug(request.GetProviderSlug()),
		LimitClasses:      trimProtoStrings(request.GetLimitClasses()),
		CapturedSince:     capturedSince,
		Page:              pageRequestFromProto(request.GetPage()),
		Meta:              meta,
	}, nil
}

// ListProviderOperationsInput maps a gRPC request to the domain read input.
func ListProviderOperationsInput(request *providersv1.ListProviderOperationsRequest) (providerservice.ListProviderOperationsInput, error) {
	meta, err := QueryMetaFromProto(request.GetMeta())
	if err != nil {
		return providerservice.ListProviderOperationsInput{}, err
	}
	externalAccountID, err := optionalUUIDPtr(request.GetExternalAccountId())
	if err != nil {
		return providerservice.ListProviderOperationsInput{}, err
	}
	operationTypes, err := operationTypesFromProto(request.GetOperationTypes())
	if err != nil {
		return providerservice.ListProviderOperationsInput{}, err
	}
	statuses, err := operationStatusesFromProto(request.GetStatuses())
	if err != nil {
		return providerservice.ListProviderOperationsInput{}, err
	}
	startedSince, err := optionalTimePtr(request.GetStartedSince())
	if err != nil {
		return providerservice.ListProviderOperationsInput{}, err
	}
	return providerservice.ListProviderOperationsInput{
		ProviderSlug:      providerSlug(request.GetProviderSlug()),
		ExternalAccountID: externalAccountID,
		OperationTypes:    operationTypes,
		Statuses:          statuses,
		TargetRef:         strings.TrimSpace(request.GetTargetRef()),
		StartedSince:      startedSince,
		Page:              pageRequestFromProto(request.GetPage()),
		Meta:              meta,
	}, nil
}

func providerSlug(slug string) enum.ProviderSlug {
	return enum.ProviderSlug(strings.TrimSpace(slug))
}

type runtimeStateIDs struct {
	stateID           *uuid.UUID
	externalAccountID *uuid.UUID
}

func optionalRuntimeStateIDs(request *providersv1.GetProviderAccountRuntimeStateRequest) (runtimeStateIDs, error) {
	stateID, err := optionalUUIDPtr(request.GetProviderAccountRuntimeStateId())
	if err != nil {
		return runtimeStateIDs{}, err
	}
	accountID, err := optionalUUIDPtr(request.GetExternalAccountId())
	if err != nil {
		return runtimeStateIDs{}, err
	}
	return runtimeStateIDs{stateID: stateID, externalAccountID: accountID}, nil
}

func requiredUUIDs(values []string) ([]uuid.UUID, error) {
	result := make([]uuid.UUID, 0, len(values))
	for _, value := range values {
		id, err := requiredUUID(value)
		if err != nil {
			return nil, err
		}
		result = append(result, id)
	}
	return result, nil
}

func trimProtoStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
