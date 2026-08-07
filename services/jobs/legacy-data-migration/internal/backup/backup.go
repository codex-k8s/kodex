// Package backup создаёт зашифрованный immutable pg_dump и проверяет restore stream.
package backup

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/jobs/legacy-data-migration/internal/model"
)

const magic = "MCMIG02\x00"
const tagSize = sha256.Size
const evidenceSize = sha256.Size * 2

type backupEvidence struct {
	SourceSHA256 string
	CountsSHA256 string
}

type Result struct {
	BackupPath     string
	ManifestPath   string
	BackupSHA256   string
	ManifestSHA256 string
	BackupBytes    int64
}

func Create(ctx context.Context, directory, planID, dsn, tlsServerName, caFile, snapshotID, keyFile, sourceSHA string,
	counts map[string]uint64, now time.Time,
) (Result, error) {
	key, err := loadKey(keyFile)
	if err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return Result{}, errors.New("create backup directory")
	}
	info, err := os.Stat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return Result{}, errors.New("backup directory permissions are unsafe")
	}
	backupPath := filepath.Join(directory, planID+".dump.enc")
	manifestPath := filepath.Join(directory, planID+".manifest.json")
	if _, err := os.Stat(backupPath); err == nil {
		if _, manifestErr := os.Stat(manifestPath); errors.Is(manifestErr, os.ErrNotExist) {
			return recoverManifest(ctx, directory, backupPath, manifestPath, key, planID, sourceSHA, counts, now)
		} else if manifestErr != nil {
			return Result{}, errors.New("inspect backup manifest path")
		}
		return readExisting(ctx, backupPath, manifestPath, key, planID, sourceSHA, counts)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Result{}, errors.New("inspect backup path")
	}
	backupSHA, backupBytes, err := createEncryptedDump(ctx, backupPath, dsn, tlsServerName, caFile, snapshotID, key,
		sourceSHA, counts)
	if err != nil {
		return Result{}, err
	}
	if _, err := verifyRestore(ctx, backupPath, key); err != nil {
		return Result{}, err
	}
	manifest := model.Manifest{
		SchemaVersion: "mattercodex.legacy-data-backup-manifest.v1",
		PlanID:        planID, SourceSHA256: sourceSHA, BackupSHA256: backupSHA,
		BackupBytes: backupBytes, TableCounts: counts,
		CreatedAt: now.UTC().Truncate(time.Microsecond), RestoreCheck: "pg_restore_list_verified",
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return Result{}, errors.New("encode backup manifest")
	}
	manifestBytes = append(manifestBytes, '\n')
	manifestSHA := digest(manifestBytes)
	if err := writeExclusive(manifestPath, manifestBytes); err != nil {
		return Result{}, err
	}
	if err := syncDirectory(directory); err != nil {
		return Result{}, err
	}
	return Result{BackupPath: backupPath, ManifestPath: manifestPath, BackupSHA256: backupSHA,
		ManifestSHA256: manifestSHA, BackupBytes: backupBytes}, nil
}

// recoverManifest завершает единственное допустимое crash-окно: dump уже
// полностью записан и аутентифицируется, а sidecar manifest ещё не создан.
func recoverManifest(ctx context.Context, directory, backupPath, manifestPath string, key []byte, planID, sourceSHA string,
	counts map[string]uint64, now time.Time,
) (Result, error) {
	evidence, err := verifyRestore(ctx, backupPath, key)
	if err != nil {
		return Result{}, err
	}
	if !evidenceMatches(evidence, sourceSHA, counts) {
		return Result{}, errors.New("orphaned backup evidence does not match current source snapshot")
	}
	backupSHA, backupBytes, err := fileDigest(ctx, backupPath)
	if err != nil {
		return Result{}, err
	}
	manifest := model.Manifest{SchemaVersion: "mattercodex.legacy-data-backup-manifest.v1",
		PlanID: planID, SourceSHA256: sourceSHA, BackupSHA256: backupSHA, BackupBytes: backupBytes,
		TableCounts: counts, CreatedAt: now.UTC().Truncate(time.Microsecond), RestoreCheck: "pg_restore_list_verified"}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return Result{}, errors.New("encode recovered backup manifest")
	}
	manifestBytes = append(manifestBytes, '\n')
	if err := writeExclusive(manifestPath, manifestBytes); err != nil {
		return Result{}, err
	}
	if err := syncDirectory(directory); err != nil {
		return Result{}, err
	}
	return Result{BackupPath: backupPath, ManifestPath: manifestPath, BackupSHA256: backupSHA,
		ManifestSHA256: digest(manifestBytes), BackupBytes: backupBytes}, nil
}

