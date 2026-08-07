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
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/codex-k8s/matter-codex/services/jobs/legacy-data-migration/internal/inventory"
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

func TestAuthenticatedStagingIsIndependentFromSourcePath(t *testing.T) {
	t.Parallel()
	key := bytes.Repeat([]byte{0x42}, 32)
	plaintext := []byte("authenticated immutable bytes")
	path := filepath.Join(t.TempDir(), "backup.dump.enc")
	if err := os.WriteFile(path, encodeTestBackup(t, key, plaintext), 0o600); err != nil {
		t.Fatal(err)
	}
	proof, err := stageAuthenticated(context.Background(), path, key)
	if err != nil {
		t.Fatalf("stageAuthenticated() error = %v", err)
	}
	defer proof.close()
	stagingInfo, err := proof.file.Stat()
	if err != nil || !safeStagingInode(stagingInfo, int64(len(plaintext))) {
		t.Fatalf("authenticated staging inode is not private: info=%v error=%v", stagingInfo, err)
	}
	if err := os.WriteFile(path, encodeTestBackup(t, key, []byte("replacement bytes")), 0o600); err != nil {
		t.Fatal(err)
	}
	actual, err := io.ReadAll(proof.file)
	if err != nil || !bytes.Equal(actual, plaintext) {
		t.Fatalf("authenticated staging changed with source path: value=%q error=%v", actual, err)
	}
}

func TestDatabaseCLIEnvironmentIsClosedAndTLS13Pinned(t *testing.T) {
	t.Parallel()
	environment, err := databaseEnvironment(
		"postgres://migration:secret@postgres.example:5432/source?sslmode=verify-full",
		"postgres.example", "/var/run/ca/source.crt",
	)
	if err != nil {
		t.Fatalf("databaseEnvironment() error = %v", err)
	}
	for _, expected := range []string{
		"LANG=C", "LC_ALL=C", "TZ=UTC", "PGHOST=postgres.example", "PGSSLMODE=verify-full",
		"PGSSLROOTCERT=/var/run/ca/source.crt", "PGSSLMINPROTOCOLVERSION=TLSv1.3",
		"PGSSLMAXPROTOCOLVERSION=TLSv1.3", "PGDATABASE=source", "PGUSER=migration",
	} {
		if !slices.Contains(environment, expected) {
			t.Fatalf("database environment misses %q: %#v", expected, environment)
		}
	}
	for _, entry := range environment {
		if strings.HasPrefix(entry, "PATH=") || strings.HasPrefix(entry, "HOME=") ||
			strings.HasPrefix(entry, "PGOPTIONS=") || strings.HasPrefix(entry, "PGSERVICE=") ||
			strings.Contains(entry, "postgres://") || strings.Contains(entry, "postgresql://") {
			t.Fatalf("database environment inherited an unsafe entry: %q", entry)
		}
	}
	for _, unsafe := range []struct {
		dsn, serverName, caFile string
	}{
		{"postgres://migration:secret@postgres.example/source?sslmode=disable", "postgres.example", "/ca.pem"},
		{"postgres://migration:secret@other.example/source?sslmode=verify-full", "postgres.example", "/ca.pem"},
		{"postgres://migration:secret@postgres.example/source?sslmode=verify-full&host=other", "postgres.example", "/ca.pem"},
		{"postgres://migration:secret@postgres.example/source?sslmode=verify-full&sslrootcert=/other.pem", "postgres.example", "/ca.pem"},
	} {
		if _, err := databaseEnvironment(unsafe.dsn, unsafe.serverName, unsafe.caFile); err == nil {
			t.Fatal("databaseEnvironment() accepted unsafe CLI routing")
		}
	}
}

func TestDumpAndRestoreInventoryUseExactClosedTableSet(t *testing.T) {
	t.Parallel()
	arguments := tableArguments()
	if len(arguments) != len(inventory.Tables) {
		t.Fatalf("table argument count = %d, want %d", len(arguments), len(inventory.Tables))
	}
	for index, table := range inventory.Tables {
		if arguments[index] != "--table=public."+table || strings.Contains(arguments[index], "*") {
			t.Fatalf("table argument %d is not exact: %q", index, arguments[index])
		}
	}
	var archive strings.Builder
	for index, table := range inventory.Tables {
		archive.WriteString(strconv.Itoa(index + 1))
		archive.WriteString("; 1259 1 TABLE public ")
		archive.WriteString(table)
		archive.WriteString(" owner\n")
	}
	if !validArchiveList(archive.String()) {
		t.Fatal("exact closed archive inventory was rejected")
	}
	archive.WriteString("999; 1259 1 TABLE public mattermost_posts owner\n")
	if validArchiveList(archive.String()) {
		t.Fatal("archive inventory accepted a foreign public table")
	}
	var foreignSchema strings.Builder
	foreignSchema.WriteString(strings.ReplaceAll(archive.String(),
		"999; 1259 1 TABLE public mattermost_posts owner\n", ""))
	foreignSchema.WriteString("999; 1259 1 TABLE private matter_codex_projects owner\n")
	if validArchiveList(foreignSchema.String()) {
		t.Fatal("archive inventory accepted a foreign schema object")
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
