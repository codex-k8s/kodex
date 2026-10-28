package platform

import (
	"errors"
	"strconv"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/sshcompose"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// sourceActor registers a real limited root initiator and separately binds GitHub source access.
// Project execution, SSH connection and secret permissions survive revocation of that one binding;
// neither platform-owner role nor the agent's immutable READ grants are substituted for actor ACL.
// sourceActor регистрирует настоящего ограниченного root initiator с отдельным GitHub access.
// Права исполнения проекта, SSH connection и secret сохраняются при отзыве одной привязки;
// platform-owner role и immutable READ grants сотрудника не подменяют actor ACL.
func (fixture *sshComposeComponentFixture) sourceActor(t *testing.T, sourceRef string, targetRefs ...string) (value.Principal, entity.AccessBinding, []entity.AccessBinding) {
	t.Helper()
	input := port.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000001838", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		ExternalDisplayName: "SSH limited source initiator", CallerWorkload: "control-api-gateway", Operation: "platform.runs.launch", ProjectRef: fixture.projectRef}
	if _, err := fixture.repository.ResolveProofAuthority(fixture.ctx, input); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("unbound SSH initiator unexpectedly admitted")
	}
	subjects, _, err := fixture.service.ListAccessSubjects(fixture.ctx, fixture.owner, query.Filter{Query: input.ExternalDisplayName}, "USER")
	if err != nil || len(subjects) != 1 {
		t.Fatal("resolve limited SSH initiator")
	}
	subject := subjects[0].Ref
	receiptAccessBinding(t, fixture.ctx, fixture.service, fixture.owner, subject, "ssh-source-actor-project",
		[]string{"project.view", "workflow.view", "workflow.launch", "agent.view", "agent.launch", "run.view", "run.cancel", "secret.view"},
		entity.AccessScope{Kind: "PROJECT", ProjectRef: fixture.projectRef})
	targets := make([]entity.AccessBinding, 0, len(targetRefs))
	for index, targetRef := range targetRefs {
		targets = append(targets, receiptAccessBinding(t, fixture.ctx, fixture.service, fixture.owner, subject, "ssh-source-actor-target-"+strconv.Itoa(index), []string{"integration.manage"},
			entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: targetRef}))
	}
	source := receiptAccessBinding(t, fixture.ctx, fixture.service, fixture.owner, subject, "ssh-source-actor-source", []string{"integration.manage"},
		entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: sourceRef})
	return resolvedTestPrincipal(t, fixture.ctx, fixture.repository, input, "control-api-gateway"), source, targets
}

// assertSourceAccessRevocation uses owner commands to remove only the initiator's source ACL.
// New inspect/deploy admission, renewal and broker projection must then deny the same immutable
// graph, even while SSH/secret access and agent grants remain. Cancelling execution does not
// rewrite a possible provider effect into success or erase its UNKNOWN readback.
// assertSourceAccessRevocation отзывает owner-командой только source ACL инициатора.
// Новые inspect/deploy, renew и broker projection должны отказать для того же immutable графа,
// хотя SSH/secret access и agent grants сохранены. Отмена исполнения не превращает возможный
// эффект в успех и не удаляет UNKNOWN readback.
func (fixture *sshComposeComponentFixture) assertSourceAccessRevocation(t *testing.T, f *workflowRecoveryRegression, model value.Principal,
	source entity.AccessBinding, connectionRef string, execution, deployed map[string]any, snapshot sshcompose.Snapshot) {
	t.Helper()
	fixture.execute(t, fixture.owner, command.RevokeAccessBinding, "source-access-revoke", &source.Version, command.AccessBindingInput{BindingRef: source.Ref})
	for _, operation := range []string{"ssh.compose.inspect", "ssh.compose.deploy"} {
		input := map[string]any{"merge_invocation_ref": snapshot.MergeInvocationRef}
		if operation == "ssh.compose.deploy" {
			input = map[string]any{"inspect_invocation_ref": snapshot.InspectInvocationRef}
		}
		_, err := fixture.service.ResolveIntegrationInvocation(fixture.ctx, model, map[string]string{
			"run_ref": stringMap(execution, "runRef"), "node_ref": stringMap(execution, "nodeRef"), "connection_ref": connectionRef,
			"capability_key": operation, "idempotency_key": "ssh-source-denied-" + operation,
		}, input)
		if !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
			t.Fatal("source actor revocation did not close new SSH admission")
		}
	}
	binding := port.SSHComposeLeaseBinding{InvocationRef: stringMap(deployed, "invocationRef"), WorkloadInstance: "ssh-workflow-fixture",
		LeaseRef: stringMap(deployed, "leaseRef"), Fence: stringMap(deployed, "fence"), Generation: runtimeRevisionMapInt64(deployed, "generation")}
	if result, err := fixture.service.RenewSSHComposeInvocation(fixture.ctx, fixture.principal(t, "integration-gateway", "platform.runtime.ssh-compose.renew"), binding); result != nil || (!errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden)) {
		t.Fatal("source actor revocation did not close SSH renewal")
	}
	if result, err := fixture.service.ResolveSSHComposeCredentialProjection(fixture.ctx, fixture.principal(t, "secret-broker", "platform.credential-projections.ssh-compose.resolve"), binding); result != nil || (!errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden)) {
		t.Fatal("source actor revocation did not close broker credential projection")
	}
	gateway := fixture.principal(t, "integration-gateway", "platform.runtime.integrations.claim")
	completeSSHFixtureInvocation(t, fixture, gateway, deployed, "", "", true)
	root, err := f.service.GetRun(f.ctx, f.owner, f.runRef)
	if err != nil {
		t.Fatal("read root before explicit owner cancellation")
	}
	f.execute(command.CancelRun, "source-access-owner-cancel", &root.Version, command.RunCommandInput{RunRef: root.Ref, Reason: "Source access revoked"})
	read, err := fixture.service.GetIntegrationInvocation(fixture.ctx, gateway, stringMap(deployed, "invocationRef"))
	if err != nil || stringMap(read, "state") != "UNKNOWN_OUTCOME" || stringMap(read, "effectReceiptRef") != "" {
		t.Fatal("owner cancellation fabricated or erased the unknown external outcome")
	}
}
