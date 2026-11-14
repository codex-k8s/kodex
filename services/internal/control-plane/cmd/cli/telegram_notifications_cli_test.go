package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"syscall"
	"testing"
)

// TestTelegramBackupCLIReceipt requires successful bounded migration/cleanup before the fixed version receipt and keeps failures silent; no database or runtime credentials are used.
// TestTelegramBackupCLIReceipt требует успешной bounded migration/cleanup перед fixed version receipt и сохраняет ошибки без stdout; БД и runtime credentials не используются.
func TestTelegramBackupCLIReceipt(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "completed", true: "failed"}[failed], func(t *testing.T) {
			fixture := &postgresProbeFixture{t: t, ping: func(context.Context) error { return nil }}
			calls := 0
			dependencies := postgresProbeDependencies(t, fixture, func(_ context.Context, _ *sql.DB, command string) error {
				if command != telegramBackupUpgradeCommand {
					t.Fatal("backup selector changed")
				}
				calls++
				if failed {
					return syscall.ECONNRESET
				}
				return nil
			})
			var output bytes.Buffer
			err := runPostgres(t.Context(), telegramBackupUpgradeCommand, &output, dependencies)
			if failed {
				if !errors.Is(err, syscall.ECONNRESET) || output.Len() != 0 {
					t.Fatal("failed backup maintenance emitted a completion receipt")
				}
			} else if err != nil || output.String() != telegramBackupUpgradeCompleted+"\n" {
				t.Fatal("backup maintenance receipt is incomplete")
			}
			if calls != 1 || fixture.pings != 1 || fixture.closes != 1 {
				t.Fatal("backup maintenance lifecycle changed")
			}
		})
	}
}
