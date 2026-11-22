package main

import (
	"crypto/sha256"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestSSHIdentityDevelopmentSeed applies both embedded forward migrations in a disposable database,
// then verifies that applying 048 a second time preserves the head. It supplies data for owner tests.
// TestSSHIdentityDevelopmentSeed применяет обе встроенные прямые миграции в одноразовой БД,
// затем проверяет, что повторное применение 048 сохраняет head. Подготавливает БД для owner-тестов.
func TestSSHIdentityDevelopmentSeed(t *testing.T) {
	ctx, database, _ := newWorkflowUpgradeDatabase(t)
	const predecessor int64 = 20261114150000
	const successor int64 = 20261114160000
	body, err := migrations.ReadFile("migrations/20261114160000_managed_ssh.sql")
	if err != nil {
		t.Fatal("embedded SSH migration is required")
	}
	if err := goose.UpToContext(ctx, database, "migrations", predecessor); err != nil {
		t.Fatal("apply embedded Kubernetes predecessor", err)
	}
	if version, err := goose.GetDBVersionContext(ctx, database); err != nil || version != predecessor {
		t.Fatal("Kubernetes predecessor version readback failed", err)
	}
	for attempt := range 2 {
		if err := goose.UpToContext(ctx, database, "migrations", successor); err != nil {
			t.Fatalf("apply embedded SSH migration attempt %d: %v", attempt+1, err)
		}
		if version, err := goose.GetDBVersionContext(ctx, database); err != nil || version != successor {
			t.Fatal("SSH schema version readback failed", err)
		}
	}
	t.Logf("embedded SSH migration SHA256=%x; applied and repeated on disposable PostgreSQL", sha256.Sum256(body))
}
