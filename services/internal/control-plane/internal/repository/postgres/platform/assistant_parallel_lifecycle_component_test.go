package platform

import (
	"context"
	_ "embed"
	"errors"
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
	"github.com/jackc/pgx/v5/pgxpool"
)

// queryAssistantParallelLifecycleExpire expires one exact claimed lease to
// exercise recovery without changing unrelated concurrent conversations.
// RU: queryAssistantParallelLifecycleExpire истекает один точный claimed
// lease для проверки recovery без изменения соседних диалогов.
//
//go:embed testdata/sql/assistant_parallel_lifecycle_expire.sql
var queryAssistantParallelLifecycleExpire string

// queryAssistantParallelLifecycleGraph counts active nodes, leases and turns
// under one root run to detect cross-conversation lifecycle leaks.
// RU: queryAssistantParallelLifecycleGraph считает активные узлы, leases
// и turns одного root run для выявления утечки состояния между диалогами.
//
//go:embed testdata/sql/assistant_parallel_lifecycle_graph.sql
var queryAssistantParallelLifecycleGraph string

// TestAssistantParallelLifecycleComponent verifies queued and interrupting
// turns, cancellation, restart and history isolation with disposable
// PostgreSQL and synthetic callbacks, not a real provider or user session.
// RU: TestAssistantParallelLifecycleComponent проверяет очередь и прерывание
// turns, отмену, restart и изоляцию истории на disposable PostgreSQL и
// синтетических callback без реального provider и пользовательской сессии.
func TestAssistantParallelLifecycleComponent(t *testing.T) {
	dsn := isolatedAssistantComponentDSN(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repository := newConfiguredAssistantComponentRepository(t, pool, "gpt-5")
	if err := repository.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	prepareObservedWarmFixture(t, ctx, repository)
	principal := func(workload, operation, actor, tenant string) value.Principal {
		t.Helper()
		return resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
			ExternalActorID: actor, ExternalTenantID: tenant, CallerWorkload: workload, Operation: operation,
		}, workload)
	}
	owner := principal("control-api-gateway", "platform.assistant.turns.add", "20000000-0000-4000-8000-000000000001", "20000000-0000-4000-8000-000000000002")
	worker := principal("runtime-controller", "platform.runtime.execution.claim", "kodex-system-subject", "kodex-installation")
	warmWorker := principal("runtime-controller", "platform.runtime.warm.report", "kodex-system-subject", "kodex-installation")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	assistant, err := service.GetSystemAssistant(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReportWarmRuntime(ctx, warmWorker, command.WarmRuntimeInput{
		WorkloadInstance: "catalog-observed-warm-fixture", RuntimeRevision: assistant.DesiredRuntimeRevision, State: "READY",
	}); err != nil {
		t.Fatal(err)
	}
	execute := func(kind command.Kind, actor value.Principal, key string, version *int64, payload any) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: actor,
			Mutation: value.Mutation{IdempotencyKey: "parallel-lifecycle-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("execute %s (%s): %v", kind, key, err)
		}
		return result
	}
	create := func(key string) entity.AssistantConversation {
		t.Helper()
		return *execute(command.CreateAssistantConversation, owner, key+"-create", nil, command.AssistantConversationInput{AssistantScope: "SYSTEM"}).Conversation
	}
	add := func(conversation entity.AssistantConversation, key, mode string) entity.AssistantConversation {
		t.Helper()
		return *execute(command.AddAssistantTurn, owner, key, nil, command.AssistantTurnInput{
			ConversationRef: conversation.Ref, Content: "Synthetic message " + key, DeliveryMode: mode}).Conversation
	}
	readConversation := func(ref string) entity.AssistantConversation {
		t.Helper()
		items, _, err := service.ListAssistantConversations(ctx, owner, query.AssistantConversationFilter{Filter: query.Filter{Page: query.Page{Size: 50}}})
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range items {
			if item.Ref == ref {
				return item
			}
		}
		t.Fatal("conversation missing from authoritative readback")
		return entity.AssistantConversation{}
	}
	claim := func(key string, count int32) []map[string]any {
		t.Helper()
		return execute(command.ClaimExecution, worker, key, nil, command.LeaseInput{WorkloadInstance: "parallel-lifecycle-worker-" + key, Limit: count}).RuntimeItems
	}
	assertRun := func(ref, want string) {
		t.Helper()
		run, err := service.GetRun(ctx, owner, ref)
		if err != nil || run.State != want {
			t.Fatalf("run state want=%s got=%s err=%v", want, run.State, err)
		}
	}
	assertClosedGraph := func(runRef string) {
		t.Helper()
		var roots, openNodes, openLeases, openTurns int
		if err := pool.QueryRow(ctx, queryAssistantParallelLifecycleGraph, runRef).Scan(&roots, &openNodes, &openLeases, &openTurns); err != nil || roots != 1 || openNodes != 0 || openLeases != 0 || openTurns != 0 {
			t.Fatalf("terminal root retained graph: roots=%d nodes=%d leases=%d turns=%d err=%v", roots, openNodes, openLeases, openTurns, err)
		}
	}
	assertLateRejected := func(lease map[string]any, key string) {
		t.Helper()
		_, err := service.Execute(ctx, command.Command{Kind: command.CompleteExecution, Principal: worker,
			Mutation: value.Mutation{IdempotencyKey: "parallel-lifecycle-late-" + key}, Payload: command.CompleteExecutionInput{
				LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: lease["generation"].(int64), Success: true,
				ResultSummary: "Rejected synthetic stale callback", Usage: turnUsageFixture(),
			}})
		if !errors.Is(err, errs.ErrForbidden) {
			t.Fatalf("closed lease accepted completion: %v", err)
		}
	}
	a, b := add(create("a"), "a-first", "QUEUE"), add(create("b"), "b-first", "QUEUE")
	leases := claim("initial", 2)
	if len(leases) != 2 || a.SessionRef == b.SessionRef {
		t.Fatalf("two independent sessions were not claimed: %d", len(leases))
	}
	bySession := map[string]map[string]any{}
	for _, lease := range leases {
		bySession[stringMap(lease, "sessionRef")] = lease
	}
	aFirst, bFirst := bySession[a.SessionRef], bySession[b.SessionRef]
	if aFirst == nil || bFirst == nil || stringMap(aFirst, "runtimeRevisionRef") == stringMap(bFirst, "runtimeRevisionRef") || stringMap(aFirst, "leaseRef") == stringMap(bFirst, "leaseRef") {
		t.Fatal("parallel execution identities were shared or crossed")
	}
	aQueued := add(a, "a-followup", "QUEUE")
	if len(claim("same-session-blocked", 10)) != 0 {
		t.Fatal("queued followup ran while its session already had an active execution")
	}
	assertRun(aQueued.Turns[0].RunRef, "RUNNING")
	aImmediate := add(a, "a-immediate", "INTERRUPT_ACTIVE")
	assertRun(stringMap(aFirst, "runRef"), "CANCELLED")
	assertClosedGraph(stringMap(aFirst, "runRef"))
	assertLateRejected(aFirst, "interrupt")
	assertRun(stringMap(bFirst, "runRef"), "RUNNING")
	immediateClaims := claim("immediate-priority", 10)
	if len(immediateClaims) != 1 || stringMap(immediateClaims[0], "runRef") != aImmediate.Turns[0].RunRef {
		t.Fatal("interrupt did not take priority over the queued followup in its own session")
	}
	if stringMap(immediateClaims[0], "task") != "Synthetic message a-immediate" {
		t.Fatal("interrupt task was substituted")
	}
	completeClaimedExecutionWithSummary(t, ctx, service, worker, immediateClaims[0], "parallel-lifecycle-immediate", false, "Only conversation A immediate result")
	followupClaims := claim("followup-after-immediate", 10)
	if len(followupClaims) != 1 || stringMap(followupClaims[0], "runRef") != aQueued.Turns[0].RunRef {
		t.Fatal("queued followup did not resume after the immediate turn")
	}
	if stringMap(followupClaims[0], "task") != "Synthetic message a-followup" {
		t.Fatal("queued task was replaced by the later interrupt")
	}
	completeClaimedExecutionWithSummary(t, ctx, service, worker, followupClaims[0], "parallel-lifecycle-followup", false, "Only conversation A queued result")
	assertRun(stringMap(bFirst, "runRef"), "RUNNING")
	// Создание нового adapter/service не должно зависеть от памяти прежнего controller.
	repository = newConfiguredAssistantComponentRepository(t, pool, "gpt-5")
	service, err = platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	if len(claim("after-repository-restart", 10)) != 0 {
		t.Fatal("repository reconstruction duplicated an active execution")
	}
	readA, readB := readConversation(a.Ref), readConversation(b.Ref)
	if readA.SessionRef != a.SessionRef || readB.SessionRef != b.SessionRef || len(readA.Turns) != 5 || len(readB.Turns) != 1 {
		t.Fatalf("restart readback mixed conversation histories: a=%d b=%d", len(readA.Turns), len(readB.Turns))
	}
	for _, turn := range readB.Turns {
		if strings.Contains(turn.Content, "conversation A") || strings.Contains(turn.Content, "a-followup") || strings.Contains(turn.Content, "a-immediate") {
			t.Fatal("conversation B history contains conversation A data")
		}
	}
	t.Run("retry cancelled assistant run preserves conversation and foreign execution", func(t *testing.T) {
		previous, err := service.GetRun(ctx, owner, stringMap(aFirst, "runRef"))
		if err != nil {
			t.Fatal(err)
		}
		retried, err := service.Execute(ctx, command.Command{Kind: command.RetryRun, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: "parallel-lifecycle-retry-a", ExpectedVersion: &previous.Version},
			Payload:  command.RunCommandInput{RunRef: previous.Ref}})
		if err != nil || retried.Run == nil || retried.Run.SessionRef != a.SessionRef ||
			retried.Run.RetryOfRunRef != previous.Ref || retried.Run.Attempt != previous.Attempt+1 {
			t.Fatalf("retry did not preserve exact assistant lineage: run=%#v err=%v", retried.Run, err)
		}
		if previous.AssistantPin == nil || retried.Run.AssistantPin == nil || *previous.AssistantPin != *retried.Run.AssistantPin ||
			retried.Run.AssistantPin.Scope != "SYSTEM" || retried.Run.AssistantPin.ConversationRef != a.Ref || retried.Run.AssistantPin.AssistantRef != assistant.Ref ||
			retried.Run.AssistantPin.OrganizationRef == "" || retried.Run.AssistantPin.ProfileRef != "" || retried.Run.AssistantPin.ProjectRef != "" {
			t.Fatal("assistant retry changed the immutable conversation/profile pin")
		}
		retryClaims := claim("retry-a", 10)
		if len(retryClaims) != 1 || stringMap(retryClaims[0], "runRef") != retried.Run.Ref ||
			stringMap(retryClaims[0], "sessionRef") != a.SessionRef || stringMap(retryClaims[0], "runtimeRevisionRef") == stringMap(aFirst, "runtimeRevisionRef") {
			t.Fatal("retry crossed a chat or reused an old immutable runtime revision")
		}
		assertRun(stringMap(bFirst, "runRef"), "RUNNING")
		assertLateRejected(aFirst, "retry")
		completeClaimedExecutionWithSummary(t, ctx, service, worker, retryClaims[0], "parallel-lifecycle-retry", false, "Only conversation A retry result")
		terminal, err := service.GetRun(ctx, owner, retried.Run.Ref)
		if err != nil || terminal.AssistantPin == nil || terminal.Target.Version < 1 {
			t.Fatalf("terminal assistant retry pin: %v", err)
		}
		replay, err := service.Execute(ctx, command.Command{Kind: command.RetryRun, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: "parallel-lifecycle-retry-a", ExpectedVersion: &previous.Version},
			Payload:  command.RunCommandInput{RunRef: previous.Ref}})
		if err != nil || replay.Run == nil || replay.Run.AssistantPin == nil || *replay.Run.AssistantPin != *terminal.AssistantPin ||
			replay.Run.Target != terminal.Target || replay.Run.State != retried.Run.State || replay.Run.Ref != terminal.Ref {
			t.Fatalf("receipt retry view lost fresh authoritative pin or rewrote its saved state: %v", err)
		}
	})
	readA = readConversation(a.Ref)
	archived := execute(command.ArchiveAssistantConversation, owner, "archive-a", &readA.Version, command.AssistantConversationArchiveInput{ConversationRef: a.Ref})
	if archived.Conversation == nil || archived.Conversation.State != "ARCHIVED" {
		t.Fatal("idle conversation A was not archived")
	}
	execute(command.PurgeAssistantConversation, owner, "purge-a", &archived.Conversation.Version, command.AssistantConversationArchiveInput{ConversationRef: a.Ref})
	assertRun(stringMap(bFirst, "runRef"), "RUNNING")
	// Только точная lease B старится фикстурой; новый claim выполняет штатный expiry.
	if tag, err := pool.Exec(ctx, queryAssistantParallelLifecycleExpire, stringMap(bFirst, "leaseRef")); err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("expire exact disposable lease: %v", err)
	}
	reclaimed := claim("expire-b", 10)
	assertLateRejected(bFirst, "expiry")
	if len(reclaimed) != 1 || stringMap(reclaimed[0], "sessionRef") != b.SessionRef ||
		stringMap(reclaimed[0], "leaseRef") == stringMap(bFirst, "leaseRef") ||
		stringMap(reclaimed[0], "runtimeRevisionRef") == stringMap(bFirst, "runtimeRevisionRef") {
		t.Fatal("expired execution did not get a fresh isolated lease and runtime revision")
	}
	completeClaimedExecutionWithSummary(t, ctx, service, worker, reclaimed[0], "parallel-lifecycle-reclaimed", false, "Only conversation B recovered result")
	assertClosedGraph(stringMap(bFirst, "runRef"))
}
