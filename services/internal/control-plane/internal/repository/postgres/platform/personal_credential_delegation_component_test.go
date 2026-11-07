package platform

import (
	"context"
	"errors"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

// assertPersonalDelegatedCredentialContinuation uses a published legacy workflow to create
// a real delegated child, not SQL-prepared run/lease rows. The child retains root identity,
// consumes one Begin RPC per read and refuses a new intent after revoke. Explicit replacement
// and verification allow a fresh intent with the same root and a new immutable credential pin;
// completed receipts survive both changes. Child callback and a reconstructed service produce
// a fresh parent continuation/RR and terminal success. A fresh pool/repository/bootstrap also
// reopens durable state; later retry reads and Begin use that reconstructed owner. Provider
// health/output and transport peer
// admission are synthetic; this does not exercise live HTTP/JWT, process restart or archives.
// assertPersonalDelegatedCredentialContinuation создаёт реальный delegated child опубликованного
// legacy workflow без SQL-подготовки run/lease. Child сохраняет root identity, расходует один
// Begin RPC на read и отклоняет новое намерение после revoke. Явная замена и проверка ключа
// разрешают свежее намерение с прежним root и новым immutable credential pin; завершённые
// receipts сохраняются. Child callback и новый service создают свежие parent continuation/RR
// и terminal success. Новые pool/repository/bootstrap заново читают durable state; поздние
// retry reads и Begin используют восстановленный owner. Provider health/output и transport
// peer синтетические; live HTTP/JWT,
// process restart и archives здесь не проверяются.
func assertPersonalDelegatedCredentialContinuation(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner, secondUser value.Principal, connectionRef string) {
	t.Helper()
	workers, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	owner.Permission = "platform.personal-integrations.get"
	binding, err := service.GetPersonalIntegrationCredential(ctx, owner, connectionRef)
	if err != nil || binding.State != "READY" || binding.Credential == nil {
		t.Fatal("missing initial delegated credential", err)
	}
	original := binding.Credential.Ref
	execute := newComponentCommandExecutor(t, ctx, service, "personal-delegated-")
	project := execute(command.CreateProject, owner, "project", nil, command.ProjectInput{Name: "Personal delegated continuation", Language: "ru"}).Project
	coordinator := createLifecycleAgent(t, ctx, service, owner, project.Ref, "personal-delegated-coordinator", "Personal delegated coordinator")
	coordinator = *execute(command.ChangeAgentCapability, owner, "coordinator-delegation", &coordinator.Version, command.AgentBindingInput{AgentRef: coordinator.Ref, BindingRef: "platform.run.delegate", Enabled: true}).Agent
	reader := createLifecycleAgent(t, ctx, service, owner, project.Ref, "personal-delegated-reader", "Personal delegated reader")
	connection, err := service.GetIntegrationConnection(ctx, owner, connectionRef)
	if err != nil {
		t.Fatal(err)
	}
	execute(command.ChangeIntegrationGrant, owner, "grant", &connection.Version, command.IntegrationGrantInput{ConnectionRef: connectionRef, AgentRef: reader.Ref, CapabilityKey: "gitlab.project.metadata.read", ApprovalPolicy: "NONE", Enabled: true})
	draft := entity.WorkflowVersion{Name: "Personal delegated workflow", Purpose: "Keep root credentials through child completion", CoordinatorAgentRef: coordinator.Ref, Concurrency: 1, TimeoutSeconds: 240, CompletionCriteria: "Complete the single synthetic stage", ResultSchema: map[string]any{}, Steps: []entity.WorkflowStep{{Key: "read", Position: 1, Name: "Read", AgentRef: reader.Ref, Instructions: "Read synthetic metadata", ExpectedResult: "Bounded metadata result", TimeoutSeconds: 120, RequiredCapabilityKeys: []string{"gitlab.project.metadata.read"}}}}
	workflow := execute(command.CreateWorkflow, owner, "create", nil, command.WorkflowInput{ProjectRef: project.Ref, Name: draft.Name, Purpose: draft.Purpose, CoordinatorAgentRef: coordinator.Ref, Draft: &draft}).Workflow
	workflow = execute(command.ValidateWorkflow, owner, "validate", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
	workflow = execute(command.PublishWorkflow, owner, "publish", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
	root := execute(command.LaunchRun, owner, "launch", nil, command.LaunchRunInput{ProjectRef: project.Ref, Target: entity.RunTarget{Type: "WORKFLOW", Ref: workflow.Ref}, Task: "Read synthetic metadata through one delegated child"}).Run
	worker := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
	parent := claimComponentExecution(t, ctx, service, worker, root.Ref, "personal-delegated-parent")
	child := execute(command.DelegateExecution, worker, "delegate", nil, command.DelegateInput{LeaseRef: stringMap(parent, "leaseRef"), Fence: stringMap(parent, "fence"), Generation: runtimeRevisionMapInt64(parent, "generation"), TargetAgentRef: reader.Ref, WorkflowStepKey: "read", Task: "Return synthetic metadata"}).Run
	if child == nil || child.RootRunRef != root.Ref || child.ParentRunRef != root.Ref {
		t.Fatal("delegation lost its server-owned root edge")
	}
	completeClaimedExecution(t, ctx, service, worker, parent, "personal-delegated-parent-wait", false)
	lease := claimComponentExecution(t, ctx, service, worker, child.Ref, "personal-delegated-child")
	_, _, gateway := resolveIntegrationEffectTestPrincipals(t, ctx, repository)
	input := map[string]string{"run_ref": child.Ref, "node_ref": stringMap(lease, "nodeRef"), "connection_ref": connectionRef, "capability_key": "gitlab.project.metadata.read", "idempotency_key": "personal-delegated-first"}
	call := func(key, expectedCredential string) string {
		t.Helper()
		input["idempotency_key"] = key
		invocation, err := service.ResolveIntegrationInvocation(ctx, worker, input, map[string]any{})
		if err != nil {
			t.Fatal("resolve delegated personal intent", err)
		}
		work, err := service.ClaimIntegrationInvocations(ctx, gateway, "personal-delegated-gateway", 1)
		if err != nil || len(work) != 1 || stringMap(work[0], "invocationRef") != stringMap(invocation, "invocationRef") || work[0]["credential"] != nil {
			t.Fatal("claim delegated personal intent", err)
		}
		request := &cp.BeginPersonalIntegrationInvocationRequest{InvocationRef: stringMap(work[0], "invocationRef"), LeaseRef: stringMap(work[0], "leaseRef"), Fence: stringMap(work[0], "fence"), Generation: runtimeRevisionMapInt64(work[0], "generation"), WorkloadInstance: "personal-delegated-gateway"}
		admission, err := personalRuntimeRPCClient(t, workers).BeginPersonalIntegrationInvocation(personalRuntimeRPCContext(ctx), request)
		if err != nil || !admission.GetAdmitted() || admission.GetCredentialRevision().GetRef() != expectedCredential {
			t.Fatal("delegated Begin selected a different user's key", err)
		}
		var subject, storedRoot string
		if err := repository.pool.QueryRow(ctx, `SELECT pin.subject_id::text,root.ref FROM control_plane.integration_personal_invocation_credentials pin
     JOIN control_plane.integration_invocations invocation ON invocation.id=pin.invocation_id AND invocation.organization_id=pin.organization_id
     JOIN control_plane.runs root ON root.id=pin.root_run_id AND root.organization_id=pin.organization_id AND root.initiated_by=pin.subject_id
     WHERE invocation.ref=$1`, request.InvocationRef).Scan(&subject, &storedRoot); err != nil || subject != owner.ActorID || storedRoot != root.Ref {
			t.Fatal("delegated intent did not retain the root initiator", err)
		}
		summary := string(asJSON(map[string]any{"id": 5, "path_with_namespace": "fixture/delegated-project", "default_branch": "main", "visibility": "private", "archived": false}))
		execute(command.CompleteIntegrationInvocation, gateway, key+"-complete", nil, command.IntegrationInvocationInput{InvocationRef: request.InvocationRef, LeaseRef: request.LeaseRef, Fence: request.Fence, Generation: request.Generation, Success: true, EffectKey: stringMap(work[0], "effectKey"), InputDigest: stringMap(work[0], "inputDigest"), ProviderEffectRef: "gitlab-project:5", ResultSummary: summary, ResponseDigest: workflowResultSHA([]byte(summary))})
		outcome, err := service.GetIntegrationInvocation(ctx, gateway, request.InvocationRef)
		if err != nil || stringMap(outcome, "state") != "SUCCEEDED" || stringMap(outcome, "effectReceiptRef") == "" {
			t.Fatal("delegated read lost its durable receipt", err)
		}
		return request.InvocationRef
	}
	first := call("personal-delegated-first", original)
	owner.Permission = "platform.personal-integrations.revoke"
	execute(command.RevokePersonalIntegrationCredential, owner, "revoke", &binding.Version, command.PersonalIntegrationCredentialInput{ConnectionRef: connectionRef})
	input["idempotency_key"] = "personal-delegated-after-revoke"
	_, err = service.ResolveIntegrationInvocation(ctx, worker, input, map[string]any{})
	var problem *platformservice.IntegrationCredentialPreflightError
	if !errors.As(err, &problem) || len(problem.Issues) != 1 || problem.Issues[0].Problem != entity.IntegrationCredentialRevoked {
		t.Fatal("delegated call borrowed another key after revoke", err)
	}
	retained, err := service.GetIntegrationInvocation(ctx, gateway, first)
	if err != nil || stringMap(retained, "state") != "SUCCEEDED" || stringMap(retained, "effectReceiptRef") == "" {
		t.Fatal("revoke erased the completed delegated receipt", err)
	}
	owner.Permission = "platform.personal-integrations.get"
	binding, err = service.GetPersonalIntegrationCredential(ctx, owner, connectionRef)
	if err != nil {
		t.Fatal(err)
	}
	owner.Permission = "platform.personal-integrations.configure"
	binding, err = service.ConfigurePersonalIntegrationCredential(ctx, owner, value.Mutation{IdempotencyKey: "personal-delegated-replace", ExpectedVersion: &binding.Version}, connectionRef, binding.ConnectionVersion, []byte("synthetic-delegated-replacement"))
	if err != nil {
		t.Fatal(err)
	}
	binding = verifyPersonalAssistantKey(t, ctx, repository, service, workers, owner, connectionRef, "personal-delegated-replacement")
	if binding.Credential == nil || binding.Credential.Ref == original {
		t.Fatal("delegated rotation reused the previous immutable revision")
	}
	second := call("personal-delegated-after-revoke", binding.Credential.Ref)
	if first == second {
		t.Fatal("new delegated intent overwrote its completed predecessor")
	}
	completeClaimedExecution(t, ctx, service, worker, lease, "personal-delegated-child-complete", false)
	restartedPool, err := pgxpool.NewWithConfig(ctx, repository.pool.Config())
	if err != nil {
		t.Fatal("open fresh disposable owner pool", err)
	}
	defer restartedPool.Close()
	restartedRepository, err := New(restartedPool, "openai-codex", "gpt-5", repository.objects)
	if err != nil {
		t.Fatal("reconstruct owner repository", err)
	}
	configureSyntheticRuntimeComponentRepository(t, restartedRepository)
	configureComponentArtifactScanner(t, restartedRepository)
	if err := restartedRepository.Bootstrap(ctx); err != nil {
		t.Fatal("repeat owner startup on durable personal state", err)
	}
	rebuilt, err := platformservice.New(restartedRepository)
	if err != nil {
		t.Fatal(err)
	}
	continuation := claimComponentExecution(t, ctx, rebuilt, worker, root.Ref, "personal-delegated-continuation")
	if stringMap(continuation, "sessionRef") != stringMap(parent, "sessionRef") || stringMap(continuation, "runtimeRevisionRef") == stringMap(parent, "runtimeRevisionRef") {
		t.Fatal("delegated continuation lost its session or reused a stale RR")
	}
	result := completeClaimedExecution(t, ctx, rebuilt, worker, continuation, "personal-delegated-final", false)
	if result.Run == nil || result.Run.Ref != root.Ref || result.Run.State != "SUCCEEDED" {
		t.Fatal("delegated personal root failed its final continuation")
	}
	originalSession := root.SessionRef
	previewAuthorityGrant(t, ctx, service, owner, "personal-delegated-second-launch", "usr_personal_second", []string{"project.view", "agent.view", "agent.launch", "workflow.view", "workflow.launch", "run.view", "run.cancel"}, entity.AccessScope{Kind: "PROJECT", ProjectRef: project.Ref})
	secondUser.Permission = "platform.personal-integrations.get"
	other, err := service.GetPersonalIntegrationCredential(ctx, secondUser, connectionRef)
	if err != nil || other.State != "READY" || other.Credential == nil || other.Credential.Ref == binding.Credential.Ref {
		t.Fatal("second launcher lacks its independent ready key", err)
	}
	counts := func() [2]int {
		t.Helper()
		var value [2]int
		if err := repository.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM control_plane.runs),(SELECT count(*) FROM control_plane.session_turns)").Scan(&value[0], &value[1]); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := counts()
	_, err = service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: secondUser, Mutation: value.Mutation{IdempotencyKey: "personal-delegated-foreign-resume"}, Payload: command.LaunchRunInput{ProjectRef: project.Ref, SessionRef: originalSession, Target: entity.RunTarget{Type: "WORKFLOW", Ref: workflow.Ref}, Task: "Synthetic refused cross-user session continuation"}})
	if !errors.Is(err, errs.ErrForbidden) || counts() != before {
		t.Fatal("different launcher took over a personal session", err)
	}
	resumed := execute(command.LaunchRun, owner, "owner-session-reuse", nil, command.LaunchRunInput{ProjectRef: project.Ref, SessionRef: originalSession, Target: entity.RunTarget{Type: "WORKFLOW", Ref: workflow.Ref}, Task: "Synthetic owner session continuation"}).Run
	if resumed == nil || resumed.SessionRef != originalSession || resumed.Ref == root.Ref {
		t.Fatal("owner manual continuation lost its existing session")
	}
	cancelled := execute(command.CancelRun, owner, "cancel-continuation", &resumed.Version, command.RunCommandInput{RunRef: resumed.Ref}).Run
	if cancelled == nil || cancelled.State != "CANCELLED" {
		t.Fatal("personal retry source was not cancelled")
	}
	retry := command.Command{Kind: command.RetryRun, Principal: secondUser, Mutation: value.Mutation{IdempotencyKey: "personal-delegated-foreign-retry", ExpectedVersion: &cancelled.Version}, Payload: command.RunCommandInput{RunRef: cancelled.Ref}}
	before = counts()
	_, err = service.Execute(ctx, retry)
	if !errors.Is(err, errs.ErrForbidden) || counts() != before {
		t.Fatal("project launcher replaced the personal retry initiator", err)
	}
	owner.Permission = "platform.personal-integrations.revoke"
	execute(command.RevokePersonalIntegrationCredential, owner, "retry-revoke", &binding.Version, command.PersonalIntegrationCredentialInput{ConnectionRef: connectionRef})
	retry.Principal = owner
	retry.Mutation.IdempotencyKey = "personal-delegated-owner-retry"
	_, err = service.Execute(ctx, retry)
	problem = nil
	if !errors.As(err, &problem) || len(problem.Issues) != 1 || problem.Issues[0].Problem != entity.IntegrationCredentialRevoked || counts() != before {
		t.Fatal("retry created execution before current personal preflight", err)
	}
	owner.Permission = "platform.personal-integrations.get"
	binding, err = service.GetPersonalIntegrationCredential(ctx, owner, connectionRef)
	if err != nil {
		t.Fatal(err)
	}
	owner.Permission = "platform.personal-integrations.configure"
	binding, err = service.ConfigurePersonalIntegrationCredential(ctx, owner, value.Mutation{IdempotencyKey: "personal-delegated-retry-replacement", ExpectedVersion: &binding.Version}, connectionRef, binding.ConnectionVersion, []byte("synthetic-delegated-retry-key"))
	if err != nil {
		t.Fatal(err)
	}
	binding = verifyPersonalAssistantKey(t, ctx, repository, service, workers, owner, connectionRef, "personal-delegated-retry")
	retried, err := service.Execute(ctx, retry)
	if err != nil || retried.Run == nil || retried.Run.RetryOfRunRef != cancelled.Ref || retried.Run.SessionRef != originalSession {
		t.Fatal("owner retry lost session/retry lineage after key replacement", err)
	}
	root = retried.Run
	service, workers = rebuilt, rebuilt
	execute = newComponentCommandExecutor(t, ctx, rebuilt, "personal-delegated-rebuilt-")
	parent = claimComponentExecution(t, ctx, rebuilt, worker, root.Ref, "personal-delegated-retry-parent")
	child = execute(command.DelegateExecution, worker, "retry-delegate", nil, command.DelegateInput{LeaseRef: stringMap(parent, "leaseRef"), Fence: stringMap(parent, "fence"), Generation: runtimeRevisionMapInt64(parent, "generation"), TargetAgentRef: reader.Ref, WorkflowStepKey: "read", Task: "Return synthetic retry metadata"}).Run
	if child == nil || child.RootRunRef != root.Ref || child.ParentRunRef != root.Ref {
		t.Fatal("retry child lost the new root and original initiating user")
	}
	completeClaimedExecution(t, ctx, rebuilt, worker, parent, "personal-delegated-retry-parent-wait", false)
	lease = claimComponentExecution(t, ctx, rebuilt, worker, child.Ref, "personal-delegated-retry-child")
	input["run_ref"], input["node_ref"] = child.Ref, stringMap(lease, "nodeRef")
	call("personal-delegated-retry-read", binding.Credential.Ref)
	completeClaimedExecution(t, ctx, rebuilt, worker, lease, "personal-delegated-retry-child-complete", false)
	final := claimComponentExecution(t, ctx, rebuilt, worker, root.Ref, "personal-delegated-retry-continuation")
	result = completeClaimedExecution(t, ctx, rebuilt, worker, final, "personal-delegated-retry-final", false)
	if result.Run == nil || result.Run.State != "SUCCEEDED" {
		t.Fatal("retried personal workflow did not complete")
	}
}
