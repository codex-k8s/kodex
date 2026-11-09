package platform

import (
	"context"
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
	"testing"
	"time"
)

// TestPersonalGitLabScopedPolicyComponent proves each resource mode and selected policy through owner verification, launch, immutable personal pin, optional human gate, claim and one-time Begin; provider effects remain synthetic and unexecuted.
// TestPersonalGitLabScopedPolicyComponent проверяет каждый resource mode и selected policy через owner verification, launch, immutable personal pin, optional human gate, claim и one-time Begin; provider effects остаются synthetic и не исполняются.
func TestPersonalGitLabScopedPolicyComponent(t *testing.T) {
	for _, scenario := range []struct{ kind, policy string }{{"PROJECT", "NONE"}, {"GROUP", "HUMAN_EACH_EFFECT"}, {"INSTANCE", "HUMAN_SCOPED"}} {
		t.Run(scenario.kind+"/"+scenario.policy, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
			defer cancel()
			pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			repository := newSyntheticRuntimeComponentRepository(t, ctx, pool)
			seedObservedCatalogFixture(t, ctx, repository)
			materializer := &personalCredentialComponentMaterializer{}
			service, err := platformservice.New(repository, platformservice.WithCredentialMaterializer(materializer))
			if err != nil {
				t.Fatal(err)
			}
			owner, runtimeWorker, gateway := resolveIntegrationEffectTestPrincipals(t, ctx, repository)
			execute := newComponentCommandExecutor(t, ctx, service, "scoped-personal-")
			configuration := map[string]any{"base_url": "https://git.example.invalid", "scope_kind": scenario.kind}
			if scenario.kind == "PROJECT" {
				configuration["project_path"] = "fixture/project"
			}
			if scenario.kind == "GROUP" {
				configuration["group_path"] = "fixture"
				configuration["include_subgroups"] = "false"
			}
			connection := execute(command.CreateConnection, owner, "connection", nil, command.ConnectionInput{DefinitionKey: "gitlab", Name: "Synthetic scoped personal", PublicConfiguration: configuration}).Connection
			connection = execute(command.SetConnectionEnabled, owner, "disable", &connection.Version, command.ConnectionInput{Ref: connection.Ref, Enabled: false}).Connection
			owner.Permission = "platform.integrations.credential-policy.set"
			connection = execute(command.SetIntegrationCredentialPolicy, owner, "policy", &connection.Version, command.IntegrationCredentialPolicyInput{ConnectionRef: connection.Ref, Policy: entity.IntegrationCredentialInitiatorRequired}).Connection
			owner.Permission = "platform.command.integrations.enable"
			connection = execute(command.SetConnectionEnabled, owner, "enable", &connection.Version, command.ConnectionInput{Ref: connection.Ref, Enabled: true}).Connection
			owner.Permission = "platform.personal-integrations.create"
			binding := execute(command.CreatePersonalIntegrationCredential, owner, "binding", nil, command.PersonalIntegrationCredentialInput{ConnectionRef: connection.Ref, ExpectedConnectionVersion: connection.Version}).PersonalIntegrationCredential
			owner.Permission = "platform.personal-integrations.configure"
			configured, err := service.ConfigurePersonalIntegrationCredential(ctx, owner, value.Mutation{IdempotencyKey: "scoped-personal-key", ExpectedVersion: &binding.Version}, connection.Ref, connection.Version, []byte("synthetic-scoped-personal-key"))
			if err != nil {
				t.Fatal(err)
			}
			owner.Permission = "platform.personal-integrations.test"
			execute(command.TestPersonalIntegrationCredential, owner, "test", &configured.Version, command.PersonalIntegrationCredentialInput{ConnectionRef: connection.Ref, ExpectedConnectionVersion: connection.Version})
			verifier := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "integration-gateway", Operation: "platform.personal-integration-tests.claim"}, "integration-gateway")
			probes, err := service.ClaimPersonalIntegrationCredentialTests(ctx, verifier, "scoped-personal-verifier", 1)
			if err != nil || len(probes) != 1 {
				t.Fatal("claim scoped probe", err)
			}
			probe := personalTestCompletionFromClaim(probes[0])
			verifier.Permission = "platform.personal-integration-tests.begin"
			if err := service.BeginPersonalIntegrationCredentialProbe(ctx, verifier, probe); err != nil {
				t.Fatal(err)
			}
			probe.Outcome = entity.PersonalIntegrationCredentialAuthenticated
			probe.ConfirmedCapabilityKeys = []string{"gitlab.scope.health.read"}
			verifier.Permission = "platform.personal-integration-tests.complete"
			execute(command.CompletePersonalIntegrationCredentialTest, verifier, "verified", nil, probe)
			project := execute(command.CreateProject, owner, "project", nil, command.ProjectInput{Name: "Scoped personal project", Language: "en"}).Project
			agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "scoped-personal-agent", "Scoped commenter")
			const operation = "gitlab.issue.note.create"
			capability, _ := repository.integrationDefinitions["gitlab"].Capability(operation)
			paths := []string{}
			if scenario.policy == "HUMAN_SCOPED" {
				paths = capability.ApprovalScope.RequiredInputPaths
			}
			connection = execute(command.ChangeIntegrationGrant, owner, "grant", &connection.Version, command.IntegrationGrantInput{ConnectionRef: connection.Ref, AgentRef: agent.Ref, CapabilityKey: operation, ApprovalPolicy: scenario.policy, ApprovalScopePaths: paths, Enabled: true}).Connection
			readCapability, _ := repository.integrationDefinitions["gitlab"].Capability("gitlab.project.metadata.read")
			readPaths := []string{}
			if scenario.policy == "HUMAN_SCOPED" {
				readPaths = readCapability.ApprovalScope.RequiredInputPaths
			}
			connection = execute(command.ChangeIntegrationGrant, owner, "grant-read", &connection.Version, command.IntegrationGrantInput{ConnectionRef: connection.Ref, AgentRef: agent.Ref, CapabilityKey: readCapability.Key, ApprovalPolicy: scenario.policy, ApprovalScopePaths: readPaths, Enabled: true}).Connection

			run := execute(command.LaunchRun, owner, "run", nil, command.LaunchRunInput{ProjectRef: project.Ref, Title: "Scoped personal comment", Task: "Synthetic only", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}}).Run
			runtime := execute(command.ClaimExecution, runtimeWorker, "runtime", nil, command.LeaseInput{WorkloadInstance: "scoped-personal-runtime", Limit: 1}).RuntimeItems
			if len(runtime) != 1 {
				t.Fatal("scoped personal runtime absent")
			}
			invocation, err := service.ResolveIntegrationInvocation(ctx, runtimeWorker, map[string]string{"run_ref": run.Ref, "node_ref": stringMap(runtime[0], "nodeRef"), "connection_ref": connection.Ref, "capability_key": operation, "idempotency_key": "scoped-personal-comment"}, map[string]any{"project_path": "fixture/project", "project_id": 7, "issue_iid": 12, "body": "Synthetic comment"})
			if err != nil {
				t.Fatal("scoped personal immutable pin rejected", err)
			}
			expected := "READY"
			if scenario.policy != "NONE" {
				expected = "WAITING_APPROVAL"
			}
			if stringMap(invocation, "state") != expected {
				t.Fatal("selected scoped personal policy ignored")
			}
			if scenario.policy != "NONE" {
				gate, err := service.GetOwnerGate(ctx, owner, stringMap(invocation, "gateRef"))
				if err != nil {
					t.Fatal(err)
				}
				execute(command.ResolveOwnerGate, owner, "approve", &gate.Version, command.GateResolutionInput{GateRef: gate.Ref, Decision: "APPROVE"})
			}
			claims, err := service.ClaimIntegrationInvocations(ctx, gateway, "scoped-personal-gateway", 1)
			if err != nil || len(claims) != 1 {
				t.Fatal("scoped personal call not claimed", err)
			}
			claim := claims[0]
			if stringMap(claim, "resourcePermissionProfile") != integrationpackage.CredentialResourcePermissionGitLabScopedV2 {
				t.Fatal("scoped personal object check omitted")
			}
			request := &cp.BeginPersonalIntegrationInvocationRequest{InvocationRef: stringMap(claim, "invocationRef"), LeaseRef: stringMap(claim, "leaseRef"), Fence: stringMap(claim, "fence"), Generation: runtimeRevisionMapInt64(claim, "generation"), WorkloadInstance: "scoped-personal-gateway"}
			rpc := personalRuntimeRPCClient(t, service)
			admitted, err := rpc.BeginPersonalIntegrationInvocation(personalRuntimeRPCContext(ctx), request)
			if err != nil || !admitted.GetAdmitted() || admitted.GetResourcePermissionProfile() != cp.IntegrationCredentialResourcePermissionProfile_INTEGRATION_CREDENTIAL_RESOURCE_PERMISSION_PROFILE_GITLAB_SCOPED_RESOURCE_V2 {
				t.Fatal("scoped personal Begin rejected or dropped object check", err)
			}
			if repeated, err := rpc.BeginPersonalIntegrationInvocation(personalRuntimeRPCContext(ctx), request); err == nil && repeated.GetAdmitted() {
				t.Fatal("scoped personal Begin repeated admission")
			}
			readInvocation, err := service.ResolveIntegrationInvocation(ctx, runtimeWorker, map[string]string{"run_ref": run.Ref, "node_ref": stringMap(runtime[0], "nodeRef"), "connection_ref": connection.Ref, "capability_key": readCapability.Key, "idempotency_key": "scoped-personal-read"}, map[string]any{"project_path": "fixture/project", "project_id": 7})
			if err != nil || stringMap(readInvocation, "state") != expected {
				t.Fatal("READ selected human policy ignored or rejected", err)
			}
			if scenario.policy != "NONE" {
				gate, err := service.GetOwnerGate(ctx, owner, stringMap(readInvocation, "gateRef"))
				if err != nil {
					t.Fatal(err)
				}
				execute(command.ResolveOwnerGate, owner, "approve-read", &gate.Version, command.GateResolutionInput{GateRef: gate.Ref, Decision: "APPROVE"})
			}
			readClaims, err := service.ClaimIntegrationInvocations(ctx, gateway, "scoped-personal-gateway", 1)
			if err != nil || len(readClaims) != 1 || stringMap(readClaims[0], "invocationRef") != stringMap(readInvocation, "invocationRef") {
				t.Fatal("READ human policy lost owner claim", err)
			}
			readClaim := readClaims[0]
			readAdmission, err := rpc.BeginPersonalIntegrationInvocation(personalRuntimeRPCContext(ctx), &cp.BeginPersonalIntegrationInvocationRequest{InvocationRef: stringMap(readClaim, "invocationRef"), LeaseRef: stringMap(readClaim, "leaseRef"), Fence: stringMap(readClaim, "fence"), Generation: runtimeRevisionMapInt64(readClaim, "generation"), WorkloadInstance: "scoped-personal-gateway"})
			if err != nil || !readAdmission.GetAdmitted() {
				t.Fatal("READ human policy lost personal admission", err)
			}

		})
	}
}
