package main

import (
	"strings"
	"testing"
)

func TestRuntimeScopeGrantMigrationIsFailClosed(t *testing.T) {
	const migrationVersion int64 = 20260813000200
	raw, err := migrations.ReadFile("migrations/20260813000200_control_plane_runtime_scope_execute.sql")
	if err != nil {
		t.Fatalf("read runtime scope grant migration: %v", err)
	}
	statement := string(raw)
	for _, invariant := range []string{
		"REVOKE ALL ON FUNCTION control_plane.runtime_scope() FROM PUBLIC",
		"GRANT EXECUTE ON FUNCTION control_plane.runtime_scope() TO control_plane_runtime",
		"acl.grantee = 0",
		"acl.privilege_type = 'EXECUTE'",
		"stored_version <> 20260813000100",
		"SET version = 20260813000200",
	} {
		if !strings.Contains(statement, invariant) {
			t.Fatalf("runtime scope grant migration missed invariant %q", invariant)
		}
	}
}
