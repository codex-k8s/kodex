package platform

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/repositorybundle"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// bundlePolicyScanner is an isolated mandatory-scanner fixture: it hashes exact decoded bytes, can reject EICAR or simulate unavailable/corrupt evidence, and never contacts a real scanner.
// bundlePolicyScanner — isolated mandatory-scanner fixture: хеширует exact decoded bytes, отклоняет EICAR либо моделирует unavailable/corrupt evidence без реального сканера.
type bundlePolicyScanner struct {
	calls                int
	unavailable, corrupt bool
}

// ScanReader observes every decoded member and returns server-shaped exact evidence; the synthetic EICAR marker represents an antivirus rejection rather than a format verdict.
// ScanReader наблюдает каждый decoded member и возвращает server-shaped exact evidence; synthetic EICAR marker означает antivirus rejection, не format verdict.
func (scanner *bundlePolicyScanner) ScanReader(_ context.Context, reader io.Reader, size int64) (value.ArtifactScan, error) {
	scanner.calls++
	if scanner.unavailable {
		return value.ArtifactScan{}, errors.New("scanner unavailable")
	}
	raw, err := io.ReadAll(io.LimitReader(reader, size+1))
	if err != nil || int64(len(raw)) != size {
		return value.ArtifactScan{}, errors.New("scanner byte mismatch")
	}
	digest := "sha256:" + repositorybundle.Digest(raw)
	if scanner.corrupt {
		digest = "sha256:" + strings.Repeat("0", 64)
	}
	return value.ArtifactScan{Policy: value.ArtifactAntivirusPolicy, Engine: "ClamAV fixture", Digest: digest, SizeBytes: size, ScannedAt: time.Now().UTC(), Infected: strings.Contains(string(raw), "EICAR-STANDARD-ANTIVIRUS-TEST-FILE")}, nil
}

// TestRepositoryBundleDecodedContentPolicy proves that an outer valid JSON container cannot grant CLEAN to quarantined, malformed, infected or unscanned decoded members; successful receipts bind every actual file.
// TestRepositoryBundleDecodedContentPolicy доказывает, что outer valid JSON container не выдаёт CLEAN quarantined, malformed, infected либо unscanned decoded members; успешные receipts связывают каждый actual file.
func TestRepositoryBundleDecodedContentPolicy(t *testing.T) {
	for _, scenario := range []struct {
		name, path                     string
		body                           []byte
		rejected, unavailable, corrupt bool
	}{
		{name: "ordinary Go", path: "main.go", body: []byte("package main\nfunc main() {}\n")},
		{name: "SQLite schema source", path: "migrations/001.sql", body: []byte("CREATE TABLE item(id INTEGER PRIMARY KEY);\n")},
		{name: "Dockerfile source", path: "Dockerfile", body: []byte("FROM scratch\nCOPY server /server\n")},
		{name: "native direct JavaScript text", path: "source.js", body: []byte("export const answer = 42;\n")},
		{name: "HTML active member", path: "index.html", body: []byte("<!doctype html><p>active</p>")},
		{name: "SVG active member", path: "logo.svg", body: []byte("<svg xmlns=\"http://www.w3.org/2000/svg\"></svg>")},
		{name: "shebang script", path: "start.sh", body: []byte("#!/bin/sh\necho active\n")},
		{name: "MZ executable", path: "binary.dat", body: []byte("MZ executable"), rejected: true},
		{name: "ELF executable", path: "server.dat", body: []byte("\x7fELF executable"), rejected: true},
		{name: "SQLite binary database", path: "data.db", body: []byte("SQLite format 3\x00binary"), rejected: true},
		{name: "invalid UTF8", path: "bad.go", body: []byte{0xff, 0xfe}, rejected: true},
		{name: "decoded EICAR", path: "fixture.txt", body: []byte("EICAR-STANDARD-ANTIVIRUS-TEST-FILE"), rejected: true},
		{name: "HTML scanner unavailable", path: "index.html", body: []byte("<html>hello</html>"), rejected: true, unavailable: true},
		{name: "script scanner digest mismatch", path: "start.sh", body: []byte("#!/bin/sh\ntrue\n"), rejected: true, corrupt: true},
		{name: "infected active text", path: "index.html", body: []byte("<html>EICAR-STANDARD-ANTIVIRUS-TEST-FILE</html>"), rejected: true},
		{name: "malformed active UTF8", path: "index.html", body: []byte{0xff}, rejected: true},
		{name: "unavailable mandatory scanner", path: "main.go", body: []byte("package main\n"), rejected: true, unavailable: true},
		{name: "wrong scanner digest", path: "main.go", body: []byte("package main\n"), rejected: true, corrupt: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			files := []repositorybundle.File{{Entry: repositorybundle.Entry{Path: scenario.path, Mode: "100644"}, Bytes: scenario.body}}
			container, manifest, err := repositorybundle.CanonicalContainer(files)
			if err != nil {
				t.Fatal(err)
			}
			_, decoded, err := repositorybundle.Open(container, manifest)
			if err != nil {
				t.Fatal(err)
			}
			scanner := &bundlePolicyScanner{unavailable: scenario.unavailable, corrupt: scenario.corrupt}
			repository := &Repository{artifactScanner: scanner}
			receipts, err := repository.inspectRepositoryBundleFiles(t.Context(), decoded)
			if scenario.rejected {
				if err == nil || receipts != nil {
					t.Fatal("outer JSON bypassed decoded content policy")
				}
				return
			}
			if err != nil || len(receipts) != 1 || scanner.calls != 1 || receipts[0].Entry != decoded[0].Entry || receipts[0].Scan.Infected || receipts[0].MediaType == "" || receipts[0].Scan.Digest != "sha256:"+decoded[0].Entry.SHA256 {
				t.Fatal("decoded file evidence missing", err)
			}
		})
	}
	t.Run("no partial clean receipt", func(t *testing.T) {
		files := []repositorybundle.File{{Entry: repositorybundle.Entry{Path: "a.go", Mode: "100644"}, Bytes: []byte("package main\n")}, {Entry: repositorybundle.Entry{Path: "z.html", Mode: "100644"}, Bytes: []byte("MZ binary")}}
		container, manifest, err := repositorybundle.CanonicalContainer(files)
		if err != nil {
			t.Fatal(err)
		}
		_, decoded, err := repositorybundle.Open(container, manifest)
		if err != nil {
			t.Fatal(err)
		}
		scanner := &bundlePolicyScanner{}
		repository := &Repository{artifactScanner: scanner}
		if receipts, err := repository.inspectRepositoryBundleFiles(t.Context(), decoded); !errors.Is(err, errs.ErrForbidden) || receipts != nil || scanner.calls != 1 {
			t.Fatal("partial set admitted", err)
		}
	})
}

