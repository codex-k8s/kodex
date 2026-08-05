package controlplane

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/jackc/pgx/v5"
)

var namedPlaceholderPattern = regexp.MustCompile(`@([a-z][a-z0-9_]*)`)

func TestRuntimeResidualMigrationUsesExactCatalogIdentityAndCanonicalRestoreColumns(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..", "cmd", "cli", "migrations", "20260804000300_control_plane_runtime_residuals.sql")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	migration := string(raw)
	for _, required := range []string{
		"conname = 'runtime_executions_terminal_outcome_check'",
		"conkey <> ARRAY[terminal_attnum]",
		"DROP CONSTRAINT IF EXISTS runtime_executions_check3",
		"DROP CONSTRAINT IF EXISTS runtime_executions_terminal_outcome_v2_ck",
		"'SUSPENDED', 'CANCELLED', 'EXPIRED', 'BLOCKED'",
		"(state = 'SUSPENDED' AND terminal_outcome IN ('SUSPENDED', 'BLOCKED'))",
		"restore_source_archive_object_key",
		"DROP COLUMN restore_source_archive_key",
		"CREATE TRIGGER resources_default_retention_policy",
		"CREATE TABLE control_plane.runtime_retention_holds",
	} {
		if !strings.Contains(migration, required) {
			t.Fatalf("residual migration misses exact invariant %q", required)
		}
	}
	if strings.Contains(migration, "pg_get_constraintdef(") {
		t.Fatal("residual migration identifies applied constraints by rendered DDL")
	}
	dropOld := strings.Index(migration, "DROP CONSTRAINT IF EXISTS runtime_executions_terminal_outcome_check")
	addCanonical := strings.Index(migration, "ADD CONSTRAINT runtime_executions_terminal_outcome_v3_ck")
	if dropOld < 0 || addCanonical < 0 || dropOld >= addCanonical ||
		strings.Contains(migration[addCanonical:], "ADD CONSTRAINT runtime_executions_terminal_outcome_check") {
		t.Fatal("terminal constraint upgrade can leave old and canonical checks active together")
	}
	for name, query := range map[string]string{
		"insert":                 sqlRuntimeExecutionInsert,
		"get_by_turn":            sqlRuntimeExecutionGetByTurn,
		"get_for_update":         sqlRuntimeExecutionGetForUpdate,
		"get_by_turn_for_update": sqlRuntimeExecutionGetByTurnForUpdate,
		"next_expired":           sqlRuntimeExecutionNextExpired,
		"latest_session_restore": sqlRuntimeExecutionLatestSessionArchiveForRestore,
	} {
		for _, canonical := range []string{"restore_source_version", "restore_source_archive_object_key", "restore_source_archive_kms_key_arn", "restore_source_archive_object_lock_mode"} {
			if !strings.Contains(query, canonical) {
				t.Fatalf("%s misses canonical restore column %s", name, canonical)
			}
		}
		for _, transition := range []string{"restore_source_execution_version", "restore_source_archive_key", "restore_source_kms_key_arn", "restore_source_object_lock_mode"} {
			if strings.Contains(query, transition) {
				t.Fatalf("%s still uses transition restore column %s", name, transition)
			}
		}
	}
	if !strings.Contains(sqlSessionBlocksRuntimeCleanup, "runtime_retention_holds") ||
		!strings.Contains(sqlSessionBlocksRuntimeCleanup, "hold.state = 'ACTIVE'") {
		t.Fatal("full cleanup graph does not include active owner retention holds")
	}
}

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
			name: "schedule occurrence update", sql: sqlScheduleOccurrenceUpdate,
			args: scheduleOccurrenceUpdateArgs(domainrepo.ScheduleOccurrence{
				EffectiveInputSHA256: strings.Repeat("a", 64),
			}, 1, ""),
		},
		{
			name: "scheduled candidate", sql: sqlScheduleOccurrenceGetByCurrentTurn,
			args: pgx.StrictNamedArgs{
				"organization_id": "", "project_id": "", "turn_id": "",
			},
		},
		{
			name: "schedule blocking execution", sql: sqlScheduleOccurrenceHasBlockingExecution,
			args: pgx.StrictNamedArgs{
				"organization_id": "", "project_id": "", "schedule_id": "",
				"candidate_occurrence_id": "", "open_execution_states": []string{},
			},
		},
		{
			name: "schedule open execution", sql: sqlScheduleOccurrenceHasOpen,
			args: pgx.StrictNamedArgs{
				"organization_id": "", "project_id": "", "schedule_id": "",
				"open_execution_states": []string{},
			},
		},
		{
			name: "schedule next occurrence", sql: sqlScheduleOccurrenceNext,
			args: pgx.StrictNamedArgs{
				"organization_id": "", "project_id": "", "now": time.Time{},
				"open_execution_states": []string{},
			},
		},
		{
			name: "schedule skip overlap", sql: sqlScheduleOccurrenceSkipOverlap,
			args: pgx.StrictNamedArgs{
				"organization_id": "", "project_id": "", "now": time.Time{},
				"limit": 16, "open_execution_states": []string{},
			},
		},
		{
			name: "session cleanup blocker", sql: sqlIntegrationContinuationBlocksCleanup,
			args: pgx.StrictNamedArgs{
				"organization_id": "", "project_id": "", "session_id": "",
			},
		},
		{
			name: "full session cleanup graph", sql: sqlSessionBlocksRuntimeCleanup,
			args: pgx.StrictNamedArgs{
				"organization_id": "", "project_id": "", "session_id": "",
			},
		},
		{
			name: "retention policy lookup", sql: sqlResourceRetentionPolicyCurrent,
			args: pgx.StrictNamedArgs{"organization_id": "", "project_id": ""},
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

func TestScheduleOccurrenceUpdatePersistsCurrentDigest(t *testing.T) {
	digest := strings.Repeat("b", 64)
	args := scheduleOccurrenceUpdateArgs(domainrepo.ScheduleOccurrence{
		EffectiveInputSHA256: digest,
	}, 1, "")
	if args["effective_input_sha256"] != digest {
		t.Fatal("schedule occurrence update loses current digest argument")
	}
	if !strings.Contains(
		sqlScheduleOccurrenceUpdate,
		"effective_input_sha256 = @effective_input_sha256",
	) {
		t.Fatal("schedule occurrence update does not persist current digest")
	}
}

func TestScheduleCurrentTurnDiscoveryCoversApprovedOpenStates(t *testing.T) {
	for name, query := range map[string]string{
		"candidate": sqlScheduleOccurrenceGetByCurrentTurn,
	} {
		for _, state := range []string{"CLAIMED", "WAITING_OWNER", "CONTINUATION", "FAILED"} {
			if !strings.Contains(query, "'"+state+"'") {
				t.Fatalf("%s current-turn query misses %s", name, state)
			}
		}
		if strings.Contains(query, "FOR UPDATE") {
			t.Fatal("candidate discovery unexpectedly takes a row lock")
		}
	}
	if strings.Contains(sqlTurnExpiredClaimed, "FOR UPDATE") {
		t.Fatal("stale ClaimTurn candidate discovery unexpectedly locks Turn")
	}
	if strings.Contains(sqlOwnerGateNextExpired, "FOR UPDATE") {
		t.Fatal("expired OwnerGate candidate discovery unexpectedly locks Gate")
	}
	if strings.Contains(sqlScheduleOccurrenceExpiredCandidates, "FOR UPDATE") {
		t.Fatal("scheduler recovery candidate discovery unexpectedly locks occurrence")
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

func TestMaterializedPredecessorDoesNotRemainInSessionOpenSet(t *testing.T) {
	if strings.Contains(sqlSessionOpenTurns, "'CANCELLED'") {
		t.Fatal("terminal integration predecessor remains in the session open set")
	}
	if !strings.Contains(
		sqlIntegrationContinuationBlocksCleanup,
		"continuation_state <> 'REJOINED'",
	) {
		t.Fatal("cleanup does not become eligible after exact continuation rejoin")
	}
}

func TestScheduledContinuationRemainsOpenAndUnclaimable(t *testing.T) {
	for name, query := range map[string]string{
		"open": sqlScheduleOccurrenceHasOpen,
		"next": sqlScheduleOccurrenceNext,
		"skip": sqlScheduleOccurrenceSkipOverlap,
	} {
		if !strings.Contains(query, "@open_execution_states::text[]") {
			t.Fatalf("%s query bypasses shared open execution states", name)
		}
	}
	states := scheduleOpenExecutionStates()
	if strings.Join(states, ",") != "RESERVED,CLAIMED,WAITING_OWNER,CONTINUATION" {
		t.Fatalf("unexpected closed schedule execution state set: %v", states)
	}
}

func TestScheduleOpenGraphIncludesCurrentScheduledRunAuthority(t *testing.T) {
	for _, required := range []string{
		"LEFT JOIN control_plane.scheduled_runs AS run",
		"run.occurrence_id = occurrence.id",
		"run.state = ANY(@open_execution_states::text[])",
	} {
		if !strings.Contains(sqlScheduleOccurrenceHasOpen, required) {
			t.Fatalf("schedule open-graph query misses %q", required)
		}
	}
}

func TestScheduleClaimCardinalityIncludesHistoricalOpenRun(t *testing.T) {
	for name, query := range map[string]string{
		"selection":         sqlScheduleOccurrenceNext,
		"overlap skip":      sqlScheduleOccurrenceSkipOverlap,
		"post-lock recheck": sqlScheduleOccurrenceHasBlockingExecution,
	} {
		for _, required := range []string{
			"control_plane.scheduled_runs AS open_run",
			"open_run.occurrence_id =",
			"open_run.state = ANY(@open_execution_states::text[])",
		} {
			if !strings.Contains(query, required) {
				t.Fatalf("%s misses historical run predicate %q", name, required)
			}
		}
	}
	for _, required := range []string{
		"owned_occurrence.id <> @candidate_occurrence_id::uuid",
		"owned_occurrence.schedule_id = @schedule_id::uuid",
		"open_run.occurrence_id IS NOT NULL",
	} {
		if !strings.Contains(sqlScheduleOccurrenceHasBlockingExecution, required) {
			t.Fatalf("post-lock cardinality query misses %q", required)
		}
	}
}
