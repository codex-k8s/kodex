package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed testdata/sql/artifact_preview_effect_counts.sql
var queryPreviewEffectCounts string

type previewTrackedStore struct {
	objectstorage.Store
	gets, closes atomic.Int64
}

func (store *previewTrackedStore) Get(ctx context.Context, key, version string) (objectstorage.Object, error) {
	object, err := store.Store.Get(ctx, key, version)
	if err == nil {
		store.gets.Add(1)
		object.Body = &previewTrackedBody{ReadCloser: object.Body, store: store}
	}
	return object, err
}

type previewTrackedBody struct {
	io.ReadCloser
	store *previewTrackedStore
}

func (body *previewTrackedBody) Close() error {
	body.store.closes.Add(1)
	return body.ReadCloser.Close()
}

type previewTraceContextKey struct{}
type previewTraceEvent struct{ identity, state string }
type previewParallelTrace struct {
	mu      sync.Mutex
	events  []previewTraceEvent
	barrier chan struct{}
	arrived int
}

// SQL и аргументы не сохраняются: tracer распознаёт только закрытые identities.
func previewQueryIdentity(sql string) string {
	for _, identity := range []string{
		"artifacts_downloadartifact_select_artifact_for_grant",
		"queries_getartifact_select_artifact_bindings_artifact_id_id_organization_id",
		"artifact_effective_actions", "artifact_revision_get",
		"artifacts_downloadartifact_insert_download_grant",
		"artifacts_downloadartifact_consume_download_grant",
		"artifacts_downloadartifact_select_artifact_content",
		"artifacts_downloadartifact_insert_audit_event",
	} {
		if strings.HasPrefix(sql, "-- name: "+identity+" ") {
			return identity
		}
	}
	if strings.EqualFold(sql, "commit") {
		return "commit"
	}
	return "OTHER"
}

func (trace *previewParallelTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, previewTraceContextKey{}, previewQueryIdentity(data.SQL))
}

func (trace *previewParallelTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	identity, _ := ctx.Value(previewTraceContextKey{}).(string)
	state := "NONE"
	if data.Err != nil {
		state = "OTHER"
		var cause *pgconn.PgError
		if errors.As(data.Err, &cause) && (cause.Code == "40001" || cause.Code == "40P01" || cause.Code == "23505" || cause.Code == "57014") {
			state = cause.Code
		}
	}
	trace.mu.Lock()
	trace.events = append(trace.events, previewTraceEvent{identity, state})
	barrier := trace.barrier
	if barrier != nil && state == "NONE" && identity == "artifacts_downloadartifact_select_artifact_for_grant" {
		trace.arrived++
		if trace.arrived == 3 {
			close(barrier)
		}
	} else {
		barrier = nil
	}
	trace.mu.Unlock()
	if barrier != nil {
		select {
		case <-barrier:
		case <-ctx.Done():
		}
	}
}

func (trace *previewParallelTrace) reset(parallel bool) {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	trace.events, trace.arrived, trace.barrier = nil, 0, nil
	if parallel {
		trace.barrier = make(chan struct{})
	}
}

type previewFailedRow struct{ cause error }

func (row previewFailedRow) Scan(...any) error { return row.cause }

func TestArtifactPreviewScanDropsSerializationCause(t *testing.T) {
	for _, scan := range []func(rowScanner) error{
		func(row rowScanner) error { _, err := scanArtifact(row); return err },
		func(row rowScanner) error { _, err := artifactRevisionRow(row); return err },
	} {
		cause := &pgconn.PgError{Code: "40001", Message: "PRIVATE_DRIVER_SENTINEL"}
		err := scan(previewFailedRow{cause})
		var exposed *pgconn.PgError
		if !errors.Is(err, errs.ErrUnavailable) || errors.As(err, &exposed) || strings.Contains(err.Error(), "PRIVATE_DRIVER_SENTINEL") {
			t.Fatal("existing scan error mapping changed")
		}
	}
}

