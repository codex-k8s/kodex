package postgres

import (
	"strings"
	"testing"
)

func TestClaimQueryKeepsBoundedFencedLifecycle(t *testing.T) {
	for _, fragment := range []string{
		"FOR UPDATE OF artifact SKIP LOCKED",
		"LIMIT @batch_size",
		"content.object_version",
		"retention_claim_generation + 1",
		"@lease_seconds * interval '1 second'",
		"active_run.state IN ('QUEUED', 'RUNNING', 'WAITING_HUMAN', 'CANCELLING')",
		"input_item.artifact_revision = artifact.revision",
		"source_turn.created_at < active_run.created_at",
		"artifact.deleted_at > active_run.created_at",
		"runtime_revision.safe_snapshot -> 'artifacts'",
		"runtime_revision.root_run_id = active_run.id",
		"exact.item -> 'revision' = to_jsonb(artifact.revision)",
	} {
		if !strings.Contains(queryClaimDue, fragment) {
			t.Fatalf("claim query lacks %q", fragment)
		}
	}
}

func TestFinalizationRequiresOwnerAndGenerationFence(t *testing.T) {
	for _, query := range []string{queryLockClaim, queryFinalizeTombstone} {
		for _, fragment := range []string{"retention_claim_owner = @claim_owner", "retention_claim_generation = @claim_generation"} {
			if !strings.Contains(query, fragment) {
				t.Fatalf("finalization query lacks %q", fragment)
			}
		}
	}
}

func TestFinalizationKeepsPurgedTombstoneNameUnique(t *testing.T) {
	if !strings.Contains(queryFinalizeTombstone, "file_name = 'purged-' || ref") {
		t.Fatal("artifact retention finalization reuses a shared tombstone file name")
	}
}
