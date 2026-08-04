package admin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeAgentBindingProducerStartsFromCommittedBotTurn(t *testing.T) {
	migrationPath := filepath.Join("..", "migrations", "000039_runtime_agent_binding_discovery.sql")
	raw, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatal(err)
	}
	migration := string(raw)
	for _, required := range []string{
		"AFTER INSERT ON matter_codex_agent_session_turns",
		"agent_session_turn_id, agent_run_id, source_ref",
		"NEW.id, NEW.run_id, NEW.mattermost_post_id",
		"ON CONFLICT (agent_session_turn_id) DO NOTHING",
	} {
		if !strings.Contains(migration, required) {
			t.Fatalf("bot turn producer misses durable invariant %q", required)
		}
	}
	for _, name := range []string{
		"runtime_agent_binding_discovery__claim.sql",
		"runtime_agent_binding_discovery__complete.sql",
		"runtime_agent_binding_discovery__retry.sql",
	} {
		queryRaw, readErr := queryFiles.ReadFile(filepath.Join("sql", name))
		if readErr != nil || !strings.HasPrefix(string(queryRaw), "-- name: ") {
			t.Fatalf("discovery query %s has no canonical named SQL header: %v", name, readErr)
		}
	}
}
