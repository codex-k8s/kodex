package platform

import (
	"context"
	"errors"
	"slices"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgconn"
)

// assertPersonalObjectReadPermissions exercises all nine minimal object READs through native
// verification, launch, DB profile guard, worker claim and Begin RPC. Metadata proofs remain
// metadata-only; synthetic object refusals create no effect receipt or global permission proof.
// assertPersonalObjectReadPermissions проверяет девять минимальных object READ через native
// verification, launch, DB profile guard, worker claim и Begin RPC. Metadata proofs остаются
// metadata-only; synthetic отказы объекта не создают effect receipt или глобальное право.
func assertPersonalObjectReadPermissions(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner value.Principal) {
	t.Helper()
	for _, definition := range []string{"gitlab", "jira-server-dc", "confluence-server-dc"} {
		t.Run("object-read-"+definition, func(t *testing.T) {
			workers, err := platformservice.New(repository)
			if err != nil {
				t.Fatal(err)
			}
			prefix := "personal-object-" + definition + "-"
			execute := newComponentCommandExecutor(t, ctx, service, prefix)
			configuration := map[string]any{"base_url": "https://git.example.invalid", "project_path": "fixture/objects"}
			inputs := map[string]map[string]any{
				"gitlab.repository.file.read": {"file_path": "README.md", "ref": "main"},
				"gitlab.issue.read":           {"issue_iid": 1}, "gitlab.merge_request.read": {"merge_request_iid": 1}, "gitlab.pipeline.read": {"pipeline_id": 1},
			}
			proofs := []string{"gitlab.project.metadata.read"}
			if definition == "jira-server-dc" {
				configuration = map[string]any{"base_url": "https://issues.example.invalid", "context_path": "/tracker", "project_key": "DEMO", "issue_type": "Task", "auth_scheme": "BEARER"}
				inputs = map[string]map[string]any{"jira.issue.search": {"text": "fixture"}, "jira.issue.read": {"issue_key": "DEMO-1"}, "jira.issue.transition.list": {"issue_key": "DEMO-1"}}
				proofs = []string{"jira.project.read", "jira.identity.read"}
			} else if definition == "confluence-server-dc" {
				configuration = map[string]any{"base_url": "https://docs.example.invalid", "context_path": "/docs", "space_key": "DEMO", "auth_scheme": "BEARER"}
				inputs = map[string]map[string]any{"confluence.page.search": {"title": "Fixture"}, "confluence.page.read": {"page_id": "101"}}
				proofs = []string{"confluence.space.read", "confluence.identity.read"}
			}
			connection := execute(command.CreateConnection, owner, "connection", nil, command.ConnectionInput{DefinitionKey: definition, Name: "Synthetic personal object reader " + definition, PublicConfiguration: configuration}).Connection
			connection = execute(command.SetConnectionEnabled, owner, "disable", &connection.Version, command.ConnectionInput{Ref: connection.Ref, Enabled: false}).Connection
			owner.Permission = "platform.integrations.credential-policy.set"
			connection = execute(command.SetIntegrationCredentialPolicy, owner, "policy", &connection.Version, command.IntegrationCredentialPolicyInput{ConnectionRef: connection.Ref, Policy: entity.IntegrationCredentialInitiatorRequired}).Connection
			owner.Permission = "platform.command.integrations.enable"
			connection = execute(command.SetConnectionEnabled, owner, "enable", &connection.Version, command.ConnectionInput{Ref: connection.Ref, Enabled: true}).Connection
			owner.Permission = "platform.personal-integrations.create"
			binding := execute(command.CreatePersonalIntegrationCredential, owner, "binding", nil, command.PersonalIntegrationCredentialInput{ConnectionRef: connection.Ref, ExpectedConnectionVersion: connection.Version}).PersonalIntegrationCredential
			owner.Permission = "platform.personal-integrations.configure"
			configured, err := service.ConfigurePersonalIntegrationCredential(ctx, owner, value.Mutation{IdempotencyKey: prefix + "key", ExpectedVersion: &binding.Version}, connection.Ref, connection.Version, []byte("synthetic-object-personal-key"))
			if err != nil {
				t.Fatal(err)
			}
			owner.Permission = "platform.personal-integrations.test"
			execute(command.TestPersonalIntegrationCredential, owner, "verify", &configured.Version, command.PersonalIntegrationCredentialInput{ConnectionRef: connection.Ref, ExpectedConnectionVersion: connection.Version})
			verifier := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "integration-gateway", Operation: "platform.personal-integration-tests.claim"}, "integration-gateway")
			claims, err := workers.ClaimPersonalIntegrationCredentialTests(ctx, verifier, prefix+"verifier", 1)
			if err != nil || len(claims) != 1 || stringMap(claims[0], "connectionRef") != connection.Ref {
				t.Fatal("claim object reader verification", err)
			}
			probe := personalTestCompletionFromClaim(claims[0])
			verifier.Permission = "platform.personal-integration-tests.begin"
			if err := workers.BeginPersonalIntegrationCredentialProbe(ctx, verifier, probe); err != nil {
				t.Fatal(err)
			}
			slices.Sort(proofs)
			probe.Outcome, probe.ConfirmedCapabilityKeys = entity.PersonalIntegrationCredentialAuthenticated, proofs
			verifier.Permission = "platform.personal-integration-tests.complete"
			execute(command.CompletePersonalIntegrationCredentialTest, verifier, "verified", nil, probe)
			owner.Permission = "platform.personal-integrations.get"
			current, err := service.GetPersonalIntegrationCredential(ctx, owner, connection.Ref)
			if err != nil || current.State != "READY" || !slices.Equal(current.VerifiedCapabilities, proofs) {
				t.Fatal("object reader verification fabricated additional permissions", err)
			}
			project := execute(command.CreateProject, owner, "project", nil, command.ProjectInput{Name: "Synthetic object reader " + definition, Language: "ru"}).Project
			agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, prefix+"reader", "Personal object reader")
			operations := make([]string, 0, len(inputs))
			for operation := range inputs {
				operations = append(operations, operation)
			}
			slices.Sort(operations)
			for _, operation := range operations {
				connection = execute(command.ChangeIntegrationGrant, owner, "grant-"+operation, &connection.Version, command.IntegrationGrantInput{ConnectionRef: connection.Ref, AgentRef: agent.Ref, CapabilityKey: operation, ApprovalPolicy: "NONE", Enabled: true}).Connection
			}
			run := execute(command.LaunchRun, owner, "launch", nil, command.LaunchRunInput{ProjectRef: project.Ref, Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, Task: "Read only selected synthetic objects"}).Run
			worker := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
			leases := execute(command.ClaimExecution, worker, "runtime", nil, command.LeaseInput{WorkloadInstance: prefix + "runtime", Limit: 1}).RuntimeItems
			if len(leases) != 1 || stringMap(leases[0], "runRef") != run.Ref {
				t.Fatal("conditional object reader launch was not claimed")
			}
			_, _, gateway := resolveIntegrationEffectTestPrincipals(t, ctx, repository)
			rpc := personalRuntimeRPCClient(t, workers)
			for _, operation := range operations {
				contract, _ := integrationpackage.CredentialResourcePermissionContractFor(definition, operation)
				invocation, err := service.ResolveIntegrationInvocation(ctx, worker, map[string]string{"run_ref": run.Ref, "node_ref": stringMap(leases[0], "nodeRef"), "connection_ref": connection.Ref, "capability_key": operation, "idempotency_key": prefix + operation}, inputs[operation])
				if err != nil || stringMap(invocation, "state") != "READY" || stringMap(invocation, "gateRef") != "" {
					t.Fatal("object reader did not retain READ/NONE admission", err)
				}
				for _, wrong := range []string{"", integrationpackage.CredentialResourcePermissionConfluencePageUpdateV1} {
					_, err := repository.pool.Exec(ctx, `INSERT INTO control_plane.integration_personal_invocation_credentials
(invocation_id,organization_id,connection_id,root_run_id,subject_id,binding_id,binding_generation,credential_revision_id,definition_digest,configuration_digest,resource_permission_profile)
SELECT pin.invocation_id,pin.organization_id,pin.connection_id,pin.root_run_id,pin.subject_id,pin.binding_id,pin.binding_generation,pin.credential_revision_id,pin.definition_digest,pin.configuration_digest,$2
FROM control_plane.integration_personal_invocation_credentials pin
JOIN control_plane.integration_invocations invocation ON invocation.id=pin.invocation_id WHERE invocation.ref=$1`, stringMap(invocation, "invocationRef"), wrong)
					var rejected *pgconn.PgError
					if !errors.As(err, &rejected) || rejected.Code != "23514" {
						t.Fatal("database accepted a missing or foreign object-read profile", err)
					}
				}
				work, err := service.ClaimIntegrationInvocations(ctx, gateway, prefix+"gateway", 1)
				if err != nil || len(work) != 1 || stringMap(work[0], "invocationRef") != stringMap(invocation, "invocationRef") || stringMap(work[0], "resourcePermissionProfile") != contract.Profile || work[0]["credential"] != nil {
					t.Fatal("object read claim lost its mandatory profile", err)
				}
				request := &cp.BeginPersonalIntegrationInvocationRequest{InvocationRef: stringMap(work[0], "invocationRef"), LeaseRef: stringMap(work[0], "leaseRef"), Fence: stringMap(work[0], "fence"), Generation: runtimeRevisionMapInt64(work[0], "generation"), WorkloadInstance: prefix + "gateway"}
				admitted, err := rpc.BeginPersonalIntegrationInvocation(personalRuntimeRPCContext(ctx), request)
				if err != nil || !admitted.GetAdmitted() || admitted.GetCredentialRevision().GetRef() != current.Credential.Ref || admitted.GetResourcePermissionProfile().String() != "INTEGRATION_CREDENTIAL_RESOURCE_PERMISSION_PROFILE_"+contract.Profile {
					t.Fatal("object read Begin lost root key or typed profile", err)
				}
				execute(command.CompleteIntegrationInvocation, gateway, "refused-"+operation, nil, command.IntegrationInvocationInput{InvocationRef: request.InvocationRef, LeaseRef: request.LeaseRef, Fence: request.Fence, Generation: request.Generation, SafeErrorCode: "INTEGRATION_REQUEST_REJECTED"})
				stored, err := service.GetIntegrationInvocation(ctx, gateway, request.InvocationRef)
				if err != nil || stringMap(stored, "state") != "FAILED" || stringMap(stored, "effectReceiptRef") != "" {
					t.Fatal("object read refusal fabricated a completed effect", err)
				}
			}
			retained, err := service.GetPersonalIntegrationCredential(ctx, owner, connection.Ref)
			if err != nil || retained.Version != current.Version || retained.State != "READY" || !slices.Equal(retained.VerifiedCapabilities, current.VerifiedCapabilities) {
				t.Fatal("object refusals modified key readiness or global proofs", err)
			}
			fresh, err := service.GetRun(ctx, owner, run.Ref)
			if err != nil {
				t.Fatal(err)
			}
			execute(command.CancelRun, owner, "cancel", &fresh.Version, command.RunCommandInput{RunRef: run.Ref})
		})
	}
}
