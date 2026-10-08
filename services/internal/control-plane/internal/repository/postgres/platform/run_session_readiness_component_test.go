package platform

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	serviceplatform "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRunSessionReadinessComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	r, err := New(pool, "openai-codex", "gpt-5", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ConfigureProviderCredential(ProviderCredentialConfig{SecretName: "runtime-provider-openai-default-r1", SecretUID: "10000000-0000-4000-8000-000000000001", SecretResourceVersion: "1", ContentSHA256: strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	if err := r.ConfigureRoleImages(RoleImageConfig{PolicyRevision: 1, RoleRuntimeContractRevision: 1, PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64), BuildLeaseDuration: time.Minute, AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute, MaximumAttempts: 3, StagingRepository: "registry.invalid/staging", PromotedRepository: "registry.invalid/roles", DefaultImageReference: "registry.invalid/roles/system@sha256:" + strings.Repeat("c", 64), LeaseSigningKey: []byte(strings.Repeat("d", 32))}); err != nil {
		t.Fatal(err)
	}
	if err := r.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	seedObservedCatalogFixture(t, ctx, r)
	owner := resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.runs.launch"}, "control-api-gateway")
	worker := resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
	service, err := serviceplatform.New(r)
	if err != nil {
		t.Fatal(err)
	}
	execute := func(kind command.Kind, actor value.Principal, key string, payload any) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: actor, Mutation: value.Mutation{IdempotencyKey: "session-readiness-" + key}, Payload: payload})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	project := execute(command.CreateProject, owner, "project", command.ProjectInput{Name: "Bounded session diagnostic", Language: "en"}).Project
	agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "session-readiness-agent", "Session diagnostic")
	run := execute(command.LaunchRun, owner, "launch", command.LaunchRunInput{ProjectRef: project.Ref, Task: "Synthetic archive lifecycle", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}}).Run
	read, err := service.GetRun(ctx, owner, run.Ref)
	if err != nil || read.SessionReadiness == nil || read.SessionReadiness.StorageState != "UNTRACKED" || read.SessionReadiness.Reason != "NO_SESSION_BLOCKER" {
		t.Fatalf("untracked session read: %v", err)
	}
	graphRun, graph, err := service.GetRunGraph(ctx, owner, run.Ref)
	if err != nil || graphRun.Ref != run.Ref || graph.RunRef != run.RootRunRef || !reflect.DeepEqual(graphRun.SessionReadiness, read.SessionReadiness) {
		t.Fatalf("graph lost untracked session readiness: %v", err)
	}
	leases := execute(command.ClaimExecution, worker, "claim", command.LeaseInput{WorkloadInstance: "session-readiness-runtime", Limit: 1}).RuntimeItems
	if len(leases) != 1 {
		t.Fatal("synthetic claim missing")
	}
	lease := leases[0]
	execute(command.CompleteExecution, worker, "complete", command.CompleteExecutionInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: runtimeRevisionMapInt64(lease, "generation"), Success: true, Usage: turnUsageFixture(), CodexSessionID: "00000000-0000-4000-8000-000000000003", ArchiveRelativePath: ".kodex/state/codex-home/sessions/2026/08/28/rollout-2026-08-28T23-23-39-00000000-0000-4000-8000-000000000003.jsonl", ArchiveSHA256: strings.Repeat("a", 64), ArchiveSizeBytes: 128})
	if _, err := pool.Exec(ctx, "UPDATE control_plane.session_storage SET idle_since=clock_timestamp()-interval '1 hour' WHERE session_id=(SELECT id FROM control_plane.sessions WHERE ref=$1)", run.SessionRef); err != nil {
		t.Fatal(err)
	}
	claimPrincipal := sessionArchivePrincipal(t, ctx, r, "platform.session-archive.tasks.claim")
	failPrincipal := sessionArchivePrincipal(t, ctx, r, "platform.session-archive.tasks.fail")
	var taskRef string
	for attempt := 1; attempt <= sessionArchiveMaxAttempts; attempt++ {
		task := claimSingleSessionArchiveTask(t, ctx, service, claimPrincipal, "SNAPSHOT")
		taskRef = stringMap(task, "taskRef")
		payload := claimedSessionArchivePayload(task)
		payload.SafeErrorCode = "SESSION_ARCHIVE_SOURCE_INVALID"
		execute(command.FailSessionArchiveTask, failPrincipal, fmt.Sprintf("fail-%d", attempt), payload)
		if attempt < sessionArchiveMaxAttempts {
			if _, err := pool.Exec(ctx, "UPDATE control_plane.session_archive_tasks SET available_at=clock_timestamp()-interval '1 second' WHERE ref=$1", taskRef); err != nil {
				t.Fatal(err)
			}
		}
	}
	effects := func() [3]int64 {
		var counts [3]int64
		if err := pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM control_plane.audit_events),(SELECT count(*) FROM control_plane.idempotency_receipts),(SELECT count(*) FROM control_plane.run_events)").Scan(&counts[0], &counts[1], &counts[2]); err != nil {
			t.Fatal(err)
		}
		return counts
	}
	before := effects()
	read, err = service.GetRun(ctx, owner, run.Ref)
	if err != nil || read.SessionReadiness == nil {
		t.Fatalf("protected session diagnostic: %v", err)
	}
	proof := read.SessionReadiness
	if proof.SessionRef != run.SessionRef || proof.StorageState != "ERROR" || proof.Reason != "STORAGE_NOT_LIVE" || proof.LatestArchiveTask == nil || proof.LatestArchiveTask.Ref != taskRef || proof.LatestArchiveTask.State != "DEAD_LETTER" || proof.LatestArchiveTask.Attempt != 5 || proof.LatestArchiveTask.SafeErrorCode != "SESSION_ARCHIVE_SOURCE_INVALID" {
		t.Fatal("exact exhausted snapshot/session binding missing")
	}
	graphRun, graph, err = service.GetRunGraph(ctx, owner, run.Ref)
	if err != nil || graphRun.Ref != run.Ref || graph.RunRef != run.RootRunRef || !reflect.DeepEqual(graphRun.SessionReadiness, proof) {
		t.Fatalf("graph lost exact exhausted snapshot/session readiness: %v", err)
	}
	if before != effects() {
		t.Fatal("diagnostic read changed durable state")
	}
	foreignProject := execute(command.CreateProject, owner, "foreign-project", command.ProjectInput{Name: "Other scope", Language: "en"}).Project
	signed := owner
	signed.ProjectRef = gateTestProjectID(t, ctx, r, owner, foreignProject.Ref)
	if _, err := service.GetRun(ctx, signed, run.Ref); !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("foreign scope obtained session diagnostic: %v", err)
	}
	if foreignRun, foreignGraph, err := service.GetRunGraph(ctx, signed, run.Ref); (!errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden)) || foreignRun.Ref != "" || foreignRun.SessionReadiness != nil || foreignGraph.RunRef != "" {
		t.Fatal("foreign scope obtained graph session diagnostic")
	}
	// Считаем только protected reads; создание тестового project имеет штатные effects.
	before = effects()
	if _, err := service.GetRun(ctx, owner, run.Ref); err != nil {
		t.Fatal(err)
	}
	if before != effects() {
		t.Fatal("diagnostic read changed durable state")
	}
	// Schema допускает 6, но закрытый readiness predicate разрешает только 5.
	// Ошибочный snapshot не выдаёт частичный graph и освобождает read transaction.
	if _, err := pool.Exec(ctx, "UPDATE control_plane.session_archive_tasks SET maximum_attempts=6 WHERE ref=$1", taskRef); err != nil {
		t.Fatal(err)
	}
	before = effects()
	if invalidRun, invalidGraph, err := service.GetRunGraph(ctx, owner, run.Ref); !errors.Is(err, errs.ErrUnavailable) || invalidRun.Ref != "" || invalidRun.SessionReadiness != nil || invalidGraph.RunRef != "" || len(invalidGraph.Nodes) != 0 {
		t.Fatal("invalid readiness yielded a partial graph")
	}
	if invalidRun, err := service.GetRun(ctx, owner, run.Ref); !errors.Is(err, errs.ErrUnavailable) || invalidRun.Ref != "" || invalidRun.SessionReadiness != nil {
		t.Fatal("single Run and graph readiness predicates diverged")
	}
	if before != effects() || pool.Stat().AcquiredConns() != 0 {
		t.Fatal("failed readiness read changed state or retained a transaction")
	}
	if _, err := pool.Exec(ctx, "UPDATE control_plane.session_archive_tasks SET maximum_attempts=5 WHERE ref=$1", taskRef); err != nil {
		t.Fatal(err)
	}
	graphRun, _, err = service.GetRunGraph(ctx, owner, run.Ref)
	if err != nil || !reflect.DeepEqual(graphRun.SessionReadiness, proof) {
		t.Fatalf("graph readiness did not recover after fixture restoration: %v", err)
	}
}
