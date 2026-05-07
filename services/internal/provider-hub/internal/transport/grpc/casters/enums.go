package casters

import (
	providersv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/providers/v1"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
)

type domainEnum interface {
	~string
}

var runtimeStatuses = map[providersv1.ProviderAccountRuntimeStatus]enum.ProviderAccountRuntimeStatus{
	providersv1.ProviderAccountRuntimeStatus_PROVIDER_ACCOUNT_RUNTIME_STATUS_ACTIVE:                   enum.ProviderAccountRuntimeStatusActive,
	providersv1.ProviderAccountRuntimeStatus_PROVIDER_ACCOUNT_RUNTIME_STATUS_REAUTHORIZATION_REQUIRED: enum.ProviderAccountRuntimeStatusReauthorizationRequired,
	providersv1.ProviderAccountRuntimeStatus_PROVIDER_ACCOUNT_RUNTIME_STATUS_LIMITED:                  enum.ProviderAccountRuntimeStatusLimited,
	providersv1.ProviderAccountRuntimeStatus_PROVIDER_ACCOUNT_RUNTIME_STATUS_DISABLED:                 enum.ProviderAccountRuntimeStatusDisabled,
	providersv1.ProviderAccountRuntimeStatus_PROVIDER_ACCOUNT_RUNTIME_STATUS_ERROR:                    enum.ProviderAccountRuntimeStatusError,
}

var webhookStatuses = map[providersv1.WebhookProcessingStatus]enum.WebhookProcessingStatus{
	providersv1.WebhookProcessingStatus_WEBHOOK_PROCESSING_STATUS_PENDING:   enum.WebhookProcessingStatusPending,
	providersv1.WebhookProcessingStatus_WEBHOOK_PROCESSING_STATUS_PROCESSED: enum.WebhookProcessingStatusProcessed,
	providersv1.WebhookProcessingStatus_WEBHOOK_PROCESSING_STATUS_FAILED:    enum.WebhookProcessingStatusFailed,
	providersv1.WebhookProcessingStatus_WEBHOOK_PROCESSING_STATUS_IGNORED:   enum.WebhookProcessingStatusIgnored,
}

var operationTypes = map[providersv1.ProviderOperationType]enum.ProviderOperationType{
	providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_CREATE_ISSUE:         enum.ProviderOperationCreateIssue,
	providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_UPDATE_ISSUE:         enum.ProviderOperationUpdateIssue,
	providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_CREATE_COMMENT:       enum.ProviderOperationCreateComment,
	providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_UPDATE_COMMENT:       enum.ProviderOperationUpdateComment,
	providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_CREATE_PULL_REQUEST:  enum.ProviderOperationCreatePullRequest,
	providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_CREATE_REVIEW_SIGNAL: enum.ProviderOperationCreateReviewSignal,
	providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_UPDATE_RELATIONSHIP:  enum.ProviderOperationUpdateRelationship,
}

var operationStatuses = map[providersv1.ProviderOperationStatus]enum.ProviderOperationStatus{
	providersv1.ProviderOperationStatus_PROVIDER_OPERATION_STATUS_SUCCEEDED:        enum.ProviderOperationStatusSucceeded,
	providersv1.ProviderOperationStatus_PROVIDER_OPERATION_STATUS_FAILED:           enum.ProviderOperationStatusFailed,
	providersv1.ProviderOperationStatus_PROVIDER_OPERATION_STATUS_RETRYABLE_FAILED: enum.ProviderOperationStatusRetryableFailed,
	providersv1.ProviderOperationStatus_PROVIDER_OPERATION_STATUS_DENIED:           enum.ProviderOperationStatusDenied,
}

func runtimeStatusesFromProto(statuses []providersv1.ProviderAccountRuntimeStatus) ([]enum.ProviderAccountRuntimeStatus, error) {
	return enumsFromProto(statuses, providersv1.ProviderAccountRuntimeStatus_PROVIDER_ACCOUNT_RUNTIME_STATUS_UNSPECIFIED, runtimeStatuses)
}

func RuntimeStatusToProto(status enum.ProviderAccountRuntimeStatus) providersv1.ProviderAccountRuntimeStatus {
	return enumToProto(status, providersv1.ProviderAccountRuntimeStatus_PROVIDER_ACCOUNT_RUNTIME_STATUS_UNSPECIFIED, invertEnum(runtimeStatuses))
}

func webhookStatusesFromProto(statuses []providersv1.WebhookProcessingStatus) ([]enum.WebhookProcessingStatus, error) {
	return enumsFromProto(statuses, providersv1.WebhookProcessingStatus_WEBHOOK_PROCESSING_STATUS_UNSPECIFIED, webhookStatuses)
}

func WebhookStatusToProto(status enum.WebhookProcessingStatus) providersv1.WebhookProcessingStatus {
	return enumToProto(status, providersv1.WebhookProcessingStatus_WEBHOOK_PROCESSING_STATUS_UNSPECIFIED, invertEnum(webhookStatuses))
}

func operationTypesFromProto(types []providersv1.ProviderOperationType) ([]enum.ProviderOperationType, error) {
	return enumsFromProto(types, providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_UNSPECIFIED, operationTypes)
}

func OperationTypeToProto(operationType enum.ProviderOperationType) providersv1.ProviderOperationType {
	return enumToProto(operationType, providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_UNSPECIFIED, invertEnum(operationTypes))
}

func operationStatusesFromProto(statuses []providersv1.ProviderOperationStatus) ([]enum.ProviderOperationStatus, error) {
	return enumsFromProto(statuses, providersv1.ProviderOperationStatus_PROVIDER_OPERATION_STATUS_UNSPECIFIED, operationStatuses)
}

func OperationStatusToProto(status enum.ProviderOperationStatus) providersv1.ProviderOperationStatus {
	return enumToProto(status, providersv1.ProviderOperationStatus_PROVIDER_OPERATION_STATUS_UNSPECIFIED, invertEnum(operationStatuses))
}

func enumsFromProto[Proto comparable, Domain domainEnum](values []Proto, unspecified Proto, mapping map[Proto]Domain) ([]Domain, error) {
	result := make([]Domain, 0, len(values))
	for _, value := range values {
		if value == unspecified {
			return nil, errs.ErrInvalidArgument
		}
		mapped, ok := mapping[value]
		if !ok {
			return nil, errs.ErrInvalidArgument
		}
		result = append(result, mapped)
	}
	return result, nil
}

func enumToProto[Domain comparable, Proto any](value Domain, unspecified Proto, mapping map[Domain]Proto) Proto {
	mapped, ok := mapping[value]
	if !ok {
		return unspecified
	}
	return mapped
}

func invertEnum[Proto comparable, Domain comparable](mapping map[Proto]Domain) map[Domain]Proto {
	inverted := make(map[Domain]Proto, len(mapping))
	for protoValue, domainValue := range mapping {
		inverted[domainValue] = protoValue
	}
	return inverted
}
