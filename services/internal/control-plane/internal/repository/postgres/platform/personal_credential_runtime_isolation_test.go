package platform

import (
	"context"
	"errors"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// assertPersonalRuntimeSecondUserPreflight distinguishes Connection visibility from call authority
// and proves a project launcher cannot borrow the owner's ready key. Refused launches create
// neither sessions nor runs. All identities and permissions belong to the disposable fixture.
// assertPersonalRuntimeSecondUserPreflight отделяет видимость Connection от права вызова и
// проверяет, что project launcher не берёт готовый ключ владельца. Отклонённые запуски не создают
// sessions и runs. Все identities и permissions принадлежат disposable fixture.
func assertPersonalRuntimeSecondUserPreflight(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner, second value.Principal, connectionRef, projectRef, agentRef string) {
	t.Helper()
	previewAuthorityGrant(t, ctx, service, owner, "personal-runtime-second-project", "usr_personal_second", []string{"project.view", "agent.view", "agent.launch", "run.view"}, entity.AccessScope{Kind: "PROJECT", ProjectRef: projectRef})
	previewAuthorityGrant(t, ctx, service, owner, "personal-runtime-second-visible", "usr_personal_second", []string{"integration.view"}, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: connectionRef})
	for index, problem := range []entity.IntegrationCredentialProblem{entity.IntegrationCredentialConnectionUnavailable, entity.IntegrationCredentialRevoked} {
		if index == 1 {
			previewAuthorityGrant(t, ctx, service, owner, "personal-runtime-second-usable", "usr_personal_second", []string{"integration.manage"}, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: connectionRef})
		}
		var sessionsBefore, runsBefore, sessionsAfter, runsAfter int
		if err := repository.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM control_plane.sessions),(SELECT count(*) FROM control_plane.runs)").Scan(&sessionsBefore, &runsBefore); err != nil {
			t.Fatal(err)
		}
		_, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: second,
			Mutation: value.Mutation{IdempotencyKey: "personal-runtime-second-refused-" + string(problem)},
			Payload:  command.LaunchRunInput{ProjectRef: projectRef, Target: entity.RunTarget{Type: "AGENT", Ref: agentRef}, Task: "Synthetic second-user preflight"}})
		var preflight *platformservice.IntegrationCredentialPreflightError
		if !errors.As(err, &preflight) || len(preflight.Issues) != 1 || preflight.Issues[0].ConnectionRef != connectionRef || preflight.Issues[0].Problem != problem {
			t.Fatal("second user borrowed an owner key or lost exact preflight diagnostics", problem, err)
		}
		if err := repository.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM control_plane.sessions),(SELECT count(*) FROM control_plane.runs)").Scan(&sessionsAfter, &runsAfter); err != nil || sessionsBefore != sessionsAfter || runsBefore != runsAfter {
			t.Fatal("refused second-user preflight created execution state", err)
		}
	}
}

