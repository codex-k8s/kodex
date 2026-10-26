package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"testing"
	"time"

	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// concurrentBootstrapReadback reads owner/effect cardinality and exact definition versions,
// never credential values; only native Bootstrap commands modify disposable business state.
// concurrentBootstrapReadback читает owner/effect cardinality и exact definition versions,
// исключая credential values; business state меняют лишь native Bootstrap команды.
//
//go:embed testdata/sql/concurrent_bootstrap_readback.sql
var concurrentBootstrapReadback string

// readConcurrentBootstrapState snapshots immutable owner counts and the full definition version map.
// readConcurrentBootstrapState снимает owner counts и полную карту версий definitions.
func readConcurrentBootstrapState(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (string, map[string]int64) {
	t.Helper()
	var owner, definitions string
	if err := pool.QueryRow(ctx, concurrentBootstrapReadback).Scan(&owner, &definitions); err != nil {
		t.Fatal("read concurrent bootstrap state")
	}
	var versions map[string]int64
	if json.Unmarshal([]byte(definitions), &versions) != nil || len(versions) == 0 {
		t.Fatal("bootstrap definition state invalid")
	}
	return owner, versions
}

// TestConcurrentBootstrapComponent proves the real SERIALIZABLE overlap using an already
// completed installation-lock query and an exact server-side waiter/blocker observation.
// TestConcurrentBootstrapComponent доказывает настоящий SERIALIZABLE overlap завершённым
// installation-lock query и exact server-side waiter/blocker observation.
func TestConcurrentBootstrapComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 75*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated bootstrap database")
	}
	t.Cleanup(pool.Close)
	r := newConfiguredAssistantComponentRepository(t, pool, "gpt-5")
	configureComponentArtifactScanner(t, r)
	if err = r.Bootstrap(ctx); err != nil {
		t.Fatal("initialize native bootstrap fixture")
	}
	before, versions := readConcurrentBootstrapState(t, ctx, pool)
	call := func(ctx context.Context, s *platformservice.Service) (string, error) { return "", s.Bootstrap(ctx) }
	a, release, adone := startHistoricalSecretCall(t, ctx, r, "repository_bootstrap_select_installation_singleton", false, call)
	blocker := awaitHistoricalSecretPhase(t, ctx, a, adone)
	b, _, bdone := startHistoricalSecretCall(t, ctx, r, "repository_bootstrap_select_installation_singleton", true, call)
	waiter := awaitHistoricalSecretPhase(t, ctx, b, bdone)
	awaitHistoricalSecretLock(t, ctx, pool, waiter, blocker, bdone)
	release()
	if result := awaitHistoricalSecretResult(t, ctx, adone); result.err != nil {
		t.Fatal("first native bootstrap failed")
	}
	result := awaitHistoricalSecretResult(t, ctx, bdone)
	var database *pgconn.PgError
	if !errors.As(result.err, &database) || database.Code != "40001" {
		t.Fatal("overlapped bootstrap did not expose expected private40001")
	}
	t.Log("native bootstrap overlap: exact server Lock observed; SQLSTATE=40001; no retry configured")
	after, next := readConcurrentBootstrapState(t, ctx, pool)
	if after != before || len(next) != len(versions) {
		t.Fatal("failed bootstrap changed owner/effect state")
	}
	for key, version := range versions {
		if next[key] != version+1 {
			t.Fatal("failed bootstrap committed a definition write")
		}
	}
}
