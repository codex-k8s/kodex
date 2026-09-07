package platform

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestWorkerGrantInstancesComponent(t *testing.T) {
	dsn := os.Getenv("KODEX_CONTROL_PLANE_TEST_DSN")
	if dsn == "" {
		t.Skip("KODEX_CONTROL_PLANE_TEST_DSN is not configured")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("open disposable PostgreSQL")
	}
	defer pool.Close()
	repository := &Repository{pool: pool}
	now := time.Now().UTC().Truncate(time.Second)
	grant := func(instance string, generation uint64, age time.Duration) platformrepo.WorkerGrantInput {
		issued := now.Add(-age)
		return platformrepo.WorkerGrantInput{WorkloadID: "image-promotion", InstanceID: instance,
			EnvelopeSHA256: strings.Repeat("a", 64), CredentialGeneration: generation,
			Revision: uint64(issued.Unix()), IssuedAt: issued, ExpiresAt: issued.Add(4 * time.Minute)}
	}
	accept := func(input platformrepo.WorkerGrantInput, allowed bool) {
		t.Helper()
		err := repository.AcceptWorkerGrant(ctx, input)
		if allowed && err != nil || !allowed && !errors.Is(err, errs.ErrForbidden) {
			t.Fatalf("grant acceptance: allowed=%v err=%v", allowed, err)
		}
	}
	legacy := grant("", 101, 70*time.Second)
	accept(legacy, true)
	a, b := grant(uuid.NewString(), 101, 50*time.Second), grant(uuid.NewString(), 101, 20*time.Second)
	for range 10 {
		accept(b, true)
		accept(a, true)
		accept(legacy, true)
	}
	advanced := grant(a.InstanceID, 101, 10*time.Second)
	accept(advanced, true)
	accept(a, false)
	altered := advanced
	altered.EnvelopeSHA256 = strings.Repeat("b", 64)
	accept(altered, false)
	expired := grant(uuid.NewString(), 102, 5*time.Minute)
	accept(expired, false)
	accept(b, true)
	current := grant(uuid.NewString(), 102, 0)
	accept(current, true)
	accept(grant(uuid.NewString(), 101, 0), false)
	accept(legacy, false)
	accept(grant("", 102, 0), true)
	// Новый repository не теряет устойчивый instance watermark.
	repository = &Repository{pool: pool}
	accept(current, true)
	accept(grant(a.InstanceID, 101, 0), false)

	t.Run("old generation waits for committed floor", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		var generation uint64
		if err := tx.QueryRow(ctx, queryWorkerGrantAcceptGeneration, "image-promotion", 103, now, now.Add(time.Minute)).Scan(&generation); err != nil {
			t.Fatal(err)
		}
		reader, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Release()
		result := make(chan error, 1)
		go func() {
			var value uint64
			result <- reader.QueryRow(ctx, queryWorkerGrantAcceptGeneration, "image-promotion", 102, now, now.Add(time.Minute)).Scan(&value)
		}()
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			var blocked bool
			if err := pool.QueryRow(ctx, queryWorkerGrantWaiting, reader.Conn().PgConn().PID()).Scan(&blocked); err != nil {
				t.Fatal(err)
			}
			if blocked {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("row lock was not observed")
			case <-ticker.C:
			}
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if err := <-result; !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("old generation accepted: %v", err)
		}
	})
}
