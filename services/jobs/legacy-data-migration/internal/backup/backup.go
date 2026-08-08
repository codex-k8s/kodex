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
	"syscall"
	"time"

	"github.com/codex-k8s/matter-codex/services/jobs/legacy-data-migration/internal/inventory"
	"github.com/codex-k8s/matter-codex/services/jobs/legacy-data-migration/internal/model"
)

const magic = "MCMIG02\x00"
const tagSize = sha256.Size
const evidenceSize = sha256.Size * 2

type backupEvidence struct {
	SourceSHA256 string
	CountsSHA256 string
}

type authenticatedBackup struct {
	file         *os.File
	evidence     backupEvidence
	backupSHA256 string
	backupBytes  int64
	close        func()
}

type Result struct {
	BackupPath     string
	ManifestPath   string
	BackupSHA256   string
	ManifestSHA256 string
	BackupBytes    int64
}

func Create(ctx context.Context, directory, planID, dsn, tlsServerName, caFile, snapshotID, keyFile, sourceSHA string,
	counts map[string]uint64, now time.Time, maximumPlaintextBytes int64,
) (Result, error) {
	key, err := loadKey(keyFile)
	if err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return Result{}, errors.New("create backup directory")
	}
	info, err := os.Lstat(directory)
	if err != nil || !safePrivateDirectory(info) {
		return Result{}, errors.New("backup directory permissions are unsafe")
	}
	backupPath := filepath.Join(directory, planID+".dump.enc")
	manifestPath := filepath.Join(directory, planID+".manifest.json")
	if _, err := os.Stat(backupPath); err == nil {
		if _, manifestErr := os.Stat(manifestPath); errors.Is(manifestErr, os.ErrNotExist) {
			return recoverManifest(ctx, directory, backupPath, manifestPath, key, planID, sourceSHA, counts, now,
				maximumPlaintextBytes)
		} else if manifestErr != nil {
			return Result{}, errors.New("inspect backup manifest path")
		}
		return readExisting(ctx, backupPath, manifestPath, key, planID, sourceSHA, counts, maximumPlaintextBytes)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Result{}, errors.New("inspect backup path")
	}
	backupSHA, backupBytes, err := createEncryptedDump(ctx, backupPath, dsn, tlsServerName, caFile, snapshotID, key,
		sourceSHA, counts)
	if err != nil {
		return Result{}, err
	}
	proof, err := verifyRestore(ctx, backupPath, key, maximumPlaintextBytes)
	if err != nil {
		return Result{}, err
	}
	if proof.backupSHA256 != backupSHA || proof.backupBytes != backupBytes {
		return Result{}, errors.New("created backup readback mismatch")
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
	counts map[string]uint64, now time.Time, maximumPlaintextBytes int64,
) (Result, error) {
	proof, err := verifyRestore(ctx, backupPath, key, maximumPlaintextBytes)
	if err != nil {
		return Result{}, err
	}
	if !evidenceMatches(proof.evidence, sourceSHA, counts) {
		return Result{}, errors.New("orphaned backup evidence does not match current source snapshot")
	}
	backupSHA, backupBytes := proof.backupSHA256, proof.backupBytes
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
	environment, err := databaseEnvironment(dsn, tlsServerName, caFile)
	if err != nil {
		return "", 0, err
	}
	if err := verifyCLITransport(ctx, environment); err != nil {
		return "", 0, err
	}
	arguments := []string{"--format=custom", "--no-owner", "--no-acl", "--strict-names",
		"--section=pre-data", "--section=data", "--snapshot=" + snapshotID}
	arguments = append(arguments, tableArguments()...)
	command := exec.CommandContext(ctx, "pg_dump", arguments...)
	command.Env = environment
	command.Stdout = encrypted
	command.Stderr = io.Discard
	if err := runSubprocess(command); err != nil {
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

func verifyRestore(ctx context.Context, path string, key []byte, maximumPlaintextBytes int64) (authenticatedBackup, error) {
	proof, err := stageAuthenticated(ctx, path, key, maximumPlaintextBytes)
	if err != nil {
		return authenticatedBackup{}, err
	}
	defer proof.close()
	command := exec.CommandContext(ctx, "pg_restore", "--list", "-")
	command.Stdin = proof.file
	var list bytes.Buffer
	command.Stdout = &list
	command.Stderr = io.Discard
	if err := runSubprocess(command); err != nil || !validArchiveList(list.String()) {
		return authenticatedBackup{}, errors.New("backup restore verification failed")
	}
	return proof, nil
}

// Restore загружает аутентифицированный backup только в заранее проверенную
// пустую изолированную PostgreSQL database.
func Restore(ctx context.Context, path, keyFile, dsn, tlsServerName, caFile string,
	maximumPlaintextBytes int64,
) error {
	key, err := loadKey(keyFile)
	if err != nil {
		return err
	}
	proof, err := stageAuthenticated(ctx, path, key, maximumPlaintextBytes)
	if err != nil {
		return err
	}
	defer proof.close()
	environment, database, err := restoreEnvironment(dsn, tlsServerName, caFile)
	if err != nil {
		return err
	}
	if err := verifyCLITransport(ctx, environment); err != nil {
		return err
	}
	listCommand := exec.CommandContext(ctx, "pg_restore", "--list", "-")
	listCommand.Stdin = proof.file
	var list bytes.Buffer
	listCommand.Stdout = &list
	listCommand.Stderr = io.Discard
	if err := runSubprocess(listCommand); err != nil || !validArchiveList(list.String()) {
		return errors.New("backup restore inventory is invalid")
	}
	if _, err := proof.file.Seek(0, io.SeekStart); err != nil {
		return errors.New("rewind authenticated restore staging")
	}
	command := exec.CommandContext(ctx, "pg_restore", "--exit-on-error", "--single-transaction",
		"--no-owner", "--no-acl", "--dbname", database, "-")
	command.Env = environment
	command.Stdin = proof.file
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := runSubprocess(command); err != nil {
		return errors.New("restore backup into verification database")
	}
	return nil
}

func authenticatedReader(ctx context.Context, path string, key []byte,
	maximumPlaintextBytes int64,
) (io.Reader, backupEvidence, func(), error) {
	proof, err := stageAuthenticated(ctx, path, key, maximumPlaintextBytes)
	if err != nil {
		return nil, backupEvidence{}, func() {}, err
	}
	return proof.file, proof.evidence, proof.close, nil
}

// stageAuthenticated не выдаёт consumer ни одного plaintext byte, пока один
// проход по exact O_NOFOLLOW inode не проверил HMAC. Consumer затем читает
// только unlinked private staging file, поэтому rename/hardlink/truncate
// исходного PVC-файла не образует TOCTOU.
func stageAuthenticated(ctx context.Context, path string, key []byte,
	maximumPlaintextBytes int64,
) (authenticatedBackup, error) {
	descriptor, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return authenticatedBackup{}, errors.New("open immutable backup")
	}
	file := os.NewFile(uintptr(descriptor), path)
	closeSource := func() { _ = file.Close() }
	info, err := file.Stat()
	headerSize := len(magic) + evidenceSize + aes.BlockSize
	if err != nil || maximumPlaintextBytes < 1 || !safeBackupInode(info, headerSize) {
		closeSource()
		return authenticatedBackup{}, errors.New("encrypted backup inode is unsafe")
	}
	header := make([]byte, headerSize)
	if _, err := io.ReadFull(file, header); err != nil || string(header[:len(magic)]) != magic {
		closeSource()
		return authenticatedBackup{}, errors.New("encrypted backup header is invalid")
	}
	evidence := backupEvidence{
		SourceSHA256: hex.EncodeToString(header[len(magic) : len(magic)+sha256.Size]),
		CountsSHA256: hex.EncodeToString(header[len(magic)+sha256.Size : len(magic)+evidenceSize]),
	}
	derived := sha512.Sum512(key)
	mac := hmac.New(sha256.New, derived[32:])
	fileHash := sha256.New()
	ciphertextSize := info.Size() - int64(len(header)) - tagSize
	// AES-CTR сохраняет длину plaintext. Проверка выполняется до создания
	// unlinked inode и учитывает весь authenticated envelope отдельно: kubelet
	// eviction не является синхронной границей для deleted-but-open файла.
	maximumEnvelopeBytes := maximumPlaintextBytes + int64(headerSize+tagSize)
	if ciphertextSize <= 0 || ciphertextSize > maximumPlaintextBytes || info.Size() > maximumEnvelopeBytes {
		closeSource()
		return authenticatedBackup{}, errors.New("authenticated backup exceeds staging capacity")
	}
	if _, err := io.MultiWriter(mac, fileHash).Write(header); err != nil {
		closeSource()
		return authenticatedBackup{}, errors.New("verify backup header")
	}
	stagingDirectory, err := os.MkdirTemp("", "mattercodex-authenticated-restore-")
	if err != nil || os.Chmod(stagingDirectory, 0o700) != nil {
		closeSource()
		return authenticatedBackup{}, errors.New("create private restore staging")
	}
	staging, err := os.CreateTemp(stagingDirectory, "plaintext-")
	if err != nil || staging.Chmod(0o600) != nil {
		closeSource()
		if staging != nil {
			stagingPath := staging.Name()
			_ = staging.Close()
			_ = os.Remove(stagingPath)
		}
		_ = os.Remove(stagingDirectory)
		return authenticatedBackup{}, errors.New("create private restore staging")
	}
	stagingPath := staging.Name()
	if err := os.Remove(stagingPath); err != nil {
		_ = staging.Close()
		closeSource()
		_ = os.Remove(stagingDirectory)
		return authenticatedBackup{}, errors.New("unlink private restore staging")
	}
	_ = os.Remove(stagingDirectory)
	cleanup := func() {
		_ = staging.Close()
		closeSource()
		_ = os.Remove(stagingDirectory)
	}
	if stagingInfo, statErr := staging.Stat(); statErr != nil || !safeStagingInode(stagingInfo, 0) {
		cleanup()
		return authenticatedBackup{}, errors.New("private restore staging inode is unsafe")
	}
	block, err := aes.NewCipher(derived[:32])
	if err != nil {
		cleanup()
		return authenticatedBackup{}, errors.New("initialize restore decryption")
	}
	stream := cipher.NewCTR(block, header[len(magic)+evidenceSize:])
	remaining := ciphertextSize
	buffer := make([]byte, 64*1024)
	for remaining > 0 {
		if err := ctx.Err(); err != nil {
			cleanup()
			return authenticatedBackup{}, errors.New("authenticate backup was canceled")
		}
		chunk := int64(len(buffer))
		if remaining < chunk {
			chunk = remaining
		}
		if _, err := io.ReadFull(file, buffer[:chunk]); err != nil {
			cleanup()
			return authenticatedBackup{}, errors.New("read encrypted backup")
		}
		ciphertext := buffer[:chunk]
		_, _ = mac.Write(ciphertext)
		_, _ = fileHash.Write(ciphertext)
		plaintext := make([]byte, len(ciphertext))
		stream.XORKeyStream(plaintext, ciphertext)
		if _, err := staging.Write(plaintext); err != nil {
			cleanup()
			return authenticatedBackup{}, errors.New("write private restore staging")
		}
		remaining -= chunk
	}
	tag := make([]byte, tagSize)
	if _, err := io.ReadFull(file, tag); err != nil {
		cleanup()
		return authenticatedBackup{}, errors.New("read backup authentication tag")
	}
	_, _ = fileHash.Write(tag)
	finalInfo, statErr := file.Stat()
	if statErr != nil || !safeBackupInode(finalInfo, headerSize) || finalInfo.Size() != info.Size() ||
		!os.SameFile(info, finalInfo) || !hmac.Equal(tag, mac.Sum(nil)) {
		cleanup()
		return authenticatedBackup{}, errors.New("backup authentication failed")
	}
	if err := staging.Sync(); err != nil {
		cleanup()
		return authenticatedBackup{}, errors.New("sync private restore staging")
	}
	if stagingInfo, statErr := staging.Stat(); statErr != nil || !safeStagingInode(stagingInfo, ciphertextSize) {
		cleanup()
		return authenticatedBackup{}, errors.New("private restore staging readback is unsafe")
	}
	if _, err := staging.Seek(0, io.SeekStart); err != nil {
		cleanup()
		return authenticatedBackup{}, errors.New("rewind private restore staging")
	}
	closeSource()
	return authenticatedBackup{file: staging, evidence: evidence,
		backupSHA256: hex.EncodeToString(fileHash.Sum(nil)), backupBytes: info.Size(), close: func() { _ = staging.Close() }}, nil
}

func safeBackupInode(info os.FileInfo, headerSize int) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && info.Mode().IsRegular() && info.Mode().Perm() == 0o600 &&
		info.Size() > int64(headerSize+tagSize) && stat.Nlink == 1 && int(stat.Uid) == os.Geteuid()
}

