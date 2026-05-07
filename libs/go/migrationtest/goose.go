package migrationtest

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var migrationFileNamePattern = regexp.MustCompile(`^\d{14}_[a-z0-9_]+\.sql$`)

// AssertGooseMigrationFiles validates the common goose migration file contract.
func AssertGooseMigrationFiles(t testing.TB, dir string) {
	t.Helper()

	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("expected at least one SQL migration")
	}

	seenVersions := make(map[string]string, len(files))
	for _, file := range files {
		assertMigrationFile(t, file, seenVersions)
	}
}

func assertMigrationFile(t testing.TB, file string, seenVersions map[string]string) {
	t.Helper()

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

// GooseUpStatements returns ordered executable statements from goose Up sections.
func GooseUpStatements(t testing.TB, dir string) []string {
	t.Helper()

	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	sort.Strings(files)
	statements := make([]string, 0, len(files))
	for _, file := range files {
		contentBytes, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read migration %s: %v", file, err)
		}
		statements = append(statements, splitStatements(upSection(t, string(contentBytes), filepath.Base(file)))...)
	}
	return statements
}

func upSection(t testing.TB, content string, name string) string {
	t.Helper()

	upIndex := strings.Index(content, "-- +goose Up")
	downIndex := strings.Index(content, "-- +goose Down")
	if upIndex < 0 || downIndex < 0 || downIndex < upIndex {
		t.Fatalf("invalid goose migration markers in %s", name)
	}
	return content[upIndex+len("-- +goose Up") : downIndex]
}

func splitStatements(content string) []string {
	parts := strings.Split(content, ";")
	statements := make([]string, 0, len(parts))
	for _, part := range parts {
		statement := strings.TrimSpace(part)
		if statement != "" {
			statements = append(statements, statement)
		}
	}
	return statements
}
