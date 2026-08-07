package team

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5"
)

func TestQueryLoaderRejectsClosedContractViolations(t *testing.T) {
	tests := []struct {
		name     string
		files    fstest.MapFS
		expected map[string]string
	}{
		{name: "missing", files: fstest.MapFS{}, expected: map[string]string{"read": "one"}},
		{name: "unknown", files: fstest.MapFS{"sql/extra.sql": {Data: []byte("-- name: extra :one\n-- params: \nSELECT 1;\n")}}, expected: map[string]string{}},
		{name: "duplicate", files: fstest.MapFS{
			"sql/read.sql": {Data: []byte("-- name: read :one\n-- params: \nSELECT 1;\n")},
			"sql/copy.sql": {Data: []byte("-- name: read :one\n-- params: \nSELECT 1;\n")},
		}, expected: map[string]string{"read": "one"}},
		{name: "cardinality", files: fstest.MapFS{
			"sql/read.sql": {Data: []byte("-- name: read :many\n-- params: \nSELECT 1;\n")},
		}, expected: map[string]string{"read": "one"}},
		{name: "parameter mismatch", files: fstest.MapFS{
			"sql/read.sql": {Data: []byte("-- name: read :one\n-- params: @id\nSELECT @other::text;\n")},
		}, expected: map[string]string{"read": "one"}},
		{name: "positional", files: fstest.MapFS{
			"sql/read.sql": {Data: []byte("-- name: read :one\n-- params: \nSELECT $1::text;\n")},
		}, expected: map[string]string{"read": "one"}},
		{name: "multiple statements", files: fstest.MapFS{
			"sql/read.sql": {Data: []byte("-- name: read :one\n-- params: \nSELECT 1; SELECT 2;\n")},
		}, expected: map[string]string{"read": "one"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateQueryCorpus(test.files, test.expected); err == nil {
				t.Fatal("invalid SQL corpus was accepted")
			}
		})
	}
}

func TestEmbeddedQueryCorpusIsClosed(t *testing.T) {
	if err := validateTeamQueries(); err != nil {
		t.Fatal(err)
	}
	entries, err := embeddedTeamSQL.ReadDir("sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		raw, readErr := embeddedTeamSQL.ReadFile("sql/" + entry.Name())
		if readErr != nil {
			t.Fatal(readErr)
		}
		arguments := pgx.StrictNamedArgs{}
		for _, match := range queryArgPattern.FindAllStringSubmatch(string(raw), -1) {
			arguments[match[1]] = nil
		}
		if _, _, rewriteErr := arguments.RewriteQuery(context.Background(), nil, string(raw), nil); rewriteErr != nil {
			t.Fatalf("pgx strict named rewrite failed for %s: %v", entry.Name(), rewriteErr)
		}
	}
}

func TestRecoveryDeadlineUsesPostgreSQLClock(t *testing.T) {
	for _, path := range []string{
		"sql/team_operation__mark_ambiguous.sql",
		"sql/team_operation__claim_recovery.sql",
		"sql/workspace_mapping_operation__mark_ambiguous.sql",
		"sql/workspace_mapping_operation__claim_recovery.sql",
	} {
		raw, err := embeddedTeamSQL.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		query := string(raw)
		if !strings.Contains(query, "recovery_deadline") || !strings.Contains(query, "clock_timestamp()") {
			t.Fatalf("DB-time recovery contract is absent in %s", path)
		}
	}
}

func TestRuntimeRouteCheckpointSurvivesRouteDeletionAndRejectsRollback(t *testing.T) {
	lockQuery, err := embeddedTeamSQL.ReadFile("sql/mattermost_runtime_route__lock_project.sql")
	if err != nil {
		t.Fatal(err)
	}
	upsertQuery, err := embeddedTeamSQL.ReadFile("sql/mattermost_runtime_checkpoint__upsert.sql")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(lockQuery), "interaction_gateway_mattermost_runtime_checkpoints") ||
		strings.Contains(string(lockQuery), "interaction_gateway_mattermost_runtime_routes") ||
		!strings.Contains(string(upsertQuery), "mapping_generation < EXCLUDED.mapping_generation") ||
		!strings.Contains(string(upsertQuery), "mapping_digest_sha256 = EXCLUDED.mapping_digest_sha256") {
		t.Fatal("runtime route high-watermark is not durable or monotonic")
	}
}
