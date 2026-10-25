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
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// queryArtifactAntivirusUnprovenContent embeds a disposable-only negative content insertion using
// a caller-supplied antivirus report on an existing synthetic artifact. Production proof
// constraints are expected to reject the tested absent/forged reports; the statement must not run
// against live data.
// queryArtifactAntivirusUnprovenContent встраивает disposable-only negative content insertion с
// caller-supplied antivirus report существующего synthetic artifact. Production proof constraints
// должны отклонить tested absent/forged reports; statement не запускается на live data.
//
//go:embed testdata/sql/artifact_antivirus_unproven_content.sql
var queryArtifactAntivirusUnprovenContent string

// componentArtifactScanner is a synthetic scanner with injected error, infection and
// digest-mismatch switches for local/component tests. Its ClamAV-looking metadata is fixture data
// and must never stand in for production scanner composition or real AV acceptance.
// componentArtifactScanner является synthetic scanner с injected error, infection и
// digest-mismatch switches для local/component tests. ClamAV-looking metadata — fixture data, не
// заменяющая production scanner composition либо real AV acceptance.
type componentArtifactScanner struct {
	err                error
	infected, mismatch bool
}

// ScanReader returns injected failure first, checks initial context, then hashes at most size+1
// bytes and requires exact length before constructing synthetic report metadata. The mismatch flag
// replaces the digest and infected flag only sets verdict input; no antivirus daemon is contacted
// or cancellation polled during copying.
// ScanReader сначала возвращает injected failure, проверяет initial context, затем хеширует не
// более size+1 bytes и требует exact length до synthetic report metadata. Mismatch flag заменяет
// digest, infected flag задаёт verdict input; antivirus daemon не вызывается, cancellation во
// время copying не опрашивается.
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

// configureComponentArtifactScanner marks its test frame and installs the default synthetic
// scanner, failing the test on configuration error. It is test-only composition and proves no real
// antivirus service readiness.
// configureComponentArtifactScanner помечает test frame и устанавливает default synthetic scanner
// с test failure при configuration error. Это test-only composition без доказательства real
// antivirus service readiness.
func configureComponentArtifactScanner(t *testing.T, repository *Repository) {
	t.Helper()
	if err := repository.ConfigureArtifactScanner(componentArtifactScanner{}); err != nil {
		t.Fatal(err)
	}
}

// TestArtifactAntivirusReaderBoundary covers missing/unavailable scanner, mismatched bytes, clean
// and quarantined reports, requiring reader rewind and exact receipt round-trip for success. An
// empty historical receipt must fail; synthetic hashes/verdicts do not certify a live AV scan.
// TestArtifactAntivirusReaderBoundary проверяет missing/unavailable scanner, mismatched bytes,
// clean и quarantined reports с reader rewind и exact receipt round-trip при success. Empty
// historical receipt отклоняется; synthetic hashes/verdicts не подтверждают live AV scan.
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

// testArtifactAntivirus when invoked by disposable fixtures checks scanner admission, quarantine
// download denial, clean readback, rejection of unproven stored content and legacy quarantined
// binding cleanup/replay without new binding admission. It also reprojects historical result/event
// data to blocked scan state; the helper restores synthetic scanner configuration through cleanup
// and is not a live AV acceptance run.
// testArtifactAntivirus при вызове disposable fixtures проверяет scanner admission, quarantine
// download denial, clean readback, отказ unproven stored content и legacy quarantined binding
// cleanup/replay без нового binding admission. Также заново проецирует historical result/event
// data в blocked scan state; cleanup восстанавливает synthetic scanner configuration, helper не
// является live AV acceptance.
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
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "av-legacy-project"}, Payload: command.ProjectInput{Name: "AV legacy recovery", Language: "en"}})
	if err != nil {
		t.Fatal(err)
	}
	projectID := gateTestProjectID(t, ctx, repository, owner, project.Project.Ref)
	err = repository.pool.QueryRow(ctx, queryArtifactsUploadartifactInsertArtifactsRefProjectIdFileName,
		ref, current.organizationID, projectID, "", "unverified.txt", "text/plain", int64(3), item.Digest,
		"CLEAN", "obj_unproven_component", "AVAILABLE", current.actorID).Scan(
		&unverified.Ref, &unverified.FileName, &unverified.MediaType, &unverified.SizeBytes, &unverified.Digest,
		&unverified.ScanState, &unverified.PreviewState, &unverified.Revision, &unverified.Version, &unverified.CreatedAt, &unverified.CurrentRevisionRef)
	if err != nil {
		t.Fatal(err)
	}
	historical := unverified
	unverified, err = service.GetArtifact(ctx, owner, ref)
	if err != nil || unverified.ScanState != "QUARANTINED" || unverified.PreviewState != "BLOCKED" {
		t.Fatalf("unverified revision was admitted: %s %v", unverified.ScanState, err)
	}
	for _, invalid := range [][]byte{nil, []byte(`{}`), []byte(`{"policy":"clamav-instream-v1","verdict":"CLEAN"}`)} {
		if _, err := repository.pool.Exec(ctx, queryArtifactAntivirusUnprovenContent, ref, invalid); err == nil {
			t.Fatal("missing or forged proof accepted")
		}
	}
	// Существующую до cutover связь моделирует только disposable fixture;
	// снять её пользователь должен обычной owner-командой без допуска bytes.
	agent := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "av-legacy-agent", "Legacy knowledge")
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var artifactID, lockedProjectID string
	var version int64
	if err := tx.QueryRow(ctx, queryArtifactRevisionCreateLock, current.organizationID, ref).Scan(&artifactID, &lockedProjectID, &version); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, queryArtifactsChangeartifactbindingInsertArtifactBindingsArtifactIdTargetRef, artifactID, current.organizationID, current.actorID, projectID, agent.Ref); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, queryArtifactsChangeartifactbindingUpdateArtifactsVersion, artifactID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	targets, err := service.ListArtifactBindingTargets(ctx, owner, ref, query.Filter{})
	if err != nil || len(targets.Items) != 1 || !targets.Items[0].CanUnbind || targets.Items[0].CanBind {
		t.Fatal("quarantine binding cleanup unavailable", err)
	}
	unverified, err = service.GetArtifact(ctx, owner, ref)
	if err != nil {
		t.Fatal(err)
	}
	bind := command.Command{Kind: command.ChangeArtifactBinding, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "av-no-new-binding", ExpectedVersion: &unverified.Version},
		Payload: command.ArtifactBindingInput{ArtifactRef: ref, AgentRef: agent.Ref, Enabled: true}}
	if _, err := service.Execute(ctx, bind); err == nil {
		t.Fatal("quarantine granted a new binding")
	}
	bind.Mutation.IdempotencyKey = "av-remove-old-binding"
	bind.Payload = command.ArtifactBindingInput{ArtifactRef: ref, AgentRef: agent.Ref, Enabled: false}
	for range 2 {
		if _, err := service.Execute(ctx, bind); err != nil {
			t.Fatal("quarantine unbind/replay failed", err)
		}
	}
	tx, err = repository.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	eventArtifact := historical
	event := entity.RunEvent{}
	event.Delta.Artifact = &eventArtifact
	if err := projectArtifactResults(ctx, tx, current, &command.Result{Artifact: &historical, Event: &event}); err != nil {
		t.Fatal(err)
	}
	if historical.ScanState != "QUARANTINED" || historical.PreviewState != "BLOCKED" || eventArtifact.ScanState != "QUARANTINED" || eventArtifact.PreviewState != "BLOCKED" {
		t.Fatal("old aggregate version retained format-only CLEAN/preview")
	}
}
