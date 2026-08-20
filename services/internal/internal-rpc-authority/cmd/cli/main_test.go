package main

import (
	"os"
	"strings"
	"testing"
)

func TestParseCommandAcceptsDeploy(t *testing.T) {
	t.Parallel()

	action, err := parseCommand([]string{"migrate", "deploy"})
	if err != nil {
		t.Fatalf("parse deploy command: %v", err)
	}
	if action != commandDeploy {
		t.Fatalf("unexpected deploy command: %q", action)
	}
}

func TestReadbackContentionMigrationUsesScopedAdvisoryLock(t *testing.T) {
	t.Parallel()

	content, err := os.ReadFile(
		"migrations/20260820000200_internal_rpc_authority_readback_contention.sql",
	)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	text := string(content)
	for _, required := range []string{
		"pg_advisory_xact_lock",
		"p_idempotency_key::text",
		"FOR UPDATE",
		"authority_readback_attestation_receipts",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("migration is missing concurrency invariant %q", required)
		}
	}
}

func TestPeerScopedReadbackMigrationUsesCompositeIdempotencyLookup(t *testing.T) {
	t.Parallel()

	content, err := os.ReadFile(
		"migrations/20260820000300_internal_rpc_authority_peer_scoped_readback.sql",
	)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	text := string(content)
	for _, required := range []string{
		"challenge_peer_spiffe_id text",
		"WHERE peer_spiffe_id = challenge_peer_spiffe_id",
		"AND idempotency_key = p_idempotency_key",
		"challenge_peer_spiffe_id || ':' || p_idempotency_key::text",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("migration is missing peer-scoped invariant %q", required)
		}
	}
	if strings.Contains(text, "WHERE idempotency_key = p_idempotency_key") {
		t.Fatal("migration restored an unscoped idempotency lookup")
	}
}
