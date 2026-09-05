package platform

import (
	"context"
	_ "embed"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/jackc/pgx/v5"
)

//go:embed testdata/sql/worker_grant_waiting.sql
var queryWorkerGrantWaiting string

func TestWorkerGrantHighWatermarkUsesCredentialGenerationAndRevision(t *testing.T) {
	for _, required := range []string{
		"(workload_id, credential_generation, revision, issued_at, expires_at)",
		"credential_generation < EXCLUDED.credential_generation",
		"credential_generation = EXCLUDED.credential_generation",
		"revision < EXCLUDED.revision",
		"revision = EXCLUDED.revision",
		"issued_at = EXCLUDED.issued_at",
		"expires_at = EXCLUDED.expires_at",
	} {
		if !strings.Contains(queryAcceptWorkerGrantHighWatermark, required) {
			t.Fatalf("worker grant watermark lost invariant %q", required)
		}
	}
	if strings.Contains(queryAcceptWorkerGrantHighWatermark, "UNION") {
		t.Fatal("watermark must not accept an unlocked statement snapshot")
	}
}

func testEmailWorkerWatermark(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	input := platformrepo.WorkerGrantInput{WorkloadID: "email-bridge", CredentialGeneration: 1, Revision: 10, IssuedAt: now, ExpiresAt: now.Add(time.Minute)}
	for _, step := range []struct {
		name                 string
		generation, revision uint64
		denied               bool
	}{
		{"first", 1, 10, false}, {"exact replay", 1, 10, false}, {"revision rollback", 1, 9, true},
		{"advance", 1, 11, false}, {"old replay", 1, 10, true}, {"new credential", 2, 1, false},
		{"old credential higher revision", 1, 100, true}, {"new credential replay", 2, 1, false},
	} {
		t.Run(step.name, func(t *testing.T) {
			input.CredentialGeneration, input.Revision = step.generation, step.revision
			err := repository.AcceptWorkerGrant(ctx, input)
			if step.denied && !errors.Is(err, errs.ErrForbidden) || !step.denied && err != nil {
				t.Fatalf("accept grant: %v", err)
			}
		})
	}
	input.IssuedAt = input.IssuedAt.Add(time.Second)
	if err := repository.AcceptWorkerGrant(ctx, input); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("different envelope replay: %v", err)
	}
	t.Run("concurrent old snapshot cannot authorize", func(t *testing.T) {
		writer, err := repository.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer writer.Rollback(ctx)
		var revision uint64
		if err := writer.QueryRow(ctx, queryAcceptWorkerGrantHighWatermark, "email-bridge", 3, 1, now, now.Add(time.Minute)).Scan(&revision); err != nil {
			t.Fatal(err)
		}
		reader, err := repository.pool.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Release()
		result := make(chan error, 1)
		go func() {
			var accepted uint64
			result <- reader.QueryRow(ctx, queryAcceptWorkerGrantHighWatermark, "email-bridge", 2, 1, now, now.Add(time.Minute)).Scan(&accepted)
		}()
		// Ждём подтверждённую блокировку строки, а не предполагаем порядок по задержке.
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			var blocked bool
			if err := repository.pool.QueryRow(ctx, queryWorkerGrantWaiting, reader.Conn().PgConn().PID()).Scan(&blocked); err != nil {
				writer.Rollback(ctx)
				<-result
				t.Fatal(err)
			}
			if blocked {
				break
			}
			select {
			case err := <-result:
				t.Fatalf("reader did not await writer: %v", err)
			case <-ticker.C:
			case <-ctx.Done():
				writer.Rollback(ctx)
				<-result
				t.Fatal(ctx.Err())
			}
		}
		if err := writer.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if err := <-result; !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("concurrent rollback accepted: %v", err)
		}
	})
}
