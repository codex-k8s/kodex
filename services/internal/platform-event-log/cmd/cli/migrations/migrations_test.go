package migrations

import (
	"os"
	"testing"

	migrationtest "github.com/codex-k8s/kodex/libs/go/migrationtest"
)

func TestGooseMigrationFiles(t *testing.T) {
	migrationtest.AssertGooseMigrationFiles(t, ".")
}

func TestPlatformEventLogMigrationMatchesLibraryFixture(t *testing.T) {
	serviceMigration, err := os.ReadFile("20260504130000_platform_event_log.sql")
	if err != nil {
		t.Fatalf("read platform event log migration: %v", err)
	}
	libraryMigration, err := os.ReadFile("../../../../../../libs/go/eventlog/migrations/20260504130000_platform_event_log.sql")
	if err != nil {
		t.Fatalf("read eventlog library migration fixture: %v", err)
	}
	if string(serviceMigration) != string(libraryMigration) {
		t.Fatal("platform-event-log migration must match libs/go/eventlog integration fixture")
	}
}