func safePrivateDirectory(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && info.IsDir() && info.Mode()&os.ModeSymlink == 0 &&
		info.Mode().Perm()&0o077 == 0 && int(stat.Uid) == os.Geteuid()
}

func safeStagingInode(info os.FileInfo, expectedSize int64) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && info.Mode().IsRegular() && info.Mode().Perm() == 0o600 &&
		info.Size() == expectedSize && stat.Nlink == 0 && int(stat.Uid) == os.Geteuid()
}

func readExisting(ctx context.Context, backupPath, manifestPath string, key []byte, planID, sourceSHA string,
	counts map[string]uint64, maximumPlaintextBytes int64,
) (Result, error) {
	proof, err := verifyRestore(ctx, backupPath, key, maximumPlaintextBytes)
	if err != nil {
		return Result{}, err
	}
	backupSHA, backupBytes := proof.backupSHA256, proof.backupBytes
	manifestBytes, err := readBounded(manifestPath, 1024*1024)
	if err != nil {
		return Result{}, errors.New("read existing backup manifest")
	}
	manifest, err := decodeManifest(manifestBytes)
	if err != nil || !validManifest(manifest) || manifest.PlanID != planID ||
		manifest.SourceSHA256 != sourceSHA || manifest.BackupSHA256 != backupSHA ||
		manifest.BackupBytes != backupBytes || manifest.SchemaVersion != "mattercodex.legacy-data-backup-manifest.v1" ||
		manifest.RestoreCheck != "pg_restore_list_verified" || !evidenceMatches(proof.evidence, manifest.SourceSHA256, manifest.TableCounts) ||
		counts != nil && !sameCounts(manifest.TableCounts, counts) {
		return Result{}, errors.New("existing backup manifest mismatch")
	}
	return Result{BackupPath: backupPath, ManifestPath: manifestPath,
		BackupSHA256: manifest.BackupSHA256, ManifestSHA256: digest(manifestBytes), BackupBytes: backupBytes}, nil
}

