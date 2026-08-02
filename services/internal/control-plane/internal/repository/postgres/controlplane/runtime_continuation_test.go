package controlplane

import (
	"regexp"
	"strings"
	"testing"

	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/jackc/pgx/v5"
)

var namedPlaceholderPattern = regexp.MustCompile(`@([a-z][a-z0-9_]*)`)

func TestRuntimeContinuationStrictNamedArgumentsMatchSQL(t *testing.T) {
	integrationArgs, err := integrationContinuationArgs(domainrepo.IntegrationContinuation{})
	if err != nil {
		t.Fatalf("integration continuation args: %v", err)
	}
	if string(integrationArgs["credential_bindings"].([]byte)) != "[]" {
		t.Fatal("empty credential binding set must persist as an exact JSON array")
	}
	tests := []struct {
		name string
		sql  string
		args pgx.StrictNamedArgs
	}{
		{
			name: "runtime insert", sql: sqlRuntimeExecutionInsert,
			args: runtimeExecutionArgs(domainrepo.RuntimeExecution{}),
		},
		{
			name: "runtime update", sql: sqlRuntimeExecutionUpdate,
			args: runtimeExecutionUpdateArgs(domainrepo.RuntimeExecution{}, 1, 1),
		},
		{
			name: "integration insert", sql: sqlIntegrationContinuationInsert,
			args: integrationArgs,
		},
		{
			name: "integration update", sql: sqlIntegrationContinuationUpdate,
			args: integrationContinuationUpdateArgs(domainrepo.IntegrationContinuation{}, 1, 1),
		},
		{
			name: "scheduled suspension", sql: sqlScheduledRunSuspendExternal,
			args: scheduledRunSuspendArgs(domainrepo.ScheduledRun{}, "", 1),
		},
		{
			name: "scheduled prelock", sql: sqlScheduleOccurrenceGetByCurrentTurnForUpdate,
			args: pgx.StrictNamedArgs{
				"organization_id": "", "project_id": "", "turn_id": "",
			},
		},
		{
			name: "session cleanup blocker", sql: sqlIntegrationContinuationBlocksCleanup,
			args: pgx.StrictNamedArgs{
				"organization_id": "", "project_id": "", "session_id": "",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			placeholders := make(map[string]struct{})
			for _, match := range namedPlaceholderPattern.FindAllStringSubmatch(test.sql, -1) {
				placeholders[match[1]] = struct{}{}
			}
			for name := range placeholders {
				if _, ok := test.args[name]; !ok {
					t.Fatalf("placeholder has no argument: %s", name)
				}
			}
			for name := range test.args {
				if _, ok := placeholders[name]; !ok {
					t.Fatalf("strict argument is unused: %s", name)
				}
			}
		})
	}
}

func TestIntegrationContinuationCleanupBlockerIsSessionScoped(t *testing.T) {
	for _, required := range []string{
		"organization_id = @organization_id::uuid",
		"project_id = @project_id::uuid",
		"session_id = @session_id::uuid",
		"continuation_state <> 'REJOINED'",
	} {
		if !strings.Contains(sqlIntegrationContinuationBlocksCleanup, required) {
			t.Fatalf("cleanup blocker misses %q", required)
		}
	}
	for _, forbidden := range []string{"turn_id =", "attempt ="} {
		if strings.Contains(sqlIntegrationContinuationBlocksCleanup, forbidden) {
			t.Fatalf("cleanup blocker remains execution-scoped: %s", forbidden)
		}
	}
}

func TestScheduledContinuationRemainsOpenAndUnclaimable(t *testing.T) {
	for name, query := range map[string]string{
		"open": sqlScheduleOccurrenceHasOpen,
		"next": sqlScheduleOccurrenceNext,
		"skip": sqlScheduleOccurrenceSkipOverlap,
	} {
		for _, state := range []string{"WAITING_OWNER", "CONTINUATION"} {
			if !strings.Contains(query, state) {
				t.Fatalf("%s query does not protect %s", name, state)
			}
		}
	}
}
