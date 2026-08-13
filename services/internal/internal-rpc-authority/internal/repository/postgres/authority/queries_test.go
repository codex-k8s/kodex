package authority

import (
	"context"
	"strings"
	"testing"
	"time"

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

func TestReadinessReservationUsesModeSpecificWritePath(t *testing.T) {
	now := time.Date(2026, time.August, 13, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		kind repository.ReservationKind
		want string
	}{
		{name: "issuer", kind: repository.ReservationAuthorityProof, want: proofReserveSQL},
		{name: "verifier", kind: repository.ReservationAuthorizationContext, want: contextReserveSQL},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := Store{
				targetWorkloadID:         "workload",
				readinessReservationKind: test.kind,
				queries:                  mustLoadQueriesForTest(t),
			}
			query, args, err := store.readinessReservation(
				"00000000-0000-4000-8000-000000000001",
				now,
			)
			if err != nil {
				t.Fatalf("build readiness reservation: %v", err)
			}
			if query != test.want {
				t.Fatal("readiness selected a write path for another capability")
			}
			if _, _, err := args.RewriteQuery(context.Background(), nil, query, nil); err != nil {
				t.Fatalf("readiness reservation arguments are not exact: %v", err)
			}
		})
	}
}

func mustLoadQueriesForTest(t *testing.T) querySet {
	t.Helper()
	queries, err := loadQueries()
	if err != nil {
		t.Fatalf("load authority queries: %v", err)
	}
	return queries
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
