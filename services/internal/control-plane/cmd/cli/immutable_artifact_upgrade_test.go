package main

import (
	"context"
	_ "embed"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed testdata/immutable_artifact_upgrade_seed.sql
var immutableArtifactUpgradeSeed string

//go:embed testdata/immutable_artifact_upgrade_assert.sql
var immutableArtifactUpgradeAssert string

func TestImmutableArtifactProtocolUpgrade(t *testing.T) {
	dsn := os.Getenv("KODEX_ARTIFACT_REVISION_UPGRADE_DSN")
	if dsn == "" {
		t.Skip("dedicated disposable migration database is required")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil || config.Host != "127.0.0.1" || config.Port < 1024 || config.Database != "control_plane_artifact_revision_upgrade" || config.User != "control_plane_migrator" || config.Password != "" || config.TLSConfig != nil || len(config.Fallbacks) != 0 || os.Getenv("KODEX_ARTIFACT_REVISION_DISPOSABLE") != "dedicated-loopback-container" {
		t.Fatal("invalid disposable artifact migration configuration")
	}
	database := stdlib.OpenDB(*config)
	database.SetMaxOpenConns(1)
	defer database.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpToContext(ctx, database, "migrations", 20261008000100); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, immutableArtifactUpgradeSeed); err != nil {
		t.Fatalf("seed old schema: %v", err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := goose.UpContext(ctx, database, "migrations"); err != nil {
			t.Fatal(err)
		}
		if _, err := database.ExecContext(ctx, immutableArtifactUpgradeAssert); err != nil {
			t.Fatalf("backfill/replay preserved pins: %v", err)
		}
	}
}
