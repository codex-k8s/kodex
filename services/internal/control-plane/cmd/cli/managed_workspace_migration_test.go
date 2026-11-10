package main

import (
	"bytes"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestManagedWorkspaceColumnUpgrade exercises the exact038→039 transition and replay in the
// existing dedicated disposable harness. It adds no backup grants, dump or bootstrap workaround.
// TestManagedWorkspaceColumnUpgrade проверяет exact038→039 transition и replay в существующей
// dedicated disposable оснастке без backup grants, dump и bootstrap workaround.
func TestManagedWorkspaceColumnUpgrade(t *testing.T) {
	ctx, database, observer := newWorkflowUpgradeDatabase(t)
	if err := goose.UpToContext(ctx, database, "migrations", 20261110000100); err != nil {
		t.Fatal("prepare managed workspace predecessor", err)
	}
	readControlPlaneMigrationProof(t, ctx, observer, "managed-workspace", "precondition")
	if err := goose.UpToContext(ctx, database, "migrations", 20261110000200); err != nil {
		t.Fatal("apply managed workspace column", err)
	}
	first := readControlPlaneMigrationProof(t, ctx, observer, "managed-workspace", "postcondition")
	if err := goose.UpToContext(ctx, database, "migrations", 20261110000200); err != nil {
		t.Fatal("replay managed workspace column", err)
	}
	second := readControlPlaneMigrationProof(t, ctx, observer, "managed-workspace", "postcondition")
	if !bytes.Equal(first, second) {
		t.Fatal("no-op migration changed schema proof")
	}
}
