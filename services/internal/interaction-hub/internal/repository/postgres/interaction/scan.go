package interaction

import (
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/value"
)

func scanThread(row postgreslib.RowScanner) (entity.ConversationThread, error) {
	var thread entity.ConversationThread
	var latestMessageID pgtype.UUID
	var closedAt pgtype.Timestamptz
	var scopeType, threadKind, sourceKind, status string
	err := row.Scan(
		&thread.ID,
		&scopeType,
		&thread.Scope.Ref,
		&threadKind,
		&thread.PrimaryActorRef,
		&sourceKind,
		&thread.SourceRef,
		&status,
		&latestMessageID,
		&thread.CorrelationID,
		&thread.RetentionClass,
		&thread.Version,
		&thread.CreatedAt,
		&thread.UpdatedAt,
		&closedAt,
	)
	thread.Scope.Type = enum.ScopeType(scopeType)
	thread.ThreadKind = enum.ConversationThreadKind(threadKind)
	thread.SourceKind = enum.ConversationSourceKind(sourceKind)
	thread.Status = enum.ConversationThreadStatus(status)
	thread.LatestMessageID = postgreslib.UUIDPtrFromPG(latestMessageID)
	thread.ClosedAt = postgreslib.TimePtrFromPG(closedAt)
	return thread, err
}

func scanMessage(row postgreslib.RowScanner) (entity.ConversationMessage, error) {
	var message entity.ConversationMessage
	var messageKind string
	var objectSize pgtype.Int8
	var safeMetadata []byte
	err := row.Scan(
		&message.ID,
		&message.ThreadID,
		&messageKind,
		&message.AuthorRef,
		&message.BodySummary,
		&message.BodyObject.URI,
		&message.BodyObject.Digest,
		&objectSize,
		&message.BodyDigest,
		&message.Locale,
		&safeMetadata,
		&message.CreatedAt,
	)
	message.MessageKind = enum.ConversationMessageKind(messageKind)
	if objectSize.Valid {
		value := objectSize.Int64
		message.BodyObject.SizeBytes = &value
	}
	if err != nil {
		return message, err
	}
	if len(safeMetadata) == 0 {
		message.SafeMetadata = map[string]string{}
		return message, nil
	}
	if err := json.Unmarshal(safeMetadata, &message.SafeMetadata); err != nil {
		return message, err
	}
	if message.SafeMetadata == nil {
		message.SafeMetadata = map[string]string{}
	}
	return message, nil
}

func scanRequest(row postgreslib.RowScanner) (entity.InteractionRequest, error) {
	var request entity.InteractionRequest
	var requestKind, scopeType, sourceOwnerKind, ingressKind, decisionOwnerKind, riskClass, status string
	var threadID pgtype.UUID
	var promptObjectSize pgtype.Int8
	var targetRefs, contextRefs, allowedActions []byte
	var deadlineAt, resolvedAt pgtype.Timestamptz
	err := row.Scan(
		&request.ID,
		&requestKind,
		&scopeType,
		&request.Scope.Ref,
		&threadID,
		&sourceOwnerKind,
		&request.SourceOwner.Ref,
		&ingressKind,
		&request.Ingress.Ref,
		&decisionOwnerKind,
		&request.DecisionOwner.OwnerRequestRef,
		&request.DecisionOwner.OwnerDecisionRef,
		&targetRefs,
		&contextRefs,
		&request.PromptSummary,
		&request.PromptObject.URI,
		&request.PromptObject.Digest,
		&promptObjectSize,
		&allowedActions,
		&riskClass,
		&status,
		&deadlineAt,
		&request.ReminderPolicyRef,
		&request.Version,
		&request.CreatedAt,
		&request.UpdatedAt,
		&resolvedAt,
	)
	request.RequestKind = enum.InteractionRequestKind(requestKind)
	request.Scope.Type = enum.ScopeType(scopeType)
	request.ThreadID = postgreslib.UUIDPtrFromPG(threadID)
	request.SourceOwner.Kind = enum.SourceOwnerKind(sourceOwnerKind)
	request.Ingress.Kind = enum.IngressKind(ingressKind)
	request.DecisionOwner.Kind = enum.DecisionOwnerKind(decisionOwnerKind)
	if promptObjectSize.Valid {
		value := promptObjectSize.Int64
		request.PromptObject.SizeBytes = &value
	}
	request.RiskClass = enum.InteractionRiskClass(riskClass)
	request.Status = enum.InteractionRequestStatus(status)
	request.DeadlineAt = postgreslib.TimePtrFromPG(deadlineAt)
	request.ResolvedAt = postgreslib.TimePtrFromPG(resolvedAt)
	if err != nil {
		return request, err
	}
	if request.TargetRefs, err = unmarshalArray[value.ActorRef](targetRefs); err != nil {
		return request, err
	}
	if request.ContextRefs, err = unmarshalArray[value.ExternalRef](contextRefs); err != nil {
		return request, err
	}
	if request.AllowedActions, err = unmarshalArray[value.InteractionAction](allowedActions); err != nil {
		return request, err
	}
	return request, nil
}

