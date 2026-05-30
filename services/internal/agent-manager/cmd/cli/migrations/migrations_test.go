package migrations

import (
	"os"
	"strings"
	"testing"

	migrationtest "github.com/codex-k8s/kodex/libs/go/migrationtest"
)

func TestGooseMigrationFiles(t *testing.T) {
	t.Parallel()
	migrationtest.AssertGooseMigrationFiles(t, ".")
}

func TestReadSurfaceIndexesCoverStandaloneFilters(t *testing.T) {
	t.Parallel()

	payload, err := os.ReadFile("20260530090000_agent_manager_read_surface_indexes.sql")
	if err != nil {
		t.Fatalf("read read surface migration: %v", err)
	}
	content := string(payload)
	for _, expected := range []string{
		"agent_manager_sessions_created_by_actor_idx",
		"ON agent_manager_sessions (created_by_actor_ref, updated_at DESC, id)",
		"agent_manager_runs_provider_pull_request_idx",
		"ON agent_manager_runs ((provider_target->>'pull_request_ref'), updated_at DESC, id)",
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("read surface migration misses %q:\n%s", expected, content)
		}
	}
}
