package app

import (
	"context"
	"testing"
	"time"

	securityrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/security"
)

type cleanupRepository struct {
	input securityrepo.CapabilityCleanupInput
	calls int
}

func (repository *cleanupRepository) CleanupInteractionCapabilities(_ context.Context, input securityrepo.CapabilityCleanupInput) (int64, error) {
	repository.calls++
	repository.input = input
	return 7, nil
}

func TestCleanupInteractionCapabilitiesUsesRetentionAndBoundedBatch(t *testing.T) {
	now := time.Date(2026, time.July, 17, 12, 0, 0, 0, time.UTC)
	repository := &cleanupRepository{}
	deleted, err := cleanupInteractionCapabilities(context.Background(), repository, now, 48*time.Hour, 250)
	if err != nil || deleted != 7 {
		t.Fatalf("cleanup = %d, %v", deleted, err)
	}
	if repository.calls != 1 || !repository.input.DeleteBefore.Equal(now.Add(-48*time.Hour)) || repository.input.Limit != 250 {
		t.Fatalf("cleanup input = %#v", repository.input)
	}
}

func TestCleanupInteractionCapabilitiesRejectsUnboundedConfiguration(t *testing.T) {
	for _, test := range []struct {
		retention time.Duration
		batch     int
	}{{retention: 0, batch: 1}, {retention: time.Hour, batch: 0}} {
		repository := &cleanupRepository{}
		if _, err := cleanupInteractionCapabilities(context.Background(), repository, time.Now(), test.retention, test.batch); err == nil {
			t.Fatal("cleanupInteractionCapabilities() error = nil")
		}
		if repository.calls != 0 {
			t.Fatalf("invalid cleanup reached repository: calls=%d", repository.calls)
		}
	}
}
