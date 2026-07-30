package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/observability"
	"github.com/codex-k8s/matter-codex/libs/go/serviceruntime"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/repository"
)

type cleanerStub struct {
	cancel       context.CancelFunc
	deleteBefore time.Time
	kind         repository.ReservationKind
	err          error
}

func (stub *cleanerStub) DeleteExpired(
	_ context.Context,
	kind repository.ReservationKind,
	deleteBefore time.Time,
) error {
	stub.kind = kind
	stub.deleteBefore = deleteBefore
	stub.cancel()
	return stub.err
}

func TestReplayCleanupClosesReadinessOnFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cleaner := &cleanerStub{
		cancel: cancel,
		err:    errors.New("persistence unavailable"),
	}
	readiness := serviceruntime.NewReadiness()
	readiness.Set(true, "ready")
	metrics := observability.NewMetrics("test_authority", "test", nil)
	retention := 10 * time.Minute
	started := time.Now().UTC()

	runReplayCleanup(
		ctx,
		Config{
			Mode:                       ModeVerifier,
			ReplayCleanupInterval:      time.Hour,
			ReplayRetentionAfterExpiry: retention,
			ReadinessTimeout:           time.Second,
		},
		cleaner,
		readiness,
		metrics,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	ready, reason := readiness.Ready()
	if ready || reason != "replay-cleanup-failed" {
		t.Fatalf("readiness = %v, %q", ready, reason)
	}
	if cleaner.kind != repository.ReservationAuthorizationContext {
		t.Fatalf("cleanup kind = %q", cleaner.kind)
	}
	minimum := started.Add(-retention)
	maximum := time.Now().UTC().Add(-retention)
	if cleaner.deleteBefore.Before(minimum) || cleaner.deleteBefore.After(maximum) {
		t.Fatalf("delete before = %s, want [%s, %s]", cleaner.deleteBefore, minimum, maximum)
	}
}
