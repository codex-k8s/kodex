package main

import (
	"strings"
	"testing"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/schema"
)

func TestOwnerReadRepairMigrationPinsDiagnosticsContract(t *testing.T) {
	const migrationVersion int64 = 20260817000200
	if schema.CurrentVersion != migrationVersion {
		t.Fatalf("schema fence %d does not match owner read repair migration %d", schema.CurrentVersion, migrationVersion)
	}
	raw, err := migrations.ReadFile("migrations/20260817000200_control_plane_owner_read_repair.sql")
	if err != nil {
		t.Fatalf("read owner read repair migration: %v", err)
	}
	source := string(raw)
	for _, expected := range []string{
		"CREATE OR REPLACE FUNCTION control_plane.safe_diagnostics()",
		"0)::double precision",
		"stored_version IS DISTINCT FROM 20260817000100",
		"SET version = 20260817000200",
	} {
		if !strings.Contains(source, expected) {
			t.Fatalf("owner read repair migration missed invariant %q", expected)
		}
	}
	if strings.Contains(source, "DISABLE ROW LEVEL SECURITY") ||
		strings.Contains(source, "NO FORCE ROW LEVEL SECURITY") ||
		strings.Contains(source, "BYPASSRLS") {
		t.Fatal("owner read repair migration weakens RLS")
	}
}
