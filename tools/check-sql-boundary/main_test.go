package main

import (
	_ "embed"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// selectQueryFixture supplies a production-shaped SELECT literal to the detector.
// RU: selectQueryFixture передаёт детектору строку с формой production SELECT.
//
//go:embed testdata/select.sql
var selectQueryFixture string

// commonTableExpressionFixture supplies a WITH-query literal to the detector.
// RU: commonTableExpressionFixture передаёт детектору строку WITH-запроса.
//
//go:embed testdata/common_table_expression.sql
var commonTableExpressionFixture string

// updateQueryFixture supplies an UPDATE literal to the detector.
// RU: updateQueryFixture передаёт детектору строку UPDATE-запроса.
//
//go:embed testdata/update.sql
var updateQueryFixture string

// grantQueryFixture supplies a GRANT literal to the detector.
// RU: grantQueryFixture передаёт детектору строку GRANT-запроса.
//
//go:embed testdata/grant.sql
var grantQueryFixture string

// TestSQLLiteralBoundary checks representative SQL statements and benign
// lookalikes so the heuristic does not flag ordinary identifiers or routes.
// RU: TestSQLLiteralBoundary проверяет SQL-операторы и безопасные похожие
// строки, чтобы эвристика не помечала обычные идентификаторы и маршруты.
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

// TestValidateQueryHeader checks both a filename-matched query header and a
// header whose declared query name conflicts with its file.
// RU: TestValidateQueryHeader проверяет заголовок с совпадающим именем файла
// и заголовок, где объявленное имя противоречит файлу.
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

// TestInspectRepositoryIgnoresSQLLiteralsInTests ensures a test-only Go
// string does not become a production SQL ownership violation.
// RU: TestInspectRepositoryIgnoresSQLLiteralsInTests подтверждает, что строка
// в тестовом Go-файле не считается нарушением production SQL-границы.
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
