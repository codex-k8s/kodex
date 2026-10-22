package readbackrow

import (
	"testing"

	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
)

// TestDestinationsPreservesColumnPointerIdentity pins the eighteen exact
// destination addresses and their order, including the separate JWK byte slot.
// It also checks fresh slice storage without performing a row scan, validating
// a key or opening a database connection.
// TestDestinationsPreservesColumnPointerIdentity закрепляет восемнадцать точных
// адресов результата и их порядок, включая отдельный слот байтов JWK. Проверяется
// и новая память среза без Scan строки, проверки ключа и соединения с БД.
func TestDestinationsPreservesColumnPointerIdentity(t *testing.T) {
	t.Parallel()
	var intent model.ReadbackIntent
	var publicJWK []byte
	want := []any{
		&intent.IntentID, &intent.Kind, &intent.IntentRevision,
		&intent.IntentDigestSHA256, &intent.WorkloadID, &intent.WorkloadSPIFFEID,
		&intent.Role, &intent.WorkloadGeneration, &intent.CredentialGeneration,
		&intent.MaterialGeneration, &intent.PossessionKeyID,
		&intent.PossessionKeyGeneration, &publicJWK, &intent.PossessionKeyThumbprint,
		&intent.SourceRevision, &intent.ServedStateDigestSHA256, &intent.Status,
		&intent.ExpiresAt,
	}
	first, second := Destinations(&intent, &publicJWK), Destinations(&intent, &publicJWK)
	if len(first) != len(want) || len(second) != len(want) {
		t.Fatal("intent column count changed")
	}
	for index, pointer := range want {
		if first[index] != pointer || second[index] != pointer {
			t.Fatalf("intent destination %d changed address or column order", index)
		}
	}
	first[0] = nil
	if second[0] != &intent.IntentID || Destinations(&intent, &publicJWK)[0] != &intent.IntentID {
		t.Fatal("intent destination slices share mutable storage")
	}
}
