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
	"github.com/jackc/pgx/v5/pgxpool"
)

// queryAssistantLockedReadLeaseID embeds exact fixture lease ref-to-ID lookup so the concurrent
// writer updates the same owner row as the reader. It resolves a disposable internal ID without
// authorizing lease renewal.
// queryAssistantLockedReadLeaseID встраивает exact fixture lease ref→ID lookup, чтобы concurrent
// writer менял ту же owner row, которую читает reader. Разрешает disposable internal ID без выдачи
// права lease renewal.
//
//go:embed testdata/sql/assistant_locked_read_lease_id.sql
var queryAssistantLockedReadLeaseID string

// queryAssistantLockedReadWaiter embeds observation of another same-database backend waiting on
// the named assistant lease-guard query. It is a test synchronization signal, not production
// readiness or proof of which caller owns a lease.
// queryAssistantLockedReadWaiter встраивает наблюдение другого same-database backend, ожидающего
// named assistant lease-guard query. Это test synchronization signal, а не production readiness и
// доказательство владельца lease.
//
//go:embed testdata/sql/assistant_locked_read_waiter.sql
var queryAssistantLockedReadWaiter string

// assistantLeaseReadTrace collects named lease-read attempt/serialization-conflict counts under a
// mutex and signals the first start through an initialized channel. It observes a disposable pgx
// connection without changing query or authority data.
// assistantLeaseReadTrace собирает под mutex число named lease-read attempts/serialization
// conflicts и сигнализирует первый start через назначенный channel. Наблюдает disposable pgx
// connection без изменения query и authority data.
type assistantLeaseReadTrace struct {
	mu                  sync.Mutex
	attempts, conflicts int
	started             chan struct{}
}

// assistantLeaseReadTraceKey is a private context marker linking the matched query start/end
// callbacks. It carries only a boolean trace tag and cannot supply principal, grant or owner
// scope.
// assistantLeaseReadTraceKey является private context marker связи matched query start/end
// callbacks. Хранит лишь boolean trace tag и не задаёт principal, grant и owner scope.
type assistantLeaseReadTraceKey struct{}

// TraceQueryStart matches the lease-guard SQL marker, counts the attempt and closes the
// initialized start channel only for the first match under lock. It returns a tagged child
// context; unmatched queries keep the original context and no raw SQL is logged.
// TraceQueryStart сопоставляет lease-guard SQL marker, считает attempt и закрывает назначенный
// start channel лишь для первого совпадения под lock. Возвращает tagged child context; остальные
// queries сохраняют исходный context, raw SQL не логируется.
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

// TraceQueryEnd counts only wrapped PostgreSQL40001 errors for a context tagged by the matching
// start callback. Other queries and failures are ignored; the tracer does not retry, map or
// suppress an error itself.
// TraceQueryEnd считает лишь wrapped PostgreSQL40001 errors для context, помеченного matching
// start callback. Прочие queries и failures игнорируются; tracer сам не повторяет, не отображает и
// не подавляет ошибку.
func (trace *assistantLeaseReadTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if matched, _ := ctx.Value(assistantLeaseReadTraceKey{}).(bool); !matched {
		return
	}
	recordReadSerializationConflict(&trace.mu, &trace.conflicts, data.Err)
}

// testAssistantLockedReadConcurrentLeaseRenew coordinates two real fixture connections within5s: a
// production renewal UPDATE blocks a fresh RepeatableRead lease read, then commit triggers40001
// and a whole fresh retry. It asserts two attempts/one conflict, exact catalog, single-attempt
// denial for stale/missing pins and no durable catalog effects. It needs disposable PostgreSQL; no
// runtime execution or actual remote RPC occurs.
// testAssistantLockedReadConcurrentLeaseRenew согласует две real fixture connections за5s:
// production renewal UPDATE блокирует fresh RepeatableRead lease read, затем Commit вызывает40001
// и полный fresh retry. Проверяет две attempts/один conflict, exact catalog, single-attempt denial
// stale/missing pins и отсутствие durable catalog effects. Требует disposable PostgreSQL; runtime
// execution и real remote RPC не выполняются.
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
	// outcome carries one catalog response/error through a buffered channel after the concurrent read
	// finishes. It does not publish partial authority or own the read transaction; the parent checks
	// the complete result and trace counts.
	// outcome передаёт один catalog response/error через buffered channel после concurrent read. Не
	// публикует partial authority и не владеет read transaction; parent проверяет полный результат и
	// trace counts.
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
