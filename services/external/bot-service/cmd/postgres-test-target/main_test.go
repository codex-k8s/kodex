package main

import (
	"os"
	"slices"
	"testing"
)

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
	const sentinel = "synthetic-secret-sentinel"
	environment := postgresTestCommandEnvironment([]string{
		"PATH=/usr/bin",
		"MATTERCODEX_BOT_SERVICE_TEST_DATABASE_DSN_15=" + sentinel,
		"MATTERCODEX_POSTGRES_TEST_BOOTSTRAP_PROOF_16=" + sentinel,
	})
	if !slices.Equal(environment, []string{"PATH=/usr/bin"}) {
		t.Fatalf("filtered environment = %#v", environment)
	}
}
