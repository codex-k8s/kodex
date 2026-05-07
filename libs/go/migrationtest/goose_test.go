package migrationtest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAssertGooseMigrationFilesAcceptsValidFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	migrationPath := filepath.Join(dir, "20260507120000_valid.sql")
	content := "-- +goose Up\nSELECT 1;\n-- +goose Down\nSELECT 1;\n"
	if err := os.WriteFile(migrationPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write migration: %v", err)
	}

	AssertGooseMigrationFiles(t, dir)
}

func TestGooseUpStatementsReturnsOrderedUpStatements(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeMigration(t, dir, "20260507130000_second.sql", "-- +goose Up\nSELECT 2;\n-- +goose Down\nSELECT -2;\n")
	writeMigration(t, dir, "20260507120000_first.sql", "-- +goose Up\nSELECT 1;\nSELECT 1 + 1;\n-- +goose Down\nSELECT -1;\n")

	statements := GooseUpStatements(t, dir)
	expected := []string{"SELECT 1", "SELECT 1 + 1", "SELECT 2"}
	if len(statements) != len(expected) {
		t.Fatalf("statements = %v, want %v", statements, expected)
	}
	for i := range expected {
		if statements[i] != expected[i] {
			t.Fatalf("statement[%d] = %q, want %q", i, statements[i], expected[i])
		}
	}
}

func writeMigration(t *testing.T, dir string, name string, content string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write migration %s: %v", name, err)
	}
}
