package controlplane

import (
	"strings"
	"testing"
)

func TestPermissionIndexRebuildDoesNotDeleteAndReinsertDesiredKeys(t *testing.T) {
	t.Parallel()

	for _, required := range []string{
		"WITH indexed AS MATERIALIZED",
		"AND NOT EXISTS (",
		"desired.actor_id = current.actor_id",
		"desired.permission = current.permission",
		"ON CONFLICT (organization_id, project_id, actor_id, permission) DO UPDATE",
		"source_version = excluded.source_version",
	} {
		if !strings.Contains(sqlPermissionIndexRebuild, required) {
			t.Fatalf("permission index rebuild misses atomic reconciliation clause %q", required)
		}
	}
	if strings.Contains(sqlPermissionIndexRebuild, "WITH removed AS (\n    DELETE") {
		t.Fatal("permission index rebuild returned to delete-all-then-reinsert semantics")
	}
}
