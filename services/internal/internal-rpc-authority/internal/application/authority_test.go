package application

import (
	"context"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/service"
)

func TestRestoreBlockAtomicallyClosesAdmissionAndWaitsForInflight(t *testing.T) {
	t.Parallel()
	authority := &Authority{}
	authority.domain.Store(&service.Authority{})
	authority.SetAvailable(true)

	_, done, err := authority.begin()
	if err != nil {
		t.Fatalf("begin admitted operation: %v", err)
	}
	authority.SetRestoreBlocked(true)
	if authority.available.Load() {
		t.Fatal("restore block did not close admission")
	}
	if _, _, err := authority.begin(); err == nil {
		t.Fatal("operation was admitted after restore block")
	}
	authority.SetAvailable(true)
	if authority.available.Load() {
		t.Fatal("generic readiness reopened a restore-blocked authority")
	}

	drainDone := make(chan error, 1)
	go func() {
		drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		drainDone <- authority.WaitDrained(drainCtx)
	}()
	select {
	case err := <-drainDone:
		t.Fatalf("drain completed before inflight operation: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	done()
	if err := <-drainDone; err != nil {
		t.Fatalf("wait for inflight drain: %v", err)
	}

	authority.SetRestoreBlocked(false)
	authority.SetAvailable(true)
	if !authority.available.Load() {
		t.Fatal("authority did not reopen after restore fence release")
	}
}
