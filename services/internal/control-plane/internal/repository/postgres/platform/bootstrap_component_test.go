package platform

import (
	"context"
	_ "embed"
	"os"
	"strings"
	"testing"
	"time"

	platformrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/systemassistant"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	//go:embed sql/bootstrap_component_readback.sql
	bootstrapComponentReadbackQuery string
	//go:embed sql/bootstrap_component_disable_system_assistant.sql
	bootstrapComponentDisableSystemAssistantQuery string
	//go:embed sql/bootstrap_component_delete_system_assistant.sql
	bootstrapComponentDeleteSystemAssistantQuery string
	//go:embed sql/bootstrap_component_replace_core_prompt.sql
	bootstrapComponentReplaceCorePromptQuery string
	//go:embed sql/bootstrap_component_replace_session_provider_account.sql
	bootstrapComponentReplaceSessionProviderAccountQuery string
)

func TestBootstrapComponent(t *testing.T) {
	dsn := os.Getenv("MATTERCODEX_CONTROL_PLANE_TEST_DSN")
	if dsn == "" {
		t.Skip("MATTERCODEX_CONTROL_PLANE_TEST_DSN is not configured")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open disposable PostgreSQL: %v", err)
	}
	defer pool.Close()
	repository, err := New(pool, "openai-codex", "gpt-5")
	if err != nil {
		t.Fatalf("construct repository: %v", err)
	}
	if err := repository.ConfigureProviderCredential(ProviderCredentialConfig{
		SecretName:            "runtime-provider-openai-default-r1",
		SecretUID:             "10000000-0000-4000-8000-000000000001",
		SecretResourceVersion: "1",
		ContentSHA256:         "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
	}); err != nil {
		t.Fatalf("configure provider credential: %v", err)
	}
	if err := repository.ConfigureRoleImages(RoleImageConfig{
		PolicyRevision: 1, RoleRuntimeContractRevision: 1,
		PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64),
		BuildLeaseDuration: time.Minute, AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute, MaximumAttempts: 3,
		StagingRepository: "registry.invalid/mattercodex/staging", PromotedRepository: "registry.invalid/mattercodex/roles",
		DefaultImageReference: "registry.invalid/mattercodex/roles/system@sha256:" + strings.Repeat("c", 64), LeaseSigningKey: []byte(strings.Repeat("d", 32)),
	}); err != nil {
		t.Fatalf("configure role images: %v", err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := repository.Bootstrap(ctx); err != nil {
			t.Fatalf("bootstrap attempt %d: %v", attempt+1, err)
		}
	}
	assertBootstrapReadback(t, ctx, pool)

	for name, query := range map[string]string{
		"disable system assistant":         bootstrapComponentDisableSystemAssistantQuery,
		"delete system assistant":          bootstrapComponentDeleteSystemAssistantQuery,
		"replace core prompt":              bootstrapComponentReplaceCorePromptQuery,
		"replace session provider account": bootstrapComponentReplaceSessionProviderAccountQuery,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, query); err == nil {
				t.Fatal("protected system state was changed")
			}
		})
	}
	assertBootstrapReadback(t, ctx, pool)
	t.Run("system assistant proposes and applies typed plan", func(t *testing.T) {
		testSystemAssistantTypedPlan(t, ctx, repository)
	})
}

