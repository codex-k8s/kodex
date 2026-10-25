package runner

import (
	"context"
	"errors"
	"fmt"

	"github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/s3backup"
)

// VerificationReadback binds a read-only payload inspection to the immutable verified backup and observed repository lock.
// VerificationReadback связывает проверку данных без записи с неизменяемой подтверждённой копией и наблюдаемой блокировкой репозитория.
type VerificationReadback struct {
	Backup           BackupMetadata          `json:"backup"`
	Lock             s3backup.OperationState `json:"lock"`
	PayloadsVerified bool                    `json:"payloadsVerified"`
}

// InspectVerification reads the existing verification binding and every exact-version payload without producing any receipt or external write; concurrent lock changes reject the projection.
// InspectVerification читает прежнюю связь verification и все данные точных версий без новой квитанции и внешней записи; смена блокировки закрывает результат.
func (runner *Runner) InspectVerification(ctx context.Context, backupID string) (VerificationReadback, error) {
	before, err := runner.repository.InspectOperation(ctx, runner.now())
	if err != nil {
		return VerificationReadback{}, fmt.Errorf("inspect backup operation: %w", err)
	}
	value, receipt, err := runner.repository.LoadVerifiedManifest(ctx, backupID)
	if err != nil {
		return VerificationReadback{}, fmt.Errorf("inspect backup verification binding: %w", err)
	}
	if err := runner.repository.Verify(ctx, value, receipt, runner.workDirectory); err != nil {
		return VerificationReadback{}, fmt.Errorf("inspect backup payloads: %w", err)
	}
	after, err := runner.repository.InspectOperation(ctx, runner.now())
	if err != nil {
		return VerificationReadback{}, fmt.Errorf("inspect backup operation readback: %w", err)
	}
	if before != after {
		return VerificationReadback{}, errors.New("backup operation changed during inspection")
	}
	metadata, err := projectBackupMetadata(value, receipt)
	if err != nil {
		return VerificationReadback{}, err
	}
	return VerificationReadback{Backup: metadata, Lock: after, PayloadsVerified: true}, nil
}
