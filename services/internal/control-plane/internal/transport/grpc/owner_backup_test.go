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
		Version: 9, SourceVersion: 9, SourceRuntimeRevisionSHA256: strings.Repeat("a", 64),
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
		{name: "retried", execution: "RETRIED", want: controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_RETRYING},
		{name: "successor queued", want: controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_RETRYING},
		{name: "cancelled before claim", assignment: "", want: controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_CANCELLED},
		{name: "invalid pending", execution: "PENDING", assignment: "UNKNOWN", want: controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_UNSPECIFIED},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			operation := domainrepo.RuntimeRestoreOperation{
				TargetExecutionState: test.execution, TargetRestoreAssignmentState: test.assignment,
			}
			if test.name == "successor queued" {
				operation.Generation = 2
			}
			if test.name == "cancelled before claim" {
				operation.TargetTurnState = "CANCELLED"
			}
			got, _ := restoreOperationState(operation)
			if got != test.want {
				t.Fatalf("restoreOperationState() = %s, want %s", got, test.want)
			}
		})
	}
}

func TestRestoreOperationNextActionIsSafeForPreTargetTerminal(t *testing.T) {
	t.Parallel()
	preTarget := domainrepo.RuntimeRestoreOperation{}
	if got := restoreOperationNextAction(
		preTarget,
		controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_CANCELLED,
	); got != controlplanev1.RestoreOperationNextAction_RESTORE_OPERATION_NEXT_ACTION_NONE {
		t.Fatalf("pre-target next action = %s, want NONE", got)
	}
	materialized := domainrepo.RuntimeRestoreOperation{
		TargetExecutionID:    "4f2d2178-1b8f-4674-a7ef-479811208528",
		TargetExecutionState: "FAILED",
	}
	if got := restoreOperationNextAction(
		materialized,
		controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_FAILED,
	); got != controlplanev1.RestoreOperationNextAction_RESTORE_OPERATION_NEXT_ACTION_RETRY_RUNTIME {
		t.Fatalf("materialized next action = %s, want RETRY_RUNTIME", got)
	}
	materialized.TargetExecutionState = "CANCELLED"
	if got := restoreOperationNextAction(
		materialized,
		controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_CANCELLED,
	); got != controlplanev1.RestoreOperationNextAction_RESTORE_OPERATION_NEXT_ACTION_NONE {
		t.Fatalf("cancelled next action = %s, want NONE", got)
	}
	materialized.TargetExecutionState, materialized.TargetAttempt = "FAILED", 100
	if got := restoreOperationNextAction(
		materialized,
		controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_FAILED,
	); got != controlplanev1.RestoreOperationNextAction_RESTORE_OPERATION_NEXT_ACTION_NONE {
		t.Fatalf("attempt-cap next action = %s, want NONE", got)
	}
}

func TestRestoreOperationVersionGrowsAcrossSuccessorGeneration(t *testing.T) {
	t.Parallel()
	base := domainrepo.RuntimeRestoreOperation{
		ID: "7d47e1f6-617e-4166-a825-6287bbdd25de", BackupID: "b2331810-56a3-451b-b08a-bc307a2b44ad",
		SourceVersion: 9, SessionID: "c546a470-cf00-426d-956d-2e3548526634",
		ArchiveSHA256: strings.Repeat("a", 64), ProvenanceSHA256: strings.Repeat("b", 64),
		TargetTurnID: "d18a68b1-4ab8-4890-979c-7604b8de2f89", TargetAttempt: 2,
		Generation: 1, TargetExecutionVersion: 41, TargetTurnVersion: 7,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	first, err := restoreOperationToProto(base)
	if err != nil {
		t.Fatalf("first restoreOperationToProto() error = %v", err)
	}
	base.Generation, base.TargetAttempt, base.TargetExecutionVersion = 2, 3, 0
	successor, err := restoreOperationToProto(base)
	if err != nil {
		t.Fatalf("successor restoreOperationToProto() error = %v", err)
	}
	if successor.GetVersion() <= first.GetVersion() {
		t.Fatalf("successor version = %d, first = %d", successor.GetVersion(), first.GetVersion())
	}
}

func TestRestoreOperationVersionGrowsOnPreTargetTurnChange(t *testing.T) {
	t.Parallel()
	operation := domainrepo.RuntimeRestoreOperation{
		ID: "7d47e1f6-617e-4166-a825-6287bbdd25de", BackupID: "b2331810-56a3-451b-b08a-bc307a2b44ad",
		SourceVersion: 9, SessionID: "c546a470-cf00-426d-956d-2e3548526634",
		ArchiveSHA256: strings.Repeat("a", 64), ProvenanceSHA256: strings.Repeat("b", 64),
		TargetTurnID: "d18a68b1-4ab8-4890-979c-7604b8de2f89", TargetAttempt: 2,
		Generation: 1, TargetTurnVersion: 7, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	queued, err := restoreOperationToProto(operation)
	if err != nil {
		t.Fatalf("queued restoreOperationToProto() error = %v", err)
	}
	operation.TargetTurnVersion++
	cancelled, err := restoreOperationToProto(operation)
	if err != nil {
		t.Fatalf("cancelled restoreOperationToProto() error = %v", err)
	}
	if cancelled.GetVersion() <= queued.GetVersion() {
		t.Fatalf("cancelled version = %d, queued = %d", cancelled.GetVersion(), queued.GetVersion())
	}
}
