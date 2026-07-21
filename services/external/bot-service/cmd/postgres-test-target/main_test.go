package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParsePostgresTestModeAcceptsOnlyDocumentedModes(t *testing.T) {
	for raw, expected := range map[string]postgresTestMode{
		"": postgresTestModeLocalBinaries, "local-binaries": postgresTestModeLocalBinaries,
		"scoped-dsn": postgresTestModeScopedDSN, "docker": postgresTestModeDocker, "kubernetes": postgresTestModeKubernetes,
	} {
		actual, err := parsePostgresTestMode(raw)
		if err != nil || actual != expected {
			t.Fatalf("parsePostgresTestMode(%q) = %q, %v", raw, actual, err)
		}
	}
	if _, err := parsePostgresTestMode("shared-production"); err == nil {
		t.Fatal("неизвестный PostgreSQL mode принят")
	}
}

func TestParsePostgresMajorsRequiresSupportedDistinctVersions(t *testing.T) {
	majors, err := parsePostgresMajors("15,16,15")
	if err != nil || !slices.Equal(majors, []string{"15", "16"}) {
		t.Fatalf("parsePostgresMajors() = %v, %v", majors, err)
	}
	for _, value := range []string{"", "14", "15,17", "15,"} {
		if _, err := parsePostgresMajors(value); err == nil {
			t.Fatalf("parsePostgresMajors(%q) не вернул ошибку", value)
		}
	}
}

func TestSelectMajorEnvironmentUsesOnlyScopedDisposableTarget(t *testing.T) {
	t.Setenv("MATTERCODEX_BOT_SERVICE_TEST_DATABASE_DSN", "generic-dsn")
	t.Setenv("MATTERCODEX_BOT_SERVICE_TEST_DATABASE_MARKER", "generic-marker")
	t.Setenv("MATTERCODEX_BOT_SERVICE_TEST_DATABASE_DSN_15", "postgres-15-dsn")
	t.Setenv("MATTERCODEX_BOT_SERVICE_TEST_DATABASE_MARKER_15", "postgres-15-marker")
	t.Setenv("MATTERCODEX_POSTGRES_TEST_BINDIR_15", "/synthetic/postgres/15/bin")
	restore, err := selectMajorEnvironment("15")
	if err != nil {
		t.Fatalf("selectMajorEnvironment() error = %v", err)
	}
	if os.Getenv("MATTERCODEX_POSTGRES_TEST_MAJOR") != "15" ||
		os.Getenv("MATTERCODEX_BOT_SERVICE_TEST_DATABASE_DSN") != "postgres-15-dsn" ||
		os.Getenv("MATTERCODEX_BOT_SERVICE_TEST_DATABASE_MARKER") != "postgres-15-marker" ||
		os.Getenv("MATTERCODEX_POSTGRES_TEST_BINDIR") != "/synthetic/postgres/15/bin" {
		t.Fatal("цель PostgreSQL для конкретной версии не выбрана")
	}
	restore()
	if os.Getenv("MATTERCODEX_BOT_SERVICE_TEST_DATABASE_DSN") != "generic-dsn" ||
		os.Getenv("MATTERCODEX_BOT_SERVICE_TEST_DATABASE_MARKER") != "generic-marker" {
		t.Fatal("исходная PostgreSQL-конфигурация не восстановлена")
	}
}

func TestSelectMajorEnvironmentRejectsIncompleteScopedPair(t *testing.T) {
	t.Setenv("MATTERCODEX_BOT_SERVICE_TEST_DATABASE_DSN_16", "postgres-16-dsn")
	t.Setenv("MATTERCODEX_BOT_SERVICE_TEST_DATABASE_MARKER_16", "")
	if _, err := selectMajorEnvironment("16"); err == nil {
		t.Fatal("неполная конфигурация конкретной версии принята")
	}
}

func TestPostgresTestCommandEnvironmentDropsMatrixSecretInputs(t *testing.T) {
	environment := postgresTestCommandEnvironment([]string{
		"PATH=/usr/bin",
		"MATTERCODEX_BOT_SERVICE_TEST_DATABASE_DSN_15=sentinel-a",
		"MATTERCODEX_POSTGRES_TEST_BOOTSTRAP_PROOF_16=sentinel-b",
		"MATTERCODEX_DATABASE_DSN=sentinel-c",
		"MATTERCODEX_MIGRATIONS_DATABASE_DSN=sentinel-d",
		"MATTERCODEX_POSTGRES_HOST=sentinel-e",
		"MATTERCODEX_POSTGRES_DB=sentinel-f",
		"PGHOST=sentinel-g",
		"PGPASSWORD=sentinel-h",
		"DATABASE_URL=sentinel-i",
		"POSTGRES_PASSWORD=sentinel-j",
		"HOME=/synthetic/home-with-pgpass",
		"GOFLAGS=-run=^$",
		"GOENV=/tmp/adversarial-goenv",
		"GOWORK=/tmp/adversarial-workspace",
	})
	if !slices.Equal(environment, []string{"PATH=/usr/bin"}) {
		t.Fatalf("filtered environment names = %q", environmentNames(environment))
	}
}

func TestMaterializeGoCacheEnvironmentUsesPreHomeDefaultsAndOfflineMode(t *testing.T) {
	home := t.TempDir()
	adversarial := filepath.Join(t.TempDir(), "must-not-pass")
	environment := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"GOMODCACHE=" + filepath.Join(adversarial, "mod"),
		"GOCACHE=" + filepath.Join(adversarial, "build"),
		"GOPATH=" + filepath.Join(adversarial, "gopath"),
		"GOFLAGS=-run=^$",
		"GOENV=/tmp/adversarial-goenv",
		"GOWORK=/tmp/adversarial-workspace",
		"GOPROXY=off",
		"GOSUMDB=off",
	}
	cacheEnvironment, err := materializeGoCacheEnvironment(context.Background(), environment)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for _, item := range cacheEnvironment {
		name, value, _ := strings.Cut(item, "=")
		values[name] = value
	}
	for name, expected := range map[string]string{
		"GOMODCACHE": filepath.Join(home, "go", "pkg", "mod"),
		"GOCACHE":    filepath.Join(home, ".cache", "go-build"),
		"GOPATH":     filepath.Join(home, "go"),
	} {
		if values[name] != expected || !filepath.IsAbs(values[name]) || strings.Contains(values[name], adversarial) {
			t.Fatalf("%s = %q, want pre-HOME default %q", name, values[name], expected)
		}
	}
	if offline := safeOfflineGoEnvironment(environment); !slices.Equal(offline, []string{"GOPROXY=off", "GOSUMDB=off"}) {
		t.Fatalf("offline Go environment = %q", offline)
	}
}

func environmentNames(environment []string) []string {
	names := make([]string, 0, len(environment))
	for _, item := range environment {
		name, _, _ := strings.Cut(item, "=")
		names = append(names, name)
	}
	return names
}

func TestExecutionSentinelRequiresExactMajorAndContent(t *testing.T) {
	directory, path, value, err := newPostgresExecutionSentinel("15")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(directory)
	if err := os.WriteFile(path, []byte("15\n"+value+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyPostgresExecutionSentinel(path, "15", value); err != nil {
		t.Fatal(err)
	}
	if err := verifyPostgresExecutionSentinel(path, "16", value); err == nil {
		t.Fatal("sentinel несовпадающей major-версии принят")
	}
}
