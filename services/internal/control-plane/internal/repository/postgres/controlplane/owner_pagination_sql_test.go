package controlplane

import (
	"os"
	"path/filepath"
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

func TestScheduleReplacementUsesEarlyProjectFenceAndAdmissionIndex(t *testing.T) {
	t.Parallel()

	projectFence := "@organization_id::text || ':' || @project_id::text"
	for name, statement := range map[string]string{
		"early schedule fence": sqlScheduleSessionProjectFence,
		"Session insert fence": sqlResourceInsert,
	} {
		if !strings.Contains(statement, "pg_advisory_xact_lock") ||
			!strings.Contains(statement, projectFence) {
			t.Fatalf("%s does not use the shared project graph fence", name)
		}
	}
	for _, fragment := range []string{
		"kind = 'SESSION'", "state IN (", "'ACTIVE'", "'PAUSED'", "'WAITING_OWNER'", "'BLOCKED'",
		"spec ->> 'conversationId'", "ORDER BY id", "FOR UPDATE",
	} {
		if !strings.Contains(sqlScheduleSessionConversationForUpdate, fragment) {
			t.Fatalf("locked Session candidate reread misses %q", fragment)
		}
	}
	if strings.Contains(sqlScheduleSessionConversationForUpdate, "'ARCHIVED'") {
		t.Fatal("immutable archived Session remained in the admission candidate set")
	}
	migrationPath := filepath.Join("..", "..", "..", "..", "cmd", "cli", "migrations",
		"20260806023400_control_plane_owner_configuration.sql")
	migration, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatal(err)
	}
	source := string(migration)
	for _, fragment := range []string{
		"DROP INDEX control_plane.resources_session_conversation_uidx",
		"CREATE UNIQUE INDEX resources_session_conversation_uidx",
		"control-plane Session conversation admission boundary is ambiguous",
		"state IN (",
	} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("forward migration misses %q", fragment)
		}
	}
	indexStart := strings.Index(source, "CREATE UNIQUE INDEX resources_session_conversation_uidx")
	if indexStart < 0 {
		t.Fatal("forward Session admission index is absent")
	}
	indexEnd := strings.Index(source[indexStart:], "CREATE TABLE control_plane.protected_resource_history")
	if indexEnd < 0 {
		t.Fatal("forward Session admission index is not bounded in migration")
	}
	indexSQL := source[indexStart : indexStart+indexEnd]
	if strings.Contains(indexSQL, "'ARCHIVED'") || strings.Contains(indexSQL, "state <> 'DELETED'") {
		t.Fatal("forward Session index still lets immutable history block replacement")
	}
}

func TestInstructionReadinessUsesCrossReplicaTransactionFence(t *testing.T) {
	t.Parallel()

	for _, fragment := range []string{
		"pg_advisory_xact_lock",
		"control-plane:instruction-object-store-readiness:v1",
	} {
		if !strings.Contains(sqlInstructionObjectReadinessFence, fragment) {
			t.Fatalf("instruction readiness fence misses %q", fragment)
		}
	}
}
