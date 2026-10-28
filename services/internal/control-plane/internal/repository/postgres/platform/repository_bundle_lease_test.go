package platform

import (
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestRepositoryBundleLeaseEpochAndExpiry proves the work tuple cannot be widened by caller timestamps, stale claims or terminal state; clock boundary is exact and UNKNOWN stays nonterminal without granting another push.
// TestRepositoryBundleLeaseEpochAndExpiry доказывает запрет расширения work tuple caller timestamp, stale claim либо terminal state; clock boundary точна, UNKNOWN не terminal и не выдаёт ещё один push.
func TestRepositoryBundleLeaseEpochAndExpiry(t *testing.T) {
	now := time.Unix(1700000000, 0)
	lease := entity.RepositoryBundleLease{ProposalRef: "rbp_fixture000", Claimant: "gateway-fixture", Fence: "exact-fence", Attempt: 2, Generation: 7, ExpiresAt: now.Add(time.Minute)}
	row := repositoryBundleRow{proposal: entity.RepositoryBundlePublication{Ref: lease.ProposalRef, State: "CLAIMED"}, lease: lease}
	if !repositoryBundleLeaseMatches(row, lease, now) {
		t.Fatal("positive live lease rejected")
	}
	for _, name := range []string{"proposal", "claimant", "fence", "attempt", "generation", "empty expiry", "extended expiry", "exact expiry", "past expiry", "terminal"} {
		t.Run(name, func(t *testing.T) {
			input, stored, current := lease, row, now
			switch name {
			case "proposal":
				input.ProposalRef = "rbp_other000"
			case "claimant":
				input.Claimant = "other"
			case "fence":
				input.Fence = "other"
			case "attempt":
				input.Attempt--
			case "generation":
				input.Generation--
			case "empty expiry":
				input.ExpiresAt = time.Time{}
			case "extended expiry":
				input.ExpiresAt = input.ExpiresAt.Add(time.Second)
			case "exact expiry":
				current = lease.ExpiresAt
			case "past expiry":
				current = lease.ExpiresAt.Add(time.Nanosecond)
			case "terminal":
				stored.proposal.State = "CANCELLED"
			}
			if repositoryBundleLeaseMatches(stored, input, current) {
				t.Fatal("invalid work authority accepted")
			}
		})
	}
	if repositoryBundleTerminal("UNKNOWN_OUTCOME") {
		t.Fatal("uncertainty became terminal and released retention")
	}
}
