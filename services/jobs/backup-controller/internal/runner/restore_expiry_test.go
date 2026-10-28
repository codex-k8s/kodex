package runner

import (
	"context"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/configspec"
	"github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/s3backup"
)

// TestExpiredHistoricalApprovalCannotMutate denies expired typed approvals before repository, client, lock or observer access, while their exact immutable pins remain valid read identity.
// TestExpiredHistoricalApprovalCannotMutate отклоняет истёкшую типизированную запись до repository/client/lock/observer, сохраняя её точные immutable pins как идентичность чтения.
func TestExpiredHistoricalApprovalCannotMutate(t *testing.T) {
	value, receipt, drill := inspectionFixture()
	now := drill.CompletedAt.Add(48 * time.Hour)
	approval := configspec.RestoreApproval{SchemaVersion: 1, BackupID: drill.BackupID, RestoreID: drill.RestoreID,
		ApprovalID: drill.ApprovalID, TargetSetSHA256: drill.TargetSetSHA256, ExpiresAt: now.Add(-time.Hour)}
	root := &Runner{now: func() time.Time { return now }}
	if _, err := root.InspectRestoreTarget(context.Background(), approval, configspec.RestoreTargets{}, s3backup.TargetCleanup); err == nil {
		t.Fatal("expired typed cleanup authority reached storage")
	}
	if err := root.RestoreDrill(context.Background(), approval, configspec.RestoreTargets{}); err == nil {
		t.Fatal("expired typed restore authority reached storage")
	}
	if _, err := projectRestoreMetadata(value, receipt, drill, approval.RestoreID, approval.ApprovalID, approval.TargetSetSHA256); err != nil {
		t.Fatal("historical identity could not inspect the immutable receipt", err)
	}
	approval.ApprovalID = "foreign-approval"
	if _, err := projectRestoreMetadata(value, receipt, drill, approval.RestoreID, approval.ApprovalID, approval.TargetSetSHA256); err == nil {
		t.Fatal("expiry relaxation accepted foreign immutable pins")
	}
}
