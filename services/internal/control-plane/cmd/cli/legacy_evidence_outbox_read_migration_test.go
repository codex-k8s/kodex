package main

import (
	"strings"
	"testing"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/schema"
)

func TestLegacyEvidenceOutboxReadMigrationKeepsColumnScope(t *testing.T) {
	const migrationVersion int64 = 20260820000100
	if schema.CurrentVersion != migrationVersion {
		t.Fatalf("schema fence %d does not match legacy evidence migration %d", schema.CurrentVersion, migrationVersion)
	}
	raw, err := migrations.ReadFile("migrations/20260820000100_legacy_evidence_outbox_read.sql")
	if err != nil {
		t.Fatalf("read legacy evidence outbox migration: %v", err)
	}
	source := string(raw)
	for _, expected := range []string{
		"aggregate_type",
		"aggregate_version",
		"correlation_id",
		"causation_id",
		"available_at",
		"envelope",
		"ON control_plane.outbox_events TO control_plane_runtime",
		"stored_version IS DISTINCT FROM 20260817000200",
		"SET version = 20260820000100",
	} {
		if !strings.Contains(source, expected) {
			t.Fatalf("legacy evidence outbox migration missed invariant %q", expected)
		}
	}
	for _, forbidden := range []string{
		"GRANT SELECT ON control_plane.outbox_events",
		"DISABLE ROW LEVEL SECURITY",
		"NO FORCE ROW LEVEL SECURITY",
		"BYPASSRLS",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("legacy evidence outbox migration contains forbidden grant or RLS weakening %q", forbidden)
		}
	}
}
