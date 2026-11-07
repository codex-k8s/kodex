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

// assertPersonalConfluenceResourcePermission qualifies native owner preflight, Human Gate,
// claim and one-time Begin RPC with READ-only key proofs and an exact mandatory object-check
// profile. The external permission/effect is synthetic; no live Confluence or TLS is claimed.
// assertPersonalConfluenceResourcePermission проверяет native owner preflight, Human Gate,
// claim и одноразовый Begin RPC с READ-only proof ключа и обязательным exact object-check
// профилем. Внешние права/effect синтетические; live Confluence и TLS не подтверждаются.
func assertPersonalConfluenceResourcePermission(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner value.Principal) {
	t.Helper()
	workers, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	execute := newComponentCommandExecutor(t, ctx, service, "personal-confluence-")
	connection := execute(command.CreateConnection, owner, "connection", nil, command.ConnectionInput{
		DefinitionKey: "confluence-server-dc", Name: "Synthetic personal pages", PublicConfiguration: map[string]any{
			"base_url": "https://docs.example.invalid", "context_path": "/docs", "space_key": "DEMO", "auth_scheme": "BEARER"}}).Connection
	connection = execute(command.SetConnectionEnabled, owner, "disable", &connection.Version, command.ConnectionInput{Ref: connection.Ref, Enabled: false}).Connection
	owner.Permission = "platform.integrations.credential-policy.set"
	connection = execute(command.SetIntegrationCredentialPolicy, owner, "policy", &connection.Version, command.IntegrationCredentialPolicyInput{ConnectionRef: connection.Ref, Policy: entity.IntegrationCredentialInitiatorRequired}).Connection
	owner.Permission = "platform.command.integrations.enable"
	connection = execute(command.SetConnectionEnabled, owner, "enable", &connection.Version, command.ConnectionInput{Ref: connection.Ref, Enabled: true}).Connection
	owner.Permission = "platform.personal-integrations.create"
	binding := execute(command.CreatePersonalIntegrationCredential, owner, "binding", nil, command.PersonalIntegrationCredentialInput{ConnectionRef: connection.Ref, ExpectedConnectionVersion: connection.Version}).PersonalIntegrationCredential
	owner.Permission = "platform.personal-integrations.configure"
	configured, err := service.ConfigurePersonalIntegrationCredential(ctx, owner, value.Mutation{IdempotencyKey: "personal-confluence-key", ExpectedVersion: &binding.Version}, connection.Ref, connection.Version, []byte("synthetic-confluence-personal-key"))
	if err != nil {
		t.Fatal("configure personal Confluence key", err)
	}
	owner.Permission = "platform.personal-integrations.test"
	execute(command.TestPersonalIntegrationCredential, owner, "verify", &configured.Version, command.PersonalIntegrationCredentialInput{ConnectionRef: connection.Ref, ExpectedConnectionVersion: connection.Version})
	verifier := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "integration-gateway", Operation: "platform.personal-integration-tests.claim"}, "integration-gateway")
	claims, err := workers.ClaimPersonalIntegrationCredentialTests(ctx, verifier, "personal-confluence-verifier", 1)
	if err != nil || len(claims) != 1 || stringMap(claims[0], "connectionRef") != connection.Ref {
		t.Fatal("claim Confluence personal key verification", err)
	}
	probe := personalTestCompletionFromClaim(claims[0])
	verifier.Permission = "platform.personal-integration-tests.begin"
	if err := workers.BeginPersonalIntegrationCredentialProbe(ctx, verifier, probe); err != nil {
		t.Fatal("begin Confluence key verification", err)
	}
	probe.Outcome, probe.ConfirmedCapabilityKeys = entity.PersonalIntegrationCredentialAuthenticated, []string{"confluence.space.read", "confluence.identity.read"}
	verifier.Permission = "platform.personal-integration-tests.complete"
	execute(command.CompletePersonalIntegrationCredentialTest, verifier, "verified", nil, probe)
	owner.Permission = "platform.personal-integrations.get"
	current, err := service.GetPersonalIntegrationCredential(ctx, owner, connection.Ref)
	if err != nil || current.State != "READY" || slices.Contains(current.VerifiedCapabilities, "confluence.page.update") {
		t.Fatal("Confluence identity verification fabricated global WRITE", err)
	}
	project := execute(command.CreateProject, owner, "project", nil, command.ProjectInput{Name: "Synthetic personal page update", Language: "ru"}).Project
	agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "personal-confluence-writer", "Personal page editor")
	connection = execute(command.ChangeIntegrationGrant, owner, "grant", &connection.Version, command.IntegrationGrantInput{
		ConnectionRef: connection.Ref, AgentRef: agent.Ref, CapabilityKey: "confluence.page.update", ApprovalPolicy: "HUMAN_EACH_EFFECT", Enabled: true}).Connection
	run := execute(command.LaunchRun, owner, "launch", nil, command.LaunchRunInput{ProjectRef: project.Ref, Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, Task: "Update one approved synthetic page"}).Run
	worker := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
	leases := execute(command.ClaimExecution, worker, "runtime", nil, command.LeaseInput{WorkloadInstance: "personal-confluence-runtime", Limit: 1}).RuntimeItems
	if len(leases) != 1 || stringMap(leases[0], "runRef") != run.Ref {
		t.Fatal("conditional personal Confluence launch was not claimed")
	}
	invocation, err := service.ResolveIntegrationInvocation(ctx, worker, map[string]string{
		"run_ref": run.Ref, "node_ref": stringMap(leases[0], "nodeRef"), "connection_ref": connection.Ref,
		"capability_key": "confluence.page.update", "idempotency_key": "personal-confluence-update"},
		map[string]any{"page_id": "101", "expected_version": 2, "body": "<p>synthetic update</p>"})
	if err != nil || stringMap(invocation, "state") != "WAITING_APPROVAL" {
		t.Fatal("conditional write bypassed Human Gate or failed owner pinning", err)
	}
	var storedProfile string
	if err := repository.pool.QueryRow(ctx, `SELECT pin.resource_permission_profile
FROM control_plane.integration_personal_invocation_credentials pin
JOIN control_plane.integration_invocations invocation ON invocation.id=pin.invocation_id
WHERE invocation.ref=$1`, stringMap(invocation, "invocationRef")).Scan(&storedProfile); err != nil || storedProfile != integrationpackage.CredentialResourcePermissionConfluencePageUpdateV1 {
		t.Fatal("native credential pin omitted mandatory page permission profile", err)
	}
	_, denied := repository.pool.Exec(ctx, `INSERT INTO control_plane.integration_personal_invocation_credentials
(invocation_id,organization_id,connection_id,root_run_id,subject_id,binding_id,binding_generation,credential_revision_id,definition_digest,configuration_digest,resource_permission_profile)
SELECT pin.invocation_id,pin.organization_id,pin.connection_id,pin.root_run_id,pin.subject_id,pin.binding_id,pin.binding_generation,pin.credential_revision_id,pin.definition_digest,pin.configuration_digest,''
FROM control_plane.integration_personal_invocation_credentials pin
JOIN control_plane.integration_invocations invocation ON invocation.id=pin.invocation_id
WHERE invocation.ref=$1`, stringMap(invocation, "invocationRef"))
	var rejectedProfile *pgconn.PgError
	if !errors.As(denied, &rejectedProfile) || rejectedProfile.Code != "23514" {
		t.Fatal("database accepted an omitted object-check profile", denied)
	}
	gate, err := service.GetOwnerGate(ctx, owner, stringMap(invocation, "gateRef"))
	if err != nil {
		t.Fatal("read synthetic page update Gate", err)
	}
	execute(command.ResolveOwnerGate, owner, "approve", &gate.Version, command.GateResolutionInput{GateRef: gate.Ref, Decision: "APPROVE"})
	_, _, gateway := resolveIntegrationEffectTestPrincipals(t, ctx, repository)
	work, err := service.ClaimIntegrationInvocations(ctx, gateway, "personal-confluence-gateway", 1)
	if err != nil || len(work) != 1 || stringMap(work[0], "invocationRef") != stringMap(invocation, "invocationRef") ||
		stringMap(work[0], "resourcePermissionProfile") != integrationpackage.CredentialResourcePermissionConfluencePageUpdateV1 || work[0]["credential"] != nil {
		t.Fatal("personal page update claim lost mandatory object check", err)
	}
	request := &cp.BeginPersonalIntegrationInvocationRequest{InvocationRef: stringMap(work[0], "invocationRef"), LeaseRef: stringMap(work[0], "leaseRef"), Fence: stringMap(work[0], "fence"), Generation: runtimeRevisionMapInt64(work[0], "generation"), WorkloadInstance: "personal-confluence-gateway"}
	rpc := personalRuntimeRPCClient(t, workers)
	admitted, err := rpc.BeginPersonalIntegrationInvocation(personalRuntimeRPCContext(ctx), request)
	if err != nil || !admitted.GetAdmitted() || admitted.GetCredentialRevision().GetRef() != current.Credential.Ref ||
		admitted.GetResourcePermissionProfile() != cp.IntegrationCredentialResourcePermissionProfile_INTEGRATION_CREDENTIAL_RESOURCE_PERMISSION_PROFILE_CONFLUENCE_SERVER_PAGE_UPDATE_V1 {
		t.Fatal("personal Begin lost exact key or object-check profile", err)
	}
	// The adapter's separate HTTP tests prove object permission rejection. This native fixture
	// records recovery observations without claiming an external write or tainting key identity.
	// Отдельные HTTP tests adapter проверяют отказ прав объекта. Native fixture сохраняет
	// recovery observations, не заявляя внешнюю запись и не портя identity ключа.
	assertPersonalServerReadRecovery(t, ctx, repository, service, workers, gateway, work[0], request, current.Credential.Ref, "confluence-page:101", map[string]any{"id": "101", "title": "Synthetic page", "version": 3, "status": "current"})
	retained, err := service.GetPersonalIntegrationCredential(ctx, owner, connection.Ref)
	if err != nil || retained.State != "READY" || retained.Version != current.Version || slices.Contains(retained.VerifiedCapabilities, "confluence.page.update") {
		t.Fatal("object denial poisoned key readiness or fabricated global WRITE", err)
	}
	fresh, err := service.GetRun(ctx, owner, run.Ref)
	if err != nil {
		t.Fatal(err)
	}
	execute(command.CancelRun, owner, "cancel", &fresh.Version, command.RunCommandInput{RunRef: run.Ref})
}
