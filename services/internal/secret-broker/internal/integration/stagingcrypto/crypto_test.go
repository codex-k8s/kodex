package stagingcrypto

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// testKeys supplies disposable synthetic key copies and configurable reservation/cancellation failures;
// its counter is not the durable production guard.
// testKeys предоставляет отдельные synthetic key copies и настраиваемые reservation/cancellation failures;
// счётчик не является устойчивым production guard.
type testKeys struct {
	identity    value.DraftEncryptionKey
	material    []byte
	reserves    int
	denied      bool
	unavailable bool
	cancel      context.CancelFunc
}

// Current returns an independent synthetic material copy or a configured unavailable error without
// checking context or manifest state.
// Current возвращает независимую synthetic material copy либо настроенную unavailable error без проверки
// context и manifest state.
func (keys *testKeys) Current(context.Context) (value.DraftEncryptionKey, []byte, error) {
	if keys.unavailable {
		return value.DraftEncryptionKey{}, nil, errors.New("fixture key source unavailable")
	}
	return keys.identity, bytes.Clone(keys.material), nil
}

// Resolve returns a material copy only for the exact configured identity, otherwise a synthetic
// missing-key error.
// Resolve возвращает material copy лишь точной настроенной identity, иначе synthetic missing-key error.
func (keys *testKeys) Resolve(_ context.Context, identity value.DraftEncryptionKey) ([]byte, error) {
	if keys.unavailable || identity != keys.identity {
		return nil, errors.New("fixture key missing")
	}
	return bytes.Clone(keys.material), nil
}

// ReserveEncryption counts every attempt, can reject it or cancel the caller and otherwise succeeds
// without validating identity or persistence.
// ReserveEncryption учитывает каждую попытку, может отказать либо отменить caller, иначе успешен без
// проверки identity и persistence.
func (keys *testKeys) ReserveEncryption(context.Context, value.DraftEncryptionKey) error {
	keys.reserves++
	if keys.denied {
		return errors.New("fixture reservation denied")
	}
	if keys.cancel != nil {
		keys.cancel()
	}
	return nil
}

// cryptoFixture builds a real Cipher over synthetic key ports and a matching scoped binding/digest; it
// needs no cluster, provider or production key.
// cryptoFixture создаёт реальный Cipher с synthetic key ports и совпадающими scoped binding/digest;
// cluster, provider и production key не нужны.
func cryptoFixture(t *testing.T) (*Cipher, *testKeys, value.SecretDraftBinding, []byte) {
	t.Helper()
	material := bytes.Repeat([]byte{0x63}, 32)
	keyDigest := sha256.Sum256(material)
	keys := &testKeys{identity: value.DraftEncryptionKey{ID: hex.EncodeToString(keyDigest[:]), Generation: 1}, material: material}
	crypt, err := New(keys)
	if err != nil {
		t.Fatal("fixture cipher initialization failed")
	}
	plaintext := []byte("synthetic draft value")
	digest := sha256.Sum256(plaintext)
	binding := value.SecretDraftBinding{ScopeKind: "PROJECT", OrganizationRef: "org_fixture", ProjectRef: "prj_fixture", SecretRef: "sec_fixture", DraftRef: "drf_fixture", DraftGeneration: 1, ValueType: "STRING", ContentSHA256: hex.EncodeToString(digest[:])}
	return crypt, keys, binding, plaintext
}

// TestCipherRoundTripRandomNonceAndNoPlaintextAlias encrypts twice, checks distinct ciphertext and
// overhead/reservation counts, decrypts and confirms clearing output leaves caller plaintext/key
// untouched; it is not a statistical nonce proof.
// TestCipherRoundTripRandomNonceAndNoPlaintextAlias дважды шифрует, проверяет разные ciphertext и
// overhead/reservation counts, расшифровывает и проверяет сохранность caller plaintext/key после очистки
// output; это не статистическое доказательство nonce.
func TestCipherRoundTripRandomNonceAndNoPlaintextAlias(t *testing.T) {
	crypt, keys, binding, plaintext := cryptoFixture(t)
	first, err := crypt.Encrypt(t.Context(), binding, plaintext)
	if err != nil {
		t.Fatal("first encryption failed")
	}
	second, err := crypt.Encrypt(t.Context(), binding, plaintext)
	if err != nil {
		t.Fatal("second encryption failed")
	}
	if bytes.Equal(first.Ciphertext, second.Ciphertext) || bytes.Contains(first.Ciphertext, plaintext) || len(first.Ciphertext) != len(plaintext)+28 || keys.reserves != 2 {
		t.Fatal("encryption nonce, output boundary or durable reservation is invalid")
	}
	decoded, err := crypt.Decrypt(t.Context(), binding, first)
	if err != nil || !bytes.Equal(decoded, plaintext) {
		t.Fatal("round trip failed")
	}
	clear(decoded)
	if len(plaintext) == 0 || plaintext[0] == 0 || keys.material[0] == 0 {
		t.Fatal("caller-owned input or key was cleared")
	}
}

