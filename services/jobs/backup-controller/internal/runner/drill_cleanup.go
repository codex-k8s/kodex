package runner

import (
	"context"
	"errors"
	"path"

	"github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/configspec"
	"github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/s3backup"
)

// InspectRestoreTarget requires the same immutable approval and target fingerprint as the completed drill, then inspects exact receipt-owned target versions. Explicit cleanup uses the existing operation lock and never changes source databases, backup payloads or restore algorithms.
// InspectRestoreTarget требует неизменяемые подтверждение и fingerprint завершённого drill, затем проверяет только принадлежащие квитанции версии цели. Явный cleanup использует штатный lock и не меняет исходные БД, backup payload либо алгоритмы восстановления.
func (runner *Runner) InspectRestoreTarget(ctx context.Context, approval configspec.RestoreApproval, targets configspec.RestoreTargets, mode s3backup.TargetInspectionMode) (remaining int, resultErr error) {
	targetSHA, err := configspec.FingerprintTargets(targets)
	if err != nil || targetSHA != approval.TargetSetSHA256 {
		return 0, errors.New("cleanup target fingerprint mismatches approval")
	}
	backup, receipt, err := runner.repository.LoadVerifiedManifest(ctx, approval.BackupID)
	if err != nil {
		return 0, err
	}
	drill, err := runner.repository.LoadRestoreDrill(ctx, path.Join("backups", approval.BackupID, "restore-drills", approval.RestoreID+".json"), backup)
	if err != nil {
		return 0, err
	}
	if _, err := projectRestoreMetadata(backup, receipt, drill, approval.RestoreID, approval.ApprovalID, targetSHA); err != nil {
		return 0, err
	}
	client, err := s3backup.NewClient(ctx, targets.ObjectStore)
	if err != nil {
		return 0, err
	}
	if err := client.Check(ctx); err != nil {
		return 0, err
	}
	if mode == s3backup.TargetCleanup {
		lock, err := runner.repository.AcquireOperationLock(ctx, "restore", "cleanup-"+approval.RestoreID, runner.now(), operationLockTTL)
		if err != nil {
			return 0, err
		}
		defer func() { resultErr = errors.Join(resultErr, releaseOperationLock(ctx, lock)) }()
	}
	return client.InspectDrillTarget(ctx, drill, mode)
}
