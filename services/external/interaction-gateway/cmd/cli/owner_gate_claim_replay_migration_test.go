package main

import (
	"strings"
	"testing"
)

func TestOwnerGateClaimReplayMigrationKeepsExactIdempotency(t *testing.T) {
	t.Parallel()

	raw, err := migrations.ReadFile("migrations/20260814000200_owner_gate_claim_replay.sql")
	if err != nil {
		t.Fatalf("read owner gate replay migration: %v", err)
	}
	source := string(raw)
	for _, expected := range []string{
		"SET ROLE interaction_gateway_owner;",
		"CREATE OR REPLACE FUNCTION interaction_gateway_bind_owner_gate_request",
		"existing_state = 'CLAIMED'",
		"existing_gate_id = requested_gate_id AND existing_delivery_id = requested_delivery_id",
		"CREATE OR REPLACE FUNCTION interaction_gateway_complete_owner_gate_request",
		"existing_state = 'COMPLETED'",
		"WHERE request.idempotency_key = requested_key",
		"FOR UPDATE",
		"state IN ('PENDING', 'CLAIMED')",
		"REVOKE ALL ON FUNCTION interaction_gateway_bind_owner_gate_request(uuid, uuid, uuid) FROM PUBLIC;",
		"GRANT EXECUTE ON FUNCTION interaction_gateway_complete_owner_gate_request(uuid)",
	} {
		if !strings.Contains(source, expected) {
			t.Fatalf("owner gate replay migration missed invariant %q", expected)
		}
	}
	if strings.Contains(source, "DROP TABLE") || strings.Contains(source, "TRUNCATE") {
		t.Fatal("owner gate replay migration must preserve durable request evidence")
	}
}
