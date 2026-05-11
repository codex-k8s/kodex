package casters

import (
	"strings"

	"github.com/google/uuid"

	providersv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/providers/v1"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/errs"
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
	id, meta, err := metaAndRequiredUUID(request.GetMeta(), request.GetWebhookEventId(), CommandMetaFromProto)
	input := providerservice.RetryWebhookEventProcessingInput{WebhookEventID: id, Meta: meta}
	return input, err
}

// GetWorkItemProjectionInput maps a gRPC request to the domain read input.
func GetWorkItemProjectionInput(request *providersv1.GetWorkItemProjectionRequest) (providerservice.GetWorkItemProjectionInput, error) {
	id, meta, err := metaAndRequiredUUID(request.GetMeta(), request.GetWorkItemProjectionId(), QueryMetaFromProto)
	input := providerservice.GetWorkItemProjectionInput{WorkItemProjectionID: id, Meta: meta}
	return input, err
}

// FindWorkItemByProviderRefInput maps a gRPC request to the domain read input.
func FindWorkItemByProviderRefInput(request *providersv1.FindWorkItemByProviderRefRequest) (providerservice.FindWorkItemByProviderRefInput, error) {
	meta, err := QueryMetaFromProto(request.GetMeta())
	if err != nil {
		return providerservice.FindWorkItemByProviderRefInput{}, err
	}
	target := request.GetTarget()
	if target == nil {
		return providerservice.FindWorkItemByProviderRefInput{}, errs.ErrInvalidArgument
	}
	kind, err := workItemKindFromProto(target.GetWorkItemKind())
	if err != nil {
		return providerservice.FindWorkItemByProviderRefInput{}, err
	}
	return providerservice.FindWorkItemByProviderRefInput{
		ProviderSlug:       providerSlug(target.GetProviderSlug()),
		RepositoryFullName: strings.TrimSpace(target.GetRepositoryFullName()),
		Kind:               kind,
		Number:             target.GetNumber(),
		ProviderObjectID:   strings.TrimSpace(target.GetProviderObjectId()),
		WebURL:             strings.TrimSpace(target.GetWebUrl()),
		Meta:               meta,
	}, nil
}

// ListWorkItemProjectionsInput maps a gRPC request to the domain read input.
func ListWorkItemProjectionsInput(request *providersv1.ListWorkItemProjectionsRequest) (providerservice.ListWorkItemProjectionsInput, error) {
	meta, err := QueryMetaFromProto(request.GetMeta())
	if err != nil {
		return providerservice.ListWorkItemProjectionsInput{}, err
	}
	projectID, err := optionalUUIDPtr(request.GetProjectId())
	if err != nil {
		return providerservice.ListWorkItemProjectionsInput{}, err
	}
	repositoryID, err := optionalUUIDPtr(request.GetRepositoryId())
	if err != nil {
		return providerservice.ListWorkItemProjectionsInput{}, err
	}
	kinds, err := workItemKindsFromProto(request.GetKinds())
	if err != nil {
		return providerservice.ListWorkItemProjectionsInput{}, err
	}
	driftStatuses, err := driftStatusesFromProto(request.GetDriftStatuses())
	if err != nil {
		return providerservice.ListWorkItemProjectionsInput{}, err
	}
	updatedSince, err := optionalTimePtr(request.GetUpdatedSince())
	if err != nil {
		return providerservice.ListWorkItemProjectionsInput{}, err
	}
	return providerservice.ListWorkItemProjectionsInput{
		ProjectID:          projectID,
		RepositoryID:       repositoryID,
		ProviderSlug:       providerSlug(request.GetProviderSlug()),
		RepositoryFullName: strings.TrimSpace(request.GetRepositoryFullName()),
		Kinds:              kinds,
		States:             trimProtoStrings(request.GetStates()),
		Labels:             trimProtoStrings(request.GetLabels()),
		WorkItemTypes:      trimProtoStrings(request.GetWorkItemTypes()),
		DriftStatuses:      driftStatuses,
		UpdatedSince:       updatedSince,
		Page:               pageRequestFromProto(request.GetPage()),
		Meta:               meta,
	}, nil
}

