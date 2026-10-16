package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

//go:embed testdata/sql/artifact_antivirus_unproven_content.sql
var queryArtifactAntivirusUnprovenContent string

// Только синтетическая оснастка PostgreSQL/unit. Не доказывает реальный AV PASS
// и не используется production composition root.
type componentArtifactScanner struct {
	err                error
	infected, mismatch bool
}

func (scanner componentArtifactScanner) ScanReader(ctx context.Context, reader io.Reader, size int64) (value.ArtifactScan, error) {
	if scanner.err != nil {
		return value.ArtifactScan{}, scanner.err
	}
	if ctx.Err() != nil {
		return value.ArtifactScan{}, ctx.Err()
	}
	digest := sha256.New()
	read, err := io.Copy(digest, io.LimitReader(reader, size+1))
	if err != nil || read != size {
		return value.ArtifactScan{}, errs.ErrInvalid
	}
	hexDigest := hex.EncodeToString(digest.Sum(nil))
	if scanner.mismatch {
		hexDigest = strings.Repeat("a", 64)
	}
	return value.ArtifactScan{Engine: "ClamAV synthetic/1/fixture", Policy: value.ArtifactAntivirusPolicy,
		Digest: "sha256:" + hexDigest, SizeBytes: size, ScannedAt: time.Now().UTC(), Infected: scanner.infected}, nil
}

func configureComponentArtifactScanner(t *testing.T, repository *Repository) {
	t.Helper()
	if err := repository.ConfigureArtifactScanner(componentArtifactScanner{}); err != nil {
		t.Fatal(err)
	}
}

func TestArtifactAntivirusReaderBoundary(t *testing.T) {
	digest := "sha256:ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	for _, test := range []struct {
		name    string
		scanner *componentArtifactScanner
		want    error
	}{
		{"missing", nil, errs.ErrUnavailable},
		{"unavailable", &componentArtifactScanner{err: io.ErrUnexpectedEOF}, errs.ErrUnavailable},
		{"different bytes", &componentArtifactScanner{mismatch: true}, errs.ErrConflict},
		{"clean", &componentArtifactScanner{}, nil},
		{"quarantined", &componentArtifactScanner{infected: true}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := &Repository{}
			if test.scanner != nil {
				r.artifactScanner = *test.scanner
			}
			reader := strings.NewReader("abc")
			report, err := r.scanArtifact(t.Context(), reader, digest, 3)
			if !errors.Is(err, test.want) {
				t.Fatalf("unexpected scan boundary: %v", err)
			}
			if err == nil {
				position, _ := reader.Seek(0, io.SeekCurrent)
				if position != 0 || report.Infected != test.scanner.infected {
					t.Fatal("scan lost exact reader/verdict")
				}
				raw, err := artifactAntivirusJSON(report)
				if err != nil {
					t.Fatal(err)
				}
				decoded, err := decodeArtifactAntivirus(raw, digest, 3)
				if err != nil || decoded != report {
					t.Fatal("receipt changed provenance", err)
				}
			}
		})
	}
	if _, err := decodeArtifactAntivirus([]byte(`{}`), digest, 3); err == nil {
		t.Fatal("historical unknown receipt admitted")
	}
}

func testArtifactAntivirus(t *testing.T, ctx context.Context, repository *Repository) {
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.artifacts.upload"}, "control-api-gateway")
	upload := func(key string) (string, error) {
		item, err := service.UploadArtifact(ctx, owner, value.Mutation{IdempotencyKey: key}, platformrepo.ArtifactUpload{
			FileName: "material.txt", MediaType: "text/plain", SizeBytes: 3, Reader: strings.NewReader("abc")})
		return item.Ref, err
	}
	t.Cleanup(func() { configureComponentArtifactScanner(t, repository) })
	repository.artifactScanner = nil
	if _, err := upload("av-missing"); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatalf("missing scanner: %v", err)
	}
	repository.artifactScanner = componentArtifactScanner{mismatch: true}
	if _, err := upload("av-mismatch"); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("mismatched bytes: %v", err)
	}
	repository.artifactScanner = componentArtifactScanner{infected: true}
	blocked, err := upload("av-quarantine")
	if err != nil {
		t.Fatal(err)
	}
	item, err := service.GetArtifact(ctx, owner, blocked)
	if err != nil || item.ScanState != "QUARANTINED" {
		t.Fatalf("quarantine readback: %s %v", item.ScanState, err)
	}
	if download, err := service.DownloadArtifact(ctx, owner, blocked, "DOWNLOAD"); err == nil {
		download.Reader.Close()
		t.Fatal("quarantine downloaded")
	}
	configureComponentArtifactScanner(t, repository)
	clean, err := upload("av-clean")
	if err != nil {
		t.Fatal(err)
	}
	item, err = service.GetArtifact(ctx, owner, clean)
	if err != nil || item.ScanState != "CLEAN" {
		t.Fatalf("clean readback: %s %v", item.ScanState, err)
	}
	download, err := service.DownloadArtifact(ctx, owner, clean, "DOWNLOAD")
	if err != nil {
		t.Fatal(err)
	}
	download.Reader.Close()

	// Старое format-only состояние без proof не становится допущенным даже
	// при наличии сырой CLEAN revision. Данные не переписываются при чтении.
	principal, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := newRef("art")
	if err != nil {
		t.Fatal(err)
	}
	var unverified entity.Artifact
	err = repository.pool.QueryRow(ctx, queryArtifactsUploadartifactInsertArtifactsRefProjectIdFileName,
		ref, current.organizationID, "", "", "unverified.txt", "text/plain", int64(3), item.Digest,
		"CLEAN", "obj_unproven_component", "AVAILABLE", current.actorID).Scan(
		&unverified.Ref, &unverified.FileName, &unverified.MediaType, &unverified.SizeBytes, &unverified.Digest,
		&unverified.ScanState, &unverified.PreviewState, &unverified.Revision, &unverified.Version, &unverified.CreatedAt, &unverified.CurrentRevisionRef)
	if err != nil {
		t.Fatal(err)
	}
	unverified, err = service.GetArtifact(ctx, owner, ref)
	if err != nil || unverified.ScanState != "QUARANTINED" {
		t.Fatalf("unverified revision was admitted: %s %v", unverified.ScanState, err)
	}
	for _, invalid := range [][]byte{nil, []byte(`{}`), []byte(`{"policy":"clamav-instream-v1","verdict":"CLEAN"}`)} {
		if _, err := repository.pool.Exec(ctx, queryArtifactAntivirusUnprovenContent, ref, invalid); err == nil {
			t.Fatal("missing or forged proof accepted")
		}
	}
}
