package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/pressly/goose/v3"
)

// integrationCohortCaptureStages selects the actual authored 046→049 sequence, without assigning expected catalog hashes or object counts.
// integrationCohortCaptureStages выбирает настоящую authored последовательность046→049 без назначения expected catalog hashes либо object counts.
var integrationCohortCaptureStages = []struct {
	name    string
	version int64
}{
	{"046", 20261114140000}, {"047", 20261114150000},
	{"048", 20261114160000}, {"049", 20261114170000},
}

// integrationCohortMigrationName admits only Goose SQL migration filenames; duplicate versions and source drift are rejected separately.
// integrationCohortMigrationName допускает только имена Goose SQL migrations; повтор versions и source drift проверяются отдельно.
var integrationCohortMigrationName = regexp.MustCompile(`^([0-9]{14})_[a-z0-9_]+\.sql$`)

// integrationCohortCaptureHash hashes exact artifact bytes; it never supplies an expected database fingerprint.
// integrationCohortCaptureHash хеширует точные artifact bytes и не назначает expected database fingerprint.
func integrationCohortCaptureHash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// integrationCohortCaptureRoot retains a descriptor for the caller's private owned directory, rejecting symlink roots and directory replacement before opening it.
// integrationCohortCaptureRoot удерживает descriptor приватного owned каталога caller, отвергая symlink roots и подмену каталога перед открытием.
func integrationCohortCaptureRoot(directory string) (*os.Root, error) {
	before, err := os.Lstat(directory)
	if err != nil || !filepath.IsAbs(directory) || before == nil || !before.IsDir() || before.Mode().Perm() != 0700 {
		return nil, errors.New("capture directory must be absolute, owned and private")
	}
	owner, ok := before.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Geteuid()) {
		return nil, errors.New("capture directory owner differs")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, errors.New("open private capture directory")
	}
	after, err := root.Stat(".")
	if err != nil || !os.SameFile(before, after) || after.Mode().Perm() != 0700 {
		_ = root.Close()
		return nil, errors.New("capture directory identity changed")
	}
	return root, nil
}

