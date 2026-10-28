package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// TestWorkspaceRestoreOwnerMigration extends the actual qualified017+SSH database through Goose.
// It preserves historical verifier closure and checks the new read-only proof after an idempotent second up.
// TestWorkspaceRestoreOwnerMigration обновляет настоящую qualified017+SSH базу через Goose.
// Он сохраняет закрытость исторических verifier и проверяет новый read-only proof после повторного up.
func TestWorkspaceRestoreOwnerMigration(t *testing.T) {
	if os.Getenv("KODEX_WORKFLOW_REVIEW_QA_UPGRADE_DSN") == "" {
		t.Skip("dedicated disposable restore upgrade database is required")
	}
	TestWorkspaceVolumeMigrationPredecessor(t)
	if t.Failed() {
		return
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	configuration, err := pgx.ParseConfig(os.Getenv("KODEX_WORKFLOW_REVIEW_QA_UPGRADE_DSN"))
	if err != nil {
		t.Fatal("parse qualified restore fixture")
	}
	database := stdlib.OpenDB(*configuration)
	database.SetMaxOpenConns(1)
	defer database.Close()
	observerConfiguration := configuration.Copy()
	observerConfiguration.User = "postgres"
	observer, err := pgx.ConnectConfig(ctx, observerConfiguration)
	if err != nil {
		t.Fatal("connect qualified restore observer")
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer stop()
		_ = observer.Close(cleanup)
	}()
	readControlPlaneMigrationProof(t, ctx, observer, "workspace-volume-ssh", "postcondition")
	for attempt := 0; attempt < 2; attempt++ {
		if err := goose.UpToContext(ctx, database, "migrations", 20261028000100); err != nil {
			t.Fatal("restore owner forward migration failed", err)
		}
		readControlPlaneMigrationProof(t, ctx, observer, "workspace-restore", "postcondition")
	}
}
