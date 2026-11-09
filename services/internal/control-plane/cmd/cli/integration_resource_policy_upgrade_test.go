package main

import (
	"github.com/pressly/goose/v3"
	"testing"
)

// TestIntegrationResourcePolicyOwnerRegressionSeed applies the exact036 predecessor proof then the new forward migration in a disposable loopback database; it does not qualify portable schema admission.
// TestIntegrationResourcePolicyOwnerRegressionSeed проверяет exact036 predecessor и новую forward migration в disposable loopback БД; это не qualification portable schema admission.
func TestIntegrationResourcePolicyOwnerRegressionSeed(t *testing.T) {
	ctx, database, observer := newWorkflowUpgradeDatabase(t)
	if err := goose.UpToContext(ctx, database, "migrations", 20261108000100); err != nil {
		t.Fatal("prepare resource policy predecessor", err)
	}
	readControlPlaneMigrationProof(t, ctx, observer, "authority-proof-counter", "postcondition")
	if err := goose.UpToContext(ctx, database, "migrations", 20261109000100); err != nil {
		t.Fatal("apply resource policy migration", err)
	}
	if err := goose.UpToContext(ctx, database, "migrations", 20261109000100); err != nil {
		t.Fatal("repeat resource policy Up", err)
	}
	var columns int
	if err := observer.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema='control_plane' AND table_name='integration_approval_scopes' AND column_name IN ('scope_lifetime','initiator_id','version')`).Scan(&columns); err != nil || columns != 3 {
		t.Fatal("resource policy columns unavailable", err)
	}
}