func createEncryptedDump(ctx context.Context, path, dsn, tlsServerName, caFile, snapshotID string, key []byte,
	sourceSHA string, counts map[string]uint64,
) (string, int64, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", 0, errors.New("create immutable backup")
	}
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	derived := sha512.Sum512(key)
	block, err := aes.NewCipher(derived[:32])
	if err != nil {
		return "", 0, errors.New("initialize backup encryption")
	}
	nonce := make([]byte, aes.BlockSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", 0, errors.New("create backup nonce")
	}
	evidence, err := encodeEvidence(sourceSHA, counts)
	if err != nil {
		return "", 0, err
	}
	header := append([]byte(magic), evidence...)
	header = append(header, nonce...)
	fileHash := sha256.New()
	mac := hmac.New(sha256.New, derived[32:])
	if _, err := io.MultiWriter(file, fileHash, mac).Write(header); err != nil {
		return "", 0, errors.New("write backup header")
	}
	encrypted := &cipher.StreamWriter{S: cipher.NewCTR(block, nonce), W: io.MultiWriter(file, fileHash, mac)}
	command := exec.CommandContext(ctx, "pg_dump", "--format=custom", "--no-owner", "--no-acl", "--snapshot="+snapshotID)
	command.Env = databaseEnvironment(dsn, tlsServerName, caFile)
	command.Stdout = encrypted
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return "", 0, errors.New("create source backup")
	}
	tag := mac.Sum(nil)
	if _, err := io.MultiWriter(file, fileHash).Write(tag); err != nil {
		return "", 0, errors.New("write backup authentication tag")
	}
	if err := file.Sync(); err != nil {
		return "", 0, errors.New("sync immutable backup")
	}
	info, err := file.Stat()
	if err != nil {
		return "", 0, errors.New("read backup metadata")
	}
	if err := file.Close(); err != nil {
		return "", 0, errors.New("close immutable backup")
	}
	if err := syncDirectory(filepath.Dir(path)); err != nil {
		return "", 0, err
	}
	ok = true
	return hex.EncodeToString(fileHash.Sum(nil)), info.Size(), nil
}

func verifyRestore(ctx context.Context, path string, key []byte) (backupEvidence, error) {
	reader, evidence, closeReader, err := authenticatedReader(ctx, path, key)
	if err != nil {
		return backupEvidence{}, err
	}
	defer closeReader()
	command := exec.CommandContext(ctx, "pg_restore", "--list", "-")
	command.Stdin = reader
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return backupEvidence{}, errors.New("backup restore verification failed")
	}
	return evidence, nil
}

// Restore загружает аутентифицированный backup только в заранее проверенную
// пустую изолированную PostgreSQL database.
func Restore(ctx context.Context, path, keyFile, dsn, tlsServerName, caFile string) error {
	key, err := loadKey(keyFile)
	if err != nil {
		return err
	}
	reader, _, closeReader, err := authenticatedReader(ctx, path, key)
	if err != nil {
		return err
	}
	defer closeReader()
	environment, database, err := restoreEnvironment(dsn, tlsServerName, caFile)
	if err != nil {
		return err
	}
	command := exec.CommandContext(ctx, "pg_restore", "--exit-on-error", "--single-transaction",
		"--no-owner", "--no-acl", "--dbname", database, "-")
	command.Env = environment
	command.Stdin = reader
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return errors.New("restore backup into verification database")
	}
	return nil
}

