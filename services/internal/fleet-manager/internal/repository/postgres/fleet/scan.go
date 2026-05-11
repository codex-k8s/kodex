package fleet

import (
	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/entity"
)

func scanOutboxEvent(row postgreslib.RowScanner) (entity.OutboxEvent, error) {
	record, err := postgreslib.ScanOutboxEventRow(row)
	if err != nil {
		return entity.OutboxEvent{}, err
	}
	return entity.OutboxEvent{
		Event: outboxlib.NewEvent(
			record.Identity.RowID,
			record.Identity.TypeName,
			record.Identity.ContractVersion,
			record.Identity.SubjectKind,
			record.Identity.SubjectID,
			record.Body,
			record.Identity.CreatedAt,
			record.Delivery.Attempts,
		),
		PublishedAt:         record.Delivery.SentAt,
		NextAttemptAt:       record.Delivery.RetryAt,
		LockedUntil:         record.Delivery.LeaseUntil,
		FailureKind:         record.Failure.FailureCode,
		FailedPermanentlyAt: record.Failure.DeadAt,
		LastError:           record.Failure.ErrorText,
	}, nil
}
