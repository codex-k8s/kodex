package postgres

import (
	"strings"
	"testing"
)

// TestClaimQueryKeepsBoundedFencedLifecycle pins skip-locked batching, exact receipts and generation leases.
// RU: TestClaimQueryKeepsBoundedFencedLifecycle закрепляет skip-locked пачки, точные receipts и аренды поколений.
func TestClaimQueryKeepsBoundedFencedLifecycle(t *testing.T) {
	for _, fragment := range []string{
		"FOR UPDATE OF artifact SKIP LOCKED",
		"LIMIT @batch_size",
		"content.object_version",
		"retention_claim_generation + 1",
		"@lease_seconds * interval '1 second'",
		"artifact_has_retained_revisions(artifact.id)",
		"control_plane.artifact_heads",
		"content.revision_id=revision.id",
		"ORDER BY revision.revision, revision.id LIMIT 1",
	} {
		if !strings.Contains(queryClaimDue, fragment) {
			t.Fatalf("claim query lacks %q", fragment)
		}
	}
}

// TestFinalizationRequiresOwnerAndGenerationFence checks both locking and tombstone SQL conditions.
// RU: TestFinalizationRequiresOwnerAndGenerationFence проверяет условия блокировки и tombstone SQL.
func TestFinalizationRequiresOwnerAndGenerationFence(t *testing.T) {
	for _, query := range []string{queryLockClaim, queryFinalizeTombstone} {
		for _, fragment := range []string{"retention_claim_owner = @claim_owner", "retention_claim_generation = @claim_generation"} {
			if !strings.Contains(query, fragment) {
				t.Fatalf("finalization query lacks %q", fragment)
			}
		}
	}
}

// TestFinalizationRequiresAllVersionReceiptsCleared forbids terminal purge with a live revision receipt.
// RU: TestFinalizationRequiresAllVersionReceiptsCleared запрещает конечную очистку при живом receipt версии.
func TestFinalizationRequiresAllVersionReceiptsCleared(t *testing.T) {
	if !strings.Contains(queryFinalizeTombstone, "current_revision_id = NULL") ||
		!strings.Contains(queryFinalizeTombstone, "NOT EXISTS") ||
		!strings.Contains(queryFinalizeTombstone, "artifact_revision_content") {
		t.Fatal("artifact retention finalization keeps a current pointer or ignores historical receipts")
	}
}
