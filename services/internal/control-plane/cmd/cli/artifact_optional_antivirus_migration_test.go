package main

import (
	"testing"

	"github.com/pressly/goose/v3"
)

// TestArtifactOptionalAntivirusMigration applies the full canonical chain in an explicitly
// disposable database and checks idempotent up plus disjoint real/no-AV receipt validation.
// TestArtifactOptionalAntivirusMigration применяет всю каноническую цепочку только в явно
// disposable БД, проверяет idempotent up и различие настоящего AV и no-AV receipt.
func TestArtifactOptionalAntivirusMigration(t *testing.T) {
	ctx, database, observer := newWorkflowUpgradeDatabase(t)
	if err := goose.UpToContext(ctx, database, "migrations", 20261109000100); err != nil {
		t.Fatal("prepare optional artifact predecessor", err)
	}
	readControlPlaneMigrationProof(t, ctx, observer, "artifact-optional-antivirus", "precondition")
	if err := goose.UpToContext(ctx, database, "migrations", 20261110000100); err != nil {
		t.Fatal("apply optional artifact admission migration", err)
	}
	if err := goose.UpToContext(ctx, database, "migrations", 20261110000100); err != nil {
		t.Fatal("replay optional artifact admission migration", err)
	}
	readControlPlaneMigrationProof(t, ctx, observer, "artifact-optional-antivirus", "postcondition")
	var valid, forged bool
	if err := observer.QueryRow(ctx, `SELECT
 control_plane.artifact_scan_receipt_valid('{"engine":"format-policy-v1","policy":"artifact-format-only-v1","checkedAt":"2026-01-01T00:00:00Z","verdict":"NOT_SCANNED"}'),
 control_plane.artifact_scan_receipt_valid('{"engine":"format-policy-v1","policy":"artifact-format-only-v1","checkedAt":"2026-01-01T00:00:00Z","verdict":"CLEAN"}')`).Scan(&valid, &forged); err != nil || !valid || forged {
		t.Fatalf("optional receipt distinction: valid=%v forged=%v error=%v", valid, forged, err)
	}
}
