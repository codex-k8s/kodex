package backup

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestAuthenticatedReaderRejectsCiphertextTampering(t *testing.T) {
	t.Parallel()
	key := bytes.Repeat([]byte{0x42}, 32)
	plaintext := []byte("immutable source backup")
	encoded := encodeTestBackup(t, key, plaintext)
	path := filepath.Join(t.TempDir(), "backup.dump.enc")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	reader, evidence, closeReader, err := authenticatedReader(context.Background(), path, key)
	if err != nil {
		t.Fatalf("authenticatedReader() error = %v", err)
	}
	decoded, err := io.ReadAll(reader)
	closeReader()
	if err != nil || !bytes.Equal(decoded, plaintext) || evidence.SourceSHA256 != stringsOf("11", sha256.Size) ||
		evidence.CountsSHA256 != stringsOf("22", sha256.Size) {
		t.Fatalf("authenticated backup read mismatch: value=%q error=%v", decoded, err)
	}
	encoded[len(magic)+evidenceSize+aes.BlockSize] ^= 0xff
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, closeTampered, err := authenticatedReader(context.Background(), path, key); err == nil {
		closeTampered()
		t.Fatal("authenticatedReader() accepted a modified ciphertext")
	}
}

func TestBackupEvidenceBindsSourceAndCounts(t *testing.T) {
	t.Parallel()
	sourceSHA := stringsOf("ab", sha256.Size)
	counts := map[string]uint64{"matter_codex_projects": 2}
	encoded, err := encodeEvidence(sourceSHA, counts)
	if err != nil {
		t.Fatalf("encodeEvidence() error = %v", err)
	}
	evidence := backupEvidence{SourceSHA256: hex.EncodeToString(encoded[:sha256.Size]),
		CountsSHA256: hex.EncodeToString(encoded[sha256.Size:])}
	if !evidenceMatches(evidence, sourceSHA, counts) ||
		evidenceMatches(evidence, sourceSHA, map[string]uint64{"matter_codex_projects": 3}) ||
		evidenceMatches(evidence, stringsOf("cd", sha256.Size), counts) {
		t.Fatal("authenticated header did not bind exact source evidence")
	}
}

func encodeTestBackup(t *testing.T, key, plaintext []byte) []byte {
	t.Helper()
	derived := sha512.Sum512(key)
	block, err := aes.NewCipher(derived[:32])
	if err != nil {
		t.Fatal(err)
	}
	nonce := bytes.Repeat([]byte{0x24}, aes.BlockSize)
	sourceEvidence := bytes.Repeat([]byte{0x11}, sha256.Size)
	countsEvidence := bytes.Repeat([]byte{0x22}, sha256.Size)
	header := append([]byte(magic), sourceEvidence...)
	header = append(header, countsEvidence...)
	header = append(header, nonce...)
	ciphertext := make([]byte, len(plaintext))
	cipher.NewCTR(block, nonce).XORKeyStream(ciphertext, plaintext)
	authenticator := hmac.New(sha256.New, derived[32:])
	_, _ = authenticator.Write(header)
	_, _ = authenticator.Write(ciphertext)
	return append(append(header, ciphertext...), authenticator.Sum(nil)...)
}

func stringsOf(pair string, count int) string {
	value := bytes.Repeat([]byte{0}, count)
	decoded, _ := hex.DecodeString(pair)
	for index := range value {
		value[index] = decoded[0]
	}
	return hex.EncodeToString(value)
}
