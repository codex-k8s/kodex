package controlplane

import (
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestLegacyRPCDiagnosticExposesOnlyBoundedTrustedCode(t *testing.T) {
	current := status.New(codes.DataLoss, "control-plane stored data is corrupt")
	withDetail, err := current.WithDetails(&controlplanev1.ErrorDetail{Code: "LEGACY_EVIDENCE_EVENTS_CHAT_3"})
	if err != nil {
		t.Fatal(err)
	}
	diagnostic := legacyRPCDiagnostic(withDetail.Err())
	if diagnostic == nil || !strings.Contains(diagnostic.Error(), "LEGACY_EVIDENCE_EVENTS_CHAT_3") {
		t.Fatalf("trusted safe code is absent: %v", diagnostic)
	}
	unsafe, err := current.WithDetails(&controlplanev1.ErrorDetail{Code: "LEGACY_secret-value"})
	if err != nil {
		t.Fatal(err)
	}
	if diagnostic = legacyRPCDiagnostic(unsafe.Err()); strings.Contains(diagnostic.Error(), "LEGACY_secret-value") {
		t.Fatal("unbounded diagnostic code was exposed")
	}
}

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

func TestValidateMigrationRequiresCompletePreparedOperationReadback(t *testing.T) {
	t.Parallel()
	migration := committedMigration()
	migration.State = controlplanev1.LegacyGraphMigrationState_LEGACY_GRAPH_MIGRATION_STATE_PREPARED
	if err := validateMigration(migration, "plan-1", 2, false); err != nil {
		t.Fatalf("полный readback PREPARED отклонён: %v", err)
	}

	migration.OperationReceipts = migration.OperationReceipts[:1]

	if err := validateMigration(migration, "plan-1", 2, false); err == nil {
		t.Fatal("неполный readback PREPARED должен закрыто блокировать импорт")
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
