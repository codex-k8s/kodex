package readback

import (
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// TestReadbackTrustReadinessArgsMatchQuery checks the readiness argument map has exactly the expected nine trust-binding keys. Despite its name it does not rewrite SQL or query the database.
// TestReadbackTrustReadinessArgsMatchQuery проверяет ровно девять ожидаемых ключей привязки доверия в карте аргументов готовности. Несмотря на имя, SQL не переписывается и БД не опрашивается.
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

// TestSerializationFailureClassificationIsExact accepts a wrapped typed 40001 error and rejects 23505 and plain errors. It does not create a serialization conflict or execute a retry transaction.
// TestSerializationFailureClassificationIsExact принимает обёрнутую типизированную ошибку 40001 и отклоняет 23505 и обычную ошибку. Конфликт сериализации не создаётся и повтор транзакции не выполняется.
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

// TestReadbackConsumeUsesReadCommittedIsolation checks the transaction-options value is READ COMMITTED locally; it does not begin a transaction or prove concurrent challenge-consumption semantics.
// TestReadbackConsumeUsesReadCommittedIsolation локально проверяет значение READ COMMITTED в параметрах транзакции; транзакция не начинается и семантика конкурентного потребления challenge не доказывается.
func TestReadbackConsumeUsesReadCommittedIsolation(t *testing.T) {
	t.Parallel()

	options := readbackConsumeTransactionOptions()
	if options.IsoLevel != pgx.ReadCommitted {
		t.Fatalf("unexpected readback consume isolation level: %q", options.IsoLevel)
	}
}
