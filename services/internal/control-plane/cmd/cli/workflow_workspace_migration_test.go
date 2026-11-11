package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestWorkflowWorkspaceScopeUpgrade executes the real039→040 transition and its no-op replay
// in the existing disposable harness; it does not touch application data or backup permissions.
// TestWorkflowWorkspaceScopeUpgrade исполняет реальный переход039→040 и его no-op replay
// в существующей disposable оснастке без изменения рабочих данных и backup permissions.
func TestWorkflowWorkspaceScopeUpgrade(t *testing.T) {
	ctx, database, observer := newWorkflowUpgradeDatabase(t)
	if err := goose.UpToContext(ctx, database, "migrations", 20261110000200); err != nil {
		t.Fatal("prepare workflow workspace predecessor", err)
	}
	readControlPlaneMigrationProof(t, ctx, observer, "workflow-workspace", "precondition")
	readiness, err := os.ReadFile(filepath.Join("..", "..", "internal", "repository", "postgres", "platform", "sql", "workspace_storage__readiness.sql"))
	if err != nil {
		t.Fatal("read shipped workspace readiness query", err)
	}
	var ready bool
	if err := observer.QueryRow(ctx, string(readiness)).Scan(&ready); err != nil || ready {
		t.Fatal("workspace readiness admitted the predecessor schema", err)
	}
	if err := goose.UpToContext(ctx, database, "migrations", 20261111000100); err != nil {
		t.Fatal("apply workflow workspace scope", err)
	}
	first := readControlPlaneMigrationProof(t, ctx, observer, "workflow-workspace", "postcondition")
	if err := observer.QueryRow(ctx, string(readiness)).Scan(&ready); err != nil || !ready {
		t.Fatal("workspace readiness rejected the migrated schema", err)
	}
	if err := goose.UpToContext(ctx, database, "migrations", 20261111000100); err != nil {
		t.Fatal("replay workflow workspace scope", err)
	}
	second := readControlPlaneMigrationProof(t, ctx, observer, "workflow-workspace", "postcondition")
	if !bytes.Equal(first, second) {
		t.Fatal("no-op migration changed workflow workspace schema proof")
	}
}