// ListCommentsInput maps a gRPC request to the domain read input.
func ListCommentsInput(request *providersv1.ListCommentsRequest) (providerservice.ListCommentsInput, error) {
	id, meta, err := metaAndRequiredUUID(request.GetMeta(), request.GetWorkItemProjectionId(), QueryMetaFromProto)
	if err != nil {
		return providerservice.ListCommentsInput{}, err
	}
	kinds, err := commentKindsFromProto(request.GetKinds())
	if err != nil {
		return providerservice.ListCommentsInput{}, err
	}
	return providerservice.ListCommentsInput{WorkItemProjectionID: id, Kinds: kinds, Page: pageRequestFromProto(request.GetPage()), Meta: meta}, nil
}

func metaAndRequiredUUID[MetaRequest any, Meta any](metaRequest MetaRequest, idText string, cast func(MetaRequest) (Meta, error)) (uuid.UUID, Meta, error) {
	id, err := requiredUUID(idText)
	if err != nil {
		var zero Meta
		return uuid.Nil, zero, err
	}
	meta, err := cast(metaRequest)
	if err != nil {
		var zero Meta
		return uuid.Nil, zero, err
	}
	return id, meta, nil
}

// ListRelationshipsInput maps a gRPC request to the domain read input.
func ListRelationshipsInput(request *providersv1.ListRelationshipsRequest) (providerservice.ListRelationshipsInput, error) {
	meta, err := QueryMetaFromProto(request.GetMeta())
	if err != nil {
		return providerservice.ListRelationshipsInput{}, err
	}
	id, err := optionalUUIDPtr(request.GetWorkItemProjectionId())
	if err != nil {
		return providerservice.ListRelationshipsInput{}, err
	}
	sources, err := relationshipSourcesFromProto(request.GetSources())
	if err != nil {
		return providerservice.ListRelationshipsInput{}, err
	}
	levels, err := relationshipConfidenceLevelsFromProto(request.GetConfidenceLevels())
	if err != nil {
		return providerservice.ListRelationshipsInput{}, err
	}
	return providerservice.ListRelationshipsInput{
		WorkItemProjectionID: id,
		RelationshipTypes:    trimProtoStrings(request.GetRelationshipTypes()),
		Sources:              sources,
		ConfidenceLevels:     levels,
		Page:                 pageRequestFromProto(request.GetPage()),
		Meta:                 meta,
	}, nil
}

// RegisterProviderArtifactSignalInput maps an accelerating signal to the domain command input.
func RegisterProviderArtifactSignalInput(request *providersv1.RegisterProviderArtifactSignalRequest) (providerservice.RegisterProviderArtifactSignalInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return providerservice.RegisterProviderArtifactSignalInput{}, err
	}
	observedAt, err := requiredTime(request.GetObservedAt())
	if err != nil {
		return providerservice.RegisterProviderArtifactSignalInput{}, err
	}
	externalAccountID, err := requiredUUID(request.GetExternalAccountId())
	if err != nil {
		return providerservice.RegisterProviderArtifactSignalInput{}, err
	}
	target := request.GetTarget()
	if target == nil {
		return providerservice.RegisterProviderArtifactSignalInput{}, errs.ErrInvalidArgument
	}
	workItemKind, err := workItemKindFromProto(target.GetWorkItemKind())
	if err != nil {
		return providerservice.RegisterProviderArtifactSignalInput{}, err
	}
	return providerservice.RegisterProviderArtifactSignalInput{
		SignalID:          strings.TrimSpace(request.GetSignalId()),
		ExternalAccountID: externalAccountID,
		Target: providerservice.ProviderArtifactTarget{
			ProviderSlug:         providerSlug(target.GetProviderSlug()),
			RepositoryFullName:   strings.TrimSpace(target.GetRepositoryFullName()),
			ProviderRepositoryID: strings.TrimSpace(target.GetProviderRepositoryId()),
			WorkItemKind:         workItemKind,
			Number:               target.GetNumber(),
			ProviderObjectID:     strings.TrimSpace(target.GetProviderObjectId()),
			WebURL:               strings.TrimSpace(target.GetWebUrl()),
		},
		Source:      strings.TrimSpace(request.GetSource()),
		ObservedAt:  observedAt,
		PayloadJSON: []byte(strings.TrimSpace(request.GetPayloadJson())),
		Meta:        meta,
	}, nil
}

