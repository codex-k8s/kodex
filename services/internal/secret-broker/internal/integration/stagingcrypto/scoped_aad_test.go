package stagingcrypto

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimesecret"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

func TestScopedCipherRejectsLegacyV1AndCrossOrganization(t *testing.T) {
	crypt, keys, binding, plaintext := cryptoFixture(t)
	for _, scope := range []string{"PROJECT", "ORGANIZATION"} {
		t.Run(scope, func(t *testing.T) {
			current := binding
			current.ScopeKind = runtimesecret.ScopeKind(scope)
			if scope == "ORGANIZATION" {
				current.ProjectRef = ""
			}
			encrypted, err := crypt.Encrypt(t.Context(), current, plaintext)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := crypt.Decrypt(t.Context(), current, encrypted)
			if err != nil || !bytes.Equal(decoded, plaintext) {
				t.Fatal("scoped V2 round trip failed")
			}
			clear(decoded)
			foreign := current
			foreign.OrganizationRef = "org_foreign"
			if decoded, err := crypt.Decrypt(t.Context(), foreign, encrypted); !errors.Is(err, ErrEncryptedDraftInvalid) || len(decoded) != 0 {
				t.Fatal("cross-organization ciphertext returned plaintext")
			}
		})
	}
	legacy := struct {
		ProjectRef      string `json:"project_ref"`
		SecretRef       string `json:"secret_ref"`
		DraftRef        string `json:"draft_ref"`
		DraftGeneration int64  `json:"draft_generation"`
		ValueType       string `json:"value_type"`
		ContentSHA256   string `json:"content_sha256"`
	}{binding.ProjectRef, binding.SecretRef, binding.DraftRef, binding.DraftGeneration, binding.ValueType, binding.ContentSHA256}
	encoded, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	aead, err := newAEAD(keys.material)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext := aead.Seal(nil, nil, plaintext, keyAssociatedData(append([]byte("kodex.secret-draft.aead.v1\x00"), encoded...), keys.identity))
	if output, err := crypt.Decrypt(t.Context(), binding, value.EncryptedSecretDraft{Key: keys.identity, Ciphertext: ciphertext}); !errors.Is(err, ErrEncryptedDraftInvalid) || len(output) != 0 {
		t.Fatal("legacy V1 ciphertext accepted by V2 decoder")
	}
}
