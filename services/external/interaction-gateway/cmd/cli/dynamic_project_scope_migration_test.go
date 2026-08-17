package main

import (
	"strings"
	"testing"
)

func TestDynamicProjectScopeMigrationKeepsOrganizationBoundary(t *testing.T) {
	t.Parallel()

	raw, err := migrations.ReadFile("migrations/20260817000100_dynamic_project_scope.sql")
	if err != nil {
		t.Fatalf("read dynamic project scope migration: %v", err)
	}
	source := string(raw)
	for _, expected := range []string{
		"ADD COLUMN all_projects boolean NOT NULL DEFAULT true",
		"authority.organization_id = requested_organization_id",
		"authority.all_projects",
		"tenant.project_id = requested_project_id",
		"FOR SHARE OF fence, principal",
		"GRANT EXECUTE ON FUNCTION interaction_gateway_activate_runtime_context",
	} {
		if !strings.Contains(source, expected) {
			t.Fatalf("dynamic project scope migration missed invariant %q", expected)
		}
	}
	if strings.Contains(source, "DISABLE ROW LEVEL SECURITY") || strings.Contains(source, "BYPASSRLS") {
		t.Fatal("dynamic project scope migration weakens RLS")
	}
}
