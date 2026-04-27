package migrations

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var migrationFileNamePattern = regexp.MustCompile(`^\d{14}_[a-z0-9_]+\.sql$`)

func TestGooseMigrationFiles(t *testing.T) {
	files, err := filepath.Glob("*.sql")
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("expected at least one SQL migration")
	}

	seenVersions := make(map[string]string, len(files))
	for _, file := range files {
		name := filepath.Base(file)
		if !migrationFileNamePattern.MatchString(name) {
			t.Fatalf("migration %s does not match timestamp_name.sql", name)
		}
		version := name[:14]
		if previous, ok := seenVersions[version]; ok {
			t.Fatalf("migration %s duplicates timestamp from %s", name, previous)
		}
		seenVersions[version] = name

		contentBytes, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read migration %s: %v", name, err)
		}
		content := string(contentBytes)
		upIndex := strings.Index(content, "-- +goose Up")
		downIndex := strings.Index(content, "-- +goose Down")
		if upIndex < 0 {
			t.Fatalf("migration %s has no -- +goose Up marker", name)
		}
		if downIndex < 0 {
			t.Fatalf("migration %s has no -- +goose Down marker", name)
		}
		if downIndex < upIndex {
			t.Fatalf("migration %s has -- +goose Down before -- +goose Up", name)
		}
	}
}
