package platform

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPersonalGitHubScopedPolicyComponent proves each resource mode and selected policy through owner verification, launch, immutable personal pin, optional human gate, claim and one-time Begin; provider effects remain synthetic and unexecuted.
// TestPersonalGitHubScopedPolicyComponent проверяет каждый resource mode и selected policy через owner verification, launch, immutable personal pin, optional human gate, claim и one-time Begin; provider effects остаются synthetic и не исполняются.
func TestPersonalGitHubScopedPolicyComponent(t *testing.T) {
	for _, scenario := range []struct{ kind, policy, change string }{{"READ", "NONE", ""}, {"READ_MANAGED", "NONE", ""}, {"WRITE", "HUMAN_EACH_EFFECT", ""}, {"WRITE", "HUMAN_SCOPED", ""}, {"WRITE", "HUMAN_EACH_EFFECT", "REVOKE"}, {"WRITE", "HUMAN_EACH_EFFECT", "ROTATE"}} {
		t.Run(scenario.kind+"/"+scenario.policy+"/"+scenario.change, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
			defer cancel()
			pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			repository := newSyntheticRuntimeComponentRepository(t, ctx, pool, currentGitHubComponent(t))
			seedObservedCatalogFixture(t, ctx, repository)
			materializer := &personalCredentialComponentMaterializer{}
			service, err := platformservice.New(repository, platformservice.WithCredentialMaterializer(materializer))
			if err != nil {
				t.Fatal(err)
			}
			owner, runtimeWorker, gateway := resolveIntegrationEffectTestPrincipals(t, ctx, repository)
			execute := newComponentCommandExecutor(t, ctx, service, "scoped-personal-")
			readScenario := strings.HasPrefix(scenario.kind, "READ")
			configuration := map[string]any{"scope_kind": "ALL_ACCESSIBLE"}
			connection := execute(command.CreateConnection, owner, "connection", nil, command.ConnectionInput{DefinitionKey: "github", Name: "Synthetic personal GitHub", PublicConfiguration: configuration}).Connection
			if connection.CredentialPolicy != entity.IntegrationCredentialInitiatorRequired {
				t.Fatal("personal GitHub connection defaulted to shared credential")
			}
			if scenario.kind == "READ_MANAGED" {
				connection = rebindManagedGitHubScopedComponent(t, ctx, repository, service, owner, *connection)
				disabled := execute(command.SetConnectionEnabled, owner, "managed-disable", &connection.Version, command.ConnectionInput{Ref: connection.Ref, Enabled: false}).Connection
				policyOwner := owner
				policyOwner.Permission = "platform.integrations.credential-policy.set"
				_, policyErr := service.Execute(ctx, command.Command{Kind: command.SetIntegrationCredentialPolicy, Principal: policyOwner, Mutation: value.Mutation{IdempotencyKey: "managed-scope-shared-without-service", ExpectedVersion: &disabled.Version}, Payload: command.IntegrationCredentialPolicyInput{ConnectionRef: connection.Ref, Policy: entity.IntegrationCredentialProjectShared}})
				if !errors.Is(policyErr, errs.ErrInvalid) {
					t.Fatal("managed version bypassed explicit service-identity policy", policyErr)
				}
				connection = execute(command.SetConnectionEnabled, owner, "managed-enable", &disabled.Version, command.ConnectionInput{Ref: connection.Ref, Enabled: true}).Connection
			}

			owner.Permission = "platform.personal-integrations.create"
			binding := execute(command.CreatePersonalIntegrationCredential, owner, "binding", nil, command.PersonalIntegrationCredentialInput{ConnectionRef: connection.Ref, ExpectedConnectionVersion: connection.Version}).PersonalIntegrationCredential
			owner.Permission = "platform.personal-integrations.configure"
			configured, err := service.ConfigurePersonalIntegrationCredential(ctx, owner, value.Mutation{IdempotencyKey: "scoped-personal-key", ExpectedVersion: &binding.Version}, connection.Ref, connection.Version, []byte("synthetic-scoped-personal-key"))
			if err != nil {
				t.Fatal(err)
			}
			owner.Permission = "platform.personal-integrations.test"
			discoveryPage := 0
			if readScenario {
				discoveryPage = 1
			}
			execute(command.TestPersonalIntegrationCredential, owner, "test", &configured.Version, command.PersonalIntegrationCredentialInput{ConnectionRef: connection.Ref, ExpectedConnectionVersion: connection.Version, GitHubDiscoveryPage: discoveryPage})
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
			probe.ConfirmedCapabilityKeys = []string{"github.identity.read"}
			if discoveryPage != 0 {
				probe.GitHubDiscovery = &integrationpackage.GitHubDiscoveryPage{IdentityID: 11, Login: "synthetic", Page: 1, Items: []integrationpackage.GitHubDiscoveredRepository{{ID: 7, FullName: "fixture/repo", OwnerKind: "Organization", CanRead: true}}}
			}
			verifier.Permission = "platform.personal-integration-tests.complete"
			execute(command.CompletePersonalIntegrationCredentialTest, verifier, "verified", nil, probe)
			if readScenario {
				owner.Permission = "platform.command.integrations.update"
				forged := command.ConnectionInput{Ref: connection.Ref, Name: connection.Name, PublicConfiguration: map[string]any{"scope_kind": "SELECTED", "repositories": `[{"id":8,"full_name":"foreign/repo"}]`}}
				_, err := service.Execute(ctx, command.Command{Kind: command.UpdateConnection, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "scoped-personal-forged-selection", ExpectedVersion: &connection.Version}, Payload: forged})
				if !errors.Is(err, errs.ErrForbidden) {
					t.Fatal("caller-created repository ID received configuration authority", err)
				}
				forged.PublicConfiguration["repositories"] = `[{"id":7,"full_name":"fixture/repo"}]`
				connection = execute(command.UpdateConnection, owner, "selected", &connection.Version, forged).Connection
				current, err := service.GetPersonalIntegrationCredential(ctx, owner, connection.Ref)
				if err != nil || current.State != "CHECK_REQUIRED" || current.GitHubDiscovery != nil {
					t.Fatal("scope update retained stale discovery/verification", err)
				}
				owner.Permission = "platform.personal-integrations.test"
				execute(command.TestPersonalIntegrationCredential, owner, "test-selected", &current.Version, command.PersonalIntegrationCredentialInput{ConnectionRef: connection.Ref, ExpectedConnectionVersion: connection.Version})
				verifier.Permission = "platform.personal-integration-tests.claim"
				claimed, err := service.ClaimPersonalIntegrationCredentialTests(ctx, verifier, "scoped-selected-verifier", 1)
				if err != nil || len(claimed) != 1 {
					t.Fatal("selected verification not claimed", err)
				}
				verified := personalTestCompletionFromClaim(claimed[0])
				verifier.Permission = "platform.personal-integration-tests.begin"
				if err := service.BeginPersonalIntegrationCredentialProbe(ctx, verifier, verified); err != nil {
					t.Fatal(err)
				}
				verified.Outcome = entity.PersonalIntegrationCredentialAuthenticated
				verified.ConfirmedCapabilityKeys = []string{"github.identity.read"}
				verifier.Permission = "platform.personal-integration-tests.complete"
				execute(command.CompletePersonalIntegrationCredentialTest, verifier, "verified-selected", nil, verified)
			}

			project := execute(command.CreateProject, owner, "project", nil, command.ProjectInput{Name: "Scoped personal project", Language: "en"}).Project
			agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "scoped-personal-agent", "Scoped commenter")
			operation := "github.issue.comment.create"
			if readScenario {
				operation = "github.issue.read"
			}
			capability, _ := repository.integrationDefinitions["github"].Capability(operation)
			paths := []string{}
			if scenario.policy == "HUMAN_SCOPED" {
				paths = capability.ApprovalScope.RequiredInputPaths
			}
			connection = execute(command.ChangeIntegrationGrant, owner, "grant", &connection.Version, command.IntegrationGrantInput{ConnectionRef: connection.Ref, AgentRef: agent.Ref, CapabilityKey: operation, ApprovalPolicy: scenario.policy, ApprovalScopePaths: paths, Enabled: true}).Connection

			run := execute(command.LaunchRun, owner, "run", nil, command.LaunchRunInput{ProjectRef: project.Ref, Title: "Scoped personal comment", Task: "Synthetic only", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}}).Run
			runtime := execute(command.ClaimExecution, runtimeWorker, "runtime", nil, command.LeaseInput{WorkloadInstance: "scoped-personal-runtime", Limit: 1}).RuntimeItems
			if len(runtime) != 1 {
				t.Fatal("scoped personal runtime absent")
			}
			invokeInput := map[string]string{"run_ref": run.Ref, "node_ref": stringMap(runtime[0], "nodeRef"), "connection_ref": connection.Ref, "capability_key": operation, "idempotency_key": "scoped-personal-comment"}
			boundedInput := map[string]any{"repository_full_name": "fixture/repo", "repository_id": 7, "issue_number": 12}
			if !readScenario {
				boundedInput["body"] = "Synthetic comment"
			}
			invocation, err := service.ResolveIntegrationInvocation(ctx, runtimeWorker, invokeInput, boundedInput)
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
				invokeInput["idempotency_key"] += "-alias"
				aliased, err := service.ResolveIntegrationInvocation(ctx, runtimeWorker, invokeInput, boundedInput)
				if err != nil || stringMap(aliased, "invocationRef") != stringMap(invocation, "invocationRef") || stringMap(aliased, "gateRef") != stringMap(invocation, "gateRef") {
					t.Fatal("personal pending call did not preserve its immutable credential pin", err)
				}
				if scenario.change != "" {
					current, err := service.GetPersonalIntegrationCredential(ctx, owner, connection.Ref)
					if err != nil {
						t.Fatal(err)
					}
					if scenario.change == "REVOKE" {
						owner.Permission = "platform.personal-integrations.revoke"
						execute(command.RevokePersonalIntegrationCredential, owner, "revoke-before-alias", &current.Version, command.PersonalIntegrationCredentialInput{ConnectionRef: connection.Ref})
					} else {
						owner.Permission = "platform.personal-integrations.configure"
						if _, err := service.ConfigurePersonalIntegrationCredential(ctx, owner, value.Mutation{IdempotencyKey: "scoped-personal-rotate", ExpectedVersion: &current.Version}, connection.Ref, connection.Version, []byte("synthetic-rotated-key")); err != nil {
							t.Fatal(err)
						}
					}
					invokeInput["idempotency_key"] += "-after-credential-change"
					if _, err := service.ResolveIntegrationInvocation(ctx, runtimeWorker, invokeInput, boundedInput); err == nil {
						t.Fatal("new alias accepted a revoked or replaced personal credential")
					}
					claims, err := service.ClaimIntegrationInvocations(ctx, gateway, "scoped-personal-denied", 1)
					if err != nil || len(claims) != 0 {
						t.Fatal("credential change bypassed approval or fell back to another key", err)
					}
					currentRun, err := service.GetRun(ctx, owner, run.Ref)
					if err != nil {
						t.Fatal(err)
					}
					execute(command.CancelRun, owner, "cancel-credential-change", &currentRun.Version, command.RunCommandInput{RunRef: run.Ref, Reason: "Close synthetic credential-change case"})
					return
				}
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
			if stringMap(claim, "resourcePermissionProfile") != integrationpackage.CredentialResourcePermissionGitHubScopedV1 {
				t.Fatal("scoped personal object check omitted")
			}
			request := &cp.BeginPersonalIntegrationInvocationRequest{InvocationRef: stringMap(claim, "invocationRef"), LeaseRef: stringMap(claim, "leaseRef"), Fence: stringMap(claim, "fence"), Generation: runtimeRevisionMapInt64(claim, "generation"), WorkloadInstance: "scoped-personal-gateway"}
			rpc := personalRuntimeRPCClient(t, service)
			admitted, err := rpc.BeginPersonalIntegrationInvocation(personalRuntimeRPCContext(ctx), request)
			if err != nil || !admitted.GetAdmitted() || admitted.GetResourcePermissionProfile() != cp.IntegrationCredentialResourcePermissionProfile_INTEGRATION_CREDENTIAL_RESOURCE_PERMISSION_PROFILE_GITHUB_SCOPED_RESOURCE_V1 {
				t.Fatal("scoped personal Begin rejected or dropped object check", err)
			}
			if repeated, err := rpc.BeginPersonalIntegrationInvocation(personalRuntimeRPCContext(ctx), request); err == nil && repeated.GetAdmitted() {
				t.Fatal("scoped personal Begin repeated admission")
			}

		})
	}
}
