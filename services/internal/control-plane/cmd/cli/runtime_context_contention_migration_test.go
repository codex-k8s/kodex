package main

import (
	"strings"
	"testing"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/schema"
)

func TestRuntimeContextContentionMigrationKeepsCleanupBackendLocal(t *testing.T) {
	const migrationVersion int64 = 20260814000200
	if schema.CurrentVersion != migrationVersion {
		t.Fatalf("schema fence %d does not match runtime context migration %d", schema.CurrentVersion, migrationVersion)
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