func authenticatedReader(ctx context.Context, path string, key []byte) (io.Reader, backupEvidence, func(), error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, backupEvidence{}, func() {}, errors.New("open backup for restore verification")
	}
	closeReader := func() { _ = file.Close() }
	info, err := file.Stat()
	headerSize := len(magic) + evidenceSize + aes.BlockSize
	if err != nil || info.Size() <= int64(headerSize+tagSize) {
		closeReader()
		return nil, backupEvidence{}, func() {}, errors.New("encrypted backup is incomplete")
	}
	header := make([]byte, headerSize)
	if _, err := io.ReadFull(file, header); err != nil || string(header[:len(magic)]) != magic {
		closeReader()
		return nil, backupEvidence{}, func() {}, errors.New("encrypted backup header is invalid")
	}
	evidence := backupEvidence{
		SourceSHA256: hex.EncodeToString(header[len(magic) : len(magic)+sha256.Size]),
		CountsSHA256: hex.EncodeToString(header[len(magic)+sha256.Size : len(magic)+evidenceSize]),
	}
	derived := sha512.Sum512(key)
	mac := hmac.New(sha256.New, derived[32:])
	ciphertextSize := info.Size() - int64(len(header)) - tagSize
	if _, err := mac.Write(header); err != nil {
		closeReader()
		return nil, backupEvidence{}, func() {}, errors.New("verify backup header")
	}
	if _, err := copyContext(ctx, mac, io.NewSectionReader(file, int64(len(header)), ciphertextSize)); err != nil {
		closeReader()
		return nil, backupEvidence{}, func() {}, errors.New("verify backup ciphertext")
	}
	tag := make([]byte, tagSize)
	if _, err := file.ReadAt(tag, info.Size()-tagSize); err != nil || !hmac.Equal(tag, mac.Sum(nil)) {
		closeReader()
		return nil, backupEvidence{}, func() {}, errors.New("backup authentication failed")
	}
	block, err := aes.NewCipher(derived[:32])
	if err != nil {
		closeReader()
		return nil, backupEvidence{}, func() {}, errors.New("initialize restore decryption")
	}
	reader := &cipher.StreamReader{
		S: cipher.NewCTR(block, header[len(magic)+evidenceSize:]),
		R: io.NewSectionReader(file, int64(len(header)), ciphertextSize),
	}
	return reader, evidence, closeReader, nil
}

func readExisting(ctx context.Context, backupPath, manifestPath string, key []byte, planID, sourceSHA string,
	counts map[string]uint64,
) (Result, error) {
	evidence, err := verifyRestore(ctx, backupPath, key)
	if err != nil {
		return Result{}, err
	}
	backupSHA, backupBytes, err := fileDigest(ctx, backupPath)
	if err != nil {
		return Result{}, err
	}
	manifestBytes, err := readBounded(manifestPath, 1024*1024)
	if err != nil {
		return Result{}, errors.New("read existing backup manifest")
	}
	manifest, err := decodeManifest(manifestBytes)
	if err != nil || !validManifest(manifest) || manifest.PlanID != planID ||
		manifest.SourceSHA256 != sourceSHA || manifest.BackupSHA256 != backupSHA ||
		manifest.BackupBytes != backupBytes || manifest.SchemaVersion != "mattercodex.legacy-data-backup-manifest.v1" ||
		manifest.RestoreCheck != "pg_restore_list_verified" || !evidenceMatches(evidence, manifest.SourceSHA256, manifest.TableCounts) ||
		counts != nil && !sameCounts(manifest.TableCounts, counts) {
		return Result{}, errors.New("existing backup manifest mismatch")
	}
	return Result{BackupPath: backupPath, ManifestPath: manifestPath,
		BackupSHA256: manifest.BackupSHA256, ManifestSHA256: digest(manifestBytes), BackupBytes: backupBytes}, nil
}

func copyContext(ctx context.Context, destination io.Writer, source io.Reader) (int64, error) {
	buffer := make([]byte, 64*1024)
	var written int64
	for {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		count, readErr := source.Read(buffer)
		if count > 0 {
			output, writeErr := destination.Write(buffer[:count])
			written += int64(output)
			if writeErr != nil {
				return written, writeErr
			}
			if output != count {
				return written, io.ErrShortWrite
			}
		}
		if errors.Is(readErr, io.EOF) {
			return written, nil
		}
		if readErr != nil {
			return written, readErr
		}
	}
}

// LoadExisting проверяет immutable backup/manifest без обращения к source.
func LoadExisting(ctx context.Context, directory, planID, keyFile string) (Result, model.Manifest, error) {
	key, err := loadKey(keyFile)
	if err != nil {
		return Result{}, model.Manifest{}, err
	}
	backupPath := filepath.Join(directory, planID+".dump.enc")
	manifestPath := filepath.Join(directory, planID+".manifest.json")
	manifestBytes, err := readBounded(manifestPath, 1024*1024)
	if err != nil {
		return Result{}, model.Manifest{}, errors.New("read existing backup manifest")
	}
	manifest, err := decodeManifest(manifestBytes)
	if err != nil || !validManifest(manifest) || manifest.PlanID != planID {
		return Result{}, model.Manifest{}, errors.New("existing backup manifest mismatch")
	}
	result, err := readExisting(ctx, backupPath, manifestPath, key, planID, manifest.SourceSHA256, nil)
	return result, manifest, err
}

func fileDigest(ctx context.Context, path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, errors.New("open existing backup")
	}
	hash := sha256.New()
	size, copyErr := copyContext(ctx, hash, file)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		return "", 0, errors.New("digest existing backup")
	}
	return hex.EncodeToString(hash.Sum(nil)), size, nil
}

