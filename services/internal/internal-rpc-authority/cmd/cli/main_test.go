package main

import (
	"crypto/sha256"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

//go:embed testdata/publisher_append_snapshot_history.sql
var publicationFunctionDeclaration string

const baselineMigration = "migrations/20260823000100_internal_rpc_authority_baseline.sql"
const workloadBoundaryMigration = "migrations/20260906000100_workload_database_boundary.sql"

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
		"ira_restore_controller_g1",
		"ira_publisher_g4",
		"ira_readback_attestor_g4",
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

func TestSnapshotPromotionUsesExactRequiredReadbackTargets(t *testing.T) {
	t.Parallel()

	content, err := os.ReadFile(baselineMigration)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	text := string(content)
	for _, required := range []string{
		`"p_expected_workload_ids" "text"[]`,
		`"p_expected_roles" "text"[]`,
		`"p_expected_workload_generations" bigint[]`,
		"pg_catalog.cardinality(p_expected_workload_ids)",
		"readback.workload_id = expected.workload_id",
		"readback.role = expected.role",
		"readback.workload_generation = expected.workload_generation",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("snapshot promotion is missing exact target invariant %q", required)
		}
	}
	if strings.Contains(
		text,
		"FROM internal_rpc_authority.authority_snapshot_readbacks\n"+
			"    WHERE source_revision = p_source_revision",
	) {
		t.Fatal("snapshot promotion still counts optional dynamic readbacks")
	}
}

func TestAuthorityMigrationHistoryPreservesPublishedBaseline(t *testing.T) {
	t.Parallel()

	entries, err := filepath.Glob("migrations/*.sql")
	if err != nil {
		t.Fatalf("list migrations: %v", err)
	}
	if len(entries) != 2 || entries[0] != baselineMigration || entries[1] != workloadBoundaryMigration {
		t.Fatalf("unexpected forward migration set: %v", entries)
	}
	content, err := os.ReadFile(baselineMigration)
	if err != nil {
		t.Fatal("read published baseline")
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(content)); got != "d4c9ed792ae0e157247fd3e1b58d15f7bbff43bf38f202bf72e9201398be4e0a" {
		t.Fatal("published authority baseline bytes changed")
	}
}

func TestBaselineWrapsEveryFunctionForGoose(t *testing.T) {
	t.Parallel()

	content, err := os.ReadFile(baselineMigration)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	text := string(content)
	createFunctionPrefix := "CREATE " + "FUNCTION "
	functionCount := strings.Count(text, createFunctionPrefix)
	wrappedFunctionPattern := regexp.MustCompile(
		`(?s)-- \+goose StatementBegin\s+` +
			regexp.QuoteMeta(createFunctionPrefix) +
			`.*?(?:\$_\$|\$\$);\s+-- \+goose StatementEnd`,
	)
	wrappedFunctionCount := len(wrappedFunctionPattern.FindAllString(text, -1))
	if functionCount == 0 || wrappedFunctionCount != functionCount {
		t.Fatalf(
			"Goose function boundaries = %d, want %d",
			wrappedFunctionCount,
			functionCount,
		)
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
		"ira_secret_broker_issuer_g1",
		"ira_control_plane_issuer_g1",
		"ira_secret_broker_verifier_g1",
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
	for _, retired := range []string{
		"ira_database_credential_reconciler",
		"ira_publisher_g3",
		"ira_readback_attestor_g3",
		"reconcile_runtime_database_identity",
		"retire_runtime_database_identity",
	} {
		if strings.Contains(text, retired) {
			t.Fatalf("baseline still contains retired credential lifecycle %q", retired)
		}
	}
	if strings.Count(text, strings.TrimSpace(publicationFunctionDeclaration)) != 1 ||
		!strings.Contains(text, `"p_published_at" timestamp with time zone) RETURNS boolean`) {
		t.Fatal("baseline does not contain exactly one current snapshot publication function")
	}
}

func TestBaselineGrantsDatabaseConnectToExactRuntimePrincipals(t *testing.T) {
	t.Parallel()

	content, err := os.ReadFile(baselineMigration)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	text := string(content)
	connectStart := strings.Index(text, "GRANT CONNECT ON DATABASE internal_rpc_authority")
	if connectStart < 0 {
		t.Fatal("baseline does not grant database CONNECT to runtime principals")
	}
	connectEnd := strings.Index(text[connectStart:], ";")
	if connectEnd < 0 {
		t.Fatal("database CONNECT grant is incomplete")
	}
	grant := text[connectStart : connectStart+connectEnd]
	for _, principal := range []string{
		"ira_restore_controller_g1",
		"ira_publisher_g4",
		"ira_readback_attestor_g4",
		"ira_role_image_builder_issuer_g1",
		"ira_image_admission_issuer_g1",
		"ira_image_promotion_issuer_g1",
		"ira_automation_scheduler_issuer_g1",
		"ira_secret_broker_issuer_g1",
		"ira_control_plane_issuer_g1",
		"ira_secret_broker_verifier_g1",
		"ira_control_api_gateway_issuer_g1",
		"ira_control_plane_verifier_g1",
		"ira_control_plane_resolver_g1",
		"ira_integration_gateway_issuer_g1",
		"ira_interaction_gateway_issuer_g1",
		"ira_runtime_controller_issuer_g1",
	} {
		if !strings.Contains(grant, principal) {
			t.Fatalf("database CONNECT grant misses %q", principal)
		}
	}
	if strings.Contains(grant, "PUBLIC") || strings.Contains(grant, "internal_rpc_authority_owner") {
		t.Fatal("database CONNECT grant widened beyond exact runtime principals")
	}
}
