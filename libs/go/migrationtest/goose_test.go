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