// TestRepositoryBundlePolicyRegistryPreservesFrozenReceipts keeps old snapshots readable while
// rejecting unknown policy, mismatched entries and missing antivirus; active media belong only
// to the new fixed Git-source profile, without rewriting historical snapshot identities.
// TestRepositoryBundlePolicyRegistryPreservesFrozenReceipts сохраняет чтение старых снимков,
// отклоняет неизвестную политику, подмену файлов и отсутствие антивируса; активные форматы
// принадлежат только новому профилю Git без переписывания идентификаторов старых снимков.
func TestRepositoryBundlePolicyRegistryPreservesFrozenReceipts(t *testing.T) {
	entry := repositorybundle.Entry{Path: "main.go", Mode: "100644", Bytes: 13, SHA256: repositorybundle.Digest([]byte("package main\n"))}
	receipt := repositoryBundleFileReceipt{Entry: entry, MediaType: "text/plain", Scan: value.ArtifactScan{
		Policy: value.ArtifactAntivirusPolicy, Engine: "ClamAV fixture", Digest: "sha256:" + entry.SHA256,
		SizeBytes: entry.Bytes, ScannedAt: time.Now().UTC(),
	}}
	for _, policy := range []string{repositoryBundleLegacyContentPolicy, repositoryBundleContentPolicy} {
		if !validRepositoryBundleFileReceipts(policy, []repositorybundle.Entry{entry}, []repositoryBundleFileReceipt{receipt}) {
			t.Fatal("known frozen policy rejected", policy)
		}
	}
	for _, policy := range []string{"", "unknown", repositoryBundleContentPolicy + "-future"} {
		if validRepositoryBundleFileReceipts(policy, []repositorybundle.Entry{entry}, []repositoryBundleFileReceipt{receipt}) {
			t.Fatal("unknown policy implicitly accepted")
		}
	}
	for _, mutation := range []string{"missing", "entry", "infected", "digest", "size", "policy", "engine", "time"} {
		t.Run(mutation, func(t *testing.T) {
			bad := receipt
			switch mutation {
			case "entry":
				bad.Entry.Path = "foreign.go"
			case "infected":
				bad.Scan.Infected = true
			case "digest":
				bad.Scan.Digest = "sha256:" + strings.Repeat("0", 64)
			case "size":
				bad.Scan.SizeBytes++
			case "policy":
				bad.Scan.Policy = "format-only"
			case "engine":
				bad.Scan.Engine = ""
			case "time":
				bad.Scan.ScannedAt = time.Time{}
			}
			receipts := []repositoryBundleFileReceipt{bad}
			if mutation == "missing" {
				receipts = nil
			}
			for _, policy := range []string{repositoryBundleLegacyContentPolicy, repositoryBundleContentPolicy} {
				if validRepositoryBundleFileReceipts(policy, []repositorybundle.Entry{entry}, receipts) {
					t.Fatal("invalid evidence accepted", policy)
				}
			}
		})
	}
	for _, media := range []string{"text/html", "image/svg+xml", "application/xhtml+xml"} {
		active := receipt
		active.MediaType = media
		if validRepositoryBundleFileReceipts(repositoryBundleLegacyContentPolicy, []repositorybundle.Entry{entry}, []repositoryBundleFileReceipt{active}) ||
			!validRepositoryBundleFileReceipts(repositoryBundleContentPolicy, []repositorybundle.Entry{entry}, []repositoryBundleFileReceipt{active}) {
			t.Fatal("active media admitted under wrong frozen profile", media)
		}
	}
	if _, err := (&Repository{}).inspectRepositoryBundleFiles(t.Context(), []repositorybundle.File{{Entry: entry, Bytes: []byte("package main\n")}}); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("missing mandatory scanner admitted source", err)
	}
}
