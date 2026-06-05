package migrations

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	migrationtest "github.com/codex-k8s/kodex/libs/go/migrationtest"
)

func TestGooseMigrationFiles(t *testing.T) {
	migrationtest.AssertGooseMigrationFiles(t, ".")
}

func TestPlatformEventLogMigrationsMatchLibraryFixtures(t *testing.T) {
	serviceMigrations := readMigrationSet(t, ".")
	libraryMigrations := readMigrationSet(t, "../../../../../../libs/go/eventlog/migrations")
	if len(serviceMigrations) != len(libraryMigrations) {
		t.Fatalf("migration count = %d, fixture count = %d", len(serviceMigrations), len(libraryMigrations))
	}
	for name, serviceMigration := range serviceMigrations {
		libraryMigration, ok := libraryMigrations[name]
		if !ok {
			t.Fatalf("library fixture is missing migration %s", name)
		}
		if serviceMigration != libraryMigration {
			t.Fatalf("platform-event-log migration %s must match libs/go/eventlog integration fixture", name)
		}
	}
}

func TestCheckpointRetryRepairMigrationContract(t *testing.T) {
	migration, err := os.ReadFile("20260605090000_platform_event_log_checkpoint_retry_fields.sql")
	if err != nil {
		t.Fatalf("read retry repair migration: %v", err)
	}
	content := string(migration)
	required := []string{
		"ADD COLUMN IF NOT EXISTS retry_sequence_id bigint NOT NULL DEFAULT 0",
		"ADD COLUMN IF NOT EXISTS retry_attempt integer NOT NULL DEFAULT 0",
		"ADD COLUMN IF NOT EXISTS last_error text NOT NULL DEFAULT ''",
		"ALTER COLUMN retry_sequence_id SET NOT NULL",
		"ALTER COLUMN retry_attempt SET NOT NULL",
		"ALTER COLUMN last_error SET NOT NULL",
		"DO $$",
		"platform_event_consumer_retry_sequence_chk",
		"platform_event_consumer_retry_attempt_chk",
		"CHECK (retry_sequence_id >= 0)",
		"CHECK (retry_attempt >= 0)",
	}
	for _, fragment := range required {
		if !strings.Contains(content, fragment) {
			t.Fatalf("retry repair migration is missing %q", fragment)
		}
	}
	if !strings.Contains(content, "Откат намеренно пустой") {
		t.Fatal("retry repair migration Down section must document the no-op rollback")
	}
}

func readMigrationSet(t *testing.T, dir string) map[string]string {
	t.Helper()

	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		t.Fatalf("glob migrations in %s: %v", dir, err)
	}
	if len(files) == 0 {
		t.Fatalf("expected migrations in %s", dir)
	}
	sort.Strings(files)
	migrations := make(map[string]string, len(files))
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read migration %s: %v", file, err)
		}
		migrations[filepath.Base(file)] = string(content)
	}
	return migrations
}
