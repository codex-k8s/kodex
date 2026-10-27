package main

import (
	"context"
	"embed"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// workspacePredecessorFixtures contains schema-only mutations rolled back in the dedicated database.
// workspacePredecessorFixtures содержит только изменения схемы, откатываемые в отдельной БД.
//
//go:embed testdata/sql/workspace_predecessor_*.sql
var workspacePredecessorFixtures embed.FS

// TestSessionWorkspaceMigrationPredecessor applies the full014 predecessor and real015 using the migrator,
// then rejects catalog drift and proves repeated migration is a no-op.
// TestSessionWorkspaceMigrationPredecessor применяет полный014 и настоящую015 от имени migrator,
// затем отклоняет изменения каталога и доказывает отсутствие эффектов повторного обновления.
func TestSessionWorkspaceMigrationPredecessor(t *testing.T) {
	// Both upgrade modes share the existing isolated harness, not live credentials.
	// Оба режима обновления используют существующую изолированную оснастку, не живые реквизиты.
	dsn := os.Getenv("KODEX_WORKFLOW_REVIEW_QA_UPGRADE_DSN")
	if dsn == "" {
		t.Skip("dedicated disposable upgrade database is required")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil || config.Host != "127.0.0.1" || config.Port < 1024 ||
		config.Database != "control_plane" || config.User != "control_plane_migrator" ||
		config.Password != "" || config.TLSConfig != nil || len(config.Fallbacks) != 0 ||
		os.Getenv("KODEX_WORKFLOW_REVIEW_QA_UPGRADE_DISPOSABLE") != "dedicated-loopback-container" {
		t.Fatal("invalid disposable workspace upgrade configuration")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Second)
	defer cancel()
	database := stdlib.OpenDB(*config)
	database.SetMaxOpenConns(1)
	defer database.Close()
	var empty bool
	if err := database.QueryRowContext(ctx, schedulerUpgradeMetadataFixture).Scan(&empty); err != nil || !empty {
		t.Fatal("workspace upgrade requires empty canonical bootstrap metadata")
	}
	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal("workspace upgrade dialect configuration failed")
	}
	guardBytes, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "tools", "release", "session-workspace-catalog-guard.sql"))
	if err != nil {
		t.Fatal("read shipped workspace catalog proof", err)
	}
	guard := string(guardBytes)
	if err := goose.UpToContext(ctx, database, "migrations", 20261026001400); err != nil {
		t.Fatal("full workflow predecessor migration failed", err)
	}
	if !t.Run("forward015", func(t *testing.T) {
		if err := goose.UpToContext(ctx, database, "migrations", 20261026001500); err != nil {
			t.Fatal("workspace migration failed", err)
		}
	}) {
		return
	}
	observerConfig := config.Copy()
	observerConfig.User = "postgres"
	observer, err := pgx.ConnectConfig(ctx, observerConfig)
	if err != nil {
		t.Fatal("connect disposable schema observer")
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer stop()
		_ = observer.Close(cleanup)
	}()
	if !t.Run("catalog015", func(t *testing.T) {
		if _, err := observer.Exec(ctx, guard); err != nil {
			t.Fatal("exact workspace catalog rejected", err)
		}
	}) {
		return
	}
	entries, err := workspacePredecessorFixtures.ReadDir("testdata/sql")
	if err != nil || len(entries) != 11 {
		t.Fatal("workspace drift fixture set changed")
	}
	for _, entry := range entries {
		name := strings.TrimSuffix(strings.TrimPrefix(entry.Name(), "workspace_predecessor_"), ".sql")
		t.Run(name, func(t *testing.T) {
			mutation, err := workspacePredecessorFixtures.ReadFile("testdata/sql/" + entry.Name())
			if err != nil {
				t.Fatal("read workspace drift fixture")
			}
			tx, err := observer.Begin(ctx)
			if err != nil {
				t.Fatal("begin disposable schema mutation")
			}
			defer func() {
				cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
				defer stop()
				if err := tx.Rollback(cleanup); err != nil {
					t.Error("rollback disposable schema mutation failed")
				}
			}()
			if _, err := tx.Exec(ctx, string(mutation)); err != nil {
				t.Fatal("apply disposable schema mutation", err)
			}
			_, err = tx.Exec(ctx, guard)
			var failure *pgconn.PgError
			if !errors.As(err, &failure) || failure.Code != "P0001" ||
				!strings.HasPrefix(failure.Message, "session workspace ") {
				t.Fatal("changed workspace schema was not rejected by its predecessor guard")
			}
		})
	}
	t.Run("repeat015", func(t *testing.T) {
		for attempt := 0; attempt < 2; attempt++ {
			if err := goose.UpToContext(ctx, database, "migrations", 20261026001500); err != nil {
				t.Fatal("composed forward migration failed", err)
			}
			version, err := goose.GetDBVersionContext(ctx, database)
			if err != nil || version != 20261026001500 {
				t.Fatal("composed migration version readback failed")
			}
		}
	})
}
