package platform

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// queryWorkflowArtifactBoundaryLock holds only the selected mutable HEAD in a rollback-only probe.
	// queryWorkflowArtifactBoundaryLock удерживает только выбранный mutable HEAD в откатываемом probe.
	//go:embed testdata/sql/workflow_artifact_boundary_lock.sql
	queryWorkflowArtifactBoundaryLock string
	// queryWorkflowArtifactBoundaryStatistics refreshes statistics only in the guarded isolated clone.
	// queryWorkflowArtifactBoundaryStatistics обновляет статистику только guarded isolated clone.
	//go:embed testdata/sql/workflow_artifact_boundary_statistics.sql
	queryWorkflowArtifactBoundaryStatistics string
	// queryWorkflowArtifactBoundaryCounts confirms the unrelated native catalog population without
	// inventing completed business state or returning private execution payloads.
	// queryWorkflowArtifactBoundaryCounts подтверждает посторонний native catalog без назначения
	// завершённых business states и выдачи приватных execution payloads.
	//go:embed testdata/sql/workflow_artifact_boundary_counts.sql
	queryWorkflowArtifactBoundaryCounts string
)

// TestWorkflowArtifactQueryBoundaryComponent exercises a rare named artifact among unrelated native
// catalog history. Each real completion and direct selector keeps the unchanged five-second budget;
// exact early capture, replay, downstream dispatch, HEAD contention and invalid pins remain required.
// TestWorkflowArtifactQueryBoundaryComponent проверяет редкое имя файла среди посторонней native
// history каталога. Настоящие completion и direct selector сохраняют прежний бюджет пять секунд;
// exact early capture, replay, downstream dispatch, HEAD contention и invalid pins обязательны.
func TestWorkflowArtifactQueryBoundaryComponent(t *testing.T) {
	testWorkflowCaptureProgressComponent(t, true)
}

// seedWorkflowArtifactBoundaryCatalog creates only unrelated synthetic resources through owner and
// worker commands. No model or external provider runs; SQL never fabricates capture/journal evidence.
// Statistics administration is restricted to this exact disposable loopback clone.
// seedWorkflowArtifactBoundaryCatalog создаёт посторонние синтетические ресурсы только owner/worker
// командами. Модель и внешний провайдер не запускаются; SQL не подделывает capture/journal evidence.
// Администрирование статистики ограничено exact disposable loopback clone.
func seedWorkflowArtifactBoundaryCatalog(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	service *platformservice.Service, owner value.Principal, projectRef, agentRef string,
	execute func(command.Kind, string, *int64, any) command.Result,
) {
	t.Helper()
	for index := range 12 {
		key := fmt.Sprintf("query-noise-agent-%02d", index)
		createLifecycleAgent(t, ctx, service, owner, projectRef, key, key)
	}
	for index := range 220 {
		key := fmt.Sprintf("query-noise-%03d", index)
		run := execute(command.LaunchRun, key, nil, command.LaunchRunInput{ProjectRef: projectRef,
			Task: "Unrelated isolated catalog history.", Target: entity.RunTarget{Type: "AGENT", Ref: agentRef}}).Run
		if index >= 32 {
			execute(command.CancelRun, key+"-cancel", &run.Version, command.RunCommandInput{RunRef: run.Ref, Reason: "Unrelated isolated catalog history."})
			continue
		}
		items := execute(command.ClaimExecution, key+"-claim", nil, command.LeaseInput{WorkloadInstance: "query-boundary-fixture", Limit: 1}).RuntimeItems
		if len(items) != 1 || stringMap(items[0], "runRef") != run.Ref || stringMap(items[0], "agentRef") != agentRef {
			t.Fatal("unrelated catalog fixture claimed a different execution")
		}
		artifacts := make([]command.CompletedArtifact, 0, 10)
		for file := range 10 {
			name := fmt.Sprintf("common-%02d.md", file)
			artifacts = append(artifacts, workflowCaptureProgressArtifact(name, []byte("Unrelated native artifact.\n")))
		}
		lease := items[0]
		result := execute(command.CompleteExecution, key+"-complete", nil, command.CompleteExecutionInput{
			LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: runtimeRevisionMapInt64(lease, "generation"),
			Success: true, Usage: turnUsageFixture(), ResultSummary: "Unrelated native work completed.", Artifacts: artifacts})
		if len(result.CreatedRefs) != 10 {
			t.Fatal("unrelated completion did not create its exact artifact batch")
		}
	}
	var runs, artifacts, agents int
	if err := pool.QueryRow(ctx, queryWorkflowArtifactBoundaryCounts, pgx.StrictNamedArgs{"project_ref": projectRef}).Scan(&runs, &artifacts, &agents); err != nil || runs != 220 || artifacts != 320 || agents != 15 {
		t.Fatal("unrelated native catalog cardinality mismatch")
	}
	adminConfig, err := pgx.ParseConfig(os.Getenv("KODEX_CONTROL_PLANE_TEST_ADMIN_DSN"))
	applicationConfig := pool.Config().ConnConfig
	if err != nil || adminConfig.User != "postgres" || adminConfig.Database != "postgres" || adminConfig.Port < 1024 ||
		(adminConfig.Host != "127.0.0.1" && adminConfig.Host != "localhost") || adminConfig.Host != applicationConfig.Host ||
		adminConfig.Port != applicationConfig.Port || !strings.HasPrefix(applicationConfig.Database, "kodex_assistant_test_") {
		t.Fatal("statistics require the exact disposable loopback administrator")
	}
	adminConfig.Database = applicationConfig.Database
	admin, err := pgx.ConnectConfig(ctx, adminConfig)
	if err != nil {
		t.Fatal("connect isolated statistics administrator")
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer stop()
		if err := admin.Close(cleanup); err != nil {
			t.Error("close isolated statistics administrator")
		}
	}()
	if _, err := admin.Exec(ctx, queryWorkflowArtifactBoundaryStatistics); err != nil {
		t.Fatal("analyze exact isolated catalog")
	}
	t.Log("unrelated native catalog ready: runs=220 artifacts=320 agents=15")
}

