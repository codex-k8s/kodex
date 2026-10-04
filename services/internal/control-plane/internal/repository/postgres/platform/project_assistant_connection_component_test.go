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
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	serviceplatform "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed testdata/sql/project_assistant_connection_readback.sql
var queryProjectAssistantConnectionReadback string

//go:embed testdata/sql/project_assistant_connection_purpose.sql
var queryProjectAssistantConnectionPurpose string

func TestProjectAssistantConnectionComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated connection PostgreSQL")
	}
	defer pool.Close()
	r, err := New(pool, "openai-codex", "gpt-5", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	if err = r.ConfigureProviderCredential(ProviderCredentialConfig{SecretName: "runtime-provider-openai-default-r1", SecretUID: "10000000-0000-4000-8000-000000000001", SecretResourceVersion: "1", ContentSHA256: strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	if err = r.ConfigureRoleImages(RoleImageConfig{PolicyRevision: 1, RoleRuntimeContractRevision: 1, PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64), BuildLeaseDuration: time.Minute, AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute, MaximumAttempts: 3, StagingRepository: "registry.invalid/staging", PromotedRepository: "registry.invalid/roles", DefaultImageReference: "registry.invalid/roles/system@sha256:" + strings.Repeat("c", 64), LeaseSigningKey: []byte(strings.Repeat("d", 32))}); err != nil {
		t.Fatal(err)
	}
	if err = r.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	seedObservedCatalogFixture(t, ctx, r)
	owner := resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.assistant.turns.add"}, "control-api-gateway")
	worker := resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
	service, err := serviceplatform.New(r)
	if err != nil {
		t.Fatal(err)
	}
	execute := func(kind command.Kind, actor value.Principal, key string, version *int64, payload any) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: actor, Mutation: value.Mutation{IdempotencyKey: "project-connection-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("%s %s: %v", kind, key, err)
		}
		return result
	}
	project := execute(command.CreateProject, owner, "project", nil, command.ProjectInput{Name: "Connection purpose", Language: "en"}).Project
	profile := execute(command.CreateProjectAssistant, owner, "profile", nil, command.ProjectAssistantInput{ProjectRef: project.Ref, Name: "Own helper", Purpose: "Synthetic integration setup", Instructions: "Configure approved project resources after confirmation."}).ProjectAssistant
	foreignProject := execute(command.CreateProject, owner, "foreign-project", nil, command.ProjectInput{Name: "Other purpose", Language: "en"}).Project
	foreign := execute(command.CreateProjectAssistant, owner, "foreign-profile", nil, command.ProjectAssistantInput{ProjectRef: foreignProject.Ref, Name: "Other helper", Purpose: "Synthetic other setup", Instructions: "Configure only this project after confirmation."}).ProjectAssistant
	conversation := execute(command.CreateAssistantConversation, owner, "conversation", nil, command.AssistantConversationInput{AssistantScope: "PROJECT", ProjectRef: project.Ref}).Conversation
	if !contains(conversation.Context.AllowedOperations, prepareProjectAssistantConnection) {
		t.Fatal("initial context omitted approved connection preparation")
	}
	execute(command.AddAssistantTurn, owner, "turn", nil, command.AssistantTurnInput{ConversationRef: conversation.Ref, Content: "Prepare repository connection", DeliveryMode: "QUEUE"})
	claims := execute(command.ClaimExecution, worker, "claim", nil, command.LeaseInput{WorkloadInstance: "project-connection-fixture", Limit: 1}).RuntimeItems
	if len(claims) != 1 {
		t.Fatalf("claim count %d", len(claims))
	}
	lease := claims[0]
	contextJSON, ok := lease["assistantContext"].(map[string]any)
	if !ok {
		t.Fatal("claim missing context")
	}
	operations, ok := contextJSON["allowedOperations"].([]any)
	allowed := false
	if ok {
		for _, operation := range operations {
			allowed = allowed || operation == prepareProjectAssistantConnection
		}
	}
	if !allowed {
		t.Fatal("immutable claim omitted approved preparation")
	}
	op := entity.AssistantPlanOperation{Key: "connection", Type: prepareProjectAssistantConnection, Title: "Prepare repository", Summary: "Prepare organization-owned connection for this helper", Parameters: map[string]any{"projectAssistantRef": profile.AgentRef, "definitionKey": "github", "name": "Own repository", "publicConfiguration": map[string]any{"owner": "fixture", "repository": "repository"}}}
	propose := func(key string, operation entity.AssistantPlanOperation) (command.Result, error) {
		return service.Execute(ctx, command.Command{Kind: command.ProposeAssistantPlan, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "project-connection-" + key}, Payload: command.ProposeAssistantPlanInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: lease["generation"].(int64), Summary: operation.Summary, Operations: []entity.AssistantPlanOperation{operation}}})
	}
	foreignOp := op
	foreignOp.Parameters = cloneAssistantFields(op.Parameters)
	foreignOp.Parameters["projectAssistantRef"] = foreign.AgentRef
	if _, err := propose("foreign", foreignOp); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("foreign helper proposal: %v", err)
	}
	generic := op
	generic.Type = "CREATE_INTEGRATION_CONNECTION"
	generic.Parameters = cloneAssistantFields(op.Parameters)
	delete(generic.Parameters, "projectAssistantRef")
	if _, err := propose("generic", generic); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("ordinary organization create broadened: %v", err)
	}
	prepared, err := propose("prepare", op)
	if err != nil || prepared.Plan == nil || prepared.Plan.State != "DRAFT" {
		t.Fatalf("prepare: %v", err)
	}
	plan := prepared.Plan
	if plan.ProjectRef != project.Ref || plan.Operations[0].Target.Ref != profile.AgentRef || plan.Operations[0].Parameters["scopeKind"] != "ORGANIZATION" {
		t.Fatal("prepared owner tuple mismatch")
	}
	readCounts := func() [5]int64 {
		t.Helper()
		var count [5]int64
		if err := pool.QueryRow(ctx, queryProjectAssistantConnectionReadback).Scan(&count[0], &count[1], &count[2], &count[3], &count[4]); err != nil {
			t.Fatal("read safe aggregate counts")
		}
		return count
	}
	before := readCounts()
	if before[1] != 0 {
		t.Fatal("proposal created an external effect")
	}
	signed := owner
	signed.ProjectRef = gateTestProjectID(t, ctx, r, owner, project.Ref)
	for _, kind := range []command.Kind{command.ValidateAssistantPlan, command.ApplyAssistantPlan} {
		if _, err := service.Execute(ctx, command.Command{Kind: kind, Principal: signed, Mutation: value.Mutation{IdempotencyKey: "project-connection-signed-" + string(kind), ExpectedVersion: &plan.Version}, Payload: command.AssistantPlanInput{PlanRef: plan.Ref, Revision: plan.Revision}}); !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
			t.Fatalf("signed project owner effect %s: %v", kind, err)
		}
	}
	if readCounts() != before {
		t.Fatal("rejected signed owner effect changed receipts/audit/data")
	}
	firstValidated := execute(command.ValidateAssistantPlan, owner, "validate-before-drift", &plan.Version, command.AssistantPlanInput{PlanRef: plan.Ref, Revision: plan.Revision}).Plan
	if firstValidated == nil || firstValidated.State != "VALID" {
		t.Fatal("initial exact snapshot not valid")
	}
	agent, err := service.GetAgent(ctx, owner, profile.AgentRef)
	if err != nil {
		t.Fatal(err)
	}
	execute(command.UpdateAgent, owner, "rename-agent", &agent.Version, command.AgentInput{Ref: agent.Ref, ProjectRef: agent.ProjectRef, Name: "Changed helper", Purpose: agent.Purpose, RoleDescription: agent.RoleDescription, AvatarURL: agent.AvatarURL, RoleDefinitionRef: agent.RoleDefinitionRef})
	beforeStale := readCounts()
	stale := execute(command.ApplyAssistantPlan, owner, "stale-apply", &firstValidated.Version, command.AssistantPlanInput{PlanRef: plan.Ref, Revision: plan.Revision})
	if stale.Plan == nil || stale.Plan.State != "STALE" || stale.PlanReceipt == nil || stale.PlanReceipt.Outcome != "CONFLICT" {
		t.Fatal("external helper drift did not close with STALE receipt")
	}
	afterStale := readCounts()
	if beforeStale[0] != afterStale[0] || beforeStale[1] != afterStale[1] {
		t.Fatal("STALE application created connection/purpose")
	}
	edited := stale.Plan.Operations[0]
	edited.Parameters = cloneAssistantFields(edited.Parameters)
	edited.After = cloneAssistantFields(edited.After)
	edited.Parameters["name"], edited.After["name"] = "Reviewed repository", "Reviewed repository"
	refreshed := execute(command.UpdateAssistantPlan, owner, "stale-edit", &stale.Plan.Version, command.AssistantPlanDraftInput{PlanRef: plan.Ref, Summary: "Explicit fresh owner revision", Operations: []entity.AssistantPlanOperation{edited}}).Plan
	if refreshed == nil || refreshed.State != "DRAFT" || refreshed.Revision != stale.Plan.Revision+1 || refreshed.Operations[0].Target.Name != "Changed helper" {
		t.Fatal("explicit STALE recovery did not create fresh closed revision")
	}
	plan = refreshed
	validated := execute(command.ValidateAssistantPlan, owner, "validate", &plan.Version, command.AssistantPlanInput{PlanRef: plan.Ref, Revision: plan.Revision}).Plan
	if validated == nil || validated.State != "VALID" {
		t.Fatalf("validation: %#v", validated)
	}
	payload := command.AssistantPlanInput{PlanRef: validated.Ref, Revision: validated.Revision}
	applied := execute(command.ApplyAssistantPlan, owner, "apply", &validated.Version, payload)
	if applied.Plan == nil || applied.Plan.State != "APPLIED" || applied.PlanReceipt == nil || len(applied.PlanReceipt.Operations) != 1 {
		t.Fatal("owner effect lacks canonical receipt")
	}
	connectionRef := applied.PlanReceipt.Operations[0].ResourceRef
	var actualProject, actualProfile, actualAgent, organizationRef string
	var profileVersion, agentVersion int64
	var organizationOwned bool
	if err := pool.QueryRow(ctx, queryProjectAssistantConnectionPurpose, connectionRef).Scan(&actualProject, &actualProfile, &actualAgent, &profileVersion, &agentVersion, &organizationRef, &organizationOwned); err != nil {
		t.Fatalf("purpose readback: %v", err)
	}
	if actualProject != project.Ref || actualProfile != profile.Ref || actualAgent != profile.AgentRef || profileVersion != profile.Version || !organizationOwned || organizationRef != plan.Operations[0].Parameters["organizationRef"] || agentVersion != mustAssistantInt64(plan.Operations[0].Parameters, "agentVersion") {
		t.Fatal("connection purpose/organization boundary mismatch")
	}
	after := readCounts()
	replay := execute(command.ApplyAssistantPlan, owner, "apply", &validated.Version, payload)
	if replay.PlanReceipt == nil || replay.PlanReceipt.Ref != applied.PlanReceipt.Ref || readCounts() != after {
		t.Fatal("lost ACK replay duplicated owner effects")
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.ApplyAssistantPlan, Principal: signed, Mutation: value.Mutation{IdempotencyKey: "project-connection-apply", ExpectedVersion: &validated.Version}, Payload: payload}); !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("signed replay escaped owner gate: %v", err)
	}
	if readCounts() != after {
		t.Fatal("signed replay changed owner graph")
	}
	resolved, err := r.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	current, err := r.resolveScope(ctx, resolved)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, queryOrganizationImageComponentOwner, current.organizationID, current.actorID, "MEMBER"); err != nil {
		t.Fatal("revoke synthetic owner")
	}
	revokedCounts := readCounts()
	for _, version := range []int64{validated.Version, 1} {
		if _, err := service.Execute(ctx, command.Command{Kind: command.ApplyAssistantPlan, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "project-connection-apply", ExpectedVersion: &version}, Payload: payload}); !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
			t.Fatalf("revoked owner replay/OCC: %v", err)
		}
	}
	if _, err := propose("revoked", op); !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("revoked source owner proposes connection: %v", err)
	}
	if readCounts() != revokedCounts {
		t.Fatal("revoked authority created receipts/events/effects")
	}
	if _, err := pool.Exec(ctx, queryOrganizationImageComponentOwner, current.organizationID, current.actorID, "OWNER"); err != nil {
		t.Fatal("restore synthetic owner")
	}
	// Архив проекта не превращает сохранённую цель подключения в полномочия.
	trashed := execute(command.TrashProject, owner, "trash", &project.Version, command.ProjectLifecycleInput{Ref: project.Ref}).Project
	if trashed.Lifecycle != "TRASHED" {
		t.Fatal("canonical project trash failed")
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.ApplyAssistantPlan, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "project-connection-apply", ExpectedVersion: &validated.Version}, Payload: payload}); !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("historical purpose resurrected project replay: %v", err)
	}
	if _, err := service.GetIntegrationConnection(ctx, owner, connectionRef); err != nil {
		t.Fatalf("organization connection incorrectly follows project lifecycle: %v", err)
	}
}