func sameCounts(left, right map[string]uint64) bool {
	if len(left) != len(right) {
		return false
	}
	for table, count := range left {
		if right[table] != count {
			return false
		}
	}
	return true
}

func encodeEvidence(sourceSHA string, counts map[string]uint64) ([]byte, error) {
	sourceDigest, err := hex.DecodeString(sourceSHA)
	if err != nil || len(sourceDigest) != sha256.Size || sourceSHA != strings.ToLower(sourceSHA) || counts == nil {
		return nil, errors.New("backup source evidence is invalid")
	}
	encodedCounts, err := json.Marshal(counts)
	if err != nil {
		return nil, errors.New("encode backup table counts")
	}
	countsDigest := sha256.Sum256(encodedCounts)
	return append(sourceDigest, countsDigest[:]...), nil
}

func evidenceMatches(evidence backupEvidence, sourceSHA string, counts map[string]uint64) bool {
	encoded, err := encodeEvidence(sourceSHA, counts)
	return err == nil && evidence.SourceSHA256 == hex.EncodeToString(encoded[:sha256.Size]) &&
		evidence.CountsSHA256 == hex.EncodeToString(encoded[sha256.Size:])
}

func validManifest(manifest model.Manifest) bool {
	return manifest.SchemaVersion == "mattercodex.legacy-data-backup-manifest.v1" &&
		manifest.PlanID != "" && validDigest(manifest.SourceSHA256) && validDigest(manifest.BackupSHA256) &&
		manifest.BackupBytes > 0 && manifest.TableCounts != nil && !manifest.CreatedAt.IsZero() &&
		manifest.RestoreCheck == "pg_restore_list_verified"
}

func validDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == strings.ToLower(value)
}

func decodeManifest(content []byte) (model.Manifest, error) {
	var manifest model.Manifest
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return model.Manifest{}, err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return model.Manifest{}, errors.New("backup manifest has trailing data")
	}
	return manifest, nil
}

func loadKey(path string) ([]byte, error) {
	raw, err := readBounded(path, 4096)
	if err != nil {
		return nil, errors.New("read backup encryption key")
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(decoded) != 32 {
		return nil, errors.New("backup encryption key is invalid")
	}
	return decoded, nil
}

func readBounded(path string, maximum int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || int64(len(content)) > maximum {
		return nil, errors.New("file exceeds safe size")
	}
	return content, nil
}

func databaseEnvironment(dsn, tlsServerName, caFile string) []string {
	environment := cleanDatabaseEnvironment()
	return append(environment, "PGDATABASE="+dsn, "PGHOST="+tlsServerName,
		"PGSSLMODE=verify-full", "PGSSLROOTCERT="+caFile)
}

func restoreEnvironment(dsn, tlsServerName, caFile string) ([]string, string, error) {
	parsed, err := url.Parse(dsn)
	if err != nil || parsed == nil || parsed.User == nil || parsed.Hostname() != tlsServerName {
		return nil, "", errors.New("restore database configuration is invalid")
	}
	database := strings.TrimPrefix(parsed.EscapedPath(), "/")
	database, err = url.PathUnescape(database)
	if err != nil || database == "" || strings.Contains(database, "/") {
		return nil, "", errors.New("restore database name is invalid")
	}
	password, passwordPresent := parsed.User.Password()
	if !passwordPresent || parsed.User.Username() == "" {
		return nil, "", errors.New("restore database credentials are invalid")
	}
	port := parsed.Port()
	if port == "" {
		port = "5432"
	}
	environment := append(cleanDatabaseEnvironment(),
		"PGHOST="+tlsServerName, "PGPORT="+port, "PGUSER="+parsed.User.Username(), "PGPASSWORD="+password,
		"PGDATABASE="+database, "PGSSLMODE=verify-full", "PGSSLROOTCERT="+caFile)
	return environment, database, nil
}

func cleanDatabaseEnvironment() []string {
	environment := make([]string, 0, len(os.Environ())+1)
	for _, value := range os.Environ() {
		name, _, _ := strings.Cut(value, "=")
		if !strings.HasPrefix(name, "PG") {
			environment = append(environment, value)
		}
	}
	return environment
}

func writeExclusive(path string, content []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return errors.New("create immutable backup manifest")
	}
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(content); err != nil {
		return errors.New("write backup manifest")
	}
	if err := file.Sync(); err != nil {
		return errors.New("sync backup manifest")
	}
	if err := file.Close(); err != nil {
		return errors.New("close backup manifest")
	}
	ok = true
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return errors.New("open backup directory for sync")
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil || closeErr != nil {
		return errors.New("sync backup directory")
	}
	return nil
}

func digest(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}
