package authority

import (
	"strings"
	"testing"
)

func TestIssuanceVerificationAndReadinessUseDurableRestoreFence(t *testing.T) {
	queries, err := loadQueries()
	if err != nil {
		t.Fatalf("load queries: %v", err)
	}
	for name, query := range map[string]string{
		"proof reservation":   queries.proofReserve,
		"context reservation": queries.contextReserve,
		"snapshot activation": queries.verifierActivateSnapshot,
		"context acceptance":  queries.verifierAcceptContext,
		"verifier readiness":  queries.verifierReadiness,
	} {
		t.Run(name, func(t *testing.T) {
			if strings.Count(
				query,
				"internal_rpc_authority.runtime_restore_fence_allows_work()",
			) != 1 {
				t.Fatal("query does not use the canonical restore fence exactly once")
			}
		})
	}
}

func TestSnapshotQueriesUseSignedHistoryAnchor(t *testing.T) {
	queries, err := loadQueries()
	if err != nil {
		t.Fatalf("load queries: %v", err)
	}
	for name, query := range map[string]string{
		"snapshot activation": queries.verifierActivateSnapshot,
		"context acceptance":  queries.verifierAcceptContext,
	} {
		t.Run(name, func(t *testing.T) {
			for _, fragment := range []string{
				"@history_revisions::bigint[]",
				"@history_digests::text[]",
				"current.source_revision < @source_revision",
			} {
				if !strings.Contains(query, fragment) {
					t.Fatalf("query lacks signed history fragment %q", fragment)
				}
			}
		})
	}
}
