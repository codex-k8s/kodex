package main

import (
	"bytes"
	_ "embed"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

// workspaceArchiveABIDefinition reads exact function identity and all non-body attributes in
// the isolated migration fixture, including OID, ACL, argument names, language and search path.
// workspaceArchiveABIDefinition читает identity и все атрибуты кроме тела в изолированной
// оснастке: OID, ACL, имена аргументов, язык и search_path.
//
//go:embed testdata/workspace_archive_abi_definition.sql
var workspaceArchiveABIDefinition string

// workspaceArchiveABIEmpty proves the bootstrap fixture has no applied migrations or task tables.
// workspaceArchiveABIEmpty доказывает отсутствие применённых миграций и task таблиц в bootstrap оснастке.
//
//go:embed testdata/workspace_archive_abi_empty.sql
var workspaceArchiveABIEmpty string

// TestWorkspaceArchiveABIUpgrade rejects a modified predecessor, preserves all function
// attributes on the exact forward change, and exercises replay.
// TestWorkspaceArchiveABIUpgrade отклоняет изменённый predecessor, сохраняет все атрибуты
// функции при точном переходе и проверяет повторный запуск.
func TestWorkspaceArchiveABIUpgrade(t *testing.T) {
	ctx, database, observer := newWorkflowUpgradeDatabase(t)
	if err := goose.UpToContext(ctx, database, "migrations", 20261111000200); err != nil {
		t.Fatal("prepare workspace archive ABI predecessor", err)
	}
	readControlPlaneMigrationProof(t, ctx, observer, "workspace-archive-abi", "precondition")
	var definition, before, digest string
	if err := observer.QueryRow(ctx, workspaceArchiveABIDefinition).Scan(&definition, &before, &digest); err != nil || digest != "3655b43600ba95332c11ed4926a2f948" {
		t.Fatal("read exact workspace archive predecessor", err)
	}
	changed := strings.Replace(definition, "workspace archive task source is invalid", "synthetic changed predecessor", 1)
	if changed == definition {
		t.Fatal("predecessor drift fixture did not change source")
	}
	if _, err := observer.Exec(ctx, changed); err != nil {
		t.Fatal("prepare isolated predecessor drift", err)
	}
	if err := goose.UpToContext(ctx, database, "migrations", 20261112000100); err == nil || !strings.Contains(err.Error(), "workspace archive runtime ABI predecessor changed") {
		t.Fatal("modified predecessor was accepted", err)
	}
	if _, err := observer.Exec(ctx, definition); err != nil {
		t.Fatal("restore exact disposable predecessor", err)
	}
	if err := goose.UpToContext(ctx, database, "migrations", 20261112000100); err != nil {
		t.Fatal("upgrade workspace archive ABI", err)
	}
	var after string
	if err := observer.QueryRow(ctx, workspaceArchiveABIDefinition).Scan(&definition, &after, &digest); err != nil || before != after || digest != "18927e175e85b3441e667b62fec8044c" {
		t.Fatal("workspace archive ABI changed function attributes or unexpected source", err)
	}
	first := readControlPlaneMigrationProof(t, ctx, observer, "workspace-archive-abi", "postcondition")
	if err := goose.UpToContext(ctx, database, "migrations", 20261112000100); err != nil {
		t.Fatal("repeat workspace archive ABI migration", err)
	}
	if second := readControlPlaneMigrationProof(t, ctx, observer, "workspace-archive-abi", "postcondition"); !bytes.Equal(first, second) {
		t.Fatal("repeat changed workspace archive ABI proof")
	}
}

// TestWorkspaceArchiveABIFreshInstall runs the full migration chain in a separately created
// empty database; a reused upgraded fixture fails the explicit emptiness check.
// TestWorkspaceArchiveABIFreshInstall выполняет всю цепочку в отдельно созданной пустой БД;
// повторно использованная обновлённая оснастка отклоняется проверкой пустоты.
func TestWorkspaceArchiveABIFreshInstall(t *testing.T) {
	ctx, database, observer := newWorkflowUpgradeDatabase(t)
	var empty bool
	if err := observer.QueryRow(ctx, workspaceArchiveABIEmpty).Scan(&empty); err != nil || !empty {
		t.Fatal("fresh workspace archive ABI fixture is not empty", err)
	}
	if err := goose.UpToContext(ctx, database, "migrations", 20261112000100); err != nil {
		t.Fatal("fresh workspace archive ABI installation", err)
	}
	readControlPlaneMigrationProof(t, ctx, observer, "workspace-archive-abi", "postcondition")
}
