package session

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"errors"
	"testing"
)

// TestStateCipherPreservesEnvelopeAndKeyFallback cross-checks the shared helpers
// against direct AEAD construction for all three record AAD domains, previous-key
// overlap, empty/binary plaintext and rejection of tampering or invalid bindings.
// All keys/envelopes are local synthetic fixtures, not deployed session records.
// TestStateCipherPreservesEnvelopeAndKeyFallback сверяет общие helpers с прямым
// AEAD construction для трёх record AAD domains, previous-key overlap, empty
// и binary plaintext и отказа при tampering либо invalid bindings. Все keys
// и envelopes — local synthetic fixtures, а не deployed session records.
func TestStateCipherPreservesEnvelopeAndKeyFallback(t *testing.T) {
	var keys [2]cipher.AEAD
	for index := range keys {
		block, err := aes.NewCipher(bytes.Repeat([]byte{byte(index + 17)}, 32))
		if err != nil {
			t.Fatal(err)
		}
		keys[index], err = cipher.NewGCM(block)
		if err != nil {
			t.Fatal(err)
		}
	}
	current := &Store{current: keys[0]}
	rotated := &Store{current: keys[1], previous: keys[0]}
	for _, aad := range []string{
		"browser-family-v1:family-fixture",
		"browser-login-v1:login-fixture",
		"browser-ws-tickets-v1:ledger-fixture",
	} {
		t.Run(aad, func(t *testing.T) {
			plain := []byte{'a', 0, 0xff, 'z'}
			sealed, err := current.sealState(plain, aad)
			if err != nil || len(sealed) != keys[0].NonceSize()+len(plain)+keys[0].Overhead() {
				t.Fatal("server-state envelope length changed", err)
			}
			opened, err := keys[0].Open(nil, sealed[:keys[0].NonceSize()], sealed[keys[0].NonceSize():], []byte(aad))
			if err != nil || !bytes.Equal(opened, plain) {
				t.Fatal("shared seal no longer matches direct AEAD", err)
			}
			nonce := bytes.Repeat([]byte{3}, keys[0].NonceSize())
			oldEnvelope := keys[0].Seal(nonce, nonce, plain, []byte(aad))
			for _, reader := range []*Store{current, rotated, {previous: keys[0]}} {
				opened, err := reader.openState(oldEnvelope, aad)
				if err != nil || !bytes.Equal(opened, plain) {
					t.Fatal("existing server-state envelope rejected", err)
				}
			}
			changed := bytes.Clone(oldEnvelope)
			changed[len(changed)-1] ^= 1
			for _, test := range []struct {
				name   string
				reader *Store
				raw    []byte
				aad    string
			}{
				{"different AAD", rotated, oldEnvelope, aad + ":different"},
				{"tampered bytes", rotated, changed, aad},
				{"truncated envelope", current, oldEnvelope[:keys[0].NonceSize()+keys[0].Overhead()-1], aad},
				{"missing overlap", &Store{current: keys[1]}, oldEnvelope, aad},
				{"no keys", &Store{}, oldEnvelope, aad},
			} {
				t.Run(test.name, func(t *testing.T) {
					if _, err := test.reader.openState(test.raw, test.aad); err == nil {
						t.Fatal("invalid server-state envelope accepted")
					}
				})
			}
		})
	}
	if _, err := current.openState(nil, "empty"); !errors.Is(err, ErrReauthentication) {
		t.Fatal("empty server-state rejection changed", err)
	}
	sealed, err := current.sealState(nil, "empty-plaintext")
	if err != nil {
		t.Fatal(err)
	}
	if opened, err := current.openState(sealed, "empty-plaintext"); err != nil || len(opened) != 0 {
		t.Fatal("empty plaintext no longer round-trips", err)
	}
}
