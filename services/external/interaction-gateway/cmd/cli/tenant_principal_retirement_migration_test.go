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
		"requested_generation >= current_high_watermark",
		"WHERE generation = requested_generation",
		"AND status <> 'RETIRED'",
		"ALTER ROLE %I NOLOGIN",
		"REVOKE interaction_gateway_runtime FROM %I",
		"pg_terminate_backend(pid)",
	} {
		if !strings.Contains(source, expected) {
			t.Fatalf("retry-safe retirement migration missed invariant %q", expected)
		}
	}
	if count := strings.Count(source, "status <> 'RETIRED'"); count != 1 {
		t.Fatalf("retry-safe retirement must filter only the no-op UPDATE, got %d status filters", count)
	}
}
