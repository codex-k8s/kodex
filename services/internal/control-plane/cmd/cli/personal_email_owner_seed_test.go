package main

import (
	"testing"

	"github.com/pressly/goose/v3"
)

// TestPersonalEmailOwnerDevelopmentSeed prepares exact current-source049 for disposable owner
// development only. It proves neither the missing047/048 predecessor nor a full catalog upgrade,
// backup, restore or installed readiness; the normal closed schema selector remains unchanged.
// TestPersonalEmailOwnerDevelopmentSeed готовит точную current-source049 только для разработки
// disposable owner path. Не доказывает отсутствующий predecessor047/048, полный catalog upgrade,
// backup, restore либо installed readiness; обычный закрытый schema selector не меняется.
func TestPersonalEmailOwnerDevelopmentSeed(t *testing.T) {
	ctx, database, _ := newWorkflowUpgradeDatabase(t)
	if err := goose.UpToContext(ctx, database, "migrations", 20261114170000); err != nil {
		t.Fatal("prepare personal email development owner schema", err)
	}
	version, err := goose.GetDBVersionContext(ctx, database)
	if err != nil || version != 20261114170000 {
		t.Fatal("personal email development schema head mismatch", err)
	}
}
