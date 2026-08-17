package main

import (
	"strings"
	"testing"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/schema"
)

func TestRuntimeContextContentionMigrationKeepsCleanupBackendLocal(t *testing.T) {
	const migrationVersion int64 = 20260814000200
	if schema.CurrentVersion < migrationVersion {
		t.Fatalf("schema fence %d excludes runtime context migration %d", schema.CurrentVersion, migrationVersion)
	}
	raw, err := migrations.ReadFile("migrations/20260814000200_control_plane_runtime_context_contention.sql")
	if err != nil {
		t.Fatalf("read runtime context migration: %v", err)
	}
	source := string(raw)
	for _, expected := range []string{
		"CREATE OR REPLACE FUNCTION control_plane.activate_runtime_context",
		"WHERE backend_pid = pg_backend_pid()",
		"AND transaction_id <> txid_current()",
		"stored_version <> 20260814000100",
		"SET version = 20260814000200",
	} {
		if !strings.Contains(source, expected) {
			t.Fatalf("runtime context migration missed invariant %q", expected)
		}
	}
	functionStart := strings.Index(source, "CREATE OR REPLACE FUNCTION control_plane.activate_runtime_context")
	if functionStart < 0 {
		t.Fatal("runtime context function is absent")
	}
	functionBody := source[functionStart:]
	if strings.Contains(functionBody, "WHERE ctid IN") || strings.Contains(functionBody, "ORDER BY expired.expires_at") {
		t.Fatal("runtime context activation must not perform global cleanup")
	}
}

func TestRuntimeContextColumnMigrationQualifiesCleanupColumns(t *testing.T) {
	const migrationVersion int64 = 20260814000300
	if schema.CurrentVersion < migrationVersion {
		t.Fatalf("schema fence %d excludes runtime context column migration %d", schema.CurrentVersion, migrationVersion)
	}
	raw, err := migrations.ReadFile("migrations/20260814000300_control_plane_runtime_context_column.sql")
	if err != nil {
		t.Fatalf("read runtime context column migration: %v", err)
	}
	source := string(raw)
	for _, expected := range []string{
		"DELETE FROM control_plane.runtime_transaction_contexts AS runtime_context",
		"runtime_context.backend_pid = pg_backend_pid()",
		"runtime_context.transaction_id <> txid_current()",
		"runtime_context.expires_at < clock_timestamp()",
		"context_expires_at timestamptz",
		"stored_version <> 20260814000200",
		"SET version = 20260814000300",
	} {
		if !strings.Contains(source, expected) {
			t.Fatalf("runtime context column migration missed invariant %q", expected)
		}
	}
}
