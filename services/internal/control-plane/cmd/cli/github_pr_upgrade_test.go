package main

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// TestGitHubPRForwardUpgrade applies only the additive PR collaboration successor to a native029 database with real backup-reader SELECT, then proves once-only replay, exact schema readback, and idempotent ACL restoration.
// TestGitHubPRForwardUpgrade применяет только добавочное обновление GitHub к native029 БД с настоящими backup-reader SELECT, затем проверяет однократность replay, exact schema readback и идемпотентное восстановление ACL.
func TestGitHubPRForwardUpgrade(t *testing.T) {
	dsn := os.Getenv("KODEX_WORKFLOW_REVIEW_QA_UPGRADE_DSN")
	if dsn == "" {
		t.Skip("dedicated disposable GitHub upgrade database is required")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil || config.Host != "127.0.0.1" || config.Port < 1024 || config.Database != "control_plane" ||
		config.User != "control_plane_migrator" || config.Password != "" || config.TLSConfig != nil || len(config.Fallbacks) != 0 ||
		os.Getenv("KODEX_WORKFLOW_REVIEW_QA_UPGRADE_DISPOSABLE") != "dedicated-loopback-container" {
		t.Fatal("invalid disposable GitHub upgrade configuration")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Second)
	defer cancel()
	database := stdlib.OpenDB(*config)
	database.SetMaxOpenConns(1)
	defer database.Close()
	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal("GitHub upgrade dialect configuration failed")
	}
	if err := goose.UpToContext(ctx, database, "migrations", 20261029000100); err != nil {
		t.Fatal("GitHub upgrade predecessor failed", err)
	}
	observerConfig := config.Copy()
	observerConfig.User = "postgres"
	observer, err := pgx.ConnectConfig(ctx, observerConfig)
	if err != nil {
		t.Fatal("connect disposable GitHub observer")
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer stop()
		_ = observer.Close(cleanup)
	}()
	if _, err := observer.Exec(ctx, "GRANT SELECT ON ALL TABLES IN SCHEMA public, control_plane TO kodex_backup_reader; ALTER DEFAULT PRIVILEGES FOR ROLE control_plane_owner IN SCHEMA control_plane GRANT SELECT ON TABLES TO kodex_backup_reader"); err != nil {
		t.Fatal("prepare canonical backup SELECT before GitHub upgrade")
	}
	readControlPlaneMigrationProof(t, ctx, observer, "github-pr-backup", "precondition")
	var first []byte
	for attempt := 0; attempt < 2; attempt++ {
		if err := applyPostgresMigrations(ctx, database, "up"); err != nil {
			t.Fatal("GitHub PR native forward upgrade failed", err)
		}
		proof := readControlPlaneMigrationProof(t, ctx, observer, "github-pr-backup", "postcondition")
		if attempt == 0 {
			first = proof
		} else if !bytes.Equal(first, proof) {
			t.Fatal("GitHub schema proof changed after no-op upgrade")
		}
		if err := applyPostgresMigrations(ctx, database, workspaceBackupRestoreCommand); err != nil {
			t.Fatal("exact successor backup ACL restoration failed", err)
		}
		readControlPlaneMigrationProof(t, ctx, observer, "github-pr-backup", "postcondition")
	}
}
