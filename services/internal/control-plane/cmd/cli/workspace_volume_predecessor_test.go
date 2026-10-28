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
// It also checks SSH composition after017 and preserves the complete existing review evidence guard.
// This schema-only proof does not accept the unfinished physical owner/controller lifecycle or enable the V2 profile.
// TestWorkspaceVolumeMigrationPredecessor применяет настоящее обновление016→017 в пустой disposable database и атомарно отклоняет drift.
// Дополнительно проверяет SSH после017 с сохранением всего прежнего review evidence guard.
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
	t.Run("ssh_preserves_review_evidence", func(t *testing.T) {
		query, err := workspaceVolumeFixtures.ReadFile("testdata/sql/volume_ssh_effect_guard.sql")
		if err != nil {
			t.Fatal("read combined guard metadata query")
		}
		var before string
		var boundary bool
		if err := observer.QueryRow(ctx, string(query)).Scan(&before, &boundary); err != nil || !boundary {
			t.Fatal("review evidence predecessor boundary is invalid")
		}
		raw, err := migrations.ReadFile("migrations/20261027000100_ssh_compose_runtime.sql")
		if err != nil {
			t.Fatal("read SSH migration")
		}
		up, _, found := strings.Cut(string(raw), "-- +goose Down")
		if !found {
			t.Fatal("SSH migration has no closed forward section")
		}
		drift, err := workspaceVolumeFixtures.ReadFile("testdata/sql/volume_ssh_effect_drift.sql")
		if err != nil {
			t.Fatal("read disposable effect guard drift")
		}
		tx, err := observer.Begin(ctx)
		if err != nil {
			t.Fatal("begin disposable effect guard drift")
		}
		if _, err := tx.Exec(ctx, string(drift)); err != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
			t.Fatal("prepare disposable effect guard drift")
		}
		_, migrationErr := tx.Exec(ctx, up)
		var failure *pgconn.PgError
		if !errors.As(migrationErr, &failure) || failure.Code != "P0001" {
			_ = tx.Rollback(context.WithoutCancel(ctx))
			t.Fatal("SSH migration accepted changed predecessor")
		}
		if err := tx.Rollback(ctx); err != nil {
			t.Fatal("rollback disposable effect guard drift")
		}
		for attempt := 0; attempt < 2; attempt++ {
			if err := goose.UpToContext(ctx, database, "migrations", 20261027000100); err != nil {
				t.Fatal("SSH migration after workspace failed", err)
			}
		}
		var after string
		if err := observer.QueryRow(ctx, string(query)).Scan(&after, &boundary); err != nil || !boundary {
			t.Fatal("combined SSH and evidence boundary is invalid")
		}
		_, tail, found := strings.Cut(up, "v_deployment:=$deployment$")
		deployment, _, closed := strings.Cut(tail, "$deployment$;")
		if !found || !closed || deployment == "" || strings.Count(after, deployment) != 1 {
			t.Fatal("combined SSH slot is missing or ambiguous")
		}
		if strings.Replace(after, deployment+"\n                    ", "", 1) != before {
			t.Fatal("SSH migration changed existing review evidence semantics")
		}
		version, err := goose.GetDBVersionContext(ctx, database)
		if err != nil || version != 20261027000100 {
			t.Fatal("combined migration version readback failed")
		}
		catalogSQL, err := workspaceVolumeFixtures.ReadFile("testdata/sql/volume_catalog_readback.sql")
		if err != nil {
			t.Fatal("read volume catalog metadata query")
		}
		var catalog string
		if err := observer.QueryRow(ctx, string(catalogSQL)).Scan(&catalog); err != nil {
			t.Fatal("read frozen volume catalog metadata", err)
		}
		t.Log("VOLUME_CATALOG_FINGERPRINTS " + catalog)
	})
}
