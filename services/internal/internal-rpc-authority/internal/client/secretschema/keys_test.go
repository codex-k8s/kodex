package secretschema

import (
	"slices"
	"testing"
)

// TestSecretSchemasPreserveKeysAndIndependentStorage pins all four ordered
// application schemas and mutates one returned slice to prove later and parallel
// copies retain their names. Fixtures are key names only, without secret values,
// filesystem reads, Kubernetes clients or changes to material generations.
// TestSecretSchemasPreserveKeysAndIndependentStorage закрепляет четыре упорядоченные
// прикладные схемы и меняет один возвращённый срез, проверяя сохранение имён в
// последующих и параллельных копиях. Эталоны содержат лишь имена полей без значений
// секретов, чтения файлов, Kubernetes clients или изменения поколений материала.
func TestSecretSchemasPreserveKeysAndIndependentStorage(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name string
		keys func() []string
		want []string
	}{
		{"readback credential", ReadbackCredentialKeys, []string{
			"pinned_intent_id", "readback_credential_compact_jws",
			"readback_credential_jti", "readback_credential_digest_sha256",
			"intent_digest_sha256", "expires_at",
		}},
		{"readback possession", ReadbackPossessionKeys, []string{
			"possession_private_jwk", "possession_key_kid",
			"possession_key_generation", "possession_key_thumbprint_sha256",
		}},
		{"restore credential", RestoreCredentialKeys, []string{
			"semantic_digest_sha256", "issuance_directive_jti",
			"role_credential_compact_jws", "role_credential_digest_sha256",
			"delivery_receipt_jti", "issued_at",
		}},
		{"restore ACK", RestoreACKKeys, []string{
			"semantic_digest_sha256", "issuance_directive_jti", "ack_private_jwk",
			"ack_key_kid", "ack_key_thumbprint_sha256", "role_credential_jti",
			"delivery_receipt_jti", "issued_at",
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			first, second := testCase.keys(), testCase.keys()
			if !slices.Equal(first, testCase.want) || !slices.Equal(second, testCase.want) {
				t.Fatal("application key names or order changed")
			}
			first[0] = "changed by caller"
			if !slices.Equal(second, testCase.want) || !slices.Equal(testCase.keys(), testCase.want) {
				t.Fatal("application schemas share mutable storage")
			}
		})
	}
}
