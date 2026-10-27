package platform

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestRuntimeFilesLockedReadComponent creates isolated PostgreSQL plus an in-memory store and
// synthetic scanner/credential/image configuration, bootstraps warm owner metadata and delegates
// the direct-run lifecycle containing file retry tests. The90s component fixture is opt-in and
// tests no real provider, registry, Kubernetes or AV service.
// TestRuntimeFilesLockedReadComponent создаёт isolated PostgreSQL с in-memory store и synthetic
// scanner/credential/image configuration, подготавливает warm owner metadata и вызывает direct-run
// lifecycle с file retry tests. Component fixture с90s budget opt-in и не проверяет реальные
// provider, registry, Kubernetes и AV service.
func TestRuntimeFilesLockedReadComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("create isolated file fixture")
	}
	defer pool.Close()
	repository, err := New(pool, "openai-codex", "gpt-5", objectstoragetest.New())
	if err != nil {
		t.Fatal("construct isolated file repository")
	}
	configureComponentArtifactScanner(t, repository)
	if err := repository.ConfigureProviderCredential(ProviderCredentialConfig{SecretName: "runtime-provider-openai-default-r1", SecretUID: "10000000-0000-4000-8000-000000000001", SecretResourceVersion: "1", ContentSHA256: strings.Repeat("a", 64)}); err != nil {
		t.Fatal("configure synthetic credential identity")
	}
	if err := repository.ConfigureRoleImages(syntheticRoleImageComponentConfig()); err != nil {
		t.Fatal("configure synthetic image identity")
	}
	if err := repository.Bootstrap(ctx); err != nil {
		t.Fatal("bootstrap isolated file fixture")
	}
	prepareObservedWarmFixture(t, ctx, repository)
	testDirectRunLifecycle(t, ctx, repository)
}

// runtimeFilesReadTrace counts matching file-catalog read attempts and40001 conflicts under mutex
// and sends the first backend PID through an initialized buffered channel. It is test
// synchronization state, not runtime authorization or a production query observer.
// runtimeFilesReadTrace считает matching file-catalog read attempts и40001 conflicts под mutex и
// передаёт первый backend PID через назначенный buffered channel. Это test synchronization state,
// а не runtime authorization и production query observer.
type runtimeFilesReadTrace struct {
	mu                  sync.Mutex
	attempts, conflicts int
	started             chan uint32
}

// runtimeFilesReadTraceKey tags only the named file-catalog query context so its end callback can
// count serialization failure. The private zero-sized key carries no caller identity or file
// grant.
// runtimeFilesReadTraceKey помечает лишь named file-catalog query context для подсчёта
// serialization failure end callback. Private zero-sized key не несёт caller identity и file
// grant.
type runtimeFilesReadTraceKey struct{}

// TraceQueryStart matches the exact SQL-name prefix, counts the attempt and sends the first
// connection PID under lock before returning a tagged context. Other SQL is untouched; the fixture
// must supply a buffered channel to avoid blocking the tracer.
// TraceQueryStart сопоставляет exact SQL-name prefix, считает attempt и передаёт первый connection
// PID под lock до возврата tagged context. Другой SQL не меняется; fixture задаёт buffered
// channel, чтобы tracer не блокировался.
func (trace *runtimeFilesReadTrace) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if !strings.HasPrefix(data.SQL, "-- name: runtime_files_read_catalog :one") {
		return ctx
	}
	trace.mu.Lock()
	trace.attempts++
	if trace.attempts == 1 {
		trace.started <- conn.PgConn().PID()
	}
	trace.mu.Unlock()
	return context.WithValue(ctx, runtimeFilesReadTraceKey{}, true)
}

// TraceQueryEnd counts only tagged wrapped pgconn40001 failures under lock, ignoring every other
// query/error. It never retries the read or rewrites the original failure.
// TraceQueryEnd считает под lock лишь tagged wrapped pgconn40001 failures, игнорируя остальные
// query/error. Сам не повторяет чтение и не переписывает исходную ошибку.
func (trace *runtimeFilesReadTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if matched, _ := ctx.Value(runtimeFilesReadTraceKey{}).(bool); !matched {
		return
	}
	recordReadSerializationConflict(&trace.mu, &trace.conflicts, data.Err)
}

