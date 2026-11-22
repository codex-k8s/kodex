package platform

import (
	"context"
	"errors"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"testing"
	"time"
)

// kubernetesRuntimeReadTrace observes the exact post-origin Connection lock without changing production timing or authority.
// kubernetesRuntimeReadTrace наблюдает точную блокировку Connection после origin, не изменяя production authority.
type kubernetesRuntimeReadTrace struct{ started chan uint32 }

// TraceQueryStart identifies the backend that has already passed the live origin query.
// TraceQueryStart определяет backend, уже прошедший проверку действующего origin.
func (t *kubernetesRuntimeReadTrace) TraceQueryStart(ctx context.Context, c *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if d.SQL == queryConfigurationChangeconnectionLock {
		select {
		case t.started <- c.PgConn().PID():
		default:
		}
	}
	return ctx
}

// TraceQueryEnd leaves all database outcomes unchanged; no query data is retained.
// TraceQueryEnd сохраняет исходы БД без изменений; данные запросов не сохраняются.
func (*kubernetesRuntimeReadTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// testKubernetesRuntimeExpiryDuringConnectionLock proves actual expiry while waiting cannot disclose even pending metadata.
// A separate transaction blocks Connection only after a valid live origin; the lease is restored solely in disposable fixtures.
// testKubernetesRuntimeExpiryDuringConnectionLock доказывает отказ после реального истечения lease при ожидании Connection.
// Другая транзакция блокирует Connection после действующего origin; lease восстанавливается лишь в disposable fixture.
func testKubernetesRuntimeExpiryDuringConnectionLock(t *testing.T, parent context.Context, r *Repository, p value.Principal, q query.KubernetesRuntimeRead, connection string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	trace := &kubernetesRuntimeReadTrace{started: make(chan uint32, 1)}
	cfg := r.pool.Config()
	cfg.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		cancel()
		t.Fatal("create runtime read trace pool")
	}
	copyRepository := *r
	copyRepository.pool = pool
	service, err := platformservice.New(&copyRepository)
	if err != nil {
		cancel()
		pool.Close()
		t.Fatal("create runtime read service")
	}
	blocker, err := r.pool.Begin(ctx)
	if err != nil {
		cancel()
		pool.Close()
		t.Fatal("begin runtime read blocker")
	}
	type outcome struct {
		view entity.KubernetesRuntimeResult
		err  error
	}
	completed := make(chan outcome, 1)
	started, joined := false, false
	t.Cleanup(func() {
		cancel()
		cleanup, done := context.WithTimeout(context.Background(), 3*time.Second)
		defer done()
		if err := blocker.Rollback(cleanup); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Error("rollback runtime read blocker")
		}
		if started && !joined {
			select {
			case <-completed:
			case <-cleanup.Done():
				t.Error("runtime result reader did not join")
			}
		}
		pool.Close()
		if _, err := r.pool.Exec(cleanup, "UPDATE control_plane.runtime_leases SET expires_at=clock_timestamp()+interval '30 seconds' WHERE ref=$1", q.LeaseRef); err != nil {
			t.Error("restore disposable runtime lease")
		}
	})
	var connectionID string
	if err := blocker.QueryRow(ctx, "SELECT id::text FROM control_plane.integration_connections WHERE ref=$1 FOR UPDATE", connection).Scan(&connectionID); err != nil {
		t.Fatal("lock disposable Connection")
	}
	var expires time.Time
	if err := r.pool.QueryRow(ctx, "UPDATE control_plane.runtime_leases SET expires_at=clock_timestamp()+interval '3 seconds' WHERE ref=$1 RETURNING expires_at", q.LeaseRef).Scan(&expires); err != nil {
		t.Fatal("set disposable short lease")
	}
	started = true
	go func() { v, err := service.ReadKubernetesRuntimeInvocation(ctx, p, q); completed <- outcome{v, err} }()
	var pid uint32
	select {
	case pid = <-trace.started:
	case <-ctx.Done():
		t.Fatal("runtime reader did not pass live origin")
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		if err := r.pool.QueryRow(ctx, queryWorkerGrantWaiting, pid).Scan(&waiting); err != nil {
			t.Fatal("observe runtime reader blocker")
		}
		if waiting {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("runtime reader did not block on Connection")
		case <-ticker.C:
		}
	}
	for {
		var expired bool
		if err := r.pool.QueryRow(ctx, "SELECT clock_timestamp()>$1::timestamptz", expires).Scan(&expired); err != nil {
			t.Fatal("observe actual lease expiry")
		}
		if expired {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("runtime lease did not expire")
		case <-ticker.C:
		}
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal("release runtime read blocker")
	}
	select {
	case result := <-completed:
		joined = true
		if !errors.Is(result.err, errs.ErrNotFound) || result.view != (entity.KubernetesRuntimeResult{}) {
			t.Fatal("expired runtime reader disclosed result", result.err)
		}
	case <-ctx.Done():
		t.Fatal("runtime result reader did not complete")
	}
	// Restore before the parent test completes the original execution.
	// Восстановление до завершения исходного выполнения родительским тестом.
	if _, err := r.pool.Exec(ctx, "UPDATE control_plane.runtime_leases SET expires_at=clock_timestamp()+interval '30 seconds' WHERE ref=$1", q.LeaseRef); err != nil {
		t.Fatal("restore live disposable lease")
	}
}
