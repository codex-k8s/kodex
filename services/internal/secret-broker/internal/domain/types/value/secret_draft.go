package value

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

// Kubernetes ограничивает весь data payload одним MiB, включая nonce/tag.
const MaximumDraftValueBytes = (1 << 20) - 28

var ErrDraftBindingInvalid = errors.New("secret draft binding is invalid")

// SecretDraftBinding строится только из проверенного ответа владельца intent.
// Версия содержимого не меняется при validate/publish той же immutable revision.
type SecretDraftBinding struct {
	ProjectRef      string `json:"project_ref"`
	SecretRef       string `json:"secret_ref"`
	DraftRef        string `json:"draft_ref"`
	DraftGeneration int64  `json:"draft_generation"`
	ValueType       string `json:"value_type"`
	ContentSHA256   string `json:"content_sha256"`
}

func (binding SecretDraftBinding) Validate() error {
	for _, ref := range []string{binding.ProjectRef, binding.SecretRef, binding.DraftRef} {
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

// AssociatedData привязывает ciphertext к одному владельцу и содержимому.
func (binding SecretDraftBinding) AssociatedData() ([]byte, error) {
	if err := binding.Validate(); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(binding)
	if err != nil {
		return nil, ErrDraftBindingInvalid
	}
	return append([]byte("kodex.secret-draft.aead.v1\x00"), encoded...), nil
}

type DraftEncryptionKey struct {
	ID         string
	Generation int64
}

type EncryptedSecretDraft struct {
	Key        DraftEncryptionKey
	Ciphertext []byte
}

// DraftKeyManifest не содержит material; его digest канонически связывает
// только проверенные идентичности всех ключей и выбранный write key.
type DraftKeyManifest struct {
	Revision int64                `json:"revision"`
	Current  DraftEncryptionKey   `json:"current"`
	Keys     []DraftEncryptionKey `json:"keys"`
	Digest   string               `json:"digest"`
}
