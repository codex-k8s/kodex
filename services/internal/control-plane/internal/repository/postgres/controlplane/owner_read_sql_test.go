package controlplane

import (
	"strings"
	"testing"
)

func TestRunLineageSQLUsesBoundedTraversalBeforePageAllocation(t *testing.T) {
	traversal := strings.Index(sqlRunGraphNodes, "owner_run_graph_process_ids")
	pageLimit := strings.LastIndex(sqlRunGraphNodes, "LIMIT @limit")
	if traversal < 0 || pageLimit < 0 || traversal > pageLimit {
		t.Fatalf("lineage traversal is not bounded before page allocation: %s", sqlRunGraphNodes)
	}
	for _, required := range []string{"@graph_process_limit", "@graph_node_limit", "@graph_hard_limit", "graph_overflow"} {
		if !strings.Contains(sqlRunGraphNodes, required) {
			t.Fatalf("lineage hard bound %q is absent", required)
		}
	}
	if strings.Contains(sqlRunGraphNodes, "WITH RECURSIVE") {
		t.Fatal("unbounded recursive materialization returned to lineage query")
	}
}

func TestRuntimeIncidentSQLFiltersExactExecutionBeforeCursorAndLimit(t *testing.T) {
	executionFilter := strings.Index(sqlRuntimeIncidentList, "incident.execution_id = @execution_id::uuid")
	cursor := strings.Index(sqlRuntimeIncidentList, "incident.id > coalesce")
	limit := strings.Index(sqlRuntimeIncidentList, "LIMIT @limit")
	if executionFilter < 0 || cursor < 0 || limit < 0 || executionFilter > cursor || cursor > limit {
		t.Fatalf("runtime incident filter is not applied before cursor/limit: %s", sqlRuntimeIncidentList)
	}
}

func TestWorkspaceRestoreSQLFiltersExactBackupBeforeCursorAndLimit(t *testing.T) {
	backupFilter := strings.Index(sqlOwnerResourceList, "spec ->> 'backupId' = @backup_id")
	cursor := strings.Index(sqlOwnerResourceList, "id > @after_id::uuid")
	limit := strings.Index(sqlOwnerResourceList, "LIMIT @limit")
	if backupFilter < 0 || cursor < 0 || limit < 0 || backupFilter > cursor || cursor > limit {
		t.Fatalf("workspace restore backup filter is not applied before cursor/limit: %s", sqlOwnerResourceList)
	}
	if !strings.Contains(sqlOwnerResourceList, "owner_actor_id = @actor_id::uuid") {
		t.Fatalf("owner resource list is not fail-closed by exact actor: %s", sqlOwnerResourceList)
	}
}
