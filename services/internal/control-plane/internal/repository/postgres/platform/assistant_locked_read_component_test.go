package platform

import (
	"context"
	_ "embed"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed testdata/sql/assistant_locked_read_lease_id.sql
var queryAssistantLockedReadLeaseID string

//go:embed testdata/sql/assistant_locked_read_waiter.sql
var queryAssistantLockedReadWaiter string

type assistantLeaseReadTrace struct {
	mu                  sync.Mutex
	attempts, conflicts int
	started             chan struct{}
}
type assistantLeaseReadTraceKey struct{}

func (trace *assistantLeaseReadTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if !strings.Contains(data.SQL, "-- name: assistant_search_resolve_lease :one") {
		return ctx
	}
	trace.mu.Lock()
	trace.attempts++
	if trace.attempts == 1 {
		close(trace.started)
	}
	trace.mu.Unlock()
	return context.WithValue(ctx, assistantLeaseReadTraceKey{}, true)
}
func (trace *assistantLeaseReadTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if matched, _ := ctx.Value(assistantLeaseReadTraceKey{}).(bool); !matched {
		return
	}
	var pgError *pgconn.PgError
	if errors.As(data.Err, &pgError) && pgError.Code == "40001" {
		trace.mu.Lock()
		trace.conflicts++
		trace.mu.Unlock()
	}
}

// Два реальных PostgreSQL connections: renew удерживает UPDATE, fresh RR read
// берёт snapshot и ждёт FOR SHARE. Commit renew воспроизводит ровно 40001.
// Используется production renew SQL той же lease без смены fence/generation.
func testAssistantLockedReadConcurrentLeaseRenew(t *testing.T, parent context.Context, repository *Repository, reader value.Principal, lease map[string]any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	resolvedReader, err := repository.ResolvePrincipal(ctx, reader)
	if err != nil {
		t.Fatal("resolve synthetic verified principal")
	}
	current, err := repository.resolveScope(ctx, resolvedReader)
	if err != nil {
		t.Fatal("resolve disposable reader scope")
	}
	var beforeEffects, afterEffects string
	if err := repository.pool.QueryRow(ctx, queryAssistantConfigurationComponentEffects, current.organizationID).Scan(&beforeEffects); err != nil {
		t.Fatal("read synthetic effects before catalog")
	}
	trace := &assistantLeaseReadTrace{started: make(chan struct{})}
	config := repository.pool.Config()
	config.ConnConfig.Tracer = trace
	readPool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("create disposable catalog read connection")
	}
	defer readPool.Close()
	readRepository := *repository
	readRepository.pool = readPool
	readService, err := platformservice.New(&readRepository)
	if err != nil {
		t.Fatal("construct synthetic catalog owner service")
	}
	renew, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin disposable concurrent renew")
	}
	defer renew.Rollback(ctx)
	var leaseID string
	if err := renew.QueryRow(ctx, queryAssistantLockedReadLeaseID, stringMap(lease, "leaseRef")).Scan(&leaseID); err != nil {
		t.Fatal("resolve disposable lease")
	}
	if _, err := renew.Exec(ctx, queryRuntimeRenewexecutionUpdateRuntimeLeasesExpiresAtUpdatedAt, leaseID, time.Now().UTC().Add(30*time.Second)); err != nil {
		t.Fatal("hold actual lease renew update")
	}
	type outcome struct {
		response entity.AssistantConfigurationCatalogResponse
		err      error
	}
	completed := make(chan outcome, 1)
	go func() {
		response, err := readService.ListAssistantConfigurationCatalog(ctx, reader, stringMap(lease, "leaseRef"), stringMap(lease, "fence"), lease["generation"].(int64), entity.AssistantConfigurationCatalogRequest{Kind: "ROLE_ENVIRONMENTS", AssistantRef: stringMap(lease, "agentRef")})
		completed <- outcome{response, err}
	}()
	select {
	case <-trace.started:
	case <-ctx.Done():
		t.Fatal("catalog read did not reach lease authority")
	}
	for {
		var blocked bool
		if err := repository.pool.QueryRow(ctx, queryAssistantLockedReadWaiter).Scan(&blocked); err != nil {
			t.Fatal("observe disposable blocked lease read")
		}
		if blocked {
			break
		}
		timer := time.NewTimer(time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			t.Fatal("catalog read did not wait on concurrent renew")
		case <-timer.C:
		}
	}
	if err := renew.Commit(ctx); err != nil {
		t.Fatal("commit disposable concurrent lease renew")
	}
	var result outcome
	select {
	case result = <-completed:
	case <-ctx.Done():
		t.Fatal("catalog fresh retry exceeded request budget")
	}
	trace.mu.Lock()
	attempts, conflicts := trace.attempts, trace.conflicts
	trace.mu.Unlock()
	if result.err != nil || attempts != 2 || conflicts != 1 || result.response.Kind != "ROLE_ENVIRONMENTS" || len(result.response.Entries) == 0 || result.response.AssistantRef != stringMap(lease, "agentRef") {
		t.Fatalf("concurrent renew did not preserve fresh authoritative catalog: attempts=%d conflicts=%d", attempts, conflicts)
	}
	for _, denied := range []struct {
		lease, fence string
		generation   int64
	}{
		{stringMap(lease, "leaseRef"), "stale-fence", lease["generation"].(int64)},
		{stringMap(lease, "leaseRef"), stringMap(lease, "fence"), lease["generation"].(int64) + 1},
		{"lease_missing123", stringMap(lease, "fence"), lease["generation"].(int64)},
	} {
		trace.mu.Lock()
		before := trace.attempts
		trace.mu.Unlock()
		response, err := readService.ListAssistantConfigurationCatalog(ctx, reader, denied.lease, denied.fence, denied.generation, entity.AssistantConfigurationCatalogRequest{Kind: "ROLE_ENVIRONMENTS", AssistantRef: stringMap(lease, "agentRef")})
		trace.mu.Lock()
		after := trace.attempts
		trace.mu.Unlock()
		if !errors.Is(err, errs.ErrNotFound) || len(response.Entries) != 0 || after != before+1 {
			t.Fatal("stale or missing lease was retried or exposed a catalog")
		}
	}
	if err := repository.pool.QueryRow(ctx, queryAssistantConfigurationComponentEffects, current.organizationID).Scan(&afterEffects); err != nil {
		t.Fatal("read synthetic effects after catalog")
	}
	if beforeEffects != afterEffects {
		t.Fatal("catalog retry created durable side effects")
	}
}
