package main

import (
	"context"
	"embed"
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

// workspacePredecessorFixtures contains schema-only mutations rolled back in the dedicated database.
// workspacePredecessorFixtures содержит только изменения схемы, откатываемые в отдельной БД.
//
//go:embed testdata/sql/workspace_predecessor_*.sql
var workspacePredecessorFixtures embed.FS

// TestSessionWorkspaceMigrationPredecessor proves both historical011 and exact01150 admission,
// rejects schema/ACL/trigger drift, and applies the real012 migration using its migrator identity.
// TestSessionWorkspaceMigrationPredecessor доказывает допуск прежнего011 и точного01150,
// отклоняет изменения схемы/прав/триггеров и применяет настоящую012 от имени migrator.
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
	source, err := migrations.ReadFile("migrations/20261026001200_workflow_exception_context.sql")
	if err != nil {
		t.Fatal("read shipped migration predecessor")
	}
	start := strings.Index(string(source), "DO $migration$")
	end := strings.Index(string(source), "END $migration$;")
	if start < 0 || end <= start {
		t.Fatal("shipped predecessor block is missing")
	}
	guard := string(source[start : end+len("END $migration$;")])
	if err := goose.UpToContext(ctx, database, "migrations", 20261026001100); err != nil {
		t.Fatal("legacy predecessor migration failed", err)
	}
	if !t.Run("legacy011", func(t *testing.T) {
		if _, err := database.ExecContext(ctx, guard); err != nil {
			t.Fatal("legacy predecessor rejected", err)
		}
	}) {
		return
	}
	if err := goose.UpToContext(ctx, database, "migrations", 20261026001150); err != nil {
		t.Fatal("workspace predecessor migration failed", err)
	}
	if !t.Run("workspace01150", func(t *testing.T) {
		if _, err := database.ExecContext(ctx, guard); err != nil {
			t.Fatal("exact workspace predecessor rejected", err)
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
	entries, err := workspacePredecessorFixtures.ReadDir("testdata/sql")
	if err != nil || len(entries) != 12 {
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
				!strings.HasPrefix(failure.Message, "workflow exception context ") {
				t.Fatal("changed workspace schema was not rejected by its predecessor guard")
			}
		})
	}
	t.Run("forward012", func(t *testing.T) {
		for attempt := 0; attempt < 2; attempt++ {
			if err := goose.UpToContext(ctx, database, "migrations", 20261026001200); err != nil {
				t.Fatal("composed forward migration failed", err)
			}
			version, err := goose.GetDBVersionContext(ctx, database)
			if err != nil || version != 20261026001200 {
				t.Fatal("composed migration version readback failed")
			}
		}
	})
}
