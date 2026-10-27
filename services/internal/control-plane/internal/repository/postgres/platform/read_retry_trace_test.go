package platform

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// recordReadSerializationConflict counts only wrapped PostgreSQL40001 observations while holding
// the supplied initialized tracer's mutex. The caller first checks its own private query-context
// marker and supplies the matching nonnil mutex/counter pair. Other failures do not change state;
// the helper neither retries, classifies deadlocks as read conflicts nor logs driver details.
// recordReadSerializationConflict считает лишь wrapped PostgreSQL40001 observations под mutex
// назначенного initialized tracer. Caller сначала проверяет собственный private query-context
// marker и передаёт соответствующую nonnil mutex/counter пару. Другие failures не меняют state;
// helper не повторяет чтение, не считает deadlocks read conflicts и не логирует driver details.
func recordReadSerializationConflict(mu *sync.Mutex, conflicts *int, failure error) {
	var cause *pgconn.PgError
	if errors.As(failure, &cause) && cause.Code == "40001" {
		mu.Lock()
		*conflicts++
		mu.Unlock()
	}
}

// TestLockedReadTracersCountOnlyTaggedSerializationConflicts checks both actual tracer end
// callbacks ignore untagged/cross-tagged/false contexts and agree with the production read-retry
// classifier on nil, wrapped40001, deadlock and unrelated failures. Concurrent callbacks must
// preserve exact counts without touching attempt counters. This local race test uses no database
// and does not replace the opt-in two-connection PostgreSQL scenarios.
// TestLockedReadTracersCountOnlyTaggedSerializationConflicts проверяет игнорирование untagged,
// cross-tagged и false contexts обоими actual tracer end callbacks и согласие с production
// read-retry classifier для nil, wrapped40001, deadlock и прочих failures. Concurrent callbacks
// сохраняют точные counts без изменения attempt counters. Локальный race test не использует БД
// и не заменяет opt-in two-connection PostgreSQL scenarios.
func TestLockedReadTracersCountOnlyTaggedSerializationConflicts(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		failure error
		want    int
	}{
		{"success", nil, 0},
		{"serialization", &pgconn.PgError{Code: "40001"}, 1},
		{"wrapped serialization", fmt.Errorf("synthetic wrapper: %w", &pgconn.PgError{Code: "40001"}), 1},
		{"deadlock", &pgconn.PgError{Code: "40P01"}, 0},
		{"unavailable", &pgconn.PgError{Code: "08006"}, 0},
		{"cancellation", context.Canceled, 0},
		{"unclassified", errors.New("synthetic observation"), 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			assistant, files := &assistantLeaseReadTrace{}, &runtimeFilesReadTrace{}
			assistantContext := context.WithValue(t.Context(), assistantLeaseReadTraceKey{}, true)
			fileContext := context.WithValue(t.Context(), runtimeFilesReadTraceKey{}, true)
			data := pgx.TraceQueryEndData{Err: test.failure}
			for _, ctx := range []context.Context{t.Context(), fileContext, context.WithValue(assistantContext, assistantLeaseReadTraceKey{}, false)} {
				assistant.TraceQueryEnd(ctx, nil, data)
			}
			for _, ctx := range []context.Context{t.Context(), assistantContext, context.WithValue(fileContext, runtimeFilesReadTraceKey{}, false)} {
				files.TraceQueryEnd(ctx, nil, data)
			}
			if assistant.conflicts != 0 || files.conflicts != 0 {
				t.Fatal("tracer counted an unrelated query context")
			}
			var callbacks sync.WaitGroup
			for range 32 {
				callbacks.Add(1)
				go func() {
					defer callbacks.Done()
					assistant.TraceQueryEnd(assistantContext, nil, data)
					files.TraceQueryEnd(fileContext, nil, data)
				}()
			}
			callbacks.Wait()
			if assistant.conflicts != 32*test.want || files.conflicts != 32*test.want || assistant.attempts != 0 || files.attempts != 0 {
				t.Fatal("tracer conflict counts or attempt ownership changed")
			}
			if marked := errors.Is(assistantLockedReadError(test.failure, nil), errSerializableTransactionRetry); marked != (test.want == 1) {
				t.Fatal("tracer classification diverged from the production read retry")
			}
		})
	}
}
