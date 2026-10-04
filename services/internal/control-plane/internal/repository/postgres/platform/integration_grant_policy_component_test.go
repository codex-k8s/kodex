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

//go:embed testdata/sql/integration_grant_policy_credential_fixture.sql
var queryIntegrationGrantPolicyCredentialFixture string

// Проверка не вызывает внешний GitHub или model: claim/ACK синтетические,
// owner выдаёт реальные доменные grants на текущий immutable GitHub package.
func TestIntegrationGrantApprovalPoliciesComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated grant-policy PostgreSQL")
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
	principal := func(workload, operation, actor, tenant string) value.Principal {
		return resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: actor, ExternalTenantID: tenant, CallerWorkload: workload, Operation: operation}, workload)
	}
	owner := principal("control-api-gateway", "platform.command.integrations.create", "20000000-0000-4000-8000-000000000001", "20000000-0000-4000-8000-000000000002")
	worker := principal("runtime-controller", "platform.runtime.execution.claim", "kodex-system-subject", "kodex-installation")
	gateway := principal("integration-gateway", "platform.runtime.integrations.claim", "kodex-system-subject", "kodex-installation")
	service, err := serviceplatform.New(r)
	if err != nil {
		t.Fatal(err)
	}
	execute := func(kind command.Kind, p value.Principal, key string, version *int64, payload any) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: p, Mutation: value.Mutation{IdempotencyKey: "policy-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("execute %s: %v", kind, err)
		}
		return result
	}
	connection := execute(command.CreateConnection, owner, "connection", nil, command.ConnectionInput{DefinitionKey: "github", Name: "Policy repository", PublicConfiguration: map[string]any{"owner": "fixture", "repository": "repository"}}).Connection
	if _, err = pool.Exec(ctx, queryIntegrationGrantPolicyCredentialFixture, connection.Ref); err != nil {
		t.Fatal("prepare synthetic credential metadata")
	}
	project := execute(command.CreateProject, owner, "project", nil, command.ProjectInput{Name: "Grant policies", Language: "en"}).Project
	agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "policy-agent", "Policy operator")
	for _, policy := range []string{"NONE", "HUMAN_EACH_EFFECT", "HUMAN_SCOPED"} {
		t.Run(policy, func(t *testing.T) {
			fresh, err := service.GetIntegrationConnection(ctx, owner, connection.Ref)
			if err != nil {
				t.Fatal(err)
			}
			paths := []string{}
			if policy == "HUMAN_SCOPED" {
				paths = []string{"/pull_request_number"}
			}
			input := command.IntegrationGrantInput{ConnectionRef: connection.Ref, CapabilityKey: "github.pull_request.review.create", AgentRef: agent.Ref, Enabled: true, ApprovalPolicy: policy, ApprovalScopePaths: paths}
			granted := execute(command.ChangeIntegrationGrant, owner, policy+"-grant", &fresh.Version, input).Connection
			if len(granted.Grants) != 1 || granted.Grants[0].ApprovalPolicy != policy {
				t.Fatal("selected grant policy was not persisted")
			}
			replayed := execute(command.ChangeIntegrationGrant, owner, policy+"-grant", &fresh.Version, input).Connection
			if len(replayed.Grants) != 1 || replayed.Grants[0].Version != granted.Grants[0].Version {
				t.Fatal("grant receipt replay changed version")
			}
			for _, invalid := range []string{"", "UNKNOWN"} {
				bad := input
				bad.ApprovalPolicy = invalid
				if _, err := service.Execute(ctx, command.Command{Kind: command.ChangeIntegrationGrant, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "policy-invalid-" + policy + invalid, ExpectedVersion: &granted.Version}, Payload: bad}); !errors.Is(err, errs.ErrInvalid) {
					t.Fatal("invalid selected policy accepted")
				}
			}
			run := execute(command.LaunchRun, owner, policy+"-run", nil, command.LaunchRunInput{ProjectRef: project.Ref, Title: "Policy effect", Task: "Synthetic effect", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}}).Run
			claimed := execute(command.ClaimExecution, worker, policy+"-runtime", nil, command.LeaseInput{WorkloadInstance: "policy-runtime", Limit: 1}).RuntimeItems
			if len(claimed) != 1 {
				t.Fatal("runtime was not claimed")
			}
			grants, ok := claimed[0]["integrationGrants"].([]map[string]string)
			if !ok || len(grants) != 1 || grants[0]["approvalPolicy"] != policy || grants[0]["grantVersion"] == "" {
				t.Fatal("immutable runtime lost grant policy/version")
			}
			call := func(key, event string) (map[string]any, error) {
				return service.ResolveIntegrationInvocation(ctx, worker, map[string]string{"run_ref": run.Ref, "node_ref": stringMap(claimed[0], "nodeRef"), "connection_ref": connection.Ref, "capability_key": input.CapabilityKey, "idempotency_key": key}, map[string]any{"pull_request_number": 1, "sha": strings.Repeat("a", 40), "body": "Fixture review", "event": event})
			}
			if policy == "NONE" {
				if _, err := call("policy-forbidden-review", "APPROVE"); !errors.Is(err, errs.ErrForbidden) {
					t.Fatal("autonomous approve accepted")
				}
			}
			invocation, err := call("policy-effect-"+policy, "COMMENT")
			if err != nil {
				t.Fatal(err)
			}
			wantState := "READY"
			if policy != "NONE" {
				wantState = "WAITING_APPROVAL"
			}
			if stringMap(invocation, "state") != wantState {
				t.Fatal("policy gate state differs")
			}
			bad := input
			bad.ApprovalPolicy = "HUMAN_EACH_EFFECT"
			bad.ApprovalScopePaths = nil
			if policy == "HUMAN_EACH_EFFECT" {
				bad.ApprovalPolicy = "NONE"
			}
			if _, err := service.Execute(ctx, command.Command{Kind: command.ChangeIntegrationGrant, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "policy-active-change-" + policy, ExpectedVersion: &granted.Version}, Payload: bad}); !errors.Is(err, errs.ErrConflict) {
				t.Fatal("active grant policy changed")
			}
			if policy != "NONE" {
				before, err := service.ClaimIntegrationInvocations(ctx, gateway, "policy-gateway", 1)
				if err != nil || len(before) != 0 {
					t.Fatal("unapproved effect was claimed")
				}
				gate, err := service.GetOwnerGate(ctx, owner, stringMap(invocation, "gateRef"))
				if err != nil {
					t.Fatal(err)
				}
				execute(command.ResolveOwnerGate, owner, policy+"-approval", &gate.Version, command.GateResolutionInput{GateRef: gate.Ref, Decision: "APPROVE"})
			}
			work, err := service.ClaimIntegrationInvocations(ctx, gateway, "policy-gateway", 1)
			if err != nil || len(work) != 1 {
				t.Fatalf("approved effect claim: %v", err)
			}
			if stringMap(work[0], "approvalPolicy") != policy || stringMap(work[0], "grantRef") != granted.Grants[0].Ref || work[0]["grantVersion"] != granted.Grants[0].Version {
				t.Fatal("claim lost exact selected grant pins")
			}
			execute(command.CompleteIntegrationInvocation, gateway, policy+"-failure", nil, command.IntegrationInvocationInput{InvocationRef: stringMap(work[0], "invocationRef"), LeaseRef: stringMap(work[0], "leaseRef"), Fence: stringMap(work[0], "fence"), Generation: work[0]["generation"].(int64), SafeErrorCode: "INTEGRATION_REQUEST_REJECTED"})
			freshRun, err := service.GetRun(ctx, owner, run.Ref)
			if err != nil {
				t.Fatal(err)
			}
			execute(command.CancelRun, owner, policy+"-cancel", &freshRun.Version, command.RunCommandInput{RunRef: run.Ref, Reason: "Synthetic policy fixture cleanup"})
		})
	}
	t.Run("cancel_project_graph", func(t *testing.T) {
		exerciseIntegrationCancelGraph(t, ctx, pool, service, owner, worker, gateway, connection.Ref, "cancel-project", func(key, policy string) {
			fresh, err := service.GetIntegrationConnection(ctx, owner, connection.Ref)
			if err != nil {
				t.Fatal(err)
			}
			paths := []string{}
			if policy == "HUMAN_SCOPED" {
				paths = []string{"/pull_request_number"}
			}
			execute(command.ChangeIntegrationGrant, owner, "cancel-project-"+key+"-grant", &fresh.Version, command.IntegrationGrantInput{ConnectionRef: connection.Ref, CapabilityKey: "github.pull_request.review.create", AgentRef: agent.Ref, Enabled: true, ApprovalPolicy: policy, ApprovalScopePaths: paths})
		}, func(key string) string {
			return execute(command.LaunchRun, owner, "cancel-project-"+key+"-run", nil, command.LaunchRunInput{ProjectRef: project.Ref, Title: "Cancel graph", Task: "Synthetic cancelled integration graph", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}}).Run.Ref
		})
	})
}
