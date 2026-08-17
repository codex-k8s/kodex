package main

import (
	"strings"
	"testing"
)

func TestTenantPrincipalRetirementMigrationIsRetrySafe(t *testing.T) {
	t.Parallel()

	raw, err := migrations.ReadFile("migrations/20260817000200_retry_safe_principal_retirement.sql")
	if err != nil {
		t.Fatalf("read retry-safe retirement migration: %v", err)
	}
	source := string(raw)
	for _, expected := range []string{
		"SET ROLE interaction_gateway_owner",
		"GRANT CREATE ON SCHEMA public TO interaction_gateway_role_controller",
		"SET ROLE interaction_gateway_role_controller",
		"requested_generation >= current_high_watermark",
		"WHERE generation = requested_generation",
		"AND status <> 'RETIRED'",
		"ALTER ROLE %I NOLOGIN",
		"REVOKE interaction_gateway_runtime FROM %I",
		"pg_terminate_backend(pid)",
		"REVOKE CREATE ON SCHEMA public FROM interaction_gateway_role_controller",
	} {
		if !strings.Contains(source, expected) {
			t.Fatalf("retry-safe retirement migration missed invariant %q", expected)
		}
	}
	if grant := strings.Index(source, "GRANT CREATE ON SCHEMA public TO interaction_gateway_role_controller"); grant < 0 {
		t.Fatal("retry-safe retirement must temporarily grant schema CREATE to the function owner")
	} else if replace := strings.Index(source, "CREATE OR REPLACE FUNCTION interaction_gateway_retire_runtime_identity"); replace < grant {
		t.Fatal("retry-safe retirement must grant schema CREATE before replacing the function")
	} else if revoke := strings.Index(source, "REVOKE CREATE ON SCHEMA public FROM interaction_gateway_role_controller"); revoke < replace {
		t.Fatal("retry-safe retirement must revoke schema CREATE after replacing the function")
	}
	if count := strings.Count(source, "status <> 'RETIRED'"); count != 1 {
		t.Fatalf("retry-safe retirement must filter only the no-op UPDATE, got %d status filters", count)
	}
}