// testRuntimeFilesConcurrentLeaseRenew for search/manifest/metadata/preview/expiry coordinates a
// locked renewal and exact backend waiter within5s, requiring one40001 and a whole second read. It
// preserves cursor/content pins, rejects expiry with no partial page and checks expected effect
// counters. A separate uncanceled one-second cleanup restores expiry; it needs real disposable
// PostgreSQL and synthetic file fixtures, not a running tool or provider.
// testRuntimeFilesConcurrentLeaseRenew для search/manifest/metadata/preview/expiry согласует
// locked renewal и exact backend waiter за5s, требуя один40001 и полный second read. Сохраняет
// cursor/content pins, отклоняет expiry без partial page и проверяет ожидаемые effect counters.
// Отдельный uncanceled one-second cleanup восстанавливает expiry; нужны real disposable PostgreSQL
// и synthetic file fixtures, а не running tool и provider.
func testRuntimeFilesConcurrentLeaseRenew(t *testing.T, parent context.Context, repository *Repository, lease map[string]any, first entity.ExecutionFilePage) {
	for _, operation := range []string{"search", "manifest", "metadata", "preview", "expiry"} {
		t.Run(operation, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(parent, 5*time.Second)
			defer cancel()
			purpose := runtimeFilesTestContext(t, lease, runtimecontract.FilePurposeWorkspaceInput)
			permission := operation
			if permission == "expiry" {
				permission = "search"
			}
			reader := runtimeFilesTestPrincipal(t, ctx, repository, permission)
			var beforeRaw, afterRaw string
			resolvedReader, err := repository.ResolvePrincipal(ctx, reader)
			if err != nil {
				t.Fatal("resolve verified disposable file principal")
			}
			current, err := repository.resolveScope(ctx, resolvedReader)
			if err != nil {
				t.Fatal("resolve disposable file reader")
			}
			if err := repository.pool.QueryRow(ctx, queryAssistantConfigurationComponentEffects, current.organizationID).Scan(&beforeRaw); err != nil {
				t.Fatal("read disposable effects")
			}
			trace := &runtimeFilesReadTrace{started: make(chan uint32, 1)}
			config := repository.pool.Config()
			config.ConnConfig.Tracer = trace
			pool, err := pgxpool.NewWithConfig(ctx, config)
			if err != nil {
				t.Fatal("create disposable file read pool")
			}
			defer pool.Close()
			readRepository := *repository
			readRepository.pool = pool
			service, err := platformservice.New(&readRepository)
			if err != nil {
				t.Fatal("construct disposable file service")
			}
			renew, err := repository.pool.Begin(ctx)
			if err != nil {
				t.Fatal("begin disposable lease renew")
			}
			defer renew.Rollback(ctx)
			var leaseID string
			if err := renew.QueryRow(ctx, queryAssistantLockedReadLeaseID, stringMap(lease, "leaseRef")).Scan(&leaseID); err != nil {
				t.Fatal("resolve disposable lease")
			}
			expires := time.Now().UTC().Add(30 * time.Second)
			if operation == "expiry" {
				expires = time.Now().UTC().Add(-time.Second)
			}
			if _, err := renew.Exec(ctx, queryRuntimeRenewexecutionUpdateRuntimeLeasesExpiresAtUpdatedAt, leaseID, expires); err != nil {
				t.Fatal("hold exact renew update")
			}
			defer func() {
				cleanup, stop := context.WithTimeout(context.WithoutCancel(parent), time.Second)
				defer stop()
				if _, err := repository.pool.Exec(cleanup, queryAssistantCurrentConfigurationRestoreExpiry, stringMap(lease, "leaseRef"), time.Now().UTC().Add(30*time.Second)); err != nil {
					t.Error("restore disposable lease expiry")
				}
			}()
			file := first.Items[0]
			exact := query.ExecutionFileRef{EntryRef: file.EntryRef, ArtifactRef: file.ArtifactRef, Revision: file.Revision, Digest: file.Digest}
			// outcome carries the operation-specific page/metadata/preview and error through a buffered result
			// channel. Only the selected operation populates its field; the parent checks freshness, exact
			// pins and trace/effect counts before accepting it.
			// outcome передаёт operation-specific page/metadata/preview и error через buffered result channel.
			// Лишь выбранная operation заполняет своё поле; parent проверяет freshness, exact pins и
			// trace/effect counts до принятия результата.
			type outcome struct {
				page     entity.ExecutionFilePage
				metadata entity.ExecutionFileMetadata
				preview  entity.ExecutionFilePreview
				err      error
			}
			completed := make(chan outcome, 1)
			go func() {
				var result outcome
				switch operation {
				case "search", "expiry":
					result.page, result.err = service.SearchExecutionFiles(ctx, reader, purpose, "", query.Page{Size: 1, Token: first.Next})
				case "manifest":
					result.page, result.err = service.GetExecutionFileManifest(ctx, reader, purpose, query.Page{Size: 1})
				case "metadata":
					result.metadata, result.err = service.GetExecutionFileMetadata(ctx, reader, purpose, exact)
				case "preview":
					result.preview, result.err = service.PreviewExecutionFile(ctx, reader, purpose, exact, 16384)
				}
				completed <- result
			}()
			var pid uint32
			select {
			case pid = <-trace.started:
			case <-ctx.Done():
				t.Fatal("file read did not reach lease guard")
			}
			for {
				var blocked bool
				if err := repository.pool.QueryRow(ctx, queryWorkerGrantWaiting, pid).Scan(&blocked); err != nil {
					t.Fatal("observe exact blocked file reader")
				}
				if blocked {
					break
				}
				timer := time.NewTimer(time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
					t.Fatal("file reader did not wait for renew")
				case <-timer.C:
				}
			}
			if err := renew.Commit(ctx); err != nil {
				t.Fatal("commit disposable renew")
			}
			var result outcome
			select {
			case result = <-completed:
			case <-ctx.Done():
				t.Fatal("file retry exceeded request budget")
			}
			trace.mu.Lock()
			attempts, conflicts := trace.attempts, trace.conflicts
			trace.mu.Unlock()
			if attempts != 2 || conflicts != 1 {
				t.Fatalf("whole file read retry: attempts=%d conflicts=%d", attempts, conflicts)
			}
			if operation == "expiry" {
				if !errors.Is(result.err, errs.ErrNotFound) || len(result.page.Items) != 0 || result.page.Next != "" {
					t.Fatal("expired lease returned partial page or was not rechecked")
				}
			} else if result.err != nil {
				t.Fatal("concurrent renew closed valid file catalog")
			}
			switch operation {
			case "search":
				if result.page.Total != first.Total || len(result.page.Items) != 1 || result.page.Next != "" || result.page.Items[0].EntryRef == file.EntryRef || result.page.Catalog.Digest != purpose.CatalogDigest {
					t.Fatal("continuation changed immutable catalog or cursor")
				}
			case "manifest":
				if result.page.Total != first.Total || len(result.page.Items) != 1 || result.page.Next == "" {
					t.Fatal("manifest retry lost bounded page")
				}
			case "metadata":
				if result.metadata.File != file {
					t.Fatal("metadata retry changed exact file pins")
				}
			case "preview":
				if result.preview.Metadata.File != file || result.preview.Truncated || result.preview.Digest != file.Digest {
					t.Fatal("preview retry changed exact content")
				}
			}
			if err := repository.pool.QueryRow(ctx, queryAssistantConfigurationComponentEffects, current.organizationID).Scan(&afterRaw); err != nil {
				t.Fatal("read disposable effects after retry")
			}
			var before, after []int64
			if json.Unmarshal([]byte(beforeRaw), &before) != nil || json.Unmarshal([]byte(afterRaw), &after) != nil || len(before) != len(after) {
				t.Fatal("invalid disposable effects snapshot")
			}
			for i := range before {
				want := before[i]
				if i == 0 && operation != "expiry" {
					want++
				}
				if after[i] != want {
					t.Fatal("retry duplicated audit or created business effects")
				}
			}
		})
	}
}
