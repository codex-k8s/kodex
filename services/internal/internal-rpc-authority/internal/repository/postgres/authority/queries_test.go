package authority

import (
	"strings"
	"testing"
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
