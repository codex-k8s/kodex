package runner

import (
	"context"

	"github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/configspec"
	"github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/s3backup"
)

// PreflightRestore checks target DNS/TLS/auth, database absence, S3 versioning and empty prefix without a lock, intent or write. The approved restore still repeats its own checks; this preflight grants no authority and does not eliminate races.
// PreflightRestore проверяет DNS/TLS/auth, отсутствие БД, versioning и пустой S3 prefix без lock, intent и записи. Подтверждённый restore повторяет свои проверки; предварительное чтение не выдаёт полномочий и не устраняет гонки.
func (runner *Runner) PreflightRestore(ctx context.Context, targets configspec.RestoreTargets) error {
	if err := runner.postgres.CheckRestoreTargets(ctx, targets.Databases); err != nil {
		return err
	}
	client, err := s3backup.NewClient(ctx, targets.ObjectStore)
	if err != nil {
		return err
	}
	if err := client.Check(ctx); err != nil {
		return err
	}
	return client.CheckEmptyDrillTarget(ctx)
}
