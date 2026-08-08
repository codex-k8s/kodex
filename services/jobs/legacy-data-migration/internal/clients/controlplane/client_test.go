package controlplane

import (
	"testing"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
)

func TestValidateMigrationRequiresCompleteCommittedOperationReadback(t *testing.T) {
	t.Parallel()
	migration := committedMigration()
	if err := validateMigration(migration, "plan-1", 2, true); err != nil {
		t.Fatalf("полный readback отклонён: %v", err)
	}

	migration.OperationReceipts[1].ProvenanceEvidenceSha256 = ""
	if err := validateMigration(migration, "plan-1", 2, true); err == nil {
		t.Fatal("отсутствующее provenance evidence должно закрыто блокировать readback")
	}
}

func TestValidateMigrationRejectsDuplicateCommittedOrdinal(t *testing.T) {
	t.Parallel()
	migration := committedMigration()
	migration.OperationReceipts[1].Ordinal = 1
	if err := validateMigration(migration, "plan-1", 2, true); err == nil {
		t.Fatal("повтор ordinal должен закрыто блокировать readback")
	}
}

func committedMigration() *controlplanev1.LegacyGraphMigration {
	return &controlplanev1.LegacyGraphMigration{
		PlanId: "plan-1", State: controlplanev1.LegacyGraphMigrationState_LEGACY_GRAPH_MIGRATION_STATE_COMMITTED,
		VerificationState: controlplanev1.LegacyGraphVerificationState_LEGACY_GRAPH_VERIFICATION_STATE_VERIFIED,
		SemanticSha256:    "semantic", SourceSnapshotSha256: "snapshot", OperationCount: 2,
		OperationReceipts: []*controlplanev1.LegacyOperationReceipt{
			committedReceipt(1, "PROJECT"), committedReceipt(2, "PROCESS_RUN"),
		},
	}
}

func committedReceipt(ordinal uint32, kind string) *controlplanev1.LegacyOperationReceipt {
	return &controlplanev1.LegacyOperationReceipt{
		Ordinal: ordinal, OperationKind: kind, InputSha256: "input", TargetId: "target",
		TargetKind: kind, TargetVersion: 1, TargetState: controlplanev1.LifecycleState_LIFECYCLE_STATE_ACTIVE,
		ProjectionSha256: "projection",
		ProvenanceSha256: "provenance", ProvenanceEvidenceSha256: "evidence", AuditIds: []string{"audit"},
	}
}
