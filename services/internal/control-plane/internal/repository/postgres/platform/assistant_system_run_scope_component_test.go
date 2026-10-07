package platform

import (
	"context"
	_ "embed"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed testdata/sql/assistant_run_scope_effects.sql
var queryAssistantRunScopeEffects string

// Проверяется signed-project boundary авторитетного чтения, без provider/Pod.
func TestAssistantSystemRunProjectScopeComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
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
	if err := r.ConfigureProviderCredential(ProviderCredentialConfig{SecretName: "runtime-provider-openai-default-r1",
		SecretUID: "10000000-0000-4000-8000-000000000001", SecretResourceVersion: "1", ContentSHA256: strings.Repeat("e", 64)}); err != nil {
		t.Fatal(err)
	}
	if err := r.ConfigureRoleImages(RoleImageConfig{PolicyRevision: 1, RoleRuntimeContractRevision: 1,
		PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64), BuildLeaseDuration: time.Minute,
		AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute, MaximumAttempts: 3,
		StagingRepository: "registry.invalid/kodex/staging", PromotedRepository: "registry.invalid/kodex/roles",
		DefaultImageReference: "registry.invalid/kodex/roles/system@sha256:" + strings.Repeat("c", 64), LeaseSigningKey: []byte(strings.Repeat("d", 32))}); err != nil {
		t.Fatal(err)
	}
	if err := r.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	prepareObservedWarmFixture(t, ctx, r)
	owner := resolvedTestPrincipal(t, ctx, r, platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001",
		ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.assistant.turns.add"}, "control-api-gateway")
	service, err := platformservice.New(r)
	if err != nil {
		t.Fatal(err)
	}
	warm := resolvedTestPrincipal(t, ctx, r, platformrepo.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.warm.report"}, "runtime-controller")
	assistant, err := service.GetSystemAssistant(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReportWarmRuntime(ctx, warm, command.WarmRuntimeInput{WorkloadInstance: "catalog-observed-warm-fixture", RuntimeRevision: assistant.DesiredRuntimeRevision, State: "READY"}); err != nil {
		t.Fatal(err)
	}
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
