package provider

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
)

func scanAccountRuntimeState(row postgreslib.RowScanner) (entity.ProviderAccountRuntimeState, error) {
	var state entity.ProviderAccountRuntimeState
	var providerSlug, status string
	var lastCheckedAt, lastSuccessAt pgtype.Timestamptz
	err := row.Scan(
		&state.ID,
		&state.ExternalAccountID,
		&providerSlug,
		&status,
		&lastCheckedAt,
		&lastSuccessAt,
		&state.LastErrorCode,
		&state.LastErrorMessage,
		&state.Version,
		&state.CreatedAt,
		&state.UpdatedAt,
	)
	state.ProviderSlug = enum.ProviderSlug(providerSlug)
	state.Status = enum.ProviderAccountRuntimeStatus(status)
	state.LastCheckedAt = timePtrFromPG(lastCheckedAt)
	state.LastSuccessAt = timePtrFromPG(lastSuccessAt)
	return state, err
}

func scanLimitSnapshot(row postgreslib.RowScanner) (entity.ProviderLimitSnapshot, error) {
	var snapshot entity.ProviderLimitSnapshot
	var providerSlug, source string
	var remaining, limitValue pgtype.Int8
	var resetAt pgtype.Timestamptz
	err := row.Scan(
		&snapshot.ID,
		&snapshot.ExternalAccountID,
		&providerSlug,
		&snapshot.LimitClass,
		&remaining,
		&limitValue,
		&resetAt,
		&snapshot.CapturedAt,
		&source,
	)
	snapshot.ProviderSlug = enum.ProviderSlug(providerSlug)
	snapshot.Remaining = int64PtrFromPG(remaining)
	snapshot.LimitValue = int64PtrFromPG(limitValue)
	snapshot.ResetAt = timePtrFromPG(resetAt)
	snapshot.Source = enum.ProviderLimitSource(source)
	return snapshot, err
}

type limitSnapshotWriteResult struct {
	snapshot entity.ProviderLimitSnapshot
	inserted bool
}

func scanLimitSnapshotWriteResult(row postgreslib.RowScanner) (limitSnapshotWriteResult, error) {
	var result limitSnapshotWriteResult
	var providerSlug, source string
	var remaining, limitValue pgtype.Int8
	var resetAt pgtype.Timestamptz
	err := row.Scan(
		&result.snapshot.ID,
		&result.snapshot.ExternalAccountID,
		&providerSlug,
		&result.snapshot.LimitClass,
		&remaining,
		&limitValue,
		&resetAt,
		&result.snapshot.CapturedAt,
		&source,
		&result.inserted,
	)
	result.snapshot.ProviderSlug = enum.ProviderSlug(providerSlug)
	result.snapshot.Remaining = int64PtrFromPG(remaining)
	result.snapshot.LimitValue = int64PtrFromPG(limitValue)
	result.snapshot.ResetAt = timePtrFromPG(resetAt)
	result.snapshot.Source = enum.ProviderLimitSource(source)
	return result, err
}

func scanProviderOperation(row postgreslib.RowScanner) (entity.ProviderOperation, error) {
	var operation entity.ProviderOperation
	var providerSlug, operationType, status string
	var actorID, snapshotID pgtype.UUID
	var finishedAt pgtype.Timestamptz
	err := row.Scan(
		&operation.ID,
		&operation.CommandID,
		&actorID,
		&operation.ExternalAccountID,
		&providerSlug,
		&operationType,
		&operation.TargetRef,
		&status,
		&operation.ResultRef,
		&operation.ErrorCode,
		&operation.ErrorMessage,
		&snapshotID,
		&operation.StartedAt,
		&finishedAt,
		&operation.Version,
		&operation.CreatedAt,
		&operation.UpdatedAt,
	)
	operation.ActorID = postgreslib.UUIDPtrFromPG(actorID)
	operation.ProviderSlug = enum.ProviderSlug(providerSlug)
	operation.OperationType = enum.ProviderOperationType(operationType)
	operation.Status = enum.ProviderOperationStatus(status)
	operation.RateLimitSnapshotID = postgreslib.UUIDPtrFromPG(snapshotID)
	operation.FinishedAt = timePtrFromPG(finishedAt)
	return operation, err
}

func timePtrFromPG(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	converted := value.Time.UTC()
	return &converted
}

func int64PtrFromPG(value pgtype.Int8) *int64 {
	if !value.Valid {
		return nil
	}
	converted := value.Int64
	return &converted
}