// workflowArtifactBoundaryProbe rechecks real owner evidence within five seconds and always rolls
// back independently from cancellation. It returns only the existing safe domain failure mapping.
// workflowArtifactBoundaryProbe перепроверяет owner evidence за пять секунд и всегда откатывает
// транзакцию независимо от отмены. Возвращается только существующий safe domain failure mapping.
func workflowArtifactBoundaryProbe(t *testing.T, ctx context.Context, pool *pgxpool.Pool, current scope, lease map[string]any,
	requirement entity.WorkflowResultRequirement, declaration entity.WorkflowDeclaredResult,
) (entity.WorkflowAcceptedResult, bool, error) {
	t.Helper()
	probe, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	tx, err := pool.Begin(probe)
	if err != nil {
		t.Fatal("begin bounded artifact selector")
	}
	result, found, lookupErr := resolveWorkflowArtifactTx(probe, tx, current, lease, requirement, declaration)
	rollbackWorkflowJournalProbe(t, ctx, tx)
	return result, found, lookupErr
}

// assertWorkflowArtifactBoundaryCurrent checks current pins and coordinates, then proves HEAD
// NOWAIT denial while another transaction holds UPDATE and success after that lock is released.
// assertWorkflowArtifactBoundaryCurrent проверяет current pins и coordinates, затем доказывает
// HEAD NOWAIT denial при UPDATE lock другой транзакции и успех после освобождения lock.
func assertWorkflowArtifactBoundaryCurrent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, nodeRef, leaseRef, artifactRef string) {
	t.Helper()
	current, lease, _ := workflowJournalFixture(t, ctx, pool, nodeRef, leaseRef)
	requirement := entity.WorkflowResultRequirement{Key: "source", Kind: "ARTIFACT", FileName: "early.md", MediaType: "text/markdown"}
	declaration := entity.WorkflowDeclaredResult{Key: "source", Source: &entity.WorkflowArtifactSource{Kind: "CURRENT_COMPLETION"}}
	for _, fault := range []string{"none", "tenant", "root", "node", "runtimeRevision", "lease", "name", "media"} {
		coordinates := make(map[string]any, len(lease))
		for key, value := range lease {
			coordinates[key] = value
		}
		probeScope, wanted := current, requirement
		switch fault {
		case "tenant":
			probeScope.organizationID = "00000000-0000-4000-8000-000000000001"
		case "root", "node", "runtimeRevision", "lease":
			coordinates[fault+"ID"] = "00000000-0000-4000-8000-000000000001"
			if fault == "root" {
				coordinates["rootRunID"] = "00000000-0000-4000-8000-000000000001"
			}
		case "name":
			wanted.FileName = "missing.md"
		case "media":
			wanted.MediaType = "application/json"
		}
		result, found, err := workflowArtifactBoundaryProbe(t, ctx, pool, probeScope, coordinates, wanted, declaration)
		if err != nil || found != (fault == "none") || found && result.ArtifactRef != artifactRef {
			t.Fatalf("bounded artifact selector changed coordinate eligibility: fault=%s outcome=%s", fault, completionOutcomeClass(err))
		}
	}
	locked, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin competing HEAD lock")
	}
	if _, err := locked.Exec(ctx, queryWorkflowArtifactBoundaryLock, pgx.StrictNamedArgs{"artifact_ref": artifactRef}); err != nil {
		rollbackWorkflowJournalProbe(t, ctx, locked)
		t.Fatal("hold competing HEAD update lock")
	}
	probe, stop := context.WithTimeout(ctx, 5*time.Second)
	tx, err := pool.Begin(probe)
	if err != nil {
		stop()
		rollbackWorkflowJournalProbe(t, ctx, locked)
		t.Fatal("begin HEAD NOWAIT probe")
	}
	rows, queryErr := tx.Query(probe, queryWorkflowResultArtifact, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "node_id": stringMap(lease, "nodeID"),
		"root_run_id": stringMap(lease, "rootRunID"), "runtime_revision_id": stringMap(lease, "runtimeRevisionID"),
		"lease_id": stringMap(lease, "leaseID"), "source_kind": "CURRENT_COMPLETION",
		"artifact_ref": "", "artifact_revision": int64(0), "artifact_digest": "",
		"file_name": requirement.FileName, "media_type": requirement.MediaType,
	})
	if queryErr == nil {
		for rows.Next() {
		}
		queryErr = rows.Err()
		rows.Close()
	}
	var failure *pgconn.PgError
	closed := errors.As(queryErr, &failure) && failure.Code == "55P03"
	rollbackWorkflowJournalProbe(t, ctx, tx)
	stop()
	rollbackWorkflowJournalProbe(t, ctx, locked)
	if !closed {
		t.Fatal("HEAD NOWAIT contention was accepted or did not abort the selector transaction")
	}
	result, found, err := workflowArtifactBoundaryProbe(t, ctx, pool, current, lease, requirement, declaration)
	if err != nil || !found || result.ArtifactRef != artifactRef {
		t.Fatal("selector did not recover after competing HEAD lock release", completionOutcomeClass(err))
	}
}

