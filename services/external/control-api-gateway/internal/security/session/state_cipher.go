package session

import (
	"crypto/cipher"
	"crypto/rand"
	"io"

	"github.com/codex-k8s/kodex/libs/go/eventing/browserstate"
)

// sealState preserves the server-record nonce-prefix envelope using the current
// key and the caller's exact AAD. It interprets no JSON or owner identity and
// maps entropy failure to the existing browser-state unavailable sentinel.
// sealState сохраняет server-record nonce-prefix envelope с current key и точным
// AAD вызывающего. JSON и owner identity не интерпретируются; entropy failure
// переводится в прежний browser-state unavailable sentinel.
func (store *Store) sealState(plain []byte, aad string) ([]byte, error) {
	nonce := make([]byte, store.current.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, browserstate.ErrUnavailable
	}
	return store.current.Seal(nonce, nonce, plain, []byte(aad)), nil
}

// openState tries the current then previous key against the exact raw envelope
// and AAD, preserving nil-key/short-input rejection and the final AEAD error.
// It does not decode JSON, check expiry, choose a record ID or alter storage.
// openState пробует current, затем previous key для точных raw envelope и AAD,
// сохраняя отказ nil-key и short-input и итоговую AEAD error. JSON, expiry
// и record ID не проверяются, storage не изменяется.
func (store *Store) openState(raw []byte, aad string) ([]byte, error) {
	decrypt := func(key cipher.AEAD) ([]byte, error) {
		if key == nil || len(raw) < key.NonceSize()+key.Overhead() {
			return nil, ErrReauthentication
		}
		return key.Open(nil, raw[:key.NonceSize()], raw[key.NonceSize():], []byte(aad))
	}
	plain, err := decrypt(store.current)
	if err != nil {
		plain, err = decrypt(store.previous)
	}
	return plain, err
}
