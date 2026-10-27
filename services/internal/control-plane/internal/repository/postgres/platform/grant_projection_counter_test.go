package platform

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
)

// grantProjectionCounter counts only the two exact full connection/grant queries for both
// assistant grant-plan fixtures. Its zero value supports concurrent callbacks; it retains no
// SQL, arguments, results or errors and does not measure all database reads or elapsed time.
// grantProjectionCounter считает только два точных полных запроса соединения и разрешения для
// обеих тестовых оснасток планов ассистентов. Нулевое значение поддерживает конкурентные callbacks;
// счётчик не хранит SQL, аргументы, результаты и ошибки, не измеряет все чтения БД либо время.
type grantProjectionCounter struct{ fullReads atomic.Int64 }

// TraceQueryStart increments once for either exact query, independently of its arguments or
// eventual outcome, and returns the original context without changing query execution.
// TraceQueryStart увеличивает счётчик один раз для любого из двух точных запросов независимо от
// аргументов и итогового результата и возвращает исходный контекст без изменения исполнения.
func (counter *grantProjectionCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if data.SQL == queryQueriesAttachconnectionSelectIntegrationGrantsOrganizationIdConnectionIdRef || data.SQL == queryQueriesGetintegrationconnectionSelectIntegrationConnectionsOrganizationIdRef {
		counter.fullReads.Add(1)
	}
	return ctx
}

// TraceQueryEnd intentionally ignores completion: failed queries were already counted at start,
// and neither errors nor results are retained by this test-only observer.
// TraceQueryEnd намеренно игнорирует завершение: ошибочные запросы уже учтены в начале, а ошибки
// и результаты не сохраняются этим тестовым наблюдателем.
func (*grantProjectionCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

// TestGrantProjectionCounterCountsExactQueries checks exact query selection, context identity,
// outcome-independent counting, per-instance isolation and concurrent callbacks without a DB.
// TestGrantProjectionCounterCountsExactQueries проверяет точный выбор запросов, идентичность
// контекста, независимость счётчика от результата, изоляцию экземпляров и конкурентные callbacks
// без подключения к БД.
func TestGrantProjectionCounterCountsExactQueries(t *testing.T) {
	var counter, other grantProjectionCounter
	var tracer pgx.QueryTracer = &counter
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, test := range []struct {
		sql   string
		count int64
	}{
		{queryQueriesAttachconnectionSelectIntegrationGrantsOrganizationIdConnectionIdRef, 1},
		{queryQueriesGetintegrationconnectionSelectIntegrationConnectionsOrganizationIdRef, 2},
		{"", 2},
		{"SELECT 1", 2},
		{" " + queryQueriesAttachconnectionSelectIntegrationGrantsOrganizationIdConnectionIdRef, 2},
		{queryQueriesGetintegrationconnectionSelectIntegrationConnectionsOrganizationIdRef + " ", 2},
	} {
		if returned := tracer.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{SQL: test.sql, Args: []any{"synthetic-private-marker"}}); returned != ctx {
			t.Fatal("projection counter changed query context")
		}
		tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: errors.New("synthetic query failure")})
		tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})
		if counter.fullReads.Load() != test.count || other.fullReads.Load() != 0 {
			t.Fatal("projection counter lost exact query or instance isolation")
		}
	}
	counter.fullReads.Store(0)
	var callbacks sync.WaitGroup
	for range 64 {
		callbacks.Add(1)
		go func() {
			defer callbacks.Done()
			for _, sql := range []string{queryQueriesAttachconnectionSelectIntegrationGrantsOrganizationIdConnectionIdRef, queryQueriesGetintegrationconnectionSelectIntegrationConnectionsOrganizationIdRef} {
				tracer.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{SQL: sql})
				tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})
			}
		}()
	}
	callbacks.Wait()
	if counter.fullReads.Load() != 128 || other.fullReads.Load() != 0 {
		t.Fatal("concurrent projection count is not exact")
	}
}
