package gateway

import (
	"strings"
	"testing"
)

func TestManagementEffectSQLPinsOwnerWinnerAcrossLifecycle(t *testing.T) {
	t.Parallel()

	for query, required := range map[string][]string{
		"effect__claim": {
			"owner.version = effect.owner_version", "owner.generation = effect.owner_generation",
			"owner.status = effect.owner_status", "FOR UPDATE SKIP LOCKED",
		},
		"effect__dispatch": {
			"effect.input_sha256 = effect.intent_sha256", "owner.version = effect.owner_version",
			"owner.generation = effect.owner_generation", "owner.status = effect.owner_status", "FOR UPDATE",
		},
		"effect__renew": {
			"owner.version = effect.owner_version", "owner.generation = effect.owner_generation",
			"owner.status = effect.owner_status", "authorization.generation = renewed.resource_generation",
		},
		"effect__complete": {
			"provider_phase IN ('SUCCEEDED', 'UNKNOWN', 'SKIPPED')",
			"secret_phase = 'SUCCEEDED'", "control_plane_phase = 'SUCCEEDED'",
		},
	} {
		source := managementSQL(query)
		for _, invariant := range required {
			if !strings.Contains(source, invariant) {
				t.Fatalf("%s missed lifecycle fence %q", query, invariant)
			}
		}
	}
}

func TestTerminalCommandsCloseStaleDependentEffects(t *testing.T) {
	t.Parallel()

	revoke := managementSQL("connection__revoke")
	for _, invariant := range []string{
		"integration_test_receipts", "category = 'CREDENTIAL_UNAVAILABLE'",
		"effect.status = 'CLAIMED' AND effect.dispatch_state = 'DISPATCHED'",
		"effect.status IN ('PENDING', 'CLAIMED') AND effect.dispatch_state = 'PENDING'",
	} {
		if !strings.Contains(revoke, invariant) {
			t.Fatalf("connection revoke missed dependent-effect closure %q", invariant)
		}
	}
	gitUpdate := managementSQL("git_binding__update")
	for _, invariant := range []string{
		"effect.status = 'CLAIMED' AND effect.dispatch_state = 'DISPATCHED'",
		"reconciliation.state IN ('PENDING', 'FETCHED')",
		"effect.status IN ('PENDING', 'CLAIMED') AND effect.dispatch_state = 'PENDING'",
	} {
		if !strings.Contains(gitUpdate, invariant) {
			t.Fatalf("Git update/archive missed stale-effect closure %q", invariant)
		}
	}
}

func TestGitFetchCompletionPreservesOriginalInputFence(t *testing.T) {
	t.Parallel()

	source := managementSQL("git__fetch_complete")
	if !strings.Contains(source, "effect.input_sha256 = @fetch_input_sha256") ||
		!strings.Contains(source, "@command_intent_sha256") {
		t.Fatal("Git fetch completion does not separate fetch input from apply semantic intent")
	}
}
