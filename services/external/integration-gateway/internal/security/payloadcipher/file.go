package payloadcipher

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

const maximumKeysetBytes = 64 << 10

type FileCipher struct {
	path string
}

type keyset struct {
	Active string            `json:"active"`
	Keys   map[string]string `json:"keys"`
}

func New(path string) (*FileCipher, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("payload keyset path must be absolute")
	}
	value := &FileCipher{path: path}
	if _, _, err := value.load(); err != nil {
		return nil, err
	}
	return value, nil
}

func (file *FileCipher) Check(_ context.Context) error {
	_, _, err := file.load()
	return err
}

func (file *FileCipher) Encrypt(_ context.Context, plaintext []byte) ([]byte, error) {
	active, keys, err := file.load()
	if err != nil {
		return nil, err
	}
	aead, err := newAEAD(keys[active])
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, errors.New("generate payload nonce")
	}
	if len(active) > 255 {
		return nil, errors.New("payload key identifier is invalid")
	}
	result := make([]byte, 1+len(active)+len(nonce))
	result[0] = byte(len(active))
	copy(result[1:], active)
	copy(result[1+len(active):], nonce)
	return aead.Seal(result, nonce, plaintext, []byte(active)), nil
}

func (file *FileCipher) Decrypt(_ context.Context, ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < 2 {
		return nil, errors.New("encrypted payload is invalid")
	}
	identifierLength := int(ciphertext[0])
	if identifierLength == 0 || len(ciphertext) <= 1+identifierLength {
		return nil, errors.New("encrypted payload is invalid")
	}
	identifier := string(ciphertext[1 : 1+identifierLength])
	_, keys, err := file.load()
	if err != nil {
		return nil, err
	}
	aead, err := newAEAD(keys[identifier])
	if err != nil {
		return nil, errors.New("payload key is unavailable")
	}
	offset := 1 + identifierLength
	if len(ciphertext) < offset+aead.NonceSize()+aead.Overhead() {
		return nil, errors.New("encrypted payload is invalid")
	}
	nonce := ciphertext[offset : offset+aead.NonceSize()]
	plaintext, err := aead.Open(nil, nonce, ciphertext[offset+aead.NonceSize():], []byte(identifier))
	if err != nil {
		return nil, errors.New("decrypt payload")
	}
	return plaintext, nil
}

func (file *FileCipher) load() (string, map[string][]byte, error) {
	info, err := os.Stat(file.path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximumKeysetBytes || info.Mode().Perm()&0o007 != 0 {
		return "", nil, errors.New("payload keyset file is unsafe")
	}
	raw, err := os.ReadFile(file.path)
	if err != nil {
		return "", nil, errors.New("read payload keyset")
	}
	var source keyset
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&source); err != nil || source.Active == "" || len(source.Keys) == 0 || len(source.Keys) > 8 {
		return "", nil, errors.New("payload keyset is invalid")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return "", nil, errors.New("trailing payload keyset data is forbidden")
	}
	keys := make(map[string][]byte, len(source.Keys))
	for identifier, encoded := range source.Keys {
		decoded, err := base64.StdEncoding.Strict().DecodeString(encoded)
		if err != nil || len(decoded) != 32 || identifier == "" || len(identifier) > 64 {
			return "", nil, errors.New("payload keyset is invalid")
		}
		keys[identifier] = decoded
	}
	if _, ok := keys[source.Active]; !ok {
		return "", nil, errors.New("active payload key is unavailable")
	}
	return source.Active, keys, nil
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, errors.New("payload key is invalid")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errors.New("initialize payload cipher")
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, errors.New("initialize payload AEAD")
	}
	return aead, nil
}
