package main

import (
	"testing"

	"github.com/pressly/goose/v3"
)

// TestTelegramNotificationsOwnerSeed prepares only the exact empty046 schema for disposable owner lifecycle tests; it is not a substitute for the full closed upgrade/restore qualification.
// TestTelegramNotificationsOwnerSeed готовит лишь точную пустую046 схему для disposable owner lifecycle tests и не заменяет полную закрытую upgrade/restore qualification.
func TestTelegramNotificationsOwnerSeed(t *testing.T) {
	ctx, database, _ := newWorkflowUpgradeDatabase(t)
	if err := goose.UpToContext(ctx, database, "migrations", 20261114140000); err != nil {
		t.Fatal("prepare Telegram owner schema", err)
	}
}
