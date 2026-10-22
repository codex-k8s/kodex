// Package stagingcrypto isolates authenticated encryption and private keyring files from owner lifecycle.
// Package stagingcrypto изолирует authenticated encryption и private keyring files от owner lifecycle.
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

// ciphertextOverhead is the 12-byte random nonce plus 16-byte GCM tag added to every nonempty staged
// value.
// ciphertextOverhead — 12-байтовый случайный nonce и 16-байтовый GCM tag, добавляемые к каждому непустому
// staged value.
const ciphertextOverhead = 28

var (
	// ErrEncryptionUnavailable hides key-source, reservation and cipher-construction failures without exposing
	// key material or storage paths.
	// ErrEncryptionUnavailable скрывает key-source, reservation и cipher-construction failures без раскрытия
	// материала ключей и storage paths.
	ErrEncryptionUnavailable = errors.New("secret draft encryption is unavailable")
	// ErrEncryptedDraftInvalid rejects malformed bounds, mismatched binding/content or failed authentication
	// without returning plaintext.
	// ErrEncryptedDraftInvalid отклоняет malformed bounds, несовпавшие binding/content и неуспешную
	// authentication без возврата plaintext.
	ErrEncryptedDraftInvalid = errors.New("encrypted secret draft is invalid")
)

// Keys returns disposable copies of key material and durably reserves encryption use before Seal;
// implementations must prevent rollback/retire and cannot substitute an in-memory counter.
// Keys возвращает отдельные очищаемые копии материала и устойчиво резервирует использование до Seal;
// реализация защищает от rollback/retire и не заменяет это in-memory счётчиком.
type Keys interface {
	// Current returns the current write-key identity and an independent material copy that the cipher clears
	// on return.
	// Current возвращает identity текущего write key и независимую копию материала, которую cipher очищает при
	// return.
	Current(context.Context) (value.DraftEncryptionKey, []byte, error)
	// Resolve returns a disposable copy for the exact retained read-key identity; reading an old key must not
	// select the current write key instead.
	// Resolve возвращает очищаемую копию точного retained read-key identity; чтение старого ключа не должно
	// подставлять текущий write key.
	Resolve(context.Context, value.DraftEncryptionKey) ([]byte, error)
	// ReserveEncryption durably spends a write allowance for the exact current identity before encryption;
	// readiness and decryption do not spend it.
	// ReserveEncryption устойчиво расходует write allowance точной current identity до шифрования; readiness и
	// расшифровка его не расходуют.
	ReserveEncryption(context.Context, value.DraftEncryptionKey) error
}

// Cipher authenticates scoped draft bindings and exact content using AES-256-GCM with random nonces; it
// does not own grants, lifecycle transitions or caller plaintext lifetime.
// Cipher аутентифицирует scoped draft bindings и точное содержимое через AES-256-GCM со случайными nonce;
// grants, lifecycle transitions и lifetime caller plaintext ему не принадлежат.
type Cipher struct{ keys Keys }

// New requires a nonnil key-source interface but does not probe it or detect typed nils; key validity is
// checked by each operation.
// New требует nonnil key-source interface без проверки готовности и typed nil; пригодность ключа
// проверяется при каждой операции.
func New(keys Keys) (*Cipher, error) {
	if keys == nil {
		return nil, ErrEncryptionUnavailable
	}
	return &Cipher{keys: keys}, nil
}

// Encrypt checks cancellation/binding/content, obtains a disposable current key, reserves durable use and
// rechecks cancellation before Seal. It clears only its key copy, not caller plaintext; the returned
// ciphertext is independently allocated.
// Encrypt проверяет cancellation/binding/content, получает отдельную копию current key, устойчиво
// резервирует использование и повторно проверяет cancellation до Seal. Очищает лишь свою копию ключа, не
// caller plaintext; ciphertext выделяется независимо.
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

// Decrypt validates bounds/identity, resolves the exact key and authenticates binding/key AAD plus content
// digest. It clears the material copy and clears the allocated output on failure or late cancellation;
// successful plaintext belongs to the caller.
// Decrypt проверяет bounds/identity, разрешает точный ключ и аутентифицирует binding/key AAD и content
// digest. Очищает копию материала и выделенный output при отказе либо поздней отмене; успешный plaintext
// принадлежит caller.
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

// newAEAD accepts exactly 32 key bytes and constructs standard-library AES-GCM with a prepended random
// nonce: NonceSize is zero and overhead is 28; it does not enforce a durable use budget itself.
// newAEAD принимает ровно 32 байта ключа и создаёт стандартный AES-GCM с добавляемым случайным nonce:
// NonceSize равен нулю, overhead — 28; устойчивый use budget сам не обеспечивает.
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

// keyAssociatedData appends a zero separator, key ID and big-endian uint64 generation to binding AAD;
// append may reuse the supplied slice's capacity, and identity validation belongs to callers.
// keyAssociatedData добавляет к binding AAD нулевой разделитель, key ID и big-endian uint64 generation;
// append может использовать capacity исходного slice, identity проверяет caller.
func keyAssociatedData(binding []byte, identity value.DraftEncryptionKey) []byte {
	result := append(binding, 0)
	result = append(result, identity.ID...)
	return binary.BigEndian.AppendUint64(result, uint64(identity.Generation))
}

// validKeyIdentity requires a positive generation and canonical 32-byte lowercase hexadecimal ID, not
// proof that unseen material hashes to it.
// validKeyIdentity требует положительный generation и canonical 32-byte lowercase hexadecimal ID, не
// доказывая соответствия неизвестному материалу.
func validKeyIdentity(identity value.DraftEncryptionKey) bool {
	if identity.Generation < 1 || len(identity.ID) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(identity.ID)
	return err == nil && hex.EncodeToString(decoded) == identity.ID
}

// contentMatches bounds nonempty plaintext, decodes an exact 32-byte expected digest and compares SHA-256
// in constant time; canonical binding representation is validated elsewhere.
// contentMatches ограничивает непустой plaintext, декодирует точный 32-байтовый expected digest и
// сравнивает SHA-256 в constant time; canonical binding representation проверяется отдельно.
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
