package platform

import (
	"context"
	_ "embed"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// queryAssistantRunScopeEffects reads fixture organization Run/receipt/audit/turn counts and
// global outbox count to detect durable effects from rejected signed-project commands.
// queryAssistantRunScopeEffects читает счётчики Run, квитанций, аудита и turn тестовой организации
// и общий счётчик outbox для обнаружения устойчивых эффектов отклонённых команд подписанного
// проекта.
//
//go:embed testdata/sql/assistant_run_scope_effects.sql
var queryAssistantRunScopeEffects string

// TestAssistantSystemRunProjectScopeComponent checks exact SYSTEM run/target pins and
// concurrent-renew read behavior, then denies project-signed run/graph/cancel/retry/receipt access
// before OCC without durable effects; no real provider or Pod is used.
// TestAssistantSystemRunProjectScopeComponent проверяет точные привязки SYSTEM Run и цели и чтение
// при конкурентном renew, затем запрещает подписанному проекту доступ к Run, графу, отмене,
// повтору и квитанции до OCC без устойчивых эффектов; реальный провайдер и Pod не используются.
func TestAssistantSystemRunProjectScopeComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := newIsolatedAssistantComponentPool(t, ctx)
	var err error
	defer pool.Close()
	r, service, owner, assistant := newWarmTurnComponentFixture(t, ctx, pool, namespacedRoleImageComponentConfig)
	execute := func(kind command.Kind, key string, payload any) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "system-run-scope-" + key}, Payload: payload})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	project := execute(command.CreateProject, "project", command.ProjectInput{Name: "Scope isolation fixture", Language: "en"}).Project
	conversation := execute(command.CreateAssistantConversation, "conversation", command.AssistantConversationInput{AssistantScope: "SYSTEM"}).Conversation
	expectedTarget, err := service.GetAgentRuntimeConfiguration(ctx, owner, assistant.Ref)
	if err != nil {
		t.Fatal(err)
	}
	execute(command.AddAssistantTurn, "turn", command.AssistantTurnInput{ConversationRef: conversation.Ref, Content: "Synthetic scope isolation", DeliveryMode: "QUEUE"})
	items, _, err := service.ListAssistantConversations(ctx, owner, query.AssistantConversationFilter{Filter: query.Filter{Page: query.Page{Size: 50}}})
	if err != nil {
		t.Fatal(err)
	}
	var runRef string
	for _, item := range items {
		if item.Ref == conversation.Ref && len(item.Turns) > 0 {
			runRef = item.Turns[0].RunRef
		}
	}
	if runRef == "" {
		t.Fatal("synthetic SYSTEM run was not materialized")
	}
	run, err := service.GetRun(ctx, owner, runRef)
	if err != nil || run.ProjectRef != "" || run.Source != "SYSTEM_ASSISTANT" {
		t.Fatalf("authoritative SYSTEM run fixture: %v", err)
	}
	if run.AssistantPin == nil || run.AssistantPin.Scope != "SYSTEM" ||
		run.AssistantPin.OrganizationRef != expectedTarget.Environment.OrganizationRef ||
		run.AssistantPin.ConversationRef != conversation.Ref || run.AssistantPin.AssistantRef != assistant.Ref ||
		run.AssistantPin.ProjectRef != "" || run.AssistantPin.ProfileRef != "" ||
		run.Target.Type != "SYSTEM_ASSISTANT" || run.Target.Ref != assistant.Ref ||
		run.Target.Version != expectedTarget.AgentVersion || expectedTarget.AgentVersion < 1 {
		t.Fatal("system run lost exact assistant owner/target version")
	}
	t.Run("fresh catalog survives concurrent lease renew", func(t *testing.T) {
		catalog, _ := promotionComponentCatalog(t)
		r.ConfigureRoleImageCatalog(catalog)
		worker := resolvedTestPrincipal(t, ctx, r, platformrepo.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
		claimed, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "system-run-scope-catalog-renew-claim"}, Payload: command.LeaseInput{WorkloadInstance: "system-run-scope-catalog-renew", Limit: 10}})
		if err != nil {
			t.Fatal("claim synthetic SYSTEM catalog execution")
		}
		var lease map[string]any
		for _, item := range claimed.RuntimeItems {
			if stringMap(item, "runRef") == runRef {
				lease = item
			}
		}
		if lease == nil {
			t.Fatal("synthetic SYSTEM lease missing")
		}
		reader := resolvedTestPrincipal(t, ctx, r, platformrepo.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.assistant.resources.search"}, "runtime-controller")
		testAssistantLockedReadConcurrentLeaseRenew(t, ctx, r, reader, lease)
	})
	signed := owner
	signed.ProjectRef = gateTestProjectID(t, ctx, r, owner, project.Ref)
	t.Run("run", func(t *testing.T) {
		if _, err := service.GetRun(ctx, signed, runRef); !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
			t.Fatalf("signed PROJECT credential read SYSTEM run: %v", err)
		}
	})
	t.Run("graph", func(t *testing.T) {
		if _, _, err := service.GetRunGraph(ctx, signed, runRef); !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
			t.Fatalf("signed PROJECT credential read SYSTEM graph: %v", err)
		}
	})
	resolved, err := r.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	current, err := r.resolveScope(ctx, resolved)
	if err != nil {
		t.Fatal(err)
	}
	effects := func() [5]int {
		t.Helper()
		var result [5]int
		if err := pool.QueryRow(ctx, queryAssistantRunScopeEffects, current.organizationID).Scan(&result[0], &result[1], &result[2], &result[3], &result[4]); err != nil {
			t.Fatal(err)
		}
		return result
	}
	assertRejectedCommand := func(t *testing.T, kind command.Kind, key string, version int64) {
		t.Helper()
		before := effects()
		_, err := service.Execute(ctx, command.Command{Kind: kind, Principal: signed,
			Mutation: value.Mutation{IdempotencyKey: "system-run-scope-" + key, ExpectedVersion: &version},
			Payload:  command.RunCommandInput{RunRef: runRef, Reason: "Synthetic project scope rejection"}})
		if !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
			t.Errorf("signed PROJECT command %s reached SYSTEM run: %v", kind, err)
		}
		if effects() != before {
			t.Error("rejected signed PROJECT run command changed durable state")
		}
	}
	t.Run("cancel", func(t *testing.T) { assertRejectedCommand(t, command.CancelRun, "signed-cancel", run.Version) })
	t.Run("cancel-before-occ", func(t *testing.T) {
		assertRejectedCommand(t, command.CancelRun, "signed-cancel-stale", run.Version+999999)
	})
	run, err = service.GetRun(ctx, owner, runRef)
	if err != nil {
		t.Fatal(err)
	}
	ownerCancelVersion := run.Version
	if run.State != "CANCELLED" {
		if _, err := service.Execute(ctx, command.Command{Kind: command.CancelRun, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: "system-run-scope-owner-cancel", ExpectedVersion: &run.Version},
			Payload:  command.RunCommandInput{RunRef: runRef, Reason: "Synthetic project scope rejection"}}); err != nil {
			t.Fatal(err)
		}
	}
	run, err = service.GetRun(ctx, owner, runRef)
	if err != nil || run.State != "CANCELLED" {
		t.Fatalf("owner canonical terminal fixture: %v", err)
	}
	t.Run("cancel-receipt-replay", func(t *testing.T) { assertRejectedCommand(t, command.CancelRun, "owner-cancel", ownerCancelVersion) })
	t.Run("retry", func(t *testing.T) { assertRejectedCommand(t, command.RetryRun, "signed-retry", run.Version) })
	t.Run("retry-before-occ", func(t *testing.T) {
		assertRejectedCommand(t, command.RetryRun, "signed-retry-stale", run.Version+999999)
	})
}
