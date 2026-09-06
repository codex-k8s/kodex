package platform

import (
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func TestProviderDeletionRejectsPrematureTerminalReadback(t *testing.T) {
	now := time.Now().UTC()
	fixture := entity.ProviderAccountDeletion{Ref: "pdel_testterminal", Version: 2, State: "DELETED",
		SafeReason: "ACCOUNT_DELETED", RequestedAt: now, CompletedAt: &now}
	for _, test := range []struct {
		name   string
		change func(*entity.ProviderAccountDeletion)
	}{
		{"pending cleanup", func(v *entity.ProviderAccountDeletion) { v.PendingCleanup = 1 }},
		{"active lease", func(v *entity.ProviderAccountDeletion) {
			v.Blockers = []entity.ProviderAccountBlockerCount{{Kind: "ACTIVE_TURN", Total: 1}}
		}},
		{"missing completion", func(v *entity.ProviderAccountDeletion) { v.CompletedAt = nil }},
		{"unknown lifecycle", func(v *entity.ProviderAccountDeletion) { v.State = "DONE" }},
		{"mismatched reason", func(v *entity.ProviderAccountDeletion) { v.SafeReason = "CREDENTIAL_CLEANUP_PENDING" }},
		{"unknown blocker", func(v *entity.ProviderAccountDeletion) {
			v.Blockers = []entity.ProviderAccountBlockerCount{{Kind: "UNKNOWN"}}
		}},
		{"duplicate blocker", func(v *entity.ProviderAccountDeletion) {
			v.Blockers = []entity.ProviderAccountBlockerCount{{Kind: "AGENT"}, {Kind: "AGENT"}}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := fixture
			test.change(&value)
			if normalizeProviderAccountBlockerCounts(&value) == nil {
				t.Fatal("unsafe deletion snapshot was accepted")
			}
		})
	}
	if err := normalizeProviderAccountBlockerCounts(&fixture); err != nil || len(fixture.Blockers) != 6 {
		t.Fatalf("complete terminal snapshot rejected: %v", err)
	}
}
