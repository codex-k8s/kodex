package readback

import (
	"errors"
	"reflect"
	"testing"
	"time"

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

// TestReadbackActivationArgsPreserveAllPins pins all eleven activation values,
// including the nine readiness fields and the activation-only predecessor/time.
// It also checks independent result maps; no trust activation or SQL runs.
// TestReadbackActivationArgsPreserveAllPins закрепляет одиннадцать значений активации,
// включая девять полей readiness и отдельные предшественник и время активации.
// Проверяется и независимость карт; активация доверия и SQL не выполняются.
func TestReadbackActivationArgsPreserveAllPins(t *testing.T) {
	t.Parallel()
	observed := time.Unix(123456789, 0).UTC()
	state := model.ReadbackTrustState{
		RootID: "root", RootFingerprintSHA256: "root-digest",
		ManifestBundleRevision: 2, ManifestBundleDigestSHA256: "bundle-digest",
		TrustSourceRevision: 3, TrustSetDigestSHA256: "set-digest",
		TrustKeySetRevision: 4, SignerGeneration: 5,
		PredecessorStateDigestSHA256: "predecessor-digest",
		ServedStateDigestSHA256:      "served-digest", ServedAt: observed,
	}
	want := pgx.StrictNamedArgs{
		"root_id": "root", "root_fingerprint_sha256": "root-digest",
		"manifest_bundle_revision": uint64(2), "manifest_bundle_digest_sha256": "bundle-digest",
		"trust_source_revision": uint64(3), "trust_set_digest_sha256": "set-digest",
		"trust_key_set_revision": uint64(4), "signer_generation": uint64(5),
		"predecessor_state_digest_sha256": "predecessor-digest",
		"served_state_digest_sha256":      "served-digest", "served_at": observed,
	}
	got := readbackTrustArgs(state)
	if !reflect.DeepEqual(got, want) {
		t.Fatal("readback activation pins changed")
	}
	got["root_id"] = "changed"
	if !reflect.DeepEqual(readbackTrustArgs(state), want) {
		t.Fatal("readback activation argument maps share mutable storage")
	}
}
