package main

import (
	"bytes"
	_ "embed"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

// workspaceUnboundDefinition captures the guarded predecessor and its non-body attributes.
// workspaceUnboundDefinition читает guarded predecessor и его атрибуты помимо тела.
//
//go:embed testdata/workspace_unbound_definition.sql
var workspaceUnboundDefinition string

// TestWorkspaceUnboundStopUpgrade rejects predecessor drift and proves atomic upgrade/replay
// without weakening archive guards or changing existing function ownership and grants.
// TestWorkspaceUnboundStopUpgrade отклоняет predecessor drift и проверяет atomic upgrade/replay
// без ослабления archive guards и изменения ownership/grants существующей функции.
func TestWorkspaceUnboundStopUpgrade(t *testing.T) {
	ctx, database, observer := newWorkflowUpgradeDatabase(t)
	if err := goose.UpToContext(ctx, database, "migrations", 20261112000100); err != nil {
		t.Fatal("prepare unbound stop predecessor", err)
	}
	readControlPlaneMigrationProof(t, ctx, observer, "workspace-unbound-stop", "precondition")
	var definition, before, digest string
	if err := observer.QueryRow(ctx, workspaceUnboundDefinition).Scan(&definition, &before, &digest); err != nil || digest != "cea70dfdf75d0fe5c9a943bfc884bcb2" {
		t.Fatal("read unbound predecessor", err)
	}
	changed := strings.Replace(definition, "session workspace metadata is retained", "synthetic changed predecessor", 1)
	if changed == definition {
		t.Fatal("predecessor drift fixture did not change source")
	}
	if _, err := observer.Exec(ctx, changed); err != nil {
		t.Fatal("prepare predecessor drift", err)
	}
	if err := goose.UpToContext(ctx, database, "migrations", 20261113000100); err == nil || !strings.Contains(err.Error(), "workspace unbound predecessor changed") {
		t.Fatal("modified predecessor accepted", err)
	}
	if _, err := observer.Exec(ctx, definition); err != nil {
		t.Fatal("restore disposable predecessor", err)
	}
	if err := goose.UpToContext(ctx, database, "migrations", 20261113000100); err != nil {
		t.Fatal("upgrade unbound stop", err)
	}
	var after string
	if err := observer.QueryRow(ctx, workspaceUnboundDefinition).Scan(&definition, &after, &digest); err != nil || before != after || digest != "452c9a0632bcfe184e9896ef246fb97a" {
		t.Fatal("unbound upgrade changed function attributes or unexpected source", err)
	}
	first := readControlPlaneMigrationProof(t, ctx, observer, "workspace-unbound-stop", "postcondition")
	if err := goose.UpToContext(ctx, database, "migrations", 20261113000100); err != nil {
		t.Fatal("repeat unbound stop upgrade", err)
	}
	if second := readControlPlaneMigrationProof(t, ctx, observer, "workspace-unbound-stop", "postcondition"); !bytes.Equal(first, second) {
		t.Fatal("repeat changed unbound stop proof")
	}
}

// TestWorkspaceUnboundStopFreshInstall qualifies the complete chain from a separately empty database.
// TestWorkspaceUnboundStopFreshInstall проверяет полную цепочку в отдельно созданной пустой БД.
func TestWorkspaceUnboundStopFreshInstall(t *testing.T) {
	ctx, database, observer := newWorkflowUpgradeDatabase(t)
	var empty bool
	if err := observer.QueryRow(ctx, workspaceArchiveABIEmpty).Scan(&empty); err != nil || !empty {
		t.Fatal("fresh unbound stop fixture is not empty", err)
	}
	if err := goose.UpToContext(ctx, database, "migrations", 20261113000100); err != nil {
		t.Fatal("fresh unbound stop install", err)
	}
	readControlPlaneMigrationProof(t, ctx, observer, "workspace-unbound-stop", "postcondition")
}
