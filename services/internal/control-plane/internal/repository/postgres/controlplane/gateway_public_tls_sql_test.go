package controlplane

import (
	"strings"
	"testing"
)

func TestGatewayPublicTLSNamedSQLKeepsForwardOnlyStates(t *testing.T) {
	t.Parallel()

	prepareRequired := []string{
		"pending_generation = gateway_public_tls_state.applied_generation + 1",
		"@predecessor_generation = gateway_public_tls_state.applied_generation",
		"gateway_public_tls_state.pending_generation IS NULL",
	}
	for _, required := range prepareRequired {
		if !strings.Contains(sqlGatewayPublicTLSPrepare, required) {
			t.Fatalf("prepare SQL lacks forward-only condition %q", required)
		}
	}
	for _, required := range []string{
		"previous_generation = applied_generation",
		"applied_generation = pending_generation",
		"pending_generation = NULL",
		"overlap_expires_at",
	} {
		if !strings.Contains(sqlGatewayPublicTLSConfirm, required) {
			t.Fatalf("confirm SQL lacks atomic promotion %q", required)
		}
	}
	if !strings.Contains(sqlGatewayPublicTLSCheck, "overlap_expires_at > @checked_at") ||
		strings.Contains(sqlGatewayPublicTLSCheck, "UPDATE") {
		t.Fatal("readiness SQL must be read-only and expire PREVIOUS")
	}
}
