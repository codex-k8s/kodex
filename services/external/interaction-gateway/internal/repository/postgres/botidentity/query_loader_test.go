package botidentity

import (
	"strings"
	"testing"
)

func TestOperationQueriesEnforceOneWinnerCheckpointAndFencedRecovery(t *testing.T) {
	t.Parallel()
	if err := validateQueries(); err != nil {
		t.Fatal(err)
	}
	for name, statement := range map[string]string{
		"one winner":        operationInsertSQL,
		"winner lock":       operationLockSQL,
		"pre-effect marker": operationMarkEffectSQL,
		"recovery claim":    operationClaimSQL,
	} {
		if statement == "" {
			t.Fatalf("required query is empty: %s", name)
		}
	}
	if !strings.Contains(operationInsertSQL, "ON CONFLICT DO NOTHING") ||
		!strings.Contains(operationLockSQL, "FOR UPDATE OF operation") ||
		!strings.Contains(operationMarkEffectSQL, "effect_started_at = COALESCE(effect_started_at, clock_timestamp())") ||
		!strings.Contains(operationMarkEffectSQL, "lease_token_sha256") ||
		!strings.Contains(operationClaimSQL, "FOR UPDATE SKIP LOCKED") ||
		!strings.Contains(operationClaimSQL, "recovery_deadline > clock_timestamp()") {
		t.Fatal("one-winner or ambiguous-effect recovery SQL invariant is incomplete")
	}
	if !strings.Contains(operationClaimSQL, "failure_code = 'RECOVERY_TIMEOUT'") ||
		!strings.Contains(operationClaimSQL, "recovery_deadline <= clock_timestamp()") ||
		!strings.Contains(operationClaimSQL, "SELECT count(*)::bigint FROM expired") ||
		strings.Contains(operationClaimSQL, "now()") || strings.Contains(operationClaimSQL, "CURRENT_TIMESTAMP") {
		t.Fatal("recovery timeout classification is not a PostgreSQL-clock typed claim outcome")
	}
	if !strings.Contains(repairBacklogCountSQL, "state = 'REPAIR_REQUIRED'") ||
		!strings.Contains(repairBacklogCountSQL, "failure_code = 'RECOVERY_TIMEOUT'") {
		t.Fatal("durable repair backlog query is incomplete")
	}
}

func TestEmptyRecoveryScopeRemainsNullable(t *testing.T) {
	t.Parallel()
	if !strings.Contains(workScopeNextSQL, "interaction_gateway_next_work_scope") ||
		strings.Contains(workScopeNextSQL, "COALESCE") {
		t.Fatal("Agent bot recovery scope must preserve nullable empty-work result")
	}
}

func TestGenerationAdvanceAndProviderAcceptAreSeparateNamedStepsForOneTransaction(t *testing.T) {
	t.Parallel()
	if !strings.Contains(watermarkAdvanceSQL, "admitted = false") ||
		!strings.Contains(operationAcceptSQL, "state = 'PROVIDER_ACCEPTED'") ||
		!strings.Contains(operationAcceptSQL, "fence = @arg3") {
		t.Fatal("generation closure and provider accept query invariant is incomplete")
	}
}

func TestTerminalOperationReadbackDoesNotJoinMutableCurrentBinding(t *testing.T) {
	t.Parallel()
	for name, statement := range map[string]string{"lock": operationLockSQL, "get": operationGetSQL} {
		if strings.Contains(statement, "interaction_gateway_agent_bot_bindings") ||
			!strings.Contains(statement, "operation.result_agent_version") ||
			!strings.Contains(statement, "operation.receipt_sha256") {
			t.Fatalf("terminal %s readback is not immutable", name)
		}
	}
}

func TestGenerationClosureReservationAndTerminalWritesRequireCurrentLeaseFence(t *testing.T) {
	t.Parallel()
	for name, statement := range map[string]string{
		"provider object reservation": ownershipReserveOperationSQL,
		"generation closure":          watermarkCloseSQL,
		"terminal finish":             operationFinishSQL,
		"repair terminal":             operationRepairSQL,
	} {
		if !strings.Contains(statement, "fence =") ||
			!strings.Contains(statement, "lease_token_sha256 =") ||
			!strings.Contains(statement, "lease_expires_at > clock_timestamp()") {
			t.Fatalf("%s is not guarded by the current database lease fence", name)
		}
	}
}