func testSystemAssistantTypedPlan(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.assistant.turns.add",
	}, "control-api-gateway")
	worker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "mattercodex-system-subject", ExternalTenantID: "mattercodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.assistant.plan.propose",
	}, "runtime-controller")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct platform service: %v", err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.ReportWarmRuntime, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: "assistant-ready-1"}, Payload: command.WarmRuntimeInput{
			WorkloadInstance: "runtime-test", RuntimeRevision: systemassistant.CorePromptRevision, State: "READY",
		}}); err != nil {
		t.Fatalf("report assistant readiness: %v", err)
	}
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateAssistantConversation, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "assistant-conversation-1"}, Payload: command.AssistantConversationInput{Title: "Configure sales team"}})
	if err != nil {
		t.Fatalf("create assistant conversation: %v", err)
	}
	resolvedOwner, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatalf("resolve owner readback: %v", err)
	}
	ownerScope, err := repository.resolveScope(ctx, resolvedOwner)
	if err != nil {
		t.Fatalf("resolve owner scope readback: %v", err)
	}
	var conversationID, sessionID, sessionRef, projectID, projectRef string
	var conversationVersion int64
	if err := repository.pool.QueryRow(ctx, queryConfigurationAddassistantturncommandSelectAssistantConversationsOrganizationIdRefState,
		ownerScope.organizationID, created.Conversation.Ref,
	).Scan(&conversationID, &sessionID, &sessionRef, &projectID, &projectRef, &conversationVersion); err != nil {
		t.Fatalf("read assistant conversation before turn: %v", err)
	}
	turn, err := service.Execute(ctx, command.Command{Kind: command.AddAssistantTurn, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "assistant-turn-1"}, Payload: command.AssistantTurnInput{
			ConversationRef: created.Conversation.Ref, Content: "Create a sales project",
		}})
	if err != nil || turn.Plan != nil {
		t.Fatalf("queue assistant turn without keyword fallback: plan=%#v err=%v", turn.Plan, err)
	}
	claimed, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: "assistant-claim-1"}, Payload: command.LeaseInput{WorkloadInstance: "runtime-test", Limit: 1}})
	if err != nil || len(claimed.RuntimeItems) != 1 {
		t.Fatalf("claim assistant execution: claims=%d err=%v", len(claimed.RuntimeItems), err)
	}
	lease := claimed.RuntimeItems[0]
	planResult, err := service.Execute(ctx, command.Command{Kind: command.ProposeAssistantPlan, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: "assistant-plan-1"}, Payload: command.ProposeAssistantPlanInput{
			LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: lease["generation"].(int64),
			Summary: "Create project Sales", Operations: []entity.AssistantPlanOperation{{Key: "operation-001", Type: "CREATE_PROJECT", Summary: "Create sales project",
				Input: map[string]any{"name": "Sales", "purpose": "Qualify and convert leads", "language": "en"}}},
		}})
	if err != nil || planResult.Plan == nil || planResult.Plan.State != "PROPOSED" {
		t.Fatalf("propose assistant plan: result=%#v err=%v", planResult.Plan, err)
	}
	expectedPlanVersion := int64(1)
	applied, err := service.Execute(ctx, command.Command{Kind: command.ApplyAssistantPlan, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "assistant-apply-1", ExpectedVersion: &expectedPlanVersion},
		Payload:  command.AssistantPlanInput{PlanRef: planResult.Plan.Ref}})
	if err != nil || applied.Plan == nil || applied.Plan.State != "APPLIED" || len(applied.CreatedRefs) != 1 {
		t.Fatalf("apply assistant plan: result=%#v refs=%v err=%v", applied.Plan, applied.CreatedRefs, err)
	}
}

func resolvedTestPrincipal(t *testing.T, ctx context.Context, repository *Repository, input platformrepo.ProofPrincipalInput, workload string) value.Principal {
	t.Helper()
	authority, err := repository.ResolveProofAuthority(ctx, input)
	if err != nil {
		t.Fatalf("resolve test proof authority: %v", err)
	}
	return value.Principal{ActorID: authority.ActorID, AuthorityTenant: authority.OrganizationID,
		Permission: input.Operation, CorrelationRef: input.Operation + "-component", CallerWorkload: workload, CredentialRevision: 1}
}

func assertBootstrapReadback(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var organizationCount, ownerContractCount, systemAssistantCount, corePromptCount int
	var assistantRuntimeCount, capabilityCount, integrationDefinitionCount int
	var providerDefinitionCount, providerAccountCount, providerCredentialRevisionCount, completedBootstrapCount int
	if err := pool.QueryRow(ctx, bootstrapComponentReadbackQuery).Scan(
		&organizationCount, &ownerContractCount, &systemAssistantCount, &corePromptCount,
		&assistantRuntimeCount, &capabilityCount, &integrationDefinitionCount,
		&providerDefinitionCount, &providerAccountCount, &providerCredentialRevisionCount,
		&completedBootstrapCount,
	); err != nil {
		t.Fatalf("read bootstrap state: %v", err)
	}
	if organizationCount != 1 || ownerContractCount != 1 || systemAssistantCount != 1 ||
		corePromptCount != 1 || assistantRuntimeCount != 1 || capabilityCount != 8 ||
		integrationDefinitionCount != 3 || providerDefinitionCount != 1 || providerAccountCount != 1 ||
		providerCredentialRevisionCount != 1 || completedBootstrapCount != 1 {
		t.Fatalf("unexpected bootstrap state: organization=%d owner_contract=%d assistant=%d core_prompt=%d runtime=%d capabilities=%d integrations=%d provider_definitions=%d provider_accounts=%d provider_credentials=%d completed=%d",
			organizationCount, ownerContractCount, systemAssistantCount, corePromptCount,
			assistantRuntimeCount, capabilityCount, integrationDefinitionCount, providerDefinitionCount,
			providerAccountCount, providerCredentialRevisionCount, completedBootstrapCount)
	}
}
