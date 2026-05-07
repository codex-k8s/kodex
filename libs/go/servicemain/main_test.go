package servicemain

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
)

func TestRunReturnsZeroOnSuccess(t *testing.T) {
	t.Parallel()

	code := run(context.Background(), "test-service", discardLogger(), func() (string, error) {
		return "ready", nil
	}, func(_ context.Context, cfg string, _ *slog.Logger) error {
		if cfg != "ready" {
			t.Fatalf("cfg = %q, want ready", cfg)
		}
		return nil
	})
	if code != 0 {
		t.Fatalf("run() code = %d, want 0", code)
	}
}

func TestRunReturnsOneOnConfigError(t *testing.T) {
	t.Parallel()

	code := run(context.Background(), "test-service", discardLogger(), func() (string, error) {
		return "", fmt.Errorf("config failed")
	}, func(context.Context, string, *slog.Logger) error {
		t.Fatal("runService must not be called")
		return nil
	})
	if code != 1 {
		t.Fatalf("run() code = %d, want 1", code)
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRunReturnsOneOnServiceError(t *testing.T) {
	t.Parallel()

	code := run(context.Background(), "test-service", discardLogger(), func() (string, error) {
		return "ready", nil
	}, func(context.Context, string, *slog.Logger) error {
		return fmt.Errorf("service failed")
	})
	if code != 1 {
		t.Fatalf("run() code = %d, want 1", code)
	}
}