func scanResponse(row postgreslib.RowScanner) (entity.InteractionResponse, error) {
	var response entity.InteractionResponse
	var responseAction, sourceKind string
	var responseObjectSize pgtype.Int8
	err := row.Scan(
		&response.ID,
		&response.RequestID,
		&responseAction,
		&response.RespondedByActorRef,
		&response.ResponseSummary,
		&response.ResponseObject.URI,
		&response.ResponseObject.Digest,
		&responseObjectSize,
		&sourceKind,
		&response.SourceRef,
		&response.OwnerDecisionRef,
		&response.CreatedAt,
	)
	response.ResponseAction = enum.InteractionResponseAction(responseAction)
	response.SourceKind = enum.InteractionResponseSourceKind(sourceKind)
	if responseObjectSize.Valid {
		value := responseObjectSize.Int64
		response.ResponseObject.SizeBytes = &value
	}
	return response, err
}

func scanCommandResult(row postgreslib.RowScanner) (entity.CommandResult, error) {
	var result entity.CommandResult
	var commandID pgtype.UUID
	var operation string
	var resultPayload []byte
	err := row.Scan(
		&result.Key,
		&commandID,
		&result.IdempotencyKey,
		&result.ActorRef,
		&operation,
		&result.AggregateType,
		&result.AggregateID,
		&result.RequestFingerprint,
		&resultPayload,
		&result.CreatedAt,
	)
	if commandID.Valid {
		result.CommandID = uuid.UUID(commandID.Bytes)
	}
	result.Operation = enum.Operation(operation)
	result.ResultPayload = append(result.ResultPayload[:0], resultPayload...)
	return result, err
}

func scanOutboxEvent(row postgreslib.RowScanner) (entity.OutboxEvent, error) {
	raw, err := postgreslib.ScanOutboxEventRow(row)
	if err != nil {
		return entity.OutboxEvent{}, err
	}
	return entity.OutboxEvent{
		Event: outboxlib.Event{
			ID:            raw.Identity.RowID,
			EventType:     raw.Identity.TypeName,
			SchemaVersion: raw.Identity.ContractVersion,
			AggregateType: raw.Identity.SubjectKind,
			AggregateID:   raw.Identity.SubjectID,
			Payload:       raw.Body,
			OccurredAt:    raw.Identity.CreatedAt,
			AttemptCount:  raw.Delivery.Attempts,
		},
		PublishedAt:         raw.Delivery.SentAt,
		NextAttemptAt:       raw.Delivery.RetryAt,
		LockedUntil:         raw.Delivery.LeaseUntil,
		FailedPermanentlyAt: raw.Failure.DeadAt,
		FailureKind:         raw.Failure.FailureCode,
		LastError:           raw.Failure.ErrorText,
	}, nil
}

func unmarshalArray[T any](input []byte) ([]T, error) {
	if len(input) == 0 || string(input) == "null" {
		return []T{}, nil
	}
	var result []T
	if err := json.Unmarshal(input, &result); err != nil {
		return nil, err
	}
	if result == nil {
		return []T{}, nil
	}
	return result, nil
}
