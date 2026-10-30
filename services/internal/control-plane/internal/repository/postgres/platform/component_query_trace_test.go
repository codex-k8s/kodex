package platform

import (
	"context"
	"crypto/sha256"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// componentQueryRejectionTracer observes disposable query failures without SQL, arguments, messages or private diagnostic context.
// It retains only a query digest, bounded timing/error flags, SQLSTATE, constraint identifier and parsed function/line frames.
// componentQueryRejectionTracer наблюдает отказы disposable queries без SQL, аргументов, messages и private context.
// Сохраняются только digest запроса, время и признаки отказа, SQLSTATE, имя constraint и разобранные function/line frames.
type componentQueryRejectionTracer struct{ t *testing.T }

// componentQueryTraceKey keeps the per-query digest in the returned caller context without shared mutable state.
// componentQueryTraceKey хранит digest отдельного запроса в возвращённом context без общего mutable state.
type componentQueryTraceKey struct{}

// componentQueryTrace retains only a digest and monotonic start time; SQL and values are discarded.
// componentQueryTrace сохраняет только digest и монотонный момент начала; SQL и значения отбрасываются.
type componentQueryTrace struct {
	digest  [sha256.Size]byte
	started time.Time
}

// componentSQLFunctionFrame accepts only PostgreSQL function signatures and numeric lines, excluding statements and values.
// componentSQLFunctionFrame принимает только сигнатуры функций PostgreSQL и номера строк, исключая statements и значения.
var componentSQLFunctionFrame = regexp.MustCompile(`(?m)^PL/pgSQL function ([a-z_][a-z_0-9.]*\([a-z_0-9., ()\[\]]*\)) line ([0-9]+) at [A-Za-z ]+$`)

// TraceQueryStart hashes the supplied SQL without retaining its text or parameters and preserves the caller's cancellation.
// TraceQueryStart хеширует SQL без сохранения текста либо parameters и сохраняет отмену caller context.
func (tracer componentQueryRejectionTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, componentQueryTraceKey{}, componentQueryTrace{digest: sha256.Sum256([]byte(data.SQL)), started: time.Now()})
}

// TraceQueryEnd records bounded metadata for PostgreSQL rejections; raw Message, Detail, Where, SQL and arguments never reach logs.
// TraceQueryEnd записывает bounded metadata отказов PostgreSQL; сырые Message, Detail, Where, SQL и аргументы не попадают в лог.
func (tracer componentQueryRejectionTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, result pgx.TraceQueryEndData) {
	trace, _ := ctx.Value(componentQueryTraceKey{}).(componentQueryTrace)
	elapsed := time.Since(trace.started)
	var failure *pgconn.PgError
	if !errors.As(result.Err, &failure) {
		if result.Err != nil || elapsed >= time.Second {
			tracer.t.Logf("component SQL timing: query_sha256=%x elapsed_ms=%d failed=%t deadline=%t canceled=%t", trace.digest, elapsed.Milliseconds(), result.Err != nil, errors.Is(result.Err, context.DeadlineExceeded), errors.Is(result.Err, context.Canceled))
		}
		return
	}
	frames := make([]string, 0, 8)
	for _, frame := range componentSQLFunctionFrame.FindAllStringSubmatch(failure.Where, 8) {
		frames = append(frames, frame[1]+":"+frame[2])
	}
	tracer.t.Logf("component SQL rejection: code=%s constraint=%s query_sha256=%x elapsed_ms=%d frames=%v", failure.Code, failure.ConstraintName, trace.digest, elapsed.Milliseconds(), frames)
}
