package controlplane

import (
	"strings"
	"testing"
)

func TestOwnerEligibilityPrecedesPageLimitInNamedSQL(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		statement string
		predicate string
	}{
		"list resources":   {statement: sqlResourceList, predicate: "owner_actor_id = @actor_id::uuid"},
		"search resources": {statement: sqlResourceSearch, predicate: "owner_actor_id = @actor_id::uuid"},
		"list incidents":   {statement: sqlRuntimeIncidentList, predicate: "process.owner_actor_id = @actor_id::uuid"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			predicate := strings.Index(test.statement, test.predicate)
			order := strings.Index(test.statement, "ORDER BY")
			limit := strings.Index(test.statement, "LIMIT")
			if predicate < 0 || order < 0 || limit < 0 || predicate > order || predicate > limit {
				t.Fatalf("authoritative owner predicate must be applied before ORDER BY/LIMIT in %s", name)
			}
		})
	}
}

func TestOwnerHistoricalAndRunQueriesDoNotReuseAdmissionSnapshot(t *testing.T) {
	t.Parallel()

	for _, required := range []string{"kind = 'SESSION'", "state <> 'DELETED'", "FOR UPDATE"} {
		if !strings.Contains(sqlSessionHistoricalOwnerListForUpdate, required) {
			t.Fatalf("historical Session query misses %q", required)
		}
	}
	if strings.Contains(sqlSessionHistoricalOwnerListForUpdate, "state = 'ACTIVE'") {
		t.Fatal("historical backup membership still uses runtime admission state")
	}
	for _, required := range []string{"turn_attempts", "runtime_revision_id", "predecessorTurnId"} {
		if !strings.Contains(sqlRunGraphNodes, required) {
			t.Fatalf("run graph query misses %q", required)
		}
	}
	if !strings.Contains(sqlRunGraphArtifacts, "inputArtifacts") {
		t.Fatal("run artifact projection misses pre-admission Turn references")
	}
}

func TestProviderPinsAndReceiptReplayUseTargetProjections(t *testing.T) {
	t.Parallel()

	for _, required := range []string{"providerCredentialBindingId", "runtime_executions", "providerAccountBindingId"} {
		if !strings.Contains(sqlProviderBindingActiveSessions, required) {
			t.Fatalf("provider capacity query misses %q", required)
		}
	}
	if !strings.Contains(sqlExternalCommandReceiptFinalize, "result_snapshot") ||
		!strings.Contains(sqlExternalCommandReceiptGet, "result_snapshot") {
		t.Fatal("external one-use receipt does not persist immutable replay result")
	}
}

func TestRuntimeIncidentUpdateAndHistoryPersistFence(t *testing.T) {
	t.Parallel()

	for name, statement := range map[string]string{
		"incident update":  sqlRuntimeIncidentUpdate,
		"history insert":   sqlRuntimeIncidentHistoryInsert,
		"history readback": sqlRuntimeIncidentHistoryList,
	} {
		if !strings.Contains(statement, "execution_fence") {
			t.Fatalf("%s does not persist exact execution fence", name)
		}
	}
}

func TestScheduleRebindLocksOtherSessionReferences(t *testing.T) {
	t.Parallel()

	for _, fragment := range []string{
		"kind = 'SCHEDULE'",
		"state IN ('ACTIVE', 'PAUSED')",
		"spec ->> 'executionSessionId' = @session_id",
		"FOR UPDATE",
	} {
		if !strings.Contains(sqlScheduleOtherSessionReferencesForUpdate, fragment) {
			t.Fatalf("schedule rebind reference lock misses %q", fragment)
		}
	}
}
