package runtime

import (
	"github.com/jackc/pgx/v5/pgtype"

	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/entity"
)

func scanOutboxEvent(row postgreslib.RowScanner) (entity.OutboxEvent, error) {
	var event entity.OutboxEvent
	var publishedAt pgtype.Timestamptz
	var lockedUntil pgtype.Timestamptz
	var failedPermanentlyAt pgtype.Timestamptz
	err := row.Scan(
		&event.ID,
		&event.EventType,
		&event.SchemaVersion,
		&event.AggregateType,
		&event.AggregateID,
		&event.Payload,
		&event.OccurredAt,
		&publishedAt,
		&event.AttemptCount,
		&event.NextAttemptAt,
		&lockedUntil,
		&failedPermanentlyAt,
		&event.FailureKind,
		&event.LastError,
	)
	event.PublishedAt = postgreslib.TimePtrFromPG(publishedAt)
	event.LockedUntil = postgreslib.TimePtrFromPG(lockedUntil)
	event.FailedPermanentlyAt = postgreslib.TimePtrFromPG(failedPermanentlyAt)
	return event, err
}
