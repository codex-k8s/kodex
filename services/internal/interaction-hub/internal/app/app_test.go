package app

import (
	"context"
	"testing"

	interactionservice "github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/service"
	interactionstub "github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/repository/stub/interaction"
)

func TestReadinessChecksRequireComposedService(t *testing.T) {
	t.Parallel()

	checks := readinessChecks(interactionservice.New(interactionstub.NewRepository()), fakePingStore{}, nil)
	if len(checks) != 2 {
		t.Fatalf("len(checks) = %d, want 2", len(checks))
	}
	for i, check := range checks {
		if err := check.Check(context.Background()); err != nil {
			t.Fatalf("readiness check %d: %v", i, err)
		}
	}

	checks = readinessChecks(nil, fakePingStore{}, nil)
	if err := checks[0].Check(context.Background()); err == nil {
		t.Fatal("nil service readiness succeeded, want failure")
	}
}

type fakePingStore struct{}

func (fakePingStore) Ping(context.Context) error {
	return nil
}
