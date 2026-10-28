package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// TestWorkspaceBackupDefaultACLUpgrade reproduces the shipped backup reconciler's table grants on a real015 database.
// The canonical upgrade must preserve backup SELECT without bypassing frozen migration authority checks.
// TestWorkspaceBackupDefaultACLUpgrade воспроизводит grants штатного backup reconciler на настоящей015 базе.
// Штатное обновление должно сохранять backup SELECT без обхода исторических проверок полномочий миграций.
func TestWorkspaceBackupDefaultACLUpgrade(t *testing.T) {
	dsn := os.Getenv("KODEX_WORKFLOW_REVIEW_QA_UPGRADE_DSN")
	if dsn == "" {
		t.Skip("dedicated disposable backup upgrade database is required")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil || config.Host != "127.0.0.1" || config.Port < 1024 || config.Database != "control_plane" ||
		config.User != "control_plane_migrator" || config.Password != "" || config.TLSConfig != nil || len(config.Fallbacks) != 0 ||
		os.Getenv("KODEX_WORKFLOW_REVIEW_QA_UPGRADE_DISPOSABLE") != "dedicated-loopback-container" {
		t.Fatal("invalid disposable backup upgrade configuration")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Second)
	defer cancel()
	database := stdlib.OpenDB(*config)
	database.SetMaxOpenConns(1)
	defer database.Close()
	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal("backup upgrade dialect configuration failed")
	}
	if err := goose.UpToContext(ctx, database, "migrations", 20261026001500); err != nil {
		t.Fatal("backup upgrade predecessor failed", err)
	}
	observerConfig := config.Copy()
	observerConfig.User = "postgres"
	observer, err := pgx.ConnectConfig(ctx, observerConfig)
	if err != nil {
		t.Fatal("connect disposable backup observer")
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer stop()
		_ = observer.Close(cleanup)
	}()
	// These are the exact non-secret table/default grants of the shipped reconciler.
	// Это точные несекретные table/default grants штатного reconciler.
	if _, err := observer.Exec(ctx, `GRANT SELECT ON ALL TABLES IN SCHEMA public, control_plane TO kodex_backup_reader;
ALTER DEFAULT PRIVILEGES FOR ROLE control_plane_owner IN SCHEMA control_plane GRANT SELECT ON TABLES TO kodex_backup_reader;`); err != nil {
		t.Fatal("apply canonical backup grants", err)
	}
	if err := goose.UpToContext(ctx, database, "migrations", 20261026001600); err == nil {
		t.Fatal("frozen016 unexpectedly accepted backup ACL")
	} else {
		var cause *pgconn.PgError
		if !errors.As(err, &cause) || cause.Code != "P0001" || !strings.Contains(cause.Message, "session workspace authority changed") {
			t.Fatal("backup fixture did not reproduce the exact migration boundary")
		}
	}
	if err := applyPostgresMigrations(ctx, database, "up-workspace-backup-compatible"); err != nil {
		t.Fatal("backup-compatible forward upgrade failed", err)
	}
	var version int64
	if err := observer.QueryRow(ctx, "SELECT max(version_id) FROM public.goose_db_version WHERE is_applied").Scan(&version); err != nil || version != 20261028000100 {
		t.Fatal("backup-compatible upgrade did not reach the exact restore schema")
	}
	var readable bool
	if err := observer.QueryRow(ctx, "SELECT has_table_privilege('kodex_backup_reader','control_plane.session_workspace_storage','SELECT')").Scan(&readable); err != nil || !readable {
		t.Fatal("backup SELECT was not restored")
	}
}