// Это regression настоящего owner PREVIEW, а не live запрос и не grant обход.
// Все три файла относятся к одному синтетическому Run; provider не запускается.
func TestArtifactPreviewParallelComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("parse isolated fixture configuration")
	}
	trace := &previewParallelTrace{}
	config.ConnConfig.Tracer, config.MaxConns = trace, 8
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("open isolated preview fixture")
	}
	defer pool.Close()
	objects := &previewTrackedStore{Store: objectstoragetest.New()}
	repository, err := New(pool, "openai-codex", "gpt-5", objects)
	if err != nil {
		t.Fatal("construct synthetic owner")
	}
	if err := repository.ConfigureProviderCredential(ProviderCredentialConfig{SecretName: "runtime-provider-openai-default-r1", SecretUID: "10000000-0000-4000-8000-000000000001", SecretResourceVersion: "1", ContentSHA256: strings.Repeat("a", 64)}); err != nil {
		t.Fatal("configure synthetic credential receipt")
	}
	if err := repository.ConfigureRoleImages(RoleImageConfig{PolicyRevision: 1, RoleRuntimeContractRevision: 1, PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64), BuildLeaseDuration: time.Minute, AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute, MaximumAttempts: 3, StagingRepository: "registry.invalid/staging", PromotedRepository: "registry.invalid/roles", DefaultImageReference: "registry.invalid/roles/system@sha256:" + strings.Repeat("c", 64), LeaseSigningKey: []byte(strings.Repeat("d", 32))}); err != nil {
		t.Fatal("configure synthetic image receipt")
	}
	if err := repository.Bootstrap(ctx); err != nil {
		t.Fatal("bootstrap isolated preview fixture")
	}
	seedObservedCatalogFixture(t, ctx, repository)
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal("construct preview owner service")
	}
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.projects.create"}, "control-api-gateway")
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "preview-project"}, Payload: command.ProjectInput{Name: "Parallel preview fixture", Language: "en"}})
	if err != nil || project.Project == nil {
		t.Fatal("create synthetic preview project")
	}
	agent := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "preview-agent", "Preview fixture")
	run, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "preview-run"}, Payload: command.LaunchRunInput{ProjectRef: project.Project.Ref, Title: "Preview fixture", Task: "Synthetic fixture only", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}}})
	if err != nil || run.Run == nil {
		classification := "OTHER"
		for _, candidate := range []struct {
			err   error
			label string
		}{{errs.ErrInvalid, "INVALID"}, {errs.ErrForbidden, "FORBIDDEN"}, {errs.ErrNotFound, "NOT_FOUND"}, {errs.ErrConflict, "CONFLICT"}, {errs.ErrUnavailable, "UNAVAILABLE"}, {errs.ErrCapabilityRequired, "CAPABILITY_REQUIRED"}} {
			if errors.Is(err, candidate.err) {
				classification = candidate.label
				break
			}
		}
		t.Fatalf("create synthetic run without provider execution: class=%s", classification)
	}
	artifacts := make([]entity.Artifact, 3)
	bodies := []string{strings.Repeat("a", 328), strings.Repeat("b", 16718), strings.Repeat("c", 22156)}
	for index := range artifacts {
		artifact, uploadErr := service.UploadArtifact(ctx, owner, value.Mutation{IdempotencyKey: "preview-upload-" + string(rune('0'+index))}, platformrepo.ArtifactUpload{ProjectRef: project.Project.Ref, RunRef: run.Run.Ref, FileName: "preview-" + string(rune('0'+index)) + ".txt", MediaType: "text/plain", SizeBytes: int64(len(bodies[index])), Reader: strings.NewReader(bodies[index])})
		if uploadErr != nil {
			t.Fatal("upload synthetic run artifact")
		}
		artifacts[index] = artifact
	}
	read := func(readCtx context.Context, index int) error {
		download, err := service.DownloadArtifactRevision(readCtx, owner, artifacts[index].Ref, artifacts[index].CurrentRevisionRef, "PREVIEW")
		if err != nil {
			return err
		}
		body, readErr := io.ReadAll(io.LimitReader(download.Reader, int64(len(bodies[index])+1)))
		closeErr := download.Reader.Close()
		if readErr != nil || closeErr != nil {
			return errs.ErrUnavailable
		}
		digest := sha256.Sum256(body)
		if string(body) != bodies[index] || "sha256:"+hex.EncodeToString(digest[:]) != artifacts[index].Digest {
			return errs.ErrConflict
		}
		return nil
	}
	refs := []string{artifacts[0].Ref, artifacts[1].Ref, artifacts[2].Ref}
	counts := func() (int64, int64) {
		var grants, audits int64
		if err := pool.QueryRow(ctx, queryPreviewEffectCounts, refs).Scan(&grants, &audits); err != nil {
			t.Fatal("read disposable preview effect counts")
		}
		return grants, audits
	}
	beforeGrants, beforeAudits := counts()
	beforeGets, beforeCloses := objects.gets.Load(), objects.closes.Load()
	trace.reset(false)
	for index := range artifacts {
		if read(ctx, index) != nil {
			t.Fatal("sequential exact preview failed")
		}
	}
	var failed bool
	for round := 0; round < 4; round++ {
		trace.reset(true)
		batch, stop := context.WithTimeout(ctx, 5*time.Second)
		results := make(chan error, 3)
		for index := range artifacts {
			go func() { results <- read(batch, index) }()
		}
		unavailable, conflict, success := 0, 0, 0
		for range artifacts {
			switch result := <-results; {
			case result == nil:
				success++
			case errors.Is(result, errs.ErrUnavailable):
				unavailable++
			case errors.Is(result, errs.ErrConflict):
				conflict++
			default:
				t.Error("parallel preview returned another closed error")
				failed = true
			}
		}
		stop()
		trace.mu.Lock()
		for _, event := range trace.events {
			if event.state != "NONE" {
				t.Logf("preview_trace identity=%s sqlstate=%s", event.identity, event.state)
			}
		}
		trace.mu.Unlock()
		t.Logf("preview_batch round=%d success=%d unavailable=%d conflict=%d", round, success, unavailable, conflict)
		if success != 3 {
			failed = true
		}
	}
	trace.reset(false)
	for index := range artifacts {
		if read(ctx, index) != nil {
			t.Fatal("sequential exact preview after parallel failed")
		}
	}
	afterGrants, afterAudits := counts()
	if !failed && (afterGrants-beforeGrants != 18 || afterAudits-beforeAudits != 18) {
		t.Fatal("preview retry duplicated committed grant or audit")
	}
	trace.reset(false)
	if _, err := service.DownloadArtifactRevision(ctx, owner, artifacts[0].Ref, artifacts[1].CurrentRevisionRef, "PREVIEW"); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("foreign revision became readable")
	}
	if _, err := service.DownloadArtifactRevision(ctx, owner, artifacts[0].Ref, artifacts[0].CurrentRevisionRef, "WRITE"); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("unknown purpose became readable")
	}
	foreign := owner
	foreign.AuthorityTenant = "10000000-0000-4000-8000-000000000099"
	if _, err := service.DownloadArtifactRevision(ctx, foreign, artifacts[0].Ref, artifacts[0].CurrentRevisionRef, "PREVIEW"); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("foreign tenant principal became readable")
	}
	finalGrants, finalAudits := counts()
	if finalGrants != afterGrants || finalAudits != afterAudits {
		t.Fatal("rejected preview created grants or audit")
	}
	if objects.gets.Load()-beforeGets != objects.closes.Load()-beforeCloses {
		t.Fatal("preview retry leaked or double-closed an object reader")
	}
	if failed {
		t.Fatal("parallel exact previews are not independently available")
	}
}
