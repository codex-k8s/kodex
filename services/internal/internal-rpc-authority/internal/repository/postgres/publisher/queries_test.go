package publisher

import (
	"strings"
	"testing"
)

func TestPublisherMutationGateUsesDurableRestoreFence(t *testing.T) {
	if strings.Count(
		readinessSQL,
		"internal_rpc_authority.runtime_restore_fence_allows_work()",
	) != 1 {
		t.Fatal("publisher mutation gate lacks the canonical restore fence")
	}
}

func TestPublisherSnapshotQueriesUseNarrowOwnerFunctions(t *testing.T) {
	for name, query := range map[string]string{
		"append":  appendSnapshotSQL,
		"promote": promoteSnapshotSQL,
	} {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(query, "internal_rpc_authority.publisher_") {
				t.Fatal("publisher snapshot query bypasses the owner function")
			}
			for _, forbidden := range []string{
				"INSERT INTO internal_rpc_authority.authority_snapshot_history",
				"UPDATE internal_rpc_authority.authority_rotation_intents",
			} {
				if strings.Contains(query, forbidden) {
					t.Fatalf("publisher snapshot query contains direct DML %q", forbidden)
				}
			}
		})
	}
}