// LoadExisting проверяет immutable backup/manifest без обращения к source.
func LoadExisting(ctx context.Context, directory, planID, keyFile string,
	maximumPlaintextBytes int64,
) (Result, model.Manifest, error) {
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
	result, err := readExisting(ctx, backupPath, manifestPath, key, planID, manifest.SourceSHA256, nil,
		maximumPlaintextBytes)
	return result, manifest, err
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

func databaseEnvironment(dsn, tlsServerName, caFile string) ([]string, error) {
	environment, _, err := explicitDatabaseEnvironment(dsn, tlsServerName, caFile)
	return environment, err
}

func restoreEnvironment(dsn, tlsServerName, caFile string) ([]string, string, error) {
	return explicitDatabaseEnvironment(dsn, tlsServerName, caFile)
}

func explicitDatabaseEnvironment(dsn, tlsServerName, caFile string) ([]string, string, error) {
	parsed, err := url.Parse(dsn)
	if err != nil || parsed == nil || parsed.User == nil || parsed.Hostname() != tlsServerName ||
		(parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Fragment != "" {
		return nil, "", errors.New("database CLI configuration is invalid")
	}
	query := parsed.Query()
	if len(query["sslmode"]) != 1 || query.Get("sslmode") != "verify-full" {
		return nil, "", errors.New("database CLI TLS configuration is invalid")
	}
	for key, values := range query {
		if (key != "sslmode" && key != "sslrootcert" && key != "connect_timeout" && key != "application_name") ||
			len(values) != 1 {
			return nil, "", errors.New("database CLI routing configuration is invalid")
		}
	}
	if configuredCA := query.Get("sslrootcert"); configuredCA != "" && configuredCA != caFile {
		return nil, "", errors.New("database CLI CA configuration is invalid")
	}
	database := strings.TrimPrefix(parsed.EscapedPath(), "/")
	database, err = url.PathUnescape(database)
	if err != nil || database == "" || strings.Contains(database, "/") {
		return nil, "", errors.New("database CLI name is invalid")
	}
	password, passwordPresent := parsed.User.Password()
	if !passwordPresent || parsed.User.Username() == "" {
		return nil, "", errors.New("database CLI credentials are invalid")
	}
	port := parsed.Port()
	if port == "" {
		port = "5432"
	}
	environment := append(closedDatabaseEnvironment(),
		"PGHOST="+tlsServerName, "PGPORT="+port, "PGUSER="+parsed.User.Username(), "PGPASSWORD="+password,
		"PGDATABASE="+database, "PGSSLMODE=verify-full", "PGSSLROOTCERT="+caFile,
		"PGSSLMINPROTOCOLVERSION=TLSv1.3", "PGSSLMAXPROTOCOLVERSION=TLSv1.3")
	return environment, database, nil
}

func closedDatabaseEnvironment() []string {
	return []string{"LANG=C", "LC_ALL=C", "TZ=UTC"}
}

func verifyCLITransport(ctx context.Context, environment []string) error {
	command := exec.CommandContext(ctx, "psql", "--no-psqlrc", "--tuples-only", "--no-align", "--quiet",
		"--command=SELECT ssl::text || '|' || version FROM pg_catalog.pg_stat_ssl WHERE pid = pg_catalog.pg_backend_pid()")
	command.Env = environment
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = io.Discard
	if err := runSubprocess(command); err != nil || strings.TrimSpace(output.String()) != "true|TLSv1.3" {
		return errors.New("database CLI TLS readback is invalid")
	}
	return nil
}

func runSubprocess(command *exec.Cmd) error {
	command.Cancel = func() error {
		if command.Process == nil {
			return nil
		}
		err := command.Process.Signal(syscall.SIGTERM)
		if errors.Is(err, os.ErrProcessDone) {
			return nil
		}
		return err
	}
	command.WaitDelay = 5 * time.Second
	return command.Run()
}

func tableArguments() []string {
	arguments := make([]string, 0, len(inventory.Tables))
	for _, table := range inventory.Tables {
		arguments = append(arguments, "--table=public."+table)
	}
	return arguments
}

func validArchiveList(value string) bool {
	seen := make(map[string]bool, len(inventory.Tables))
	for _, line := range strings.Split(value, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || strings.HasPrefix(line, ";") {
			continue
		}
		publicIndex := -1
		for index, field := range fields {
			if field == "public" {
				publicIndex = index
				break
			}
		}
		if publicIndex < 0 {
			if len(fields) >= 5 && fields[4] == "-" &&
				(fields[3] == "ENCODING" || fields[3] == "STDSTRINGS" || fields[3] == "SEARCHPATH") {
				continue
			}
			return false
		}
		if publicIndex+1 >= len(fields) {
			continue
		}
		name := fields[publicIndex+1]
		object := strings.Join(fields[3:publicIndex], " ")
		if !archiveObjectAllowed(object, name) {
			return false
		}
		if inventory.Contains(name) {
			seen[name] = true
		}
	}
	for _, table := range inventory.Tables {
		if !seen[table] {
			return false
		}
	}
	return true
}

func archiveObjectAllowed(object, name string) bool {
	if inventory.Contains(name) {
		return true
	}
	if strings.HasPrefix(object, "SEQUENCE") && strings.HasSuffix(name, "_id_seq") &&
		inventory.Contains(strings.TrimSuffix(name, "_id_seq")) {
		return true
	}
	if object == "INDEX" || object == "INDEX ATTACH" {
		for _, table := range inventory.Tables {
			if strings.HasPrefix(name, table+"_") {
				return true
			}
		}
	}
	return false
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
