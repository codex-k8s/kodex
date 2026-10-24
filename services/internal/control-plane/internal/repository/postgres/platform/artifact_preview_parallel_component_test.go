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

// queryPreviewEffectCounts counts consumed PREVIEW grants and artifact.preview audits for the
// fixture artifact refs, enabling a before/after cardinality assertion. It does not itself
// authorize a read or prove reader cleanup; this is embedded disposable-test SQL.
// queryPreviewEffectCounts считает consumed PREVIEW grants и artifact.preview audits заданных
// fixture refs для проверки cardinality до и после. Сам не авторизует чтение и не доказывает
// cleanup; это встроенный SQL disposable-теста.
//
//go:embed testdata/sql/artifact_preview_effect_counts.sql
var queryPreviewEffectCounts string

// previewTrackedStore wraps a synthetic object store with atomic successful-Get and body-Close
// counters for concurrent reader-leak assertions. It preserves the underlying store and does not
// simulate PostgreSQL ownership or a live provider.
// previewTrackedStore оборачивает синтетический object store атомарными счётчиками успешных Get и
// body Close для конкурентной проверки утечек. Сохраняет исходный store, не моделируя PostgreSQL
// ownership или живого provider.
type previewTrackedStore struct {
	objectstorage.Store
	gets, closes atomic.Int64
}

// Get delegates exact context/key/version to the store and, only on success, counts the Get and
// wraps the returned body for close accounting. Receipt/error and failure behavior remain
// unchanged.
// Get передаёт store точные context, key и version, только при успехе учитывает Get и оборачивает
// body для Close accounting. Квитанция, ошибка и поведение отказа сохраняются.
func (store *previewTrackedStore) Get(ctx context.Context, key, version string) (objectstorage.Object, error) {
	object, err := store.Store.Get(ctx, key, version)
	if err == nil {
		store.gets.Add(1)
		object.Body = &previewTrackedBody{ReadCloser: object.Body, store: store}
	}
	return object, err
}

// previewTrackedBody retains the original reader/closer and owning fixture store so every close
// invocation can be counted without buffering or altering bytes. It is not an idempotent-close
// wrapper.
// previewTrackedBody хранит исходный reader/closer и fixture store для учёта каждого Close без
// буферизации и изменения байтов. Не делает Close идемпотентным.
type previewTrackedBody struct {
	io.ReadCloser
	store *previewTrackedStore
}

// Close increments the atomic close counter before forwarding to the original body and returns its
// error unchanged. Repeated closes remain observable rather than silently suppressed.
// Close увеличивает атомарный счётчик до вызова исходного Close, возвращая его ошибку. Повторные
// закрытия учитываются, а не скрываются.
func (body *previewTrackedBody) Close() error {
	body.store.closes.Add(1)
	return body.ReadCloser.Close()
}

// previewTraceContextKey provides a private typed context key for the closed SQL identity passed
// between pgx tracer callbacks. No SQL text, arguments or credential data are stored in the
// context.
// previewTraceContextKey задаёт закрытый тип ключа context для SQL identity между pgx tracer
// callbacks. SQL, аргументы и credentials в context не сохраняются.
type previewTraceContextKey struct{}

// previewTraceEvent stores only a closed query identity and normalized SQLSTATE category for
// synthetic concurrency diagnostics. It carries no raw PostgreSQL error or query payload.
// previewTraceEvent сохраняет только закрытые query identity и SQLSTATE category для диагностики
// синтетической конкуренции, без сырой ошибки PostgreSQL и query payload.
type previewTraceEvent struct{ identity, state string }

// previewParallelTrace protects its event list and optional grant-selection barrier with a mutex,
// tracking how many concurrent reads reached the selected boundary. The barrier controls fixture
// timing only and does not grant database authority.
// previewParallelTrace защищает mutex список событий и необязательный barrier выбора grant,
// учитывая число достигших границы чтений. Barrier управляет временем оснастки, не полномочиями
// БД.
type previewParallelTrace struct {
	mu       sync.Mutex
	events   []previewTraceEvent
	barrier  chan struct{}
	arrived  int
	expected int
}

// previewQueryIdentity maps the fixed download/grant/content/audit SQL name prefixes and commit to
// closed identities, falling back to OTHER. It deliberately does not retain or log the original
// SQL or any arguments.
// previewQueryIdentity сопоставляет фиксированные SQL name prefixes download, grant, content,
// audit и commit закрытым identity, иначе OTHER. Исходные SQL и аргументы намеренно не сохраняются
// и не логируются.
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

// TraceQueryStart adds only the normalized statement identity to the supplied context for
// TraceQueryEnd. It neither records arguments nor changes the query or caller deadline.
// TraceQueryStart добавляет в переданный context только нормализованную identity для
// TraceQueryEnd, не сохраняя аргументы и не меняя query и deadline.
func (trace *previewParallelTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, previewTraceContextKey{}, previewQueryIdentity(data.SQL))
}

