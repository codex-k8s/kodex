package platform

import (
	"context"
	_ "embed"
	"encoding/json"
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
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed testdata/sql/system_assistant_grant_side_effects.sql
var querySystemAssistantGrantSideEffects string

// Только disposable PostgreSQL: реальный owner graph, синтетические lease/ACK,
// без вызова model, GitHub, Kubernetes или иных внешних effects.
func TestSystemAssistantIntegrationGrantsComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated system grant PostgreSQL")
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
	prepareObservedWarmFixture(t, ctx, r)
	principal := func(workload, operation, actor, tenant string) value.Principal {
		return resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: actor, ExternalTenantID: tenant, CallerWorkload: workload, Operation: operation}, workload)
	}
	owner := principal("control-api-gateway", "platform.command.system-assistant.integration-grants.change", "20000000-0000-4000-8000-000000000001", "20000000-0000-4000-8000-000000000002")
	worker := principal("runtime-controller", "platform.runtime.execution.claim", "kodex-system-subject", "kodex-installation")
	gateway := principal("integration-gateway", "platform.runtime.integrations.claim", "kodex-system-subject", "kodex-installation")
	warm := principal("runtime-controller", "platform.runtime.warm.report", "kodex-system-subject", "kodex-installation")
	s, err := serviceplatform.New(r)
	if err != nil {
		t.Fatal(err)
	}
	assistant, err := s.GetSystemAssistant(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReportWarmRuntime(ctx, warm, command.WarmRuntimeInput{WorkloadInstance: "catalog-observed-warm-fixture", RuntimeRevision: assistant.DesiredRuntimeRevision, State: "READY"}); err != nil {
		t.Fatal(err)
	}
	execute := func(kind command.Kind, actor value.Principal, key string, version *int64, payload any) command.Result {
		t.Helper()
		result, err := s.Execute(ctx, command.Command{Kind: kind, Principal: actor, Mutation: value.Mutation{IdempotencyKey: "system-policy-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("execute %s (%s): %v", kind, key, err)
		}
		return result
	}
	connection := execute(command.CreateConnection, owner, "connection", nil, command.ConnectionInput{DefinitionKey: "github", Name: "System policy repository", PublicConfiguration: map[string]any{"owner": "fixture", "repository": "repository"}}).Connection
	if _, err = pool.Exec(ctx, queryIntegrationGrantPolicyCredentialFixture, connection.Ref); err != nil {
		t.Fatal("prepare synthetic credential metadata")
	}
	project := execute(command.CreateProject, owner, "project", nil, command.ProjectInput{Name: "Signed boundary fixture", Language: "en"}).Project
	resolvedOwner, err := r.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	current, err := r.resolveScope(ctx, resolvedOwner)
	if err != nil {
		t.Fatal(err)
	}
	counts := func() [5]int64 {
		t.Helper()
		var count [5]int64
		if err := pool.QueryRow(ctx, querySystemAssistantGrantSideEffects, current.organizationID).Scan(&count[0], &count[1], &count[2], &count[3], &count[4]); err != nil {
			t.Fatal("read synthetic grant side effects")
		}
		return count
	}
	signedProject := owner
	signedProject.ProjectRef = gateTestProjectID(t, ctx, r, owner, project.Ref)
	if _, err = s.GetSystemAssistantIntegrationGrantCandidates(ctx, signedProject, connection.Ref, "", query.Page{Size: 100}); !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("project authority read system candidates: %v", err)
	}
	readContext := func(actor value.Principal, conversationRef string) entity.AssistantContextDescriptor {
		t.Helper()
		resolved, err := r.ResolvePrincipal(ctx, actor)
		if err != nil {
			t.Fatal(err)
		}
		current, err := r.resolveScope(ctx, resolved)
		if err != nil {
			t.Fatal(err)
		}
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		context, _, err := r.assistantConversationContext(ctx, tx, current, conversationRef, "")
		if err != nil {
			t.Fatal(err)
		}
		return context
	}
	assertRuntimeContext := func(lease map[string]any, advertised bool) {
		t.Helper()
		var raw []byte
		if err := pool.QueryRow(ctx, queryAssistantConfigurationComponentRevision, stringMap(lease, "leaseRef")).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var snapshot struct {
			AssistantContext struct {
				AllowedOperations []string `json:"allowedOperations"`
			} `json:"assistantContext"`
		}
		if json.Unmarshal(raw, &snapshot) != nil || contains(snapshot.AssistantContext.AllowedOperations, "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT") != advertised {
			t.Fatal("immutable runtime did not preserve freshly authorized grant operation")
		}
	}
	for _, policy := range []string{"NONE", "HUMAN_EACH_EFFECT", "HUMAN_SCOPED"} {
		t.Run(policy, func(t *testing.T) {
			candidates, err := s.GetSystemAssistantIntegrationGrantCandidates(ctx, owner, connection.Ref, "github.pull_request.review.create", query.Page{Size: 10})
			if err != nil {
				t.Fatal(err)
			}
			if candidates.ScopeKind != "ORGANIZATION" || candidates.OrganizationRef == "" || candidates.AssistantRef != assistant.Ref || len(candidates.Items) != 1 || !candidates.Items[0].Grantable {
				t.Fatal("system grant candidates lost exact owner eligibility")
			}
			paths := []string{}
			if policy == "HUMAN_SCOPED" {
				paths = []string{"/pull_request_number"}
			}
			input := command.SystemAssistantIntegrationGrantInput{ConnectionRef: connection.Ref, CapabilityKey: "github.pull_request.review.create", Enabled: true, ApprovalPolicy: policy, ApprovalScopePaths: paths}
			granted := execute(command.ChangeSystemAssistantIntegrationGrant, owner, policy+"-grant", &candidates.ConnectionVersion, input).Connection
			if len(granted.Grants) != 1 || granted.Grants[0].TargetRef != assistant.Ref || granted.Grants[0].ApprovalPolicy != policy || granted.Grants[0].ConnectionVersion != granted.Version {
				t.Fatal("system grant response lost selected/versioned identity")
			}
			if _, err = s.Execute(ctx, command.Command{Kind: command.ChangeSystemAssistantIntegrationGrant, Principal: signedProject, Mutation: value.Mutation{IdempotencyKey: "system-policy-" + policy + "-grant", ExpectedVersion: &candidates.ConnectionVersion}, Payload: input}); !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
				t.Fatal("signed project replay returned organizational grant")
			}
			replay := execute(command.ChangeSystemAssistantIntegrationGrant, owner, policy+"-grant", &candidates.ConnectionVersion, input).Connection
			if replay.Grants[0].Version != granted.Grants[0].Version {
				t.Fatal("system grant replay changed immutable version")
			}
			conversation := execute(command.CreateAssistantConversation, owner, policy+"-conversation", nil, command.AssistantConversationInput{AssistantScope: "SYSTEM"}).Conversation
			if !contains(conversation.Context.AllowedOperations, "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT") || !contains(readContext(owner, conversation.Ref).AllowedOperations, "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT") {
				t.Fatal("SYSTEM create/read context omitted executable grant operation")
			}
			if contains(readContext(signedProject, conversation.Ref).AllowedOperations, "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT") {
				t.Fatal("signed PROJECT context advertised SYSTEM grant operation")
			}
			turn := execute(command.AddAssistantTurn, owner, policy+"-turn", nil, command.AssistantTurnInput{ConversationRef: conversation.Ref, Content: "Synthetic system integration effect", DeliveryMode: "QUEUE"}).Conversation
			leases := execute(command.ClaimExecution, worker, policy+"-runtime", nil, command.LeaseInput{WorkloadInstance: "system-policy-worker", Limit: 1}).RuntimeItems
			if len(leases) != 1 {
				t.Fatal("system runtime not claimed")
			}
			lease := leases[0]
			assertRuntimeContext(lease, true)
			grants, ok := lease["integrationGrants"].([]map[string]string)
			if !ok || len(grants) != 1 || grants[0]["approvalPolicy"] != policy || grants[0]["grantVersion"] == "" {
				t.Fatal("system runtime did not pin selected grant")
			}
			invocation, err := s.ResolveIntegrationInvocation(ctx, worker, map[string]string{"run_ref": turn.Turns[0].RunRef, "node_ref": stringMap(lease, "nodeRef"), "connection_ref": connection.Ref, "capability_key": input.CapabilityKey, "idempotency_key": "system-effect-" + policy}, map[string]any{"pull_request_number": 1, "sha": strings.Repeat("a", 40), "body": "Synthetic comment review", "event": "COMMENT"})
			if err != nil {
				t.Fatal(err)
			}
			if policy != "NONE" {
				gate, err := s.GetOwnerGate(ctx, owner, stringMap(invocation, "gateRef"))
				if err != nil {
					t.Fatal(err)
				}
				if gate.ScopeKind != "ORGANIZATION" || gate.OrganizationRef != candidates.OrganizationRef || gate.ProjectRef != "" || gate.RunRef != turn.Turns[0].RunRef {
					t.Fatal("organizational gate owner tuple differs")
				}
				if _, err = s.GetOwnerGate(ctx, signedProject, gate.Ref); !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
					t.Fatal("signed project read organizational gate")
				}
				if _, err = s.Execute(ctx, command.Command{Kind: command.ResolveOwnerGate, Principal: signedProject, Mutation: value.Mutation{IdempotencyKey: "system-denied-gate-" + policy, ExpectedVersion: &gate.Version}, Payload: command.GateResolutionInput{GateRef: gate.Ref, Decision: "APPROVE"}}); !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
					t.Fatal("signed project resolved organizational gate")
				}
				listed, total, _, err := s.ListOwnerGates(ctx, owner, query.Filter{Page: query.Page{Size: 20}})
				if err != nil || total < 1 || len(listed) < 1 {
					t.Fatal("organizational gate absent from authoritative list")
				}
				approved := execute(command.ResolveOwnerGate, owner, policy+"-approval", &gate.Version, command.GateResolutionInput{GateRef: gate.Ref, Decision: "APPROVE"})
				if approved.Gate == nil || approved.Gate.ScopeKind != "ORGANIZATION" || approved.Gate.OrganizationRef != gate.OrganizationRef {
					t.Fatal("gate receipt lost organizational tuple")
				}
			}
			work, err := s.ClaimIntegrationInvocations(ctx, gateway, "system-policy-gateway", 1)
			if err != nil || len(work) != 1 {
				t.Fatalf("approved system effect not claimable: %v", err)
			}
			if stringMap(work[0], "approvalPolicy") != policy || stringMap(work[0], "grantRef") != granted.Grants[0].Ref || work[0]["grantVersion"] != granted.Grants[0].Version {
				t.Fatal("system invocation claim lost exact grant pins")
			}
			execute(command.CompleteIntegrationInvocation, gateway, policy+"-failure", nil, command.IntegrationInvocationInput{InvocationRef: stringMap(work[0], "invocationRef"), LeaseRef: stringMap(work[0], "leaseRef"), Fence: stringMap(work[0], "fence"), Generation: work[0]["generation"].(int64), SafeErrorCode: "INTEGRATION_REQUEST_REJECTED"})
			run, err := s.GetRun(ctx, owner, turn.Turns[0].RunRef)
			if err != nil {
				t.Fatal(err)
			}
			execute(command.CancelRun, owner, policy+"-cancel", &run.Version, command.RunCommandInput{RunRef: run.Ref, Reason: "Synthetic system fixture cleanup"})
		})
	}
	t.Run("owner-confirmed-plan", func(t *testing.T) {
		conversation := execute(command.CreateAssistantConversation, owner, "plan-conversation", nil, command.AssistantConversationInput{AssistantScope: "SYSTEM"}).Conversation
		execute(command.AddAssistantTurn, owner, "plan-turn", nil, command.AssistantTurnInput{ConversationRef: conversation.Ref, Content: "Synthetic grant preparation", DeliveryMode: "QUEUE"})
		leases := execute(command.ClaimExecution, worker, "plan-runtime", nil, command.LeaseInput{WorkloadInstance: "system-policy-plan-worker", Limit: 1}).RuntimeItems
		if len(leases) != 1 {
			t.Fatal("plan runtime not claimed")
		}
		operation := entity.AssistantPlanOperation{Key: "grant-system", Type: "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT", Title: "Права интеграции Kodex", Summary: "Configure explicit system integration policy", Parameters: map[string]any{"connectionRef": connection.Ref, "capabilityKey": "github.issue.comment.create", "enabled": true, "approvalPolicy": "NONE"}}
		proposal := executeWorkerAssistantPlan(t, ctx, s, worker, leases[0], "system-policy-plan-propose", operation)
		if proposal.Plan == nil || len(proposal.Plan.Operations) != 1 {
			t.Fatal("typed grant plan missing")
		}
		plan := *proposal.Plan
		operation = plan.Operations[0]
		if operation.Target.Kind != "INTEGRATION_CONNECTION" || operation.Target.Ref != connection.Ref || assistantString(operation.Parameters, "scopeKind") != "ORGANIZATION" || assistantString(operation.Parameters, "systemAssistantRef") != assistant.Ref || assistantString(operation.Parameters, "grantRef") != "" {
			t.Fatal("typed plan lost server-owned tuple")
		}
		if _, supplied := operation.Parameters["projectRef"]; supplied {
			t.Fatal("SYSTEM grant plan invented project")
		}
		candidate, err := s.GetSystemAssistantIntegrationGrantCandidates(ctx, owner, connection.Ref, "github.issue.comment.create", query.Page{Size: 10})
		if err != nil || len(candidate.Items) != 1 || candidate.Items[0].CurrentGrantRef != "" {
			t.Fatal("prepare created grant before owner confirmation")
		}
		operation.Parameters = cloneAssistantFields(operation.Parameters)
		operation.Parameters["approvalPolicy"] = "HUMAN_SCOPED"
		operation.Parameters["approvalScopePaths"] = []string{"/issue_number"}
		edited := execute(command.UpdateAssistantPlan, owner, "plan-edit", &plan.Version, command.AssistantPlanDraftInput{PlanRef: plan.Ref, Summary: plan.Summary, Operations: []entity.AssistantPlanOperation{operation}}).Plan
		if edited == nil || edited.Revision <= plan.Revision {
			t.Fatal("owner edit did not create immutable revision")
		}
		validated := execute(command.ValidateAssistantPlan, owner, "plan-validate", &edited.Version, command.AssistantPlanInput{PlanRef: edited.Ref, Revision: edited.Revision}).Plan
		if validated == nil || validated.State != "VALID" {
			t.Fatalf("typed grant plan invalid: %+v", validated)
		}
		applied := execute(command.ApplyAssistantPlan, owner, "plan-apply", &validated.Version, command.AssistantPlanInput{PlanRef: validated.Ref, Revision: validated.Revision})
		if applied.Plan == nil || applied.Plan.State != "APPLIED" || applied.PlanReceipt == nil || len(applied.PlanReceipt.Operations) != 1 || applied.PlanReceipt.Operations[0].ResourceRef == "" || applied.Conversation == nil || applied.Conversation.ProjectRef != "" {
			t.Fatal("confirmed typed grant did not apply without rebinding source")
		}
		candidate, err = s.GetSystemAssistantIntegrationGrantCandidates(ctx, owner, connection.Ref, "github.issue.comment.create", query.Page{Size: 10})
		if err != nil || candidate.Items[0].CurrentGrantRef != applied.PlanReceipt.Operations[0].ResourceRef || candidate.Items[0].CurrentApprovalPolicy != "HUMAN_SCOPED" || !candidate.Items[0].CurrentGrantEnabled {
			t.Fatal("applied receipt does not identify canonical grant")
		}
		replay := execute(command.ApplyAssistantPlan, owner, "plan-apply", &validated.Version, command.AssistantPlanInput{PlanRef: validated.Ref, Revision: validated.Revision})
		if replay.PlanReceipt == nil || replay.PlanReceipt.Ref != applied.PlanReceipt.Ref {
			t.Fatal("plan replay created another grant receipt")
		}
		before := counts()
		if _, err = pool.Exec(ctx, queryOrganizationImageComponentOwner, current.organizationID, current.actorID, "MEMBER"); err != nil {
			t.Fatal("revoke synthetic owner role")
		}
		if contains(readContext(owner, conversation.Ref).AllowedOperations, "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT") {
			t.Fatal("downgraded owner read/rejoin still advertised SYSTEM grant operation")
		}
		if _, err = s.GetSystemAssistantIntegrationGrantCandidates(ctx, owner, connection.Ref, "", query.Page{Size: 10}); !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
			t.Fatal("revoked owner still reads system grant candidates")
		}
		for _, version := range []int64{validated.Version, 1} {
			if _, err = s.Execute(ctx, command.Command{Kind: command.ApplyAssistantPlan, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "system-policy-plan-apply", ExpectedVersion: &version}, Payload: command.AssistantPlanInput{PlanRef: validated.Ref, Revision: validated.Revision}}); !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
				t.Fatalf("revoked owner replay/OCC still returns organizational plan: %v", err)
			}
			if _, err = s.Execute(ctx, command.Command{Kind: command.ChangeSystemAssistantIntegrationGrant, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "system-policy-HUMAN_SCOPED-grant", ExpectedVersion: &version}, Payload: command.SystemAssistantIntegrationGrantInput{ConnectionRef: connection.Ref, CapabilityKey: "github.pull_request.review.create", Enabled: true, ApprovalPolicy: "HUMAN_SCOPED", ApprovalScopePaths: []string{"/pull_request_number"}}}); !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
				t.Fatalf("revoked owner grant replay/OCC returned organizational data: %v", err)
			}
		}
		if counts() != before {
			t.Fatal("revoked organizational access created durable effects")
		}
		if _, err = pool.Exec(ctx, queryOrganizationImageComponentOwner, current.organizationID, current.actorID, "OWNER"); err != nil {
			t.Fatal("restore synthetic owner role")
		}
		collaborativeKeys := []string{"github.issue.comment.create", "github.issue.comment.update", "github.pull_request.create", "github.pull_request.update", "github.pull_request.review.create"}
		proposeMany := func(key, policy string) entity.AssistantPlan {
			t.Helper()
			operations := []entity.AssistantPlanOperation{}
			for index, capability := range collaborativeKeys {
				operations = append(operations, entity.AssistantPlanOperation{Key: fmt.Sprintf("grant-%d", index), Type: "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT", Title: "Права интеграции Kodex", Summary: "Closed collaborative policy", Parameters: map[string]any{"connectionRef": connection.Ref, "capabilityKey": capability, "enabled": true, "approvalPolicy": policy}})
			}
			result := execute(command.ProposeAssistantPlan, worker, key, nil, command.ProposeAssistantPlanInput{LeaseRef: stringMap(leases[0], "leaseRef"), Fence: stringMap(leases[0], "fence"), Generation: leases[0]["generation"].(int64), Summary: "Configure five closed collaborative operations", Operations: operations})
			return *result.Plan
		}
		duplicateOperations := []entity.AssistantPlanOperation{}
		for index := 0; index < 2; index++ {
			duplicateOperations = append(duplicateOperations, entity.AssistantPlanOperation{Key: fmt.Sprintf("duplicate-%d", index), Type: "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT", Title: "Права интеграции Kodex", Summary: "Duplicate exact grant", Parameters: map[string]any{"connectionRef": connection.Ref, "capabilityKey": "github.issue.comment.update", "enabled": true, "approvalPolicy": "NONE"}})
		}
		duplicatePlan := execute(command.ProposeAssistantPlan, worker, "duplicate-prepare", nil, command.ProposeAssistantPlanInput{LeaseRef: stringMap(leases[0], "leaseRef"), Fence: stringMap(leases[0], "fence"), Generation: leases[0]["generation"].(int64), Summary: "Duplicate exact integration grant", Operations: duplicateOperations}).Plan
		duplicateValidated := execute(command.ValidateAssistantPlan, owner, "duplicate-validate", &duplicatePlan.Version, command.AssistantPlanInput{PlanRef: duplicatePlan.Ref, Revision: duplicatePlan.Revision}).Plan
		if duplicateValidated == nil || duplicateValidated.State == "VALID" || !contains(duplicateValidated.ValidationProblems, "operation-2-duplicate-integration-grant") {
			t.Fatal("duplicate exact grant reached VALID")
		}
		validateMany := func(key string, plan entity.AssistantPlan) entity.AssistantPlan {
			t.Helper()
			result := execute(command.ValidateAssistantPlan, owner, key, &plan.Version, command.AssistantPlanInput{PlanRef: plan.Ref, Revision: plan.Revision})
			if result.Plan == nil || result.Plan.State != "VALID" {
				t.Fatalf("compound grant plan invalid: %+v", result.Plan)
			}
			return *result.Plan
		}
		compound := validateMany("compound-validate", proposeMany("compound-prepare", "NONE"))
		immutable, _ := json.Marshal(compound.Operations)
		compoundApplied := execute(command.ApplyAssistantPlan, owner, "compound-apply", &compound.Version, command.AssistantPlanInput{PlanRef: compound.Ref, Revision: compound.Revision})
		if compoundApplied.Plan == nil || compoundApplied.Plan.State != "APPLIED" || compoundApplied.PlanReceipt == nil || len(compoundApplied.PlanReceipt.Operations) != 5 || !assistantJSONEqual(json.RawMessage(immutable), compoundApplied.Plan.Operations) {
			t.Fatal("five-capability atomic grant plan self-conflicted or rewrote immutable snapshots")
		}
		all, err := s.GetSystemAssistantIntegrationGrantCandidates(ctx, owner, connection.Ref, "", query.Page{Size: 100})
		if err != nil {
			t.Fatal(err)
		}
		oldVersions := map[string]int64{}
		for _, item := range all.Items {
			if item.CurrentGrantEnabled {
				oldVersions[item.Capability.Key] = item.CurrentGrantVersion
			}
		}
		if len(oldVersions) != 5 {
			t.Fatal("five closed grants were not published atomically")
		}
		blocked := validateMany("blocked-validate", proposeMany("blocked-prepare", "HUMAN_EACH_EFFECT"))
		otherConversation := execute(command.CreateAssistantConversation, owner, "blocked-other-conversation", nil, command.AssistantConversationInput{AssistantScope: "SYSTEM"}).Conversation
		otherTurn := execute(command.AddAssistantTurn, owner, "blocked-other-turn", nil, command.AssistantTurnInput{ConversationRef: otherConversation.Ref, Content: "Synthetic active last-capability intent", DeliveryMode: "QUEUE"}).Conversation
		otherLease := execute(command.ClaimExecution, worker, "blocked-other-runtime", nil, command.LeaseInput{WorkloadInstance: "system-policy-other-worker", Limit: 1}).RuntimeItems
		if len(otherLease) != 1 {
			t.Fatal("independent synthetic runtime not claimed")
		}
		pending, err := s.ResolveIntegrationInvocation(ctx, worker, map[string]string{"run_ref": otherTurn.Turns[0].RunRef, "node_ref": stringMap(otherLease[0], "nodeRef"), "connection_ref": connection.Ref, "capability_key": "github.pull_request.review.create", "idempotency_key": "blocked-last-capability-effect"}, map[string]any{"pull_request_number": 1, "sha": strings.Repeat("a", 40), "body": "Synthetic comment only", "event": "COMMENT"})
		if err != nil {
			t.Fatal(err)
		}
		conflict := execute(command.ApplyAssistantPlan, owner, "blocked-apply", &blocked.Version, command.AssistantPlanInput{PlanRef: blocked.Ref, Revision: blocked.Revision})
		if conflict.Plan == nil || conflict.Plan.State != "STALE" || conflict.PlanReceipt == nil || conflict.PlanReceipt.Outcome != "CONFLICT" {
			t.Fatal("active final capability did not close atomic plan with conflict")
		}
		all, err = s.GetSystemAssistantIntegrationGrantCandidates(ctx, owner, connection.Ref, "", query.Page{Size: 100})
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range all.Items {
			if version, exists := oldVersions[item.Capability.Key]; exists && (item.CurrentGrantVersion != version || item.CurrentApprovalPolicy != "NONE") {
				t.Fatal("later policy conflict committed an earlier grant effect")
			}
		}
		otherRun, err := s.GetRun(ctx, owner, otherTurn.Turns[0].RunRef)
		if err != nil {
			t.Fatal(err)
		}
		cancelled := execute(command.CancelRun, owner, "blocked-other-cancel", &otherRun.Version, command.RunCommandInput{RunRef: otherRun.Ref, Reason: "Synthetic pending effect cleanup"})
		cancelCounts := assertIntegrationCancelGraph(t, ctx, pool, stringMap(pending, "invocationRef"), otherRun.Ref, "CANCELLED")
		cancelReplay := execute(command.CancelRun, owner, "blocked-other-cancel", &otherRun.Version, command.RunCommandInput{RunRef: otherRun.Ref, Reason: "Synthetic pending effect cleanup"})
		if cancelReplay.Run.Version != cancelled.Run.Version || assertIntegrationCancelGraph(t, ctx, pool, stringMap(pending, "invocationRef"), otherRun.Ref, "CANCELLED") != cancelCounts {
			t.Fatal("system CancelRun replay changed effects or duplicated events")
		}
		if work, err := s.ClaimIntegrationInvocations(ctx, gateway, "cancelled-system-policy-worker", 1); err != nil || len(work) != 0 {
			t.Fatal("cancelled system READY effect was still claimable")
		}
		badOperation := conflict.Plan.Operations[0]
		badOperation.Parameters = cloneAssistantFields(badOperation.Parameters)
		badOperation.Parameters["organizationRef"] = "org_foreign_snapshot"
		badOperations := append([]entity.AssistantPlanOperation{}, conflict.Plan.Operations...)
		badOperations[0] = badOperation
		if _, err = s.Execute(ctx, command.Command{Kind: command.UpdateAssistantPlan, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "system-policy-stale-bad-owner", ExpectedVersion: &conflict.Plan.Version}, Payload: command.AssistantPlanDraftInput{PlanRef: conflict.Plan.Ref, Summary: conflict.Plan.Summary, Operations: badOperations}}); !errors.Is(err, errs.ErrForbidden) {
			t.Fatal("STALE grant refresh accepted caller-owned scope")
		}
		refreshed := execute(command.UpdateAssistantPlan, owner, "blocked-new-revision", &conflict.Plan.Version, command.AssistantPlanDraftInput{PlanRef: conflict.Plan.Ref, Summary: conflict.Plan.Summary, Operations: conflict.Plan.Operations}).Plan
		if refreshed == nil || refreshed.State != "DRAFT" || refreshed.Revision != blocked.Revision+1 {
			t.Fatal("STALE grant plan did not create new immutable revision")
		}
		freshValid := validateMany("blocked-new-validate", *refreshed)
		freshApplied := execute(command.ApplyAssistantPlan, owner, "blocked-new-apply", &freshValid.Version, command.AssistantPlanInput{PlanRef: freshValid.Ref, Revision: freshValid.Revision})
		if freshApplied.Plan == nil || freshApplied.Plan.State != "APPLIED" {
			t.Fatal("refreshed closed grant plan failed to apply after active effect cancellation")
		}
		run, err := s.GetRun(ctx, owner, stringMap(leases[0], "runRef"))
		if err != nil {
			t.Fatal(err)
		}
		execute(command.CancelRun, owner, "plan-cancel", &run.Version, command.RunCommandInput{RunRef: run.Ref, Reason: "Synthetic plan fixture cleanup"})
	})
	t.Run("cancel_system_graph", func(t *testing.T) {
		exerciseIntegrationCancelGraph(t, ctx, pool, s, owner, worker, gateway, connection.Ref, "cancel-system", func(key, policy string) {
			fresh, err := s.GetSystemAssistantIntegrationGrantCandidates(ctx, owner, connection.Ref, "github.pull_request.review.create", query.Page{Size: 10})
			if err != nil {
				t.Fatal(err)
			}
			paths := []string{}
			if policy == "HUMAN_SCOPED" {
				paths = []string{"/pull_request_number"}
			}
			execute(command.ChangeSystemAssistantIntegrationGrant, owner, "cancel-system-"+key+"-grant", &fresh.ConnectionVersion, command.SystemAssistantIntegrationGrantInput{ConnectionRef: connection.Ref, CapabilityKey: "github.pull_request.review.create", Enabled: true, ApprovalPolicy: policy, ApprovalScopePaths: paths})
		}, func(key string) string {
			conversation := execute(command.CreateAssistantConversation, owner, "cancel-system-"+key+"-conversation", nil, command.AssistantConversationInput{AssistantScope: "SYSTEM"}).Conversation
			turn := execute(command.AddAssistantTurn, owner, "cancel-system-"+key+"-turn", nil, command.AssistantTurnInput{ConversationRef: conversation.Ref, Content: "Synthetic cancelled integration graph", DeliveryMode: "QUEUE"}).Conversation
			return turn.Turns[0].RunRef
		})
	})
	t.Run("project_context_does_not_advertise_system_grant", func(t *testing.T) {
		profile := execute(command.CreateProjectAssistant, owner, "projection-project-profile", nil, command.ProjectAssistantInput{ProjectRef: project.Ref, Name: "Project projection", Purpose: "Check own assistant boundaries", Instructions: "Use only project resources"}).ProjectAssistant
		conversation := execute(command.CreateAssistantConversation, owner, "projection-project-conversation", nil, command.AssistantConversationInput{AssistantScope: "PROJECT", ProjectRef: project.Ref}).Conversation
		if profile == nil || contains(conversation.Context.AllowedOperations, "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT") || contains(readContext(owner, conversation.Ref).AllowedOperations, "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT") {
			t.Fatal("PROJECT create/read context advertised SYSTEM grant operation")
		}
		turn := execute(command.AddAssistantTurn, owner, "projection-project-turn", nil, command.AssistantTurnInput{ConversationRef: conversation.Ref, Content: "Synthetic project context", DeliveryMode: "QUEUE"}).Conversation
		leases := execute(command.ClaimExecution, worker, "projection-project-runtime", nil, command.LeaseInput{WorkloadInstance: "projection-project-runtime", Limit: 1}).RuntimeItems
		if len(leases) != 1 {
			t.Fatal("PROJECT runtime not claimed")
		}
		assertRuntimeContext(leases[0], false)
		run, err := s.GetRun(ctx, owner, turn.Turns[0].RunRef)
		if err != nil {
			t.Fatal(err)
		}
		execute(command.CancelRun, owner, "projection-project-cancel", &run.Version, command.RunCommandInput{RunRef: run.Ref, Reason: "Synthetic projection cleanup"})
	})
}
