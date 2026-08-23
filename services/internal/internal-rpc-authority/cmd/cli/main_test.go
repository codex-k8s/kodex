package main

import (
	_ "embed"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

//go:embed testdata/publisher_append_snapshot_history.sql
var publicationFunctionDeclaration string

const baselineMigration = "migrations/20260823000100_internal_rpc_authority_baseline.sql"

func TestParseCommandAcceptsFreshOnlyCommands(t *testing.T) {
	t.Parallel()

	action, err := parseCommand([]string{"up"})
	if err != nil {
		t.Fatalf("parse up command: %v", err)
	}
	if action != commandUp {
		t.Fatalf("unexpected up command: %q", action)
	}
	for _, arguments := range [][]string{{"migrate", "deploy"}, {"expand"}, {"contract"}, {"version"}} {
		if _, parseErr := parseCommand(arguments); parseErr == nil {
			t.Fatalf("retired migration command was accepted: %v", arguments)
		}
	}
}

func TestReadbackContentionMigrationUsesScopedAdvisoryLock(t *testing.T) {
	t.Parallel()

	content, err := os.ReadFile(baselineMigration)
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

	content, err := os.ReadFile(baselineMigration)
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

func TestFreshInstallContainsOneAuthorityBaseline(t *testing.T) {
	t.Parallel()

	entries, err := filepath.Glob("migrations/*.sql")
	if err != nil {
		t.Fatalf("list migrations: %v", err)
	}
	if len(entries) != 1 || entries[0] != baselineMigration {
		t.Fatalf("unexpected fresh migration set: %v", entries)
	}
}

func TestBaselineMaterializesCurrentWorkloadPrincipals(t *testing.T) {
	t.Parallel()

	content, err := os.ReadFile(baselineMigration)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	text := string(content)
	for _, required := range []string{
		"ira_role_image_builder_issuer_g1",
		"ira_image_admission_issuer_g1",
		"ira_image_promotion_issuer_g1",
		"ira_automation_scheduler_issuer_g1",
		"ira_control_api_gateway_issuer_g1",
		"ira_control_plane_verifier_g1",
		"ira_control_plane_resolver_g1",
		"ira_integration_gateway_issuer_g1",
		"ira_interaction_gateway_issuer_g1",
		"ira_runtime_controller_issuer_g1",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("baseline is missing current workload principal %q", required)
		}
	}
	if strings.Count(text, strings.TrimSpace(publicationFunctionDeclaration)) != 1 ||
		!strings.Contains(text, `"p_published_at" timestamp with time zone) RETURNS boolean`) {
		t.Fatal("baseline does not contain exactly one current snapshot publication function")
	}
}