// EnqueueReconciliationInput maps a gRPC request to the domain command input.
func EnqueueReconciliationInput(request *providersv1.EnqueueReconciliationRequest) (providerservice.EnqueueReconciliationInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return providerservice.EnqueueReconciliationInput{}, err
	}
	scopeType, err := syncCursorScopeFromProto(request.GetScopeType())
	if err != nil {
		return providerservice.EnqueueReconciliationInput{}, err
	}
	artifactKinds, err := syncArtifactKindsFromProto(request.GetArtifactKinds())
	if err != nil {
		return providerservice.EnqueueReconciliationInput{}, err
	}
	priority, err := syncCursorPriorityFromProto(request.GetPriority())
	if err != nil {
		return providerservice.EnqueueReconciliationInput{}, err
	}
	externalAccountID, err := requiredUUID(request.GetExternalAccountId())
	if err != nil {
		return providerservice.EnqueueReconciliationInput{}, err
	}
	return providerservice.EnqueueReconciliationInput{
		ProviderSlug:      providerSlug(request.GetProviderSlug()),
		ExternalAccountID: externalAccountID,
		ScopeType:         scopeType,
		ScopeRef:          strings.TrimSpace(request.GetScopeRef()),
		ArtifactKinds:     artifactKinds,
		Priority:          priority,
		Meta:              meta,
	}, nil
}

// RunReconciliationBatchInput maps a gRPC request to the domain command input.
func RunReconciliationBatchInput(request *providersv1.RunReconciliationBatchRequest) (providerservice.RunReconciliationBatchInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return providerservice.RunReconciliationBatchInput{}, err
	}
	syncCursorID, err := optionalUUIDPtr(request.GetSyncCursorId())
	if err != nil {
		return providerservice.RunReconciliationBatchInput{}, err
	}
	externalAccountID, err := optionalUUIDPtr(request.GetExternalAccountId())
	if err != nil {
		return providerservice.RunReconciliationBatchInput{}, err
	}
	return providerservice.RunReconciliationBatchInput{
		SyncCursorID:      syncCursorID,
		ProviderSlug:      providerSlug(request.GetProviderSlug()),
		ExternalAccountID: externalAccountID,
		MaxItems:          request.GetMaxItems(),
		LeaseOwner:        strings.TrimSpace(request.GetLeaseOwner()),
		Meta:              meta,
	}, nil
}

// GetSyncCursorInput maps a gRPC request to the domain read input.
func GetSyncCursorInput(request *providersv1.GetSyncCursorRequest) (providerservice.GetSyncCursorInput, error) {
	id, meta, err := metaAndRequiredUUID(request.GetMeta(), request.GetSyncCursorId(), QueryMetaFromProto)
	input := providerservice.GetSyncCursorInput{SyncCursorID: id, Meta: meta}
	return input, err
}

// ListSyncCursorsInput maps a gRPC request to the domain read input.
func ListSyncCursorsInput(request *providersv1.ListSyncCursorsRequest) (providerservice.ListSyncCursorsInput, error) {
	meta, err := QueryMetaFromProto(request.GetMeta())
	if err != nil {
		return providerservice.ListSyncCursorsInput{}, err
	}
	var scopeType enum.SyncCursorScopeType
	if request.ScopeType != nil {
		scopeType, err = optionalSyncCursorScopeFromProto(request.GetScopeType())
		if err != nil {
			return providerservice.ListSyncCursorsInput{}, err
		}
	}
	artifactKinds, err := syncArtifactKindsFromProto(request.GetArtifactKinds())
	if err != nil {
		return providerservice.ListSyncCursorsInput{}, err
	}
	priorities, err := syncCursorPrioritiesFromProto(request.GetPriorities())
	if err != nil {
		return providerservice.ListSyncCursorsInput{}, err
	}
	externalAccountID, err := optionalUUIDPtr(request.GetExternalAccountId())
	if err != nil {
		return providerservice.ListSyncCursorsInput{}, err
	}
	return providerservice.ListSyncCursorsInput{
		ProviderSlug:      providerSlug(request.GetProviderSlug()),
		ExternalAccountID: externalAccountID,
		ScopeType:         scopeType,
		ScopeRef:          strings.TrimSpace(request.GetScopeRef()),
		ArtifactKinds:     artifactKinds,
		Priorities:        priorities,
		IncludeHealthy:    request.GetIncludeHealthy(),
		Page:              pageRequestFromProto(request.GetPage()),
		Meta:              meta,
	}, nil
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
