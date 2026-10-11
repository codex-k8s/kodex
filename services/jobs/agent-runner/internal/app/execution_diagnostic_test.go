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
)

func TestRuntimeExecutionFailureLogKeepsContextSeparateAndRedactsCause(t *testing.T) {
	for _, state := range []string{"ACTIVE", "CANCELED", "DEADLINE_EXCEEDED"} {
		t.Run(state, func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(t.Context())
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
			slog.SetDefault(slog.New(slog.NewJSONHandler(&wire, nil)))
			defer slog.SetDefault(previous)
			logRuntimeExecutionFailure(ctx, errors.Join(secondary, private), true)
			if strings.Contains(wire.String(), "PRIVATE_SENTINEL") || strings.Contains(wire.String(), "private") {
				t.Fatal("execution boundary serialized private error or context cause")
			}
			var record map[string]any
			if json.Unmarshal(wire.Bytes(), &record) != nil || len(record) != 6 || record["msg"] != runtimeExecutionFailureMessage ||
				record["category"] != "UNKNOWN" || record["context_state"] != state || record["activity_failed"] != true {
				t.Fatal("execution boundary log escaped closed attributes")
			}
		})
	}
}
