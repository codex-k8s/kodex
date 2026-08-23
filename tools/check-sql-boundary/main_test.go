package main

import (
	_ "embed"
	"path/filepath"
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