// TestCipherBindsEveryOwnerFieldAndKeyGeneration mutates each scoped binding coordinate and then key
// generation, requiring authentication failure with no plaintext, even when the stub still returns the
// same key bytes.
// TestCipherBindsEveryOwnerFieldAndKeyGeneration меняет каждую scoped binding coordinate и key generation,
// требуя authentication failure без plaintext, даже если подмена возвращает прежние key bytes.
func TestCipherBindsEveryOwnerFieldAndKeyGeneration(t *testing.T) {
	crypt, keys, binding, plaintext := cryptoFixture(t)
	encrypted, err := crypt.Encrypt(t.Context(), binding, plaintext)
	if err != nil {
		t.Fatal("fixture encryption failed")
	}
	cases := map[string]func(*value.SecretDraftBinding){
		"organization": func(b *value.SecretDraftBinding) { b.OrganizationRef = "org_foreign" },
		"scope":        func(b *value.SecretDraftBinding) { b.ScopeKind = "ORGANIZATION"; b.ProjectRef = "" },
		"project":      func(b *value.SecretDraftBinding) { b.ProjectRef = "prj_foreign" },
		"secret":       func(b *value.SecretDraftBinding) { b.SecretRef = "sec_foreign" },
		"draft":        func(b *value.SecretDraftBinding) { b.DraftRef = "drf_foreign" },
		"generation":   func(b *value.SecretDraftBinding) { b.DraftGeneration++ },
		"type":         func(b *value.SecretDraftBinding) { b.ValueType = "BINARY" },
		"digest":       func(b *value.SecretDraftBinding) { b.ContentSHA256 = strings.Repeat("0", 64) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			foreign := binding
			change(&foreign)
			output, err := crypt.Decrypt(t.Context(), foreign, encrypted)
			if !errors.Is(err, ErrEncryptedDraftInvalid) || len(output) != 0 {
				t.Fatal("foreign draft binding returned plaintext")
			}
		})
	}
	// Даже ошибочный resolver, сохранивший bytes при смене generation, не обходит AAD.
	keys.identity.Generation++
	encrypted.Key.Generation++
	if output, err := crypt.Decrypt(t.Context(), binding, encrypted); !errors.Is(err, ErrEncryptedDraftInvalid) || len(output) != 0 {
		t.Fatal("changed key generation returned plaintext")
	}
}

// TestCipherRejectsCorruptionAndMalformedBounds corrupts nonce/body/tag, tests empty/oversized ciphertext
// and empty/oversized or digest-mismatched plaintext, requiring closed errors and no output.
// TestCipherRejectsCorruptionAndMalformedBounds повреждает nonce/body/tag, проверяет пустой/слишком
// большой ciphertext и пустой/слишком большой либо digest-mismatched plaintext, требуя отказа без output.
func TestCipherRejectsCorruptionAndMalformedBounds(t *testing.T) {
	crypt, _, binding, plaintext := cryptoFixture(t)
	encrypted, err := crypt.Encrypt(t.Context(), binding, plaintext)
	if err != nil {
		t.Fatal("fixture encryption failed")
	}
	for _, position := range []int{0, 12, len(encrypted.Ciphertext) - 1} {
		corrupted := encrypted
		corrupted.Ciphertext = bytes.Clone(encrypted.Ciphertext)
		corrupted.Ciphertext[position] ^= 0x80
		if output, err := crypt.Decrypt(t.Context(), binding, corrupted); !errors.Is(err, ErrEncryptedDraftInvalid) || len(output) != 0 {
			t.Fatal("corrupt encrypted material returned plaintext")
		}
	}
	for _, length := range []int{0, 1, 28, value.MaximumDraftValueBytes + 29} {
		bad := encrypted
		bad.Ciphertext = make([]byte, length)
		if output, err := crypt.Decrypt(t.Context(), binding, bad); !errors.Is(err, ErrEncryptedDraftInvalid) || len(output) != 0 {
			t.Fatal("unbounded or empty encrypted material accepted")
		}
	}
	for _, input := range [][]byte{nil, []byte("different synthetic input"), make([]byte, value.MaximumDraftValueBytes+1)} {
		if result, err := crypt.Encrypt(t.Context(), binding, input); !errors.Is(err, ErrEncryptedDraftInvalid) || len(result.Ciphertext) != 0 {
			t.Fatal("unauthorized plaintext encrypted")
		}
	}
}

// TestCipherRequiresDurableReservationAndLiveContext checks denial, cancellation during reservation, no
// new reservation for an already-cancelled request and unavailable keys; the stub does not prove durable
// persistence.
// TestCipherRequiresDurableReservationAndLiveContext проверяет denial, cancellation при reservation,
// отсутствие нового reservation для уже отменённого запроса и unavailable keys; подмена не доказывает
// durable persistence.
func TestCipherRequiresDurableReservationAndLiveContext(t *testing.T) {
	crypt, keys, binding, plaintext := cryptoFixture(t)
	keys.denied = true
	if output, err := crypt.Encrypt(t.Context(), binding, plaintext); !errors.Is(err, ErrEncryptionUnavailable) || len(output.Ciphertext) != 0 {
		t.Fatal("denied durable reservation was ignored")
	}
	keys.denied = false
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	keys.cancel = cancel
	if output, err := crypt.Encrypt(ctx, binding, plaintext); !errors.Is(err, context.Canceled) || len(output.Ciphertext) != 0 {
		t.Fatal("cancelled reserved request encrypted plaintext")
	}
	before := keys.reserves
	if _, err := crypt.Encrypt(ctx, binding, plaintext); !errors.Is(err, context.Canceled) || keys.reserves != before {
		t.Fatal("cancelled request reached durable reservation")
	}
	keys.cancel = nil
	keys.unavailable = true
	if _, err := crypt.Encrypt(t.Context(), binding, plaintext); !errors.Is(err, ErrEncryptionUnavailable) {
		t.Fatal("missing key source did not fail closed")
	}
}
