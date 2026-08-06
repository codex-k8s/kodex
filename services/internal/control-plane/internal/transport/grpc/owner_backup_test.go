package grpc

import (
	"strings"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
)

func TestBackupProjectionExcludesPrivateRuntimeEvidence(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	backup := domainrepo.Backup{
		ID: "b2331810-56a3-451b-b08a-bc307a2b44ad", SessionID: "c546a470-cf00-426d-956d-2e3548526634",
		SourceVersion: 9, SourceRuntimeRevisionSHA256: strings.Repeat("a", 64),
		SourceImmutableInputSHA256: strings.Repeat("b", 64), ArchiveSHA256: strings.Repeat("c", 64),
		ProvenanceSHA256: strings.Repeat("d", 64), State: "AVAILABLE", Restorable: true,
		CreatedAt: now, AvailableAt: now, RetainUntil: now.Add(time.Hour), UpdatedAt: now,
	}
	projected, err := backupToProto(backup)
	if err != nil {
		t.Fatalf("backupToProto() error = %v", err)
	}
	fields := projected.ProtoReflect().Descriptor().Fields()
	for _, forbidden := range []string{
		"verificationSha256", "cleanupSha256", "archiveObjectKey", "archiveReference",
		"credential", "workerGrant", "restoreProofReference",
	} {
		if fields.ByJSONName(forbidden) != nil {
			t.Fatalf("public Backup unexpectedly contains %q", forbidden)
		}
	}
	if projected.GetState() != controlplanev1.BackupState_BACKUP_STATE_AVAILABLE ||
		!projected.GetRestorable() || projected.GetScope() != "SESSION" {
		t.Fatalf("unexpected safe projection: %+v", projected)
	}
	backup.ArchiveSHA256 = strings.Repeat("A", 64)
	if _, err = backupToProto(backup); err == nil {
		t.Fatal("non-canonical public digest was accepted")
	}
}

func TestRestoreOperationStateUsesTargetExecution(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		execution  string
		assignment string
		want       controlplanev1.RestoreOperationState
	}{
		{name: "queued", want: controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_QUEUED},
		{name: "assigned", execution: "PENDING", assignment: "ASSIGNED", want: controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_ASSIGNED},
		{name: "materializing", execution: "PENDING", assignment: "BOUND", want: controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_MATERIALIZING},
		{name: "ready", execution: "PENDING", assignment: "CONSUMED", want: controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_READY},
		{name: "running", execution: "RUNNING", want: controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_RUNNING},
		{name: "succeeded", execution: "SUCCEEDED", want: controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_SUCCEEDED},
		{name: "failed", execution: "FAILED", want: controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_FAILED},
		{name: "cancelled", execution: "CANCELLED", want: controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_CANCELLED},
		{name: "expired", execution: "EXPIRED", want: controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_EXPIRED},
		{name: "invalid pending", execution: "PENDING", assignment: "UNKNOWN", want: controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_UNSPECIFIED},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, _ := restoreOperationState(domainrepo.RuntimeRestoreOperation{
				TargetExecutionState: test.execution, TargetRestoreAssignmentState: test.assignment,
			})
			if got != test.want {
				t.Fatalf("restoreOperationState() = %s, want %s", got, test.want)
			}
		})
	}
}
