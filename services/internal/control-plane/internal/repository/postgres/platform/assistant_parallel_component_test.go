package platform

import (
	"context"
	"errors"
	"fmt"
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

func testAssistantParallelAdmission(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	prepareObservedWarmFixture(t, ctx, repository)
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.assistant.turns.add",
	}, "control-api-gateway")
	worker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim",
	}, "runtime-controller")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	warmWorker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.warm.report",
	}, "runtime-controller")
	assistant, err := service.GetSystemAssistant(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReportWarmRuntime(ctx, warmWorker, command.WarmRuntimeInput{
		WorkloadInstance: "catalog-observed-warm-fixture", RuntimeRevision: assistant.DesiredRuntimeRevision, State: "READY",
	}); err != nil {
		t.Fatalf("report synthetic assistant readiness: %v", err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		current, err := service.GetSystemAssistant(cleanup, owner)
		if err != nil {
			t.Errorf("read synthetic warm cleanup: %v", err)
			return
		}
		// Одна смена статуса не освобождает зарегистрированный warm consumer.
		if _, err := service.Execute(cleanup, command.Command{Kind: command.RecoverAssistant, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: "parallel-release-synthetic-warm", ExpectedVersion: &current.Version},
			Payload:  struct{}{},
		}); err != nil {
			t.Errorf("release synthetic assistant readiness: %v", err)
		}
	}()
	var accountRef string
	if err := repository.pool.QueryRow(ctx, `SELECT ref FROM control_plane.provider_accounts WHERE stable_key = 'default-openai-codex'`).Scan(&accountRef); err != nil {
		t.Fatal(err)
	}
	initialCatalog, err := service.ListModelCatalog(ctx, owner, "openai-codex", accountRef, query.Filter{})
	if err != nil || initialCatalog.Status == nil || initialCatalog.Status.State != "READY" {
		t.Fatalf("initial synthetic catalog: %v", err)
	}
	var runRefs []string
	// Каждая фикстура закрывается штатной командой даже после сбоя проверки.
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		for index, ref := range runRefs {
			run, err := service.GetRun(cleanup, owner, ref)
			if err != nil {
				t.Errorf("read parallel fixture cleanup: %v", err)
				continue
			}
			if run.State == "CANCELLED" || run.State == "SUCCEEDED" || run.State == "FAILED" {
				continue
			}
			if _, err := service.Execute(cleanup, command.Command{Kind: command.CancelRun, Principal: owner,
				Mutation: value.Mutation{IdempotencyKey: fmt.Sprintf("parallel-cleanup-%d", index), ExpectedVersion: &run.Version},
				Payload:  command.RunCommandInput{RunRef: ref, Reason: "Synthetic parallel fixture cleanup"}}); err != nil {
				t.Errorf("cancel parallel fixture: %v", err)
			}
		}
	}()
	setLimit := func(limit int32, key string) {
		t.Helper()
		account, err := service.GetProviderAccountWithUsage(ctx, owner, accountRef, nil)
		if err != nil {
			t.Fatal(err)
		}
		result, err := service.Execute(ctx, command.Command{Kind: command.SetProviderAccountConcurrency, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: key, ExpectedVersion: &account.Version},
			Payload:  command.ProviderAccountInput{AccountRef: accountRef, MaximumConcurrentExecutions: limit}})
		if err != nil || result.ProviderAccount == nil || result.ProviderAccount.MaximumConcurrentExecutions != limit {
			t.Fatalf("set parallel limit %d: %v", limit, err)
		}
		catalog, err := service.ListModelCatalog(ctx, owner, "openai-codex", accountRef, query.Filter{})
		if err != nil || catalog.Status == nil || catalog.Status.State != "READY" || catalog.Digest != initialCatalog.Digest {
			t.Fatalf("capacity mutation invalidated catalog: %v", err)
		}
	}
	claim := func(key string) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker,
			Mutation: value.Mutation{IdempotencyKey: key}, Payload: command.LeaseInput{WorkloadInstance: key, Limit: 32}})
		if err != nil {
			t.Fatalf("claim parallel executions: %v", err)
		}
		return result
	}
	assertActive := func(want int64) {
		t.Helper()
		account, err := service.GetProviderAccountWithUsage(ctx, owner, accountRef, nil)
		if err != nil || account.Usage == nil || account.Usage.ActiveExecutions != want {
			t.Fatalf("parallel active count want %d: usage=%#v err=%v", want, account.Usage, err)
		}
	}
	sessions := map[string]string{}
	for index := range 11 {
		key := fmt.Sprintf("parallel-%02d", index)
		created, err := service.Execute(ctx, command.Command{Kind: command.CreateAssistantConversation, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: key + "-create"}, Payload: command.AssistantConversationInput{}})
		if err != nil || created.Conversation == nil {
			t.Fatalf("create independent parallel conversation: %v", err)
		}
		turn, err := service.Execute(ctx, command.Command{Kind: command.AddAssistantTurn, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: key + "-turn"},
			Payload:  command.AssistantTurnInput{ConversationRef: created.Conversation.Ref, Content: "Parallel admission " + key, DeliveryMode: "QUEUE"}})
		if err != nil || turn.Conversation == nil || len(turn.Conversation.Turns) != 1 {
			t.Fatalf("queue independent parallel turn: %v", err)
		}
		conversation := turn.Conversation
		if _, exists := sessions[conversation.SessionRef]; exists {
			t.Fatal("parallel conversations share a session")
		}
		sessions[conversation.SessionRef] = conversation.Ref
		runRefs = append(runRefs, conversation.Turns[0].RunRef)
	}
	setLimit(2, "parallel-limit-two")
	leases := claim("parallel-two").RuntimeItems
	if len(leases) != 2 {
		t.Fatalf("two independent chats admitted: %d", len(leases))
	}
	assertActive(2)
	setLimit(10, "parallel-limit-ten")
	type outcome struct {
		result command.Result
		err    error
	}
	start := make(chan struct{})
	results := make(chan outcome, 2)
	for _, key := range []string{"parallel-race-a", "parallel-race-b"} {
		go func() {
			<-start
			// Оба worker получают разные node locks, но конкурируют за последние слоты аккаунта.
			result, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker,
				Mutation: value.Mutation{IdempotencyKey: key}, Payload: command.LeaseInput{WorkloadInstance: key, Limit: 6}})
			results <- outcome{result: result, err: err}
		}()
	}
	close(start)
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatalf("racing parallel claim: %v", result.err)
		}
		leases = append(leases, result.result.RuntimeItems...)
	}
	if len(leases) != 10 {
		t.Fatalf("racing workers over/under admitted: %d", len(leases))
	}
	assertActive(10)
	claimedSessions := map[string]bool{}
	for _, lease := range leases {
		sessionRef := stringMap(lease, "sessionRef")
		if claimedSessions[sessionRef] || sessions[sessionRef] == "" || stringMap(lease, "providerAccountRef") != accountRef {
			t.Fatal("claim crossed or duplicated a conversation/account boundary")
		}
		claimedSessions[sessionRef] = true
	}
	if len(claim("parallel-eleventh-waits").RuntimeItems) != 0 {
		t.Fatal("eleventh chat bypassed full account capacity")
	}
	setLimit(1, "parallel-limit-lower-active")
	assertActive(10)
	if len(claim("parallel-lowered-waits").RuntimeItems) != 0 {
		t.Fatal("lowered capacity admitted new work")
	}
	stoppedLease := leases[0]
	conversationRef := sessions[stringMap(stoppedLease, "sessionRef")]
	conversations, _, err := service.ListAssistantConversations(ctx, owner, query.Filter{Page: query.Page{Size: 100}})
	if err != nil {
		t.Fatal(err)
	}
	var conversation *entity.AssistantConversation
	for index := range conversations {
		if conversations[index].Ref == conversationRef {
			conversation = &conversations[index]
			break
		}
	}
	if conversation == nil {
		t.Fatal("running parallel conversation missing")
	}
	stopped, err := service.Execute(ctx, command.Command{Kind: command.CancelAssistantTurn, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "parallel-stop-one", ExpectedVersion: &conversation.Version},
		Payload:  command.AssistantTurnCancellationInput{ConversationRef: conversationRef}})
	if err != nil || stopped.Runtime["cancelled"] != true {
		t.Fatalf("stop one parallel conversation: %v", err)
	}
	assertActive(9)
	for _, lease := range leases[1:] {
		run, err := service.GetRun(ctx, owner, stringMap(lease, "runRef"))
		if err != nil || run.State != "RUNNING" {
			t.Fatalf("stop affected another chat: %v", err)
		}
	}
	if len(claim("parallel-nine-still-over-lower-limit").RuntimeItems) != 0 {
		t.Fatal("stop ignored the lowered limit")
	}
	_, err = service.Execute(ctx, command.Command{Kind: command.CompleteExecution, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: "parallel-stopped-late-complete"}, Payload: command.CompleteExecutionInput{
			LeaseRef: stringMap(stoppedLease, "leaseRef"), Fence: stringMap(stoppedLease, "fence"), Generation: stoppedLease["generation"].(int64),
			Success: true, ResultSummary: "Must not complete a stopped chat", Usage: turnUsageFixture()}})
	if !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("late completion after isolated stop: %v", err)
	}
	setLimit(10, "parallel-limit-restore")
	eleventh := claim("parallel-eleventh-admitted").RuntimeItems
	if len(eleventh) != 1 || claimedSessions[stringMap(eleventh[0], "sessionRef")] {
		t.Fatal("queued eleventh did not get its own free slot")
	}
	assertActive(10)
	// CancelRun переводит уже исполняемую работу в CANCELLING и сохраняет lease
	// до подтверждения worker либо expiry. Для общей component fixture завершаем
	// все оставшиеся исполнения штатными callback-командами, иначе следующие
	// сценарии видят занятую ёмкость аккаунта до истечения lease.
	activeLeases := append(append([]map[string]any{}, leases[1:]...), eleventh[0])
	for index, lease := range activeLeases {
		completeClaimedExecution(t, ctx, service, worker, lease, fmt.Sprintf("parallel-complete-%02d", index), false)
	}
	assertActive(0)
}