// assertWorkflowArtifactBoundaryEarly refuses changed immutable EARLY pins after the actual final
// completion; the positive EARLY result is independently checked by the enclosing native lifecycle.
// assertWorkflowArtifactBoundaryEarly отклоняет изменённые immutable EARLY pins после настоящего
// final completion; положительный EARLY result независимо проверяется native lifecycle снаружи.
func assertWorkflowArtifactBoundaryEarly(t *testing.T, ctx context.Context, pool *pgxpool.Pool, nodeRef, leaseRef string,
	file entity.ExecutionFileDescriptor, digest string,
) {
	t.Helper()
	current, lease, _ := workflowJournalFixture(t, ctx, pool, nodeRef, leaseRef)
	requirement := entity.WorkflowResultRequirement{Key: "source", Kind: "ARTIFACT", FileName: file.Name, MediaType: file.MediaType}
	for _, fault := range []string{"none", "ref", "revision", "digest"} {
		source := entity.WorkflowArtifactSource{Kind: "CAPTURED_ARTIFACT", ArtifactRef: file.ArtifactRef, Revision: file.Revision, Digest: "sha256:" + digest}
		switch fault {
		case "ref":
			source.ArtifactRef = "art_unrelated_missing"
		case "revision":
			source.Revision++
		case "digest":
			source.Digest = "sha256:" + strings.Repeat("0", 64)
		}
		result, found, err := workflowArtifactBoundaryProbe(t, ctx, pool, current, lease, requirement,
			entity.WorkflowDeclaredResult{Key: "source", Source: &source})
		if err != nil || found != (fault == "none") || found && result.ArtifactRef != file.ArtifactRef {
			t.Fatalf("bounded EARLY selector changed immutable pins: fault=%s outcome=%s", fault, completionOutcomeClass(err))
		}
	}
}

// assertWorkflowArtifactBoundaryDeleted uses the real owner DELETE/impact path after terminal DAG
// completion, then proves the old capture cannot admit a now-deleted mutable HEAD.
// assertWorkflowArtifactBoundaryDeleted использует настоящий owner DELETE/impact path после terminal
// DAG completion и доказывает отказ принимать старый capture с удалённым mutable HEAD.
func assertWorkflowArtifactBoundaryDeleted(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	service *platformservice.Service, owner value.Principal, nodeRef, leaseRef, artifactRef string,
	execute func(command.Kind, string, *int64, any) command.Result,
) {
	t.Helper()
	item, err := service.GetArtifact(ctx, owner, artifactRef)
	if err != nil {
		t.Fatal("read exact HEAD before owner deletion")
	}
	impact, err := service.GetArtifactImpact(ctx, owner, artifactRef, "DELETE")
	if err != nil || !impact.Permitted {
		t.Fatal("terminal artifact deletion is not eligible")
	}
	execute(command.DeleteArtifact, "boundary-delete", &item.Version, command.ArtifactLifecycleInput{ArtifactRef: artifactRef, ImpactDigest: impact.Digest})
	current, lease, _ := workflowJournalFixture(t, ctx, pool, nodeRef, leaseRef)
	_, found, err := workflowArtifactBoundaryProbe(t, ctx, pool, current, lease,
		entity.WorkflowResultRequirement{Key: "source", Kind: "ARTIFACT", FileName: "early.md", MediaType: "text/markdown"},
		entity.WorkflowDeclaredResult{Key: "source", Source: &entity.WorkflowArtifactSource{Kind: "CURRENT_COMPLETION"}})
	if err != nil || found {
		t.Fatal("deleted artifact HEAD was accepted as an active captured result", completionOutcomeClass(err))
	}
}