// integrationCohortCaptureWrite creates exactly one new regular 0600 JSON artifact under the retained directory descriptor. Existing files, symlinks and path traversal never become overwrite or resume authority.
// integrationCohortCaptureWrite создаёт ровно один новый regular0600 JSON artifact под retained directory descriptor. Existing files, symlinks и traversal не становятся полномочием overwrite либо resume.
func integrationCohortCaptureWrite(root *os.Root, name string, raw []byte) error {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsAny(name, `/\`) || !utf8.Valid(raw) || !json.Valid(raw) {
		return errors.New("capture artifact name or JSON is invalid")
	}
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("capture artifact must be new")
	}
	info, statErr := file.Stat()
	if statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		_ = file.Close()
		return errors.New("capture artifact boundary differs")
	}
	written, writeErr := file.Write(raw)
	syncErr := file.Sync()
	closeErr := file.Close()
	if written != len(raw) || writeErr != nil || syncErr != nil || closeErr != nil {
		return errors.New("persist capture artifact")
	}
	directory, err := root.Open(".")
	if err != nil {
		return errors.New("open capture directory for synchronization")
	}
	syncErr, closeErr = directory.Sync(), directory.Close()
	if syncErr != nil || closeErr != nil {
		return errors.New("persist capture directory entries")
	}
	return nil
}

// integrationCohortCaptureSources pins every compiled SQL migration through the observed head and requires exact equality with the frozen checkout; stale test binaries cannot capture a different migration source.
// integrationCohortCaptureSources закрепляет каждую compiled SQL migration до observed head и требует exact equality с frozen checkout; stale test binary не захватывает другой migration source.
func integrationCohortCaptureSources(t *testing.T, head int64) []map[string]any {
	t.Helper()
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		t.Fatal("read compiled cohort migrations")
	}
	result := make([]map[string]any, 0)
	seen := make(map[int64]bool)
	for _, entry := range entries {
		match := integrationCohortMigrationName.FindStringSubmatch(entry.Name())
		if entry.IsDir() || len(match) != 2 {
			t.Fatal("unrecognized cohort migration source")
		}
		version, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil || seen[version] {
			t.Fatal("duplicate or invalid cohort migration version")
		}
		seen[version] = true
		if version > head {
			continue
		}
		name := "migrations/" + entry.Name()
		compiled, err := migrations.ReadFile(name)
		if err != nil {
			t.Fatal("read compiled cohort migration")
		}
		current, err := os.ReadFile(name)
		if err != nil || !bytes.Equal(compiled, current) {
			t.Fatal("compiled cohort migration differs from checkout")
		}
		result = append(result, map[string]any{"version": version, "file": entry.Name(), "sha256": integrationCohortCaptureHash(compiled)})
	}
	if !seen[head] {
		t.Fatal("cohort migration head is absent")
	}
	return result
}

// integrationCohortCaptureSource verifies the public entrypoint's source SHA against the current clean checkout without fetching remote refs or reading authentication material.
// integrationCohortCaptureSource сверяет source SHA публичной точки входа с текущим clean checkout без fetch remote refs либо чтения authentication material.
func integrationCohortCaptureSource(t *testing.T) string {
	t.Helper()
	wanted := os.Getenv("KODEX_INTEGRATION_COHORT_CAPTURE_SOURCE_SHA")
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(wanted) {
		t.Fatal("cohort capture requires an exact source commit")
	}
	head, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil || strings.TrimSpace(string(head)) != wanted {
		t.Fatal("cohort capture source commit changed")
	}
	status, err := exec.Command("git", "status", "--porcelain", "--untracked-files=no").Output()
	if err != nil || len(status) != 0 {
		t.Fatal("cohort capture requires clean tracked source")
	}
	return wanted
}

// integrationCohortCaptureHistory requires one successful ordered Goose record per compiled source version, allowing only the optional initial zero row. Missing, replayed, down or foreign records cannot be sealed by a matching max head alone.
// integrationCohortCaptureHistory требует одну успешную упорядоченную Goose record каждой compiled source version, допуская только optional initial zero row. Missing, replayed, down либо foreign records не допускаются одним совпадением max head.
func integrationCohortCaptureHistory(raw json.RawMessage, pins []map[string]any) bool {
	var rows [][]json.RawMessage
	if json.Unmarshal(raw, &rows) != nil {
		return false
	}
	expected, seen := make(map[int64]bool), make(map[int64]bool)
	for _, pin := range pins {
		version, ok := pin["version"].(int64)
		if !ok || version <= 0 || expected[version] {
			return false
		}
		expected[version] = true
	}
	var previous int64
	for _, row := range rows {
		var id, version int64
		var applied bool
		if len(row) != 3 || json.Unmarshal(row[0], &id) != nil || json.Unmarshal(row[1], &version) != nil || json.Unmarshal(row[2], &applied) != nil || !applied || id <= previous || seen[version] || version == 0 && previous != 0 || version != 0 && !expected[version] {
			return false
		}
		previous, seen[version] = id, true
	}
	for version := range expected {
		if !seen[version] {
			return false
		}
	}
	return len(expected) != 0
}

// TestIntegrationCohortCatalogCapture materializes actual schema-only 046/047/048/049 catalogs in one fresh disposable database for exactly one backup mode. It reuses the canonical SQL and backup initializer, emits exclusive private files and seals completion only after all migration/head/source checks pass. It is capture, never release qualification or an expected-hash generator.
// TestIntegrationCohortCatalogCapture материализует actual schema-only каталоги046/047/048/049 в одной fresh disposable БД для ровно одного backup mode. Переиспользует canonical SQL и backup initializer, создаёт exclusive private files и завершает seal только после всех migration/head/source checks. Это capture, не release qualification и не generator expected hashes.
func TestIntegrationCohortCatalogCapture(t *testing.T) {
	directory := os.Getenv("KODEX_INTEGRATION_COHORT_CAPTURE_DIRECTORY")
	if directory == "" {
		t.Skip("private integration cohort capture is not requested")
	}
	mode := os.Getenv("KODEX_INTEGRATION_COHORT_CAPTURE_MODE")
	if mode != "normal" && mode != "backup" && mode != "table-only-backup" || os.Getenv("KODEX_TEST_POSTGRES_STORAGE") != "ram" || os.Getenv("KODEX_INTEGRATION_COHORT_CAPTURE") != "1" {
		t.Fatal("cohort capture requires one closed mode and the official RAM fixture")
	}
	source := integrationCohortCaptureSource(t)
	root, err := integrationCohortCaptureRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error("close private cohort capture directory")
		}
	})
	query, err := os.ReadFile("../../../../../tools/release/managed-kubernetes-catalog.sql")
	if err != nil {
		t.Fatal("read canonical cohort catalog query")
	}
	selector := strings.LastIndex(string(query), "SELECT jsonb_build_object(\n")
	if selector < 0 || !strings.HasSuffix(strings.TrimSpace(string(query)), "FROM complete_catalog;") {
		t.Fatal("canonical complete catalog selector changed")
	}
	fullQuery := string(query[:selector]) + "SELECT content::text FROM complete_catalog;"
	legacyReader, err := os.ReadFile("../../../../../tools/release/telegram-notifications-restore-catalog.sql")
	if err != nil {
		t.Fatal("read unchanged legacy restore catalog query")
	}
	initializer := canonicalGitHubBackupInitializer(t)
	modeACL := ""
	if mode == "backup" {
		modeACL = initializer
	} else if mode == "table-only-backup" {
		modeACL = "SET ROLE control_plane_owner; GRANT SELECT ON ALL TABLES IN SCHEMA control_plane,public TO kodex_backup_reader; ALTER DEFAULT PRIVILEGES FOR ROLE control_plane_owner IN SCHEMA control_plane GRANT SELECT ON TABLES TO kodex_backup_reader; RESET ROLE"
	}
	headQuery, err := os.ReadFile("testdata/sql/integration_cohort_goose_head.sql")
	if err != nil {
		t.Fatal("read cohort Goose seal query")
	}
	ctx, database, observer := newWorkflowUpgradeDatabase(t)
	files := make([]map[string]any, 0)
	for index, stage := range integrationCohortCaptureStages {
		pins := integrationCohortCaptureSources(t, stage.version)
		if err := goose.UpToContext(ctx, database, "migrations", stage.version); err != nil {
			t.Fatal("apply exact cohort migration source", err)
		}
		if index == 0 && mode != "normal" {
			if _, err := observer.Exec(ctx, modeACL); err != nil {
				t.Fatal("apply exact cohort backup fixture ACL", err)
			}
		}
		tx, err := observer.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if err != nil {
			t.Fatal("begin readonly cohort capture")
		}
		if _, err := tx.Exec(ctx, "SET LOCAL search_path=pg_catalog; SET LOCAL statement_timeout='15s'; SET LOCAL lock_timeout='1s'"); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatal("bound cohort catalog transaction")
		}
		var summary, normalized, head json.RawMessage
		var catalog []byte
		for _, read := range []struct {
			query string
			raw   any
		}{{string(query), &summary}, {fullQuery, &catalog}, {string(legacyReader), &normalized}, {string(headQuery), &head}} {
			if err := tx.QueryRow(ctx, read.query).Scan(read.raw); err != nil {
				_ = tx.Rollback(ctx)
				t.Fatal("read cohort schema metadata", err)
			}
		}
		var sealHead struct {
			Head             int64           `json:"head"`
			History          json.RawMessage `json:"history"`
			ServerVersionNum string          `json:"serverVersionNum"`
		}
		if json.Unmarshal(head, &sealHead) != nil || sealHead.Head != stage.version || sealHead.ServerVersionNum != "180003" || !integrationCohortCaptureHistory(sealHead.History, pins) {
			_ = tx.Rollback(ctx)
			t.Fatal("cohort Goose head differs from selected stage")
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal("commit readonly cohort metadata snapshot")
		}
		var summaryFields map[string]json.RawMessage
		var restore struct {
			CatalogDigest string `json:"catalogDigest"`
		}
		var catalogDigest string
		if json.Unmarshal(summary, &summaryFields) != nil || json.Unmarshal(normalized, &restore) != nil ||
			json.Unmarshal(summaryFields["catalogDigest"], &catalogDigest) != nil || catalogDigest != integrationCohortCaptureHash(catalog) ||
			!regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(restore.CatalogDigest) {
			t.Fatal("raw content text or legacy restore digest differs")
		}
		summaryFields["restoreCatalogDigest"], _ = json.Marshal(restore.CatalogDigest)
		summaryFields["backupInitializerSHA256"], _ = json.Marshal(integrationCohortCaptureHash([]byte(initializer)))
		summary, err = json.Marshal(summaryFields)
		if err != nil {
			t.Fatal("encode observed cohort summary")
		}
		var compactHistory bytes.Buffer
		if err := json.Compact(&compactHistory, sealHead.History); err != nil {
			t.Fatal("canonicalize observed Goose history")
		}
		canonicalHead, err := json.Marshal(map[string]any{"head": sealHead.Head, "history": json.RawMessage(compactHistory.Bytes()), "serverVersionNum": sealHead.ServerVersionNum})
		if err != nil {
			t.Fatal("canonicalize observed Goose seal")
		}
		prefix := mode + "-" + stage.name
		seal := map[string]any{
			"schema": "kodex.integration-cohort-capture.v1", "purpose": "SCHEMA_CAPTURE_ONLY",
			"sourceSHA": source, "mode": mode, "stage": stage.name,
			"gooseHead": json.RawMessage(canonicalHead), "gooseSerialization": "COMPACT_SORTED_KEYS_JSON_V1",
			"gooseHistorySHA256": integrationCohortCaptureHash(compactHistory.Bytes()), "gooseSealSHA256": integrationCohortCaptureHash(canonicalHead),
			"migrations": pins, "catalogReaderSHA256": integrationCohortCaptureHash(query),
			"legacyRestoreCatalogReaderSHA256": integrationCohortCaptureHash(legacyReader), "legacyRestoreCatalogDigest": restore.CatalogDigest,
			"fullCatalogProjectionSHA256":      integrationCohortCaptureHash([]byte(fullQuery)),
			"canonicalBackupInitializerSHA256": integrationCohortCaptureHash([]byte(initializer)),
			"modeInitializerSHA256":            integrationCohortCaptureHash([]byte(modeACL)), "canonicalBackupInitializerApplied": mode == "backup",
			"summarySHA256": integrationCohortCaptureHash(summary), "catalogSHA256": integrationCohortCaptureHash(catalog),
		}
		sealRaw, err := json.MarshalIndent(seal, "", "  ")
		if err != nil {
			t.Fatal("encode cohort capture seal")
		}
		for _, artifact := range []struct {
			name string
			raw  []byte
		}{{prefix + ".summary.json", summary}, {prefix + ".catalog.json", catalog}, {prefix + ".seal.json", sealRaw}} {
			if err := integrationCohortCaptureWrite(root, artifact.name, artifact.raw); err != nil {
				t.Fatal(err)
			}
			files = append(files, map[string]any{"file": artifact.name, "sha256": integrationCohortCaptureHash(artifact.raw), "bytes": len(artifact.raw)})
		}
		if integrationCohortCaptureSource(t) != source || !bytes.Equal(query, mustReadIntegrationCohortCatalog(t)) {
			t.Fatal("cohort capture source changed before completion")
		}
	}
	complete, err := json.MarshalIndent(map[string]any{"schema": "kodex.integration-cohort-capture.v1", "purpose": "SCHEMA_CAPTURE_ONLY", "sourceSHA": source, "mode": mode, "sealedGooseHead": integrationCohortCaptureStages[len(integrationCohortCaptureStages)-1].version, "files": files}, "", "  ")
	if err != nil || integrationCohortCaptureWrite(root, mode+".complete.json", complete) != nil {
		t.Fatal("seal complete cohort capture")
	}
}

// mustReadIntegrationCohortCatalog rechecks the exact shared reader bytes before sealing, without generating or changing the reader.
// mustReadIntegrationCohortCatalog повторно проверяет exact shared reader bytes перед seal без генерации либо изменения reader.
func mustReadIntegrationCohortCatalog(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../../../../tools/release/managed-kubernetes-catalog.sql")
	if err != nil {
		t.Fatal("reread canonical cohort catalog")
	}
	return raw
}

// TestIntegrationCohortCaptureOutputCustody checks private directory identity, exclusive artifact creation, exact modes and rejection of symlink/traversal overwrites without PostgreSQL or network access.
// TestIntegrationCohortCaptureOutputCustody проверяет private directory identity, exclusive artifact creation, exact modes и отказ symlink/traversal overwrite без PostgreSQL либо network access.
func TestIntegrationCohortCaptureOutputCustody(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "capture")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := integrationCohortCaptureRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	}()
	const name = "normal-046.summary.json"
	raw := []byte(`{"capture":true}`)
	if err := integrationCohortCaptureWrite(root, name, raw); err != nil {
		t.Fatal(err)
	}
	for _, rejected := range []string{name, "../outside.json", "/absolute.json", "", "."} {
		if integrationCohortCaptureWrite(root, rejected, []byte(`{"changed":true}`)) == nil {
			t.Fatal("capture overwrote or escaped its private output")
		}
	}
	current, err := os.ReadFile(filepath.Join(directory, name))
	info, statErr := os.Lstat(filepath.Join(directory, name))
	if err != nil || statErr != nil || !bytes.Equal(current, raw) || info.Mode().Perm() != 0600 || !info.Mode().IsRegular() {
		t.Fatal("capture artifact bytes or custody changed")
	}
	link := filepath.Join(filepath.Dir(directory), "capture-link")
	if err := os.Symlink(directory, link); err != nil {
		t.Fatal(err)
	}
	if _, err := integrationCohortCaptureRoot(link); err == nil {
		t.Fatal("capture accepted a directory symlink")
	}
	if err := os.Chmod(directory, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := integrationCohortCaptureRoot(directory); err == nil {
		t.Fatal("capture accepted nonprivate output")
	}
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := root.Symlink(name, "existing-link.json"); err != nil {
		t.Fatal(fmt.Errorf("create custody counterexample: %w", err))
	}
	if integrationCohortCaptureWrite(root, "existing-link.json", raw) == nil {
		t.Fatal("capture accepted an existing symlink artifact")
	}
}

// TestIntegrationCohortCaptureHistorySeal rejects max-head-only history substitutes without PostgreSQL or authored catalog expectations.
// TestIntegrationCohortCaptureHistorySeal отклоняет подмены history при совпадающем max head без PostgreSQL либо authored catalog expectations.
func TestIntegrationCohortCaptureHistorySeal(t *testing.T) {
	pins := []map[string]any{{"version": int64(46)}, {"version": int64(47)}}
	for _, raw := range []string{`[[1,0,true],[2,46,true],[3,47,true]]`, `[[1,46,true],[2,47,true]]`} {
		if !integrationCohortCaptureHistory(json.RawMessage(raw), pins) {
			t.Fatal("exact source history was rejected")
		}
	}
	for _, raw := range []string{`[[1,46,true],[2,0,true],[3,47,true]]`, `[[1,47,true]]`, `[[1,46,true],[2,47,false]]`, `[[1,46,true],[2,47,true],[3,47,true]]`, `[[2,46,true],[1,47,true]]`, `[[1,45,true],[2,46,true],[3,47,true]]`, `null`} {
		if integrationCohortCaptureHistory(json.RawMessage(raw), pins) {
			t.Fatal("incomplete or foreign history acquired a capture seal")
		}
	}
}
