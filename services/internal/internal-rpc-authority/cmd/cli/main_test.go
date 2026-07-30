package main

import (
	"strings"
	"testing"
)

func TestParseCommand(t *testing.T) {
	for _, value := range []command{
		commandExpand,
		commandContract,
		commandUp,
		commandStatus,
		commandVersion,
	} {
		got, err := parseCommand([]string{"migrate", string(value)})
		if err != nil {
			t.Fatalf("parseCommand(%q) error = %v", value, err)
		}
		if got != value {
			t.Fatalf("parseCommand(%q) = %q", value, got)
		}
	}
}

func TestMigrationKeepsRuntimeIdentityAndReplayPrivilegesClosed(t *testing.T) {
	raw, err := migrations.ReadFile(
		"migrations/20260730000100_internal_rpc_authority_runtime.sql",
	)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	body := string(raw)
	for _, required := range []string{
		"FORCE ROW LEVEL SECURITY",
		"authority_runtime_database_identities_reconciler_read",
		"GRANT internal_rpc_authority_issuer",
		"GRANT internal_rpc_authority_verifier",
		"GRANT internal_rpc_authority_database_credential_reconciler",
		"GRANT SELECT, INSERT, DELETE\n    ON internal_rpc_authority.authority_replay_reservations",
		"GRANT SELECT, INSERT, DELETE\n    ON internal_rpc_authority.authority_proof_reservations",
		"REVOKE ALL ON ALL FUNCTIONS IN SCHEMA internal_rpc_authority FROM PUBLIC",
	} {
		if !strings.Contains(body, required) {
			t.Fatalf("migration misses %q", required)
		}
	}
	down := strings.Split(body, "-- +goose Down")
	if len(down) != 2 || strings.Contains(strings.ToUpper(down[1]), "DROP ") {
		t.Fatal("migration exposes a destructive down path")
	}
}

func TestParseCommandRejectsLegacyAndUnknownCommands(t *testing.T) {
	for _, arguments := range [][]string{
		{"migrate"},
		{"status"},
		{"migrate", "down"},
		{"migrate", "expand", "extra"},
	} {
		if _, err := parseCommand(arguments); err == nil {
			t.Fatalf("parseCommand(%q) accepted invalid arguments", arguments)
		}
	}
}
