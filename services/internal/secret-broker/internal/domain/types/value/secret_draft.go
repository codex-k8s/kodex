package value

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/runtimesecret"
)

// MaximumDraftValueBytes reserves 28 bytes for the configured AEAD nonce/tag within a one-MiB data budget;
// it is not the full Kubernetes object or RPC envelope size limit.
// MaximumDraftValueBytes резервирует 28 байт configured AEAD nonce/tag в one-MiB data budget;
// это не предел полного Kubernetes object или RPC envelope.
const MaximumDraftValueBytes = (1 << 20) - 28

// ErrDraftBindingInvalid is the fixed binding/associated-data failure without private input details.
// ErrDraftBindingInvalid — фиксированная binding/associated-data ошибка без приватных input details.
var ErrDraftBindingInvalid = errors.New("secret draft binding is invalid")

// SecretDraftBinding is derived from verified owner intent and binds scope, resource, generation,
// value type and content hash for AEAD. Validate checks shape, not owner provenance or plaintext bytes;
// validate/publish of the same immutable content must preserve this binding.
// SecretDraftBinding выводится из проверенного owner intent и связывает scope, resource, generation,
// value type и content hash для AEAD. Validate проверяет форму, не owner provenance или plaintext bytes;
// validate/publish того же immutable content должны сохранять эту binding.
type SecretDraftBinding struct {
	ScopeKind       runtimesecret.ScopeKind `json:"scope_kind"`
	OrganizationRef string                  `json:"organization_ref"`
	ProjectRef      string                  `json:"project_ref"`
	SecretRef       string                  `json:"secret_ref"`
	DraftRef        string                  `json:"draft_ref"`
	DraftGeneration int64                   `json:"draft_generation"`
	ValueType       string                  `json:"value_type"`
	ContentSHA256   string                  `json:"content_sha256"`
}

// Validate checks scope, bounded printable-ASCII resource refs, positive generation, lowercase hex
// SHA-256 and the STRING/BINARY/JSON type set. It neither computes that digest nor checks a live grant.
// Validate проверяет scope, ограниченные printable-ASCII resource refs, положительную generation,
// lowercase hex SHA-256 и STRING/BINARY/JSON. Digest не вычисляется, live grant не проверяется.
func (binding SecretDraftBinding) Validate() error {
	if runtimesecret.ValidateScope(binding.ScopeKind, binding.OrganizationRef, binding.ProjectRef) != nil {
		return ErrDraftBindingInvalid
	}
	for _, ref := range []string{binding.SecretRef, binding.DraftRef} {
		if len(ref) < 1 || len(ref) > 128 || strings.TrimSpace(ref) != ref {
			return ErrDraftBindingInvalid
		}
		for _, character := range ref {
			if character < 0x21 || character > 0x7e {
				return ErrDraftBindingInvalid
			}
		}
	}
	if binding.DraftGeneration < 1 || len(binding.ContentSHA256) != 64 ||
		strings.ToLower(binding.ContentSHA256) != binding.ContentSHA256 {
		return ErrDraftBindingInvalid
	}
	if _, err := hex.DecodeString(binding.ContentSHA256); err != nil {
		return ErrDraftBindingInvalid
	}
	switch binding.ValueType {
	case "STRING", "BINARY", "JSON":
		return nil
	default:
		return ErrDraftBindingInvalid
	}
}

// AssociatedData validates the binding and returns its fixed-field JSON behind the AEAD-v2 domain prefix.
// The bytes are authenticated context, not ciphertext or a universal canonical-JSON/authority proof.
// AssociatedData проверяет binding и возвращает fixed-field JSON после AEAD-v2 domain prefix.
// Это аутентифицируемый контекст, не ciphertext и не универсальный canonical-JSON/authority proof.
func (binding SecretDraftBinding) AssociatedData() ([]byte, error) {
	if err := binding.Validate(); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(binding)
	if err != nil {
		return nil, ErrDraftBindingInvalid
	}
	return append([]byte("kodex.secret-draft.aead.v2\x00"), encoded...), nil
}

// DraftEncryptionKey identifies a versioned key without carrying its secret material or validating itself.
// DraftEncryptionKey обозначает versioned key без secret material и самостоятельной проверки.
type DraftEncryptionKey struct {
	ID         string
	Generation int64
}

// EncryptedSecretDraft pairs key identity with ciphertext bytes; the mutable slice is not copied or
// cleared by this plain data type, so its lifetime remains the caller's responsibility.
// EncryptedSecretDraft связывает key identity с ciphertext bytes; изменяемый slice не копируется
// и не обнуляется этим типом данных, поэтому его lifetime остаётся ответственностью caller.
type EncryptedSecretDraft struct {
	Key        DraftEncryptionKey
	Ciphertext []byte
}

// DraftKeyManifest carries key identities, the chosen write key, revision and digest without key material.
// Guard/crypto adapters validate its canonical membership and digest; public fields alone prove nothing.
// DraftKeyManifest переносит key identities, выбранный write key, revision и digest без key material.
// Guard/crypto adapters проверяют canonical membership и digest; одних публичных полей недостаточно.
type DraftKeyManifest struct {
	Revision int64                `json:"revision"`
	Current  DraftEncryptionKey   `json:"current"`
	Keys     []DraftEncryptionKey `json:"keys"`
	Digest   string               `json:"digest"`
}
