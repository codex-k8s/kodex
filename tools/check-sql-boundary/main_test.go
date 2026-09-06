package main

import (
	_ "embed"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

//go:embed testdata/select.sql
var selectQueryFixture string

//go:embed testdata/common_table_expression.sql
var commonTableExpressionFixture string

//go:embed testdata/update.sql
var updateQueryFixture string

//go:embed testdata/grant.sql
var grantQueryFixture string

func TestSQLLiteralBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		matches bool
	}{
		{name: "select query", value: selectQueryFixture, matches: true},
		{name: "common table expression", value: commonTableExpressionFixture, matches: true},
		{name: "update query", value: updateQueryFixture, matches: true},
		{name: "grant query", value: grantQueryFixture, matches: true},
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

	valid := filepath.Join("testdata", "valid", "membership__list.sql")
	if err := validateQueryHeader(valid); err != nil {
		t.Fatalf("validateQueryHeader(valid) error = %v", err)
	}

	invalid := filepath.Join("testdata", "invalid", "membership__get.sql")
	if err := validateQueryHeader(invalid); err == nil {
		t.Fatal("validateQueryHeader(invalid) accepted a mismatched name")
	}
}

func TestInspectRepositoryIgnoresSQLLiteralsInTests(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, directory := range []string{"libs", "tools"} {
		if err := os.Mkdir(filepath.Join(root, directory), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(root, "services", "example", "repository_test.go")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	const source = `package example

const fixture = "SET ROLE test_role"
`
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}

	violations, err := inspectRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("test SQL literal produced violations: %s", strings.Join(violations, "; "))
	}
}
