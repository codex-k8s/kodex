package platform

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed testdata/sql/runtime_terminal_storage_fixture.sql
var queryRuntimeTerminalStorageFixture string

func TestRuntimeTerminalStorageComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
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
	prepareObservedWarmFixture(t, ctx, r)
	owner := resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.runs.launch"}, "control-api-gateway")
	worker := resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
	service, err := serviceplatform.New(r)
	if err != nil {
		t.Fatal(err)
	}
	assistant, err := service.GetSystemAssistant(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	warm := worker
	warm.Permission = "platform.runtime.warm.report"
	if _, err := service.ReportWarmRuntime(ctx, warm, command.WarmRuntimeInput{WorkloadInstance: "catalog-observed-warm-fixture", RuntimeRevision: assistant.DesiredRuntimeRevision, State: "READY"}); err != nil {
		t.Fatal(err)
	}
	execute := func(kind command.Kind, actor value.Principal, key string, payload any) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: actor, Mutation: value.Mutation{IdempotencyKey: "terminal-storage-" + key}, Payload: payload})
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		return result
	}
	project := execute(command.CreateProject, owner, "project", command.ProjectInput{Name: "Terminal storage", Language: "en"}).Project
	agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "terminal-storage-agent", "Terminal storage")
	for index, test := range []struct{ scope, state string }{{"AGENT", "ERROR"}, {"AGENT", "PURGED"}, {"SYSTEM", "ERROR"}, {"SYSTEM", "PURGED"}} {
		name := test.scope + "-" + test.state
		t.Run(name, func(t *testing.T) {
			var conversationRef, sessionRef string
			if test.scope == "SYSTEM" {
				conversation := execute(command.CreateAssistantConversation, owner, name+"-conversation", command.AssistantConversationInput{AssistantScope: "SYSTEM"}).Conversation
				conversationRef, sessionRef = conversation.Ref, conversation.SessionRef
			}
			launch := func(key string) string {
				if conversationRef != "" {
					conversation := execute(command.AddAssistantTurn, owner, name+key, command.AssistantTurnInput{ConversationRef: conversationRef, Content: "Synthetic terminal storage", DeliveryMode: "QUEUE"}).Conversation
					return conversation.Turns[len(conversation.Turns)-1].RunRef
				}
				run := execute(command.LaunchRun, owner, name+key, command.LaunchRunInput{ProjectRef: project.Ref, SessionRef: sessionRef, Task: "Synthetic terminal storage", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}}).Run
				sessionRef = run.SessionRef
				return run.Ref
			}
			launch("-initial")
			items := execute(command.ClaimExecution, worker, name+"-initial-claim", command.LeaseInput{WorkloadInstance: "terminal-storage-worker", Limit: 1}).RuntimeItems
			if len(items) != 1 {
				t.Fatal("initial claim missing")
			}
			lease := items[0]
			thread := fmt.Sprintf("00000000-0000-4000-8000-%012d", index+3)
			execute(command.CompleteExecution, worker, name+"-initial-complete", command.CompleteExecutionInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: runtimeRevisionMapInt64(lease, "generation"), Success: true, Usage: turnUsageFixture(), CodexSessionID: thread, ArchiveRelativePath: ".kodex/state/codex-home/sessions/2026/10/05/rollout-2026-10-05T00-00-00-" + thread + ".jsonl", ArchiveSHA256: strings.Repeat("a", 64), ArchiveSizeBytes: 128})
			setState := func(state string) {
				if _, err := pool.Exec(ctx, queryRuntimeTerminalStorageFixture, pgx.StrictNamedArgs{"session_ref": sessionRef, "state": state}); err != nil {
					t.Fatal(err)
				}
			}
			setState("SNAPSHOT_READY")
			runRef := launch("-queued")
			for _, state := range []string{"SNAPSHOT_READY", "SNAPSHOTTING", "DELETE_PVC_READY", "ARCHIVED", "RESTORE_READY", "RESTORING"} {
				setState(state)
				if len(execute(command.ClaimExecution, worker, name+"-transit-"+state, command.LeaseInput{WorkloadInstance: "terminal-storage-worker", Limit: 1}).RuntimeItems) != 0 {
					t.Fatal("transit claimed")
				}
				read, err := service.GetRun(ctx, owner, runRef)
				if err != nil || read.State != "RUNNING" {
					t.Fatal("transit terminalized")
				}
			}
			setState(test.state)
			if _, err := pool.Exec(ctx, queryRuntimeClaimFixtureAddSiblings, runRef); err != nil {
				t.Fatal(err)
			}
			claim := command.Command{Kind: command.ClaimExecution, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "terminal-storage-" + name + "-terminal"}, Payload: command.LeaseInput{WorkloadInstance: "terminal-storage-worker", Limit: 1}}
			// Ошибка обязательного audit откатывает уже выполненный terminal SQL.
			func() {
				original := queryCommandsExecuteInsertAuditEventsRefProjectIdAction
				queryCommandsExecuteInsertAuditEventsRefProjectIdAction = queryRuntimeClaimAuditUnavailable
				defer func() { queryCommandsExecuteInsertAuditEventsRefProjectIdAction = original }()
				if _, err := service.Execute(ctx, claim); !errors.Is(err, errs.ErrUnavailable) {
					t.Fatal("audit failure suppressed")
				}
			}()
			read, err := service.GetRun(ctx, owner, runRef)
			if err != nil || read.State != "RUNNING" {
				t.Fatal("partial terminal committed")
			}
			result, err := service.Execute(ctx, claim)
			if err != nil || len(result.RuntimeItems) != 0 {
				t.Fatalf("terminal reconcile: %v", err)
			}
			read, err = service.GetRun(ctx, owner, runRef)
			if err != nil || read.State != "FAILED" || read.SessionReadiness.StorageState != test.state {
				t.Fatal("terminal graph/storage mismatch")
			}
			_, graph, err := service.GetRunGraph(ctx, owner, runRef)
			if err != nil {
				t.Fatal(err)
			}
			for _, node := range graph.Nodes {
				if node.State == "PLANNED" || node.State == "QUEUED" || node.State == "RUNNING" || node.State == "WAITING" {
					t.Fatal("terminal storage left an open graph node")
				}
			}
			var leases, turns, revisions int64
			if err := pool.QueryRow(ctx, queryRuntimeClaimFixtureGraphCounts, runRef).Scan(&leases, &turns, &revisions); err != nil || leases != 0 || turns != 0 || revisions != 0 {
				t.Fatal("terminal graph retained authority or created revision")
			}
			counts := func() [3]int64 {
				var v [3]int64
				if err := pool.QueryRow(ctx, queryRuntimeClaimReceiptCounts, runRef, claim.Mutation.IdempotencyKey).Scan(&v[0], &v[1], &v[2]); err != nil {
					t.Fatal(err)
				}
				return v
			}
			before := counts()
			if before[0] < 1 || before[1] != 1 || before[2] < 2 {
				t.Fatal("empty terminal claim lost durable effects")
			}
			if _, err := service.Execute(ctx, claim); err != nil {
				t.Fatal(err)
			}
			execute(command.ClaimExecution, worker, name+"-repoll", command.LeaseInput{WorkloadInstance: "terminal-storage-worker", Limit: 1})
			if counts() != before {
				t.Fatal("terminal replay duplicated effects")
			}
		})
	}
}
