package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSQLLiteralBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		matches bool
	}{
		{name: "select query", value: "SELECT id FROM projects WHERE ref = $1", matches: true},
		{name: "common table expression", value: "WITH candidate AS (SELECT id FROM runs) SELECT id FROM candidate", matches: true},
		{name: "update query", value: "UPDATE runs SET state = 'FAILED' WHERE id = $1", matches: true},
		{name: "grant query", value: "GRANT SELECT ON TABLE runs TO runtime", matches: true},
		{name: "action identifier", value: "REVOKE", matches: false},
		{name: "vault route", value: "/v1/database/static-roles/", matches: false},
		{name: "runtime diagnostic", value: "select claimable executions", matches: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if actual := sqlLiteral.MatchString(test.value); actual != test.matches {
				t.Fatalf("sqlLiteral.MatchString() = %t, want %t", actual, test.matches)
			}
		})
	}
}

func TestValidateQueryHeader(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	valid := filepath.Join(directory, "membership__list.sql")
	if err := os.WriteFile(valid, []byte("-- name: membership__list :many\nSELECT id FROM memberships;\n"), 0o600); err != nil {
		t.Fatalf("write valid query: %v", err)
	}
	if err := validateQueryHeader(valid); err != nil {
		t.Fatalf("validateQueryHeader(valid) error = %v", err)
	}

	invalid := filepath.Join(directory, "membership__get.sql")
	if err := os.WriteFile(invalid, []byte("-- name: membership__list :one\nSELECT id FROM memberships;\n"), 0o600); err != nil {
		t.Fatalf("write invalid query: %v", err)
	}
	if err := validateQueryHeader(invalid); err == nil {
		t.Fatal("validateQueryHeader(invalid) accepted a mismatched name")
	}
}
