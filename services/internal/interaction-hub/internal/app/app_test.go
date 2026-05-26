package app

import (
	"context"
	"testing"

	interactionservice "github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/service"
	interactionstub "github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/repository/stub/interaction"
)

func TestReadinessChecksRequireComposedService(t *testing.T) {
	t.Parallel()

	checks := readinessChecks(interactionservice.New(interactionstub.NewRepository()))
	if len(checks) != 1 {
		t.Fatalf("len(checks) = %d, want 1", len(checks))
	}
	if err := checks[0].Check(context.Background()); err != nil {
		t.Fatalf("readiness check: %v", err)
	}

	checks = readinessChecks(nil)
	if err := checks[0].Check(context.Background()); err == nil {
		t.Fatal("nil service readiness succeeded, want failure")
	}
}
