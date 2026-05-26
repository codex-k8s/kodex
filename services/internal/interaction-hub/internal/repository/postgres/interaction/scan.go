package interaction

import (
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/enum"
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
