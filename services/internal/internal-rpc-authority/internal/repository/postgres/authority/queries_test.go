package authority

import (
	"context"
	"strings"
	"testing"

	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/repository"
)

func TestSnapshotActivationAllowsFreshReceiptForExactSnapshot(t *testing.T) {
	query := verifierActivateSnapshotSQL
	for _, required := range []string{
		"validate_snapshot_attestation_receipt(",
		"current.source_revision = @source_revision",
		"current.source_digest_sha256 = @source_digest_sha256",
		"authority_snapshot_watermarks.source_revision <= EXCLUDED.source_revision",
		"authority_snapshot_watermarks.source_digest_sha256 = EXCLUDED.source_digest_sha256",
		"readback_attestation_receipt_id =\n            EXCLUDED.readback_attestation_receipt_id",
	} {
		if !strings.Contains(query, required) {
			t.Fatalf("snapshot activation query lost required restart invariant %q", required)
		}
	}
	if strings.Contains(query, "readback_attestation_receipt_id =\n              EXCLUDED.readback_attestation_receipt_id") {
		t.Fatal("exact snapshot restart still requires the previous receipt identifier")
	}
}

func TestSnapshotReadinessUsesExactArgumentsAndSharedWorkloadReceipt(t *testing.T) {
	args := snapshotReadinessArgs("workload", repository.SnapshotState{})
	if _, _, err := args.RewriteQuery(
		context.Background(),
		nil,
		verifierReadinessSQL,
		nil,
	); err != nil {
		t.Fatalf("snapshot readiness query arguments are not exact: %v", err)
	}
	if strings.Contains(verifierReadinessSQL, "@attestation_receipt_id") {
		t.Fatal("replica readiness still requires its local receipt identifier")
	}
	if !strings.Contains(
		verifierReadinessSQL,
		"validate_snapshot_attestation_receipt(\n          readback_attestation_receipt_id,",
	) {
		t.Fatal("snapshot readiness does not validate the current workload receipt")
	}
}