// TraceQueryEnd records a closed identity/state pair under the mutex and synchronizes successful
// grant-selection queries at the configured arrival count. Only four listed SQLSTATEs escape as
// codes; other errors become OTHER. Barrier wait observes caller cancellation after releasing the
// mutex.
// TraceQueryEnd записывает закрытые identity и state под mutex и синхронизирует успешные
// grant-selection queries по заданному числу участников. Только четыре перечисленных SQLSTATE
// сохраняются кодами, прочие ошибки дают OTHER. Ожидание barrier учитывает отмену после
// освобождения mutex.
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
		if trace.arrived == trace.expected {
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

// reset clears recorded events and arrivals and replaces the barrier under lock; positive
// parallelism enables synchronization while zero disables it. Callers reset between completed
// fixture batches, not as a runtime retry or cancellation mechanism.
// reset под mutex очищает события и arrivals, заменяет barrier; положительный parallelism включает
// синхронизацию, ноль отключает. Вызывающий сбрасывает между завершёнными batches, не для runtime
// retry или cancellation.
func (trace *previewParallelTrace) reset(parallel int) {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	trace.events, trace.arrived, trace.barrier, trace.expected = nil, 0, nil, parallel
	if parallel > 0 {
		trace.barrier = make(chan struct{})
	}
}

// previewFailedRow carries an injected scan error without row values for the pure scan-mapping
// regression. It does not connect to a database or contain real driver payloads.
// previewFailedRow содержит заданную ошибку Scan без значений строки для чистой регрессии
// отображения. Не подключается к БД и не содержит реальных driver payloads.
type previewFailedRow struct{ cause error }

// Scan returns the synthetic failure without assigning destinations, allowing both artifact
// scanners to demonstrate safe domain mapping.
// Scan возвращает синтетическую ошибку без присваивания destinations, позволяя обоим artifact
// scanners проверить безопасное доменное отображение.
func (row previewFailedRow) Scan(...any) error { return row.cause }

// TestArtifactPreviewScanDropsSerializationCause injects a synthetic 40001 cause into artifact and
// revision scanners and requires Unavailable without exposed PgError or private marker. This tests
// only scanner mapping; it does not prove transaction retry or PostgreSQL behavior.
// TestArtifactPreviewScanDropsSerializationCause вводит синтетический 40001 в artifact и revision
// scanners, требуя Unavailable без PgError и private marker. Проверяет только отображение Scan, не
// повтор транзакции и не PostgreSQL.
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

// TestArtifactPreviewParallelComponent uses isolated PostgreSQL, eight connections, synthetic
// storage and closed tracing to read eight exact artifacts sequentially and in four rounds each of
// four/eight concurrent PREVIEWs. It verifies bytes/digest, one consumed grant/audit per
// successful read, balanced readers and negative revision/purpose/tenant cases. The 60-second
// contour and five-second batches do not run a provider or live environment; absent fixtures cause
// a skip rather than acceptance.
// TestArtifactPreviewParallelComponent использует isolated PostgreSQL, восемь connections,
// синтетический storage и закрытый tracing для чтения восьми точных artifacts последовательно и
// четырьмя rounds по четыре и восемь PREVIEW одновременно. Проверяет bytes, digest, один consumed
// grant и audit на чтение, баланс readers и отрицательные revision, purpose и tenant случаи.
// Контур 60 секунд и batches пять секунд не запускают provider и живую среду; отсутствие fixtures
// означает skip, не приёмку.
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
	configureComponentArtifactScanner(t, repository)
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
	artifacts := make([]entity.Artifact, 8)
	bodies := []string{strings.Repeat("a", 328), strings.Repeat("b", 16718), strings.Repeat("c", 22156), strings.Repeat("d", 3090), strings.Repeat("e", 8180), strings.Repeat("f", 2400), strings.Repeat("g", 22000), strings.Repeat("h", 4055)}
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
	refs := make([]string, len(artifacts))
	for index := range artifacts {
		refs[index] = artifacts[index].Ref
	}
	counts := func() (int64, int64) {
		var grants, audits int64
		if err := pool.QueryRow(ctx, queryPreviewEffectCounts, refs).Scan(&grants, &audits); err != nil {
			t.Fatal("read disposable preview effect counts")
		}
		return grants, audits
	}
	beforeGrants, beforeAudits := counts()
	beforeGets, beforeCloses := objects.gets.Load(), objects.closes.Load()
	trace.reset(0)
	for index := range artifacts {
		if read(ctx, index) != nil {
			t.Fatal("sequential exact preview failed")
		}
	}
	var failed bool
	const rounds = 4
	expectedReads := int64(2 * len(artifacts))
	for _, parallel := range []int{4, 8} {
		expectedReads += int64(rounds * parallel)
		for round := 0; round < rounds; round++ {
			trace.reset(parallel)
			batch, stop := context.WithTimeout(ctx, 5*time.Second)
			results := make(chan error, parallel)
			for index := 0; index < parallel; index++ {
				go func() { results <- read(batch, index) }()
			}
			unavailable, conflict, success := 0, 0, 0
			for received := 0; received < parallel; received++ {
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
			t.Logf("preview_batch parallel=%d round=%d success=%d unavailable=%d conflict=%d", parallel, round, success, unavailable, conflict)
			if success != parallel {
				failed = true
			}
		}
	}
	trace.reset(0)
	for index := range artifacts {
		if read(ctx, index) != nil {
			t.Fatal("sequential exact preview after parallel failed")
		}
	}
	afterGrants, afterAudits := counts()
	if !failed && (afterGrants-beforeGrants != expectedReads || afterAudits-beforeAudits != expectedReads) {
		t.Fatal("preview retry duplicated committed grant or audit")
	}
	trace.reset(0)
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
