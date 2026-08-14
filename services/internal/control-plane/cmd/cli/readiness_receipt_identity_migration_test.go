package main

import (
	"strings"
	"testing"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/schema"
)

func TestReadinessReceiptIdentityMigrationIsPortableAndForwardOnly(t *testing.T) {
	const migrationVersion int64 = 20260814000100
	if schema.CurrentVersion < migrationVersion {
		t.Fatalf("schema fence %d excludes readiness receipt migration %d", schema.CurrentVersion, migrationVersion)
	}
	raw, err := migrations.ReadFile("migrations/20260814000100_control_plane_readiness_receipt_identity.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	source := string(raw)
	for _, expected := range []string{
		"stored_version <> 20260813000200",
		"SET version = 20260814000100",
		"'prepare_gateway_public_tls', 'confirm_gateway_public_tls'",
		"interaction_gateway_cursors",
		"'OWNER_GATE_CLAIM'",
		"'OWNER_GATE_EXPIRE'",
		"'DELIVERY_CLAIM'",
	} {
		if !strings.Contains(source, expected) {
			t.Fatalf("migration %d lost %q", migrationVersion, expected)
		}
	}
	if strings.Contains(source, "DROP TABLE") || strings.Contains(source, "TRUNCATE") {
		t.Fatal("migration broadens cleanup beyond exact stale receipts")
	}
	if strings.Contains(source, "d9b072a0-3980-57c0-a6fe-289b7a608f31") ||
		strings.Contains(source, "1b2b8575-0cef-5f6f-8e4d-ed3960a28131") {
		t.Fatal("migration must not contain installation-specific identifiers")
	}
}
