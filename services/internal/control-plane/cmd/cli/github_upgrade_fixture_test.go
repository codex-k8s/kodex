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

// testGitHubForwardUpgrade runs the shared disposable database and exact schema/ACL replay proof.
// Each caller pins its own predecessor, successor and proof profile; zero successor uses the CLI up path.
// testGitHubForwardUpgrade выполняет общую изолированную проверку схемы, ACL и replay.
// Каждый вызов закрепляет предыдущую и следующую версии и профиль; нулевой successor выбирает CLI up.
func testGitHubForwardUpgrade(t *testing.T, predecessor, successor int64, profile string) {
	t.Helper()
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
	if err := goose.UpToContext(ctx, database, "migrations", predecessor); err != nil {
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
	readControlPlaneMigrationProof(t, ctx, observer, profile, "precondition")
	var first []byte
	for attempt := 0; attempt < 2; attempt++ {
		var upgradeErr error
		if successor == 0 {
			upgradeErr = applyPostgresMigrations(ctx, database, "up")
		} else {
			upgradeErr = goose.UpToContext(ctx, database, "migrations", successor)
		}
		if upgradeErr != nil {
			t.Fatal("GitHub native forward upgrade failed", upgradeErr)
		}
		proof := readControlPlaneMigrationProof(t, ctx, observer, profile, "postcondition")
		if attempt == 0 {
			first = proof
		} else if !bytes.Equal(first, proof) {
			t.Fatal("GitHub schema proof changed after no-op upgrade")
		}
		if err := applyPostgresMigrations(ctx, database, workspaceBackupRestoreCommand); err != nil {
			t.Fatal("exact successor backup ACL restoration failed", err)
		}
		readControlPlaneMigrationProof(t, ctx, observer, profile, "postcondition")
	}
}
