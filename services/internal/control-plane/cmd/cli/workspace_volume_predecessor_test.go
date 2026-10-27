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

// workspaceVolumeFixtures embeds only disposable schema mutations and metadata readback, never business rows or credentials.
// workspaceVolumeFixtures содержит только disposable schema mutations/metadata readback, без business rows либо credentials.
//
//go:embed testdata/sql/volume_*.sql
var workspaceVolumeFixtures embed.FS

// TestWorkspaceVolumeMigrationPredecessor applies the real016→017 upgrade in an empty disposable database and rejects drift atomically.
// This schema-only proof does not accept the unfinished physical owner/controller lifecycle or enable the V2 profile.
// TestWorkspaceVolumeMigrationPredecessor применяет настоящее обновление016→017 в пустой disposable database и атомарно отклоняет drift.
// Schema-only proof не принимает незавершённый physical owner/controller lifecycle и не включает профиль V2.
func TestWorkspaceVolumeMigrationPredecessor(t *testing.T) {
	dsn := os.Getenv("KODEX_WORKFLOW_REVIEW_QA_UPGRADE_DSN")
	if dsn == "" {
		t.Skip("dedicated disposable upgrade database is required")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil || config.Host != "127.0.0.1" || config.Port < 1024 || config.Database != "control_plane" ||
		config.User != "control_plane_migrator" || config.Password != "" || config.TLSConfig != nil || len(config.Fallbacks) != 0 ||
		os.Getenv("KODEX_WORKFLOW_REVIEW_QA_UPGRADE_DISPOSABLE") != "dedicated-loopback-container" {
		t.Fatal("invalid disposable volume upgrade configuration")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Second)
	defer cancel()
	database := stdlib.OpenDB(*config)
	database.SetMaxOpenConns(1)
	defer database.Close()
	var empty bool
	if err := database.QueryRowContext(ctx, schedulerUpgradeMetadataFixture).Scan(&empty); err != nil || !empty {
		t.Fatal("volume upgrade requires empty canonical bootstrap metadata")
	}
	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal("volume upgrade dialect configuration failed")
	}
	if err := goose.UpToContext(ctx, database, "migrations", 20261026001600); err != nil {
		t.Fatal("full volume predecessor migration failed", err)
	}
	observerConfig := config.Copy()
	observerConfig.User = "postgres"
	observer, err := pgx.ConnectConfig(ctx, observerConfig)
	if err != nil {
		t.Fatal("connect disposable volume observer")
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer stop()
		_ = observer.Close(cleanup)
	}()
	raw, err := migrations.ReadFile("migrations/20261026001700_workspace_volume_custody.sql")
	if err != nil {
		t.Fatal("read exact volume migration")
	}
	up := strings.SplitN(string(raw), "-- +goose Down", 2)[0]
	for _, name := range []string{"workspace_column", "checkpoint_trigger", "purge_body"} {
		if !t.Run("reject-"+name, func(t *testing.T) {
			mutation, err := workspaceVolumeFixtures.ReadFile("testdata/sql/volume_" + name + ".sql")
			if err != nil {
				t.Fatal("read disposable volume drift")
			}
			tx, err := observer.Begin(ctx)
			if err != nil {
				t.Fatal("begin disposable volume drift")
			}
			defer func() {
				cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
				defer stop()
				if err := tx.Rollback(cleanup); err != nil {
					t.Error("rollback disposable volume drift failed")
				}
			}()
			if _, err := tx.Exec(ctx, string(mutation)); err != nil {
				t.Fatal("apply disposable volume drift", err)
			}
			_, err = tx.Exec(ctx, up)
			var failure *pgconn.PgError
			if !errors.As(err, &failure) || failure.Code != "P0001" {
				t.Fatal("volume predecessor drift did not fail closed", err)
			}
		}) {
			return
		}
	}
	if err := goose.UpToContext(ctx, database, "migrations", 20261026001700); err != nil {
		t.Fatal("volume forward migration failed", err)
	}
	if err := goose.UpToContext(ctx, database, "migrations", 20261026001700); err != nil {
		t.Fatal("volume migration repeat failed", err)
	}
	version, err := goose.GetDBVersionContext(ctx, database)
	if err != nil || version != 20261026001700 {
		t.Fatal("volume version readback failed")
	}
	var tables int
	readback, err := workspaceVolumeFixtures.ReadFile("testdata/sql/volume_readback.sql")
	if err != nil {
		t.Fatal("read disposable volume metadata query")
	}
	if err := observer.QueryRow(ctx, string(readback)).Scan(&tables); err != nil || tables != 3 {
		t.Fatal("volume table readback failed")
	}
	if err := goose.DownToContext(ctx, database, "migrations", 20261026001600); err == nil {
		t.Fatal("volume custody was removed by rollback")
	}
	version, err = goose.GetDBVersionContext(ctx, database)
	if err != nil || version != 20261026001700 {
		t.Fatal("refused rollback changed volume version")
	}
}
