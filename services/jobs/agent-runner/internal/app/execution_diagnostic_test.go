package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	sharedobservability "github.com/codex-k8s/kodex/libs/go/observability"
	"go.opentelemetry.io/otel/trace"
)

// TestRuntimeExecutionFailureLogKeepsContextSeparateAndRedactsCause exercises active, cancelled and expired contexts with private joined errors, checking closed attributes, preserved trace identity and no mutation of the global logger.
// TestRuntimeExecutionFailureLogKeepsContextSeparateAndRedactsCause проверяет активный, отменённый и просроченный контексты с приватными joined errors, закрытые поля, trace identity и отсутствие изменения глобального logger.
func TestRuntimeExecutionFailureLogKeepsContextSeparateAndRedactsCause(t *testing.T) {
	for _, state := range []string{"ACTIVE", "CANCELED", "DEADLINE_EXCEEDED"} {
		t.Run(state, func(t *testing.T) {
			spanContext := trace.NewSpanContext(trace.SpanContextConfig{
				TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, TraceFlags: trace.FlagsSampled,
			})
			ctx, cancel := context.WithCancelCause(trace.ContextWithSpanContext(t.Context(), spanContext))
			defer cancel(nil)
			private := errors.New("PRIVATE_SENTINEL token=private frame=private")
			secondary := context.Canceled
			switch state {
			case "CANCELED":
				cancel(private)
			case "DEADLINE_EXCEEDED":
				var stop context.CancelFunc
				ctx, stop = context.WithDeadline(ctx, time.Now().Add(-time.Second))
				defer stop()
				secondary = context.DeadlineExceeded
			}
			var wire bytes.Buffer
			previous := slog.Default()
			telemetry := &sharedobservability.Runtime{}
			runtime := turnRuntime{logger: telemetry.Logger(&wire)}
			logRuntimeExecutionFailure(ctx, runtime.logger, errors.Join(secondary, private), true)
			if slog.Default() != previous {
				t.Fatal("execution observation changed the global logger")
			}
			if strings.Contains(wire.String(), "PRIVATE_SENTINEL") || strings.Contains(wire.String(), "private") {
				t.Fatal("execution boundary serialized private error or context cause")
			}
			var record map[string]any
			if json.Unmarshal(wire.Bytes(), &record) != nil || len(record) != 8 || record["msg"] != runtimeExecutionFailureMessage ||
				record["category"] != "UNKNOWN" || record["context_state"] != state || record["activity_failed"] != true {
				t.Fatal("execution boundary log escaped closed attributes")
			}
			if record["trace_id"] != spanContext.TraceID().String() || record["span_id"] != spanContext.SpanID().String() {
				t.Fatal("execution observation lost telemetry trace identity")
			}
		})
	}
}
