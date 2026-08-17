package main

import (
	"strings"
	"testing"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/schema"
)

func TestProjectBootstrapScopeMigrationRemovesTriggerPath(t *testing.T) {
	const migrationVersion int64 = 20260817000100
	if schema.CurrentVersion != migrationVersion {
		t.Fatalf("schema fence %d does not match project bootstrap migration %d", schema.CurrentVersion, migrationVersion)
	}
	raw, err := migrations.ReadFile("migrations/20260817000100_control_plane_project_bootstrap_scope.sql")
	if err != nil {
		t.Fatalf("read project bootstrap migration: %v", err)
	}
	source := string(raw)
	for _, expected := range []string{
		"DROP TRIGGER resources_default_retention_policy ON control_plane.resources",
		"DROP FUNCTION control_plane.ensure_default_resource_retention_policy()",
		"stored_version <> 20260814000300",
		"SET version = 20260817000100",
	} {
		if !strings.Contains(source, expected) {
			t.Fatalf("project bootstrap migration missed invariant %q", expected)
		}
	}
	if strings.Contains(source, "DISABLE ROW LEVEL SECURITY") ||
		strings.Contains(source, "NO FORCE ROW LEVEL SECURITY") ||
		strings.Contains(source, "BYPASSRLS") {
		t.Fatal("project bootstrap migration weakens RLS")
	}
}
