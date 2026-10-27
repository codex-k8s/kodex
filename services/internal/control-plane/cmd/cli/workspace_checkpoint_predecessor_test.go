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

// checkpointCatalogFixtures contains only reversible schema mutations in the disposable upgrade database.
// checkpointCatalogFixtures содержит только обратимые изменения схемы одноразовой базы обновления.
//
//go:embed testdata/sql/checkpoint_catalog_*.sql
var checkpointCatalogFixtures embed.FS

// TestWorkspaceCheckpointMigrationPredecessor applies the real015→016 upgrade and rejects archive catalog drift on both sides.
// It uses no runtime data or external credentials and rolls back every negative schema mutation.
// TestWorkspaceCheckpointMigrationPredecessor применяет настоящее обновление015→016 и отклоняет изменения каталога с обеих сторон.
// Проверка не использует рабочие данные или внешние реквизиты и откатывает каждое негативное изменение схемы.
func TestWorkspaceCheckpointMigrationPredecessor(t *testing.T) {
	dsn := os.Getenv("KODEX_WORKFLOW_REVIEW_QA_UPGRADE_DSN")
	if dsn == "" {
		t.Skip("dedicated disposable upgrade database is required")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil || config.Host != "127.0.0.1" || config.Port < 1024 || config.Database != "control_plane" ||
		config.User != "control_plane_migrator" || config.Password != "" || config.TLSConfig != nil || len(config.Fallbacks) != 0 ||
		os.Getenv("KODEX_WORKFLOW_REVIEW_QA_UPGRADE_DISPOSABLE") != "dedicated-loopback-container" {
		t.Fatal("invalid disposable checkpoint upgrade configuration")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Second)
	defer cancel()
	database := stdlib.OpenDB(*config)
	database.SetMaxOpenConns(1)
	defer database.Close()
	var empty bool
	if err := database.QueryRowContext(ctx, schedulerUpgradeMetadataFixture).Scan(&empty); err != nil || !empty {
		t.Fatal("checkpoint upgrade requires empty canonical bootstrap metadata")
	}
	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal("checkpoint upgrade dialect configuration failed")
	}
	if err := goose.UpToContext(ctx, database, "migrations", 20261026001500); err != nil {
		t.Fatal("full checkpoint predecessor migration failed", err)
	}
	observerConfig := config.Copy()
	observerConfig.User = "postgres"
	observer, err := pgx.ConnectConfig(ctx, observerConfig)
	if err != nil {
		t.Fatal("connect disposable checkpoint schema observer")
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer stop()
		if err := observer.Close(cleanup); err != nil {
			t.Error("close disposable checkpoint schema observer failed")
		}
	}()
	for _, profile := range []struct {
		name, file string
	}{
		{"predecessor015", "workspace-checkpoint-predecessor-guard.sql"},
		{"current016", "workspace-checkpoint-catalog-guard.sql"},
	} {
		if profile.name == "current016" && !t.Run("forward016", func(t *testing.T) {
			if err := goose.UpToContext(ctx, database, "migrations", 20261026001600); err != nil {
				t.Fatal("checkpoint migration failed", err)
			}
		}) {
			return
		}
		guard, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "tools", "release", profile.file))
		if err != nil {
			t.Fatal("read shipped checkpoint catalog proof", err)
		}
		if !t.Run(profile.name, func(t *testing.T) {
			if _, err := observer.Exec(ctx, string(guard)); err != nil {
				t.Fatal("exact checkpoint catalog rejected", err)
			}
			entries, err := checkpointCatalogFixtures.ReadDir("testdata/sql")
			if err != nil || len(entries) != 13 {
				t.Fatal("checkpoint drift fixture set changed")
			}
			for _, entry := range entries {
				name := strings.TrimSuffix(strings.TrimPrefix(entry.Name(), "checkpoint_catalog_"), ".sql")
				t.Run(name, func(t *testing.T) {
					mutation, err := checkpointCatalogFixtures.ReadFile("testdata/sql/" + entry.Name())
					if err != nil {
						t.Fatal("read checkpoint catalog drift fixture")
					}
					tx, err := observer.Begin(ctx)
					if err != nil {
						t.Fatal("begin disposable checkpoint mutation")
					}
					defer func() {
						cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
						defer stop()
						if err := tx.Rollback(cleanup); err != nil {
							t.Error("rollback disposable checkpoint mutation failed")
						}
					}()
					if _, err := tx.Exec(ctx, string(mutation)); err != nil {
						t.Fatal("apply disposable checkpoint schema mutation", err)
					}
					_, err = tx.Exec(ctx, string(guard))
					var failure *pgconn.PgError
					if !errors.As(err, &failure) || failure.Code != "P0001" || !strings.HasPrefix(failure.Message, "workspace checkpoint ") {
						t.Fatal("changed checkpoint catalog was not rejected")
					}
				})
			}
		}) {
			return
		}
	}
	t.Run("repeat016", func(t *testing.T) {
		for attempt := 0; attempt < 2; attempt++ {
			if err := goose.UpToContext(ctx, database, "migrations", 20261026001600); err != nil {
				t.Fatal("repeated checkpoint migration failed", err)
			}
			version, err := goose.GetDBVersionContext(ctx, database)
			if err != nil || version != 20261026001600 {
				t.Fatal("checkpoint migration version readback failed")
			}
		}
	})
}
