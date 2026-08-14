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

func TestContextAcceptanceUsesExactSnapshotReceiptArguments(t *testing.T) {
	state := repository.SnapshotState{
		AttestationReceiptID: "00000000-0000-4000-8000-000000000001",
	}
	args := snapshotArgs("control-plane", state)
	for key, value := range contextReservationArgs(repository.Reservation{}) {
		args[key] = value
	}
	if _, _, err := args.RewriteQuery(
		context.Background(),
		nil,
		verifierAcceptContextSQL,
		nil,
	); err != nil {
		t.Fatalf("context acceptance arguments are not exact: %v", err)
	}
	for _, required := range []string{
		"validate_snapshot_attestation_receipt(",
		"@attestation_receipt_id",
		"readback_attestation_receipt_id =\n            EXCLUDED.readback_attestation_receipt_id",
	} {
		if !strings.Contains(verifierAcceptContextSQL, required) {
			t.Fatalf("context acceptance query lost receipt invariant %q", required)
		}
	}
}

func TestSnapshotBootstrapUsesIndependentReceiptAndSignedPredecessor(t *testing.T) {
	queries := map[string]string{
		"activation": verifierActivateSnapshotSQL,
		"context":    verifierAcceptContextSQL,
	}
	for name, query := range queries {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(
				query,
				"FROM internal_rpc_authority.authority_snapshot_watermarks AS initial",
			) {
				t.Fatal("first attested snapshot still requires a pre-existing watermark")
			}
			for _, required := range []string{
				"@source_revision > 1",
				"@predecessor_revision = @source_revision - 1",
				"AS signed_predecessor(revision, digest_sha256)",
				"signed_predecessor.digest_sha256 =\n                                @predecessor_digest_sha256",
			} {
				if !strings.Contains(query, required) {
					t.Fatalf("first non-genesis snapshot lost predecessor invariant %q", required)
				}
			}
		})
	}
}

func TestProofReservationAllowsConcurrentOutOfOrderProofs(t *testing.T) {
	for _, required := range []string{
		"authority_proof_watermarks.proof_revision < EXCLUDED.proof_revision",
		"THEN EXCLUDED.proof_revision",
		"ELSE internal_rpc_authority.authority_proof_watermarks.proof_revision",
		"authority_proof_watermarks.proof_revision <> EXCLUDED.proof_revision",
		"authority_proof_watermarks.canonical_payload_digest_sha256 =\n          EXCLUDED.canonical_payload_digest_sha256",
		"ON CONFLICT (caller_workload_id, jti) DO NOTHING",
	} {
		if !strings.Contains(proofReserveSQL, required) {
			t.Fatalf("proof reservation query lost out-of-order replay invariant %q", required)
		}
	}
	if strings.Contains(
		proofReserveSQL,
		"SET proof_revision = EXCLUDED.proof_revision,",
	) {
		t.Fatal("an older valid proof can still roll back the replay watermark")
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
