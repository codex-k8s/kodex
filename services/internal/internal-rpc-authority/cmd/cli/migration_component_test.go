package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// TestAuthorityBaselineGooseComponent использует только порт одноразового
// контейнера публичной оснастки; произвольный DSN тест не принимает.
func TestAuthorityBaselineGooseComponent(t *testing.T) {
	rawPort := os.Getenv("KODEX_AUTHORITY_MIGRATION_TEST_PORT")
	if rawPort == "" {
		t.Skip("disposable migration database is required")
	}
	port, err := strconv.ParseUint(rawPort, 10, 16)
	if err != nil || port == 0 {
		t.Fatal("invalid disposable migration port")
	}
	config, err := pgx.ParseConfig(fmt.Sprintf(
		"postgresql://internal_rpc_authority_migrator@127.0.0.1:%d/internal_rpc_authority?sslmode=disable", port,
	))
	if err != nil {
		t.Fatal("invalid disposable migration configuration")
	}
	database := stdlib.OpenDB(*config)
	defer database.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	goose.SetBaseFS(migrations)
	goose.SetTableName("public.goose_db_version")
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal("configure migration dialect")
	}
	// Сначала устанавливаем опубликованный baseline, затем проверяем реальное
	// обновление этой БД. Второй up не меняет ни схему, ни историю Goose.
	if err := goose.UpToContext(ctx, database, "migrations", 20260823000100); err != nil {
		t.Fatalf("apply published baseline: %T", err)
	}
	var baselineRows int64
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM public.goose_db_version").Scan(&baselineRows); err != nil || baselineRows != 2 {
		t.Fatal("published baseline history readback failed")
	}
	var firstRows, firstID int64
	if err := goose.UpToContext(ctx, database, "migrations", 20260906000100); err != nil {
		t.Fatal("apply published workload boundary")
	}
	testSnapshotGenerationUpgradeRejection(t, port)
	for attempt := range 2 {
		if err := goose.UpContext(ctx, database, "migrations"); err != nil {
			t.Fatalf("apply authority migration attempt %d: %T", attempt+1, err)
		}
		version, err := goose.GetDBVersionContext(ctx, database)
		if err != nil || version != 20260907000100 {
			t.Fatal("authority migration version readback failed")
		}
		var rows, maximumID int64
		if err := database.QueryRowContext(ctx,
			"SELECT count(*), max(id) FROM public.goose_db_version",
		).Scan(&rows, &maximumID); err != nil || rows != 4 {
			t.Fatal("authority migration history readback failed")
		}
		if attempt == 0 {
			firstRows, firstID = rows, maximumID
		} else if rows != firstRows || maximumID != firstID {
			t.Fatal("repeated migration changed applied history")
		}
	}
	t.Run("workload boundary", func(t *testing.T) {
		testWorkloadDatabaseBoundary(t, port)
	})
}
