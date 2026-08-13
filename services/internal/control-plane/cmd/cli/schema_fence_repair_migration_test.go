package main

import (
	"strings"
	"testing"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/schema"
)

func TestSchemaFenceRepairSupportsAppliedAndFreshDatabases(t *testing.T) {
	const migrationVersion int64 = 20260813000100
	if schema.CurrentVersion < migrationVersion {
		t.Fatalf("schema fence %d excludes repair migration %d", schema.CurrentVersion, migrationVersion)
	}

	raw, err := migrations.ReadFile("migrations/20260813000100_control_plane_schema_fence_repair.sql")
	if err != nil {
		t.Fatalf("read schema fence repair migration: %v", err)
	}
	statement := string(raw)
	for _, invariant := range []string{
		"stored_version NOT IN (20260809026310, 20260812000100)",
		"SET version = 20260813000100",
		"FOR UPDATE",
	} {
		if !strings.Contains(statement, invariant) {
			t.Fatalf("schema fence repair missed invariant %q", invariant)
		}
	}
}
