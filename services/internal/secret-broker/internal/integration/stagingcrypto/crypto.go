// Package stagingcrypto изолирует стандартное AEAD от owner lifecycle.
package stagingcrypto

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

const ciphertextOverhead = 28

var (
	ErrEncryptionUnavailable = errors.New("secret draft encryption is unavailable")
	ErrEncryptedDraftInvalid = errors.New("encrypted secret draft is invalid")
)

// Keys возвращает отдельную копию материала; adapter очищает её после операции.
// ReserveEncryption устойчиво ограждает rollback/retire и расходует лимит ключа
// до Seal. Локальный счётчик не заменяет этот контракт.
type Keys interface {
	Current(context.Context) (value.DraftEncryptionKey, []byte, error)
	Resolve(context.Context, value.DraftEncryptionKey) ([]byte, error)
	ReserveEncryption(context.Context, value.DraftEncryptionKey) error
}

type Cipher struct{ keys Keys }

func New(keys Keys) (*Cipher, error) {
	if keys == nil {
		return nil, ErrEncryptionUnavailable
	}
	return &Cipher{keys: keys}, nil
}

func (crypt *Cipher) Encrypt(ctx context.Context, binding value.SecretDraftBinding, plaintext []byte) (value.EncryptedSecretDraft, error) {
	if err := ctx.Err(); err != nil {
		return value.EncryptedSecretDraft{}, err
	}
	aad, err := binding.AssociatedData()
	if err != nil || !contentMatches(binding, plaintext) {
		return value.EncryptedSecretDraft{}, ErrEncryptedDraftInvalid
	}
	identity, material, err := crypt.keys.Current(ctx)
	defer clear(material)
	if err != nil || !validKeyIdentity(identity) {
		return value.EncryptedSecretDraft{}, ErrEncryptionUnavailable
	}
	aead, err := newAEAD(material)
	if err != nil {
		return value.EncryptedSecretDraft{}, err
	}
	if err := crypt.keys.ReserveEncryption(ctx, identity); err != nil {
		return value.EncryptedSecretDraft{}, ErrEncryptionUnavailable
	}
	if err := ctx.Err(); err != nil {
		return value.EncryptedSecretDraft{}, err
	}
	return value.EncryptedSecretDraft{Key: identity, Ciphertext: aead.Seal(nil, nil, plaintext, keyAssociatedData(aad, identity))}, nil
}

func (crypt *Cipher) Decrypt(ctx context.Context, binding value.SecretDraftBinding, encrypted value.EncryptedSecretDraft) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	aad, err := binding.AssociatedData()
	if err != nil || !validKeyIdentity(encrypted.Key) || len(encrypted.Ciphertext) <= ciphertextOverhead ||
		len(encrypted.Ciphertext) > value.MaximumDraftValueBytes+ciphertextOverhead {
		return nil, ErrEncryptedDraftInvalid
	}
	material, err := crypt.keys.Resolve(ctx, encrypted.Key)
	defer clear(material)
	if err != nil {
		return nil, ErrEncryptionUnavailable
	}
	aead, err := newAEAD(material)
	if err != nil {
		return nil, err
	}
	// При неуспешном Open очищается вся выделенная область, включая частичный output.
	buffer := make([]byte, len(encrypted.Ciphertext)-ciphertextOverhead)
	plaintext, err := aead.Open(buffer[:0], nil, encrypted.Ciphertext, keyAssociatedData(aad, encrypted.Key))
	if err != nil || !contentMatches(binding, plaintext) {
		clear(buffer)
		return nil, ErrEncryptedDraftInvalid
	}
	if err := ctx.Err(); err != nil {
		clear(buffer)
		return nil, err
	}
	return plaintext, nil
}

func newAEAD(material []byte) (cipher.AEAD, error) {
	if len(material) != 32 {
		return nil, ErrEncryptionUnavailable
	}
	block, err := aes.NewCipher(material)
	if err != nil {
		return nil, ErrEncryptionUnavailable
	}
	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return nil, ErrEncryptionUnavailable
	}
	return aead, nil
}

func keyAssociatedData(binding []byte, identity value.DraftEncryptionKey) []byte {
	result := append(binding, 0)
	result = append(result, identity.ID...)
	return binary.BigEndian.AppendUint64(result, uint64(identity.Generation))
}

func validKeyIdentity(identity value.DraftEncryptionKey) bool {
	if identity.Generation < 1 || len(identity.ID) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(identity.ID)
	return err == nil && hex.EncodeToString(decoded) == identity.ID
}

func contentMatches(binding value.SecretDraftBinding, plaintext []byte) bool {
	if len(plaintext) < 1 || len(plaintext) > value.MaximumDraftValueBytes {
		return false
	}
	wanted, err := hex.DecodeString(binding.ContentSHA256)
	if err != nil || len(wanted) != sha256.Size {
		return false
	}
	actual := sha256.Sum256(plaintext)
	return subtle.ConstantTimeCompare(wanted, actual[:]) == 1
}
