package readback

import (
	"errors"
	"testing"

	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestReadbackTrustReadinessArgsMatchQuery(t *testing.T) {
	args := readbackTrustReadinessArgs(model.ReadbackTrustState{})
	expected := []string{
		"root_id",
		"root_fingerprint_sha256",
		"manifest_bundle_revision",
		"manifest_bundle_digest_sha256",
		"trust_source_revision",
		"trust_set_digest_sha256",
		"trust_key_set_revision",
		"signer_generation",
		"served_state_digest_sha256",
	}
	if len(args) != len(expected) {
		t.Fatalf("unexpected readback readiness argument count: got %d, want %d", len(args), len(expected))
	}
	for _, name := range expected {
		if _, ok := args[name]; !ok {
			t.Fatalf("readback readiness argument %q is missing", name)
		}
	}
}

func TestSerializationFailureClassificationIsExact(t *testing.T) {
	serializationFailure := &pgconn.PgError{Code: "40001"}
	if !isSerializationFailure(errors.Join(errors.New("consume failed"), serializationFailure)) {
		t.Fatal("wrapped PostgreSQL serialization failure was not classified for retry")
	}
	if isSerializationFailure(&pgconn.PgError{Code: "23505"}) {
		t.Fatal("non-serialization PostgreSQL failure was classified for retry")
	}
	if isSerializationFailure(errors.New("temporary failure")) {
		t.Fatal("untyped failure was classified for retry")
	}
}

func TestReadbackConsumeUsesReadCommittedIsolation(t *testing.T) {
	t.Parallel()

	options := readbackConsumeTransactionOptions()
	if options.IsoLevel != pgx.ReadCommitted {
		t.Fatalf("unexpected readback consume isolation level: %q", options.IsoLevel)
	}
}