// assertPersonalRuntimeSecondUserCall uses a second project and the same Connection with a
// different initiator. Even after the owner's key is revoked, the actual runtime/Begin RPC must
// select only this user's verified revision and retain that identity in the immutable root pin.
// Provider observations and transport peer admission remain synthetic; no live account is used.
// assertPersonalRuntimeSecondUserCall использует второй проект и ту же Connection с другим
// инициатором. После отзыва ключа владельца настоящий runtime/Begin RPC выбирает лишь проверенную
// revision этого пользователя и сохраняет identity в immutable root pin. Provider observations
// и transport peer admission синтетические; живой аккаунт не используется.
func assertPersonalRuntimeSecondUserCall(t *testing.T, ctx context.Context, repository *Repository, service, workerService *platformservice.Service, owner, second, worker, gateway, verifier value.Principal, connectionRef, ownerCredentialRef string, rpc cp.PersonalIntegrationCredentialServiceClient) {
	t.Helper()
	self := second
	self.Permission = "platform.personal-integrations.get"
	binding, err := service.GetPersonalIntegrationCredential(ctx, self, connectionRef)
	if err != nil || binding.State != "REVOKED" {
		t.Fatal("second user lost its separate revoked binding", err)
	}
	self.Permission = "platform.personal-integrations.configure"
	binding, err = service.ConfigurePersonalIntegrationCredential(ctx, self, value.Mutation{IdempotencyKey: "personal-runtime-second-key", ExpectedVersion: &binding.Version}, connectionRef, binding.ConnectionVersion, []byte("synthetic-runtime-second-key"))
	if err != nil || binding.Credential == nil || binding.Credential.Ref == ownerCredentialRef {
		t.Fatal("second user configured another user's credential", err)
	}
	self.Permission = "platform.personal-integrations.test"
	if _, err := service.Execute(ctx, command.Command{Kind: command.TestPersonalIntegrationCredential, Principal: self, Mutation: value.Mutation{IdempotencyKey: "personal-runtime-second-test", ExpectedVersion: &binding.Version}, Payload: command.PersonalIntegrationCredentialInput{ConnectionRef: connectionRef, ExpectedConnectionVersion: binding.ConnectionVersion}}); err != nil {
		t.Fatal(err)
	}
	verifier.Permission = "platform.personal-integration-tests.claim"
	claims, err := workerService.ClaimPersonalIntegrationCredentialTests(ctx, verifier, "personal-runtime-second-verifier", 1)
	if err != nil || len(claims) != 1 {
		t.Fatal("claim second user's health probe", err)
	}
	probe := personalTestCompletionFromClaim(claims[0])
	verifier.Permission = "platform.personal-integration-tests.begin"
	if err := workerService.BeginPersonalIntegrationCredentialProbe(ctx, verifier, probe); err != nil {
		t.Fatal(err)
	}
	verifier.Permission = "platform.personal-integration-tests.complete"
	probe.Outcome = entity.PersonalIntegrationCredentialAuthenticated
	if _, err := workerService.Execute(ctx, command.Command{Kind: command.CompletePersonalIntegrationCredentialTest, Principal: verifier, Mutation: value.Mutation{IdempotencyKey: "personal-runtime-second-verified"}, Payload: probe}); err != nil {
		t.Fatal(err)
	}
	projectResult, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "personal-runtime-second-project-create"}, Payload: command.ProjectInput{Name: "Second personal runtime project", Language: "ru"}})
	if err != nil || projectResult.Project == nil {
		t.Fatal(err)
	}
	project := projectResult.Project
	agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "personal-runtime-second-agent", "Second personal runtime reader")
	previewAuthorityGrant(t, ctx, service, owner, "personal-runtime-second-project-two", "usr_personal_second", []string{"project.view", "agent.view", "agent.launch", "run.view"}, entity.AccessScope{Kind: "PROJECT", ProjectRef: project.Ref})
	connection, err := service.GetIntegrationConnection(ctx, owner, connectionRef)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.ChangeIntegrationGrant, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "personal-runtime-second-grant", ExpectedVersion: &connection.Version}, Payload: command.IntegrationGrantInput{ConnectionRef: connectionRef, AgentRef: agent.Ref, CapabilityKey: "gitlab.project.metadata.read", ApprovalPolicy: "NONE", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	launched, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: second, Mutation: value.Mutation{IdempotencyKey: "personal-runtime-second-launch"}, Payload: command.LaunchRunInput{ProjectRef: project.Ref, Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, Task: "Read second-user synthetic metadata"}})
	if err != nil || launched.Run == nil {
		t.Fatal("launch second user's independently verified key", err)
	}
	run := launched.Run
	claimed, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "personal-runtime-second-claim"}, Payload: command.LeaseInput{WorkloadInstance: "personal-runtime-worker", Limit: 1}})
	if err != nil || len(claimed.RuntimeItems) != 1 || stringMap(claimed.RuntimeItems[0], "runRef") != run.Ref {
		t.Fatal("claim second user's runtime", err)
	}
	invocation, err := service.ResolveIntegrationInvocation(ctx, worker, map[string]string{"run_ref": run.Ref, "node_ref": stringMap(claimed.RuntimeItems[0], "nodeRef"), "connection_ref": connectionRef, "capability_key": "gitlab.project.metadata.read", "idempotency_key": "personal-runtime-second-read"}, map[string]any{})
	if err != nil {
		t.Fatal("resolve second-user invocation", err)
	}
	work, err := service.ClaimIntegrationInvocations(ctx, gateway, "personal-runtime-gateway", 1)
	if err != nil || len(work) != 1 || stringMap(work[0], "invocationRef") != stringMap(invocation, "invocationRef") || work[0]["credential"] != nil {
		t.Fatal("second user's gateway claim changed ownership or exposed a shared key", err)
	}
	request := &cp.BeginPersonalIntegrationInvocationRequest{InvocationRef: stringMap(work[0], "invocationRef"), LeaseRef: stringMap(work[0], "leaseRef"), Fence: stringMap(work[0], "fence"), Generation: runtimeRevisionMapInt64(work[0], "generation"), WorkloadInstance: "personal-runtime-gateway"}
	response, err := rpc.BeginPersonalIntegrationInvocation(personalRuntimeRPCContext(ctx), request)
	if err != nil || !response.GetAdmitted() || response.GetCredentialRevision().GetRef() != binding.Credential.Ref || response.GetCredentialRevision().GetRef() == ownerCredentialRef {
		t.Fatal("runtime selected another user's personal revision", err)
	}
	var initiator string
	if err := repository.pool.QueryRow(ctx, `SELECT pin.subject_id::text FROM control_plane.integration_personal_invocation_credentials pin
JOIN control_plane.integration_invocations invocation ON invocation.id=pin.invocation_id AND invocation.organization_id=pin.organization_id
JOIN control_plane.runs root ON root.id=pin.root_run_id AND root.organization_id=pin.organization_id AND root.initiated_by=pin.subject_id
WHERE invocation.ref=$1`, request.InvocationRef).Scan(&initiator); err != nil || initiator != second.ActorID {
		t.Fatal("immutable credential pin lost the second root initiator", err)
	}
	summary := string(asJSON(map[string]any{"id": 2, "path_with_namespace": "fixture/second-project", "default_branch": "main", "visibility": "private", "archived": false}))
	if _, err := service.Execute(ctx, command.Command{Kind: command.CompleteIntegrationInvocation, Principal: gateway, Mutation: value.Mutation{IdempotencyKey: "personal-runtime-second-complete"}, Payload: command.IntegrationInvocationInput{InvocationRef: request.InvocationRef, LeaseRef: request.LeaseRef, Fence: request.Fence, Generation: request.Generation, Success: true, EffectKey: stringMap(work[0], "effectKey"), InputDigest: stringMap(work[0], "inputDigest"), ProviderEffectRef: "gitlab-project:2", ResultSummary: summary, ResponseDigest: workflowResultSHA([]byte(summary))}}); err != nil {
		t.Fatal("complete second-user effect receipt", err)
	}
	fresh, err := service.GetRun(ctx, owner, run.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.CancelRun, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "personal-runtime-second-cancel", ExpectedVersion: &fresh.Version}, Payload: command.RunCommandInput{RunRef: run.Ref}}); err != nil {
		t.Fatal(err)
	}
	self = owner
	self.Permission = "platform.personal-integrations.get"
	retained, err := service.GetPersonalIntegrationCredential(ctx, self, connectionRef)
	if err != nil || retained.State != "REVOKED" || retained.Credential.Ref == binding.Credential.Ref {
		t.Fatal("second user's positive call changed the owner's revoked binding", err)
	}
}
